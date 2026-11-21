package ci

// THE SCANNERS BEHIND THE NO-SILENT-SKIP GATES (silent_skips_test.go).
//
// Three ways a test that exists does not run, each read off the source so the
// gate is red before the test is ever missed in a lane:
//
//   - a t.Skip: scanSkipSites finds every Skip, Skipf and SkipNow on a
//     testing.T, B, F or TB, with the reason it gives;
//   - a TestMain that leaves before m.Run: scanSkipSites reads every TestMain;
//   - a build constraint (a //go:build line, or a name ending in a GOOS or
//     GOARCH token) that keeps a _test.go file out of the default build:
//     scanConstrainedTests finds each one, and laneProofs finds the lanes
//     that compile under another tag set or on another OS.
//
// Each scanner is a function of a tree root, so the gates run them over the
// repository and the negative controls run them over a fixture in t.TempDir.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// skipScanPrune are the directories no scanner descends into: fixtures that
// are data, build output, vendored trees.
var skipScanPrune = map[string]bool{
	"testdata": true, "build": true, "dist": true, "bin": true,
	"node_modules": true, "third_party": true, "working": true,
}

func walkGoFiles(root string, visit func(rel string, src []byte) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipScanPrune[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return visit(filepath.ToSlash(rel), src)
	})
}

// skipSite is one Skip, Skipf or SkipNow call on a testing value.
type skipSite struct {
	file string
	line int
	// reason is what the call prints when its first argument is a string
	// literal (format verbs and later arguments read "X"); "" otherwise.
	reason  string
	literal bool // the reason is known from the source
	bare    bool // Skip() or SkipNow(): no reason at all
}

// earlyExit is a way out of a TestMain before m.Run that is not a loud
// failure: a return, or an os.Exit whose status is not a non-zero literal.
type earlyExit struct {
	file string
	line int
	what string
	// env are the variables the enclosing if-conditions read with os.Getenv:
	// a helper-process branch names the variable the package's own tests set.
	env []string
}

type skipScan struct {
	files     int // .go files that import testing
	sites     []skipSite
	testMains int
	noRun     []string    // TestMain functions that never call m.Run
	exits     []earlyExit // exits before m.Run
	// setenv are the variable names each directory's tests set for a child
	// (t.Setenv("NAME", ...) or a "NAME=..." literal).
	setenv map[string]map[string]bool
	// disguised are the skips that report PASS (scanDisguisedSkips).
	disguised []disguisedSkip
}

var verb = regexp.MustCompile(`%[-+# 0-9.*\[\]]*[a-zA-Z]`)

// stringLit evaluates a string literal or a + of them.
func stringLit(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		a, ok1 := stringLit(v.X)
		b, ok2 := stringLit(v.Y)
		return a + b, ok1 && ok2
	case *ast.ParenExpr:
		return stringLit(v.X)
	}
	return "", false
}

