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
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
//	               returns on such an if, or never calls m.Run; a switch on
//	               such a condition with a case that only returns; the
//	               condition read through a helper of the package, or of a
//	               package of this module, that reads one (if !have("luau"));
//	               and the rest of the body under such an if (if have
//	               { body } last, no else), unless that body only reports a
//	               check's failures or logs, or is a golden rewrite after the
//	               comparison (slowtest.Rewriting)
//	go-skip-print  in test code or under test/, a print or test log whose text
//	               announces a skip (skipped, skipping, SKIP), or a formatted
//	               line saying so handed back to be printed, unless it is a
//	               failure's message or a failure follows it; a line built
//	               into a buffer (&b) is a program or a report, not a notice
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
//	repo-gate      a workflow condition comparing github.repository (or
//	               _owner) with a literal: false here, or false on a fork
//	native-skip    a test framework's own skip in a driver (it.skip, xit,
//	               #[ignore], Skip = "...", @tag :skip, unittest.skip,
//	               pytest.mark.skip, @Disabled, @Ignore, skip_if, skipTest)
//
// A driver is a source file in a harness language anywhere in the tree, or
// a script whose first line runs a harness language's interpreter.
//
// Not scanned, each for its reason: version control, and dependency and build
// output known by its directory's name (node_modules, target, obj, deps and
// the like) only while the checkout tracks no file under it, so a tracked
// package in a directory of such a name is scanned (outside a checkout, as in
// the planted tree, every such directory is scanned); third_party/ (vendored
// sibling code, owned by its own repository); working/ (scratch); docs
// (prose about the rule, and history); and the files noSkipsExempt names,
// each with its reason. A
// testdata/ directory is scanned like any other, since driver sources live
// under some of them; a planted fixture there is named in noSkipsExempt.
// Inside this file, the plants are strings.
func noSkips(root string) ([]string, error) {
	tracked, trackedFiles, err := trackedDirs(root)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && noSkipsSkipsDir(rel, d.Name(), tracked) {
				return filepath.SkipDir
			}
			return nil
		}
		// under a directory named like build output that the checkout
		// tracks files in, only the tracked files are source: what sits
		// beside them untracked is that output
		if trackedFiles != nil && !trackedFiles[rel] && underOutputName(rel) {
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	probes, err := goProbeIndex(root, files)
	if err != nil {
		return nil, err
	}
	found := make([][]string, len(files))
	errs := make([]error, len(files))
	eachParallel(len(files), func(i int) {
		found[i], errs[i] = noSkipsFile(filepath.Join(root, filepath.FromSlash(files[i])), files[i], probes)
	})
	var out []string
	for i := range files {
		if errs[i] != nil {
			return nil, errs[i]
		}
		out = append(out, found[i]...)
	}
	sort.Strings(out)
	return out, nil
}

// eachParallel runs f(0..n-1) across the processors this test may use.
func eachParallel(n int, f func(int)) {
	next := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				f(i)
			}
		}()
	}
	for i := range n {
		next <- i
	}
	close(next)
	wg.Wait()
}

// trackedDirs is every directory that holds a file the repository at root
// tracks, or nil when root is not a checkout (a planted scratch tree, where
// every directory is scanned). A checkout whose index git cannot list is an
// error, never a scan that quietly prunes more.
func trackedDirs(root string) (dirs, files map[string]bool, err error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("listing the files %s tracks: %w", root, err)
	}
	dirs, files = map[string]bool{}, map[string]bool{}
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" {
			continue
		}
		files[f] = true
		for d := path.Dir(f); d != "." && !dirs[d]; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	if len(dirs) == 0 {
		return nil, nil, fmt.Errorf("%s is a checkout that tracks no file in a directory", root)
	}
	return dirs, files, nil
}

// gitEnv is the environment without git's own location variables, so a run
// from inside a git hook (which sets GIT_DIR and GIT_INDEX_FILE) reads the
// checkout it names with -C, never the hook's repository.
func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return env
}

// outputNames are the directory names build and dependency output goes in.
var outputNames = map[string]bool{"node_modules": true, "_build": true, "deps": true, "target": true, ".dart_tool": true, "__pycache__": true, ".venv": true, ".gradle": true, "obj": true}

// underOutputName is a path with a directory named like build output.
func underOutputName(rel string) bool {
	for d := path.Dir(rel); d != "."; d = path.Dir(d) {
		if outputNames[path.Base(d)] {
			return true
		}
	}
	return false
}

// noSkipsSkipsDir is a directory that holds no source of this repository's
// tests, or data only (the reasons are on noSkips). A dependency or build
// output directory is known by its name only while it holds no tracked
// file: a tracked package under a directory named target, obj or deps is
// source, and is scanned.
func noSkipsSkipsDir(rel, name string, tracked map[string]bool) bool {
	if name == ".git" {
		return true
	}
	if outputNames[name] {
		return tracked != nil && !tracked[rel]
	}
	switch rel {
	case "build", "working", "third_party", "bin", "dist", "docs":
		return true
	}
	return false
}

