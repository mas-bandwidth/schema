package check

import (
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

// TestEveryEmittedGoSuffixIsClaimed pins every package-level suffix Go emits
// that the section 11 claim list must cover. A declaration colliding with any
// of them must be refused by name at check time; otherwise the generated package fails
// to compile (docs/SPEC-TABLES.md section 11).
func TestEveryEmittedGoSuffixIsClaimed(t *testing.T) {
	t.Parallel()
	suffixes := []string{
		"ReadStrict",
		"ToJsonMeasureReason",
		"FixedSaveReason",
		"FixedCheckBody",
		"FixedCheckBodyExact",
		"LoadTextForm",
		"SaveTextForm",
		"EditTextForm",
		"FixedResetImage",
		"FixedHoles",
		"FixedLineageHoles",
		"FixedPlanMax",
		"GenPastBound",
	}
	for _, suffix := range suffixes {
		suffix := suffix
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			src := "package t\ntable Ship { x int32 }\ntype Ship" + suffix + " { y int32 }\n"
			errs := runUnit(t, map[string]string{"T.schema": src})
			if len(errs) == 0 {
				t.Fatalf("table Ship plus type Ship%s must be refused by name, but the checker accepted it", suffix)
			}
			var joined strings.Builder
			for _, e := range errs {
				joined.WriteString(e.Error())
				joined.WriteString("\n")
			}
			if !strings.Contains(joined.String(), "Ship"+suffix) {
				t.Errorf("the refusal must name the colliding symbol Ship%s: %v", suffix, joined.String())
			}
			if !strings.Contains(joined.String(), "Ship") {
				t.Errorf("the refusal must name the table Ship: %v", joined.String())
			}
		})
	}

	t.Run("a near-miss spelling is accepted", func(t *testing.T) {
		t.Parallel()
		src := "package t\ntable Ship { x int32 }\ntype ShipReadStrictly { y int32 }\n"
		if errs := runUnit(t, map[string]string{"T.schema": src}); len(errs) > 0 {
			t.Fatalf("type ShipReadStrictly is not a claimed suffix and must be accepted: %v", errs)
		}
	})

	// the field-qualified helper suffixes (Add, Erase, Each, Insert, Find,
	// IndexMeasure, Index, IndexFind, Append, AppendAbsent, TextAt, Type) are
	// emitted from <Table><Field> or the union name, never the bare table name,
	// so a declaration spelling one of them is not a collision and is accepted.
	t.Run("type ShipIndex is accepted", func(t *testing.T) {
		t.Parallel()
		src := "package t\ntable Ship { x int32 }\ntype ShipIndex { y int32 }\n"
		if errs := runUnit(t, map[string]string{"T.schema": src}); len(errs) > 0 {
			t.Fatalf("type ShipIndex is not a bare table claim and must be accepted: %v", errs)
		}
	})

	t.Run("enum ShipType is accepted", func(t *testing.T) {
		t.Parallel()
		src := "package t\nenum ShipType { A }\ntable Ship { kind ShipType }\n"
		if errs := runUnit(t, map[string]string{"T.schema": src}); len(errs) > 0 {
			t.Fatalf("enum ShipType beside table Ship is not a bare table claim and must be accepted: %v", errs)
		}
	})
}

var packageSuffixDeclaration = regexp.MustCompile("\\b(func|var|type|const) %(\\[1\\])?s([A-Z][A-Za-z0-9_]*)")

func packageLevelGoSuffixes(source string) []string {
	matches := packageSuffixDeclaration.FindAllStringSubmatch(source, -1)
	suffixes := make([]string, 0, len(matches))
	for _, match := range matches {
		suffixes = append(suffixes, match[3])
	}
	return suffixes
}

