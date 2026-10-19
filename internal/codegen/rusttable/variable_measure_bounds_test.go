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
	const guard = "if off > end || l > (end - off) as u64 { return -1; }"
	if n := strings.Count(code, guard); n < 2 {
		t.Fatalf("variable load measure emitted %d cursor-safe length guards, want both node-table and list payload guards", n)
	}
	if !strings.Contains(code, "_load_measure(wire) < 0 { report.malformed = true; return None; }") {
		t.Fatal("a refused load measure did not produce a typed malformed read")
	}
}
