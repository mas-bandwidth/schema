package jstable

// EVERY TESTGEN SAMPLE OF EVERY FIXED ROOT IN tables/examples, THROUGH THE
// FIXED FORM (docs/SPEC-TABLES.md §3.4, §16.11): the Go engine
// (internal/tablewire) writes the sample's form-3 file, the generated
// <Root>FixedLoad reads it, and the generated <Root>FixedSave writes it back.
// The JS bytes must equal the engine's, and the engine must read the JS bytes
// to the sample's own value.
//
// The samples are the Go leg's testgen samples, read out of gotable.Testgen's
// output, so this test and the generated round-trip tests agree on what
// "defaults", "low", "high", "inside" and "arm_<k>" are. The "high" sample puts
// every counted array at its declared maximum at every nesting depth, and this
// test checks that it does: an array inside an array element inside an array
// element is where a decoder's offset arithmetic has to compose.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedSample is one testgen sample of one root.
type fixedSample struct {
	root, name, text string
}

// testgenSamples reads every Test<Root>GenRoundTrip's sample table out of the
// Go leg's generated tests, and evaluates each sample's text expression: string
// literals joined by +, strings.Repeat, and the base64 of a repeated byte. A
// construction outside that set fails the test rather than being guessed at.
func testgenSamples(t *testing.T, u *ir.Unit) map[string][]fixedSample {
	t.Helper()
	files, err := gotable.Testgen(u)
	if err != nil {
		t.Fatalf("gotable.Testgen: %v", err)
	}
	out := map[string][]fixedSample{}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(token.NewFileSet(), name, files[name], 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") || !strings.HasSuffix(fn.Name.Name, "GenRoundTrip") {
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
				lit, ok := as.Rhs[0].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("%s: samples is not a composite literal", fn.Name.Name)
				}
				for _, e := range lit.Elts {
					pair, ok := e.(*ast.CompositeLit)
					if !ok || len(pair.Elts) != 2 {
						t.Fatalf("%s: a sample is not a {name, text} pair", fn.Name.Name)
					}
					sname := evalSampleExpr(t, pair.Elts[0])
					out[root] = append(out[root], fixedSample{root, sname, evalSampleExpr(t, pair.Elts[1])})
				}
				return false
			})
		}
	}
	return out
}

func evalSampleExpr(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			s, err := strconv.Unquote(x.Value)
			if err != nil {
				t.Fatalf("sample literal %s: %v", x.Value, err)
			}
			return s
		}
	case *ast.ParenExpr:
		return evalSampleExpr(t, x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return evalSampleExpr(t, x.X) + evalSampleExpr(t, x.Y)
		}
	case *ast.CallExpr:
		switch sampleCallee(x.Fun) {
		case "strings.Repeat":
			if len(x.Args) == 2 {
				return strings.Repeat(evalSampleExpr(t, x.Args[0]), evalSampleInt(t, x.Args[1]))
			}
		case "base64.StdEncoding.EncodeToString":
			if len(x.Args) == 1 {
				if c, ok := x.Args[0].(*ast.CallExpr); ok && sampleCallee(c.Fun) == "bytes.Repeat" && len(c.Args) == 2 {
					if b, ok := c.Args[0].(*ast.CompositeLit); ok && len(b.Elts) == 1 {
						v := evalSampleInt(t, b.Elts[0])
						return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(v)}, evalSampleInt(t, c.Args[1])))
					}
				}
			}
		}
	}
	t.Fatalf("a testgen sample uses a construction this reader does not evaluate: %T", e)
	return ""
}

func evalSampleInt(t *testing.T, e ast.Expr) int {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		t.Fatalf("a testgen sample count is not an integer literal: %T", e)
	}
	n, err := strconv.ParseInt(lit.Value, 0, 64)
	if err != nil {
		t.Fatalf("sample count %s: %v", lit.Value, err)
	}
	return int(n)
}

func sampleCallee(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return sampleCallee(x.X) + "." + x.Sel.Name
	}
	return ""
}

// atCapacity walks a sample's JSON beside its declaration and answers every
// bounded array that is not at its declared maximum, and the deepest array
// nesting it saw. Keyed arrays (always full) and maps are not bounded arrays.
func atCapacity(st *ir.Struct, v any, path string, depth int, short *[]string) int {
	obj, ok := v.(map[string]any)
	if !ok {
		return depth
	}
	deepest := depth
	for _, f := range st.Fields {
		fv, ok := obj[ir.TableFieldJsonKey(f)]
		if !ok || fv == nil {
			continue
		}
		deepest = max(deepest, atCapacityField(f, fv, path+"."+f.Name, depth, short))
	}
	return deepest
}

