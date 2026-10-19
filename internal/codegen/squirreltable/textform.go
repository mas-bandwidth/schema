// THE HAND-EDITABLE TEXT FORM (docs/SPEC-TABLES.md §16.9): the document
// surface — LoadTextForm reads a document and its typed value, SaveTextForm
// returns the retained source bytes, EditTextForm applies leaf edits. The
// heavy lifting lives in the emitted runtime schema_document.nut; here the
// module binds the surface to the per-unit table descriptors.
package squirreltable

func init() {
	RegisterSurface(Surface{Name: "textform", Module: textformModule})
}

func textformModule(m *Module) {
	m.Runtime("document")
	m.Printf("// The hand-editable text form (docs/SPEC-TABLES.md §16.9): a strict\n")
	m.Printf("// authoring form whose document retains its source bytes.\n")
	m.Printf("_M.LoadTextForm <- function(T, rootName, text, limits, report) { return _rt_document.LoadTextForm(_M, T, rootName, text, limits, report); };\n")
	m.Printf("_M.SaveTextForm <- function(T, rootName, value) { return _rt_document.SaveTextForm(_M, T, rootName, value); };\n")
	m.Printf("_M.EditTextForm <- function(T, rootName, doc, edits, limits, report) { return _rt_document.EditTextForm(_M, T, rootName, doc, edits, limits, report); };\n")
}
