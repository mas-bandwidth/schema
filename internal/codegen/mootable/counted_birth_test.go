package mootable_test

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A COUNTED ARRAY [Min..Max]T IS ELIDED AT ITS BIRTH VALUE (docs/SPEC-TABLES.md
// §3.3 and the §22.2 [M..N]T row; the Go engine's encodeArray and
// messageElides): a plain counted field holding exactly Min elements, each at
// its default (a float by its bits, a table by a body that is its terminator
// alone), stays off the wire on the variable save and on the message form, and
// rides at any other count or with one live element. Each case is a value the
// Go engine writes (the file form and the message batch); moo reads the file
// strictly (an elided field reads as the born value) and must write both
// forms back to the engine's bytes.
var countedBirthCases = []constructCase{
	{"counted_int", "§3.3 counted birth", "package probe\ntable Root\n{\n    few [2..4]int32\n}\n", "Root", `{}`},
	{"counted_int_min_default", "§3.3 counted birth", "package probe\ntable Root\n{\n    few [2..4]int32\n}\n", "Root", `{"few":[0,0]}`},
	{"counted_int_min_one", "§3.3 counted birth", "package probe\ntable Root\n{\n    few [2..4]int32\n}\n", "Root", `{"few":[0,7]}`},
	{"counted_int_plus1_default", "§3.3 counted birth", "package probe\ntable Root\n{\n    few [2..4]int32\n}\n", "Root", `{"few":[0,0,0]}`},
	{"counted_table", "§3.3 counted birth", "package probe\ntable Rec\n{\n    n int32 = 55\n    on bool\n}\ntable Root\n{\n    few [2..4]Rec\n}\n", "Root", `{}`},
	{"counted_table_min_default", "§3.3 counted birth", "package probe\ntable Rec\n{\n    n int32 = 55\n    on bool\n}\ntable Root\n{\n    few [2..4]Rec\n}\n", "Root", `{"few":[{},{}]}`},
	{"counted_table_min_one", "§3.3 counted birth", "package probe\ntable Rec\n{\n    n int32 = 55\n    on bool\n}\ntable Root\n{\n    few [2..4]Rec\n}\n", "Root", `{"few":[{},{"n":56}]}`},
	{"counted_float_negzero", "§3.3 counted birth", "package probe\ntable Root\n{\n    few [2..4]float64\n}\n", "Root", `{"few":[0,-0.0]}`},
}

func TestMooCountedArrayBornAtItsMinimum(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	for _, c := range countedBirthCases {
		t.Run(c.name+"/variable", func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, c.schema)
			wire := goOracleWire(t, u, c.root, c.json)
			files, err := generate(u)
			if err != nil {
				t.Fatal(err)
			}
			g := writeUnit(t, u, files)
			out := g.run(t, strictRoundTrip(g, c.root), map[string][]byte{"wire": wire})
			if want := fmt.Sprintf("ok %d %x", len(wire), wire); out != want {
				t.Fatalf("moo does not write the engine's variable wire:\nmoo: %s\ngo:  %s", out, want)
			}
		})
		t.Run(c.name+"/message", func(t *testing.T) {
			t.Parallel()
			if _, problem := messageDiffers(t, c, nil); problem != "" {
				t.Fatal(problem)
			}
		})
	}
}
