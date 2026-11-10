package tablenames

// Python is the Python table backend (internal/codegen/pythontable). Its
// runtime is a subpackage of the generated package (_schema), one module per
// field kind beside one dispatch table, and every spelling it defines lives at
// that subpackage's module scope. A schema declaration is a class in its own
// schema-file module and is reached by a relative import, so no declaration
// can collide with a runtime name: every entry here is Scoped and claims
// nothing. They are registered anyway, as Lua registers its module keys, so
// the scan that holds this list honest has an answer for every name the
// emitter spells.
//
// The allocation takes bit 13, the next one after Squirrel's 12.
const Python Backend = 1 << 13

func init() {
	define(Python,
		Name{Name: "Refused", What: "the read refusal every generated package re-exports", Scoped: true},
		Name{Name: "InvalidValue", What: "the write refusal every generated package re-exports", Scoped: true},
		Name{Name: "LoadReport", What: "the counters a successful load fills", Scoped: true},
		Name{Name: "FieldDesc", What: "one field's descriptor", Scoped: true},
		Name{Name: "TableDesc", What: "one table's descriptor", Scoped: true},
		Name{Name: "ArmDesc", What: "one table-side union arm's descriptor", Scoped: true},
		Name{Name: "UnionDesc", What: "one table-side union's descriptor", Scoped: true},
		Name{Name: "register", What: "register a generated table class by type id and name", Scoped: true},
		Name{Name: "by_id", What: "the table class a node record's type id names", Scoped: true},
		Name{Name: "by_name", What: "the table class a descriptor's target name names", Scoped: true},
		Name{Name: "new_by_name", What: "a fresh table of a descriptor's target name", Scoped: true},
		Name{Name: "Reader", What: "the form-1 byte reader", Scoped: true},
		Name{Name: "Ctx", What: "the load and save walk's state", Scoped: true},
		Name{Name: "buffer_view", What: "the bytes, bytearray or B memoryview a read takes", Scoped: true},
		Name{Name: "load_table", What: "form-1 load: the whole value or Refused", Scoped: true},
		Name{Name: "read_body", What: "read one nested body over the descriptors", Scoped: true},
		Name{Name: "Writer", What: "the form-1 byte writer and its counting sink", Scoped: true},
		Name{Name: "save_table", What: "form-1 save", Scoped: true},
		Name{Name: "measure_table", What: "the measured size of a form-1 save", Scoped: true},
		Name{Name: "write_body", What: "write one nested body over the descriptors", Scoped: true},
		Name{Name: "kinds", What: "the field-kind dispatch table", Scoped: true},
		Name{Name: "Kind", What: "the field codec protocol", Scoped: true},
		Name{Name: "REGISTRY", What: "the field-kind registry", Scoped: true},
		Name{Name: "FORM", What: "the variable form's form byte (1)", Scoped: true},
		Name{Name: "NODE_WIRE_ID", What: "the reserved id the node table rides under", Scoped: true},
		Name{Name: "ID_BOUND", What: "the id table's 4096-entry bound", Scoped: true},
	)
}
