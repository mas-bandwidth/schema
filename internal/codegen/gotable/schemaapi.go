package gotable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

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
}

// schemaAPIEntries recognizes only table-rooted callable names the emitter
// defines. The roots come from declared schema tables after Go escaping; the
// suffixes are the generated table API, not arbitrary names with a table-like
// prefix. The AST lookup ensures a name is retained only when this unit emits
// that entry point.
func schemaAPIEntries(u *ir.Unit, files map[string][]byte) map[string]bool {
	roots := map[string]bool{}
	for _, st := range u.Tables {
		if st == nil || st.MapEntryOf != "" || st.TextElem || token.IsExported(st.Name) {
			continue
		}
		roots[st.Name+"_"] = true
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
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			for root := range roots {
				if strings.HasPrefix(fn.Name.Name, root) && schemaCallableSuffixes[strings.TrimPrefix(fn.Name.Name, root)] {
					entries[fn.Name.Name] = true
				}
			}
		}
	}
	return entries
}