func atCapacityField(f *ir.Field, v any, path string, depth int, short *[]string) int {
	if f.IsMap() || f.KeyEnum != "" {
		return depth
	}
	if f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted {
		arr, ok := v.([]any)
		if !ok {
			return depth
		}
		if int64(len(arr)) != f.ArrayBound {
			*short = append(*short, fmt.Sprintf("%s holds %d of %d", path, len(arr), f.ArrayBound))
		}
		deepest := depth + 1
		for i, e := range arr {
			deepest = max(deepest, atCapacityElem(f, e, fmt.Sprintf("%s[%d]", path, i), depth+1, short))
		}
		return deepest
	}
	return atCapacityElem(f, v, path, depth, short)
}

func atCapacityElem(f *ir.Field, v any, path string, depth int, short *[]string) int {
	if f.Type.Kind != ir.TNamed {
		return depth
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return atCapacity(r, v, path, depth, short)
	case *ir.Union:
		obj, ok := v.(map[string]any)
		if !ok {
			return depth
		}
		deepest := depth
		for _, arm := range r.Variants {
			if av, ok := obj[arm.Name]; ok && arm.F != nil && av != nil {
				deepest = max(deepest, atCapacityField(arm.F, av, path+"."+arm.Name, depth, short))
			}
		}
		return deepest
	}
	return depth
}

// fixedSampleDriver loads each case's engine bytes through the generated
// <Root>FixedLoad and writes them back through <Root>FixedSave. A throw is a
// failure by name, never a crash of the whole run.
const fixedSampleDriver = `import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
for (const c of spec.cases) {
  const tag = c.root + "/" + c.name;
  try {
    const M = await import(pathToFileURL(c.module).href);
    const H = await import(pathToFileURL(c.home).href);
    const wire = new Uint8Array(readFileSync(c.wire));
    const v = new H[c.root]();
    H["Init" + c.root](v);
    const rep = new H.TableFixedReport();
    const n = M[c.root + "FixedLoad"]([v], 1, wire, wire.length, M[c.root + "FixedNewPlan"](), rep);
    if (n !== 1 || rep.damaged || rep.refused !== 0 || rep.unknown !== 0 || rep.mismatch !== 0 ||
        rep.oor !== 0 || rep.widened !== 0 || rep.repeated !== 0) {
      console.log("FAIL " + tag + ": FixedLoad answered " + n + " refused=" + rep.refused + " damaged=" + rep.damaged +
        " unknown=" + rep.unknown + " mismatch=" + rep.mismatch + " oor=" + rep.oor + " widened=" + rep.widened + " repeated=" + rep.repeated);
      continue;
    }
    const out = new Uint8Array(M[c.root + "FixedMeasure"](1));
    const w = M[c.root + "FixedSave"]([v], 1, out);
    if (w !== out.length) {
      console.log("FAIL " + tag + ": FixedSave answered " + w + " of " + out.length);
      continue;
    }
    writeFileSync(c.saved, out);
    console.log("OK " + tag);
  } catch (e) {
    console.log("FAIL " + tag + ": threw " + (e && e.name) + ": " + (e && e.message));
  }
}
`

// TestJSFixedFormEveryExampleSample: engine bytes in, JS FixedLoad, JS
// FixedSave out, byte-equal to the engine's, for every sample of every root in
// tables/examples that this leg gives a fixed form. RootConfig and ArchiveConfig
// are the roots whose high sample fills a bounded array inside an array
// element inside an array element.
func TestJSFixedFormEveryExampleSample(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	node := jsNode(t)
	u := loadUnitPaths(t, []string{"../../../tables/examples"})
	fixedSamplesThroughJS(t, node, u, map[string]int{"RootConfig": 2, "ArchiveConfig": 2})
}

// TestJSFixedFormUnionArmArraySamples is the same round trip over a union whose
// arms are themselves a text and an array, held in an array: those arms are
// written and checked INLINE inside the element loop, with no function call
// between the two loops. tables/examples has no such union.
func TestJSFixedFormUnionArmArraySamples(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	node := jsNode(t)
	u := unitFrom(t, `package fixedarmprobe

union Arm
{
    vals  [..2]float32
    label string(8)
    nums  [..3]int32
}

fixed table Box
{
    arms [..4]Arm
    pair [2]Arm
}
`)
	fixedSamplesThroughJS(t, node, u, map[string]int{"Box": 2})
}

