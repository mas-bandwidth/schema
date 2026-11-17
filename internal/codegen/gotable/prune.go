package gotable

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// pruneUnreferencedDeclarations drops private package declarations that the
// unit's API cannot reach: an exported name, an init function or a schema-rooted
// entry point, followed through the names those declarations use. The table
// runtime is assembled from shared source fragments, so a unit that needs one
// path should not carry every helper those fragments can serve, and a private
// cluster that only calls itself leaves too.
func pruneUnreferencedDeclarations(files map[string][]byte, schemaAPI map[string]bool) (map[string][]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, ".go") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	parsed := make(map[string]*ast.File, len(names))
	declarations := map[string][]*ast.Ident{}
	methods := map[string]bool{}
	for _, name := range names {
		file, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse generated Go file %s: %w", name, err)
		}
		parsed[name] = file
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv != nil {
					methods[decl.Name.Name] = true
				}
				addDeclaration(declarations, decl.Name)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						addDeclaration(declarations, spec.Name)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							addDeclaration(declarations, name)
						}
					}
				}
			}
		}
	}

	// removed holds, per file, the source ranges of the declarations dropped,
	// so their doc and interior comments leave with them.
	removed := map[string][]sourceRange{}
	for {
		roots := map[string]bool{"init": true}
		for name := range declarations {
			if token.IsExported(name) {
				roots[name] = true
			}
		}
		for name := range schemaAPI {
			roots[name] = true
		}
		referenced, usedIdentifiers := reachableDeclarations(names, parsed, declarations, methods, roots)
		changed := false
		for _, name := range names {
			file := parsed[name]
			var kept []ast.Decl
			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if (decl.Recv != nil || decl.Name.Name != "init") && isPrivate(decl.Name.Name) && !referenced[decl.Name.Name] {
						removed[name] = append(removed[name], nodeRange(decl.Doc, decl, nil))
						changed = true
						continue
					}
					kept = append(kept, decl)
				case *ast.GenDecl:
					old := len(decl.Specs)
					whole := nodeRange(decl.Doc, decl, nil) // End needs the specs
					specs, specsChanged, dropped := pruneSpecs(decl, referenced, usedIdentifiers[name])
					decl.Specs = specs
					removed[name] = append(removed[name], dropped...)
					if specsChanged || len(decl.Specs) < old {
						changed = true
					}
					if len(decl.Specs) != 0 {
						kept = append(kept, decl)
					} else {
						removed[name] = append(removed[name], whole)
					}
				default:
					kept = append(kept, decl)
				}
			}
			if len(kept) != len(file.Decls) {
				changed = true
			}
			file.Decls = kept
		}
		if !changed {
			break
		}
	}

	for _, name := range names {
		file := parsed[name]
		file.Comments = commentsOutside(file.Comments, removed[name])
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, file); err != nil {
			return nil, fmt.Errorf("format generated Go file %s: %w", name, err)
		}
		files[name] = buf.Bytes()
	}
	return files, nil
}

