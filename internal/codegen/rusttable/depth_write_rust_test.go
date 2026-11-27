package rusttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// depthWriteRustUnit is internal/tabletext/depth_write_test.go's
// depthWriteUnit: a chain of nodes, each holding one EMPTY container of a kind
// the writer always emits.
const depthWriteRustUnit = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

// TestRustWriteDepthCapCountsEveryContainer: THE WRITE COUNTS EVERY CONTAINER
// THE READ DOES (docs/SPEC-TABLES.md §16.2, §16.14 W2), as the oracle's
// TestWriteDepthCapCountsEveryContainer. 127 nodes under the root put the last
// node's container at depth 128: the text writes and reads back. 128 nodes put
// it at 129: the write refuses (to_json_checked is None), in every build.
func TestRustWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (JSON write depth-cap probe)")
	keyedRequireRustc(t)

	files, err := Generate(unitFrom(t, depthWriteRustUnit))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", files)
	main := `#![allow(dead_code, unused_imports, unused_variables, unused_mut)]
mod probe;
use probe::*;
use std::rc::Rc;

macro_rules! probe {
    ($ty:ident, $to_json:ident, $from_json:ident, $name:expr) => {{
        let chain = |nodes: usize| -> $ty {
            let mut cur: Option<Rc<$ty>> = None;
            for _ in 0..nodes {
                let mut n = $ty::default();
                n.next = cur;
                cur = Some(Rc::new(n));
            }
            let mut root = $ty::default();
            root.next = cur;
            root
        };
        let text = $to_json(&chain(127)).unwrap_or_else(|| panic!("{}: 127 nodes, the last container at depth 128, were refused", $name));
        let mut report = TableReport::default();
        assert!($from_json(&text, &mut report).is_some(), "{}: the 127-node text did not read back", $name);
        assert!($to_json(&chain(128)).is_none(), "{}: 128 nodes put the last container at depth 129 and the write did not refuse", $name);
    }};
}

fn main() {
    probe!(ArrNode, arr_node_to_json_checked, arr_node_from_json, "ArrNode");
    probe!(UnionNode, union_node_to_json_checked, union_node_from_json, "UnionNode");
    probe!(MapNode, map_node_to_json_checked, map_node_from_json, "MapNode");
    probe!(KeyedNode, keyed_node_to_json_checked, keyed_node_from_json, "KeyedNode");
    probe!(FlagsNode, flags_node_to_json_checked, flags_node_from_json, "FlagsNode");
    println!("JSON_WRITE_DEPTH_OK");
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	// both builds: the refusal is not a debug assert
	for _, opt := range [][]string{nil, {"-O", "-C", "debug-assertions=off"}} {
		bin := filepath.Join(dir, "probe-bin")
		args := append([]string{"--edition=2024", "--cfg", `feature="cook"`, "-A", "warnings", "-o", bin}, opt...)
		cmd := testproc.Command(t, "rustc", append(args, "main.rs")...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Rust compile %v: %v\n%s", opt, err, out)
		}
		out, err := testproc.Command(t, bin).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "JSON_WRITE_DEPTH_OK") {
			t.Fatalf("Rust probe %v: %v\n%s", opt, err, out)
		}
	}
}
