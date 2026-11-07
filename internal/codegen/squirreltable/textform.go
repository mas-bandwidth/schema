package squirreltable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE HAND-EDITABLE TEXT FORM (docs/SPEC-TABLES.md §16.9): a table with its
// document, the strict authoring form, joined to the table module through
// [RegisterSurface] as every form joins it (docs/PORT-SQUIRREL.md, THE RUNTIME
// LAYOUT). The reader, the resolver, the attachment and the edit live once in
// runtime/document.nut, a rule-for-rule port of
// internal/codegen/gotable/textform.go; it rides on the JSON walk
// (runtime/json.nut) and reads the json surface's text facts (json.go), so
// this file adds no descriptor key. Each member that carries the JSON form
// gets the three entry points Go's writeTextFormSurface emits:
//
//	<Name>LoadTextForm(text, limits, report)  the document, its typed value at
//	    doc.value, or null with report.name the stable refusal (§16.9.4)
//	<Name>SaveTextForm(doc)                   the retained bytes (§16.9.3)
//	<Name>EditTextForm(doc, edits, report)    a new document from leaf edits
//	    [{ path, text }] by original span (§16.9.3), or null
//
// The three suffixes are the checker's (internal/check, the LoadTextForm row),
// so no declaration collides with a wrapper; the runtime's own exports are
// reached through _rt_document and registered as scoped
// (internal/tablenames/squirrel.go).
func init() {
	RegisterSurface(Surface{Name: "textform", Table: textformTable})
}

// textformTable emits one member's three entry points (docs/SPEC-TABLES.md
// §16.9, Go's writeTextFormSurface): a map entry has no document of its own,
// and a table that declares its forms without json has no JSON walk to land
// on.
func textformTable(m *Module, st *ir.Struct) {
	if st.IsMapEntry() || (st.IsTable && !st.CarriesForm("json")) {
		return
	}
	rt := m.Runtime("document")
	n := st.Name
	q := squirrel.StringLit([]byte(n))
	m.Printf("// The hand-editable text form (docs/SPEC-TABLES.md §16.9): the document and its\n")
	m.Printf("// typed value, the retained bytes, and leaf edits by original span.\n")
	m.Printf("_M.%sLoadTextForm <- function(text, limits, report) { return %s.LoadTextForm(_M, %s, text, limits, report); };\n", n, rt, q)
	m.Printf("_M.%sSaveTextForm <- function(doc) { return %s.SaveTextForm(_M, %s, doc); };\n", n, rt, q)
	m.Printf("_M.%sEditTextForm <- function(doc, edits, report) { return %s.EditTextForm(_M, %s, doc, edits, report); };\n\n", n, rt, q)
}
