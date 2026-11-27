package jstable

import "testing"

// A flags value's text is an array of names, a container the read counts, so
// the write's depth cap counts it too, an empty [] included
// (docs/SPEC-TABLES.md §16.14 W2; the oracle's FlagsNode row in
// internal/tabletext/depth_write_test.go): 127 nodes under the root put the
// last node's flags at depth 128, which writes and reads back; 128 nodes put
// it at depth 129, which the read refuses, so toJson refuses it too (null).
func TestJsJsonWriteDepthCountsFlags(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

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
`)
	jsDiscardRun(t, u, nil, `
function chain(nodes) {
  const root = new m.FlagsNode();
  let cur = root;
  for (let i = 0; i < nodes; i++) { cur.next = new m.FlagsNode(); cur = cur.next; }
  return root;
}
const text = m.FlagsNodeToJson(chain(127));
if (text === null) {
  fail("127 nodes, the last flags array at depth 128, were refused");
} else {
  if (!text.includes('"marks": []')) { fail("the empty flags array was not written"); }
  const r = new m.TableVariableReport();
  const back = m.FlagsNodeFromJson(text, r);
  if (back === null) { fail("the 127-node text did not read back"); }
  else if (m.FlagsNodeToJson(back) !== text) { fail("the 127-node text did not round-trip"); }
}
if (m.FlagsNodeToJson(chain(128)) !== null) {
  fail("128 nodes put the last flags array at depth 129, and toJson did not refuse");
}
`)
}
