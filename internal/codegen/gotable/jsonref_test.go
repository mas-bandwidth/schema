package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// The reference JSON read (internal/tabletext) and the generated Go walk must
// answer every text with the same verdict (docs/rulings.json
// refusal-verdict-only). Review 2026-11-21 found five texts they disagreed
// on; each case below carries the verdict the spec gives it, and the test
// requires the reference, the generated Go reader and the spec to agree.
//
//   - TABLETEXT-1, §16.2 "The read's cap counts every object and array the
//     reader walks ... 128 container levels are read and 129 refuse": a map
//     object and a union object are levels.
//   - TABLETEXT-2, §16.2 malformed: `{"&node": 1 "x": 2}` has no
//     value-separator.
//   - TABLETEXT-3, §16.7 "The label grammar and the definition rules apply
//     EVERYWHERE, inside an unknown key's skipped value too".
//   - TABLETEXT-4, §16.2 "The cap never reaches a map key or a string value";
//     a `string(N)` key's bound is N (§2.8).
//   - TABLETEXT-8, §16.2 "Trailing commas in objects and arrays are accepted
//     on read": `{"&node": 1,}` is a reference.
const jsonrefUnit = `package jsonref

table Node
{
    kids map[string(8)]*Node
}

table Link
{
    u Choice
}

union Choice
{
    w Wrap
}

table Wrap
{
    next *Link
}

table Root
{
    a *PNode
    b *PNode
    p *PNode
}

table PNode
{
    x int32
}

table Item
{
    v int32
}

table E
{
    names map[string(300)]Item
}
`

type jsonrefCase struct {
	root   string
	name   string
	text   string
	accept bool
}

func jsonrefCases() []jsonrefCase {
	mapChain := func(n int) string {
		return strings.Repeat(`{"kids":{"a":`, n-1) + `{}` + strings.Repeat(`}}`, n-1)
	}
	unionChain := func(n int) string {
		return strings.Repeat(`{"u":{"w":{"next":`, n) + `{}` + strings.Repeat(`}}}`, n)
	}
	unionHead, unionTail := strings.Repeat(`{"u":{"w":{"next":`, 42), strings.Repeat(`}}}`, 42)
	key := func(n int) string { return `{"names": {"` + strings.Repeat("k", n) + `": {"v": 1}}}` }
	return []jsonrefCase{
		{"Node", "TABLETEXT-1 map chain 65 nodes, depth 128", mapChain(65), true},
		{"Node", "TABLETEXT-1 map chain 66 nodes, depth 130", mapChain(66), false},
		{"Link", "TABLETEXT-1 union chain 42 cycles, depth 126", unionChain(42), true},
		{"Link", "TABLETEXT-1 union chain 43 cycles, depth 129", unionChain(43), false},
		{"Link", "TABLETEXT-1 unknown arm value at depth 128", unionHead + `{"u":{"zz":{}}}` + unionTail, true},
		{"Link", "TABLETEXT-1 unknown arm value reaching depth 129", unionHead + `{"u":{"zz":[[]]}}` + unionTail, false},
		{"Root", "TABLETEXT-2 definition with a comma", `{"p": {"&node": 1, "x": 2}}`, true},
		{"Root", "TABLETEXT-2 definition with no comma after the label", `{"p": {"&node": 1 "x": 2}}`, false},
		{"Root", "TABLETEXT-8 reference", `{"a": {"&node": 1, "x": 5}, "b": {"&node": 1}}`, true},
		{"Root", "TABLETEXT-8 reference with a trailing comma", `{"a": {"&node": 1, "x": 5}, "b": {"&node": 1,}}`, true},
		{"Root", "TABLETEXT-8 undefined label alone with a trailing comma", `{"a": {"&node": 1,}}`, false},
		{"Root", "TABLETEXT-3a skipped reference before its definition", `{"zz": {"&node": 5}, "a": {"&node": 5, "x": 1}}`, false},
		{"Root", "TABLETEXT-3b skipped label after a field", `{"zz": {"x": 1, "&node": 2}}`, false},
		{"Root", "TABLETEXT-3c skipped label not an integer", `{"zz": {"&node": 1.5}}`, false},
		{"Root", "TABLETEXT-3c skipped reserved key of another spelling", `{"zz": {"&foo": 1}}`, false},
		{"Root", "TABLETEXT-3d skipped reference never defined", `{"zz": {"&node": 5}}`, false},
		{"Root", "TABLETEXT-3d label defined placed and again skipped", `{"a": {"&node":1,"x":1}, "zz": {"&node":1,"x":2}}`, false},
		{"Root", "TABLETEXT-3 skipped definition with no comma after the label", `{"zz": {"&node": 1 "x": 2}}`, false},
		{"Root", "TABLETEXT-3 skipped reference inside its own definition", `{"zz": {"&node": 3, "x": {"&node": 3}}}`, false},
		{"Root", "TABLETEXT-3 skipped definition, placed reference", `{"zz": {"&node": 5, "x": 1}, "a": {"&node": 5}}`, true},
		{"Root", "TABLETEXT-3 placed definition, skipped reference", `{"a": {"&node": 5, "x": 1}, "zz": {"&node": 5}}`, true},
		{"Root", "TABLETEXT-3 placed definition, skipped reference with a trailing comma", `{"a": {"&node": 5, "x": 1}, "zz": {"&node": 5,}}`, true},
		{"Root", "TABLETEXT-3 skipped definition, skipped reference", `{"zz": {"&node": 5, "x": 1}, "yy": [{"&node": 5}]}`, true},
		{"E", "TABLETEXT-4 key of 255 bytes", key(255), true},
		{"E", "TABLETEXT-4 key of 256 bytes", key(256), true},
		{"E", "TABLETEXT-4 key of 300 bytes", key(300), true},
		{"E", "TABLETEXT-4 key of 301 bytes", key(301), false},
	}
}