var (
	// a skip or lane variable: what a gate read, or a lane promised.
	skipVar = regexp.MustCompile(`\b(SCHEMA_SLOW|SCHEMA_REQUIRE_[A-Z0-9_]+|SCHEMA_SKIP[A-Z0-9_]*|SKIP_SLOW_OK|SCHEMA_[A-Z0-9_]*ALLOC_ANY[A-Z0-9_]*)\b`)
	orTrue  = regexp.MustCompile(`\|\|\s*(true|:|exit\s+0)\b|\|\|\s*:\s*$|\|\|\s*:\s*[;)]`)
	contErr = regexp.MustCompile(`^\s*-?\s*continue-on-error\s*:`)
	ifFalse = regexp.MustCompile(`^\s*-?\s*if\s*:\s*(false|\$\{\{\s*false\s*\}\}|0)\s*$`)
	branch  = regexp.MustCompile(`startsWith\(\s*github\.(head_ref|ref_name|ref)\s*,\s*'(refs/heads/)?sprint/'\s*\)`)
	goTest  = regexp.MustCompile(`\bgo\s+test\b.*\s-(skip|test\.skip|short)(=|\s|$)`)
	// an announcement that something is being skipped; a message that a
	// skip happened and is a failure ("a test SKIPPED", exit 1) is a refusal.
	echo = regexp.MustCompile(`\b(echo|printf)\b.*(\b(?i:skipping)\b|\b(?i:skip):)`)
	// a driver's verdict word for a row it did not run.
	verdict = regexp.MustCompile(`["'](SKIP|SKIPPED|skip|skipped)["']|["'](?i:skipping|skipped)\b[^"']*["']|\bSKIP:`)
	ledger  = regexp.MustCompile(`(?i)(^|[-_/])(known-)?skips?(-ledger)?\.(tsv|txt)$`)
	// a variable named for skipping, set or read in a script.
	skipAssign = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*SKIP[A-Z0-9_]*=|\$\{?[A-Z][A-Z0-9_]*SKIP[A-Z0-9_]*\b|^\s*-?\s*[A-Z][A-Z0-9_]*SKIP[A-Z0-9_]*\s*:`)
	// success on a missing tool: `exit 0` beside a probe for one.
	exitZero  = regexp.MustCompile(`\bexit\s+0\b`)
	toolProbe = regexp.MustCompile(`\bcommand\s+-v\b|\bwhich\s|\bhash\s|\btype\s+-[pP]\b|\[\s+!\s+-[xfed]\s`)
	// make's ignore-errors: a recipe line under `-`, or .IGNORE.
	makeIgnore = regexp.MustCompile(`^\t[@+]*-[^-]|^\.IGNORE\s*:`)
	// a workflow job or step run or not by a variable.
	ifVar = regexp.MustCompile(`^\s*-?\s*if\s*:.*\b(vars|env)\.`)
	// a test run's failure turned into a message: `go test ... || echo ...`
	// with no exit after.
	orEcho = regexp.MustCompile(`\b(go\s+test|ctest|cargo\s+test|dotnet\s+test|npm\s+test|pytest|mix\s+test|dart\s+test)\b[^|]*\|\|\s*(echo|printf)\b[^;}]*$`)
	// errors no longer stop the script.
	setPlusE = regexp.MustCompile(`(^|[;&|]\s*)set\s+\+[a-zA-Z]*e`)
	// a compiled test binary told to skip.
	binSkip = regexp.MustCompile(`\s-test\.(skip|short)\b`)
	// a selection computed inline: if it fails, go test runs everything.
	inlineSelect = regexp.MustCompile(`-run\s+"\$\$?\(`)
	// a recipe chosen by whether a tool is on PATH.
	makeProbe = regexp.MustCompile(`\$\(if\s+\$\(shell\s+(command\s+-v|which)\b`)
	// a script run by a harness language's interpreter.
	driverShebang = regexp.MustCompile(`python|node|ruby|perl|lua|elixir|dart|deno|bun|php`)
	// a workflow job or step run or not by which repository it is in: a
	// name compared with a literal is false on this repository or on a fork.
	repoGate = regexp.MustCompile(`\bgithub\.` + repoName + `\s*[!=]=\s*['"]|['"]\s*[!=]=\s*github\.` + repoName + `|\bgithub\.` + repoName + `\s*,\s*['"]|contains\(\s*['"][^'"]*['"]\s*,\s*github\.` + repoName)
	// the repository a run is in, by any of the names a workflow reads it by.
	repoName = `(repository(_owner)?|event\.repository\.(full_name|name|owner\.login))\b`
	// a Go string that announces a skip.
	goSkipNotice = regexp.MustCompile(`\b(?i:skipp(?:ed|ing))\b|\bSKIP(PED)?\b`)
	goSkipWords  = regexp.MustCompile(`\b(?i:skipp(?:ed|ing))\b`)
	// a test framework's own skip.
	nativeSkip = regexp.MustCompile(`\b(it|test|describe|context)\.skip\b|\bx(it|describe|test)\s*\(|#\[ignore\b|\bSkip\s*=\s*"|@tag\s+:?skip\b|@moduletag\s+:?skip\b|\bunittest\.skip|\bpytest\.mark\.skip|@Disabled\b|@Ignore\b|\bskip_if\b|\bskipTest\(`)
)

// script-like files: their lines are commands.
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

func noSkipsFile(path, rel string, probes *probeIndex) ([]string, error) {
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
		return goSkips(path, rel, probes)
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
			if repoGate.MatchString(code) {
				hit(n, "repo-gate", line)
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

func goSkips(path, rel string, probes *probeIndex) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
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
	g := newGateReader(probes, f, rel)
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
	visitFunc := func(params, results *ast.FieldList, body *ast.BlockStmt, name string) {
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
		text := func(n ast.Node) string {
			return string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset])
		}
		// every if and switch in the body, nested too (a func literal is its
		// own visit): a gate answered by a bare return, or by continue in a
		// loop; the gate read directly or through a probe
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.IfStmt:
				if !g.ifReads(x, probeGate) {
					return true
				}
				switch {
				case onlyReturnsZero(x.Body):
					hit(x.Pos(), "go-early-out", name+" returns before its body on a condition: "+text(x.Cond))
				case onlyContinues(x.Body) && g.ifReads(x, probeOutside):
					hit(x.Pos(), "go-early-out", name+" passes over its work on a condition: "+text(x.Cond))
				}
			case *ast.SwitchStmt:
				if !g.switchReads(x, probeGate) {
					return true
				}
				outside := g.switchReads(x, probeOutside)
				for _, c := range x.Body.List {
					cc := c.(*ast.CaseClause)
					blk := &ast.BlockStmt{List: cc.Body}
					switch {
					case onlyReturnsZero(blk):
						hit(cc.Pos(), "go-early-out", name+" returns before its body on a case of a switch on a condition")
					case onlyContinues(blk) && outside:
						hit(cc.Pos(), "go-early-out", name+" passes over its work on a case of a switch on a condition")
					}
				}
			}
			return true
		})
		// the whole rest of the body under a gate: if have { body } as the
		// last statement that does work (only logs may follow it), with no
		// else or an else that only says why, runs nothing when the gate is
		// shut; an else that fails is the check, not a skip
		k := len(body.List)
		for k > 0 && onlyReports(&ast.BlockStmt{List: body.List[k-1 : k]}) && !failsHere(body.List[k-1]) {
			if _, isIf := body.List[k-1].(*ast.IfStmt); isIf {
				break
			}
			// a return of values after the if hands back what it set, or a
			// default it kept: the if is not the body's end
			if r, isReturn := body.List[k-1].(*ast.ReturnStmt); isReturn && len(r.Results) > 0 {
				break
			}
			k--
		}
		if k > 0 {
			if ifs, ok := body.List[k-1].(*ast.IfStmt); ok && elseOnlySaysWhy(ifs) && g.ifReads(ifs, probeGate) &&
				!onlyReturnsZero(ifs.Body) && !onlyReports(ifs.Body) && !onlyContinues(ifs.Body) && !callsRewriting(ifs) {
				hit(ifs.Pos(), "go-early-out", name+" runs its body only on a condition: "+text(ifs.Cond))
			}
		}
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
			if !ok || !g.ifReads(ifs, probeGate) || len(ifs.Body.List) == 0 {
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
	// a print (or a line built to be printed) that announces a skip and goes
	// on: the run reads as a pass having left something out. A message that
	// is a failure (an error, a panic, a Fatal, or a print followed by one)
	// refuses, and is not this.
	// Test code, and the harness and drivers under test/.
	noticeScope := strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "test/")
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok || !noticeScope || !announcesSkip(call) {
			return true
		}
		switch name := callName(call); {
		case strings.HasPrefix(name, "Sprint"):
			// a formatted line is a notice when it is handed back to be
			// printed, and says in words that something was skipped (an
			// upper-case SKIP there is a protocol line a test compares)
			if _, ok := stack[len(stack)-2].(*ast.ReturnStmt); !ok || !announcesSkipInWords(call) {
				return true
			}
		case strings.HasPrefix(name, "Fprint") && len(call.Args) > 0:
			// a line written into a buffer (&b) is a program or a report
			// being built, printed (and judged) elsewhere
			if u, ok := call.Args[0].(*ast.UnaryExpr); ok && u.Op == token.AND {
				return true
			}
		}
		for i := len(stack) - 2; i >= 0; i-- {
			if c, ok := stack[i].(*ast.CallExpr); ok && isFailure(c) {
				return true
			}
		}
		for i := len(stack) - 2; i >= 0; i-- {
			var list []ast.Stmt
			switch b := stack[i].(type) {
			case *ast.BlockStmt:
				list = b.List
			case *ast.CaseClause:
				list = b.Body
			default:
				continue
			}
			for j, st := range list {
				if st == stack[i+1] && j+1 < len(list) && failsHere(list[j+1]) {
					return true
				}
			}
			break
		}
		hit(call.Pos(), "go-skip-print", string(src[fset.Position(call.Pos()).Offset:min(fset.Position(call.End()).Offset, fset.Position(call.Pos()).Offset+120)]))
		return true
	})
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

