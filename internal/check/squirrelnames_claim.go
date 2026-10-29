package check

// The Squirrel table backend's per-declaration spellings (internal/codegen/
// squirreltable), claimed here rather than in the tableGeneratedVerbs row
// literal because that list is a shared row literal other cards append to.
//
// A Squirrel unit module binds every declaration's class and every generated
// name on ONE export table, `_M`, so a declaration named for a generated
// spelling replaces the binding the table module wrote first. The four are
// emitted for every fixed table (the layout descriptor table, the plan's
// decode body, the deprecated-slot reader and the record size), so each is
// claimed on this list's own rule: a name free today becomes a collision the
// day a table gains the form. compiler's
// TestSquirrelTableModuleScopeNamesAreClaimed finds any spelling this list
// misses (docs/SPEC-TABLES.md §11, docs/PORTING.md I6).
func init() {
	tableGeneratedVerbs = append(tableGeneratedVerbs,
		"Descriptor", "FixedDecodeBody", "FixedDeprecated", "FixedRecord")
}
