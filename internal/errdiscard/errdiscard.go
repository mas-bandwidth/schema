// Package errdiscard finds an error a Go program drops: a call whose error
// result is assigned to _, or whose results are thrown away by an expression,
// defer or go statement.
//
// NOTHING IS ALLOWED TO FAIL SILENTLY (the owner, 2026-11-22). A tool under
// cmd/ or tools/ that drops the error of a write, a close, a flag parse or a
// child process prints its verdict over work that did not happen. The type
// checker knows which result is an error, so the gate does not guess from
// names: it compiles nothing itself, reads the export data `go list -export`
// leaves in the build cache, and type-checks the packages it was pointed at.
//
// THE ONLY EXCEPTIONS are writes that cannot fail (a strings.Builder, a
// bytes.Buffer, a hash); output to the process's own stdout and stderr, as
// os.Stdout and os.Stderr or as an io.Writer parameter named stdout or
// stderr (a tool that cannot print its report has nowhere left to say so;
// its exit status still carries the verdict); Parse on a flag set made with
// flag.ExitOnError, which never returns an error; Close on a handle only
// os.Open or os.OpenRoot assigned, which has nothing unwritten to lose; and a
// site that names its reason in a comment on the same line or the line above:
//
//	_ = os.Remove(tmp) // errdiscard: best-effort cleanup of a scratch file; the run already failed
//
// A reason is a ledger entry the reviewer reads, not a mute: an empty reason
// is refused.
package errdiscard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Marker is the comment word that names a kept discard's reason.
const Marker = "errdiscard:"

// A Finding is one dropped error.
type Finding struct {
	File   string // relative to the directory Check ran in, slash-separated
	Line   int
	Call   string // the callee as written
	How    string // "assigned to _", "not looked at", "deferred", "dropped by go"
	Reason string // the errdiscard: reason, when the site names one
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: the error from %s is %s", f.File, f.Line, f.Call, f.How)
}

// Counts is what one run looked at, so a run over nothing cannot pass.
type Counts struct {
	Packages int
	Files    int
	Calls    int // calls whose results include an error
}

type listed struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Export     string
	DepOnly    bool
	Error      *struct{ Err string }
}

// Check type-checks the non-test files of the packages patterns names,
// run from dir, and returns every dropped error: refused ones (Reason empty)
// and kept ones (Reason set). It fails on a package it cannot list or check:
// a package it did not read is never a clean one.
func Check(dir string, patterns ...string) ([]Finding, Counts, error) {
	var c Counts
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, c, err
	}
	args := append([]string{"list", "-e", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,CgoFiles,Export,DepOnly,Error"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, c, fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	exports := map[string]string{}
	var targets []listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listed
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, c, fmt.Errorf("go list output: %w", err)
		}
		if p.Error != nil {
			return nil, c, fmt.Errorf("go list %s: %s", p.ImportPath, p.Error.Err)
		}
		exports[p.ImportPath] = p.Export
		if !p.DepOnly {
			targets = append(targets, p)
		}
	}
	if len(targets) == 0 {
		return nil, c, fmt.Errorf("the patterns %v name no package", patterns)
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok || file == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
	var findings []Finding
	for _, p := range targets {
		var files []*ast.File
		for _, name := range append(append([]string{}, p.GoFiles...), p.CgoFiles...) {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.ParseComments)
			if err != nil {
				return nil, c, err
			}
			files = append(files, f)
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		conf := types.Config{Importer: imp, FakeImportC: true}
		if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
			return nil, c, fmt.Errorf("type-check %s: %w", p.ImportPath, err)
		}
		c.Packages++
		c.Files += len(files)
		harmless := harmlessHandles(files, info)
		for _, f := range files {
			v := &visitor{fset: fset, info: info, dir: dir, file: f, counts: &c, harmless: harmless}
			ast.Walk(v, f)
			findings = append(findings, v.found...)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, c, nil
}

// Refused is the findings that name no reason.
func Refused(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Reason == "" {
			out = append(out, f)
		}
	}
	return out
}

