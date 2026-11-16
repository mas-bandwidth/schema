package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCppGraphCheckedOverEveryPointer holds docs/SPEC-TABLES.md §6.5 (THE
// GRAPH, DEFINED ONCE) and §3.1 on the C++ leg: the graph conditions are
// checked over EVERY kind-17 payload in the input, in fields and records this
// reader declares and in those it only skips. A pointer naming a node whose
// descent is still open is a cycle, a record no pointer reaches is
// unreachable, a blob record named from two slots is blob_shared, and a string
// blob record is well-formed text wherever it is reached from; each refuses
// the whole load. A record of a type this reader cannot name, reached through
// any pointer, reads null and counts unknown: that stays a read.
//
// Each graph_* framing vector runs through graphdemo Scene's strict load and
// its retaining load, and the verdict alone is compared (docs/rulings.json
// refusal-verdict-only); node_chain_two through ListNode is the control that
// reads.
func TestCppGraphCheckedOverEveryPointer(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	framing := filepath.Join(repo, "testdata", "wire", "tables", "framing")
	cases := []struct {
		name string
		root string
		read bool
	}{
		{"graph_unknown_type_self", "Scene", false},
		{"graph_unknown_type_pair", "Scene", false},
		{"graph_listnode_unknown_self", "Scene", false},
		{"graph_unknown_to_root", "Scene", false},
		{"graph_blob_two_unknown_fields", "Scene", false},
		{"graph_blob_two_unknown_elements", "Scene", false},
		{"graph_string_blob_ill_formed", "Scene", false},
		{"graph_string_blob_zero_byte", "Scene", false},
		{"graph_unknown_type_unreached", "Scene", false},
		{"graph_string_blob_unreached", "Scene", false},
		{"graph_reach_unknown_keyed_slot", "Scene", true},
		// Kind 17 is not on the keyed body's closed list (§3). A separate
		// mutation keeps this refusal covered alongside the legal table case.
		{"graph_reach_keyed_illegal_kind", "Scene", false},
		{"graph_reach_unknown_field", "Scene", true},
		{"graph_reach_unknown_nested", "Scene", true},
		{"graph_reach_unknown_union_arm", "Scene", true},
		{"node_chain_two", "ListNode", true},
	}
	u := loadGraphDirUnit(t, filepath.Join(repo, "tables", "pointers"))
	var body strings.Builder
	for _, c := range cases {
		name := c.name
		if name == "graph_reach_keyed_illegal_kind" {
			name = "graph_reach_unknown_keyed_slot"
		}
		wire, err := os.ReadFile(filepath.Join(framing, name+".bin"))
		if err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", name, err)
		}
		if c.name == "graph_reach_keyed_illegal_kind" {
			if len(wire) < 5 || wire[4] != 13 {
				t.Fatal("keyed table element kind moved")
			}
			wire[4] = 17
		}
		fmt.Fprintf(&body, `
    {
        const uint8_t wire[] = { %s };
        const bool want = %t;
        const int64_t need = %sLoadMeasure( wire, (int64_t) sizeof( wire ) );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g; memset( &g, 0, sizeof( g ) );
        const %s * root = %sLoad( aligned, need > 0 ? need : 4096, wire, (int64_t) sizeof( wire ), &g );
        if ( ( root != NULL ) != want ) { printf( "%s: the strict load %s (root=%%d want=%%d)\n", root != NULL ? 1 : 0, (int) want ); return 1; }
        if ( !want && !g.damaged && !g.refused ) { printf( "%s: a refused load reported neither damage nor a refusal\n" ); return 1; }

        alignas( 64 ) static uint8_t retain_region[ 1 << 16 ];
        std::vector<uint8_t> store( 1 << 16 );
        std::vector<TableRetain::Id> ids( 1 << 12 );
        TableRetain retain;
        retain.bytes = store.data(); retain.capacity = (int64_t) store.size();
        retain.ids = ids.data(); retain.id_capacity = (int64_t) ids.size();
        TableReport rr; memset( &rr, 0, sizeof( rr ) );
        const %s * kept = %sLoadRetain( retain_region, (int64_t) sizeof( retain_region ), wire, (int64_t) sizeof( wire ), &retain, &rr );
        if ( ( kept != NULL ) != want ) { printf( "%s: the retaining load %s (root=%%d want=%%d)\n", kept != NULL ? 1 : 0, (int) want ); return 1; }
        if ( !want && ( retain.used != 0 || retain.count != 0 ) ) { printf( "%s: a refused retaining load left retained bytes or ids\n" ); return 1; }
    }
`, cppBytes(wire), c.read, c.root, c.root, c.root, c.name, map[bool]string{true: "refused a read", false: "read a refusal"}[c.read],
			c.name, c.root, c.root, c.name, map[bool]string{true: "refused a read", false: "read a refusal"}[c.read],
			c.name)
	}
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("the graph probe failed: %v\n%s", err, out)
	}
}

// loadGraphDirUnit builds one unit from every *.schema in a directory.
func loadGraphDirUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(e.Name(), ".schema")
		ast, errs := parser.Parse(e.Name(), src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", e.Name(), errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: e.Name(), Base: base, Bytes: src, AST: ast})
	}
	if len(files) == 0 {
		t.Fatalf("no .schema files in %s", dir)
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}
