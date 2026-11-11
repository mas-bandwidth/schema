// The oracle half of the round-trip gate: every root table of tables/examples,
// every sample the generated tests use (docs/SPEC-TABLES.md §16.11), and the
// bytes the Go engine writes for each (internal/tabletext reads the sample's
// JSON text, internal/tablewire.Encode writes the file). Nothing here runs a
// foreign toolchain.
package roundtrip

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// unit is one schema unit every row generates and drives, and where its
// samples come from.
type unit struct {
	name    string   // the ratchet's unit column
	dir     string   // repository-relative
	classes []string // the root classes it must carry, so no assertion compares nothing
	samples func(st *ir.Struct) (names, texts []string, err error)
}

// units is every unit, in the order a row runs them. The first carries the
// negative controls' plants.
var units = []unit{
	{name: "examples", dir: "tables/examples", classes: []string{classFixed, classCeiling, classVariable}, samples: testgenSamples},
	{name: "nested", dir: "test/roundtrip/nested", classes: []string{classFixed, classVariable}, samples: nestedSamples},
}

// testgenSamples is a root's generated-test samples (docs/SPEC-TABLES.md
// §16.11), evaluated.
func testgenSamples(st *ir.Struct) (names, texts []string, err error) {
	names, exprs, err := gotable.TestgenSamples(st)
	if err != nil {
		return nil, nil, err
	}
	for i, expr := range exprs {
		text, err := evalSample(expr)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", names[i], err)
		}
		texts = append(texts, text)
	}
	return names, texts, nil
}

// A root's CLASS decides which assertions its cases owe (docs/SPEC-TABLES.md
// §3.4, "SELECTION IS BY THE KEYWORD").
const (
	// classFixed is a declared `fixed table` that carries form 3.
	classFixed = "fixed"
	// classCeiling is a declared `fixed table` past the record ceiling: it
	// carries no file form, so a file save refuses.
	classCeiling = "ceiling"
	// classVariable is an undeclared `table`: form 1.
	classVariable = "variable"
)

type sample struct {
	name   string
	json   []byte
	engine []byte // the engine's file; nil at a ceiling root, where it refuses
}

type root struct {
	name    string
	class   string
	samples []sample
}

func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// loadUnit is a unit, loaded the way the CLI loads it: no writes.
func loadUnit(t testing.TB, repo, dir string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(repo, dir)})
	if err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	c.FormatInPlace = false
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	return u
}

// classOf is a root table's class, from the declaration alone.
func classOf(u *ir.Unit, st *ir.Struct) string {
	switch {
	case !st.FixedDeclared:
		return classVariable
	case ir.TableFixedEmitted(u, st):
		return classFixed
	default:
		return classCeiling
	}
}

// oracleRoots is every root table of the unit (§16.11: every declared table; a
// generated map entry is not one), its samples, and the engine's bytes for
// each. The engine refusing a sample it generated is a defect of the gate's
// inputs, so it stops the test rather than becoming a case.
func oracleRoots(t testing.TB, u *ir.Unit, un unit) []root {
	t.Helper()
	model := tabletext.NewModel(u)
	var roots []root
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() {
				continue
			}
			r := root{name: st.Name, class: classOf(u, st)}
			names, texts, err := un.samples(st)
			if err != nil {
				t.Fatalf("%s %s: samples: %v", un.name, st.Name, err)
			}
			for i, name := range names {
				text := texts[i]
				inst := model.New(st)
				var report tabletext.Report
				if !model.Read(inst, []byte(text), &report) {
					t.Fatalf("%s/%s: the engine refuses the sample's own JSON text: %+v", st.Name, name, report)
				}
				wire, err := tablewire.Encode(model, inst)
				switch {
				case r.class == classCeiling && err == nil:
					t.Fatalf("%s/%s: the engine wrote a file at a root past the record ceiling", st.Name, name)
				case r.class == classCeiling:
					wire = nil
				case err != nil:
					t.Fatalf("%s/%s: the engine refuses to write the sample: %v", st.Name, name, err)
				}
				r.samples = append(r.samples, sample{name: name, json: []byte(text), engine: wire})
			}
			if len(r.samples) == 0 {
				t.Fatalf("%s: no sample", st.Name)
			}
			roots = append(roots, r)
		}
	}
	if len(roots) == 0 {
		t.Fatalf("%s declares no root table", un.dir)
	}
	return roots
}

