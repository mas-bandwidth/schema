package ctable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cfmt"
)

const cHouseLayoutSchema = `package probe

enum Grade
{
    Bronze
    Silver
}

table Goal
{
    x int32
    name string(32)
    grade Grade
}

table GoalList
{
    goals []Goal
}

fixed table Point
{
    x float32
    y float32
}
`

// Every file the table emitter writes leaves through the house layout pass
// (internal/codegen/cfmt). The pass leaves alone what it cannot read, which
// would hand a user unlaid text with nothing red: this holds that it READS
// every file the emitter writes for a unit with the common shapes, and that
// what Generate returns is laid out already. The tables-c-clean gate holds the
// layout itself to clang-format over the whole corpus.
func TestGeneratedCLeavesThroughTheHouseLayout(t *testing.T) {
	u := cUnitNamed(t, "Probe", cHouseLayoutSchema)
	raw, err := generateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || len(raw) != len(out) {
		t.Fatalf("%d files before the layout pass, %d after", len(raw), len(out))
	}
	for name, data := range raw {
		before := formatCompactCFunctions(string(data))
		if why := cfmt.Why(before); why != "" {
			t.Errorf("%s: the layout pass refused the emitter's text: %s", name, why)
			continue
		}
		after := string(out[name])
		if after != cfmt.Format(before) {
			t.Errorf("%s: Generate did not return the layout pass's text", name)
		}
		if cfmt.Format(after) != after {
			t.Errorf("%s: the layout pass is not idempotent on its own output", name)
		}
	}
}

// cCarries reports whether emitted C carries needle, read as TOKENS: the
// layout is the house layout pass's and moves with .clang-format, so a
// needle is matched with all whitespace taken out of both sides. Every
// presence AND absence check on emitted text in this package goes through
// it: an absence check spelled in a layout the emitter no longer writes
// would pass on anything.
func cCarries(text, needle string) bool {
	return strings.Contains(cSquash(text), cSquash(needle))
}

func cSquash(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
}
