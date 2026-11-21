package vacuous

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// A Finding is one place where a test can pass having checked nothing.
type Finding struct {
	Class string // "1" .. "7", with a letter for the sub-kind
	File  string
	Line  int
	Func  string
	Msg   string
	Gated bool // refused by the gate (the mechanically decidable classes)

	fixKey string // class 2: the collection a non-empty check would name
	fixT   string // class 2: the testing parameter in scope at the loop
	fixAt  int    // class 2: the byte offset of the loop in its file

	fixHoist     bool // class 2: fixKey is a new name for the ranged call
	fixX0, fixX1 int  // class 2: the byte range of the ranged call
}

func (f Finding) String() string {
	return fmt.Sprintf("%s\t%s:%d\t%s\t%s", f.Class, f.File, f.Line, f.Func, f.Msg)
}

type run struct {
	strict   *analyzer
	a        *analyzer
	findings []Finding
}

func (r *run) add(class string, n ast.Node, fn string, gated bool, format string, args ...any) {
	file, line := r.a.t.pos(n)
	r.findings = append(r.findings, Finding{Class: class, File: file, Line: line, Func: fn, Msg: fmt.Sprintf(format, args...), Gated: gated})
}

// analyze runs every rule over every _test.go file of the tree.
func analyze(t *tree) []Finding {
	r := &run{a: newAnalyzer(t), strict: newAnalyzer(t)}
	r.strict.strict = true
	var dirs []string
	for dir := range t.pkgs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		for _, sf := range t.pkgs[dir].files {
			if !sf.isTest {
				continue
			}
			for _, d := range sf.ast.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				s := r.a.scopeOf(fd)
				kind, isTest := isTestFunc(fd)
				if isTest {
					t.testFuncs++
					r.rule1(s, kind)
					r.rule3early(s, fd.Name.Name, fd.Body)
					r.rule7(s)
				}
				r.rule1subtests(s)
				r.rule2(s)
				r.rule3(s)
				r.rule4(s)
				r.rule5(s)
				r.rule6(s)
			}
		}
	}
	sort.SliceStable(r.findings, func(i, j int) bool {
		x, y := r.findings[i], r.findings[j]
		if x.Class != y.Class {
			return x.Class < y.Class
		}
		if x.File != y.File {
			return x.File < y.File
		}
		return x.Line < y.Line
	})
	return r.findings
}

// ---- class 1: a test or subtest that cannot fail ----

func (r *run) rule1(s *scope, kind string) {
	if r.a.reach(s, s.decl.Body) {
		if !r.strict.reach(r.strict.scopeOf(s.decl), s.decl.Body) {
			r.add("1w", s.decl, s.decl.Name.Name, false, "%s can fail only by a panic, an exit, or a callee this tree cannot see", kind)
		}
		return
	}
	switch kind {
	case "benchmark":
		// a benchmark measures; one with no failure path is listed, not refused
		r.add("1b", s.decl, s.decl.Name.Name, false, "benchmark has no reachable call that can fail it")
	default:
		r.add("1a", s.decl, s.decl.Name.Name, true, "%s has no reachable call that can fail it", kind)
	}
}

// subtestCall returns the function literal of an x.Run(name, func(t *testing.T) {...}) call.
func subtestCall(s *scope, call *ast.CallExpr) *ast.FuncLit {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" || len(call.Args) != 2 {
		return nil
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || !s.tnames[x.Name] {
		return nil
	}
	lit, ok := call.Args[1].(*ast.FuncLit)
	if !ok || lit.Type.Params == nil || len(lit.Type.Params.List) != 1 || !isTestingType(lit.Type.Params.List[0].Type) {
		return nil
	}
	return lit
}

func (r *run) rule1subtests(s *scope) {
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		lit := subtestCall(s, call)
		if lit == nil || r.a.reach(s, lit.Body) {
			return true
		}
		sel := call.Fun.(*ast.SelectorExpr)
		gated := sel.X.(*ast.Ident).Name != "b"
		r.add("1s", call, s.decl.Name.Name, gated, "subtest %s has no reachable call that can fail it", exprString(call.Args[0]))
		return true
	})
}

// ---- class 2: the zero-iteration pass ----

// fsSources are the calls whose result is a listing of the filesystem.
var fsSources = map[string]bool{
	"os.ReadDir": true, "ioutil.ReadDir": true, "fs.ReadDir": true, "filepath.Glob": true, "fs.Glob": true,
	"filepath.Walk": true, "filepath.WalkDir": true, "fs.WalkDir": true,
}

// passthrough are the calls that reshape a collection without deciding whether it is empty.
var passthrough = map[string]bool{
	"slices.Sorted": true, "slices.Collect": true, "slices.Values": true, "slices.All": true, "slices.Backward": true,
	"slices.Clone": true, "slices.Compact": true, "maps.Keys": true, "maps.Values": true, "maps.All": true, "len": true,
}

// textSplit are the calls that cut a run-time text into a collection.
var textSplit = map[string]bool{
	"strings.Split": true, "strings.Fields": true, "strings.SplitSeq": true, "strings.Lines": true, "strings.SplitN": true,
	"strings.FieldsSeq": true, "bytes.Split": true, "bytes.Fields": true, "bytes.Lines": true, "strings.SplitAfter": true,
}

type localDefs struct {
	assigns  map[string][]ast.Expr // name -> right-hand sides assigned to it (nil entry: declared with no value)
	appended map[string]bool
	always   map[string]bool // appended to by a statement that always runs
	index    map[string]int  // name -> its position on the left of its last assignment
	params   map[string]bool
}

