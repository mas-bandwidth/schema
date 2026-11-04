package ctable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or record of it costs two wire bytes,
// so 8191 of them measure under the 2^40-byte cap and 8192 measure over it.
const cStorageCapDir = "../../../testdata/wire/tables/storage-cap"

var cStorageCapVectors = []struct {
	file  string
	nodes bool
	over  bool
}{
	{"list_at.bin", false, false},
	{"list_over.bin", false, true},
	{"nodes_at.bin", true, false},
	{"nodes_over.bin", true, true},
}

// TestStorageCapRefusedAtMeasureC: the C leg refuses each over vector as
// storage_over_cap from root_load_measure_ex (root_load_measure answers -1),
// from the strict root_load, and from root_load_tolerant and root_load_retain
// before any region or builder is filled, and measures each at vector at a
// size at or under 2^40. No at vector is loaded: its region is about 1 TiB.
//
// The node vectors are also read under a unit with no list or map, whose
// measure sums node storage without a list extent term.
func TestStorageCapRefusedAtMeasureC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	t.Run("extents", func(t *testing.T) {
		t.Parallel()
		src, err := os.ReadFile(filepath.Join(cStorageCapDir, "Cap.schema"))
		if err != nil {
			t.Fatal(err)
		}
		runCStorageCapProbe(t, unitFrom(t, string(src)), false)
	})
	t.Run("nodes-only", func(t *testing.T) {
		t.Parallel()
		runCStorageCapProbe(t, unitFrom(t, "package capcheck\nfixed table Big { a [16777216]int64 }\ntable Root { next *Big }\n"), true)
	})
}

func runCStorageCapProbe(t *testing.T, u *ir.Unit, nodesOnly bool) {
	t.Helper()
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    static uint8_t region[65536] __attribute__((aligned(64)));
    static uint8_t retain_bytes[4096];
    static TableRetainId retain_ids[64];
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, v := range cStorageCapVectors {
		if nodesOnly && !v.nodes {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(cStorageCapDir, v.file))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, "    {\n        static const uint8_t wire[] = { %s };\n", cBytes(wire))
		body.WriteString("        TableRefuseReason why = SCHEMA_TABLE_REFUSE_OK;\n        int64_t attribution = 0;\n        const int64_t size = root_load_measure_ex( wire, (int64_t) sizeof( wire ), &attribution, &why );\n        const int64_t plain = root_load_measure( wire, (int64_t) sizeof( wire ) );\n")
		if v.over {
			fmt.Fprintf(&body, `        CHECK( size == -1 && plain == -1 && why == SCHEMA_TABLE_REFUSE_STORAGE_OVER_CAP, "%[1]s measure" );
        {
            TableReport strict;
            CHECK( root_load( region, (int64_t) sizeof( region ), wire, (int64_t) sizeof( wire ), &strict ) == NULL, "%[1]s strict read" );
            CHECK( strict.refused && strict.refuse_reason == SCHEMA_TABLE_REFUSE_STORAGE_OVER_CAP && !strict.malformed, "%[1]s strict read names the cap" );
        }
        {
            TableReport report;
            memset( &report, 0, sizeof( report ) );
            CHECK( root_load_tolerant( region, (int64_t) sizeof( region ), wire, (int64_t) sizeof( wire ), &report ) == NULL, "%[1]s load" );
            CHECK( report.refused && report.reason == SCHEMA_TABLE_REFUSE_STORAGE_OVER_CAP && report.refuse_reason == SCHEMA_TABLE_REFUSE_STORAGE_OVER_CAP && !report.malformed, "%[1]s load names the cap" );
        }
        {
            TableReport report;
            TableRetain store;
            memset( &report, 0, sizeof( report ) );
            memset( &store, 0, sizeof( store ) );
            store.bytes = retain_bytes; store.capacity = (int64_t) sizeof( retain_bytes );
            store.ids = retain_ids; store.id_capacity = 64;
            CHECK( root_load_retain( region, (int64_t) sizeof( region ), wire, (int64_t) sizeof( wire ), &store, &report ) == NULL, "%[1]s retain load" );
            CHECK( report.refused && report.reason == SCHEMA_TABLE_REFUSE_STORAGE_OVER_CAP && !report.malformed, "%[1]s retain load names the cap" );
        }
`, v.file)
		} else {
			fmt.Fprintf(&body, `        CHECK( size > 0 && size <= SCHEMA_TABLE_STORAGE_CAP && plain == size && why == SCHEMA_TABLE_REFUSE_OK, "%[1]s measure" );
`, v.file)
		}
		body.WriteString("    }\n")
	}
	if !nodesOnly {
		// each list or map term is refused before it is summed: a term that
		// ends exactly at the cap reads, one element past it refuses
		body.WriteString(`    CHECK( !table_extent_over_cap( SCHEMA_TABLE_STORAGE_CAP - 16, 1, 16, 4 ), "term at the cap" );
    CHECK( table_extent_over_cap( SCHEMA_TABLE_STORAGE_CAP - 15, 1, 16, 4 ), "term past the cap" );
    CHECK( !table_extent_over_cap( 0, UINT64_C(65536), 16777216, 8 ), "a 2^40 term from zero" );
    CHECK( table_extent_over_cap( 0, UINT64_C(65537), 16777216, 8 ), "one element past a 2^40 term" );
`)
	}
	body.WriteString(`    CHECK( SCHEMA_TABLE_STORAGE_CAP == ( (int64_t) 1 << 40 ), "the cap is 2^40" );
    if (failures != 0) return 1;
`)
	out, err := runCCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
