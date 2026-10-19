package rusttable

import (
	"strings"
	"testing"
)

func TestVariableLoadMeasureChecksCursorBeforeLengthSubtraction(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

table Child
{
    value uint32
}

table Root
{
    children [..4]Child
    items []Child
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var generated strings.Builder
	for _, contents := range files {
		generated.Write(contents)
	}
	code := generated.String()
	// Every variable table gets its own measure, so a bare count would let one
	// site's guard stand in for the other's: each site is held by its own line.
	// The unbounded list `items` is what emits the list payload site at all.
	const guard = "if off > end || l > (end - off) as u64 { return -1; }"
	for site, line := range map[string]string{
		"node-table":   "\n            " + guard + "\n            let sub = off + l as usize;\n",
		"list payload": "\n                " + guard + "\n                let sub = off + l as usize;\n",
	} {
		if !strings.Contains(code, line) {
			t.Errorf("variable load measure: the %s length check does not bound the cursor before end - off", site)
		}
	}
	if !strings.Contains(code, "_load_measure(wire) < 0 { report.malformed = true; return None; }") {
		t.Fatal("a refused load measure did not produce a typed malformed read")
	}
}