func collectDefs(d *ast.FuncDecl) *localDefs {
	l := &localDefs{assigns: map[string][]ast.Expr{}, appended: map[string]bool{}, always: map[string]bool{}, index: map[string]int{}, params: map[string]bool{}}
	addParams := func(ft *ast.FuncType) {
		if ft.Params == nil {
			return
		}
		for _, f := range ft.Params.List {
			for _, n := range f.Names {
				l.params[n.Name] = true
			}
		}
	}
	addParams(d.Type)
	top := map[ast.Stmt]bool{}
	for _, st := range d.Body.List {
		top[st] = true
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			addParams(n.Type)
			for _, st := range n.Body.List {
				top[st] = true
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				var rhs ast.Expr
				if len(n.Rhs) == len(n.Lhs) {
					rhs = n.Rhs[i]
				} else if len(n.Rhs) == 1 {
					rhs = n.Rhs[0]
				}
				if call, ok := rhs.(*ast.CallExpr); ok {
					if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "append" {
						if top[n] {
							l.always[id.Name] = true
						}
						l.appended[id.Name] = true
						continue
					}
				}
				l.assigns[id.Name] = append(l.assigns[id.Name], rhs)
				l.index[id.Name] = i
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				var rhs ast.Expr
				if i < len(n.Values) {
					rhs = n.Values[i]
				}
				l.assigns[id.Name] = append(l.assigns[id.Name], rhs)
			}
		}
		return true
	})
	return l
}

// rangeSource names where a ranged collection comes from at run time, and the
// local name (if any) a count check would mention. An empty source means the
// collection is static, a parameter, or of unknown origin.
func (r *run) rangeSource(s *scope, l *localDefs, x ast.Expr, depth int) (source, key string) {
	if depth > 4 {
		return "", ""
	}
	switch x := x.(type) {
	case *ast.ParenExpr:
		return r.rangeSource(s, l, x.X, depth+1)
	case *ast.CallExpr:
		callee := exprString(x.Fun)
		switch x.Fun.(type) {
		case *ast.ArrayType, *ast.MapType:
			if len(x.Args) == 1 {
				return r.rangeSource(s, l, x.Args[0], depth+1)
			}
			return "", ""
		}
		switch callee {
		case "make", "new", "append":
			return "", ""
		}
		if r.scalarResult(s, x, 0) {
			return "", ""
		}
		if passthrough[callee] && len(x.Args) >= 1 {
			return r.rangeSource(s, l, x.Args[0], depth+1)
		}
		if textSplit[callee] && len(x.Args) >= 1 {
			if _, lit := x.Args[0].(*ast.BasicLit); lit {
				return "", ""
			}
			if id, ok := x.Args[0].(*ast.Ident); ok && l.params[id.Name] {
				return "", ""
			}
			return textSource, ""
		}
		if sortedName.MatchString(callee) && len(x.Args) == 1 {
			return r.rangeSource(s, l, x.Args[0], depth+1)
		}
		return callee, ""
	case *ast.Ident:
		if l.params[x.Name] {
			return "", ""
		}
		rhss, local := l.assigns[x.Name]
		if !local && !l.appended[x.Name] {
			init, ok := s.file.pkg.vars[x.Name]
			if !ok || init == nil {
				return "", ""
			}
			src, _ := r.rangeSource(s, l, init, depth+1)
			return src, x.Name
		}
		for _, rhs := range rhss {
			if rhs == nil {
				continue
			}
			if call, ok := rhs.(*ast.CallExpr); ok && r.scalarResult(s, call, l.index[x.Name]) {
				continue
			}
			if id, ok := rhs.(*ast.Ident); ok && id.Name == x.Name {
				continue
			}
			if src, _ := r.rangeSource(s, l, rhs, depth+1); src != "" {
				return src, x.Name
			}
		}
		if l.appended[x.Name] && !l.always[x.Name] {
			return "accumulated by append", x.Name
		}
		return "", ""
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if p := r.a.t.imported(s.file, id.Name); p != nil {
				if init, ok := p.vars[x.Sel.Name]; ok && init != nil {
					if call, ok := init.(*ast.CallExpr); ok {
						return exprString(call.Fun), ""
					}
				}
			}
		}
	}
	return "", ""
}

func assignedNames(body ast.Node) map[string]bool {
	out := map[string]bool{}
	note := func(e ast.Expr) {
		if id := rootIdent(e); id != nil && id.Name != "_" && id.Name != "err" {
			out[id.Name] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range n.Lhs {
				note(lhs)
			}
		case *ast.IncDecStmt:
			note(n.X)
		}
		return true
	})
	return out
}

// containsLen reports whether len(key) appears under n.
func containsLen(n ast.Node, key string) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "len" && len(call.Args) == 1 && exprString(call.Args[0]) == key {
				found = true
			}
		}
		return !found
	})
	return found
}

