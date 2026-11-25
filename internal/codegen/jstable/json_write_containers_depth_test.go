package jstable

import "testing"

// THE WRITE COUNTS EVERY CONTAINER THE READ DOES (docs/SPEC-TABLES.md §16.3,
// §16.14 W2; the oracle's rows in internal/tabletext/depth_write_test.go): a
// chain of nodes each holding one EMPTY container of one kind (a union's {},
// an enum-keyed array's object, a map's {}, a counted array's [] and a
// list's []) writes and reads back when the last node's container sits at
// depth 128 (127 nodes under the root), and toJson refuses (null) at depth 129
// (128 nodes). JS refuses in every build. Flags are
// TestJsJsonWriteDepthCountsFlags.
func TestJsJsonWriteDepthCountsEveryContainer(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

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

table UnionNode
{
    next *UnionNode
    pick Pick
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table ListNode
{
    next  *ListNode
    items []int32
}
`)
	jsDiscardRun(t, u, nil, `
for (const name of ["UnionNode", "KeyedNode", "MapNode", "ArrNode", "ListNode"]) {
  const chain = (nodes) => {
    const root = new m[name]();
    let cur = root;
    for (let i = 0; i < nodes; i++) { cur.next = new m[name](); cur = cur.next; }
    return root;
  };
  const text = m[name + "ToJson"](chain(127));
  if (text === null) {
    fail(name + ": 127 nodes, the last container at depth 128, were refused");
  } else {
    const r = new m.TableVariableReport();
    const back = m[name + "FromJson"](text, r);
    if (back === null) { fail(name + ": the 127-node text did not read back"); }
    else if (m[name + "ToJson"](back) !== text) { fail(name + ": the 127-node text did not round-trip"); }
  }
  if (m[name + "ToJson"](chain(128)) !== null) {
    fail(name + ": 128 nodes put the last container at depth 129, and toJson did not refuse");
  }
}
`)
}
