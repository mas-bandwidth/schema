package ctable

import (
	"os"
	"strings"
	"testing"
)

// The strict-only gate for the C leg (docs/plans/strict-only.md layer 4):
// the generator's output for a schema exercising every C read surface
// (variable wire, graph/region and builder reads, JSON, message batch and
// the fixed form) and the registered C names in internal/tablenames/c.go
// carry none of the retired tolerant surface: no Tolerant entry point, no
// clamped counter or clamp, no kind_mismatch counter, no duplicate
// counter, and no malformed binary verdict. A read returns the whole
// value or refuses the whole load; the refusal reason names that ride in
// upper case (a per-language diagnostic) are not counters and stay.
const strictOnlySchema = `package strictonly
table Leaf { entries map[int32]int32
    values []int32
    name string(16)
    score int32 | min = 0, max = 100
    ratio float32 }
union Choice { leaf Leaf }
table Holder { inner Leaf
    maybe ?Leaf
    leaves []Leaf
    choice Choice
    head *Leaf }
fixed table Root { a uint16
    b int32 | min = 0, max = 10
    c float32 }
`

func TestStrictOnlyCNoTolerantEntry(t *testing.T) {
	t.Parallel()
	retired := []string{
		"Tolerant", "tolerant", "TOLERANT",
		"clamped", "CLAMPED",
		"kind_mismatch",
		"duplicate",
		"malformed",
	}
	files := generate(t, strictOnlySchema)
	if len(files) == 0 {
		t.Fatal("generator produced no files for the strict-only schema")
	}
	for name, data := range files {
		for _, bad := range retired {
			if strings.Contains(string(data), bad) {
				t.Errorf("generated %s retains retired tolerant name %q", name, bad)
			}
		}
	}
	names, err := os.ReadFile("../../tablenames/c.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range retired {
		if strings.Contains(string(names), bad) {
			t.Errorf("internal/tablenames/c.go retains retired tolerant name %q", bad)
		}
	}
}