type visitor struct {
	fset     *token.FileSet
	info     *types.Info
	dir      string
	file     *ast.File
	counts   *Counts
	harmless map[types.Object]bool
	found    []Finding
}

// harmlessHandles is every variable whose every assignment is
// flag.NewFlagSet(_, flag.ExitOnError), whose Parse exits on a bad flag and
// returns nil otherwise, or a read-only open (os.Open, os.OpenRoot), whose
// Close has nothing unwritten to lose: neither has an error to drop.
func harmlessHandles(files []*ast.File, info *types.Info) map[types.Object]bool {
	verdict := map[types.Object]bool{}
	note := func(id *ast.Ident, rhs ast.Expr) {
		obj := info.Defs[id]
		if obj == nil {
			obj = info.Uses[id]
		}
		if obj == nil {
			return
		}
		ok := false
		if call, isCall := ast.Unparen(rhs).(*ast.CallExpr); isCall && len(call.Args) == 2 &&
			types.ExprString(call.Fun) == "flag.NewFlagSet" && types.ExprString(call.Args[1]) == "flag.ExitOnError" {
			ok = true
		}
		if call, isCall := ast.Unparen(rhs).(*ast.CallExpr); isCall && readOnlyOpeners[types.ExprString(call.Fun)] {
			ok = true
		}
		if prev, seen := verdict[obj]; seen {
			// every assignment must be one of the two, or the variable is
			// something else at some point and its error counts
			ok = ok && prev
		}
		verdict[obj] = ok
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			switch {
			case len(as.Lhs) == len(as.Rhs):
				for i, l := range as.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						note(id, as.Rhs[i])
					}
				}
			case len(as.Rhs) == 1:
				// f, err := os.Open(p): the handle is the first result
				if id, ok := as.Lhs[0].(*ast.Ident); ok {
					note(id, as.Rhs[0])
				}
				for _, l := range as.Lhs[1:] {
					if id, ok := l.(*ast.Ident); ok {
						note(id, nil)
					}
				}
			}
			return true
		})
	}
	return verdict
}

// readOnlyOpeners return a handle that is only read through: its Close has
// nothing unwritten to lose, so a dropped Close error is no lost failure.
var readOnlyOpeners = map[string]bool{"os.Open": true, "os.OpenRoot": true}

var errorType = types.Universe.Lookup("error").Type()

// errorSlots is the indexes of call's results whose type is error.
func (v *visitor) errorSlots(call *ast.CallExpr) []int {
	tv, ok := v.info.Types[call]
	if !ok || tv.IsType() || tv.Type == nil {
		return nil
	}
	var slots []int
	switch t := tv.Type.(type) {
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if types.Identical(t.At(i).Type(), errorType) {
				slots = append(slots, i)
			}
		}
	default:
		if types.Identical(t, errorType) {
			slots = []int{0}
		}
	}
	if len(slots) > 0 {
		v.counts.Calls++
	}
	return slots
}

func (v *visitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(n.X).(*ast.CallExpr); ok && len(v.errorSlots(call)) > 0 {
			v.add(call, "not looked at")
		}
	case *ast.DeferStmt:
		if len(v.errorSlots(n.Call)) > 0 {
			v.add(n.Call, "dropped by defer")
		}
	case *ast.GoStmt:
		if len(v.errorSlots(n.Call)) > 0 {
			v.add(n.Call, "dropped by go")
		}
	case *ast.AssignStmt:
		v.assign(n.Lhs, n.Rhs)
	case *ast.ValueSpec:
		lhs := make([]ast.Expr, len(n.Names))
		for i, id := range n.Names {
			lhs[i] = id
		}
		v.assign(lhs, n.Values)
	}
	return v
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

