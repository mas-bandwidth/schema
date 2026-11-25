package testreg

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/version"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
//	go-skip        a Skip, Skipf or SkipNow call on a testing value (under
//	               whatever name the file imports testing as, or a name it
//	               was given), or taken as a method value; a method of
//	               that name declared on a test double, or in a _test.go file
//	               any such call shaped like testing's (no argument, or a
//	               message), whatever it is called on
//	go-short       testing.Short()
//	go-env-gate    an environment read, set or lookup of a skip or lane
//	               variable (SCHEMA_SLOW, SCHEMA_REQUIRE_*, *SKIP*, ALLOC_ANY)
//	go-early-out   in a function that takes a testing value, an if that reads
//	               the environment, looks up a tool, stats a file, or reads
//	               the platform, and then only returns (nothing, or zero
//	               values) without failing: the body after it does not run;
//	               nested ifs too, and an if on the world outside the tree
//	               that only continues its loop; with an else or without; a
//	               print or log before the return is still one; a helper's
//	               found-case return before a failure at its end is not one,
//	               a Test's is; a TestMain that only prints and exits 0 or
//	               returns on such an if, or never calls m.Run
//	go-build-tag   a build constraint (//go:build or // +build) on a _test.go
//	               file other than the platform, cgo, race, a Go release this
//	               toolchain is at or past, or the schema_leg_<leg> build
//	               variant of a leg with a make/<leg>.mk
//	or-true        `|| true`, `|| :`, `|| exit 0` in a script, recipe or step,
//	               and a test run's `|| echo`
//	set-plus-e     `set +e` in a script
//	inline-select  `-run "$(...)"`: a selection that fails runs everything
//	make-probe     a recipe chosen by $(if $(shell command -v ...))
//	continue       `continue-on-error` in a workflow
//	if-false       `if: false` in a workflow
//	branch-skip    a workflow condition on the branch name (sprint/)
//	go-test-skip   `go test` with -skip, -test.skip or -short, and a compiled
//	               test binary run with -test.skip or -test.short
//	skip-var       a skip or lane variable named in a script, recipe or step
//	skip-echo      a script, recipe or step that prints that it skips
//	skip-verdict   a harness driver (any language) that reports a row as
//	               skipped instead of passing or failing it
//	skip-ledger    a file that lists skips (known-skips.tsv, skip-ledger.txt)
//	missing-exit0  `exit 0` on, or the line after, a probe for a tool
//	make-ignore    a recipe line under make's `-`, or .IGNORE
//	if-var         a workflow condition on vars. or env.
//	native-skip    a test framework's own skip in a driver (it.skip, xit,
//	               #[ignore], Skip = "...", @tag :skip, unittest.skip,
//	               pytest.mark.skip, @Disabled, @Ignore, skip_if, skipTest)
//
// A driver is a source file in a harness language anywhere in the tree, or
// a script whose first line runs a harness language's interpreter.
//
// Not scanned, each for its reason: version control and dependency and build
// output (never source); third_party/ (vendored sibling code, owned by its
// own repository); working/ (scratch); docs (prose about the rule, and
// history); and the files noSkipsExempt names, each with its reason. A
// testdata/ directory is scanned like any other, since driver sources live
// under some of them; a planted fixture there is named in noSkipsExempt.
// Inside this file, the plants are strings.
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
	case ".git", "node_modules", "_build", "deps", "target", ".dart_tool", "__pycache__", ".venv", ".gradle", "obj":
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
	// a variable named for skipping, set or read in a script
	skipAssign = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*SKIP[A-Z0-9_]*=|\$\{?[A-Z][A-Z0-9_]*SKIP[A-Z0-9_]*\b|^\s*-?\s*[A-Z][A-Z0-9_]*SKIP[A-Z0-9_]*\s*:`)
	// success on a missing tool: `exit 0` beside a probe for one
	exitZero  = regexp.MustCompile(`\bexit\s+0\b`)
	toolProbe = regexp.MustCompile(`\bcommand\s+-v\b|\bwhich\s|\bhash\s|\btype\s+-[pP]\b|\[\s+!\s+-[xfed]\s`)
	// make's ignore-errors: a recipe line under `-`, or .IGNORE
	makeIgnore = regexp.MustCompile(`^\t[@+]*-[^-]|^\.IGNORE\s*:`)
	// a workflow job or step run or not by a variable
	ifVar = regexp.MustCompile(`^\s*-?\s*if\s*:.*\b(vars|env)\.`)
	// a test run's failure turned into a message: `go test ... || echo ...`
	// with no exit after
	orEcho = regexp.MustCompile(`\b(go\s+test|ctest|cargo\s+test|dotnet\s+test|npm\s+test|pytest|mix\s+test|dart\s+test)\b[^|]*\|\|\s*(echo|printf)\b[^;}]*$`)
	// errors no longer stop the script
	setPlusE = regexp.MustCompile(`(^|[;&|]\s*)set\s+\+[a-zA-Z]*e`)
	// a compiled test binary told to skip
	binSkip = regexp.MustCompile(`\s-test\.(skip|short)\b`)
	// a selection computed inline: if it fails, go test runs everything
	inlineSelect = regexp.MustCompile(`-run\s+"\$\$?\(`)
	// a recipe chosen by whether a tool is on PATH
	makeProbe = regexp.MustCompile(`\$\(if\s+\$\(shell\s+(command\s+-v|which)\b`)
	// a script run by a harness language's interpreter
	driverShebang = regexp.MustCompile(`python|node|ruby|perl|lua|elixir|dart|deno|bun|php`)
	// a test framework's own skip
	nativeSkip = regexp.MustCompile(`\b(it|test|describe|context)\.skip\b|\bx(it|describe|test)\s*\(|#\[ignore\b|\bSkip\s*=\s*"|@tag\s+:?skip\b|@moduletag\s+:?skip\b|\bunittest\.skip|\bpytest\.mark\.skip|@Disabled\b|@Ignore\b|\bskip_if\b|\bskipTest\(`)
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
	head := data[:min(len(data), 128)]
	kind := scriptKind(rel, head)
	firstLine, _, _ := bytes.Cut(head, []byte("\n"))
	isDriver := driverExt[ext] || (bytes.HasPrefix(firstLine, []byte("#!")) && driverShebang.Match(firstLine))
	if kind == "" && !isDriver {
		return nil, nil
	}
	prev := ""
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
			// a missing tool answered by success, on the line or the one after
			// the probe for it
			if exitZero.MatchString(code) && (toolProbe.MatchString(code) || toolProbe.MatchString(prev)) && !strings.HasPrefix(strings.TrimLeft(code, "\t @+-"), "#") {
				hit(n, "missing-exit0", line)
			}
			if orEcho.MatchString(code) {
				hit(n, "or-true", line)
			}
			if setPlusE.MatchString(code) {
				hit(n, "set-plus-e", line)
			}
			if binSkip.MatchString(code) {
				hit(n, "go-test-skip", line)
			}
			if inlineSelect.MatchString(code) {
				hit(n, "inline-select", line)
			}
			if skipAssign.MatchString(code) {
				hit(n, "skip-var", line)
			}
		}
		if kind == "make" && makeIgnore.MatchString(code) && !strings.HasSuffix(strings.TrimRight(prev, " \t"), "\\") {
			hit(n, "make-ignore", line)
		}
		if kind == "make" && makeProbe.MatchString(code) {
			hit(n, "make-probe", line)
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
			if ifVar.MatchString(code) {
				hit(n, "if-var", line)
			}
		}
		if skipVar.MatchString(code) {
			hit(n, "skip-var", line)
		}
		if isDriver && verdict.MatchString(code) {
			hit(n, "skip-verdict", line)
		}
		if isDriver && nativeSkip.MatchString(code) {
			hit(n, "native-skip", line)
		}
		if strings.TrimSpace(line) != "" {
			prev = line
		}
	}
	return out, nil
}

