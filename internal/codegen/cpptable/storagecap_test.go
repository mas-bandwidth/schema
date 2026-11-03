package cpptable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or record of it costs two wire bytes,
// so 8191 of them measure under the 2^40-byte cap and 8192 measure over it.
// The list vectors reach the cap through the list extent (count times element
// size), the node vectors through the node scan (one record's storage each).
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

var storageCapVectors = []struct {
	file  string
	nodes bool
	count uint64
	over  bool
}{
	{"list_at.bin", false, 8191, false},
	{"list_over.bin", false, 8192, true},
	{"nodes_at.bin", true, 8191, false},
	{"nodes_over.bin", true, 8192, true},
}

// storageCapWire writes one vector: a form-1 file whose root holds count
// empty Big bodies, either as the items list or as node-table records.
func storageCapWire(u *ir.Unit, nodes bool, count uint64) []byte {
	var ids []uint64
	ref := func(id uint64) []byte {
		for i, v := range ids {
			if v == id {
				return binary.AppendUvarint(nil, uint64(i+1))
			}
		}
		ids = append(ids, id)
		return binary.AppendUvarint(nil, uint64(len(ids)))
	}
	framed := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	var root []byte
	if nodes {
		table := ref(math.MaxUint64)
		big := ref(ir.TableWireId(u.Tables["Big"].WireName()))
		records := binary.AppendUvarint(nil, count)
		for range count {
			records = append(records, big...)
			records = append(records, 1, 0)
		}
		root = append(append(table, byte(ir.TableKindString)), framed(records)...)
	} else {
		items := ref(ir.TableFieldWireId(u.Tables["Root"].Fields[0]))
		list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
		for range count {
			list = append(list, 1, 0)
		}
		root = append(append(items, byte(ir.TableKindArray)), framed(list)...)
	}
	wire := append([]byte{1}, root...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func storageCapUnit(t *testing.T) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(storageCapDir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	return loadUnitFromSource(t, string(src))
}

// TestStorageCapVectorsAreCurrent holds the committed vectors to the writer
// above. SCHEMA_UPDATE_STORAGE_CAP=1 rewrites them.
func TestStorageCapVectorsAreCurrent(t *testing.T) {
	t.Parallel()
	u := storageCapUnit(t)
	for _, v := range storageCapVectors {
		want := storageCapWire(u, v.nodes, v.count)
		path := filepath.Join(storageCapDir, v.file)
		if os.Getenv("SCHEMA_UPDATE_STORAGE_CAP") == "1" {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is not what storageCapWire writes (SCHEMA_UPDATE_STORAGE_CAP=1 rewrites it)", v.file)
		}
	}
}

// TestStorageCapRefusedAtMeasureCPP: the reference refuses each over vector
// as storage_over_cap from RootLoadMeasure, RootReadStrict and
// RootLoadTolerant, before any region is sized, and answers a size at or
// under 2^40 for each at vector.
//
// The node vectors are also read under a unit with no list or map, whose
// Load sums node storage without an extent walk.
func TestStorageCapRefusedAtMeasureCPP(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	t.Run("extents", func(t *testing.T) {
		t.Parallel()
		runStorageCapProbe(t, storageCapUnit(t), false)
	})
	t.Run("nodes-only", func(t *testing.T) {
		t.Parallel()
		u := loadUnitFromSource(t, "package capcheck\nfixed table Big { a [16777216]int64 }\ntable Root { next *Big }\n")
		runStorageCapProbe(t, u, true)
	})
}

func runStorageCapProbe(t *testing.T, u *ir.Unit, nodesOnly bool) {
	t.Helper()
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    alignas(64) static uint8_t region[65536];
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, v := range storageCapVectors {
		if nodesOnly && !v.nodes {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(storageCapDir, v.file))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, "    {\n        static const uint8_t wire[] = { %s };\n", cppBytes(wire))
		body.WriteString("        TableRefuseReason why = ok;\n        const int64_t size = RootLoadMeasure( wire, (int64_t) sizeof( wire ), NULL, &why );\n")
		if v.over {
			fmt.Fprintf(&body, `        CHECK( size == -1 && why == storage_over_cap, "%[1]s measure" );
        TableRefuseReason strict = ok;
        CHECK( RootReadStrict( wire, (int64_t) sizeof( wire ), &strict ) == NULL && strict == storage_over_cap, "%[1]s strict read" );
        TableReport report;
        CHECK( RootLoadTolerant( region, (int64_t) sizeof( region ), wire, (int64_t) sizeof( wire ), &report ) == NULL, "%[1]s load" );
        CHECK( report.refused && report.reason == storage_over_cap && report.refuse_reason == storage_over_cap && !report.malformed, "%[1]s load names the cap" );
`, v.file)
		} else {
			fmt.Fprintf(&body, `        CHECK( size > 0 && size <= kTableStorageCap && why == ok, "%[1]s measure" );
`, v.file)
		}
		body.WriteString("    }\n")
	}
	// each extent term is refused before it is summed: exactly at the cap
	// reads, one element or entry past it refuses
	if !nodesOnly {
		body.WriteString(`    {
        static const uint8_t list[] = { 4, 1, 0, 0, 0, 0 };
        static const uint8_t map[] = { 13, 1, 1, 0 };
        TableRefuseReason why = ok;
        int64_t at = kTableStorageCap - 16;
        CHECK( TableListWireExtent( list, (int64_t) sizeof( list ), at, 16, 4, 4, 4, NULL, NULL, why ) && at == kTableStorageCap, "list term at the cap" );
        at = kTableStorageCap - 15;
        CHECK( !TableListWireExtent( list, (int64_t) sizeof( list ), at, 16, 4, 4, 4, NULL, NULL, why ) && why == storage_over_cap, "list term past the cap" );
        why = ok;
        at = kTableStorageCap - 16;
        CHECK( TableMapWireExtent( map, (int64_t) sizeof( map ), at, 16, 4, NULL, NULL, why ) && at == kTableStorageCap, "map term at the cap" );
        at = kTableStorageCap - 15;
        CHECK( !TableMapWireExtent( map, (int64_t) sizeof( map ), at, 16, 4, NULL, NULL, why ) && why == storage_over_cap, "map term past the cap" );
    }
`)
	}
	body.WriteString(`    CHECK( kTableStorageCap == ( (int64_t) 1 << 40 ), "the cap is 2^40" );
    if (failures != 0) return 1;
`)
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