// stringConsts are the file's constants that are string literals.
func stringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				if s, ok := stringLit(vs.Values[i]); ok {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

func stringOrConst(e ast.Expr, consts map[string]string) (string, bool) {
	if s, ok := stringLit(e); ok {
		return s, true
	}
	if id, ok := e.(*ast.Ident); ok && consts[id.Name] != "" {
		return consts[id.Name], true
	}
	return "", false
}

// isTestingValue reports whether the identifier is declared with a type from
// package testing (t *testing.T, tb testing.TB, b *testing.B, f *testing.F).
func isTestingValue(id *ast.Ident) bool {
	if id.Obj == nil {
		return false
	}
	var typ ast.Expr
	switch d := id.Obj.Decl.(type) {
	case *ast.Field:
		typ = d.Type
	case *ast.ValueSpec:
		typ = d.Type
	}
	if typ == nil {
		return false
	}
	s := types.ExprString(typ)
	return strings.HasPrefix(strings.TrimPrefix(s, "*"), "testing.")
}

var setenvLit = regexp.MustCompile(`^([A-Z][A-Z0-9_]+)=`)

func scanSkipSites(root string) (*skipScan, error) {
	scan := &skipScan{setenv: map[string]map[string]bool{}}
	fset := token.NewFileSet()
	err := walkGoFiles(root, func(rel string, src []byte) error {
		if !bytes.Contains(src, []byte(`"testing"`)) {
			return nil
		}
		f, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		scan.files++
		dir := filepath.ToSlash(filepath.Dir(rel))
		if scan.setenv[dir] == nil {
			scan.setenv[dir] = map[string]bool{}
		}
		consts := stringConsts(f)
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				// childEnv+"=block": a child's variable, named by a constant
				if id, ok := v.X.(*ast.Ident); ok && v.Op == token.ADD {
					if rest, ok := stringLit(v.Y); ok && strings.HasPrefix(rest, "=") && consts[id.Name] != "" {
						scan.setenv[dir][consts[id.Name]] = true
					}
				}
			case *ast.BasicLit:
				if s, ok := stringLit(v); ok {
					if m := setenvLit.FindStringSubmatch(s); m != nil {
						scan.setenv[dir][m[1]] = true
					}
				}
			case *ast.CallExpr:
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "Setenv" && len(v.Args) == 2 {
					if s, ok := stringOrConst(v.Args[0], consts); ok {
						scan.setenv[dir][s] = true
					}
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || !isTestingValue(id) {
					return true
				}
				site := skipSite{file: rel, line: fset.Position(v.Pos()).Line}
				switch sel.Sel.Name {
				case "SkipNow":
					site.bare = true
				case "Skip", "Skipf":
					if len(v.Args) == 0 {
						site.bare = true
						break
					}
					if s, ok := stringLit(v.Args[0]); ok {
						orig := s
						site.literal = true
						if sel.Sel.Name == "Skipf" {
							s = verb.ReplaceAllString(s, "X")
						} else if len(v.Args) > 1 {
							s += " X"
						}
						site.reason = strings.Join(strings.Fields(s), " ")
						// a reason that opens with a verb ("%s: %v", tool, err)
						// names its subject only at run time: the lane's
						// ledger reads it there
						if sel.Sel.Name == "Skipf" && verb.FindStringIndex(strings.TrimSpace(orig)) != nil && verb.FindStringIndex(strings.TrimSpace(orig))[0] == 0 {
							site.literal, site.reason = false, ""
						}
					}
				default:
					return true
				}
				scan.sites = append(scan.sites, site)
			}
			return true
		})
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "TestMain" || fn.Recv != nil || fn.Body == nil ||
				fn.Type.Params == nil || len(fn.Type.Params.List) != 1 ||
				types.ExprString(fn.Type.Params.List[0].Type) != "*testing.M" {
				continue
			}
			scan.testMains++
			scanTestMain(scan, fset, rel, fn, consts)
		}
		scanDisguisedSkips(scan, fset, rel, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(scan.sites, func(i, j int) bool {
		a, b := scan.sites[i], scan.sites[j]
		if a.file != b.file {
			return a.file < b.file
		}
		return a.line < b.line
	})
	return scan, nil
}

// scanTestMain records a TestMain that never calls m.Run, and every quiet
// way out of it before the first m.Run.
func scanTestMain(scan *skipScan, fset *token.FileSet, rel string, fn *ast.FuncDecl, consts map[string]string) {
	m := ""
	if names := fn.Type.Params.List[0].Names; len(names) == 1 {
		m = names[0].Name
	}
	run := token.NoPos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == m && (run == token.NoPos || call.Pos() < run) {
					run = call.Pos()
				}
			}
		}
		return true
	})
	if run == token.NoPos {
		scan.noRun = append(scan.noRun, fmt.Sprintf("%s:%d", rel, fset.Position(fn.Pos()).Line))
		return
	}
	var conds []ast.Node // the enclosing if statements, outermost first
	var walk func(n ast.Node)
	envOf := func() []string {
		var out []string
		for _, c := range conds {
			ast.Inspect(c, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				if types.ExprString(call.Fun) == "os.Getenv" {
					if s, ok := stringOrConst(call.Args[0], consts); ok {
						out = append(out, s)
					}
				}
				return true
			})
		}
		return out
	}
	walk = func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.FuncLit:
				return false // a closure's return is its own
			case *ast.IfStmt:
				head := &ast.IfStmt{If: v.If, Init: v.Init, Cond: v.Cond, Body: &ast.BlockStmt{}}
				conds = append(conds, head)
				walk(v.Body)
				if v.Else != nil {
					walk(v.Else)
				}
				conds = conds[:len(conds)-1]
				return false
			case *ast.SwitchStmt:
				// switch os.Getenv(childEnv) { case "block": ... }: every
				// case is under what the switch reads
				head := &ast.SwitchStmt{Switch: v.Switch, Init: v.Init, Tag: v.Tag, Body: &ast.BlockStmt{}}
				conds = append(conds, head)
				walk(v.Body)
				conds = conds[:len(conds)-1]
				return false
			case *ast.ReturnStmt:
				if v.Pos() < run {
					scan.exits = append(scan.exits, earlyExit{rel, fset.Position(v.Pos()).Line, "return", envOf()})
				}
			case *ast.CallExpr:
				if v.End() < run && types.ExprString(v.Fun) == "os.Exit" && len(v.Args) == 1 {
					if lit, ok := v.Args[0].(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value != "0" {
						return true // a loud failure, not a silent skip
					}
					scan.exits = append(scan.exits, earlyExit{rel, fset.Position(v.Pos()).Line, "os.Exit(" + types.ExprString(v.Args[0]) + ")", envOf()})
				}
			}
			return true
		})
	}
	walk(fn.Body)
}

