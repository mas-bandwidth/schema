// A pointer's target type in the variable form (docs/SPEC-TABLES.md §3.1;
// docs/rulings.json pointer-target-type-checked). A pointer slot naming a
// record whose type this reader DECLARES but which is not the type the slot
// requires is damage, and the whole load refuses; a record whose type this
// reader cannot name at all (a newer writer's type) is counted unknown once and
// every pointer naming it reads null, the load accepted. The vectors under
// testdata/wire/pointer-target-type state the cases, and every C read of the
// variable form holds each row's verdict: the plain region read, the builder
// read, the retaining read, and the measure that answers a verdict.
package ctable

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type cPointerTargetRow struct {
	name, unit, root, file, verdict string
	unknown                         int
}

func cPointerTargetRows(t *testing.T, dir string) []cPointerTargetRow {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	defer f.Close()
	var rows []cPointerTargetRow
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 6 {
			t.Fatalf("MANIFEST.tsv row %q has %d columns, want 6", line, len(cols))
		}
		row := cPointerTargetRow{name: cols[0], unit: cols[1], root: cols[2], file: cols[3], verdict: cols[4], unknown: -1}
		if cols[5] != "-" {
			n, err := strconv.Atoi(cols[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv row %s: unknown %q: %v", row.name, cols[5], err)
			}
			row.unknown = n
		}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv row %s: verdict %q", row.name, row.verdict)
		}
		rows = append(rows, row)
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestCPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "pointer-target-type")
	rows := cPointerTargetRows(t, dir)
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	unit := rows[0].unit
	var refused, read, unknownRows int
	var body strings.Builder
	for _, row := range rows {
		if row.unit != unit || row.root != "Node" {
			t.Fatalf("row %s reads %s at %s; this probe reads %s at Node", row.name, row.unit, row.root, unit)
		}
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("row %s: %v", row.name, err)
		}
		accept := 0
		if row.verdict == "read" {
			accept = 1
			read++
		} else {
			refused++
		}
		if row.unknown > 0 {
			unknownRows++
		}
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        const int accept = %d, unknown = %d;
        int retaining;
        for (retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store;
            const Node * root;
            memset(region, 0, sizeof(region));
            memset(&report, 0, sizeof(report));
            memset(&store, 0, sizeof(store));
            store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
            store.ids = retain_ids; store.id_capacity = 64;
            root = retaining
                ? node_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : node_load(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK((root != NULL) == accept, retaining ? "%s: node_load_retain verdict" : "%s: node_load verdict");
            CHECK(accept || report.refused, retaining ? "%s: node_load_retain report" : "%s: node_load report");
            if (root != NULL) {
                CHECK(unknown < 0 || report.unknown == unknown, retaining ? "%s: node_load_retain unknown" : "%s: node_load unknown");
                CHECK(unknown <= 0 || root->next.value == 0, retaining ? "%s: node_load_retain next reads null" : "%s: node_load next reads null");
                CHECK(unknown != 0 || root->next.value != 0, retaining ? "%s: node_load_retain next reads its node" : "%s: node_load next reads its node");
            }
        }
        {
            NodeBuilder builder;
            TableReport report;
            int ok;
            CHECK(node_builder_init(&builder), "%s: builder init");
            memset(&report, 0, sizeof(report));
            ok = node_load_builder(&builder, wire, (int64_t)sizeof(wire), &report);
            CHECK((ok != 0) == accept, "%s: node_load_builder verdict");
            CHECK(!ok || unknown < 0 || report.unknown == unknown, "%s: node_load_builder unknown");
            node_builder_shutdown(&builder);
        }
        {
            int64_t attribution = 0;
            TableRefuseReason reason = SCHEMA_TABLE_REFUSE_OK;
            int64_t bytes = node_load_measure_ex(wire, (int64_t)sizeof(wire), &attribution, &reason);
            CHECK((bytes >= 0) == accept, "%s: node_load_measure_ex verdict");
        }
    }
`, cBytes(wire), accept, row.unknown,
			row.name, row.name, row.name, row.name, row.name, row.name, row.name, row.name, row.name, row.name,
			row.name, row.name, row.name, row.name)
	}
	if refused == 0 || read == 0 || unknownRows == 0 {
		t.Fatalf("MANIFEST.tsv holds %d read, %d refused and %d unknown rows; the probe needs each", read, refused, unknownRows)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "..", unit, "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	output, err := runCCountRefusalProbe(t, u, `
    int failures = 0;
    static uint8_t region[65536] __attribute__((aligned(64)));
    static uint8_t retain_bytes[4096];
    static TableRetainId retain_ids[64];
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`+body.String()+`
    if (failures != 0) { return 1; }
`)
	if err != nil {
		t.Fatalf("pointer-target-type probe: %v\n%s", err, output)
	}
}