// countChecked reports whether the function holds any sign that the loop's
// collection was counted: len(key) in a comparison or an if outside the loop
// header, key[i], the collection handed to something that can fail, or a
// variable the loop body updates that is read after the loop.
func (r *run) countChecked(s *scope, loop ast.Node, header ast.Node, body ast.Node, key string) bool {
	found := false
	carried := assignedNames(body)
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if header != nil && n == header {
			return false
		}
		switch n := n.(type) {
		case *ast.IfStmt:
			if key != "" && ((n.Init != nil && containsLen(n.Init, key)) || containsLen(n.Cond, key)) {
				found = true
				return false
			}
		case *ast.SwitchStmt:
			if key != "" && n.Tag != nil && containsLen(n.Tag, key) {
				found = true
				return false
			}
		case *ast.BinaryExpr:
			switch n.Op {
			case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
				if key != "" && containsLen(n, key) {
					found = true
					return false
				}
			}
		case *ast.CallExpr:
			if key == "" {
				return true
			}
			if fn, ok := n.Fun.(*ast.Ident); ok && (fn.Name == "append" || fn.Name == "len") {
				return true
			}
			for _, arg := range n.Args {
				if (exprString(arg) == key || containsLen(arg, key)) && r.a.reach(s, n) {
					found = true
					return false
				}
			}
		case *ast.IndexExpr:
			if id, ok := n.X.(*ast.Ident); ok && key != "" && id.Name == key {
				if _, isLit := n.Index.(*ast.BasicLit); isLit {
					found = true
					return false
				}
			}
		case *ast.Ident:
			if n.Pos() > loop.End() && carried[n.Name] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

var assertHelperName = regexp.MustCompile(`(?i)(check|assert|expect|verify|require|compare|want|equal|match|diff|refus|reject|same|judge|prove|pin|golden|agree|round|test|valid|control|must)`)

func pureErrCond(cond ast.Expr) bool {
	b, ok := cond.(*ast.BinaryExpr)
	return ok && b.Op == token.NEQ && isNil(b.Y) && errName.MatchString(exprString(b.X))
}

// realChecks reports whether n holds an assertion about behaviour, as opposed
// to set-up that stops the test when it cannot proceed: a failure not under a
// bare `if err != nil`, a helper that can fail and is named as a check, or a
// subtest holding either.
func (r *run) realChecks(s *scope, n ast.Node) bool {
	found := false
	var walk func(n ast.Node, errGuard bool)
	walk = func(n ast.Node, errGuard bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			if found || m == nil {
				return false
			}
			switch m := m.(type) {
			case *ast.IfStmt:
				if m.Init != nil {
					walk(m.Init, errGuard)
				}
				walk(m.Body, pureErrCond(m.Cond))
				if m.Else != nil {
					walk(m.Else, false)
				}
				return false
			case *ast.CallExpr:
				if directFail(s, m) {
					if !errGuard {
						found = true
					}
					return false
				}
				if subtestCall(s, m) != nil {
					return true
				}
				callee := exprString(m.Fun)
				if assertHelperName.MatchString(callee[strings.LastIndex(callee, ".")+1:]) && r.a.reach(s, m) {
					found = true
					return false
				}
			}
			return true
		})
	}
	walk(n, false)
	return found
}

func (r *run) rule2(s *scope) {
	var l *localDefs
	defs := func() *localDefs {
		if l == nil {
			l = collectDefs(s.decl)
		}
		return l
	}
	name := s.decl.Name.Name
	var stack []ast.Node
	hoisted := map[string]bool{}
	report := func(loop ast.Node, source, key string) {
		class := "2"
		if source == textSource {
			class = "2t"
		}
		r.add(class, loop, name, gatedSource(source), "assertions run only inside a loop over %s, and nothing checks that it is non-empty", source)
		if len(stack) < 2 {
			return
		}
		if _, labeled := stack[len(stack)-2].(*ast.LabeledStmt); labeled {
			return
		}
		f := &r.findings[len(r.findings)-1]
		f.fixT, f.fixAt = testingName(s.decl, stack), r.a.t.fset.Position(loop.Pos()).Offset
		if key != "" {
			f.fixKey = key
			return
		}
		// an unnamed call: name it, so that it can be counted
		rs, ok := loop.(*ast.RangeStmt)
		if !ok {
			return
		}
		call, ok := rs.X.(*ast.CallExpr)
		if !ok || !r.collectionResult(s, call) {
			return
		}
		callee := exprString(call.Fun)
		last := callee[strings.LastIndex(callee, ".")+1:]
		hoist := "all" + strings.ToUpper(last[:1]) + last[1:]
		for hoisted[hoist] || usesName(s.decl, hoist) {
			hoist += "s"
		}
		hoisted[hoist] = true
		f.fixKey, f.fixHoist = hoist, true
		f.fixX0, f.fixX1 = r.a.t.fset.Position(call.Pos()).Offset, r.a.t.fset.Position(call.End()).Offset
	}
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.RangeStmt:
			if !r.realChecks(s, n.Body) || reportsEach(s, n.Body) {
				return true
			}
			source, key := r.rangeSource(s, defs(), n.X, 0)
			if source == "" || r.countChecked(s, n, n.X, n.Body, key) {
				return true
			}
			report(n, source, key)
		case *ast.ForStmt:
			if n.Cond == nil || !r.realChecks(s, n.Body) {
				return true
			}
			source, key := "", ""
			switch c := n.Cond.(type) {
			case *ast.CallExpr:
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Scan" || sel.Sel.Name == "Next") {
					source = exprString(c.Fun)
				}
			case *ast.BinaryExpr:
				if call, ok := c.Y.(*ast.CallExpr); ok && exprString(call.Fun) == "len" && len(call.Args) == 1 {
					source, key = r.rangeSource(s, defs(), call.Args[0], 0)
				}
			}
			if source == "" || r.countChecked(s, n, n.Cond, n.Body, key) {
				return true
			}
			report(n, source, key)
		case *ast.CallExpr:
			callee := exprString(n.Fun)
			if callee != "filepath.Walk" && callee != "filepath.WalkDir" && callee != "fs.WalkDir" {
				return true
			}
			for _, arg := range n.Args {
				lit, ok := arg.(*ast.FuncLit)
				if !ok || !r.realChecks(s, lit.Body) || r.countChecked(s, n, nil, lit.Body, "") {
					continue
				}
				report(n, callee, "")
			}
		}
		return true
	})
}

// testingName is the name of the innermost testing parameter in scope at the
// top of stack, or "" when the enclosing functions take none.
func testingName(decl *ast.FuncDecl, stack []ast.Node) string {
	of := func(ft *ast.FuncType) string {
		if ft.Params == nil {
			return ""
		}
		for _, f := range ft.Params.List {
			if isTestingType(f.Type) {
				for _, n := range f.Names {
					if n.Name != "_" {
						return n.Name
					}
				}
			}
		}
		return ""
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if lit, ok := stack[i].(*ast.FuncLit); ok {
			if name := of(lit.Type); name != "" {
				return name
			}
		}
	}
	return of(decl.Type)
}

// textSource names a collection cut from run-time text.
const textSource = "run-time text"

var sortedName = regexp.MustCompile(`(?i)^(sorted|sort|keys)[A-Za-z]*$`)

// reportsEach reports whether a loop body fails on every iteration: a loop
// over a list of problems, where empty is the pass.
func reportsEach(s *scope, body *ast.BlockStmt) bool {
	for _, st := range body.List {
		if es, ok := st.(*ast.ExprStmt); ok {
			if call, ok := es.X.(*ast.CallExpr); ok && directFail(s, call) {
				return true
			}
		}
	}
	return false
}

