package tabledemo_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoSaveLoadAgreeWithTheEngine saves every testgen sample (docs/
// SPEC-TABLES.md §16.11) of every root of tables/examples through the
// generated <T>Measure, <T>Save and <T>SaveText, and reads the engine's bytes
// back through the generated <T>Load, <T>ReadStrict and <T>LoadText, every
// answer held to the engine's (internal/tablewire Encode):
//
//   - "the plain `<T>Save` of a fixed table writes form `3`, the bytes
//     `<T>FixedSave` writes" (§3), so the save at a fixed root writes the
//     engine's form-3 bytes and every read reads them;
//   - "Form `1` is `previous_form`" at every read of a fixed root (§3), so
//     every read refuses the form-1 bytes of a fixed root's value;
//   - a fixed table past the record ceiling has no file form: "its file save
//     refuses rather than falling back to form `1`" (§3.4), so the measure,
//     the save and the text save refuse where the engine does.
func TestGoSaveLoadAgreeWithTheEngine(t *testing.T) {
	t.Parallel()
	for _, f := range goSaveLoadRun(t, nil) {
		t.Error(f)
	}
}

// TestGoSaveLoadAgreeWithTheEngineNegativeControl puts the variable form's
// save back at every fixed root (the form-1 bytes of the same value, which is
// what the plain save wrote before it was the fixed form's) and requires the
// test above to see it at every fixed root's save, and nowhere else.
func TestGoSaveLoadAgreeWithTheEngineNegativeControl(t *testing.T) {
	t.Parallel()
	failures := goSaveLoadRun(t, func(c goSaveLoadCase) ([]byte, bool) {
		if !c.fixed {
			return nil, false
		}
		return c.formOne, true
	})
	roots := map[string]bool{}
	for _, f := range failures {
		root, _, _ := strings.Cut(f, "/")
		if !strings.Contains(f, ": save ") {
			t.Errorf("the control broke another answer: %s", f)
			continue
		}
		roots[root] = true
	}
	if want := goSaveLoadFixedRoots(t); len(roots) != want {
		t.Fatalf("with the form-1 save restored, %d fixed roots disagree with the engine, want all %d: the test cannot see the form", len(roots), want)
	}
}

// goSaveLoadCase is one sample: the root, the JSON text, the bytes the engine
// writes (nil where it refuses) and the form-1 bytes of the same value.
type goSaveLoadCase struct {
	root, name string
	text       []byte
	want       []byte
	formOne    []byte
	fixed      bool
}

// goSaveLoadRun runs every case through the generated entry points. A
// non-nil plant answers the save's bytes in place of the generated save, for
// the cases it claims.
func goSaveLoadRun(t *testing.T, plant func(goSaveLoadCase) ([]byte, bool)) []string {
	t.Helper()
	cases := goSaveLoadCases(t)
	binary := map[string]target{}
	for _, tg := range targets {
		binary[tg.name] = tg
	}
	text := map[string]textTarget{}
	for _, tg := range textTargets {
		text[tg.name] = tg
	}
	var failures []string
	fail := func(c goSaveLoadCase, format string, args ...any) {
		failures = append(failures, c.root+"/"+c.name+": "+fmt.Sprintf(format, args...))
	}
	for _, c := range cases {
		bt, okB := binary[c.root]
		tt, okT := text[c.root]
		if !okB || !okT {
			fail(c, "no fuzz target carries this root")
			continue
		}
		v, release, ok := tt.fromJson(c.text)
		if !ok {
			fail(c, "FromJSON refused the sample")
			release()
			continue
		}
		var saved []byte
		measure := bt.measure(v)
		if planted, ok := plant0(plant, c); ok {
			saved = planted
		} else if measure >= 0 {
			saved = make([]byte, measure)
			if n := bt.save(v, saved); n != measure {
				saved = nil
			}
		} else if n := bt.save(v, make([]byte, 1<<20)); n >= 0 {
			saved = []byte{}
		}
		savedText, textErr := tt.saveText(v)
		release()
		if c.want == nil {
			if saved != nil {
				fail(c, "save the engine refuses and Go writes %.40s...", hex.EncodeToString(saved))
			}
			if measure >= 0 {
				fail(c, "measure the engine refuses the save and Go measures %d bytes", measure)
			}
			if textErr == nil {
				fail(c, "text the engine refuses the save and Go writes %.40s...", savedText)
			}
		} else {
			if !bytes.Equal(saved, c.want) {
				fail(c, "save Go writes %.40s..., the engine %.40s...", hex.EncodeToString(saved), hex.EncodeToString(c.want))
			}
			if textErr != nil || savedText != base64.StdEncoding.EncodeToString(c.want) {
				fail(c, "text Go writes %.40s... (%v), the engine's bytes are %.40s...", savedText, textErr, base64.StdEncoding.EncodeToString(c.want))
			}
			if _, ok := bt.load(append([]byte(nil), c.want...)); !ok {
				fail(c, "load refuses the engine's bytes")
			}
			if got, err := bt.readStrict(append([]byte(nil), c.want...)); got == nil || err != nil {
				fail(c, "strict refuses the engine's bytes: %v", err)
			}
			if _, ok := tt.loadText(base64.StdEncoding.EncodeToString(c.want)); !ok {
				fail(c, "loadtext refuses the engine's bytes")
			}
		}
		if c.fixed {
			if _, ok := bt.load(append([]byte(nil), c.formOne...)); ok {
				fail(c, "formone-load reads the value's form-1 bytes at a fixed root")
			}
			if got, err := bt.readStrict(append([]byte(nil), c.formOne...)); got != nil || err == nil {
				fail(c, "formone-strict reads the value's form-1 bytes at a fixed root")
			}
			if _, ok := tt.loadText(base64.StdEncoding.EncodeToString(c.formOne)); ok {
				fail(c, "formone-loadtext reads the value's form-1 bytes at a fixed root")
			}
		}
	}
	return failures
}

func plant0(plant func(goSaveLoadCase) ([]byte, bool), c goSaveLoadCase) ([]byte, bool) {
	if plant == nil {
		return nil, false
	}
	return plant(c)
}

func goSaveLoadUnit(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{"../../../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// goSaveLoadFixedRoots counts the declared fixed roots, with a file form or not.
func goSaveLoadFixedRoots(t *testing.T) int {
	t.Helper()
	u := goSaveLoadUnit(t)
	n := 0
	for root := range goTestgenSamples(t, u) {
		if u.Tables[root].FixedDeclared {
			n++
		}
	}
	return n
}

// goSaveLoadCases is every testgen sample of every root of tables/examples
// with the engine's bytes. It fails when the unit holds no variable root, no
// fixed root with a file form or no fixed root past the ceiling, so all three
// rules are always pinned.
func goSaveLoadCases(t *testing.T) []goSaveLoadCase {
	t.Helper()
	u := goSaveLoadUnit(t)
	samples := goTestgenSamples(t, u)
	model := tabletext.NewModel(u)
	roots := make([]string, 0, len(samples))
	for r := range samples {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	kinds := map[string]int{}
	var cases []goSaveLoadCase
	for _, r := range roots {
		st := u.Tables[r]
		for _, s := range samples[r] {
			inst := model.New(st)
			report := new(tabletext.Report)
			if !model.Read(inst, []byte(s[1]), report) {
				t.Fatalf("%s/%s: the engine refuses the sample's text: %+v", r, s[0], report)
			}
			c := goSaveLoadCase{root: r, name: s[0], text: []byte(s[1]), fixed: st.FixedDeclared}
			wire, err := tablewire.Encode(model, inst)
			var refusal *tablewire.FixedRefusal
			switch {
			case err == nil:
				c.want = wire
			case errors.As(err, &refusal) && refusal.Reason == tablewire.FixedReasonLayoutUnsupported:
				kinds["past the ceiling"]++
			default:
				t.Fatalf("%s/%s: the engine refuses the sample: %v", r, s[0], err)
			}
			if c.fixed {
				if c.formOne, err = tablewire.EncodeVariable(model, inst); err != nil {
					t.Fatalf("%s/%s: %v", r, s[0], err)
				}
				if c.want != nil {
					kinds["fixed"]++
				}
			} else {
				kinds["variable"]++
			}
			cases = append(cases, c)
		}
	}
	for _, k := range []string{"variable", "fixed", "past the ceiling"} {
		if kinds[k] == 0 {
			t.Fatalf("tables/examples holds no %s root: the test pins nothing there", k)
		}
	}
	return cases
}

// goTestgenSamples is every root's testgen samples (name, JSON text), read out
// of the Go testgen's Test<Root>GenRoundTrip lists as compiler/testgen_luau.go
// reads them, so the samples are the one list every leg's generated tests
// share.
func goTestgenSamples(t *testing.T, u *ir.Unit) map[string][][2]string {
	t.Helper()
	files, err := gotable.Testgen(u)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][][2]string{}
	for name, src := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") || !strings.HasSuffix(fn.Name.Name, "GenRoundTrip") {
				continue
			}
			root := strings.TrimSuffix(strings.TrimPrefix(fn.Name.Name, "Test"), "GenRoundTrip")
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
					return true
				}
				if id, ok := as.Lhs[0].(*ast.Ident); !ok || id.Name != "samples" {
					return true
				}
				list, ok := as.Rhs[0].(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, e := range list.Elts {
					pair, ok := e.(*ast.CompositeLit)
					if !ok || len(pair.Elts) != 2 {
						t.Fatalf("%s: a sample is not a name and a text", root)
					}
					out[root] = append(out[root], [2]string{goEvalString(t, pair.Elts[0]), goEvalString(t, pair.Elts[1])})
				}
				return false
			})
		}
	}
	if len(out) == 0 {
		t.Fatal("go testgen wrote no samples")
	}
	return out
}