func reachableDeclarations(names []string, parsed map[string]*ast.File, declarations map[string][]*ast.Ident, methods map[string]bool, roots map[string]bool) (map[string]bool, map[string]map[string]bool) {
	edges := map[string]map[string]bool{}
	usedIdentifiers := map[string]map[string]bool{}
	addEdge := func(from, to string) {
		if from == "" {
			return
		}
		if edges[from] == nil {
			edges[from] = map[string]bool{}
		}
		edges[from][to] = true
	}
	for _, name := range names {
		file := parsed[name]
		usedIdentifiers[name] = map[string]bool{}
		nonReferences := map[*ast.Ident]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.SelectorExpr:
				nonReferences[node.Sel] = true
			case *ast.KeyValueExpr:
				if key, ok := node.Key.(*ast.Ident); ok {
					nonReferences[key] = true
				}
			case *ast.Field:
				for _, id := range node.Names {
					nonReferences[id] = true
				}
			case *ast.ImportSpec:
				if node.Name != nil {
					nonReferences[node.Name] = true
				}
			}
			return true
		})
		collect := func(from string, nodes ...ast.Node) {
			for _, node := range nodes {
				if node == nil || reflect.ValueOf(node).IsNil() {
					continue
				}
				ast.Inspect(node, func(node ast.Node) bool {
					switch node := node.(type) {
					case *ast.SelectorExpr:
						if methods[node.Sel.Name] {
							addEdge(from, node.Sel.Name)
						}
						return true
					}
					id, ok := node.(*ast.Ident)
					if !ok || nonReferences[id] || isDeclarationIdentifier(declarations[id.Name], id) {
						return true
					}
					usedIdentifiers[name][id.Name] = true
					if declarations[id.Name] != nil {
						addEdge(from, id.Name)
					}
					return true
				})
			}
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				collect(decl.Name.Name, decl.Recv, decl.Type, decl.Body)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						collect(spec.Name.Name, spec.Type)
					case *ast.ValueSpec:
						if len(spec.Values) == len(spec.Names) {
							for i, id := range spec.Names {
								collect(id.Name, spec.Type, spec.Values[i])
							}
						} else {
							for _, id := range spec.Names {
								collect(id.Name, spec.Type)
								for _, value := range spec.Values {
									collect(id.Name, value)
								}
							}
						}
					}
				}
			}
		}
	}
	referenced := map[string]bool{}
	var queue []string
	for name := range roots {
		if referenced[name] {
			continue
		}
		referenced[name] = true
		queue = append(queue, name)
	}
	for len(queue) != 0 {
		name := queue[0]
		queue = queue[1:]
		for ref := range edges[name] {
			if referenced[ref] {
				continue
			}
			referenced[ref] = true
			queue = append(queue, ref)
		}
	}
	return referenced, usedIdentifiers
}

func addDeclaration(dst map[string][]*ast.Ident, id *ast.Ident) {
	if id != nil && id.Name != "_" {
		dst[id.Name] = append(dst[id.Name], id)
	}
}

func isPrivate(name string) bool {
	return name != "" && name != "_" && !token.IsExported(name)
}

func isDeclarationIdentifier(ids []*ast.Ident, id *ast.Ident) bool {
	return slices.Contains(ids, id)
}

func pruneSpecs(decl *ast.GenDecl, referenced, usedIdentifiers map[string]bool) ([]ast.Spec, bool, []sourceRange) {
	kept := make([]ast.Spec, 0, len(decl.Specs))
	var dropped []sourceRange
	changed := false
	constsDroppable := decl.Tok == token.CONST && constSpecsIndependent(decl.Specs)
	for _, spec := range decl.Specs {
		switch spec := spec.(type) {
		case *ast.TypeSpec:
			if !isPrivate(spec.Name.Name) || referenced[spec.Name.Name] {
				kept = append(kept, spec)
			} else {
				dropped = append(dropped, nodeRange(spec.Doc, spec, spec.Comment))
			}
		case *ast.ValueSpec:
			if len(spec.Names) == 0 {
				kept = append(kept, spec)
				continue
			}
			if decl.Tok == token.VAR {
				live := make([]bool, len(spec.Names))
				liveCount := 0
				for i, id := range spec.Names {
					live[i] = !isPrivate(id.Name) || referenced[id.Name]
					if live[i] {
						liveCount++
					}
				}
				if liveCount == 0 {
					dropped = append(dropped, nodeRange(spec.Doc, spec, spec.Comment))
					changed = true
					continue
				}
				if liveCount < len(spec.Names) && len(spec.Values) == len(spec.Names) {
					names := make([]*ast.Ident, 0, liveCount)
					values := make([]ast.Expr, 0, liveCount)
					for i := range spec.Names {
						if live[i] {
							names = append(names, spec.Names[i])
							values = append(values, spec.Values[i])
						}
					}
					spec.Names, spec.Values = names, values
					changed = true
				} else {
					for i, id := range spec.Names {
						if !live[i] {
							id.Name = "_"
							changed = true
						}
					}
				}
				kept = append(kept, spec)
				continue
			}
			if constsDroppable && allUnreferenced(spec.Names, referenced) {
				// A constant that only names another value or a literal has
				// no check to keep, so an unused one is left out entirely.
				dropped = append(dropped, nodeRange(spec.Doc, spec, spec.Comment))
				changed = true
				continue
			}
			for _, id := range spec.Names {
				if isPrivate(id.Name) && !referenced[id.Name] {
					// A blank binding preserves initializer evaluation and the
					// position of const iota entries without exporting a name.
					id.Name = "_"
					changed = true
				}
			}
			kept = append(kept, spec)
		default:
			kept = append(kept, spec)
		}
	}
	if decl.Tok == token.IMPORT {
		imports := pruneImports(kept, usedIdentifiers)
		return imports, len(imports) != len(kept) || changed, dropped
	}
	return kept, changed, dropped
}