// silentExits are the early exits no helper-process variable explains: the
// exit is not under an os.Getenv of a variable the same directory's tests set.
func (s *skipScan) silentExits() []earlyExit {
	var out []earlyExit
	for _, e := range s.exits {
		helper := false
		for _, name := range e.env {
			if s.setenv[filepath.ToSlash(filepath.Dir(e.file))][name] {
				helper = true
			}
		}
		if !helper {
			out = append(out, e)
		}
	}
	return out
}

// THE BUILD CONSTRAINTS.

var knownOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
	"hurd": true, "illumos": true, "ios": true, "js": true, "linux": true, "nacl": true,
	"netbsd": true, "openbsd": true, "plan9": true, "solaris": true, "wasip1": true,
	"windows": true, "zos": true,
}

var knownArch = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true,
	"arm64be": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true,
	"mips64le": true, "mips64p32": true, "mips64p32le": true, "ppc": true, "ppc64": true,
	"ppc64le": true, "riscv": true, "riscv64": true, "s390": true, "s390x": true,
	"sparc": true, "sparc64": true, "wasm": true,
}

var unixOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
	"hurd": true, "illumos": true, "ios": true, "linux": true, "netbsd": true,
	"openbsd": true, "solaris": true,
}

// constrainedTest is a _test.go file the default lane (linux, amd64, no tags)
// does not compile.
type constrainedTest struct {
	file string
	dir  string
	expr constraint.Expr
	text string // the constraint as a reader would write it
}

// fileConstraint is the whole constraint on a file: its //go:build line and
// what its name implies (name_GOOS, name_GOARCH, name_GOOS_GOARCH, each
// before _test).
func fileConstraint(name string, src []byte) (constraint.Expr, string) {
	var parts []constraint.Expr
	var text []string
	for line := range strings.Lines(string(src)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			break
		}
		if constraint.IsGoBuild(line) {
			if x, err := constraint.Parse(line); err == nil {
				parts = append(parts, x)
				text = append(text, line)
			}
		}
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".go"), "_test")
	words := strings.Split(base, "_")
	if len(words) >= 2 {
		words = words[1:] // the first word is never a constraint
		n := len(words)
		var implied []string
		switch {
		case n >= 2 && knownOS[words[n-2]] && knownArch[words[n-1]]:
			implied = words[n-2:]
		case knownOS[words[n-1]] || knownArch[words[n-1]]:
			implied = words[n-1:]
		}
		for _, w := range implied {
			parts = append(parts, &constraint.TagExpr{Tag: w})
			text = append(text, "name ends _"+w)
		}
	}
	if len(parts) == 0 {
		return nil, ""
	}
	expr := parts[0]
	for _, p := range parts[1:] {
		expr = &constraint.AndExpr{X: expr, Y: p}
	}
	return expr, strings.Join(text, "; ")
}

