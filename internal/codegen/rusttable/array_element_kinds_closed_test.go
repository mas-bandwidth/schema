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

// TestRustArrayElementKindsClosed holds the Rust leg to the closed element
// lists of docs/SPEC-TABLES.md §3: an array body (kind 14) may open with 1–13,
// 15, 17 or 18–30, and an enum-keyed body (kind 16) with 1–11, 13 or 18–30.
// Any other element kind is damage under ANY count, a count of 0 included, in
// a field the reader declares and in one it cannot name alike. Each vector
// rides an unknown root field, so the wire-alone walk must judge the header
// byte even when it skips the body. Verdicts only, never a reason
// (docs/rulings.json refusal-verdict-only).
func TestRustArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		read bool
	}{
		// REFUSED: element kinds off the closed list, under a count of one or more.
		{name: "array_elem_kind_32_count_2", read: false},
		{name: "array_elem_kind_31_count_1", read: false},
		{name: "array_elem_kind_16_count_1", read: false},
		// REFUSED: element kinds off the closed list under a count of zero.
		{name: "array_elem_unassigned_count_0", read: false},
		{name: "array_elem_reserved_count_0", read: false},
		{name: "array_elem_zero_count_0", read: false},
		// READ: element kind 17 is on the array list, at count 0 and at count 1
		// where its node index names a reachable record.
		{name: "array_elem_kind_17_count_0", read: true},
		{name: "array_elem_kind_17_count_1", read: true},
	}
	framingDir := filepath.Join(root, "testdata", "wire", "tables", "framing")
	var body strings.Builder
	body.WriteString("let mut failed = false;\n")
	for _, c := range cases {
		wire, err := os.ReadFile(filepath.Join(framingDir, c.name+".bin"))
		if err != nil {
			t.Fatalf("read %s: %v", c.name, err)
		}
		fmt.Fprintf(&body, `{ let wire = vec![%s];
let mut r = TableReport::default(); let v = scene_load(&wire, &mut r);
let expected = %t;
if v.is_some() != expected || (r.err().is_none()) != expected { println!("FAIL: %s plain verdict accepted={} want={}", v.is_some(), expected); failed = true; }
let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
let mut rr = TableReport::default(); let rv = scene_load_retain(&wire, &mut retain, &mut rr);
if rv.is_some() != expected || (rr.err().is_none()) != expected || (!expected && (!retain.bytes.is_empty() || !retain.ids.is_empty())) { println!("FAIL: %s retaining verdict accepted={} want={}", rv.is_some(), expected); failed = true; }
}`, rustBytes(wire), c.read, c.name, c.name)
	}
	body.WriteString("if failed { std::process::exit(1); }\n")
	files, err := Generate(vcLoadUnit(t, root, "tables/pointers"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out, err := runRustGeneratedFilesProbe(t, files, body.String())
	if err != nil {
		t.Fatalf("element-kind probe failed: %v\n%s", err, out)
	}
}