// gatedSource reports whether a loop source is a filesystem listing or a
// registry read: the zero-iteration passes the gate refuses.
func gatedSource(source string) bool {
	return fsSources[source] || source == "languages.All" || strings.HasSuffix(source, ".Targets")
}

// ---- class 3: assertions behind a guard that may never open ----

var errName = regexp.MustCompile(`(?i)(^|\.)(e|err|[a-z0-9_]*err(or)?[0-9]*)(\(\))?$`)

func isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

func (r *run) guardKind(s *scope, l *localDefs, cond ast.Expr) string {
	switch c := cond.(type) {
	case *ast.ParenExpr:
		return r.guardKind(s, l, c.X)
	case *ast.BinaryExpr:
		switch c.Op {
		case token.LAND:
			if k := r.guardKind(s, l, c.X); k != "" {
				return k
			}
			return r.guardKind(s, l, c.Y)
		case token.EQL, token.NEQ:
			other := c.X
			if isNil(c.X) {
				other = c.Y
			}
			if isNil(c.X) || isNil(c.Y) {
				isErr := errName.MatchString(exprString(other))
				switch {
				case isErr && c.Op == token.EQL:
					return "err == nil"
				case isErr:
					return "err != nil"
				case c.Op == token.NEQ:
					return "x != nil"
				}
				return ""
			}
		}
		text := exprString(c)
		switch {
		case strings.Contains(text, "runtime.GOOS") || strings.Contains(text, "runtime.GOARCH"):
			return "runtime.GOOS"
		case strings.Contains(text, "os.Getenv(") || strings.Contains(text, "os.LookupEnv("):
			return "environment"
		}
		if c.Op == token.GTR || c.Op == token.NEQ || c.Op == token.GEQ {
			if call, ok := c.X.(*ast.CallExpr); ok && exprString(call.Fun) == "len" {
				if lit, ok := c.Y.(*ast.BasicLit); ok && (lit.Value == "0" || (lit.Value == "1" && c.Op == token.GEQ)) {
					return "len(x) > 0"
				}
			}
		}
		for _, side := range []ast.Expr{c.X, c.Y} {
			if id, ok := side.(*ast.Ident); ok && r.fromEnv(l, id.Name) {
				return "environment"
			}
		}
	case *ast.Ident:
		if c.Name == "ok" {
			return "ok"
		}
		if r.fromEnv(l, c.Name) {
			return "environment"
		}
	case *ast.StarExpr:
		if id, ok := c.X.(*ast.Ident); ok {
			if init, ok := s.file.pkg.vars[id.Name]; ok && init != nil && strings.HasPrefix(exprString(init), "flag.") {
				return "flag"
			}
		}
	case *ast.CallExpr:
		if exprString(c.Fun) == "testing.Short" {
			return "testing.Short"
		}
	case *ast.UnaryExpr:
		if c.Op == token.NOT {
			if call, ok := c.X.(*ast.CallExpr); ok && exprString(call.Fun) == "testing.Short" {
				return "testing.Short"
			}
		}
	}
	return ""
}

func (r *run) fromEnv(l *localDefs, name string) bool {
	for _, rhs := range l.assigns[name] {
		if rhs == nil {
			continue
		}
		text := exprString(rhs)
		if strings.HasPrefix(text, "os.Getenv(") || strings.HasPrefix(text, "os.LookupEnv(") {
			return true
		}
	}
	return false
}

func hasSkip(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Skip") {
				found = true
			}
		}
		return !found
	})
	return found
}

// conditionalAssertions reports whether a guarded block holds assertions that
// are themselves conditional: a nested check, or a helper that may fail. A
// block that is nothing but t.Fatal is the assertion, not a guard around one.
func (r *run) conditionalAssertions(s *scope, body *ast.BlockStmt) bool {
	for _, st := range body.List {
		if es, ok := st.(*ast.ExprStmt); ok {
			if call, ok := es.X.(*ast.CallExpr); ok && directFail(s, call) {
				continue
			}
		}
		if r.a.reach(s, st) {
			return true
		}
	}
	return false
}

func (r *run) rule3(s *scope) {
	l := collectDefs(s.decl)
	visitList := func(list []ast.Stmt) {
		for i, st := range list {
			ifs, ok := st.(*ast.IfStmt)
			if !ok || ifs.Else != nil {
				continue
			}
			kind := r.guardKind(s, l, ifs.Cond)
			if kind == "" || hasSkip(ifs.Body) || !r.conditionalAssertions(s, ifs.Body) {
				continue
			}
			// the guarded block leaves, and what follows asserts: the rest of the
			// list is the else branch
			if terminates(s, ifs.Body) && r.anyReach(s, list[i+1:]) {
				continue
			}
			// an earlier statement already failed the test on the opposite condition
			if r.complementHandled(s, list[:i], ifs.Cond) {
				continue
			}
			r.add("3g", ifs, s.decl.Name.Name, false, "assertions run only if %s, with no else: guard %s", exprString(ifs.Cond), kind)
		}
	}
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			visitList(n.List)
		case *ast.CaseClause:
			visitList(n.Body)
		case *ast.CommClause:
			visitList(n.Body)
		}
		return true
	})
	if _, isTest := isTestFunc(s.decl); isTest {
		r.onlyPath(s, s.decl.Body.List)
	}
}

func (r *run) anyReach(s *scope, list []ast.Stmt) bool {
	for _, st := range list {
		if r.a.reach(s, st) {
			return true
		}
	}
	return false
}

// terminates reports whether a block always leaves the enclosing list: its
// last statement is a return, a branch, or a call that stops the test.
func terminates(s *scope, body *ast.BlockStmt) bool {
	if len(body.List) == 0 {
		return false
	}
	switch last := body.List[len(body.List)-1].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		call, ok := last.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			return id.Name == "panic"
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			switch sel.Sel.Name {
			case "Fatal", "Fatalf", "FailNow", "Exit", "Skip", "Skipf", "SkipNow":
				return true
			}
		}
	}
	return false
}