// buildLane is a configuration some lane compiles under.
type buildLane struct {
	goos, goarch string
	tags         map[string]bool
}

func (l buildLane) has(tag string) bool {
	switch {
	case tag == l.goos || tag == l.goarch || l.tags[tag]:
		return true
	case tag == "unix":
		return unixOS[l.goos]
	case tag == "gc" || strings.HasPrefix(tag, "go1."):
		return true
	}
	return false
}

var defaultLane = buildLane{goos: "linux", goarch: "amd64"}

type constraintScan struct {
	testFiles   int
	constrained []constrainedTest // every test file that carries a constraint
	excluded    []constrainedTest // those the default lane does not compile
}

func scanConstrainedTests(root string) (*constraintScan, error) {
	scan := &constraintScan{}
	err := walkGoFiles(root, func(rel string, src []byte) error {
		if !strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		scan.testFiles++
		expr, text := fileConstraint(filepath.Base(rel), src)
		if expr == nil {
			return nil
		}
		c := constrainedTest{file: rel, dir: filepath.ToSlash(filepath.Dir(rel)), expr: expr, text: text}
		scan.constrained = append(scan.constrained, c)
		if !expr.Eval(defaultLane.has) {
			scan.excluded = append(scan.excluded, c)
		}
		return nil
	})
	return scan, err
}

// laneProof is one `go test` command some CI lane runs under a build
// configuration other than the default: where it is written, what it
// compiles under, and the packages it names.
type laneProof struct {
	where string
	lane  buildLane
	pkgs  []string
}

func (p laneProof) covers(dir string) bool {
	for _, pkg := range p.pkgs {
		pkg = strings.TrimSuffix(strings.TrimPrefix(pkg, "./"), "/")
		if rest, ok := strings.CutSuffix(pkg, "..."); ok {
			rest = strings.TrimSuffix(rest, "/")
			if rest == "" || rest == "." || dir == rest || strings.HasPrefix(dir, rest+"/") {
				return true
			}
			continue
		}
		if pkg == dir {
			return true
		}
	}
	return false
}