// What a gate reads. probeGate is the environment, a tool on PATH, a file
// on disk, the platform or -short; probeOutside is the part of that outside
// the tree (all but a file): a loop that passes over a file the tree does
// not hold is filtering its data, not skipping a test.
const (
	probeGate = 1 << iota
	probeOutside
)

var gateSelectors = map[string]int{
	"os.Getenv":      probeGate | probeOutside,
	"os.LookupEnv":   probeGate | probeOutside,
	"exec.LookPath":  probeGate | probeOutside,
	"runtime.GOOS":   probeGate | probeOutside,
	"runtime.GOARCH": probeGate | probeOutside,
	"testing.Short":  probeGate | probeOutside,
	"os.Stat":        probeGate,
	"os.Lstat":       probeGate,
}

// probeIndex is every Go function that answers a gate: one whose body
// reads one of gateSelectors, or calls such a function of its own package,
// keyed by package directory and then by name ("."+name for a method). A
// condition that calls one (if !have("luau") { return }) is a gate behind a
// helper.
type probeIndex struct {
	module string
	byDir  map[string]map[string]int
	direct map[string]map[string]int // byDir before any call through an import
	names  map[string]string         // package directory -> its package name
}

// goProbeIndex reads every Go file under root once for its probes.
func goProbeIndex(root string, files []string) (*probeIndex, error) {
	idx := &probeIndex{byDir: map[string]map[string]int{}, names: map[string]string{}}
	if mod, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		first, _, _ := strings.Cut(string(mod), "\n")
		idx.module = strings.TrimSpace(strings.TrimPrefix(first, "module"))
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	type fn struct {
		key   string
		kinds int
		calls []string
	}
	type fileProbes struct {
		dir, pkg string
		fns      []fn
		err      error
	}
	var gofiles []string
	for _, f := range files {
		if strings.HasSuffix(f, ".go") {
			if _, ok := noSkipsExempt[f]; !ok {
				gofiles = append(gofiles, f)
			}
		}
	}
	per := make([]fileProbes, len(gofiles))
	eachParallel(len(gofiles), func(i int) {
		rel := gofiles[i]
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
		if err != nil {
			per[i].err = fmt.Errorf("%s: %w", rel, err)
			return
		}
		per[i].dir, per[i].pkg = path.Dir(rel), f.Name.Name
		// an imported package by the name the file calls it: its directory
		// under this module, or "" for a package outside it
		imported := map[string]string{}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			name := path.Base(p)
			if im.Name != nil {
				name = im.Name.Name
			}
			dir := ""
			if rest, ok := strings.CutPrefix(p, idx.module); idx.module != "" && ok && (rest == "" || strings.HasPrefix(rest, "/")) {
				dir = strings.TrimPrefix(rest, "/")
				if dir == "" {
					dir = "."
				}
			}
			imported[name] = dir
		}
		// what a body reads and calls: a call into a package of this module
		// is "@dir|Name", resolved once every package is read
		probe := func(key string, body ast.Node) fn {
			p := fn{key: key}
			ast.Inspect(body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if id, ok := x.X.(*ast.Ident); ok {
						p.kinds |= gateSelectors[id.Name+"."+x.Sel.Name]
					}
				case *ast.CallExpr:
					switch c := x.Fun.(type) {
					case *ast.Ident:
						p.calls = append(p.calls, c.Name)
					case *ast.SelectorExpr:
						id, ok := c.X.(*ast.Ident)
						dir, isImport := "", false
						if ok {
							dir, isImport = imported[id.Name]
						}
						switch {
						case !isImport:
							p.calls = append(p.calls, "."+c.Sel.Name)
						case dir != "":
							p.calls = append(p.calls, "@"+dir+"|"+c.Sel.Name)
						}
					}
				}
				return true
			})
			return p
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Body == nil || decl.Type.Results == nil || len(decl.Type.Results.List) == 0 {
					continue
				}
				key := decl.Name.Name
				if decl.Recv != nil {
					key = "." + decl.Name.Name
				}
				per[i].fns = append(per[i].fns, probe(key, decl.Body))
			case *ast.GenDecl:
				// a package variable holding a function: var have =
				// sync.OnceValue(func() bool { ... }), or var have = func...
				if decl.Tok != token.VAR {
					continue
				}
				for _, s := range decl.Specs {
					vs := s.(*ast.ValueSpec)
					for j, name := range vs.Names {
						if j >= len(vs.Values) {
							break
						}
						var lits []ast.Node
						ast.Inspect(vs.Values[j], func(n ast.Node) bool {
							if fl, ok := n.(*ast.FuncLit); ok {
								lits = append(lits, fl.Body)
								return false
							}
							return true
						})
						for _, body := range lits {
							per[i].fns = append(per[i].fns, probe(name.Name, body))
						}
					}
				}
			}
		}
	})
	calls := map[string]map[string][]string{}
	for _, fp := range per {
		if fp.err != nil {
			return nil, fp.err
		}
		if fp.dir == "" {
			continue
		}
		if _, ok := idx.names[fp.dir]; !ok || strings.HasSuffix(idx.names[fp.dir], "_test") {
			idx.names[fp.dir] = fp.pkg
		}
		if idx.byDir[fp.dir] == nil {
			idx.byDir[fp.dir] = map[string]int{}
			calls[fp.dir] = map[string][]string{}
		}
		for _, p := range fp.fns {
			idx.byDir[fp.dir][p.key] |= p.kinds
			calls[fp.dir][p.key] = append(calls[fp.dir][p.key], p.calls...)
		}
	}
	// a function that calls a probe of its own package is one
	closeInPackage := func() {
		for dir, fns := range idx.byDir {
			for changed := true; changed; {
				changed = false
				for key, callees := range calls[dir] {
					for _, c := range callees {
						if strings.HasPrefix(c, "@") {
							continue
						}
						if k := fns[key] | fns[c]; k != fns[key] {
							fns[key] = k
							changed = true
						}
					}
				}
			}
		}
	}
	closeInPackage()
	// what each package answers on its own is what an importer reads its
	// functions by; a function that calls one of those through an import
	// is a probe too when it asks outside the tree (the environment, PATH,
	// the platform), one package away and no further: a loader that stats
	// the tree's own files is reading data, and a decoder that reaches a
	// debug knob three packages down is not a gate
	idx.direct = map[string]map[string]int{}
	for dir, fns := range idx.byDir {
		idx.direct[dir] = maps.Clone(fns)
	}
	for dir, fns := range idx.byDir {
		for key, callees := range calls[dir] {
			for _, c := range callees {
				if rest, ok := strings.CutPrefix(c, "@"); ok {
					d, name, _ := strings.Cut(rest, "|")
					if k := idx.direct[d][name]; k&probeOutside != 0 {
						fns[key] |= k
					}
				}
			}
		}
	}
	closeInPackage()
	return idx, nil
}