func complement(cond ast.Expr) string {
	switch c := cond.(type) {
	case *ast.ParenExpr:
		return complement(c.X)
	case *ast.BinaryExpr:
		switch c.Op {
		case token.EQL:
			return exprString(c.X) + " != " + exprString(c.Y)
		case token.NEQ:
			return exprString(c.X) + " == " + exprString(c.Y)
		case token.GTR:
			return exprString(c.X) + " == " + exprString(c.Y)
		}
	case *ast.UnaryExpr:
		if c.Op == token.NOT {
			return exprString(c.X)
		}
	case *ast.Ident:
		return "!" + c.Name
	}
	return ""
}

// complementHandled reports whether an earlier statement of the same list
// stops the test when cond is false, so the guard cannot be closed here.
func (r *run) complementHandled(s *scope, before []ast.Stmt, cond ast.Expr) bool {
	var want []string
	var conjuncts func(e ast.Expr)
	conjuncts = func(e ast.Expr) {
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.LAND {
			conjuncts(b.X)
			conjuncts(b.Y)
			return
		}
		if c := complement(e); c != "" {
			want = append(want, c)
		}
	}
	conjuncts(cond)
	for _, st := range before {
		ifs, ok := st.(*ast.IfStmt)
		if !ok || !terminates(s, ifs.Body) {
			continue
		}
		have := exprString(ifs.Cond)
		for _, w := range want {
			if have == w || strings.Contains(have, w) {
				return true
			}
		}
	}
	return false
}

// onlyPath follows a test whose every assertion sits under one statement, and
// reports when that statement is a switch that asserts only in its default.
func (r *run) onlyPath(s *scope, list []ast.Stmt) {
	var only ast.Stmt
	for _, st := range list {
		if r.a.reach(s, st) {
			if only != nil {
				return
			}
			only = st
		}
	}
	switch st := only.(type) {
	case *ast.BlockStmt:
		r.onlyPath(s, st.List)
	case *ast.ForStmt:
		r.onlyPath(s, st.Body.List)
	case *ast.RangeStmt:
		r.onlyPath(s, st.Body.List)
	case *ast.SwitchStmt:
		r.switchDefaultOnly(s, st, st.Body)
	case *ast.TypeSwitchStmt:
		r.switchDefaultOnly(s, st, st.Body)
	}
}

func (r *run) switchDefaultOnly(s *scope, sw ast.Stmt, body *ast.BlockStmt) {
	defaultReaches, otherReaches := false, false
	for _, c := range body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			continue
		}
		reaches := false
		for _, st := range cc.Body {
			reaches = reaches || r.a.reach(s, st)
		}
		if cc.List == nil {
			defaultReaches = reaches
		} else {
			otherReaches = otherReaches || reaches
		}
	}
	if defaultReaches && !otherReaches {
		r.add("3d", sw, s.decl.Name.Name, false, "the test's only assertions are in a switch default")
	}
}

// rule3early reports a bare return that leaves a test before its first assertion.
func (r *run) rule3early(s *scope, name string, body *ast.BlockStmt) {
	for _, st := range body.List {
		if r.a.reach(s, st) {
			return
		}
		ifs, ok := st.(*ast.IfStmt)
		if !ok || len(ifs.Body.List) == 0 || hasSkip(ifs.Body) {
			continue
		}
		if ret, ok := ifs.Body.List[len(ifs.Body.List)-1].(*ast.ReturnStmt); ok {
			r.add("3r", ret, name, false, "returns before any assertion when %s", exprString(ifs.Cond))
		}
	}
}

// ---- class 4: dropped errors ----

type signature struct {
	results int
	errAt   []int
	origin  string // "product", "helper", "exec", "stdlib"
	boolAt  []int  // results that are a bool verdict
}

// stdlibErrors lists standard-library functions by import path and name: the
// number of results and the index of the error.
var stdlibErrors = map[string][2]int{
	"os.ReadFile": {2, 1}, "os.WriteFile": {1, 0}, "os.MkdirAll": {1, 0}, "os.Mkdir": {1, 0}, "os.Remove": {1, 0},
	"os.RemoveAll": {1, 0}, "os.Rename": {1, 0}, "os.Chdir": {1, 0}, "os.Setenv": {1, 0}, "os.Unsetenv": {1, 0},
	"os.Chmod": {1, 0}, "os.Symlink": {1, 0}, "os.Stat": {2, 1}, "os.Lstat": {2, 1}, "os.Open": {2, 1},
	"os.Create": {2, 1}, "os.OpenFile": {2, 1}, "os.ReadDir": {2, 1}, "os.Getwd": {2, 1}, "os.MkdirTemp": {2, 1},
	"os.CreateTemp": {2, 1}, "os.Truncate": {1, 0}, "os.Link": {1, 0}, "os.Chtimes": {1, 0},
	"path/filepath.Glob": {2, 1}, "path/filepath.Abs": {2, 1}, "path/filepath.Rel": {2, 1}, "path/filepath.Walk": {1, 0},
	"path/filepath.WalkDir": {1, 0}, "path/filepath.EvalSymlinks": {2, 1}, "io/fs.WalkDir": {1, 0}, "io/fs.ReadDir": {2, 1},
	"io/fs.ReadFile": {2, 1}, "io/fs.Glob": {2, 1},
	"encoding/json.Marshal": {2, 1}, "encoding/json.MarshalIndent": {2, 1}, "encoding/json.Unmarshal": {1, 0},
	"strconv.Atoi": {2, 1}, "strconv.ParseInt": {2, 1}, "strconv.ParseUint": {2, 1}, "strconv.ParseFloat": {2, 1},
	"strconv.ParseBool": {2, 1}, "strconv.Unquote": {2, 1},
	"io.ReadAll": {2, 1}, "io.Copy": {2, 1}, "io.ReadFull": {2, 1},
	"os/exec.LookPath":          {2, 1},
	"encoding/hex.DecodeString": {2, 1}, "encoding/base64.StdEncoding.DecodeString": {2, 1},
	"regexp.Compile": {2, 1}, "regexp.MatchString": {2, 1}, "time.Parse": {2, 1},
	"encoding/binary.Read": {1, 0}, "encoding/binary.Write": {1, 0},
	"go/format.Source": {2, 1}, "go/parser.ParseFile": {2, 1},
}