func (v *visitor) assign(lhs, rhs []ast.Expr) {
	if len(rhs) == 1 && len(lhs) > 1 {
		call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr)
		if !ok {
			return
		}
		for _, i := range v.errorSlots(call) {
			if i < len(lhs) && isBlank(lhs[i]) {
				v.add(call, "assigned to _")
				return
			}
		}
		return
	}
	for i, r := range rhs {
		call, ok := ast.Unparen(r).(*ast.CallExpr)
		if !ok || i >= len(lhs) {
			continue
		}
		if slots := v.errorSlots(call); len(slots) > 0 && isBlank(lhs[i]) {
			v.add(call, "assigned to _")
		}
	}
}

// cannotFail reports whether the call is a write whose error is not a
// failure this program can have: into memory or a hash, or onto the
// process's own stdout or stderr.
func (v *visitor) cannotFail(call *ast.CallExpr) bool {
	callee := typeutilCallee(v.info, call)
	if fn, ok := callee.(*types.Func); ok && fn.Pkg() != nil {
		full := fn.Pkg().Path() + "." + fn.Name()
		switch full {
		case "fmt.Print", "fmt.Printf", "fmt.Println":
			return true
		case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln", "io.WriteString":
			return len(call.Args) > 0 && v.memoryOrStdio(call.Args[0])
		}
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if s := v.info.Selections[sel]; s != nil && s.Kind() == types.MethodVal {
			recv := types.TypeString(s.Recv(), nil)
			if (s.Obj().Name() == "Parse" && recv == "*flag.FlagSet") ||
				(s.Obj().Name() == "Close" && (recv == "*os.File" || recv == "*os.Root")) {
				if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok && v.harmless[v.info.Uses[id]] {
					return true
				}
			}
			return v.memoryOrStdio(sel.X)
		}
	}
	return false
}

// memoryOrStdio reports whether e is a writer that cannot fail or is the
// process's own stdout or stderr.
func (v *visitor) memoryOrStdio(e ast.Expr) bool {
	e = ast.Unparen(e)
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if obj, ok := v.info.Uses[sel.Sel].(*types.Var); ok && obj.Pkg() != nil && obj.Pkg().Path() == "os" && (obj.Name() == "Stdout" || obj.Name() == "Stderr") {
			return true
		}
	}
	tv, ok := v.info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	// the process's stdout and stderr, handed down as a function's io.Writer
	// so a test can capture them: the parameter's name says what it is
	if id, ok := e.(*ast.Ident); ok && (id.Name == "stdout" || id.Name == "stderr") &&
		types.TypeString(tv.Type, nil) == "io.Writer" {
		return true
	}
	switch types.TypeString(tv.Type, nil) {
	case "*strings.Builder", "strings.Builder", "*bytes.Buffer", "bytes.Buffer",
		"hash.Hash", "hash.Hash32", "hash.Hash64":
		return true
	}
	return false
}

// typeutilCallee is the function or method a call names, or nil for a call
// of a func value.
func typeutilCallee(info *types.Info, call *ast.CallExpr) types.Object {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return info.Uses[fun]
	case *ast.SelectorExpr:
		if s := info.Selections[fun]; s != nil {
			return s.Obj()
		}
		return info.Uses[fun.Sel]
	}
	return nil
}

func (v *visitor) add(call *ast.CallExpr, how string) {
	if v.cannotFail(call) {
		return
	}
	pos := v.fset.Position(call.Pos())
	rel, err := filepath.Rel(v.dir, pos.Filename)
	if err != nil {
		rel = pos.Filename
	}
	v.found = append(v.found, Finding{
		File:   filepath.ToSlash(rel),
		Line:   pos.Line,
		Call:   types.ExprString(call.Fun),
		How:    how,
		Reason: v.reason(pos.Line),
	})
}

// reason is the text after "errdiscard:" in a comment on line or the line
// above it.
func (v *visitor) reason(line int) string {
	for _, cg := range v.file.Comments {
		for _, c := range cg.List {
			at := v.fset.Position(c.Pos()).Line
			if at != line && at != line-1 {
				continue
			}
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
			if _, after, ok := strings.Cut(text, Marker); ok {
				if r := strings.TrimSpace(strings.TrimSuffix(after, "*/")); r != "" {
					return r
				}
			}
		}
	}
	return ""
}