// containsIdent reports whether e references any identifier in names.
func containsIdent(e ast.Expr, names map[string]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && names[id.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}

// typeIsUnion reports whether a parameter type expression names a union.
func typeIsUnion(e ast.Expr) bool {
	return containsIdent(e, map[string]bool{"Union": true})
}

// allStringLiterals folds an argument expression down to its string-literal
// parts (a format argument is a single literal or a build-time concatenation
// of literals), so the declaration pattern can be matched across the whole
// format string.
func allStringLiterals(e ast.Expr) string {
	var b strings.Builder
	var walk func(ast.Expr)
	walk = func(n ast.Expr) {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if s, err := strconv.Unquote(x.Value); err == nil {
					b.WriteString(s)
				}
			}
		case *ast.ParenExpr:
			walk(x.X)
		case *ast.BinaryExpr:
			walk(x.X)
			walk(x.Y)
		}
	}
	walk(e)
	return b.String()
}

const (
	claimTableName = iota
	claimFieldQualified
	claimUnionName
)

// resolveName classifies the name a declaration literal substitutes for its
// table-name slot. A bare table name must be claimed as <Table><Suffix> (§11);
// a field-qualified <Table><Field> helper or a union name is emitted from the
// field or the union, never the table, so a declaration that spells one is not
// a collision and is not required to be claimed.
func resolveName(e ast.Expr, locals map[string]ast.Expr, unionVars map[string]bool) int {
	var rec func(ast.Expr) int
	rec = func(n ast.Expr) int {
		switch x := n.(type) {
		case *ast.Ident:
			if def, ok := locals[x.Name]; ok {
				return rec(def)
			}
			if unionVars[x.Name] {
				return claimUnionName
			}
			return claimTableName
		case *ast.SelectorExpr:
			if containsIdent(x.X, unionVars) {
				return claimUnionName
			}
			return claimTableName
		case *ast.BinaryExpr:
			return claimFieldQualified
		case *ast.CallExpr:
			return claimFieldQualified
		case *ast.ParenExpr:
			return rec(x.X)
		default:
			return claimTableName
		}
	}
	return rec(e)
}

// TestEmitterTemplatesNameOnlyClaimedSuffixes keeps the section 11 registry complete
// as the Go emitter grows. It reads the emitter's source and finds every
// format-string literal that declares a package-level name from the bare table
// name (`func %s<Suffix>`, `var %s<Suffix>`, ...). The field-qualified helpers
// and union tag spellings (`<Table><Field><Verb>`, `<Union>Type`) are emitted
// from the field or the union rather than the bare table name, and are ignored.
func TestEmitterTemplatesNameOnlyClaimedSuffixes(t *testing.T) {
	t.Parallel()
	control := packageLevelGoSuffixes("func %sNotClaimedXyz(")
	if len(control) != 1 || control[0] != "NotClaimedXyz" {
		t.Fatalf("control declaration was not recognized: %v", control)
	}

	const dir = "../codegen/gotable"
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read Go table emitter directory: %v", err)
	}
	for _, entry := range files {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		locals := map[string]ast.Expr{}
		unionVars := map[string]bool{}
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Type != nil && fd.Type.Params != nil {
				for _, p := range fd.Type.Params.List {
					if typeIsUnion(p.Type) {
						for _, n := range p.Names {
							unionVars[n.Name] = true
						}
					}
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch a := n.(type) {
			case *ast.AssignStmt:
				if a.Tok == token.DEFINE || a.Tok == token.ASSIGN {
					for i, lhs := range a.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && i < len(a.Rhs) {
							locals[id.Name] = a.Rhs[i]
						}
					}
				}
			case *ast.CallExpr:
				sel, ok := a.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "pf", "tf", "Fprintf", "Fprint":
				default:
					return true
				}
				for i, arg := range a.Args {
					lit := allStringLiterals(arg)
					matches := packageSuffixDeclaration.FindAllStringSubmatch(lit, -1)
					if len(matches) == 0 || i+1 >= len(a.Args) {
						continue
					}
					switch resolveName(a.Args[i+1], locals, unionVars) {
					case claimFieldQualified, claimUnionName:
						continue
					}
					pos := fset.Position(a.Pos())
					for _, m := range matches {
						suffix := m[3]
						claimed := false
						for _, verb := range tableGeneratedVerbs {
							if suffix == verb {
								claimed = true
								break
							}
						}
						if !claimed {
							t.Errorf("%s:%d: package-level Go table suffix %s is not in tableGeneratedVerbs", path, pos.Line, suffix)
						}
					}
				}
			}
			return true
		})
	}
}