// TestJSONReferenceAndGoWalkAgree feeds every case to the reference reader
// here and to the generated Go reader in a compiled probe, and requires both
// verdicts to equal the spec's.
func TestJSONReferenceAndGoWalkAgree(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsonrefUnit)
	m := tabletext.NewModel(u)
	cases := jsonrefCases()
	var rows strings.Builder
	for _, c := range cases {
		var r tabletext.Report
		ok := m.Read(m.New(m.Lookup(c.root)), []byte(c.text), &r)
		if ok != c.accept {
			t.Errorf("reference: %s: read=%v (%+v), want %v", c.name, ok, r, c.accept)
		}
		fmt.Fprintf(&rows, "\t{%q, %q, %q, %v},\n", c.root, c.name, c.text, c.accept)
	}
	probe := `package jsonref

import "testing"

func TestJSONRef(t *testing.T) {
	read := func(root string, text []byte) (bool, TableReport) {
		var r TableReport
		switch root {
		case "Node":
			var b NodeBuilder
			if !b.Init() {
				t.Fatal("init")
			}
			defer b.Shutdown()
			return NodeFromJSON(&b, text, &r), r
		case "Link":
			var b LinkBuilder
			if !b.Init() {
				t.Fatal("init")
			}
			defer b.Shutdown()
			return LinkFromJSON(&b, text, &r), r
		case "Root":
			var b RootBuilder
			if !b.Init() {
				t.Fatal("init")
			}
			defer b.Shutdown()
			return RootFromJSON(&b, text, &r), r
		case "E":
			var b EBuilder
			if !b.Init() {
				t.Fatal("init")
			}
			defer b.Shutdown()
			return EFromJSON(&b, text, &r), r
		}
		t.Fatalf("no root %s", root)
		return false, r
	}
	for _, c := range []struct {
		root, name, text string
		accept           bool
	}{
` + rows.String() + `	} {
		ok, r := read(c.root, []byte(c.text))
		if ok != c.accept {
			t.Errorf("generated Go: %s: read=%v (%v), want %v (the reference and the spec)", c.name, ok, r.Refusal, c.accept)
		}
	}
}
`
	runGenerated(t, jsonrefUnit, probe)
}
