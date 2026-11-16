package lobstertable_test

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const positionSchema = `package probe
enum Key { A, B }
union Choice { arm int64 }
table Root {
 plain int64
 values [..2]int64
 opt ?int64
 choice Choice
 lookup map[string]int64
 keyed [Key]int64
 narrow int32
 i128 int128 | min = -100, max = 100
 u128 uint128
 bits40 bits(40)
 fixed64 fixed(48,16) | min = -10, max = 10
 fixed128 fixed(96,32) | min = -10, max = 10
 fixed32 fixed(16,16) | min = -10, max = 10
}
`

var positionCases = []struct{ name, number, quoted string }{{"plain", `{"plain":7}`, `{"plain":"7"}`},
	{"array", `{"values":[7]}`, `{"values":["7"]}`},
	{"optional", `{"opt":7}`, `{"opt":"7"}`},
	{"union", `{"choice":{"arm":7}}`, `{"choice":{"arm":"7"}}`},
	{"map", `{"lookup":{"x":7}}`, `{"lookup":{"x":"7"}}`},
	{"keyed", `{"keyed":{"A":7}}`, `{"keyed":{"A":"7"}}`},
	{"i128", `{"i128":7}`, `{"i128":"7"}`},
	{"u128", `{"u128":7}`, `{"u128":"7"}`},
	{"bits40", `{"bits40":7}`, `{"bits40":"7"}`},
	{"fixed64", `{"fixed64":1.5}`, `{"fixed64":"1.5"}`},
	{"fixed128", `{"fixed128":1.5}`, `{"fixed128":"1.5"}`}}

func TestLobsterJsonWideNumberPositions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lobster")
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(positionSchema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{p})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", mod)
	}
	b.WriteString(`import .json
def check(name:string, path:string, want:bool):
    let text = read_file(path)
    assert text
    let r = schema_rt.LoadReport {}
    let value = schema_rt.json_read("Root", text, r)
    if (want and not value) or (not want and (value or r.reason != "kind_mismatch")):
        print("BAD " + name + " " + r.reason)
`)
	inputs := map[string][]byte{}
	for _, c := range positionCases {
		inputs[c.name+"number"] = []byte(c.number)
		inputs[c.name+"quoted"] = []byte(c.quoted)
		fmt.Fprintf(&b, "check(%q, %q, false)\ncheck(%q, %q, true)\n", c.name+"number", c.name+"number", c.name+"quoted", c.name+"quoted")
	}
	inputs["narrow"] = []byte(`{"narrow":7}`)
	inputs["fixed32"] = []byte(`{"fixed32":1.5}`)
	b.WriteString("check(\"narrow\", \"narrow\", true)\ncheck(\"fixed32\", \"fixed32\", true)\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	if out := string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)); strings.Contains(out, "BAD ") {
		t.Fatal(out)
	}
}
