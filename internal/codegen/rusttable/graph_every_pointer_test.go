package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRustGraphCheckedOverEveryPointer holds both Rust strict readers to the
// declaration-free graph walk in docs/SPEC-TABLES.md §3.1 and §6.5.
func TestRustGraphCheckedOverEveryPointer(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
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
		{"graph_reach_unknown_field", "Scene", true},
		{"graph_reach_unknown_nested", "Scene", true},
		{"graph_reach_unknown_union_arm", "Scene", true},
		{"graph_reach_unknown_keyed_slot", "Scene", true},
		{"node_chain_two", "ListNode", true},
	}
	var body strings.Builder
	body.WriteString("let mut failed = false;\n")
	for _, c := range cases {
		wire, err := os.ReadFile(filepath.Join(framing, c.name+".bin"))
		if err != nil {
			t.Fatalf("read %s: %v", c.name, err)
		}
		fn := "scene"
		if c.root == "ListNode" {
			fn = "list_node"
		}
		fmt.Fprintf(&body, `{{ let wire = vec![%s];
let mut r = TableReport::default(); let v = %s_load(&wire, &mut r);
let expected = %t;
if v.is_some() != expected || (r.err().is_none()) != expected { println!("FAIL: %s plain verdict"); failed = true; }
let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256) };
let mut rr = TableReport::default(); let rv = %s_load_retain(&wire, &mut retain, &mut rr);
if rv.is_some() != expected || (rr.err().is_none()) != expected || (!expected && (!retain.bytes.is_empty() || !retain.ids.is_empty())) { println!("FAIL: %s retaining verdict"); failed = true; }
}}`, rustBytes(wire), fn, c.read, c.name, fn, c.name)
	}
	body.WriteString("if failed { std::process::exit(1); }\n")
	files, err := Generate(vcLoadUnit(t, repo, "tables/pointers"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out, err := runRustGeneratedFilesProbe(t, files, body.String())
	if err != nil {
		t.Fatalf("graph probe failed: %v\n%s", err, out)
	}
}