// goEvalString evaluates the string expressions go testgen writes: literals,
// their + concatenation, strings.Repeat, and the base64 of bytes.Repeat.
func goEvalString(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.BasicLit:
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			t.Fatal(err)
		}
		return s
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return goEvalString(t, x.X) + goEvalString(t, x.Y)
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			switch {
			case sel.Sel.Name == "Repeat" && len(x.Args) == 2:
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "bytes" {
					return strings.Repeat(string(goEvalBytes(t, x.Args[0])), goEvalInt(t, x.Args[1]))
				}
				return strings.Repeat(goEvalString(t, x.Args[0]), goEvalInt(t, x.Args[1]))
			case sel.Sel.Name == "EncodeToString" && len(x.Args) == 1:
				return base64.StdEncoding.EncodeToString(goEvalBytes(t, x.Args[0]))
			}
		}
	}
	t.Fatalf("unsupported sample expression %T", e)
	return ""
}

func goEvalInt(t *testing.T, e ast.Expr) int {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		t.Fatalf("not an integer literal: %T", e)
	}
	n, err := strconv.ParseInt(lit.Value, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func goEvalBytes(t *testing.T, e ast.Expr) []byte {
	t.Helper()
	switch x := e.(type) {
	case *ast.CompositeLit:
		var out []byte
		for _, el := range x.Elts {
			out = append(out, byte(goEvalInt(t, el)))
		}
		return out
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Repeat" && len(x.Args) == 2 {
			return []byte(strings.Repeat(string(goEvalBytes(t, x.Args[0])), goEvalInt(t, x.Args[1])))
		}
	}
	t.Fatalf("unsupported byte expression %T", e)
	return nil
}
