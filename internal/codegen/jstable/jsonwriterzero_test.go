package jstable

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJsJsonWriterRefusesZeroByte holds the JavaScript text writer to row 8 of
// docs/SPEC.md §5.1 as docs/SPEC-TABLES.md §16.3, §16.14 W0 and W8 state it: a
// string, a text-list element and a map key holding a zero byte are REFUSED,
// by this leg's own value (null), before any text is written, exactly as its
// binary writer refuses them. The same value without the zero writes as today.
func TestJsJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	u := loadSchemaString(t, "zerobyte.schema", `package zerobyte
table Root
{
    name string(8)
    list [..4]string(8)
    m map[string(8)]int32
}
`)
	m := tabletext.NewModel(u)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("the unit declares no Root table")
	}
	const clean = `{"name":"ab","list":["ab"],"m":{"ab":1}}`
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(clean), &rep) || !rep.Silent() {
		t.Fatalf("the clean value did not read: %+v", rep)
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatalf("the clean value did not write: %v", err)
	}
	want := strconv.Quote(string(text) + "\n")
	jsDiscardRun(t, u, nil, fmt.Sprintf(`
const want = %s;
const expect = (ok, msg) => { if (!ok) fail(msg); };
function base() {
  const v = new m.Root();
  v.name = "ab";
  v.list = [{ value: "ab" }];
  v.m = new Map([["ab", 1]]);
  return v;
}
try {
  const v = base();
  expect(m.RootToJson(v) === want, "the clean value does not write as today: " + JSON.stringify(m.RootToJson(v)));
} catch (e) {
  fail("the clean write raised: " + e);
}
const zeros = [
  ["string field", (v) => { v.name = "a\u0000b"; }],
  ["text-list element", (v) => { v.list = [{ value: "a\u0000b" }]; }],
  ["map key", (v) => { v.m = new Map([["a\u0000b", 1]]); }],
];
for (const [what, set] of zeros) {
  const v = base();
  set(v);
  let out;
  try {
    out = m.RootToJson(v);
  } catch (e) {
    fail(what + ": the writer raised instead of refusing: " + e);
    continue;
  }
  expect(out === null, what + ": the writer accepted a zero byte: " + JSON.stringify(out));
}
`, want))
}
