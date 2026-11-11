package gotable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// schemaCallableSuffixes are the callable names emitted from a table's Go
// spelling. They remain part of generated packages even when the spelling is
// private (for example, a legal lower-case table name or an escaped keyword).
// Keep this list aligned with the table entry points emitted by gotable.
var schemaCallableSuffixes = map[string]bool{
	"Reset": true, "TableType": true,
	"Load": true, "LoadBody": true, "LoadRegion": true, "LoadMeasure": true,
	"LoadMeasureReason": true, "LoadBuilder": true, "LoadBuilderRead": true,
	"LoadRetain": true, "ReadStrict": true,
	"Measure": true, "MeasureReason": true, "MeasureBody": true,
	"Save": true, "SaveBody": true, "SaveBodyFields": true,
	"MeasureRetain": true, "SaveRetain": true,
	"SaveText": true, "LoadText": true,
	"FromJSON": true, "FromJSONWith": true, "ToJSON": true,
	"ToJSONMeasure": true, "ToJSONMeasureReason": true, "ToJSONWith": true,
	"ToJSONMeasureWith": true,
	"SaveMessageBody":   true, "LoadMessageBody": true, "SaveMessages": true,
	"LoadMessages": true, "MeasureMessages": true,
	"CookMeasure": true, "CookMeasureReason": true, "CookFrom": true,
	"FixedLeaves": true, "FixedMeasure": true, "FixedSave": true,
	"FixedSaveReason": true, "FixedLoad": true, "FixedBounds": true,
	"FixedBoundsBody": true, "FixedCheckBody": true, "FixedCheckBodyExact": true,
	"FixedFiniteBody": true,
	"LoadTextForm":    true, "SaveTextForm": true, "EditTextForm": true,
	"Open": true, "At": true,
	"BlockOpen": true, "BlockBegin": true, "BlockBytes": true,
	"Emplace": true,
}

var schemaFieldCallableSuffixes = map[string]bool{
	"Add": true, "Erase": true, "Each": true,
	"Find": true, "Insert": true, "IndexMeasure": true,
	"Index": true, "IndexFind": true,
	"TextAt": true, "Append": true, "AppendAbsent": true,
}

var schemaTableAPIDeclarations = map[string]bool{
	"FixedPlanMax":       true,
	"LoadRetainBuilder":  true,
	"SaveRetainMessages": true, "LoadRetainMessages": true,
	"LoadRetain": true, "MeasureRetain": true, "SaveRetain": true,
}

// schemaAPIEntries recognizes only table-rooted callable names the emitter
// defines. The roots come from declared schema tables after Go escaping; the
// suffixes are the generated table API, not arbitrary names with a table-like
// prefix. The AST lookup ensures a name is retained only when this unit emits
// that entry point.
func schemaAPIEntries(u *ir.Unit, files map[string][]byte) map[string]bool {
	candidates := map[string]bool{}
	for _, st := range u.Tables {
		if st == nil || st.MapEntryOf != "" || st.TextElem || token.IsExported(st.Name) {
			continue
		}
		root := goname.Initialisms(st.Name)
		for suffix := range schemaCallableSuffixes {
			candidates[root+suffix] = true
		}
		for suffix := range schemaTableAPIDeclarations {
			candidates[root+suffix] = true
		}
		for _, f := range st.Fields {
			fieldRoot := goname.Initialisms(st.Name + ir.GoExportName(f.Name))
			for suffix := range schemaFieldCallableSuffixes {
				candidates[fieldRoot+suffix] = true
			}
		}
	}
	entries := map[string]bool{}
	fset := token.NewFileSet()
	for filename, src := range files {
		if !strings.HasSuffix(filename, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filename, src, 0)
		if err != nil {
			continue // the pruner reports the contextual parse error
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil && candidates[decl.Name.Name] {
					entries[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if candidates[spec.Name.Name] {
							entries[spec.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if candidates[name.Name] {
								entries[name.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return entries
}