var (
	skipMakeTarget = regexp.MustCompile(`^([A-Za-z0-9_.%-]+(?:[ \t]+[A-Za-z0-9_.%-]+)*)[ \t]*:([^=]|$)`)
	skipTagsFlag   = regexp.MustCompile(`-tags[ =]+["']?([A-Za-z0-9_, ]+?)["']?(\s+-|\s+\./|\s*$)`)
	skipPkgToken   = regexp.MustCompile(`(^|\s)(\./[A-Za-z0-9_./-]*)`)
	skipJobHeader  = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*(#.*)?$`)
)

var (
	literalFalse = regexp.MustCompile(`^(false|\$\{\{\s*false\s*\}\})$`)
	// cd, pushd, or go test -C: what follows runs somewhere else
	changesDir = regexp.MustCompile(`(^|[;&|(]\s*)(cd|pushd)\s|\sgo test\s+-C[ =]|^go test\s+-C[ =]`)
)

func goTestPackages(line string) []string {
	var out []string
	for _, m := range skipPkgToken.FindAllStringSubmatch(line, -1) {
		out = append(out, m[2])
	}
	return out
}

// laneProofs reads the lanes that compile tests under another configuration:
// a make recipe's `go test -tags T ./pkg` whose target a workflow names, a
// workflow's own `go test -tags T ./pkg`, and a `go test ./pkg` in a job that
// runs on Windows or macOS.
func laneProofs(root string) ([]laneProof, error) {
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return nil, err
	}
	var proofs []laneProof
	var wfText strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(wfDir, e.Name()))
		if err != nil {
			return nil, err
		}
		job, goos, goarch := "", "", ""
		type cmd struct {
			line int
			text string
		}
		var cmds []cmd
		// A command is credited only when it runs, and runs where the
		// workflow checked the tree out: a `go test` the line itself runs
		// (not text an echo prints), not in a job or step whose `if:` is the
		// literal false, and not after a working-directory or a cd, where
		// ./pkg names some other package (F-180).
		var jobOff, jobMoved, inSteps, stepOff, stepMoved bool
		flush := func() {
			for _, c := range cmds {
				lane := buildLane{goos: "linux", goarch: "amd64", tags: map[string]bool{}}
				if goos != "" {
					lane.goos, lane.goarch = goos, goarch
				}
				if m := skipTagsFlag.FindStringSubmatch(c.text); m != nil {
					for _, t := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' }) {
						lane.tags[t] = true
					}
				}
				if goos == "" && len(lane.tags) == 0 {
					continue // the default lane
				}
				proofs = append(proofs, laneProof{
					where: fmt.Sprintf(".github/workflows/%s:%d (job %s)", e.Name(), c.line, job),
					lane:  lane, pkgs: goTestPackages(c.text),
				})
			}
			cmds, goos, goarch = nil, "", ""
			jobOff, jobMoved, inSteps, stepOff, stepMoved = false, false, false, false, false
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			wfText.WriteString(line)
			wfText.WriteByte('\n')
			if m := skipJobHeader.FindStringSubmatch(line); m != nil {
				flush()
				job = m[1]
				continue
			}
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "runs-on:") {
				switch {
				case strings.Contains(t, "windows"):
					goos, goarch = "windows", "amd64"
				case strings.Contains(t, "macos"):
					goos, goarch = "darwin", "arm64"
				}
			}
			t := strings.TrimSpace(line)
			if t == "steps:" {
				inSteps = true
			}
			if strings.HasPrefix(t, "- ") {
				stepOff, stepMoved = false, false
				t = strings.TrimSpace(strings.TrimPrefix(t, "- "))
			}
			if v, ok := strings.CutPrefix(t, "if:"); ok && literalFalse.MatchString(strings.TrimSpace(v)) {
				if inSteps {
					stepOff = true
				} else {
					jobOff = true
				}
			}
			if v, ok := strings.CutPrefix(t, "working-directory:"); ok && strings.Trim(strings.TrimSpace(v), `"'`) != "." {
				if inSteps {
					stepMoved = true
				} else {
					jobMoved = true
				}
			}
			moves := changesDir.MatchString(strings.TrimSpace(strings.TrimPrefix(t, "run:")))
			if goTestCommand.MatchString(t) && !jobOff && !stepOff && !jobMoved && !stepMoved && !moves {
				cmds = append(cmds, cmd{i + 1, line})
			}
			if moves {
				stepMoved = true
			}
		}
		flush()
	}
	workflows := wfText.String()

	makefiles := []string{filepath.Join(root, "Makefile")}
	more, _ := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	makefiles = append(makefiles, more...)
	for _, mf := range makefiles {
		b, err := os.ReadFile(mf)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		rel, _ := filepath.Rel(root, mf)
		var targets []string
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "\t") {
				if m := skipMakeTarget.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, ".PHONY") {
					targets = strings.Fields(m[1])
				}
				continue
			}
			recipe := strings.TrimLeft(strings.TrimSpace(line), "@+-")
			if !goTestCommand.MatchString(recipe) || changesDir.MatchString(recipe) {
				continue // not a go test the recipe runs from the tree's root
			}
			m := skipTagsFlag.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			lane := buildLane{goos: "linux", goarch: "amd64", tags: map[string]bool{}}
			for _, t := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' }) {
				lane.tags[t] = true
			}
			for _, target := range targets {
				// the target is a lane only when a workflow runs it by name
				named, err := regexp.MatchString(`(^|[\s"'])`+regexp.QuoteMeta(target)+`([\s"';]|$)`, workflows)
				if err != nil || !named {
					continue
				}
				proofs = append(proofs, laneProof{
					where: fmt.Sprintf("%s:%d (make %s)", filepath.ToSlash(rel), i+1, target),
					lane:  lane, pkgs: goTestPackages(line),
				})
			}
		}
	}
	return proofs, nil
}

