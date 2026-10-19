package gotable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSpecRefusalNamesMatchGo pins docs/SPEC-TABLES.md §6.5's refusal-name
// table to the names this package's own non-test source can emit. The spec is
// the standard: every name in its first column must be a name Go can answer,
// and every name Go can answer must be a row there, so a port that reads the
// table learns the whole vocabulary and Go cannot invent a name the page lacks
// or drop one it promises. A Reason literal must be a snake_case member and,
// where it carries detail, its ": " suffix — never a prose sentence.
func TestSpecRefusalNamesMatchGo(t *testing.T) {
	t.Parallel()
	specText := readRefusalSpec(t)
	spec := specRefusalNames(t, specText)
	goNames, literals := goRefusalNames(t)
	reportRefusalDiff(t, spec, goNames)
	for _, literal := range literals {
		if strings.Contains(literal, " ") && !refusalDetail.MatchString(literal) {
			t.Errorf("Reason literal %q is prose: name the refusal member and its \": \" detail", literal)
		}
	}

	// A witness in each direction, so a comparison that silently stopped
	// seeing one side cannot stay green.
	t.Run("witness: a name removed from the spec is reported", func(t *testing.T) {
		const name = "truncated"
		copied := specRefusalNames(t, removeSpecRefusalRow(t, specText, name))
		diff := diffRefusalNames(copied, goNames)
		if !sameNames(diff.goOnly, []string{name}) || len(diff.specOnly) != 0 {
			t.Fatalf("removing spec row %q reported go-only %v, spec-only %v; want go-only [%s], spec-only []",
				name, diff.goOnly, diff.specOnly, name)
		}
	})
	t.Run("witness: a name added to Go is reported", func(t *testing.T) {
		const name = "planted_refusal_name"
		planted := copyRefusalNames(goNames)
		planted[name] = true
		diff := diffRefusalNames(spec, planted)
		if !sameNames(diff.goOnly, []string{name}) || len(diff.specOnly) != 0 {
			t.Fatalf("adding Go name %q reported go-only %v, spec-only %v; want go-only [%s], spec-only []",
				name, diff.goOnly, diff.specOnly, name)
		}
	})
	t.Run("witness: a name removed from Go is reported", func(t *testing.T) {
		const name = "truncated"
		trimmed := copyRefusalNames(goNames)
		delete(trimmed, name)
		diff := diffRefusalNames(spec, trimmed)
		if len(diff.goOnly) != 0 || !sameNames(diff.specOnly, []string{name}) {
			t.Fatalf("removing Go name %q reported go-only %v, spec-only %v; want go-only [], spec-only [%s]",
				name, diff.goOnly, diff.specOnly, name)
		}
	})
}

const refusalSpecTables = "../../../docs/SPEC-TABLES.md"

const (
	refusalNamesBegin = "<!-- refusal-names:begin -->"
	refusalNamesEnd   = "<!-- refusal-names:end -->"
)

var (
	// The first column of a §6.5 row is one backticked snake_case name; the
	// prose that follows may name other things, so the anchor is the row start.
	specRefusalRow = regexp.MustCompile("^\\s*\\|\\s*`([a-z][a-z0-9_]*)`\\s*\\|")
	// The emitted runtime's Error() switch: case TableRefuseX:return "name".
	refusalCase = regexp.MustCompile(`case\s+TableRefuse\w+\s*:\s*return\s+"([a-z][a-z0-9_]*)"`)
	// Reason assignments inside the emitted-source string constants, after the
	// outer literal has been unquoted.
	embeddedReason = regexp.MustCompile(`\bReason\s*=\s*"([^"]*)"`)
	// The message runtime's helper calls with a literal name.
	embeddedMessageRefuse = regexp.MustCompile(`tableMessageRefuse\([^,]+,\s*"([a-z][a-z0-9_]*)"`)
	// An Error() return that names its refusal and carries detail.
	embeddedErrorReturn = regexp.MustCompile(`return\s+"([a-z][a-z0-9_]*): `)
	// The text form's stable names, assigned by constants in its emitted source.
	embeddedTextName = regexp.MustCompile(`TableTextForm\w+\s*=\s*"(text_[a-z0-9_]+)"`)
	// A reason name as the page's first column spells it.
	refusalName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	// A Reason literal that names a member and carries its detail.
	refusalDetail = regexp.MustCompile(`^[a-z][a-z0-9_]*: `)
)