// gateReader reads a condition for a gate, directly or through a probe of
// the file's own package or of a package of this module it imports.
type gateReader struct {
	local   map[string]int
	imports map[string]map[string]int
}

func newGateReader(idx *probeIndex, f *ast.File, rel string) gateReader {
	g := gateReader{imports: map[string]map[string]int{}}
	if idx == nil {
		return g
	}
	g.local = idx.byDir[path.Dir(rel)]
	if idx.module == "" {
		return g
	}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		dir, ok := strings.CutPrefix(p, idx.module)
		if !ok || (dir != "" && !strings.HasPrefix(dir, "/")) {
			continue
		}
		dir = strings.TrimPrefix(dir, "/")
		if dir == "" {
			dir = "."
		}
		name := idx.names[dir]
		if im.Name != nil {
			name = im.Name.Name
		}
		if name != "" && idx.direct[dir] != nil {
			g.imports[name] = idx.direct[dir]
		}
	}
	return g
}

// reads is a node that reads a gate of the wanted kind.
func (g gateReader) reads(n ast.Node, want int) bool {
	if n == nil {
		return false
	}
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.SelectorExpr:
			if id, ok := v.X.(*ast.Ident); ok && gateSelectors[id.Name+"."+v.Sel.Name]&want != 0 {
				found = true
			}
		case *ast.CallExpr:
			switch fn := v.Fun.(type) {
			case *ast.Ident:
				if g.local[fn.Name]&want != 0 {
					found = true
				}
			case *ast.SelectorExpr:
				if id, ok := fn.X.(*ast.Ident); ok && g.imports[id.Name] != nil {
					if g.imports[id.Name][fn.Sel.Name]&want != 0 {
						found = true
					}
				} else if g.local["."+fn.Sel.Name]&want != 0 {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// ifReads is an if whose init or condition reads a gate of the wanted kind.
func (g gateReader) ifReads(ifs *ast.IfStmt, want int) bool {
	return (ifs.Init != nil && g.reads(ifs.Init, want)) || g.reads(ifs.Cond, want)
}

// switchReads is a switch whose init, tag or a case reads a gate.
func (g gateReader) switchReads(sw *ast.SwitchStmt, want int) bool {
	if (sw.Init != nil && g.reads(sw.Init, want)) || (sw.Tag != nil && g.reads(sw.Tag, want)) {
		return true
	}
	for _, c := range sw.Body.List {
		for _, e := range c.(*ast.CaseClause).List {
			if g.reads(e, want) {
				return true
			}
		}
	}
	return false
}

// callsRewriting is an if on internal/slowtest's Rewriting: the one way a
// golden test reads its rewrite switch, after the comparison it always runs
// (internal/ci TestGoldenRewritesGoThroughRewriting). The write it guards is
// a mode, not the test's work.
func callsRewriting(ifs *ast.IfStmt) bool {
	found := false
	ast.Inspect(ifs.Cond, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Rewriting" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "slowtest" {
				found = true
			}
		}
		return !found
	})
	return found
}

