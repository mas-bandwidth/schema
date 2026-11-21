package tablenames

// Ruby is the Ruby table backend (internal/codegen/rubytable). Its runtime
// lives inside the generated unit module's SchemaTable submodule (_table.rb
// and _table/*.rb), with the public runtime errors, report, blob node and
// retention types on the unit module (_schema.rb). Scoped entries live inside
// SchemaTable or as method/descriptor names and claim no schema declaration.
//
// The allocation takes bit 15, the next one after Unity's 14.
const Ruby Backend = 1 << 15

func init() {
	define(Ruby,
		Name{Name: "FixedLoadReport", What: "the fixed load counters and selected layout hash", Scoped: true},
		Name{Name: "fixed_register", What: "bind the generated fixed-form schema and plans", Scoped: true},
		Name{Name: "fixed_load", What: "load a whole fixed-form batch", Scoped: true},
		Name{Name: "fixed_save", What: "save a fixed-form batch", Scoped: true},
		Name{Name: "fixed_measure", What: "measure a fixed-form batch from its count", Scoped: true},
		Name{Name: "fixed_plan_max", What: "bound the size of an executed fixed-form plan", Scoped: true},
		Name{Name: "BlobRef", What: "an explicit shared byte or string blob node", Scoped: true},
		Name{Name: "Refused", What: "the read refusal every generated unit defines", Scoped: true},
		Name{Name: "InvalidValue", What: "the write refusal every generated unit defines", Scoped: true},
		Name{Name: "LoadReport", What: "the counters a successful load fills", Scoped: true},
		Name{Name: "TableRetain", What: "the caller-owned retention buffers"},
		Name{Name: "SchemaTable", What: "the unit-scoped table runtime module", Scoped: true},
		Name{Name: "Number", What: "a JSON number token retained exactly", Scoped: true},
		Name{Name: "from_json", What: "read a table from JSON text", Scoped: true},
		Name{Name: "to_json", What: "write a table as JSON text", Scoped: true},
		Name{Name: "read_text", What: "read a hand-editable text document", Scoped: true},
		Name{Name: "UnitView", What: "the optional unit registry entry point", Scoped: true},
		Name{Name: "UnitViewInfo", What: "the unit registry", Scoped: true},
		Name{Name: "ViewType", What: "one type or table declaration", Scoped: true},
		Name{Name: "ViewVariant", What: "one vocabulary variant", Scoped: true},
		Name{Name: "ViewVocabulary", What: "one enum, flags or union declaration", Scoped: true},
		Name{Name: "ViewConstant", What: "one constant declaration", Scoped: true},
		Name{Name: "FieldDesc", What: "one field's runtime descriptor", Scoped: true},
		Name{Name: "ArmDesc", What: "one union arm's runtime descriptor", Scoped: true},
		Name{Name: "TableDesc", What: "one table's runtime descriptor", Scoped: true},
		Name{Name: "UnionDesc", What: "one union's runtime descriptor", Scoped: true},
		Name{Name: "KeyedArray", What: "an enum-keyed array container", Scoped: true},
		Name{Name: "Keyed", What: "the namespace of the generated keyed-array classes, SchemaTable::Keyed::<Table>::Field_<field>", Scoped: true},
		Name{Name: "BlockOpen", What: "open a block-form buffer", Scoped: true},
		Name{Name: "BlockBuild", What: "build a block-form buffer", Scoped: true},
		Name{Name: "CookOpen", What: "open a cooked image", Scoped: true},
		Name{Name: "CookCheck", What: "check a cooked image thoroughly", Scoped: true},
		Name{Name: "MessageVocabulary", What: "a form-2 message vocabulary", Scoped: true},
		Name{Name: "announce_read", What: "read a remote message announcement", Scoped: true},
		Name{Name: "load_messages", What: "load a form-2 message batch", Scoped: true},
		Name{Name: "save_messages", What: "save a form-2 message batch", Scoped: true},
		Name{Name: "measure_messages", What: "measure a form-2 message batch", Scoped: true},
	)
}
