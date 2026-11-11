package mootable_test

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMooSaveLoadAgreeWithTheEngine saves every testgen sample (docs/
// SPEC-TABLES.md §16.11) of every root of tables/examples through moo's
// generic save (table_save.moo's save), its text carrier (<Root>SaveText) and
// reads the engine's bytes back through the generic load (table_load.moo's
// load) and the strict read (table_refusal.moo's ReadStrict), every answer
// held to the engine's (internal/tablewire Encode):
//
//   - "A declared `fixed table` encodes as form `3`, ALWAYS" and "the plain
//     `<T>Save` of a fixed table writes form `3`" (§3, NEITHER CLASS BORROWS
//     THE OTHER'S FORM), so the generic save at a fixed root writes the
//     engine's form-3 bytes and both reads read them;
//   - "One rule, for every read at a declared fixed root that carries form
//     `3`: `<T>FixedLoad`, the plain `<T>Load`, `ReadStrict` and the retain
//     load alike. Form `1` is `previous_form`" (§3), so both reads refuse the
//     form-1 bytes of a fixed root's value;
//   - a fixed table past the 65536-byte record ceiling "HAS NO FILE FORM: its
//     file save refuses (it does not fall back to form `1`), every read at its
//     root refuses form `1`" (§3.4, THE SIZE BOUNDS), so the save and the text
//     save refuse where the engine does, and both reads refuse the form-1
//     bytes of the same value.
func TestMooSaveLoadAgreeWithTheEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated save and load over every example root)")
	t.Parallel()
	for _, f := range mooSaveLoadRun(t, nil) {
		t.Error(f)
	}
}

// TestMooSaveLoadAgreeWithTheEngineNegativeControl takes the fixed branch out
// of the generic save, which then writes form 1 at every fixed root, and
// requires the test above to see it in the save, the text save and the
// reload, and nowhere else.
func TestMooSaveLoadAgreeWithTheEngineNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged table_save.moo over every example root)")
	t.Parallel()
	failures := mooSaveLoadRun(t, func(files map[string][]byte) {
		src := string(files["table_save.moo"])
		const site = "\tif d.fixed then\n\t\treturn fixed_save(d, v)\n\tend\n"
		if strings.Count(src, site) != 1 {
			t.Fatal("the control's site moved: table_save.moo's save no longer branches to fixed_save")
		}
		files["table_save.moo"] = []byte(strings.Replace(src, site, "", 1))
	})
	saves := 0
	for _, f := range failures {
		if !strings.Contains(f, " save ") && !strings.Contains(f, " text ") && !strings.Contains(f, " reload ") {
			t.Errorf("the control broke another answer: %s", f)
			continue
		}
		saves++
	}
	if saves == 0 {
		t.Fatal("with the fixed branch gone, every fixed root's save still agrees with the engine: the test cannot see the form")
	}
}

// mooSaveLoadCase is one sample: the root, its module, the JSON text, the
// bytes the engine writes (nil where it refuses), and the bytes each read is
// handed with the verdict owed.
type mooSaveLoadCase struct {
	root, module, name string
	text               []byte
	want               []byte
	formOne            []byte
	fixed              bool
}