// neverCompiled are the excluded test files no lane proof compiles.
func neverCompiled(scan *constraintScan, proofs []laneProof) []constrainedTest {
	var out []constrainedTest
	for _, c := range scan.excluded {
		ok := false
		for _, p := range proofs {
			if c.expr.Eval(p.lane.has) && p.covers(c.dir) {
				ok = true
				break
			}
		}
		if !ok {
			out = append(out, c)
		}
	}
	return out
}

// A SKIP DISGUISED AS A PASS. A test looks for its toolchain, its sibling
// checkout or its environment variable, does not find it, and returns: go
// test prints PASS over a body that never ran, no ledger sees a skip, and no
// require-the-toolchain variable can turn it red. The reviewer's case is
// internal/codegen/rust's TestAssembleLibExportsUnionModule, which logged
// "cargo ... not found; skipping compilation check" and returned.
//
// The scanner reads every function that takes a testing value and returns
// nothing. It follows what was probed: a name assigned from exec.LookPath,
// os.Stat, os.Lstat, os.Getenv or os.LookupEnv, and a name assigned under an
// if that tests one. Two shapes are refused:
//
//   - "missing, so leave": an if whose condition says a probed name came back
//     empty or in error (err != nil, path == "", os.Getenv(...) != "1"), and
//     whose body is a bare return, with at most a t.Log before it;
//   - "found, so run the rest": an if with no else whose condition says a
//     toolchain was found (the err of an exec.LookPath == nil) and whose body
//     holds the part of the test that needs it. Without the toolchain that
//     part is skipped and the test passes.
type disguisedSkip struct {
	file string
	line int
	fn   string
	what string
}

var probeCalls = map[string]bool{
	"exec.LookPath": true, "os.Stat": true, "os.Lstat": true, "os.Getenv": true, "os.LookupEnv": true,
}

func hasProbe(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && probeCalls[types.ExprString(call.Fun)] {
			found = true
		}
		return !found
	})
	return found
}

func scanDisguisedSkips(scan *skipScan, fset *token.FileSet, rel string, f *ast.File) {
	check := func(name string, typ *ast.FuncType, body *ast.BlockStmt) {
		if body == nil || typ.Results != nil || typ.Params == nil {
			return
		}
		testing := false
		for _, p := range typ.Params.List {
			if strings.HasPrefix(strings.TrimPrefix(types.ExprString(p.Type), "*"), "testing.") {
				testing = true
			}
		}
		if !testing {
			return
		}
		// probed[name] is the positions from which the name holds a probe's
		// answer, and cleared[name] those from which it holds something else
		type mark struct {
			pos    token.Pos
			probed bool
		}
		marks := map[string][]mark{}
		probedAt := func(id string, pos token.Pos) bool {
			state := false
			for _, m := range marks[id] {
				if m.pos < pos {
					state = m.probed
				}
			}
			return state
		}
		reads := func(n ast.Node, pos token.Pos) bool {
			if n == nil {
				return false
			}
			if hasProbe(n) {
				return true
			}
			found := false
			ast.Inspect(n, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && probedAt(id.Name, pos) {
					found = true
				}
				return !found
			})
			return found
		}
		var under []bool                 // whether each enclosing if tests a probe
		lookPathErr := map[string]bool{} // the names that hold an exec.LookPath error
		var walk func(n ast.Node)
		walk = func(n ast.Node) {
			ast.Inspect(n, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.FuncLit:
					return false // judged as a function of its own
				case *ast.AssignStmt:
					probe := false
					for _, r := range v.Rhs {
						if hasProbe(r) {
							probe = true
						}
					}
					for _, u := range under {
						probe = probe || u
					}
					lookPath := len(v.Rhs) == 1 && isCall(v.Rhs[0], "exec.LookPath")
					for i, l := range v.Lhs {
						if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
							marks[id.Name] = append(marks[id.Name], mark{v.Pos(), probe})
							lookPathErr[id.Name] = lookPath && i == 1
						}
					}
				case *ast.IfStmt:
					if v.Init != nil {
						walk(v.Init)
					}
					tests := reads(v.Cond, v.Body.Pos()) || (v.Init != nil && hasProbe(v.Init))
					probed := func(e ast.Expr) bool { return reads(e, v.Body.Pos()) }
					switch {
					case tests && quietReturn(v.Body) && saysMissing(v.Cond, probed):
						scan.disguised = append(scan.disguised, disguisedSkip{rel, fset.Position(v.Pos()).Line, name,
							"returns quietly when what it looked for is missing"})
					case v.Else == nil && doesTestWork(v.Body) && saysToolFound(v, func(id string) bool { return lookPathErr[id] }):
						scan.disguised = append(scan.disguised, disguisedSkip{rel, fset.Position(v.Pos()).Line, name,
							"runs part of its body only when exec.LookPath finds the toolchain"})
					}
					under = append(under, tests)
					walk(v.Body)
					under = under[:len(under)-1]
					if v.Else != nil {
						walk(v.Else)
					}
					return false
				}
				return true
			})
		}
		walk(body)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			check(v.Name.Name, v.Type, v.Body)
		case *ast.FuncLit:
			check("func literal", v.Type, v.Body)
		}
		return true
	})
}

