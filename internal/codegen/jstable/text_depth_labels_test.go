package jstable

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJsTextReadDepthAndLabelsAgreeWithEngine holds the JavaScript text read to
// the engine's verdict (internal/tabletext) on two rules of docs/SPEC-TABLES.md:
//
//   - §16.2's one nesting cap, counted as the engine counts it: a union's arm
//     value sits two levels below the union's holder (the union object is one
//     level), and a map's value two levels below the map's holder (the map
//     object is one level). Each sweep nests a skipped value one step at a time
//     across the cap, and must hold both a read and a refusal, so the boundary
//     itself is what is compared.
//   - §16.7's label rules: a label is followed by its object's `,` or `}`, and
//     in a value the walk skips a bare label refers (and must name a closed
//     definition) while a label with fields defines (and must be new).
//
// Every verdict is the engine's; the test fails if JavaScript differs on any
// row, or if a row's engine verdict is not the one the row was written for.
func TestJsTextReadDepthAndLabelsAgreeWithEngine(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Node
{
    value int32 = 0
}

table Leaf
{
    value int32 = 0
}

union Arm
{
    leaf Leaf
    tag  uint32
}

table Entry
{
    count int32 = 0
}

table Root
{
    head   *Node
    arm    Arm
    roster map[uint8]Entry
}
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	engine := func(text string) bool {
		var rep tabletext.Report
		return m.Read(m.New(m.Lookup("Root")), []byte(text), &rep)
	}

	type row struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Read bool   `json:"read"`
	}
	var rows []row

	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	sweeps := []struct {
		name string
		wrap func(inner string) string
	}{
		{"union-arm", func(inner string) string { return `{"arm": {"leaf": {"zz": ` + inner + `}}}` }},
		{"map-value", func(inner string) string { return `{"roster": {"1": {"count": 1, "zz": ` + inner + `}}}` }},
	}
	for _, s := range sweeps {
		reads, refusals := 0, 0
		for n := 120; n <= 132; n++ {
			text := s.wrap(nest(n))
			ok := engine(text)
			if ok {
				reads++
			} else {
				refusals++
			}
			rows = append(rows, row{Name: s.name, Text: text, Read: ok})
		}
		if reads == 0 || refusals == 0 {
			t.Fatalf("%s: the sweep does not cross the engine's cap (reads %d, refusals %d)", s.name, reads, refusals)
		}
	}

	labels := []struct {
		name string
		text string
		read bool
	}{
		{"label-then-comma", `{"head": {"&node": 1, "value": 2}}`, true},
		{"label-without-comma", `{"head": {"&node": 1 "value": 2}}`, false},
		{"skipped-refers-closed", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1}}`, true},
		{"skipped-defines-twice", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1, "value": 2}}`, false},
		{"skipped-refers-undefined", `{"zz": {"&node": 5}}`, false},
		{"skipped-defines-then-refers", `{"zz": {"&node": 5, "value": 1}, "head": {"&node": 5}}`, true},
		{"skipped-defines-then-placed-defines", `{"zz": {"&node": 5, "value": 1}, "head": {"&node": 5, "value": 2}}`, false},
		{"skipped-refers-inside-own-definition", `{"zz": {"&node": 5, "a": {"&node": 5}}}`, false},
	}
	for _, l := range labels {
		if got := engine(l.text); got != l.read {
			t.Fatalf("%s: the engine reads=%v, the row was written for %v", l.name, got, l.read)
		}
		rows = append(rows, row{Name: l.name, Text: l.text, Read: l.read})
	}

	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	jsDiscardRun(t, u, map[string][]byte{"rows.json": data}, `
for (const row of JSON.parse(readFileSync("rows.json", "utf8"))) {
  const r = new m.TableVariableReport();
  const read = m.RootFromJson(row.text, r) !== null;
  if (read !== row.read) {
    fail(row.name + " (" + row.text.length + " bytes): read=" + read + ", the engine read=" + row.read);
  }
}
`)
}