// execMethods are the methods of exec.Cmd that report how the child ended.
var execMethods = map[string][2]int{
	"Run": {1, 0}, "Start": {1, 0}, "Wait": {1, 0}, "Output": {2, 1}, "CombinedOutput": {2, 1},
}

// ioMethodNames are method names so common on standard-library types that a
// match by name against a type of this tree would be a guess.
var ioMethodNames = map[string]bool{
	"Write": true, "WriteString": true, "WriteByte": true, "WriteRune": true, "Close": true, "Read": true,
	"Flush": true, "Sync": true, "Run": true, "Start": true, "Wait": true, "Output": true, "CombinedOutput": true,
	"Encode": true, "Decode": true, "Execute": true, "Seek": true, "Set": true, "Scan": true, "Err": true,
}

func declSignature(d *ast.FuncDecl) (results int, errAt []int) {
	return typeSignature(d.Type)
}

func typeSignature(ft *ast.FuncType) (results int, errAt []int) {
	if ft.Results == nil {
		return 0, nil
	}
	for _, f := range ft.Results.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			if id, ok := f.Type.(*ast.Ident); ok && id.Name == "error" {
				errAt = append(errAt, results)
			}
			results++
		}
	}
	return results, errAt
}

func (r *run) origin(d *ast.FuncDecl) string {
	if sf := r.a.t.declAt[d]; sf != nil && sf.isTest {
		return "helper"
	}
	return "product"
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *run) agreed(decls []*ast.FuncDecl) (signature, bool) {
	if len(decls) == 0 {
		return signature{}, false
	}
	n, errAt := declSignature(decls[0])
	for _, d := range decls[1:] {
		n2, errAt2 := declSignature(d)
		if n2 != n || !sameInts(errAt, errAt2) {
			return signature{}, false
		}
	}
	if len(errAt) == 0 {
		return signature{}, false
	}
	return signature{results: n, errAt: errAt, origin: r.origin(decls[0]), boolAt: boolResults(decls[0].Type)}, true
}

func isExecCommand(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch exprString(call.Fun) {
	case "exec.Command", "exec.CommandContext", "testproc.Command", "testproc.CommandContext":
		return true
	}
	return false
}

func (r *run) signature(s *scope, l *localDefs, call *ast.CallExpr) (signature, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if lit, ok := s.closures[fun.Name]; ok {
			n, errAt := typeSignature(lit.Type)
			if len(errAt) == 0 {
				return signature{}, false
			}
			return signature{results: n, errAt: errAt, origin: "helper"}, true
		}
		if l.params[fun.Name] {
			return signature{}, false
		}
		if _, shadowed := l.assigns[fun.Name]; shadowed {
			return signature{}, false
		}
		return r.agreed(s.file.pkg.funcs[fun.Name])
	case *ast.SelectorExpr:
		name := fun.Sel.Name
		if x, ok := fun.X.(*ast.Ident); ok {
			if ip, isPkg := s.file.imports[x.Name]; isPkg {
				if p := r.a.t.imported(s.file, x.Name); p != nil {
					return r.agreed(p.funcs[name])
				}
				if sig, ok := stdlibErrors[ip+"."+name]; ok {
					return signature{results: sig[0], errAt: []int{sig[1]}, origin: "stdlib"}, true
				}
				return signature{}, false
			}
			if s.tnames[x.Name] {
				return signature{}, false
			}
		}
		if sig, ok := execMethods[name]; ok {
			isCmd := isExecCommand(fun.X)
			if id, ok := fun.X.(*ast.Ident); ok {
				for _, rhs := range l.assigns[id.Name] {
					isCmd = isCmd || isExecCommand(rhs)
				}
			}
			if isCmd {
				return signature{results: sig[0], errAt: []int{sig[1]}, origin: "exec"}, true
			}
		}
		if ioMethodNames[name] {
			return signature{}, false
		}
		var decls []*ast.FuncDecl
		decls = append(decls, s.file.pkg.methods[name]...)
		for local := range s.file.imports {
			if p := r.a.t.imported(s.file, local); p != nil {
				decls = append(decls, p.methods[name]...)
			}
		}
		return r.agreed(decls)
	}
	return signature{}, false
}

func (r *run) rule4(s *scope) {
	l := collectDefs(s.decl)
	name := s.decl.Name.Name
	class := map[string]string{"product": "4p", "exec": "4x", "helper": "4h", "stdlib": "4s"}
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Rhs) != 1 {
				return true
			}
			call, ok := n.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			sig, ok := r.signature(s, l, call)
			if !ok || sig.results != len(n.Lhs) || verdictKept(sig, n.Lhs) {
				return true
			}
			for _, i := range sig.errAt {
				if isBlank(n.Lhs[i]) {
					r.add(class[sig.origin], n, name, sig.origin != "stdlib", "the error from %s is assigned to _", exprString(call.Fun))
					break
				}
			}
		case *ast.ExprStmt:
			call, ok := n.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sig, ok := r.signature(s, l, call); ok {
				r.add(class[sig.origin], n, name, sig.origin != "stdlib", "the error from %s is not looked at", exprString(call.Fun))
			}
		}
		return true
	})
}

// ---- class 5: comparisons that cannot fail ----

var equalFuncs = map[string]bool{
	"reflect.DeepEqual": true, "bytes.Equal": true, "slices.Equal": true, "maps.Equal": true, "bytes.Compare": true,
	"strings.EqualFold": true, "cmp.Equal": true, "strings.Compare": true,
}

