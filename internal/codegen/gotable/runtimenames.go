package gotable

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// RuntimeNames returns the top-level names and the struct field names the
// shared Go table runtime declares (tables/goruntime, and the same
// declarations a standalone unit carries). They are the runtime's API, spelled
// once for every unit, so the Go target's initialism respelling leaves them as
// they are.
func RuntimeNames() (top, fields map[string]bool, err error) {
	sr, err := loadSharedRuntime()
	if err != nil {
		return nil, nil, err
	}
	f, err := parser.ParseFile(token.NewFileSet(), SharedRuntimeFile, sr.pkg, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}
	top, fields = map[string]bool{}, map[string]bool{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				top[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					top[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, id := range s.Names {
						top[id.Name] = true
					}
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if st, ok := n.(*ast.StructType); ok {
			for _, fl := range st.Fields.List {
				for _, id := range fl.Names {
					fields[id.Name] = true
				}
			}
		}
		return true
	})
	return top, fields, nil
}
