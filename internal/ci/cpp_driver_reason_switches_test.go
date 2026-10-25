package ci

// TestCPPDriverReasonSwitchesNameEveryRefusal holds the two C++ test drivers'
// reason_name switches to the TableRefuseReason enum the generator emits from
// tables/goruntime/goruntime.go: one case per value, in Go's order, returning
// the value's own name, and no default label.
//
// The missing default is the point. With -Werror=switch a default hides the
// next value the enum gains, and both drivers compile under -Werror, so a new
// refusal added to the Go runtime must break this test rather than wait for a
// compiler to notice. The check is syntactic: no compiler, clock or socket runs.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type refuseReason struct {
	// goName is the Go constant, e.g. TableRefuseBlobShared.
	goName string
	// name is the spelling Error() returns and the C++ enum uses, e.g. blob_shared.
	name string
}

// reasonNameSwitchRe finds a reason_name declaration over a generated
// TableRefuseReason; calls such as reason_name( reason ) carry no namespace and
// do not match.
var reasonNameSwitchRe = regexp.MustCompile(`reason_name\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)::TableRefuseReason\b`)

// reasonSwitchOpenRe finds the switch statement inside a reason_name function.
var reasonSwitchOpenRe = regexp.MustCompile(`switch\s*\(\s*reason\s*\)\s*\{`)

// reasonCaseRe reads one case arm; the drivers spell each arm on one line.
var reasonCaseRe = regexp.MustCompile(`case\s+([A-Za-z_][A-Za-z0-9_]*)::([A-Za-z_][A-Za-z0-9_]*)\s*:\s*return\s+"([^"]*)"\s*;`)

var reasonDefaultRe = regexp.MustCompile(`\bdefault\s*:`)

func TestCPPDriverReasonSwitchesNameEveryRefusal(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	reasons := tableRefuseReasons(t, filepath.Join(root, "tables", "goruntime", "goruntime.go"))
	if len(reasons) == 0 {
		t.Fatal("tables/goruntime/goruntime.go declares no TableRefuseReason constants; this gate scanned nothing")
	}
	for _, driver := range []string{
		filepath.Join("test", "tables", "cook_main.cpp"),
		filepath.Join("test", "conformance", "cpp", "main.cpp"),
	} {
		data, err := os.ReadFile(filepath.Join(root, driver))
		if err != nil {
			t.Fatalf("read %s: %v", driver, err)
		}
		for _, finding := range checkReasonNameSwitches(driver, string(data), reasons) {
			t.Error(finding)
		}
	}

	// REVERSED WITNESS: the same check over an in-memory driver must go red on
	// the two defects it exists to catch.
	fixture := reasonNameFixture("graphdemo", reasons, func(r refuseReason) bool { return true }, false)
	if findings := checkReasonNameSwitches("fixture", fixture, reasons); len(findings) != 0 {
		t.Fatalf("complete fixture should pass, got:\n%s", strings.Join(findings, "\n"))
	}
	removed := reasonNameFixture("graphdemo", reasons, func(r refuseReason) bool { return r.name != "blob_shared" }, false)
	findings := checkReasonNameSwitches("fixture-removed", removed, reasons)
	if !findingsName(findings, "graphdemo::blob_shared") {
		t.Errorf("removed blob_shared arm was not named; got:\n%s", strings.Join(findings, "\n"))
	}
	withDefault := reasonNameFixture("graphdemo", reasons, func(r refuseReason) bool { return true }, true)
	findings = checkReasonNameSwitches("fixture-default", withDefault, reasons)
	if !findingsName(findings, "default") {
		t.Errorf("added default label was not named; got:\n%s", strings.Join(findings, "\n"))
	}
}

// tableRefuseReasons reads the TableRefuseReason const block in Go's order and
// pairs each constant with the spelling its Error() switch returns.
func tableRefuseReasons(t *testing.T, path string) []refuseReason {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	spellings := tableRefuseReasonSpellings(file)
	var out []refuseReason
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		typed := false
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if value.Type != nil {
				typed = exprName(value.Type) == "TableRefuseReason"
			}
			if !typed {
				continue
			}
			for _, name := range value.Names {
				spelling, ok := spellings[name.Name]
				if !ok {
					t.Fatalf("%s: TableRefuseReason constant %s has no case in Error()", path, name.Name)
				}
				out = append(out, refuseReason{goName: name.Name, name: spelling})
			}
		}
	}
	return out
}