func (r *run) rule5(s *scope) {
	name := s.decl.Name.Name
	l := collectDefs(s.decl)
	sameCall := func(x, y ast.Expr) string {
		xi, ok1 := x.(*ast.Ident)
		yi, ok2 := y.(*ast.Ident)
		if !ok1 || !ok2 || xi.Name == yi.Name || len(l.assigns[xi.Name]) != 1 || len(l.assigns[yi.Name]) != 1 {
			return ""
		}
		xc, ok1 := l.assigns[xi.Name][0].(*ast.CallExpr)
		yc, ok2 := l.assigns[yi.Name][0].(*ast.CallExpr)
		if !ok1 || !ok2 || xc == yc || exprString(xc) != exprString(yc) {
			return ""
		}
		return exprString(xc)
	}
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if n.Op != token.EQL && n.Op != token.NEQ {
				return true
			}
			if _, lit := n.X.(*ast.BasicLit); lit {
				return true
			}
			if exprString(n.X) == exprString(n.Y) {
				r.add("5s", n, name, false, "%s compares a value with itself", exprString(n))
			} else if c := sameCall(n.X, n.Y); c != "" {
				r.add("5c", n, name, false, "both sides of %s come from the same call %s", exprString(n), c)
			}
		case *ast.CallExpr:
			if len(n.Args) < 2 {
				return true
			}
			callee := exprString(n.Fun)
			if !equalFuncs[callee] {
				return true
			}
			if exprString(n.Args[0]) == exprString(n.Args[1]) {
				r.add("5s", n, name, false, "%s compares a value with itself", exprString(n))
			} else if c := sameCall(n.Args[0], n.Args[1]); c != "" {
				r.add("5c", n, name, false, "both arguments of %s come from the same call %s", callee, c)
			}
		}
		return true
	})
}

// ---- class 6: subtests that run after their parent stopped looking ----

func callsParallel(lit *ast.FuncLit) bool {
	found := false
	for _, st := range lit.Body.List {
		if es, ok := st.(*ast.ExprStmt); ok {
			if call, ok := es.X.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
					found = true
				}
			}
		}
	}
	return found
}

func (r *run) rule6(s *scope) {
	name := s.decl.Name.Name
	// a subtest registered from a goroutine
	ast.Inspect(s.decl.Body, func(n ast.Node) bool {
		gs, ok := n.(*ast.GoStmt)
		if !ok {
			return true
		}
		ast.Inspect(gs.Call, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && subtestCall(s, call) != nil {
				r.add("6g", call, name, false, "subtest %s is registered from a goroutine", exprString(call.Args[0]))
			}
			return true
		})
		return true
	})
	// parallel subtests whose results the parent reads straight after registering them:
	// a parallel subtest only starts once the parent function has returned
	blocks := func(list []ast.Stmt) {
		for i, st := range list {
			var parallel *ast.FuncLit
			ast.Inspect(st, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if lit := subtestCall(s, call); lit != nil && callsParallel(lit) {
						parallel = lit
					}
				}
				return parallel == nil
			})
			if parallel == nil {
				continue
			}
			// what the subtest writes that it did not itself declare
			carried := assignedNames(parallel.Body)
			ast.Inspect(parallel.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.AssignStmt:
					if n.Tok == token.DEFINE {
						for _, lhs := range n.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								delete(carried, id.Name)
							}
						}
					}
				case *ast.ValueSpec:
					for _, id := range n.Names {
						delete(carried, id.Name)
					}
				}
				return true
			})
			for _, after := range list[i+1:] {
				used := ""
				ast.Inspect(after, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok && subtestCall(s, call) != nil {
						return false // a later subtest is not the parent
					}
					if id, ok := n.(*ast.Ident); ok && carried[id.Name] {
						used = id.Name
					}
					return used == ""
				})
				if used != "" {
					r.add("6p", after, name, false, "reads %s, which parallel subtests write only after this function returns", used)
					break
				}
			}
		}
	}
	blocks(s.decl.Body.List)
}

// ---- class 7: a negative control that never sees the failure ----

var negativeWords = map[string]bool{
	"refuse": true, "refuses": true, "refused": true, "refusal": true, "refusals": true,
	"red": true, "fail": true, "fails": true, "failure": true, "failures": true, "failing": true,
	"control": true, "controls": true, "reject": true, "rejects": true, "rejected": true, "negative": true,
}

var wordSplit = regexp.MustCompile(`[A-Z]+[a-z0-9]*|[a-z0-9]+`)

func nameWords(name string) []string {
	var out []string
	for _, w := range wordSplit.FindAllString(name, -1) {
		out = append(out, strings.ToLower(w))
	}
	return out
}

var failureHelperName = regexp.MustCompile(`(?i)(refus|reject|fail|invalid|error|errs?$|err[A-Z_]|deny|denied|panic|control|negative|notok|bad|sabotage|mutat|red$|red[A-Z]|wantErr|exit)`)

// seesFailure is the evidence that a test looked at a failure: an error or
// exit status compared, an error inspected, a recovered panic, a diagnostic
// matched, or a helper whose name says it expects a failure.
func seesFailure(s *scope, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		// the test fails when the operation SUCCEEDED: that is looking at a failure
		if !acceptingCond(n.Cond) {
			return false
		}
		fails := false
		ast.Inspect(n.Body, func(m ast.Node) bool {
			if call, ok := m.(*ast.CallExpr); ok && directFail(s, call) {
				fails = true
			}
			return !fails
		})
		return fails
	case *ast.CallExpr:
		callee := exprString(n.Fun)
		last := callee[strings.LastIndex(callee, ".")+1:]
		switch callee {
		case "errors.Is", "errors.As", "recover":
			return true
		}
		if failMethods[last] {
			return false
		}
		return last == "Failed" || last == "ExitCode" || failureHelperName.MatchString(last)
	}
	return false
}