// constSpecsIndependent reports whether every spec of a const group stands on
// its own: each has its own values, none names iota, and each value is a
// literal or a bare name, so leaving one out moves no other constant and drops
// no compile-time check.
func constSpecsIndependent(specs []ast.Spec) bool {
	for _, spec := range specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok || len(value.Values) == 0 {
			return false
		}
		for _, expr := range value.Values {
			switch expr := expr.(type) {
			case *ast.BasicLit:
			case *ast.Ident:
				if expr.Name == "iota" {
					return false
				}
			case *ast.SelectorExpr:
				if _, ok := expr.X.(*ast.Ident); !ok {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

// allUnreferenced reports whether every name is private and named nowhere;
// a blank name is kept, since the source wrote it as a check.
func allUnreferenced(names []*ast.Ident, referenced map[string]bool) bool {
	for _, id := range names {
		if !isPrivate(id.Name) || referenced[id.Name] {
			return false
		}
	}
	return true
}

// sourceRange is the extent of a dropped declaration, its doc comment and its
// trailing line comment included.
type sourceRange struct {
	pos, end token.Pos
}

func nodeRange(doc *ast.CommentGroup, node ast.Node, line *ast.CommentGroup) sourceRange {
	r := sourceRange{pos: node.Pos(), end: node.End()}
	if doc != nil && doc.Pos() < r.pos {
		r.pos = doc.Pos()
	}
	if line != nil && line.End() > r.end {
		r.end = line.End()
	}
	return r
}

// commentsOutside keeps the comment groups that lie outside every dropped
// declaration, so no doc or body comment outlives the code it described.
func commentsOutside(groups []*ast.CommentGroup, removed []sourceRange) []*ast.CommentGroup {
	if len(removed) == 0 {
		return groups
	}
	kept := groups[:0]
	for _, group := range groups {
		inside := false
		for _, r := range removed {
			if group.Pos() >= r.pos && group.End() <= r.end {
				inside = true
				break
			}
		}
		if !inside {
			kept = append(kept, group)
		}
	}
	return kept
}

func pruneImports(specs []ast.Spec, referenced map[string]bool) []ast.Spec {
	kept := make([]ast.Spec, 0, len(specs))
	for _, spec := range specs {
		imp := spec.(*ast.ImportSpec)
		if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
			kept = append(kept, imp)
			continue
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			name = goname.ImportName(strings.Trim(imp.Path.Value, "\""))
		}
		if referenced[name] {
			kept = append(kept, imp)
		}
	}
	return kept
}

// finishForms applies the Go initialisms and, for a unit's own output, drops
// the declarations its API cannot reach.
func finishForms(out map[string][]byte, forms map[string]Form, u *ir.Unit, prune bool) (map[string][]byte, map[string]Form, error) {
	if err := goname.RespellFiles(out); err != nil {
		return nil, nil, err
	}
	if !prune {
		return out, forms, nil
	}
	pruned, err := pruneUnreferencedDeclarations(out, schemaAPIEntries(u, out))
	return pruned, forms, err
}

// generateRuntimeForms is the unit's own output before the runtime choice,
// with the surface it declares checked.
func generateRuntimeForms(u *ir.Unit, lineage map[string][]FixedLineageEntry, s Surfaces) (map[string][]byte, error) {
	if err := refuseUndeclaredSurface(u, s); err != nil {
		return nil, err
	}
	out, _, err := generateForms(u, lineage, s, false)
	return out, err
}

func pruneRuntimeForms(out map[string][]byte, u *ir.Unit) (map[string][]byte, error) {
	return pruneUnreferencedDeclarations(out, schemaAPIEntries(goEscapeTables(u), out))
}
