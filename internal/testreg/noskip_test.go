package testreg

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// NO SKIPS (Glenn, live 2026-11-25, in capitals). "if you don't want to run a
// test, don't run it (i mean at the root)". The only place a test is left
// out is the root: the driver's query, expanded to names before the run.
// Below it nothing decides: no test, helper, adapter, driver, script,
// Makefile recipe or workflow step checks a condition and does not run, or
// turns a failure into a pass. What a skip used to wait for (a toolchain, a
// built corpus, a long run) is the test's tier, which the root selects on.
//
// noSkips finds every skip mechanism under root, each as "file:line: rule:
// text". The rules, each with a plant in TestNoSkipMechanismIsPlanted:
//
//	go-skip        a Skip, Skipf or SkipNow call on a testing value, or a
//	               method of that name declared on a test double
//	go-short       testing.Short()
//	go-env-gate    an environment read, set or lookup of a skip or lane
//	               variable (SCHEMA_SLOW, SCHEMA_REQUIRE_*, *SKIP*, ALLOC_ANY)
//	go-early-out   in a function that takes a testing value, an if that reads
//	               the environment, looks up a tool, stats a file, or reads
//	               the platform, and then only returns (nothing, or zero
//	               values) without failing: the body after it does not run
//	go-build-tag   a build constraint on a _test.go file other than the
//	               platform, cgo, a Go version or a schema_leg_* build variant
//	or-true        `|| true`, `|| :`, `|| exit 0` in a script, recipe or step
//	continue       `continue-on-error` in a workflow
//	if-false       `if: false` in a workflow
//	branch-skip    a workflow condition on the branch name (sprint/)
//	go-test-skip   `go test` with -skip, -test.skip or -short
//	skip-var       a skip or lane variable named in a script, recipe or step
//	skip-echo      a script, recipe or step that prints that it skips
//	skip-verdict   a harness driver (any language) that reports a row as
//	               skipped instead of passing or failing it
//	skip-ledger    a file that lists skips (known-skips.tsv, skip-ledger.txt)
//
// Not scanned, each for its reason: version control and dependency and build
// output (never source); third_party/ (vendored sibling code, owned by its
// own repository); working/ (scratch); docs (prose about the rule, and
// history); and any testdata/ directory (data a checker reads: Go never runs
// a test under testdata, and the checkers that read planted skips there,
// such as tools/gotestreport, refuse them). Inside this file, the plants are
// strings.
func noSkips(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && noSkipsSkipsDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		found, err := noSkipsFile(path, rel)
		if err != nil {
			return err
		}
		out = append(out, found...)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// noSkipsSkipsDir is a directory that holds no source of this repository's
// tests, or data only (the reasons are on noSkips).
func noSkipsSkipsDir(rel, name string) bool {
	switch name {
	case ".git", "node_modules", "_build", "deps", "target", ".dart_tool", "__pycache__", ".venv", "testdata", ".gradle", "obj":
		return true
	}
	switch rel {
	case "build", "working", "third_party", "bin", "dist", "docs":
		return true
	}
	return false
}

var (
	// a skip or lane variable: what a gate read, or a lane promised
	skipVar = regexp.MustCompile(`\b(SCHEMA_SLOW|SCHEMA_REQUIRE_[A-Z0-9_]+|SCHEMA_SKIP[A-Z0-9_]*|SKIP_SLOW_OK|SCHEMA_[A-Z0-9_]*ALLOC_ANY[A-Z0-9_]*)\b`)
	orTrue  = regexp.MustCompile(`\|\|\s*(true|:|exit\s+0)\b|\|\|\s*:\s*$|\|\|\s*:\s*[;)]`)
	contErr = regexp.MustCompile(`^\s*-?\s*continue-on-error\s*:`)
	ifFalse = regexp.MustCompile(`^\s*-?\s*if\s*:\s*(false|\$\{\{\s*false\s*\}\}|0)\s*$`)
	branch  = regexp.MustCompile(`startsWith\(\s*github\.(head_ref|ref_name|ref)\s*,\s*'(refs/heads/)?sprint/'\s*\)`)
	goTest  = regexp.MustCompile(`\bgo\s+test\b.*\s-(skip|test\.skip|short)(=|\s|$)`)
	// an announcement that something is being skipped; a message that a
	// skip happened and is a failure ("a test SKIPPED", exit 1) is a refusal
	echo = regexp.MustCompile(`\b(echo|printf)\b.*(\b(?i:skipping)\b|\b(?i:skip):)`)
	// a driver's verdict word for a row it did not run
	verdict = regexp.MustCompile(`["'](SKIP|SKIPPED|skip|skipped)["']|["'](?i:skipping|skipped)\b[^"']*["']|\bSKIP:`)
	ledger  = regexp.MustCompile(`(?i)(^|[-_/])(known-)?skips?(-ledger)?\.(tsv|txt)$`)
)

// script-like files: their lines are commands
func scriptKind(rel string, head []byte) string {
	base := filepath.Base(rel)
	switch {
	case base == "Makefile" || strings.HasSuffix(base, ".mk"):
		return "make"
	case strings.HasPrefix(rel, ".github/") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		return "workflow"
	case strings.HasSuffix(base, ".sh") || strings.HasSuffix(base, ".bash"):
		return "shell"
	case bytes.HasPrefix(head, []byte("#!")) && bytes.Contains(bytes.SplitN(head, []byte("\n"), 2)[0], []byte("sh")):
		return "shell"
	}
	return ""
}

// driverExt are the harness languages other than Go.
var driverExt = map[string]bool{
	".lua": true, ".luau": true, ".nut": true, ".rb": true, ".py": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true,
	".dart": true, ".ex": true, ".exs": true, ".java": true, ".cs": true, ".rs": true, ".c": true, ".h": true, ".cc": true,
	".cpp": true, ".hpp": true, ".moo": true, ".goose": true, ".lobster": true,
}

func noSkipsFile(path, rel string) ([]string, error) {
	if _, ok := noSkipsExempt[rel]; ok {
		return nil, nil
	}
	base := filepath.Base(rel)
	if ledger.MatchString(rel) {
		return []string{rel + ":1: skip-ledger: a file that lists skips"}, nil
	}
	ext := filepath.Ext(base)
	if strings.HasSuffix(base, ".md") || ext == ".sexp" {
		return nil, nil
	}
	if ext == ".go" {
		return goSkips(path, rel)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	kind := scriptKind(rel, data[:min(len(data), 128)])
	isDriver := driverExt[ext] && (strings.HasPrefix(rel, "test/") || strings.HasPrefix(rel, "tools/") || strings.HasPrefix(rel, "bench/"))
	if kind == "" && !isDriver {
		return nil, nil
	}
	var out []string
	hit := func(n int, rule, line string) {
		out = append(out, fmt.Sprintf("%s:%d: %s: %s", rel, n, rule, strings.TrimSpace(line)))
	}
	for i, line := range strings.Split(string(data), "\n") {
		n := i + 1
		code := line
		if kind != "" {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") {
				continue // a comment says why, it does not decide
			}
		}
		if kind != "" {
			if orTrue.MatchString(code) {
				hit(n, "or-true", line)
			}
			if goTest.MatchString(code) {
				hit(n, "go-test-skip", line)
			}
			if echo.MatchString(code) {
				hit(n, "skip-echo", line)
			}
		}
		if kind == "workflow" {
			if contErr.MatchString(code) {
				hit(n, "continue", line)
			}
			if ifFalse.MatchString(code) {
				hit(n, "if-false", line)
			}
			if branch.MatchString(code) {
				hit(n, "branch-skip", line)
			}
		}
		if skipVar.MatchString(code) {
			hit(n, "skip-var", line)
		}
		if isDriver && verdict.MatchString(code) {
			hit(n, "skip-verdict", line)
		}
	}
	return out, nil
}

// testingTypes are the testing values a Skip can be called on.
var testingTypes = map[string]bool{"T": true, "B": true, "F": true, "TB": true}

func isTestingType(e ast.Expr) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && testingTypes[sel.Sel.Name]
}

