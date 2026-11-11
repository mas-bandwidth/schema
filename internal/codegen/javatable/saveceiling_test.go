package javatable

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaSaveAgreesWithTheEngineAtEveryExampleRoot saves every testgen
// sample (docs/SPEC-TABLES.md §16.11) of every root of tables/examples
// through the generated <Root>Variable class and holds the answer to the
// engine's (internal/tablewire Encode): the same bytes where the engine
// writes, and a refusal where it refuses. A root declared `fixed table` whose
// record is past the 65536-byte record ceiling has no file form: "its file
// save refuses (it does not fall back to form `1`), every read at its root
// refuses form `1`" (docs/SPEC-TABLES.md §3.4, THE SIZE BOUNDS), so its save
// answers null and its load refuses the form-1 bytes of the same value.
//
// A fixed root that has a file form is written by <Root>Fixed, which carries
// no text reader; its plain save is outside this test.
func TestJavaSaveAgreesWithTheEngineAtEveryExampleRoot(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))
	failures := javaSaveCeilingRun(t, u, nil)
	for _, f := range failures {
		t.Error(f)
	}
}

// TestJavaSaveCeilingNegativeControl makes the save at the root past the
// ceiling answer bytes again (a lone form-1 byte in place of the refusal) and
// requires the test above to see it: every sample of that root turns red.
func TestJavaSaveCeilingNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))
	const site = "        return null; // no file form (§3.4)\n"
	failures := javaSaveCeilingRun(t, u, func(files map[string][]byte) int {
		n := 0
		for name, src := range files {
			if strings.HasSuffix(name, "Variable.java") && strings.Contains(string(src), site) {
				files[name] = []byte(strings.Replace(string(src), site, "        return new byte[] { 1 };\n", 1))
				n++
			}
		}
		return n
	})
	if len(failures) == 0 {
		t.Fatal("with the form-1 save restored past the ceiling, every sample still agrees: the test cannot see the refusal")
	}
	for _, f := range failures {
		if !strings.Contains(f, "the engine refuses") {
			t.Errorf("the control broke another root: %s", f)
		}
	}
}

// javaSaveCeilingCase is one sample: the root, the JSON text, the bytes the
// engine writes (nil where it refuses), and the bytes the load is handed.
type javaSaveCeilingCase struct {
	root, name string
	text       []byte
	want       []byte
	load       []byte
}

func javaSaveCeilingRun(t *testing.T, u *ir.Unit, sabotage func(map[string][]byte) int) []string {
	t.Helper()
	cases := javaSaveCeilingCases(t, u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}
	if sabotage != nil && sabotage(files) == 0 {
		t.Fatal("the control's site moved: no Variable class carries the no-file-form save refusal")
	}
	dir := t.TempDir()
	var drv strings.Builder
	fmt.Fprintf(&drv, "package %s;\n\npublic final class SaveCeilingDriver {\n", u.Package)
	drv.WriteString("    static byte[] in(String dir, String name) throws Exception { return java.nio.file.Files.readAllBytes(java.nio.file.Paths.get(dir, name)); }\n")
	drv.WriteString("    static String hex(byte[] b) { return b == null ? \"refused\" : java.util.HexFormat.of().formatHex(b); }\n")
	drv.WriteString("    public static void main(String[] args) throws Exception {\n        String d = args[0];\n")
	for i, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("t%d", i)), c.text, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("w%d", i)), c.load, 0o644); err != nil {
			t.Fatal(err)
		}
		cls := c.root + "Variable"
		fmt.Fprintf(&drv, "        { TableVariable.Report r = new TableVariable.Report(); %s.Value v = %s.fromJson(in(d, \"t%d\"), r);\n", cls, cls, i)
		fmt.Fprintf(&drv, "          System.out.println(\"%d save \" + (v == null || r.damaged || r.refused ? \"text-refused\" : hex(%s.save(v))));\n", i, cls)
		fmt.Fprintf(&drv, "          TableVariable.Report lr = new TableVariable.Report(); %s.Value l = %s.load(in(d, \"w%d\"), lr);\n", cls, cls, i)
		fmt.Fprintf(&drv, "          System.out.println(\"%d load \" + (l == null || lr.damaged || lr.refused ? \"refused\" : \"read\")); }\n", i)
	}
	drv.WriteString("    }\n}\n")
	files["SaveCeilingDriver.java"] = []byte(drv.String())
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	sort.Strings(sources)
	javac, java := javaTools(t)
	classes := filepath.Join(dir, "classes")
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, u.Package+".SaveCeilingDriver", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if i := strings.LastIndexByte(line, ' '); i > 0 {
			got[line[:i]] = line[i+1:]
		}
	}
	var failures []string
	for i, c := range cases {
		where := c.root + "/" + c.name
		save, load := got[fmt.Sprintf("%d save", i)], got[fmt.Sprintf("%d load", i)]
		switch {
		case c.want == nil && save != "refused":
			failures = append(failures, fmt.Sprintf("%s: the engine refuses the save and Java writes %.40s...", where, save))
		case c.want != nil && save != hex.EncodeToString(c.want):
			failures = append(failures, fmt.Sprintf("%s: Java saves %.40s..., the engine %.40s...", where, save, hex.EncodeToString(c.want)))
		}
		wantLoad := "read"
		if c.want == nil {
			wantLoad = "refused"
		}
		if load != wantLoad {
			failures = append(failures, fmt.Sprintf("%s: the load answers %q, owed %q", where, load, wantLoad))
		}
	}
	if len(got) != 2*len(cases) {
		failures = append(failures, fmt.Sprintf("the driver answered %d lines for %d samples", len(got), len(cases)))
	}
	return failures
}