// acceptingCond reports whether a condition holds when the operation under
// test was ACCEPTED: an error that is nil, a verdict that is true, a count or
// status that is not the refusal, a diagnostic that is missing. `err != nil`
// and `got != want` are the shapes of a test that expects success.
func acceptingCond(e ast.Expr) bool {
	switch c := e.(type) {
	case *ast.ParenExpr:
		return acceptingCond(c.X)
	case *ast.Ident:
		return c.Name != "true" && c.Name != "false"
	case *ast.CallExpr, *ast.SelectorExpr:
		return true
	case *ast.UnaryExpr:
		if c.Op != token.NOT {
			return false
		}
		if _, isCall := c.X.(*ast.CallExpr); isCall {
			return true // !strings.Contains(out, "refused"), !errors.Is(err, want), !refused(x)
		}
		return failureWord.MatchString(exprString(c.X))
	case *ast.BinaryExpr:
		switch c.Op {
		case token.LAND, token.LOR:
			return acceptingCond(c.X) || acceptingCond(c.Y)
		case token.EQL, token.GEQ, token.GTR, token.LSS, token.LEQ:
			return true
		case token.NEQ:
			if isNil(c.X) || isNil(c.Y) {
				return false
			}
			for _, pair := range [][2]ast.Expr{{c.X, c.Y}, {c.Y, c.X}} {
				if lit, ok := pair[0].(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value != "0" && failureWord.MatchString(exprString(pair[1])) {
					return true
				}
			}
			return refusalName.MatchString(exprString(c.X)) || refusalName.MatchString(exprString(c.Y))
		}
	}
	return false
}

// failureWord matches an expression that names a failure count or verdict.
var failureWord = regexp.MustCompile(`(?i)(exit|code|status|fail|finding|problem|diag|violation|refus|reject|errs|errors|red\b|bad|denied|mismatch)`)

// refusalName matches an operand that names the refusal a test expects.
var refusalName = regexp.MustCompile(`(?i)(refus|invalid|reject|malformed|damage|fail|denied|wantErr|errWant|sentinel|Err[A-Z])`)

func (r *run) rule7(s *scope) {
	negative := ""
	for _, w := range nameWords(s.decl.Name.Name) {
		if negativeWords[w] {
			negative = w
			break
		}
	}
	if negative == "" {
		return
	}
	if r.a.evidence(s, s.decl.Body, seesFailure, map[*ast.FuncDecl]bool{}) {
		return
	}
	r.add("7", s.decl, s.decl.Name.Name, true, "named as a negative control (%q) but never looks at a failure: no error, exit status, diagnostic or recovered panic is checked", negative)
}

// boolResults lists the results of a function that are a bool.
func boolResults(ft *ast.FuncType) []int {
	var at []int
	if ft.Results == nil {
		return nil
	}
	i := 0
	for _, f := range ft.Results.List {
		n := max(len(f.Names), 1)
		for range n {
			if id, ok := f.Type.(*ast.Ident); ok && id.Name == "bool" {
				at = append(at, i)
			}
			i++
		}
	}
	return at
}

// verdictKept reports whether a call that answers (ok bool, err error) had its
// bool kept: the verdict is looked at, and the error only says why.
func verdictKept(sig signature, lhs []ast.Expr) bool {
	for _, i := range sig.boolAt {
		if !isBlank(lhs[i]) {
			return true
		}
	}
	return false
}

// scalarTypes are result types that are not a collection to range over.
var scalarTypes = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "uintptr": true, "float32": true, "float64": true, "bool": true, "string": true,
	"error": true, "byte": true, "rune": true,
}

// scalarResult reports whether result idx of a call is known to be a number, a
// string or a bool: a conversion to one, or a function of this tree declared
// to return one. Ranging over it is counting, not walking a collection.
func (r *run) scalarResult(s *scope, call *ast.CallExpr, idx int) bool {
	var decls []*ast.FuncDecl
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if scalarTypes[fun.Name] {
			return true
		}
		decls = s.file.pkg.funcs[fun.Name]
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			if _, isPkg := s.file.imports[x.Name]; isPkg {
				if p := r.a.t.imported(s.file, x.Name); p != nil {
					decls = p.funcs[fun.Sel.Name]
				}
				break
			}
		}
		decls = s.file.pkg.methods[fun.Sel.Name]
	}
	if len(decls) == 0 {
		return false
	}
	for _, d := range decls {
		if d.Type.Results == nil {
			return false
		}
		i := 0
		scalar := false
		for _, f := range d.Type.Results.List {
			for range max(len(f.Names), 1) {
				if id, ok := f.Type.(*ast.Ident); ok && i == idx && scalarTypes[id.Name] {
					scalar = true
				}
				i++
			}
		}
		if !scalar {
			return false
		}
	}
	return true
}

// usesName reports whether a function already mentions an identifier.
func usesName(d *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(d, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// collectionResult reports whether a call is known to answer one slice, array
// or map: a function or method of this tree declared so.
func (r *run) collectionResult(s *scope, call *ast.CallExpr) bool {
	var decls []*ast.FuncDecl
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		decls = s.file.pkg.funcs[fun.Name]
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			if _, isPkg := s.file.imports[x.Name]; isPkg {
				if p := r.a.t.imported(s.file, x.Name); p != nil {
					decls = p.funcs[fun.Sel.Name]
				}
				break
			}
		}
		decls = append(decls, s.file.pkg.methods[fun.Sel.Name]...)
		for local := range s.file.imports {
			if p := r.a.t.imported(s.file, local); p != nil {
				decls = append(decls, p.methods[fun.Sel.Name]...)
			}
		}
	}
	if len(decls) == 0 {
		return false
	}
	for _, d := range decls {
		if d.Type.Results == nil || len(d.Type.Results.List) != 1 || len(d.Type.Results.List[0].Names) > 1 {
			return false
		}
		switch d.Type.Results.List[0].Type.(type) {
		case *ast.ArrayType, *ast.MapType:
		default:
			return false
		}
	}
	return true
}