// testingTypes are the testing values a Skip can be called on.
var testingTypes = map[string]bool{"T": true, "B": true, "F": true, "TB": true}

// isTestingType is *testing.T and its kin, under the name the file imports
// the testing package as.
func isTestingType(e ast.Expr, testingName string) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == testingName && testingTypes[sel.Sel.Name]
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
	testingName := "testing"
	for _, im := range f.Imports {
		if im.Path.Value == `"testing"` && im.Name != nil {
			testingName = im.Name.Name
		}
	}
	isTesting := func(e ast.Expr) bool { return isTestingType(e, testingName) }
	root := strings.TrimSuffix(path, filepath.FromSlash(rel))
	if strings.HasSuffix(rel, "_test.go") {
		for _, cg := range f.Comments {
			if cg.Pos() > f.Package {
				break
			}
			for _, c := range cg.List {
				if constraint.IsGoBuild(c.Text) || constraint.IsPlusBuild(c.Text) {
					expr, err := constraint.Parse(c.Text)
					if err != nil {
						continue
					}
					var bad []string
					expr.Eval(func(tag string) bool {
						if !allowedTag(tag, root) {
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
			if isTesting(x.Type) {
				for _, nm := range x.Names {
					tvals[nm.Name] = true
				}
			}
		case *ast.ValueSpec:
			if x.Type != nil && isTesting(x.Type) {
				for _, nm := range x.Names {
					tvals[nm.Name] = true
				}
			}
		}
		return true
	})
	// and a name given one of those: u := t, var u = t
	for range 2 {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, r := range x.Rhs {
					if id, ok := r.(*ast.Ident); ok && tvals[id.Name] && i < len(x.Lhs) {
						if l, ok := x.Lhs[i].(*ast.Ident); ok {
							tvals[l.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				for i, r := range x.Values {
					if id, ok := r.(*ast.Ident); ok && tvals[id.Name] && i < len(x.Names) {
						tvals[x.Names[i].Name] = true
					}
				}
			}
			return true
		})
	}
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
					if len(fl.Names) == 0 && isTesting(fl.Type) {
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
			if isTesting(p.Type) {
				takesT = true
			}
		}
		// a helper that returns when what it needs is there and fails at
		// its end when it is not (t18RequireJava's shape) skips nothing; a
		// Test's early return is a skip whatever follows it
		if !takesT || (!strings.HasPrefix(name, "Test") && endsInAFailure(body)) {
			return
		}
		// every if in the body, nested too (a func literal is its own visit):
		// a gate answered by a bare return, or by continue in a loop
		ast.Inspect(body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			ifs, ok := n.(*ast.IfStmt)
			if !ok || !readsAGate(ifs) {
				return true
			}
			cond := string(src[fset.Position(ifs.Cond.Pos()).Offset:fset.Position(ifs.Cond.End()).Offset])
			switch {
			case onlyReturnsZero(ifs.Body):
				hit(ifs.Pos(), "go-early-out", name+" returns before its body on a condition: "+cond)
			case onlyContinues(ifs.Body) && readsAnOutsideGate(ifs):
				hit(ifs.Pos(), "go-early-out", name+" passes over its work on a condition: "+cond)
			}
			return true
		})
	}
	// TestMain that exits 0 on a gate, having done nothing but say so, runs
	// no test of the package (a helper process that answers its parent and
	// exits does work first, and is not this)
	testMainExits := func(fn *ast.FuncDecl) {
		body := fn.Body
		// TestMain runs the tests only through m.Run
		runs := false
		if ps := fn.Type.Params.List; len(ps) == 1 && len(ps[0].Names) == 1 {
			m := ps[0].Names[0].Name
			ast.Inspect(body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == m {
							runs = true
						}
					}
				}
				return true
			})
		}
		if !runs {
			hit(fn.Pos(), "go-early-out", "TestMain never calls m.Run, running no test")
		}
		for _, st := range body.List {
			ifs, ok := st.(*ast.IfStmt)
			if !ok || !readsAGate(ifs) || len(ifs.Body.List) == 0 {
				continue
			}
			last := ifs.Body.List[len(ifs.Body.List)-1]
			if _, ok := last.(*ast.ReturnStmt); ok && onlyReturnsZero(ifs.Body) {
				hit(ifs.Pos(), "go-early-out", "TestMain returns on a condition, running no test")
				continue
			}
			es, ok := last.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			lit, isLit := call.Args[0].(*ast.BasicLit)
			if !ok || !isLit || lit.Value != "0" || sel.Sel.Name != "Exit" {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
				continue
			}
			says := true
			for _, before := range ifs.Body.List[:len(ifs.Body.List)-1] {
				es, ok := before.(*ast.ExprStmt)
				if !ok {
					says = false
					break
				}
				c, isCall := es.X.(*ast.CallExpr)
				if !isCall {
					says = false
					break
				}
				s, ok := c.Fun.(*ast.SelectorExpr)
				if !ok || !strings.HasPrefix(s.Sel.Name, "Print") && !strings.HasPrefix(s.Sel.Name, "Fprint") {
					says = false
				}
			}
			if says {
				hit(call.Pos(), "go-early-out", "TestMain exits 0 on a condition, running no test")
			}
		}
	}
	// a Skip taken as a method value (stop := t.SkipNow), not called here
	called := map[*ast.SelectorExpr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				called[sel] = true
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && !called[sel] {
			if recv, ok := sel.X.(*ast.Ident); ok && tvals[recv.Name] {
				switch sel.Sel.Name {
				case "Skip", "Skipf", "SkipNow":
					hit(sel.Pos(), "go-skip", recv.Name+"."+sel.Sel.Name+" as a value")
				}
			}
		}
		return true
	})
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
				switch {
				case recv != nil && tvals[recv.Name]:
					hit(x.Pos(), "go-skip", recv.Name+"."+sel.Sel.Name)
				case strings.HasSuffix(rel, "_test.go") && skipLikeArgs(sel.Sel.Name, x.Args):
					// through a field, a conversion or a name this file does
					// not declare: a Skip with no arguments or a message is a
					// test's, never a wire reader's Skip(n)
					hit(x.Pos(), "go-skip", string(src[fset.Position(sel.Pos()).Offset:fset.Position(sel.End()).Offset]))
				}
			case "Short":
				if recv != nil && recv.Name == testingName {
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
			if x.Body != nil && x.Name.Name == "TestMain" && x.Recv == nil {
				testMainExits(x)
			} else if x.Body != nil {
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
// allowedTag is a platform, cgo, race, a Go release this toolchain is at or
// past, or the build variant of a leg the tree has (make/<leg>.mk): never a
// tag no build sets.
func allowedTag(tag, root string) bool {
	switch tag {
	case "unix", "cgo", "linux", "darwin", "windows", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos", "aix", "plan9", "js", "wasip1", "android", "ios",
		"amd64", "arm64", "386", "arm", "wasm", "riscv64", "ppc64le", "s390x", "race":
		return true
	}
	if strings.HasPrefix(tag, "go1.") {
		return version.IsValid(tag) && version.Compare(tag, version.Lang(runtime.Version())) <= 0
	}
	if leg, ok := strings.CutPrefix(tag, "schema_leg_"); ok && leg != "" {
		_, err := os.Stat(filepath.Join(root, "make", leg+".mk"))
		return err == nil
	}
	return false
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
		if !ok || !saysWhy(sel.Sel.Name) {
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
// onlyContinues is a block that logs at most and goes on to the next turn
// of its loop.
func onlyContinues(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	br, ok := b.List[len(b.List)-1].(*ast.BranchStmt)
	if !ok || br.Tok != token.CONTINUE {
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
		if !ok || !saysWhy(sel.Sel.Name) {
			return false
		}
	}
	return true
}

// saysWhy is a call that only reports: a test log or a print.
func saysWhy(name string) bool {
	return name == "Log" || name == "Logf" || strings.HasPrefix(name, "Print") || strings.HasPrefix(name, "Fprint")
}

// skipLikeArgs is a call shaped like testing's: SkipNow(), Skip(), Skip of a
// message, Skipf of a format; never Skip(n) on a reader.
func skipLikeArgs(name string, args []ast.Expr) bool {
	if len(args) == 0 {
		return true
	}
	lit, ok := args[0].(*ast.BasicLit)
	if ok && lit.Kind == token.STRING {
		return true
	}
	return name == "Skipf"
}

// readsAnOutsideGate is a gate on the world outside the tree: a variable,
// a tool on PATH, the platform, -short. A loop that passes over a file the
// tree does not hold is filtering its data, not skipping a test.
func readsAnOutsideGate(ifs *ast.IfStmt) bool {
	found := false
	look := func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok {
			switch pkg.Name + "." + sel.Sel.Name {
			case "os.Getenv", "os.LookupEnv", "exec.LookPath", "runtime.GOOS", "runtime.GOARCH", "testing.Short":
				found = true
			}
		}
		return true
	}
	if ifs.Init != nil {
		ast.Inspect(ifs.Init, look)
	}
	ast.Inspect(ifs.Cond, look)
	return found
}

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
	// a driver under a testdata/ directory is scanned like any other source
	{"test/conformance/packet/testdata/drivers/run.sh", "#!/bin/sh\n./driver \"$@\" || true\n", "or-true"},
	// testing imported under another name
	{"p/i_test.go", "package p\n\nimport tt \"testing\"\n\nfunc TestI(t *tt.T) {\n\tt.Skip(\"later\")\n}\n", "go-skip"},
	// a Skip reached through a field
	{"p/j_test.go", "package p\n\nimport \"testing\"\n\ntype env struct{ tb testing.TB }\n\nfunc (e env) need() {\n\te.tb.SkipNow()\n}\n", "go-skip"},
	// TestMain that runs nothing on a gate
	{"p/k_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) {\n\tif os.Getenv(\"CI\") == \"\" {\n\t\tos.Exit(0)\n\t}\n\tos.Exit(m.Run())\n}\n", "go-early-out"},
	// a gate nested in the body, answered by continue
	{"p/l_test.go", "package p\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestL(t *testing.T) {\n\tfor _, tool := range []string{\"cc\"} {\n\t\tif _, err := exec.LookPath(tool); err != nil {\n\t\t\tcontinue\n\t\t}\n\t\tt.Log(tool)\n\t}\n}\n", "go-early-out"},
	// a Test returning early whatever it ends with
	{"p/m_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestM(t *testing.T) {\n\tif os.Getenv(\"CI\") == \"\" {\n\t\treturn\n\t}\n\tt.Fatal(\"x\")\n}\n", "go-early-out"},
	{"make/w.mk", "w-test:\n\t-go test ./p\n", "make-ignore"},
	{"make/v.mk", ".IGNORE: v-test\nv-test:\n\tgo test ./p\n", "make-ignore"},
	{"scripts/probe.sh", "#!/bin/sh\nif ! command -v luau >/dev/null; then\n  exit 0\nfi\n", "missing-exit0"},
	{"scripts/vars.sh", "#!/bin/sh\nRUN_SKIP_SLOW=1 ./drive\n", "skip-var"},
	{"scripts/check", "#!/usr/bin/env python3\nprint(\"SKIP: no tool\")\n", "skip-verdict"},
	{"runtime/js/x.test.js", "it.skip(\"slow\", () => {});\n", "native-skip"},
	{"runtime/rust/lib.rs", "#[test]\n#[ignore]\nfn slow() {}\n", "native-skip"},
	{"runtime/py/t.py", "import unittest\n@unittest.skip(\"slow\")\ndef test_x(): pass\n", "native-skip"},
	{".github/workflows/cl.yml", "jobs:\n  a:\n    if: vars.RUN_SLOW == 'true'\n", "if-var"},
	// an if with an else, and a print before the return
	{"p/n_test.go", "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestN(t *testing.T) {\n\tif os.Getenv(\"CI\") == \"\" {\n\t\tfmt.Println(\"not CI\")\n\t\treturn\n\t} else {\n\t\tt.Log(\"CI\")\n\t}\n}\n", "go-early-out"},
	// a testing value under another name, and a Skip taken as a value
	{"p/o_test.go", "package p\n\nimport \"testing\"\n\nfunc TestO(t *testing.T) {\n\tu := t\n\tu.Skip(t.Name())\n}\n", "go-skip"},
	{"p/q_test.go", "package p\n\nimport \"testing\"\n\nfunc TestQ(t *testing.T) {\n\tstop := t.SkipNow\n\tstop()\n}\n", "go-skip"},
	// TestMain that never runs the tests
	{"p/r_test.go", "package p\n\nimport \"testing\"\n\nfunc TestMain(m *testing.M) {}\n", "go-early-out"},
	// tags no build sets
	{"p/s_test.go", "//go:build go1.99\n\npackage p\n", "go-build-tag"},
	{"p/u_test.go", "//go:build schema_leg_never\n\npackage p\n", "go-build-tag"},
	{"p/v_test.go", "// +build slow\n\npackage p\n", "go-build-tag"},
	{"make/u.mk", "u-test:\n\tgo test ./p || echo flaky\n", "or-true"},
	{"make/t.mk", "t-test:\n\t\x24(if \x24(shell command -v luau),go test ./p,@true)\n", "make-probe"},
	{"make/s.mk", "s-test:\n\tgo test -c -o p.test ./p && ./p.test -test.skip TestSlow\n", "go-test-skip"},
	{"make/r.mk", "r-test:\n\tgo test -run \"\x24\x24(go run ./tools/gotestselect -not X ./p)\" ./p\n", "inline-select"},
	{"scripts/e.sh", "#!/bin/sh\nset +e\ngo test ./p\nexit 0\n", "set-plus-e"},
	{"scripts/node-driver", "#!/usr/bin/env node\nconsole.log(\"SKIP: no tool\")\n", "skip-verdict"},
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
	// lookup that fails, docs, an if on the platform that fails
	clean := map[string]string{
		"q/wire.go":     "package q\n\ntype R struct{}\n\nfunc (r *R) Skip(n int) {}\n\nfunc use(r *R) { r.Skip(3) }\n",
		"q/k_test.go":   "package q\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestK(t *testing.T) {\n\tseed := os.Getenv(\"SCHEMA_SEED\")\n\t_ = seed\n\tif _, err := exec.LookPath(\"cc\"); err != nil {\n\t\tt.Fatalf(\"no cc on PATH\")\n\t}\n}\n",
		"q/l_test.go":   "//go:build linux\n\npackage q\n\nimport \"testing\"\n\nfunc TestL(t *testing.T) {}\n",
		"q/m_test.go":   "//go:build schema_leg_moo\n\npackage q\n\nimport \"testing\"\n\nfunc TestM(t *testing.T) {}\n",
		"make/moo.mk":   "moo-test:\n\tgo test -tags schema_leg_moo ./q\n",
		"docs/RULES.md": "a test never skips: no t.Skip, no || true\n",
		"make/z.mk":     "# a comment may say || true and SCHEMA_SLOW to explain the rule\nz-test:\n\tgo test ./q\n",
		"test/c/rows.c": "/* skip unknown fields on the wire */\nstatic void skip_field(void) {}\n",
		"q/n_test.go":   "package q\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc needCC(t testing.TB) {\n\tif _, err := exec.LookPath(\"cc\"); err == nil {\n\t\treturn\n\t}\n\tt.Fatal(\"cc is not on PATH\")\n}\n",
		"q/o_test.go":   "package q\n\nimport \"testing\"\n\ntype rd struct{}\n\nfunc (rd) Skip(n int) {}\n\nfunc TestO(t *testing.T) { var r rd; r.Skip(4) }\n",
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
	// the checker itself: it reads `go test -json` actions, and a "skip"
	// action is an error it reports; it strips SCHEMA_SKIP_LEGS from the
	// environment it runs make under
	"test/conformance/nightly-readiness": "a checker that refuses skips, naming the action it refuses",
	// tools/gotestreport's record corpus: the real `go test -json` run of a
	// test that skips once is what gotestreport must refuse (F-175)
	"tools/gotestreport/testdata/recordgen/ledgertest/ledger_test.go": "a planted skip record gotestreport refuses",
	// internal/recipescan's planted recipes: a lock cleanup and a fixture
	// driver the scanner reads as text, never run
	"internal/recipescan/testdata/stamp-correct.mk":                     "a planted recipe the recipe scanner reads, never run",
	"internal/recipescan/testdata/stamp-touch-lost.mk":                  "a planted recipe the recipe scanner reads, never run",
	"internal/recipescan/testdata/tree/test/conformance/fixture/driver": "a planted driver the recipe scanner reads, never run",
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