// writeInputs lays the oracle out for a probe: samples/<root>/<sample>.json,
// engine/<root>/<sample>.bin (absent at a ceiling root) and cases.txt, one
// `<root> <sample> <class>` line per case.
func writeInputs(t testing.TB, dir string, roots []root) {
	t.Helper()
	var cases strings.Builder
	for _, r := range roots {
		for _, s := range r.samples {
			for _, sub := range []string{"samples", "engine", "out"} {
				if err := os.MkdirAll(filepath.Join(dir, sub, r.name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "samples", r.name, s.name+".json"), s.json, 0o644); err != nil {
				t.Fatal(err)
			}
			if s.engine != nil {
				if err := os.WriteFile(filepath.Join(dir, "engine", r.name, s.name+".bin"), s.engine, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			fmt.Fprintf(&cases, "%s %s %s\n", r.name, s.name, r.class)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.txt"), []byte(cases.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// evalSample is the value of a testgen sample expression. The grammar is the
// one gotable's testgen writes and nothing more: string literals joined by
// `+`, strings.Repeat(s, n), and
// base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, n)). Anything else
// is refused by name, so a testgen that learns a new spelling fails here
// rather than feeding a probe a wrong sample.
func evalSample(expr string) (string, error) {
	e, err := parser.ParseExpr(expr)
	if err != nil {
		return "", fmt.Errorf("sample expression: %w", err)
	}
	return evalString(e)
}

func evalString(e ast.Expr) (string, error) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", fmt.Errorf("sample expression: literal %s is not a string", x.Value)
		}
		return strconv.Unquote(x.Value)
	case *ast.ParenExpr:
		return evalString(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", fmt.Errorf("sample expression: operator %s", x.Op)
		}
		a, err := evalString(x.X)
		if err != nil {
			return "", err
		}
		b, err := evalString(x.Y)
		if err != nil {
			return "", err
		}
		return a + b, nil
	case *ast.CallExpr:
		switch callName(x.Fun) {
		case "strings.Repeat":
			if len(x.Args) != 2 {
				return "", fmt.Errorf("sample expression: strings.Repeat takes 2 arguments")
			}
			s, err := evalString(x.Args[0])
			if err != nil {
				return "", err
			}
			n, err := evalInt(x.Args[1])
			if err != nil {
				return "", err
			}
			return strings.Repeat(s, n), nil
		case "base64.StdEncoding.EncodeToString":
			if len(x.Args) != 1 {
				return "", fmt.Errorf("sample expression: EncodeToString takes 1 argument")
			}
			b, err := evalBytes(x.Args[0])
			if err != nil {
				return "", err
			}
			return base64.StdEncoding.EncodeToString(b), nil
		}
	}
	return "", fmt.Errorf("sample expression: %T is not in testgen's grammar", e)
}

func evalBytes(e ast.Expr) ([]byte, error) {
	call, ok := e.(*ast.CallExpr)
	if !ok || callName(call.Fun) != "bytes.Repeat" || len(call.Args) != 2 {
		return nil, fmt.Errorf("sample expression: bytes are only bytes.Repeat([]byte{b}, n)")
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok || len(lit.Elts) != 1 {
		return nil, fmt.Errorf("sample expression: bytes.Repeat of one byte")
	}
	b, err := evalInt(lit.Elts[0])
	if err != nil || b < 0 || b > 255 {
		return nil, fmt.Errorf("sample expression: byte %v", err)
	}
	n, err := evalInt(call.Args[1])
	if err != nil {
		return nil, err
	}
	return bytes.Repeat([]byte{byte(b)}, n), nil
}

func evalInt(e ast.Expr) (int, error) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, fmt.Errorf("sample expression: count is not an integer literal")
	}
	v, err := strconv.ParseInt(lit.Value, 0, 64)
	return int(v), err
}

func callName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return callName(x.X) + "." + x.Sel.Name
	}
	return ""
}

// TestOracleSampleGrammar holds the evaluator to every spelling testgen writes,
// and to refusing one it does not.
func TestOracleSampleGrammar(t *testing.T) {
	for expr, want := range map[string]string{
		`"{\"a\":1}"`:                         `{"a":1}`,
		`"[" + strings.Repeat(",x", 2) + "]"`: `[,x,x]`,
		`"\"" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, 3)) + "\""`: `"` + base64.StdEncoding.EncodeToString([]byte("ZZZ")) + `"`,
		`strings.Repeat("," + "1", 3)`: `,1,1,1`,
	} {
		got, err := evalSample(expr)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", expr, got, err, want)
		}
	}
	for _, expr := range []string{`fmt.Sprint(1)`, `1 + 2`, `"a" - "b"`} {
		if _, err := evalSample(expr); err == nil {
			t.Errorf("%s: evaluated, want a refusal", expr)
		}
	}
}

// TestOracleRoots holds the oracle's own shape: every root of every unit has
// at least one sample, every class the unit owes is present, and the engine
// wrote form 3 at every fixed root and form 1 at every variable root.
func TestOracleRoots(t *testing.T) {
	repo := repoRoot(t)
	for _, un := range units {
		t.Run(un.name, func(t *testing.T) { checkOracleRoots(t, un, oracleRoots(t, loadUnit(t, repo, un.dir), un)) })
	}
}

func checkOracleRoots(t *testing.T, un unit, roots []root) {
	seen := map[string]int{}
	for _, r := range roots {
		seen[r.class]++
		for _, s := range r.samples {
			switch r.class {
			case classFixed:
				if len(s.engine) == 0 || s.engine[0] != 3 {
					t.Errorf("%s/%s: the engine's file at a fixed root is not form 3", r.name, s.name)
				}
			case classVariable:
				if len(s.engine) == 0 || s.engine[0] != 1 {
					t.Errorf("%s/%s: the engine's file at a variable root is not form 1", r.name, s.name)
				}
			}
		}
	}
	for _, c := range un.classes {
		if seen[c] == 0 {
			t.Errorf("%s has no %s root: the gate's %s assertions would compare nothing", un.dir, c, c)
		}
	}
}