func mooSaveLoadRun(t *testing.T, sabotage func(map[string][]byte)) []string {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	cases := mooSaveLoadCases(t, u)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if sabotage != nil {
		sabotage(files)
	}
	g := writeUnit(t, u, files)
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(`--!strict
local inputs: any = require("./inputs")
local Save: any = require("./table_save")
local Load: any = require("./table_load")
local Refusal: any = require("./table_refusal")
local function hex(b: buffer?): string
	if b == nil then
		return "refused"
	end
	local s = buffer.tostring(b :: buffer)
	local parts = table.create(#s)
	for i = 1, #s do
		parts[i] = string.format("%02x", string.byte(s, i) :: number)
	end
	return table.concat(parts)
end
local function verdict(v: any, r: any): string
	if v == nil or r.damaged or r.reason ~= nil then
		return "refused"
	end
	return "read"
end
local function case(i: number, mod: any, root: string, text: buffer, wire: buffer)
	local d = mod[root .. "Descriptor"]
	local v = mod["New" .. root]()
	if not mod[root .. "FromJson"](v, text, {}) then
		print(i .. " json refused")
		return
	end
	local bytes = Save.save(d, v)
	print(i .. " save " .. hex(bytes))
	local st = mod[root .. "SaveText"](v)
	print(i .. " text " .. (if st == nil then "refused" else st))
	local r1 = Load.newReport()
	print(i .. " load " .. verdict(Load.load(root, wire, r1), r1))
	local r2 = Load.newReport()
	print(i .. " strict " .. verdict(Refusal.ReadStrict(mod, root, wire, r2), r2))
	if bytes ~= nil then
		local r3 = Load.newReport()
		print(i .. " reload " .. verdict(Load.load(root, bytes, r3), r3))
	end
end
`)
	for i, c := range cases {
		inputs[fmt.Sprintf("t%d", i)] = c.text
		wire := c.want
		if wire == nil {
			wire = c.formOne
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		fmt.Fprintf(&b, "case(%d, require(\"./%s\"), %q, inputs.t%d, inputs.w%d)\n", i, c.module, c.root, i, i)
		if c.fixed {
			// the same value's form-1 bytes: every read at a fixed root refuses
			// them as previous_form (§3)
			inputs[fmt.Sprintf("v%d", i)] = c.formOne
			fmt.Fprintf(&b, "do local r = Load.newReport(); print(\"%d formone-load \" .. verdict(Load.load(%q, inputs.v%d, r), r)) end\n", i, c.root, i)
			fmt.Fprintf(&b, "do local r = Load.newReport(); print(\"%d formone-strict \" .. verdict(Refusal.ReadStrict(require(\"./%s\"), %q, inputs.v%d, r), r)) end\n", i, c.module, c.root, i)
		}
	}
	out := g.run(t, b.String(), inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if i := strings.LastIndexByte(line, ' '); i > 0 {
			got[line[:i]] = line[i+1:]
		}
	}
	var failures []string
	check := func(where, key, want string) {
		if have, ok := got[key]; !ok {
			failures = append(failures, fmt.Sprintf("%s: no %q answer", where, key))
		} else if have != want {
			failures = append(failures, fmt.Sprintf("%s: %s answers %.48s, the engine %.48s", where, key, have, want))
		}
	}
	for i, c := range cases {
		where := c.root + "/" + c.name
		if got[fmt.Sprintf("%d json", i)] == "refused" {
			failures = append(failures, where+": FromJson refused the sample")
			continue
		}
		if c.want == nil {
			check(where, fmt.Sprintf("%d save", i), "refused")
			check(where, fmt.Sprintf("%d text", i), "refused")
			check(where, fmt.Sprintf("%d load", i), "refused")
			check(where, fmt.Sprintf("%d strict", i), "refused")
		} else {
			check(where, fmt.Sprintf("%d save", i), hex.EncodeToString(c.want))
			check(where, fmt.Sprintf("%d text", i), base64.StdEncoding.EncodeToString(c.want))
			check(where, fmt.Sprintf("%d load", i), "read")
			check(where, fmt.Sprintf("%d strict", i), "read")
			if _, ok := got[fmt.Sprintf("%d reload", i)]; ok {
				check(where, fmt.Sprintf("%d reload", i), "read")
			}
		}
		if c.fixed {
			check(where, fmt.Sprintf("%d formone-load", i), "refused")
			check(where, fmt.Sprintf("%d formone-strict", i), "refused")
		}
	}
	return failures
}

// mooSaveLoadCases is every testgen sample of every root of the unit with the
// engine's bytes. It fails when the unit holds no variable root, no fixed root
// with a file form or no fixed root past the ceiling, so all three rules are
// always pinned.
func mooSaveLoadCases(t *testing.T, u *ir.Unit) []mooSaveLoadCase {
	t.Helper()
	samples := mooTestgenSamples(t, u)
	module := map[string]string{}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			module[st.Name] = f.Base + "Table"
		}
	}
	model := tabletext.NewModel(u)
	var cases []mooSaveLoadCase
	kinds := map[string]int{}
	roots := make([]string, 0, len(samples))
	for r := range samples {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	for _, r := range roots {
		st := u.Tables[r]
		for _, s := range samples[r] {
			inst := model.New(st)
			report := new(tabletext.Report)
			if !model.Read(inst, []byte(s[1]), report) {
				t.Fatalf("%s/%s: the engine refuses the sample's text: %+v", r, s[0], report)
			}
			c := mooSaveLoadCase{root: r, module: module[r], name: s[0], text: []byte(s[1]), fixed: st.FixedDeclared}
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

// mooTestgenSamples is every root's testgen samples (name, JSON text), read
// out of the Go testgen's Test<Root>GenRoundTrip lists as
// compiler/testgen_luau.go reads them, so the samples are the one list every
// leg's generated tests share.
func mooTestgenSamples(t *testing.T, u *ir.Unit) map[string][][2]string {
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
			r := strings.TrimSuffix(strings.TrimPrefix(fn.Name.Name, "Test"), "GenRoundTrip")
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
						t.Fatalf("%s: a sample is not a name and a text", r)
					}
					out[r] = append(out[r], [2]string{mooEvalString(t, pair.Elts[0]), mooEvalString(t, pair.Elts[1])})
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

// mooEvalString evaluates the string expressions go testgen writes: literals,
// their + concatenation, strings.Repeat, and the base64 of bytes.Repeat.
func mooEvalString(t *testing.T, e ast.Expr) string {
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
			return mooEvalString(t, x.X) + mooEvalString(t, x.Y)
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			switch {
			case sel.Sel.Name == "Repeat" && len(x.Args) == 2:
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "bytes" {
					return strings.Repeat(string(mooEvalBytes(t, x.Args[0])), mooEvalInt(t, x.Args[1]))
				}
				return strings.Repeat(mooEvalString(t, x.Args[0]), mooEvalInt(t, x.Args[1]))
			case sel.Sel.Name == "EncodeToString" && len(x.Args) == 1:
				return base64.StdEncoding.EncodeToString(mooEvalBytes(t, x.Args[0]))
			}
		}
	}
	t.Fatalf("unsupported sample expression %T", e)
	return ""
}

func mooEvalInt(t *testing.T, e ast.Expr) int {
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

func mooEvalBytes(t *testing.T, e ast.Expr) []byte {
	t.Helper()
	switch x := e.(type) {
	case *ast.CompositeLit:
		var out []byte
		for _, el := range x.Elts {
			out = append(out, byte(mooEvalInt(t, el)))
		}
		return out
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Repeat" && len(x.Args) == 2 {
			return []byte(strings.Repeat(string(mooEvalBytes(t, x.Args[0])), mooEvalInt(t, x.Args[1])))
		}
	}
	t.Fatalf("unsupported byte expression %T", e)
	return nil
}
