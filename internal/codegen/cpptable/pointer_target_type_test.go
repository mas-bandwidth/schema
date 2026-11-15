package cpptable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCppPointerTargetTypeChecked holds the C++ leg to docs/SPEC-TABLES.md
// §3.1's rule for a pointer's target type (docs/rulings.json
// pointer-target-type-checked) over the shared vectors in
// testdata/wire/pointer-target-type: in the variable form, a pointer slot
// naming a record of a type this reader DECLARES that is not the type the slot
// requires is damage and refuses the whole load; a record of a type the reader
// cannot name at all is counted unknown once and every pointer naming it reads
// null. Each manifest row runs through every variable-form read: the region
// Load, the retaining Load, the builder Load and the strict read, and each
// must answer the row's verdict. Only the verdict is compared, never a
// refusal's name (docs/rulings.json refusal-verdict-only).
func TestCppPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated reader over the shared vectors)")

	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "pointer-target-type"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	type row struct {
		name, unit, root, file, verdict string
		unknown                         int
	}
	var rows []row
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("manifest line %q: %d columns, want 6", line, len(f))
		}
		r := row{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if r.verdict != "read" && r.verdict != "refused" {
			t.Fatalf("manifest row %s: verdict %q", r.name, r.verdict)
		}
		if f[5] != "-" {
			if r.unknown, err = strconv.Atoi(f[5]); err != nil {
				t.Fatalf("manifest row %s: unknown %q", r.name, f[5])
			}
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no vectors")
	}
	unit := rows[0].unit
	for _, r := range rows {
		if r.unit != unit || r.root != "Node" {
			t.Fatalf("row %s reads %s at %s; this test reads every row at Node in %s", r.name, r.unit, r.root, unit)
		}
	}
	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", unit, "*.schema"))
	if err != nil || len(schemas) != 1 {
		t.Fatalf("%s: want one schema file, got %v (%v)", unit, schemas, err)
	}
	src, err := os.ReadFile(schemas[0])
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitFromSource(t, string(src))

	var body strings.Builder
	for _, r := range rows {
		// the writer-only row is the one whose next names a record this reader
		// cannot name: its pointer must read null
		fill := fmt.Sprintf("if ( !slurp( %q, wire ) ) { printf( \"%s: cannot read the vector\\n\" ); return 1; }", filepath.Join(dir, r.file), r.name)
		body.WriteString(cppPointerTargetProbe(r.name, fill, r.verdict == "read", r.unknown, r.unknown > 0))
	}
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)

	t.Run("a declared target below the root", testCppPointerTargetInClosure)
}

// testCppPointerTargetInClosure is the same rule where the wrong type IS one
// this root can place: `next` (a *Node) turned at the root's Quant record. The
// engine encodes the graph and the one index byte is patched, and every read,
// the retaining one included, must refuse it.
func testCppPointerTargetInClosure(t *testing.T) {
	t.Parallel()
	u := loadUnitFromSource(t, `package pointertargetclosure

table Node
{
    value uint32
    next  *Node
    q     *Quant
}

table Quant
{
    q uint32
}
`)
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Node"))
	other := m.New(m.Lookup("Node"))
	quant := m.New(m.Lookup("Quant"))
	next, _ := root.FieldByKey("next")
	next.Cell.Node = other
	q, _ := root.FieldByKey("q")
	q.Cell.Node = quant
	wire, err := tablewire.Encode(m, root)
	if err != nil {
		t.Fatal(err)
	}
	body := cppPointerTargetProbe("next at a Node", cppPointerTargetInline(wire), true, 0, false)
	// next names index 2 (the second Node) and q index 3 (the Quant)
	at := bytes.Index(wire, []byte{byte(ir.TableKindPointer), 2})
	if at < 0 || bytes.Count(wire, []byte{byte(ir.TableKindPointer), 2}) != 1 || !bytes.Contains(wire, []byte{byte(ir.TableKindPointer), 3}) {
		t.Fatalf("the two pointers are not where this test expects them: % x", wire)
	}
	wire[at+1] = 3
	body += cppPointerTargetProbe("next at a Quant", cppPointerTargetInline(wire), false, -1, false)
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}