// javaSaveCeilingCases is every testgen sample of every root the test holds,
// with the engine's bytes. It fails when no root is past the ceiling, so the
// test always pins the refusal.
func javaSaveCeilingCases(t *testing.T, u *ir.Unit) []javaSaveCeilingCase {
	t.Helper()
	samples := testgenSampleTexts(t, u)
	model := tabletext.NewModel(u)
	var cases []javaSaveCeilingCase
	refusing := 0
	for _, root := range sortedRoots(samples) {
		st := u.Tables[root]
		if st.FixedDeclared && ir.TableFixedEmitted(u, st) {
			continue
		}
		for _, s := range samples[root] {
			inst := model.New(st)
			report := new(tabletext.Report)
			if !model.Read(inst, []byte(s[1]), report) {
				t.Fatalf("%s/%s: the engine refuses the sample's text: %+v", root, s[0], report)
			}
			c := javaSaveCeilingCase{root: root, name: s[0], text: []byte(s[1])}
			wire, err := tablewire.Encode(model, inst)
			var refusal *tablewire.FixedRefusal
			switch {
			case err == nil:
				c.want, c.load = wire, wire
			case errors.As(err, &refusal) && refusal.Reason == tablewire.FixedReasonLayoutUnsupported:
				// the load is handed the form-1 bytes of the same value: the
				// file a form-1 save would have written, which every read at
				// the root refuses (§3.4)
				c.load, err = tablewire.EncodeVariable(model, inst)
				if err != nil {
					t.Fatalf("%s/%s: %v", root, s[0], err)
				}
				refusing++
			default:
				t.Fatalf("%s/%s: the engine refuses the sample: %v", root, s[0], err)
			}
			cases = append(cases, c)
		}
	}
	if refusing == 0 {
		t.Fatal("no root of the unit is past the record ceiling: the test pins no refusal")
	}
	return cases
}

func sortedRoots(m map[string][][2]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// testgenSampleTexts is every root's testgen samples (name, JSON text), read
// out of the Go testgen's Test<Root>GenRoundTrip lists as
// compiler/testgen_luau.go reads them, so the samples are the one list every
// leg's generated tests share.
func testgenSampleTexts(t *testing.T, u *ir.Unit) map[string][][2]string {
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
					out[root] = append(out[root], [2]string{tgEvalString(t, pair.Elts[0]), tgEvalString(t, pair.Elts[1])})
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

// tgEvalString evaluates the string expressions go testgen writes: literals,
// their + concatenation, strings.Repeat, and the base64 of bytes.Repeat.
func tgEvalString(t *testing.T, e ast.Expr) string {
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
			return tgEvalString(t, x.X) + tgEvalString(t, x.Y)
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			switch {
			case sel.Sel.Name == "Repeat" && len(x.Args) == 2:
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "bytes" {
					return strings.Repeat(string(tgEvalBytes(t, x.Args[0])), tgEvalInt(t, x.Args[1]))
				}
				return strings.Repeat(tgEvalString(t, x.Args[0]), tgEvalInt(t, x.Args[1]))
			case sel.Sel.Name == "EncodeToString" && len(x.Args) == 1:
				return base64.StdEncoding.EncodeToString(tgEvalBytes(t, x.Args[0]))
			}
		}
	}
	t.Fatalf("unsupported sample expression %T", e)
	return ""
}

func tgEvalInt(t *testing.T, e ast.Expr) int {
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

func tgEvalBytes(t *testing.T, e ast.Expr) []byte {
	t.Helper()
	switch x := e.(type) {
	case *ast.CompositeLit:
		var out []byte
		for _, el := range x.Elts {
			out = append(out, byte(tgEvalInt(t, el)))
		}
		return out
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Repeat" && len(x.Args) == 2 {
			return []byte(strings.Repeat(string(tgEvalBytes(t, x.Args[0])), tgEvalInt(t, x.Args[1])))
		}
	}
	t.Fatalf("unsupported byte expression %T", e)
	return nil
}
