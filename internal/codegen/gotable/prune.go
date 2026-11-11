package gotable

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

// pruneUnreferencedDeclarations drops private package declarations that no
// generated Go file names. The table runtime is assembled from shared source
// fragments, so a unit that needs one path should not carry every helper those
// fragments can serve.
func pruneUnreferencedDeclarations(files map[string][]byte) (map[string][]byte, error) {
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
	for _, name := range names {
		file, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse generated Go file %s: %w", name, err)
		}
		parsed[name] = file
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil && decl.Name.Name != "init" {
					addPrivateDeclaration(declarations, decl.Name)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						addPrivateDeclaration(declarations, spec.Name)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							addPrivateDeclaration(declarations, name)
						}
					}
				}
			}
		}
	}

	for {
		referenced, usedIdentifiers := referencesInFiles(names, parsed, declarations)
		changed := false
		for _, name := range names {
			file := parsed[name]
			var kept []ast.Decl
			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Recv == nil && decl.Name.Name != "init" && isPrivate(decl.Name.Name) && !referenced[decl.Name.Name] {
						changed = true
						continue
					}
					kept = append(kept, decl)
				case *ast.GenDecl:
					old := len(decl.Specs)
					var specsChanged bool
					decl.Specs, specsChanged = pruneSpecs(decl, referenced, usedIdentifiers[name])
					if specsChanged || len(decl.Specs) < old {
						changed = true
					}
					if len(decl.Specs) != 0 {
						kept = append(kept, decl)
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
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, file); err != nil {
			return nil, fmt.Errorf("format generated Go file %s: %w", name, err)
		}
		files[name] = buf.Bytes()
	}
	return files, nil
}

func referencesInFiles(names []string, parsed map[string]*ast.File, declarations map[string][]*ast.Ident) (map[string]bool, map[string]map[string]bool) {
	referenced := map[string]bool{}
	usedIdentifiers := map[string]map[string]bool{}
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
		ast.Inspect(file, func(node ast.Node) bool {
			id, ok := node.(*ast.Ident)
			if !ok || nonReferences[id] || isDeclarationIdentifier(declarations[id.Name], id) {
				return true
			}
			usedIdentifiers[name][id.Name] = true
			if declarations[id.Name] != nil {
				referenced[id.Name] = true
			}
			return true
		})
	}
	return referenced, usedIdentifiers
}

func addPrivateDeclaration(dst map[string][]*ast.Ident, id *ast.Ident) {
	if id != nil && isPrivate(id.Name) {
		dst[id.Name] = append(dst[id.Name], id)
	}
}

func isPrivate(name string) bool {
	return name != "" && name != "_" && !token.IsExported(name)
}

func isDeclarationIdentifier(ids []*ast.Ident, id *ast.Ident) bool {
	for _, declaration := range ids {
		if declaration == id {
			return true
		}
	}
	return false
}

func pruneSpecs(decl *ast.GenDecl, referenced, usedIdentifiers map[string]bool) ([]ast.Spec, bool) {
	kept := make([]ast.Spec, 0, len(decl.Specs))
	changed := false
	for _, spec := range decl.Specs {
		switch spec := spec.(type) {
		case *ast.TypeSpec:
			if !isPrivate(spec.Name.Name) || referenced[spec.Name.Name] {
				kept = append(kept, spec)
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
		return imports, len(imports) != len(kept) || changed
	}
	return kept, changed
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
			name = path.Base(strings.Trim(imp.Path.Value, "\""))
			name = strings.TrimSuffix(name, ".v2")
		}
		if referenced[name] {
			kept = append(kept, imp)
		}
	}
	return kept
}
