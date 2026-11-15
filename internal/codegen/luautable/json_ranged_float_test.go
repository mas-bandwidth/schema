package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COMPRESSED FLOAT HOLDS ITS DECLARED RANGE IN THE TEXT READ
// (docs/SPEC-TABLES.md §3.3, §16.2 Numbers): a finite value past an end is
// refused by the plain read, as the binary strict read and the Go engine
// refuse it, and a compressed float's -0 lands as +0. A plain float keeps its
// -0. Every answer is compared with schema-go's generated <T>FromJSON over
// the same unit and the same text, never with a hand-typed expectation. A
// compressed float is float32 (docs/SPEC.md §4.3), so the float64 field is
// plain only.
const jsonRangedFloatSchema = `package probe
table Ranged
{
    a float32 | min = -10, max = 10, resolution = 0.001
    b float32 | min = 0, max = 1000, resolution = 0.5
    p float32
    q float64
}
`

// jsonRangedFloatCase is one text read of one field of Ranged.
type jsonRangedFloatCase struct {
	name, field, text string
}

var jsonRangedFloatCases = func() []jsonRangedFloatCase {
	var cases []jsonRangedFloatCase
	for _, f := range []struct{ name, lo, hi, pastLo, pastHi, nearHi string }{
		{"a", "-10", "10", "-10.00001", "10.00001", "10.0000001"},
		{"b", "0", "1000", "-0.00001", "1000.0001", "1000.00001"},
	} {
		for _, c := range []struct{ name, token string }{
			{"min", f.lo},
			{"max", f.hi},
			{"past-min", f.pastLo},
			{"past-max", f.pastHi},
			{"narrows-onto-max", f.nearHi},
			{"far", "1e30"},
			{"far-negative", "-1e30"},
			{"inside", "2.5"},
			{"negative-zero", "-0"},
			{"negative-zero-fraction", "-0.0"},
		} {
			cases = append(cases, jsonRangedFloatCase{f.name + "-" + c.name, f.name, fmt.Sprintf(`{"%s":%s}`, f.name, c.token)})
		}
	}
	for _, f := range []string{"p", "q"} {
		cases = append(cases,
			jsonRangedFloatCase{f + "-negative-zero", f, fmt.Sprintf(`{"%s":-0}`, f)},
			jsonRangedFloatCase{f + "-far", f, fmt.Sprintf(`{"%s":1e30}`, f)},
		)
	}
	return cases
}()

// jsonRangedFloatGoOracle runs the generated Go read over every case and
// returns one answer per case: "refused", or "ok:<the field's value>".
func jsonRangedFloatGoOracle(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	b.WriteString(`package probe

import (
	"fmt"
	"testing"
)

func TestJSONRangedFloatOracle(t *testing.T) {
`)
	for _, c := range jsonRangedFloatCases {
		fmt.Fprintf(&b, "\t{ var v RangedBuilder; if !v.Init() { t.Fatal(\"builder\") }; var r TableReport; if RangedFromJSON(&v, []byte(%q), &r) && r.Err() == nil { if !v.Lock() { t.Fatal(\"lock\") }; fmt.Printf(\"@%s|ok:%%.17g\\n\", float64(v.AsConst().%s)) } else { fmt.Printf(\"@%s|refused\\n\") }; v.Shutdown() }\n",
			c.text, c.name, strings.ToUpper(c.field), c.name)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	files["go.mod"] = fmt.Appendf(nil, "module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", tfSerializeGoRuntime(t))
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestJSONRangedFloatOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return jsonStrictAnswers(string(out))
}

// jsonRangedFloatLuau runs the Luau plain read over every case under the
// pinned interpreter and answers as the oracle does.
func jsonRangedFloatLuau(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	g := requiredFiles(t, u)
	if len(g.files["schema_json.luau"]) == 0 {
		t.Fatal("the unit emitted no schema_json.luau")
	}
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local inputs = require("./inputs")
local cases = {
`)
	for _, c := range jsonRangedFloatCases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&b, "\t{ %q, %q },\n", c.name, strings.ToUpper(c.field))
	}
	b.WriteString(`}
for _, c in cases do
	local name, field = c[1], c[2]
	local v = json.FromJson("Ranged", inputs[name], {})
	if v == nil then
		print("@" .. name .. "|refused")
	else
		print("@" .. name .. "|ok:" .. string.format("%.17g", v[field]))
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return jsonStrictAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

// TestLuauJsonRangedFloatHeld holds the Luau plain read's answer, verdict and
// landed value, to schema-go's on every case: each end of two compressed
// ranges is read, as is a value that narrows onto an end, a value just past
// either end and one far past it are refused, a compressed -0 lands as +0,
// and a plain float32 or float64 -0 keeps its sign.
func TestLuauJsonRangedFloatHeld(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the Go toolchain")
	path := filepath.Join(t.TempDir(), "Ranged.schema")
	if err := os.WriteFile(path, []byte(jsonRangedFloatSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the ranged-float schema does not load: %v", err)
	}
	want := jsonRangedFloatGoOracle(t, u)
	got := jsonRangedFloatLuau(t, u)
	// the cases must split refusals from reads, and the oracle must show both
	// zero rules, or the comparison below holds nothing
	refused, signed, unsigned := 0, 0, 0
	for _, c := range jsonRangedFloatCases {
		switch want[c.name] {
		case "":
			t.Fatalf("%s: the Go oracle printed no answer", c.name)
		case "refused":
			refused++
		case "ok:-0":
			signed++
		case "ok:0":
			unsigned++
		}
	}
	if refused == 0 || refused == len(jsonRangedFloatCases) || signed == 0 || unsigned == 0 {
		t.Fatalf("the cases do not exercise the range and both zero rules (%d refused, %d -0, %d +0 of %d)", refused, signed, unsigned, len(jsonRangedFloatCases))
	}
	var bad []string
	for _, c := range jsonRangedFloatCases {
		if got[c.name] != want[c.name] {
			bad = append(bad, c.name)
			t.Logf("%s %s: Luau %q, Go %q", c.name, c.text, got[c.name], want[c.name])
		}
	}
	if len(bad) > 0 {
		t.Fatalf("the Luau text reads of a ranged float disagree with schema-go on %v", bad)
	}
	t.Logf("%d cases agree with schema-go, %d of them refused", len(jsonRangedFloatCases), refused)
}