// elseOnlySaysWhy is an if with no else, or an else that only logs or
// returns: one that fails anywhere is the check's other half.
func elseOnlySaysWhy(ifs *ast.IfStmt) bool {
	if ifs.Else == nil {
		return true
	}
	e, ok := ifs.Else.(*ast.BlockStmt)
	if !ok || !onlyReports(e) {
		return false
	}
	fails := false
	ast.Inspect(e, func(n ast.Node) bool {
		if st, ok := n.(ast.Stmt); ok && failsHere(st) {
			fails = true
		}
		return !fails
	})
	return !fails
}

// onlyReports is a block that only fails or says why (t.Fatal, t.Error,
// panic, t.Log), returns at most, and loops or branches over nothing else: a
// check's reporting, not the test's work.
func onlyReports(b *ast.BlockStmt) bool {
	for _, st := range b.List {
		switch s := st.(type) {
		case *ast.ReturnStmt:
			continue
		case *ast.RangeStmt:
			if !onlyReports(s.Body) {
				return false
			}
		case *ast.ForStmt:
			if !onlyReports(s.Body) {
				return false
			}
		case *ast.IfStmt:
			if !onlyReports(s.Body) {
				return false
			}
			if e, ok := s.Else.(*ast.BlockStmt); ok && !onlyReports(e) {
				return false
			}
			if _, ok := s.Else.(*ast.IfStmt); ok {
				return false
			}
		case *ast.ExprStmt:
			call, ok := s.X.(*ast.CallExpr)
			if !ok {
				return false
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name != "panic" {
					return false
				}
			case *ast.SelectorExpr:
				switch fn.Sel.Name {
				case "Fatal", "Fatalf", "Error", "Errorf", "Fail", "FailNow", "Helper":
				default:
					if !saysWhy(fn.Sel.Name) {
						return false
					}
				}
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
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
	// the whole body under a gate, with nothing after it
	{"p/w1_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestW1(t *testing.T) {\n\tif os.Getenv(\"CI\") != \"\" {\n\t\tx := 1\n\t\tif x != 1 {\n\t\t\tt.Fatal(x)\n\t\t}\n\t}\n}\n", "go-early-out"},
	// a gate inside a switch
	{"p/w2_test.go", "package p\n\nimport (\n\t\"runtime\"\n\t\"testing\"\n)\n\nfunc TestW2(t *testing.T) {\n\tswitch runtime.GOOS {\n\tcase \"windows\":\n\t\treturn\n\t}\n\tt.Log(\"ran\")\n}\n", "go-early-out"},
	// a gate behind a helper in the same file, and in another file
	{"p/w3_test.go", "package p\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc have(tool string) bool {\n\t_, err := exec.LookPath(tool)\n\treturn err == nil\n}\n\nfunc TestW3(t *testing.T) {\n\tif !have(\"luau\") {\n\t\treturn\n\t}\n\tt.Log(\"ran\")\n}\n", "go-early-out"},
	{"p/w4_test.go", "package p\n\nimport \"testing\"\n\nfunc TestW4(t *testing.T) {\n\tif !corpusBuilt() {\n\t\treturn\n\t}\n\tt.Log(\"ran\")\n}\n", "go-early-out"},
	// a job that never runs on this repository
	{".github/workflows/cm.yml", "jobs:\n  a:\n    if: github.repository == 'someone/else'\n    steps:\n      - run: make check\n", "repo-gate"},
	// harness code that prints a skip and goes on, or hands back the line
	{"test/conformance/harness/notice.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc random(n int) {\n\tif n == 0 {\n\t\tfmt.Fprintln(os.Stderr, \"random pass skipped: no manifest seed is live\")\n\t}\n}\n", "go-skip-print"},
	{"test/conformance/harness/line.go", "package main\n\nimport \"fmt\"\n\nfunc randomLine(pinned int) string {\n\treturn fmt.Sprintf(\"random pass skipped: %d pinned vectors only\", pinned)\n}\n", "go-skip-print"},
	{"p/w5_test.go", "package p\n\nimport \"testing\"\n\nfunc TestW5(t *testing.T) {\n\tfor _, row := range []string{\"a\"} {\n\t\tt.Logf(\"skipped %s: not carried yet\", row)\n\t}\n}\n", "go-skip-print"},
	// the body under a gate with an else that only logs, and with a log
	// after it
	{"p/w6_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestW6(t *testing.T) {\n\tif os.Getenv(\"CI\") != \"\" {\n\t\tx := 1\n\t\tif x != 1 {\n\t\t\tt.Fatal(x)\n\t\t}\n\t} else {\n\t\tt.Log(\"not on CI\")\n\t}\n}\n", "go-early-out"},
	{"p/w7_test.go", "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestW7(t *testing.T) {\n\tif os.Getenv(\"CI\") != \"\" {\n\t\tx := 1\n\t\tif x != 1 {\n\t\t\tt.Fatal(x)\n\t\t}\n\t}\n\tt.Log(\"done\")\n}\n", "go-early-out"},
	// a gate behind a cached probe, and behind a helper that asks another
	// package of the module
	{"p/w8_test.go", "package p\n\nimport (\n\t\"os/exec\"\n\t\"sync\"\n\t\"testing\"\n)\n\nvar haveCC = sync.OnceValue(func() bool {\n\t_, err := exec.LookPath(\"cc\")\n\treturn err == nil\n})\n\nfunc TestW8(t *testing.T) {\n\tif !haveCC() {\n\t\treturn\n\t}\n\tt.Log(\"ran\")\n}\n", "go-early-out"},
	{"p/w9_test.go", "package p\n\nimport (\n\t\"testing\"\n\n\t\"example/lib\"\n)\n\nfunc need() bool { return lib.Have(\"cc\") }\n\nfunc TestW9(t *testing.T) {\n\tif !need() {\n\t\treturn\n\t}\n\tt.Log(\"ran\")\n}\n", "go-early-out"},
	// a skip notice built of a literal and a value
	{"p/w10_test.go", "package p\n\nimport \"testing\"\n\nfunc TestW10(t *testing.T) {\n\twhy := \"no seed\"\n\tt.Log(\"random pass skipped: \" + why)\n}\n", "go-skip-print"},
	// a job that never runs unless the event names one repository
	{".github/workflows/co.yml", "jobs:\n  a:\n    if: github.event.repository.full_name == 'someone/else'\n    steps:\n      - run: make check\n", "repo-gate"},
	// a tracked package under directories named target and deps
	{"p/target/x_test.go", "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Skip(\"later\")\n}\n", "go-skip"},
	{"deps/q/y_test.go", "package q\n\nimport \"testing\"\n\nfunc TestY(t *testing.T) {\n\tt.Skip(\"later\")\n}\n", "go-skip"},
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
		// the helper p/w4_test.go calls, in a file that is not a test's
		"p/w4.go": "package p\n\nimport \"os\"\n\nfunc corpusBuilt() bool {\n\t_, err := os.Stat(\"build/corpus\")\n\treturn err == nil\n}\n",
		// a check's reporting under a probe, a log under a variable, and a
		// golden rewrite after the comparison are no gate on the body
		"q/p_test.go": "package q\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"example/internal/slowtest\"\n)\n\nfunc gaps() []string {\n\tif _, err := os.Stat(\"x\"); err != nil {\n\t\treturn []string{\"x\"}\n\t}\n\treturn nil\n}\n\nfunc TestP(t *testing.T) {\n\tif red := gaps(); len(red) > 0 {\n\t\tfor _, r := range red {\n\t\t\tt.Error(r)\n\t\t}\n\t}\n}\n\nfunc TestP2(t *testing.T) {\n\tt.Log(\"ran\")\n\tif os.Getenv(\"VERBOSE\") != \"\" {\n\t\tt.Logf(\"more\")\n\t}\n}\n\nfunc TestP3(t *testing.T) {\n\tgot := []byte(\"x\")\n\tif slowtest.Rewriting(t, \"X_WRITE\") {\n\t\tif err := os.WriteFile(\"golden\", got, 0o644); err != nil {\n\t\t\tt.Fatal(err)\n\t\t}\n\t}\n}\n",
		// a skip named in a failure, a generated program, a protocol line,
		// a count printed before a red exit, and a tool outside test code
		"q/r_test.go":          "package q\n\nimport (\n\t\"bytes\"\n\t\"fmt\"\n\t\"os\"\n\t\"testing\"\n)\n\nfunc line(out, id string) string {\n\treturn fmt.Sprintf(\"[no SKIP line for %s: %s]\", id, out)\n}\n\nfunc TestR(t *testing.T) {\n\tvar b bytes.Buffer\n\tfmt.Fprintf(&b, \"t.Fatalf(\\\"refused or skipped\\\")\\n\")\n\tif b.Len() == 0 {\n\t\tt.Fatalf(\"a test skipped, and a skip is a failure\")\n\t}\n\tmsg := fmt.Sprintf(\"%s: cancelled or skipped would not turn the run red\", line(\"\", \"x\"))\n\tif msg == \"\" {\n\t\tfmt.Println(\"2 tests skipped\")\n\t\tos.Exit(1)\n\t}\n}\n",
		"tools/report/main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Printf(\"%d passed, %d skipped\\n\", 1, 0)\n}\n",
		// the module the plants live in, and a package of it that probes
		"go.mod":     "module example\n",
		"lib/lib.go": "package lib\n\nimport \"os/exec\"\n\nfunc Have(tool string) bool {\n\t_, err := exec.LookPath(tool)\n\treturn err == nil\n}\n",
		// a gated body whose else fails is the check, not a skip
		"q/s_test.go": "package q\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestS(t *testing.T) {\n\tif os.Getenv(\"HOME\") != \"\" {\n\t\tt.Log(\"home\")\n\t\tx := 1\n\t\t_ = x\n\t} else {\n\t\tt.Fatal(\"HOME is unset\")\n\t}\n}\n",
		// a workflow job kept off fork pull requests compares no literal;
		// fork pull requests run the fork lane's job instead (ci-fast.yml)
		".github/workflows/cn.yml": "jobs:\n  a:\n    if: github.event.pull_request.head.repo.full_name == github.repository\n",
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

// TestNoSkipScansTrackedOutputNames: in a checkout, a directory named like
// build output (deps, target) is scanned when it holds a tracked file and
// pruned when it holds none, and inside it only the tracked files are read
// (an untracked file beside a tracked one is that output). The old scan
// pruned both by name. Git runs with its location variables cleared, as
// the scan's own git does, so a run from a git hook touches only this tree.
func TestNoSkipScansTrackedOutputNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skip := "package q\n\nimport \"testing\"\n\nfunc TestY(t *testing.T) {\n\tt.Skip(\"later\")\n}\n"
	for _, f := range []string{"deps/q/y_test.go", "deps/q/vendored_test.go", "target/q/y_test.go"} {
		path := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(skip), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "deps/q/y_test.go"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = gitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	found, err := noSkips(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || !strings.HasPrefix(found[0], "deps/q/y_test.go:6: go-skip: ") {
		t.Fatalf("want the tracked deps/q skip alone (target/ holds no tracked file, deps/q/vendored_test.go is untracked), got %q", found)
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

// announcesSkip is a print, a test log or a formatted line whose literal
// text announces a skip.
func announcesSkip(call *ast.CallExpr) bool {
	name := callName(call)
	if name != "print" && name != "println" && !strings.HasPrefix(name, "Print") && !strings.HasPrefix(name, "Fprint") &&
		!strings.HasPrefix(name, "Sprint") && name != "Log" && name != "Logf" {
		return false
	}
	for _, a := range call.Args {
		if goSkipNotice.MatchString(literalText(a)) {
			return true
		}
	}
	return false
}

// literalText is the string literals an expression is built of, joined
// (a "+" of literals and other values counts its literals); a raw string
// over lines is a program a test generates, not a line, and counts nothing.
func literalText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING || (v.Value[0] == '`' && strings.Contains(v.Value, "\n")) {
			return ""
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return literalText(v.X) + " " + literalText(v.Y)
		}
	case *ast.ParenExpr:
		return literalText(v.X)
	}
	return ""
}

// announcesSkipInWords is a call whose literal text says skipped or
// skipping.
func announcesSkipInWords(call *ast.CallExpr) bool {
	for _, a := range call.Args {
		if goSkipWords.MatchString(literalText(a)) {
			return true
		}
	}
	return false
}

// callName is the name a call is made by: f(), x.f().
func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// isFailure is a call that fails or makes an error: errors.New, fmt.Errorf,
// panic, os.Exit of anything but 0, and a test's or a logger's Fatal and
// Error.
func isFailure(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "panic"
	case *ast.SelectorExpr:
		switch fn.Sel.Name {
		case "Errorf", "Error", "Fatal", "Fatalf", "Fatalln", "FailNow", "Fail", "Panic", "Panicf", "Panicln":
			return true
		case "New":
			id, ok := fn.X.(*ast.Ident)
			return ok && id.Name == "errors"
		case "Exit":
			if len(call.Args) != 1 {
				return false
			}
			// a status the code does not spell may be 0
			lit, ok := call.Args[0].(*ast.BasicLit)
			return ok && lit.Kind == token.INT && lit.Value != "0"
		}
	}
	return false
}

// failsHere is a statement that fails: a failing call, or a return of an
// error or a non-zero status.
func failsHere(st ast.Stmt) bool {
	switch s := st.(type) {
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		return ok && isFailure(call)
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			switch v := r.(type) {
			case *ast.CallExpr:
				if isFailure(v) {
					return true
				}
			case *ast.Ident:
				if v.Name == "err" {
					return true
				}
			case *ast.BasicLit:
				if v.Kind == token.INT && v.Value != "0" {
					return true
				}
			}
		}
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