func goSkips(path, rel string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", rel, err)
	}
	var out []string
	hit := func(pos token.Pos, rule, what string) {
		out = append(out, fmt.Sprintf("%s:%d: %s: %s", rel, fset.Position(pos).Line, rule, what))
	}
	if strings.HasSuffix(rel, "_test.go") {
		for _, cg := range f.Comments {
			if cg.Pos() > f.Package {
				break
			}
			for _, c := range cg.List {
				if constraint.IsGoBuild(c.Text) {
					expr, err := constraint.Parse(c.Text)
					if err != nil {
						continue
					}
					var bad []string
					expr.Eval(func(tag string) bool {
						if !allowedTag(tag) {
							bad = append(bad, tag)
						}
						return true
					})
					if len(bad) > 0 {
						hit(c.Pos(), "go-build-tag", c.Text)
					}
				}
			}
		}
	}
	// a name declared anywhere in the file with a testing type
	tvals := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			if isTestingType(x.Type) {
				for _, nm := range x.Names {
					tvals[nm.Name] = true
				}
			}
		case *ast.ValueSpec:
			if x.Type != nil && isTestingType(x.Type) {
				for _, nm := range x.Names {
					tvals[nm.Name] = true
				}
			}
		}
		return true
	})
	// a test double: a type that embeds a testing interface or struct
	doubles := map[string]bool{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				for _, fl := range st.Fields.List {
					if len(fl.Names) == 0 && isTestingType(fl.Type) {
						doubles[ts.Name.Name] = true
					}
				}
			}
		}
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		switch fn.Name.Name {
		case "Skip", "Skipf", "SkipNow":
			rt := fn.Recv.List[0].Type
			if s, ok := rt.(*ast.StarExpr); ok {
				rt = s.X
			}
			if id, ok := rt.(*ast.Ident); ok && doubles[id.Name] {
				hit(fn.Pos(), "go-skip", "method "+fn.Name.Name+" on the test double "+id.Name)
			}
		}
	}
	var visitFunc func(params *ast.FieldList, results *ast.FieldList, body *ast.BlockStmt, name string)
	visitFunc = func(params, results *ast.FieldList, body *ast.BlockStmt, name string) {
		takesT := false
		for _, p := range params.List {
			if isTestingType(p.Type) {
				takesT = true
			}
		}
		if !takesT || endsInAFailure(body) {
			return
		}
		for _, st := range body.List {
			ifs, ok := st.(*ast.IfStmt)
			if !ok || ifs.Else != nil || !onlyReturnsZero(ifs.Body) || !readsAGate(ifs) {
				continue
			}
			hit(ifs.Pos(), "go-early-out", name+" returns before its body on a condition: "+string(src[fset.Position(ifs.Cond.Pos()).Offset:fset.Position(ifs.Cond.End()).Offset]))
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, _ := sel.X.(*ast.Ident)
			switch sel.Sel.Name {
			case "Skip", "Skipf", "SkipNow":
				if recv != nil && tvals[recv.Name] {
					hit(x.Pos(), "go-skip", recv.Name+"."+sel.Sel.Name)
				}
			case "Short":
				if recv != nil && recv.Name == "testing" {
					hit(x.Pos(), "go-short", "testing.Short()")
				}
			case "Getenv", "LookupEnv", "Setenv":
				if len(x.Args) > 0 {
					if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, _ := strconv.Unquote(lit.Value); skipVar.MatchString(v) {
							hit(x.Pos(), "go-env-gate", sel.Sel.Name+"("+lit.Value+")")
						}
					}
				}
			}
		case *ast.FuncDecl:
			if x.Body != nil && x.Name.Name != "TestMain" {
				visitFunc(x.Type.Params, x.Type.Results, x.Body, x.Name.Name)
			}
		case *ast.FuncLit:
			visitFunc(x.Type.Params, x.Type.Results, x.Body, "func literal")
		}
		return true
	})
	return out, nil
}