// fixedSamplesThroughJS runs every testgen sample of every root of u that this
// leg gives a fixed form through the engine, JS FixedLoad and JS FixedSave.
// must names the roots that have to be among them, each with the array nesting
// depth its high sample has to fill at capacity.
func fixedSamplesThroughJS(t *testing.T, node string, u *ir.Unit, must map[string]int) {
	t.Helper()
	samples := testgenSamples(t, u)

	gen, err := js.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate tables: %v", err)
	}
	maps.Copy(gen, tables)
	dir := t.TempDir()
	for name, data := range gen {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(dir, runtimeHome(u)+"Table.js")

	m := tabletext.NewModel(u)
	type jsCase struct {
		Root   string `json:"root"`
		Name   string `json:"name"`
		Module string `json:"module"`
		Home   string `json:"home"`
		Wire   string `json:"wire"`
		Saved  string `json:"saved"`
		engine []byte
		text   []byte
	}
	var cases []*jsCase
	var roots []string
	deepest := map[string]int{}
	for _, root := range slices.Sorted(maps.Keys(samples)) {
		var module string
		for name, data := range gen {
			if !strings.Contains(name, "/") && bytes.Contains(data, []byte("export function "+root+"FixedLoad(")) {
				module = filepath.Join(dir, name)
			}
		}
		if module == "" {
			continue // this root keeps form 1 only
		}
		st := m.Lookup(root)
		if st == nil || !tablewire.FixedRoot(m, st) {
			t.Fatalf("%s: the JS leg emits a FixedLoad and the engine does not write it as form 3", root)
		}
		roots = append(roots, root)
		for _, s := range samples[root] {
			inst := m.New(st)
			var rep tabletext.Report
			if !m.Read(inst, []byte(s.text), &rep) || rep != (tabletext.Report{}) {
				t.Fatalf("%s/%s: the engine did not read the sample cleanly: %+v", root, s.name, rep)
			}
			engine, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Fatalf("%s/%s: the engine did not write the sample: %v", root, s.name, err)
			}
			if len(engine) == 0 || engine[0] != 3 {
				t.Fatalf("%s/%s: the engine's file is not form 3", root, s.name)
			}
			text, err := m.Write(inst)
			if err != nil {
				t.Fatalf("%s/%s: the engine did not write the sample's text: %v", root, s.name, err)
			}
			if s.name == "high" {
				var v any
				if err := json.Unmarshal([]byte(s.text), &v); err != nil {
					t.Fatalf("%s/high: %v", root, err)
				}
				var short []string
				deepest[root] = atCapacity(st, v, root, 0, &short)
				for _, why := range short {
					t.Errorf("%s/high is not at capacity: %s", root, why)
				}
			}
			c := &jsCase{Root: root, Name: s.name, Module: module, Home: home,
				Wire:   filepath.Join(dir, root+"_"+s.name+".wire"),
				Saved:  filepath.Join(dir, root+"_"+s.name+".saved"),
				engine: engine, text: text}
			if err := os.WriteFile(c.Wire, engine, 0o600); err != nil {
				t.Fatal(err)
			}
			cases = append(cases, c)
		}
	}
	// THE CASES THIS TEST EXISTS FOR ARE PRESENT: each named root, with a high
	// sample that fills every bounded array it reaches, nested as deep as named.
	for _, want := range slices.Sorted(maps.Keys(must)) {
		if !slices.Contains(roots, want) {
			t.Fatalf("no fixed root %s with testgen samples (roots %v)", want, roots)
		}
		if deepest[want] < must[want] {
			t.Errorf("%s's high sample nests bounded arrays %d deep, want at least %d", want, deepest[want], must[want])
		}
	}

	spec, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(script, []byte(fixedSampleDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, specPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL ") {
			t.Error(line)
		}
		got[line] = true
	}
	for _, c := range cases {
		tag := c.Root + "/" + c.Name
		if !got["OK "+tag] {
			if !strings.Contains(string(out), "FAIL "+tag+":") {
				t.Errorf("%s: the probe said nothing", tag)
			}
			continue
		}
		saved, err := os.ReadFile(c.Saved)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, c.engine) {
			t.Errorf("%s: JS FixedSave wrote %d bytes that differ from the engine's %d", tag, len(saved), len(c.engine))
			continue
		}
		// AND THE VALUE: the engine reads the JS bytes to the sample's value.
		inst := m.New(m.Lookup(c.Root))
		var rep tabletext.Report
		if ok, err := tablewire.Decode(m, inst, saved, &rep); !ok || err != nil || rep != (tabletext.Report{}) {
			t.Errorf("%s: the engine did not read the JS bytes cleanly: ok=%v err=%v rep=%+v", tag, ok, err, rep)
			continue
		}
		text, err := m.Write(inst)
		if err != nil || !bytes.Equal(text, c.text) {
			t.Errorf("%s: the value JS loaded and saved is not the sample's (%v)", tag, err)
		}
	}
	t.Logf("%d samples over %d fixed roots: %v", len(cases), len(roots), roots)
}