// tableRefuseReasonSpellings maps each constant in Error()'s switch to its
// returned string.
func tableRefuseReasonSpellings(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Error" || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		if exprName(fn.Recv.List[0].Type) != "TableRefuseReason" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			sw, ok := node.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			for _, stmt := range sw.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range clause.List {
					id, ok := expr.(*ast.Ident)
					if !ok {
						continue
					}
					for _, body := range clause.Body {
						ret, ok := body.(*ast.ReturnStmt)
						if !ok || len(ret.Results) != 1 {
							continue
						}
						lit, ok := ret.Results[0].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						if spelling, err := strconv.Unquote(lit.Value); err == nil {
							out[id.Name] = spelling
						}
					}
				}
			}
			return false
		})
	}
	return out
}

func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return exprName(e.X)
	}
	return ""
}

// checkReasonNameSwitches returns one finding per way a reason_name switch in
// src departs from reasons: missing or extra arms, a wrong order, a case that
// returns a spelling other than its own value name, or a default label.
func checkReasonNameSwitches(where, src string, reasons []refuseReason) []string {
	var findings []string
	locations := reasonNameSwitchRe.FindAllStringSubmatchIndex(src, -1)
	if len(locations) == 0 {
		return []string{where + ": no reason_name( <ns>::TableRefuseReason ) switch found"}
	}
	for _, loc := range locations {
		ns := src[loc[2]:loc[3]]
		line := strings.Count(src[:loc[0]], "\n") + 1
		at := fmt.Sprintf("%s:%d: reason_name( %s::TableRefuseReason )", where, line, ns)
		open := reasonSwitchOpenRe.FindStringIndex(src[loc[1]:])
		if open == nil {
			findings = append(findings, at+": has no `switch ( reason )`")
			continue
		}
		body, ok := bracedBody(src, loc[1]+open[1]-1)
		if !ok {
			findings = append(findings, at+": switch body is not closed")
			continue
		}
		var actual []struct{ name, spelling string }
		for _, arm := range reasonCaseRe.FindAllStringSubmatch(body, -1) {
			if arm[1] != ns {
				continue
			}
			actual = append(actual, struct{ name, spelling string }{arm[2], arm[3]})
		}
		for i := 0; i < len(reasons) || i < len(actual); i++ {
			switch {
			case i >= len(actual):
				findings = append(findings, fmt.Sprintf("%s: missing case %s::%s", at, ns, reasons[i].name))
			case i >= len(reasons):
				findings = append(findings, fmt.Sprintf("%s: unexpected case %s::%s", at, ns, actual[i].name))
			case actual[i].name != reasons[i].name:
				findings = append(findings, fmt.Sprintf("%s: case %d is %s::%s, want %s::%s",
					at, i+1, ns, actual[i].name, ns, reasons[i].name))
			case actual[i].spelling != reasons[i].name:
				findings = append(findings, fmt.Sprintf("%s: case %s::%s returns %q, want %q",
					at, ns, actual[i].name, actual[i].spelling, reasons[i].name))
			}
		}
		if loc := reasonDefaultRe.FindStringIndex(body); loc != nil {
			findings = append(findings, fmt.Sprintf("%s: has a default: label, which hides the next missing value", at))
		}
	}
	return findings
}

// bracedBody returns the text inside the braces that open at src[open].
func bracedBody(src string, open int) (string, bool) {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i], true
			}
		}
	}
	return "", false
}

// reasonNameFixture builds a reason_name function over ns with the same shape
// as the drivers, so the reversed witness runs the production check.
func reasonNameFixture(ns string, reasons []refuseReason, include func(refuseReason) bool, withDefault bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "static const char * reason_name( %s::TableRefuseReason reason )\n{\n    switch ( reason )\n    {\n", ns)
	for _, reason := range reasons {
		if !include(reason) {
			continue
		}
		fmt.Fprintf(&b, "        case %s::%s: return \"%s\";\n", ns, reason.name, reason.name)
	}
	if withDefault {
		b.WriteString("        default: return \"?\";\n")
	}
	b.WriteString("    }\n    return \"?\";\n}\n")
	return b.String()
}

func findingsName(findings []string, want string) bool {
	for _, finding := range findings {
		if strings.Contains(finding, want) {
			return true
		}
	}
	return false
}
