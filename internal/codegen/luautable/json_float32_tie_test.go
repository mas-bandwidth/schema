package luautable_test

import (
	"fmt"
	"maps"
	"math/big"
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

// A float32 field reads a JSON number by ONE correctly rounded conversion
// from the token's exact decimal (docs/SPEC-TABLES.md §16.2 Numbers). The
// midpoint between adjacent subnormal float32 values is an odd multiple of
// 2^-150, and that exact decimal has more than 100 significant digits. These
// cases are that midpoint, and the same decimal moved by one unit at the
// 101st significant digit and at every digit past it, including one past the
// last. The bits they must land are the generated Go engine's for the same
// text; nothing here types a verdict.
const jsonFloat32TieSchema = `package probe

fixed table Tie
{
    a float32
}
`

// f32SubnormalMidScale is the binary exponent of a subnormal float32
// midpoint: adjacent subnormals are k·2^-149 and (k+1)·2^-149, so the
// midpoint is (2k+1)·2^-150, which is (2k+1)·5^150 / 10^150.
const f32SubnormalMidScale = 150

// jsonFloat32TieCase is one JSON text of the Tie table.
type jsonFloat32TieCase struct {
	name, text string
}

// jsonFloat32TieCases builds the tokens. k = 0 is the midpoint between zero
// and the smallest subnormal; k = 2 sits in the subnormal body; k = 2^23-1
// is the midpoint between the largest subnormal and the smallest normal.
func jsonFloat32TieCases() []jsonFloat32TieCase {
	mids := []struct {
		name string
		k    uint64
	}{
		{"smallest", 0},
		{"third", 2},
		{"top", 1<<23 - 1},
	}
	var cases []jsonFloat32TieCase
	for _, mid := range mids {
		w := f32SubnormalMidInt(mid.k)
		nDigits := len(w.String())
		add := func(suffix string, n *big.Int, scale int, neg bool) {
			tok := f32TieDecimal(n, scale)
			if neg {
				tok = "-" + tok
				suffix = "neg-" + suffix
			}
			cases = append(cases, jsonFloat32TieCase{
				name: mid.name + "-" + suffix,
				text: `{"a":` + tok + `}`,
			})
		}
		for _, neg := range []bool{false, true} {
			add("exact", w, f32SubnormalMidScale, neg)
			for pos := 101; pos <= nDigits+1; pos++ {
				up, upScale := f32TieBump(w, f32SubnormalMidScale, pos, 1)
				dn, dnScale := f32TieBump(w, f32SubnormalMidScale, pos, -1)
				tag := fmt.Sprintf("d%d", pos)
				add(tag+"-up", up, upScale, neg)
				add(tag+"-dn", dn, dnScale, neg)
			}
		}
	}
	return cases
}

// f32SubnormalMidInt is (2k+1)·5^150, the integer whose division by 10^150
// is the midpoint.
func f32SubnormalMidInt(k uint64) *big.Int {
	odd := new(big.Int).SetUint64(2*k + 1)
	pow5 := new(big.Int).Exp(big.NewInt(5), big.NewInt(f32SubnormalMidScale), nil)
	return odd.Mul(odd, pow5)
}

// f32TieDecimal spells n·10^-scale as a JSON number, exact, with no exponent.
func f32TieDecimal(n *big.Int, scale int) string {
	abs := new(big.Int).Abs(n)
	s := abs.String()
	var b strings.Builder
	switch {
	case scale <= 0:
		b.WriteString(s)
		b.WriteString(strings.Repeat("0", -scale))
	case len(s) <= scale:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", scale-len(s)))
		b.WriteString(s)
	default:
		b.WriteString(s[:len(s)-scale])
		b.WriteByte('.')
		b.WriteString(s[len(s)-scale:])
	}
	return b.String()
}

// f32TieBump moves the decimal by one unit at significant digit sigPos
// (1-based). A position past the last significant digit extends the scale.
func f32TieBump(w *big.Int, scale, sigPos, dir int) (*big.Int, int) {
	body := new(big.Int).Abs(w).String()
	nDigits := len(body)
	out := new(big.Int).Set(w)
	if sigPos <= nDigits {
		unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(nDigits-sigPos)), nil)
		if dir < 0 {
			out.Sub(out, unit)
		} else {
			out.Add(out, unit)
		}
		return out, scale
	}
	extra := sigPos - nDigits
	mul := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(extra)), nil)
	out.Mul(out, mul)
	if dir < 0 {
		out.Sub(out, big.NewInt(1))
	} else {
		out.Add(out, big.NewInt(1))
	}
	return out, scale + extra
}

// jsonFloat32TieGoOracle runs the generated Go read and returns each case's
// float32 bits, or "refused".
func jsonFloat32TieGoOracle(t *testing.T, u *ir.Unit, cases []jsonFloat32TieCase) map[string]string {
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
	"math"
	"testing"
)

func TestJSONFloat32TieOracle(t *testing.T) {
`)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t{ var v Tie; var r TableReport; if TieFromJSON(&v, []byte(%q), &r) && r.Err() == nil { fmt.Printf(\"@%s|%%08x\\n\", math.Float32bits(v.A)) } else { fmt.Printf(\"@%s|refused\\n\") } }\n",
			c.text, c.name, c.name)
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
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestJSONFloat32TieOracle$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return jsonStrictAnswers(string(out))
}

// jsonFloat32TieLuau runs the same texts through the generated Luau read.
func jsonFloat32TieLuau(t *testing.T, u *ir.Unit, cases []jsonFloat32TieCase) map[string]string {
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
local function bits(x: number): string
	local p = string.pack("<f", x)
	local b1, b2, b3, b4 = string.byte(p, 1, 4)
	return string.format("%02x%02x%02x%02x", b4, b3, b2, b1)
end
local names = {
`)
	for _, c := range cases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&b, "\t%q,\n", c.name)
	}
	b.WriteString(`}
for _, name in names do
	local v = json.FromJson("Tie", inputs[name], {})
	if v == nil then
		print("@" .. name .. "|refused")
	else
		print("@" .. name .. "|" .. bits(v.A))
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return jsonStrictAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

// TestLuauJsonFloat32TiesExact holds the Luau float32 JSON read to the
// generated Go engine on subnormal midpoints whose exact decimals run past
// 100 significant digits (docs/SPEC-TABLES.md §16.2 Numbers).
func TestLuauJsonFloat32TiesExact(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	cases := jsonFloat32TieCases()
	path := filepath.Join(t.TempDir(), "Tie.schema")
	if err := os.WriteFile(path, []byte(jsonFloat32TieSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the float32 tie schema does not load: %v", err)
	}
	want := jsonFloat32TieGoOracle(t, u, cases)
	got := jsonFloat32TieLuau(t, u, cases)
	distinct := map[string]struct{}{}
	for _, c := range cases {
		if want[c.name] == "" {
			t.Fatalf("%s: the Go oracle printed no answer", c.name)
		}
		if want[c.name] == "refused" {
			t.Fatalf("%s: the Go oracle refused a finite subnormal-midpoint token", c.name)
		}
		distinct[want[c.name]] = struct{}{}
	}
	if len(distinct) < 2 {
		t.Fatal("the midpoint tokens did not land on two different float32 values")
	}
	var bad []string
	for _, c := range cases {
		if got[c.name] != want[c.name] {
			bad = append(bad, c.name)
			t.Errorf("%s: Luau %q, Go %q", c.name, got[c.name], want[c.name])
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d float32 tie tokens disagree with schema-go", len(bad))
	}
}