// allowedTag is a build tag that selects a platform, cgo, a Go version or a
// build variant of the product, never a way to leave a test out.
func allowedTag(tag string) bool {
	switch tag {
	case "unix", "cgo", "linux", "darwin", "windows", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos", "aix", "plan9", "js", "wasip1", "android", "ios",
		"amd64", "arm64", "386", "arm", "wasm", "riscv64", "ppc64le", "s390x", "race":
		return true
	}
	return strings.HasPrefix(tag, "go1.") || strings.HasPrefix(tag, "schema_leg_")
}

// onlyReturnsZero is a block that does nothing but return, with no value
// or zero values only (nil, "", 0, false), and fails nothing.
func onlyReturnsZero(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	ret, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, st := range b.List[:len(b.List)-1] {
		es, ok := st.(*ast.ExprStmt)
		if !ok {
			return false
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Log" && sel.Sel.Name != "Logf") {
			return false
		}
	}
	for _, r := range ret.Results {
		switch v := r.(type) {
		case *ast.Ident:
			if v.Name != "nil" && v.Name != "false" {
				return false
			}
		case *ast.BasicLit:
			if v.Value != `""` && v.Value != "0" && v.Value != "``" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// readsAGate is an if whose init or condition reads the environment, looks
// up a tool, stats a path or reads the platform.
func readsAGate(ifs *ast.IfStmt) bool {
	found := false
	look := func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch pkg.Name + "." + sel.Sel.Name {
		case "os.Getenv", "os.LookupEnv", "exec.LookPath", "os.Stat", "os.Lstat", "runtime.GOOS", "runtime.GOARCH", "testing.Short":
			found = true
		}
		return true
	}
	if ifs.Init != nil {
		ast.Inspect(ifs.Init, look)
	}
	ast.Inspect(ifs.Cond, look)
	return found
}

// TestNoSkipMechanismAnywhere: the repository holds no skip mechanism, in
// any language, script, recipe or workflow (see noSkips for every rule).
func TestNoSkipMechanismAnywhere(t *testing.T) {
	t.Parallel()
	found, err := noSkips("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("%d skip mechanisms; nothing below the root decides whether a test runs (Glenn, 2026-11-25: \"NO SKIPS\"). Move what each waited for to the test's tier, or fail naming what is missing:\n  %s", len(found), strings.Join(found, "\n  "))
	}
}

// noSkipPlants is one planted file per rule, and the rule each must raise.
var noSkipPlants = []struct{ file, body, rule string }{
	{"p/a_test.go", "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tt.Skip(\"later\")\n}\n", "go-skip"},
	{"p/b_test.go", "package p\n\nimport \"testing\"\n\nfunc helper(tb testing.TB) { tb.Skipf(\"%s\", \"x\") }\n", "go-skip"},
	{"p/c_test.go", "package p\n\nimport \"testing\"\n\ntype fake struct{ testing.TB }\n\nfunc (f *fake) Skip(args ...any) {}\n", "go-skip"},
	{"p/d_test.go", "package p\n\nimport \"testing\"\n\nfunc TestD(t *testing.T) {\n\tif testing.Short() {\n\t\tt.Fatal(\"x\")\n\t}\n}\n", "go-short"},
	{"p/e_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestE(t *testing.T) {\n\tif os.Getenv(\"SCHEMA_SLOW\") != \"1\" {\n\t\tt.Fatal(\"x\")\n\t}\n}\n", "go-env-gate"},
	{"p/f_test.go", "package p\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestF(t *testing.T) {\n\tif _, err := exec.LookPath(\"cc\"); err != nil {\n\t\treturn\n\t}\n\tt.Log(\"ran\")\n}\n", "go-early-out"},
	{"p/g_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc corpus(t testing.TB) string {\n\tif _, err := os.Stat(\"build/corpus\"); err != nil {\n\t\tt.Log(\"no corpus\")\n\t\treturn \"\"\n\t}\n\treturn \"build/corpus\"\n}\n", "go-early-out"},
	{"p/h_test.go", "//go:build slow\n\npackage p\n\nimport \"testing\"\n\nfunc TestH(t *testing.T) {}\n", "go-build-tag"},
	{"Makefile", "check:\n\tgo test ./... || true\n", "or-true"},
	{"make/x.mk", "x-test:\n\tgo test -skip TestSlow ./p\n", "go-test-skip"},
	{"make/y.mk", "y-test:\n\tSCHEMA_SLOW=1 go test ./p\n", "skip-var"},
	{"scripts/run.sh", "#!/bin/sh\ncommand -v luau >/dev/null || { echo \"luau not found, skipping\"; exit 0; }\n", "skip-echo"},
	{".github/workflows/ci.yml", "jobs:\n  a:\n    steps:\n      - run: make check\n        continue-on-error: true\n", "continue"},
	{".github/workflows/cj.yml", "jobs:\n  a:\n    if: false\n    steps:\n      - run: make check\n", "if-false"},
	{".github/workflows/ck.yml", "jobs:\n  a:\n    if: startsWith(github.head_ref, 'sprint/') == false\n", "branch-skip"},
	{"test/conformance/lua/driver.lua", "if not ok then\n  print(\"SKIP: row \" .. name)\nend\n", "skip-verdict"},
	{"test/hostile/known-skips.tsv", "row-1\twaits\n", "skip-ledger"},
}

// TestNoSkipMechanismIsPlanted plants one skip of each kind in a scratch tree
// and requires each to be named by its file, line and rule; a tree of the
// same files without the skips is clean, so no rule fires on the scaffolding.
func TestNoSkipMechanismIsPlanted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, p := range noSkipPlants {
		path := filepath.Join(root, p.file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(p.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// what no rule may refuse: a wire reader's Skip, a value knob, a tool
	// lookup that fails, docs, testdata, an if on the platform that fails
	clean := map[string]string{
		"q/wire.go":            "package q\n\ntype R struct{}\n\nfunc (r *R) Skip(n int) {}\n\nfunc use(r *R) { r.Skip(3) }\n",
		"q/k_test.go":          "package q\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestK(t *testing.T) {\n\tseed := os.Getenv(\"SCHEMA_SEED\")\n\t_ = seed\n\tif _, err := exec.LookPath(\"cc\"); err != nil {\n\t\tt.Fatalf(\"no cc on PATH\")\n\t}\n}\n",
		"q/l_test.go":          "//go:build linux\n\npackage q\n\nimport \"testing\"\n\nfunc TestL(t *testing.T) {}\n",
		"q/m_test.go":          "//go:build schema_leg_moo\n\npackage q\n\nimport \"testing\"\n\nfunc TestM(t *testing.T) {}\n",
		"docs/RULES.md":        "a test never skips: no t.Skip, no || true\n",
		"q/testdata/x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Skip(\"a planted record for a checker\") }\n",
		"make/z.mk":            "# a comment may say || true and SCHEMA_SLOW to explain the rule\nz-test:\n\tgo test ./q\n",
		"test/c/rows.c":        "/* skip unknown fields on the wire */\nstatic void skip_field(void) {}\n",
	}
	for f, body := range clean {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	found, err := noSkips(root)
	if err != nil {
		t.Fatal(err)
	}
	byFile := map[string][]string{}
	for _, h := range found {
		file, _, _ := strings.Cut(h, ":")
		byFile[file] = append(byFile[file], h)
	}
	for _, p := range noSkipPlants {
		hits := byFile[p.file]
		ok := false
		for _, h := range hits {
			if strings.Contains(h, ": "+p.rule+": ") {
				ok = true
			}
		}
		if !ok {
			t.Errorf("the planted %s in %s was not found (found there: %v)", p.rule, p.file, hits)
		}
		delete(byFile, p.file)
	}
	for f, hits := range byFile {
		t.Errorf("%s holds no skip, and the scan named it: %v", f, hits)
	}
}

// endsInAFailure is a helper that returns early when what it requires is
// there and fails at its end when it is not (t18RequireJava's shape): its
// early return is the found case, not a skip.
func endsInAFailure(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	es, ok := b.List[len(b.List)-1].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Fatal", "Fatalf", "FailNow":
		return true
	}
	return false
}

// noSkipsExempt are the files the scan does not read, each with its reason.
// TestNoSkipExemptionsAreCurrent holds every one to a file that exists.
var noSkipsExempt = map[string]string{
	// a client-side push guard, not a test: it refuses an agent clone's
	// push of a commit not authored by the agent, and by design does nothing
	// in a clone that is not an agent's (maintainers, outside contributors)
	".githooks/pre-push": "a push guard scoped to agent clones, not a test",
	// the nightly-readiness checker's own unit test: its skip lines, skip
	// actions and skip variables are the plants it requires the checker to
	// refuse or strip
	"test/conformance/nightly_readiness_test.py": "plants for a checker that refuses skips",
}

// TestNoSkipExemptionsAreCurrent: every exempt file exists, so an exemption
// cannot outlive its file and quietly cover a new one of the same name.
func TestNoSkipExemptionsAreCurrent(t *testing.T) {
	t.Parallel()
	for f, why := range noSkipsExempt {
		if why == "" {
			t.Errorf("%s is exempt with no reason", f)
		}
		if _, err := os.Stat(filepath.Join("..", "..", f)); err != nil {
			t.Errorf("%s is exempt from the skip scan and does not exist: drop it (%v)", f, err)
		}
	}
}