func readRefusalSpec(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(refusalSpecTables)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func specRefusalNames(t *testing.T, text string) map[string]bool {
	t.Helper()
	_, rest, ok := strings.Cut(text, refusalNamesBegin)
	if !ok {
		t.Fatalf("%s: missing %s", refusalSpecTables, refusalNamesBegin)
	}
	block, _, ok := strings.Cut(rest, refusalNamesEnd)
	if !ok {
		t.Fatalf("%s: missing %s", refusalSpecTables, refusalNamesEnd)
	}
	names := map[string]bool{}
	for line := range strings.SplitSeq(block, "\n") {
		if m := specRefusalRow.FindStringSubmatch(line); m != nil {
			names[m[1]] = true
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s §6.5: the refusal-name table has no rows", refusalSpecTables)
	}
	return names
}

// goRefusalNames returns every refusal name this package's non-test source can
// answer. It reads the enum's own switch from tableRefusalSource, then walks
// the package's real Go files with go/ast for Reason assignments and for the
// message helpers that assign one, and finally scans the emitted-source string
// constants (which are not AST nodes of this package) for the same shapes.
func goRefusalNames(t *testing.T) (map[string]bool, []string) {
	t.Helper()
	names := map[string]bool{}
	var literals []string
	seen := map[string]bool{}
	addLiteral := func(literal string) {
		literal = strings.TrimSpace(literal)
		if literal == "" || seen[literal] {
			return
		}
		seen[literal] = true
		literals = append(literals, literal)
		if name := refusalNameOf(literal); name != "" {
			names[name] = true
		}
	}
	addName := func(name string) {
		if name != "" && name != "ok" {
			names[name] = true
		}
	}

	for _, m := range refusalCase.FindAllStringSubmatch(tableRefusalSource, -1) {
		addName(m[1])
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					if i >= len(n.Rhs) || !reasonAssigned(lhs) {
						continue
					}
					if literal, ok := leadingString(n.Rhs[i]); ok {
						addLiteral(literal)
					}
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && strings.HasPrefix(id.Name, "tableMessageRefuse") {
					for _, arg := range n.Args {
						if literal, ok := leadingString(arg); ok {
							addLiteral(literal)
						}
					}
				}
			case *ast.FuncDecl:
				if n.Name.Name != "Error" || n.Body == nil {
					break
				}
				ast.Inspect(n.Body, func(inner ast.Node) bool {
					if literal, ok := stringBasicLit(inner); ok && refusalDetail.MatchString(literal) {
						addLiteral(literal)
					}
					return true
				})
			case *ast.BasicLit:
				literal, ok := stringBasicLit(n)
				if !ok {
					break
				}
				for _, m := range embeddedReason.FindAllStringSubmatch(literal, -1) {
					addLiteral(m[1])
				}
				for _, m := range embeddedMessageRefuse.FindAllStringSubmatch(literal, -1) {
					addLiteral(m[1])
				}
				for _, m := range embeddedErrorReturn.FindAllStringSubmatch(literal, -1) {
					addLiteral(m[1])
				}
				for _, m := range embeddedTextName.FindAllStringSubmatch(literal, -1) {
					addLiteral(m[1])
				}
			}
			return true
		})
	}

	// The enum's own switch is the source of truth for the members; the rest
	// of the walk supplies the names that ride on report.Reason.
	if len(names) == 0 {
		t.Fatal("no refusal names found in the gotable source")
	}
	return names, literals
}

func reasonAssigned(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "Reason"
	case *ast.StarExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == "reason"
	case *ast.Ident:
		return x.Name == "reason"
	}
	return false
}

func leadingString(expr ast.Expr) (string, bool) {
	switch x := expr.(type) {
	case *ast.BasicLit:
		return stringBasicLit(x)
	case *ast.BinaryExpr:
		return leadingString(x.X)
	}
	return "", false
}

func stringBasicLit(node ast.Node) (string, bool) {
	lit, ok := node.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func refusalNameOf(literal string) string {
	if i := strings.Index(literal, ":"); i >= 0 {
		literal = literal[:i]
	}
	literal = strings.TrimSpace(literal)
	if !refusalName.MatchString(literal) {
		return ""
	}
	return literal
}

type refusalNameDiff struct {
	goOnly   []string
	specOnly []string
}

func diffRefusalNames(spec, goNames map[string]bool) refusalNameDiff {
	var diff refusalNameDiff
	for name := range goNames {
		if !spec[name] {
			diff.goOnly = append(diff.goOnly, name)
		}
	}
	for name := range spec {
		if !goNames[name] {
			diff.specOnly = append(diff.specOnly, name)
		}
	}
	sort.Strings(diff.goOnly)
	sort.Strings(diff.specOnly)
	return diff
}

func reportRefusalDiff(t *testing.T, spec, goNames map[string]bool) {
	t.Helper()
	diff := diffRefusalNames(spec, goNames)
	for _, name := range diff.goOnly {
		t.Errorf("Go has refusal name %q that docs/SPEC-TABLES.md §6.5 does not", name)
	}
	for _, name := range diff.specOnly {
		t.Errorf("docs/SPEC-TABLES.md §6.5 has refusal name %q that Go does not", name)
	}
}

func removeSpecRefusalRow(t *testing.T, text, name string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inside, removed := false, false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case refusalNamesBegin:
			inside = true
		case refusalNamesEnd:
			inside = false
		}
		if inside {
			if m := specRefusalRow.FindStringSubmatch(line); m != nil && m[1] == name {
				removed = true
				continue
			}
		}
		out = append(out, line)
	}
	if !removed {
		t.Fatalf("docs/SPEC-TABLES.md §6.5 has no row %q to remove", name)
	}
	return strings.Join(out, "\n")
}

func copyRefusalNames(names map[string]bool) map[string]bool {
	out := make(map[string]bool, len(names))
	for name := range names {
		out[name] = true
	}
	return out
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