func isCall(e ast.Expr, fun string) bool {
	call, ok := e.(*ast.CallExpr)
	return ok && types.ExprString(call.Fun) == fun
}

// quietReturn reports whether the block is a bare return with at most log
// lines before it: nothing ran, nothing skipped, nothing failed.
func quietReturn(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	ret, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 0 {
		return false
	}
	for _, st := range b.List[:len(b.List)-1] {
		expr, ok := st.(*ast.ExprStmt)
		if !ok {
			return false
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Log" && sel.Sel.Name != "Logf") {
			return false
		}
	}
	return true
}

func isLit(e ast.Expr, value string) bool {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name == value
	}
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Value == value
	}
	return false
}

// saysMissing reports whether the condition holds when a probe came back
// empty or in error: probed != nil, probed == "", probed != "<value>",
// !probed, on either side of && or ||.
func saysMissing(cond ast.Expr, probed func(ast.Expr) bool) bool {
	switch v := cond.(type) {
	case *ast.ParenExpr:
		return saysMissing(v.X, probed)
	case *ast.UnaryExpr:
		return v.Op == token.NOT && probed(v.X)
	case *ast.BinaryExpr:
		switch v.Op {
		case token.LAND, token.LOR:
			return saysMissing(v.X, probed) || saysMissing(v.Y, probed)
		case token.NEQ:
			if isLit(v.Y, "nil") {
				return probed(v.X)
			}
			if lit, ok := v.Y.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value != `""` {
				return probed(v.X)
			}
		case token.EQL:
			if isLit(v.Y, `""`) {
				return probed(v.X)
			}
		}
	}
	return false
}

// saysToolFound reports whether the if runs its body only when an
// exec.LookPath succeeded: `if _, err := exec.LookPath(x); err == nil {`, or
// `err == nil` on a name that holds a LookPath error.
func saysToolFound(v *ast.IfStmt, lookPathErr func(string) bool) bool {
	cond, ok := v.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.EQL || !isLit(cond.Y, "nil") {
		return false
	}
	id, ok := cond.X.(*ast.Ident)
	if !ok {
		return false
	}
	if init, ok := v.Init.(*ast.AssignStmt); ok && len(init.Rhs) == 1 && isCall(init.Rhs[0], "exec.LookPath") {
		return len(init.Lhs) == 2 && types.ExprString(init.Lhs[1]) == id.Name
	}
	return v.Init == nil && lookPathErr(id.Name)
}

// doesTestWork reports whether the block runs a program or asserts: the part
// of a test whose absence matters. A block that only adds an argument (a
// linker path when cc is there) is not one.
func doesTestWork(b *ast.BlockStmt) bool {
	work := false
	ast.Inspect(b, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Command", "CommandContext", "Fatal", "Fatalf", "Error", "Errorf", "Run":
					work = true
				}
			}
		}
		return !work
	})
	return work
}