func cppPointerTargetInline(wire []byte) string {
	return fmt.Sprintf("{ const uint8_t raw[] = { %s }; wire.assign( raw, raw + sizeof( raw ) ); }", cppBytes(wire))
}

// cppPointerTargetProbe is the C++ that reads one wire (fill puts it in
// `wire`) through every variable-form read of the root Node — the region Load
// sized by its measure, the retaining Load, the builder Load and the strict
// read — and demands the verdict of each: read (with the unknown count where
// one is pinned, and next null where nullNext), or refused.
func cppPointerTargetProbe(name, fill string, want bool, unknown int, nullNext bool) string {
	return fmt.Sprintf(`
    {
        const char * name = %q;
        const bool want = %t;
        const int unknown = %d;
        const bool null_next = %t;
        std::vector<uint8_t> wire;
        %s
        const int64_t bytes = (int64_t) wire.size();
        int bad = 0;

        const int64_t need = NodeLoadMeasure( wire.data(), bytes );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g;
        const Node * root = NodeLoad( aligned, need > 0 ? need : 4096, wire.data(), bytes, &g );
        if ( ( root != NULL ) != want ) { printf( "%%s: Load: root=%%d damaged=%%d refused=%%d\n", name, root != NULL ? 1 : 0, (int) g.damaged, (int) g.refused ); bad = 1; }
        else if ( want && unknown >= 0 && g.unknown != unknown ) { printf( "%%s: Load: unknown=%%d, want %%d\n", name, (int) g.unknown, unknown ); bad = 1; }
        else if ( want && null_next && root->next.value != 0 ) { printf( "%%s: Load: next is not null\n", name ); bad = 1; }

        alignas( 64 ) static uint8_t retain_region[ 1 << 16 ];
        uint8_t store[ 1 << 12 ];
        TableRetain retain;
        TableRetain::Id ids[16];
        retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
        retain.ids = ids; retain.id_capacity = 16;
        TableReport rr;
        const Node * kept = NodeLoadRetain( retain_region, (int64_t) sizeof( retain_region ), wire.data(), bytes, &retain, &rr );
        if ( ( kept != NULL ) != want ) { printf( "%%s: LoadRetain: root=%%d damaged=%%d refused=%%d\n", name, kept != NULL ? 1 : 0, (int) rr.damaged, (int) rr.refused ); bad = 1; }
        else if ( want && unknown >= 0 && rr.unknown != unknown ) { printf( "%%s: LoadRetain: unknown=%%d, want %%d\n", name, (int) rr.unknown, unknown ); bad = 1; }
        else if ( want && null_next && kept->next.value != 0 ) { printf( "%%s: LoadRetain: next is not null\n", name ); bad = 1; }

        NodeBuilder builder;
        TableReport br;
        const bool built = NodeLoadBuilder( builder, wire.data(), bytes, &br );
        if ( built != want ) { printf( "%%s: LoadBuilder: ok=%%d damaged=%%d refused=%%d\n", name, (int) built, (int) br.damaged, (int) br.refused ); bad = 1; }
        else if ( want && unknown >= 0 && br.unknown != unknown ) { printf( "%%s: LoadBuilder: unknown=%%d, want %%d\n", name, (int) br.unknown, unknown ); bad = 1; }
        else if ( want && null_next && builder.GetRoot()->next.value != 0 ) { printf( "%%s: LoadBuilder: next is not null\n", name ); bad = 1; }

        TableRefuseReason why = ok;
        const Node * strict = NodeReadStrict( wire.data(), bytes, &why );
        if ( ( strict != NULL ) != want ) { printf( "%%s: ReadStrict: root=%%d reason=%%d\n", name, strict != NULL ? 1 : 0, (int) why ); bad = 1; }
        else if ( want && null_next && strict->next.value != 0 ) { printf( "%%s: ReadStrict: next is not null\n", name ); bad = 1; }
        if ( strict != NULL ) { table_release( TableDefaultAllocator(), (void *) strict ); }

        if ( bad ) { return 1; }
        printf( "%%s: %%s on every read\n", name, want ? "read" : "refused" );
    }
`, name, want, unknown, nullNext, fill)
}
