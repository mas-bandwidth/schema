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
		Name{Name: "FixedLoadReport", What: "the fixed load counters and selected layout hash", Scoped: true},
		Name{Name: "fixed_register", What: "bind the generated fixed-form schema and plans", Scoped: true},
		Name{Name: "fixed_load", What: "load a whole fixed-form batch", Scoped: true},
		Name{Name: "fixed_save", What: "save a fixed-form batch", Scoped: true},
		Name{Name: "fixed_measure", What: "measure a fixed-form batch from its count", Scoped: true},
		Name{Name: "BlobRef", What: "an explicit shared byte or string blob node", Scoped: true},
		Name{Name: "Refused", What: "the read refusal every generated package re-exports", Scoped: true},
		Name{Name: "InvalidValue", What: "the write refusal every generated package re-exports", Scoped: true},
		Name{Name: "LoadReport", What: "the counters a successful load fills", Scoped: true},
		Name{Name: "Number", What: "a JSON number token retained exactly", Scoped: true},
		Name{Name: "parse", What: "scan the table text dialect", Scoped: true},
		Name{Name: "from_json", What: "read a table from JSON text", Scoped: true},
		Name{Name: "to_json", What: "write a table as JSON text", Scoped: true},
		Name{Name: "FieldDesc", What: "one field's descriptor", Scoped: true},
		Name{Name: "TableDesc", What: "one table's descriptor", Scoped: true},
		Name{Name: "ArmDesc", What: "one table-side union arm's descriptor", Scoped: true},
		Name{Name: "UnionDesc", What: "one table-side union's descriptor", Scoped: true},
		Name{Name: "BlockField", What: "one field of a block record's layout", Scoped: true},
		Name{Name: "BlockRecord", What: "one record's block layout as data", Scoped: true},
		Name{Name: "BlockArray", What: "one out-of-line array of a block-form table", Scoped: true},
		Name{Name: "BlockTable", What: "a table's block form: projection, arrays and maximum extent", Scoped: true},
		Name{Name: "MAGIC", What: "the block prologue's magic word", Scoped: true},
		Name{Name: "PROLOGUE", What: "the block prologue's size in bytes", Scoped: true},
		Name{Name: "BLOCK_ALIGN", What: "the block base's and extent's 64-byte alignment", Scoped: true},
		Name{Name: "BYTE_ORDER", What: "this build's block byte-order word", Scoped: true},
		Name{Name: "HEADER_BYTES", What: "the cooked form's header size", Scoped: true},
		Name{Name: "MAX_ALIGN", What: "the largest accepted cooked alignment", Scoped: true},
		Name{Name: "DIRECTORY_ENTRY", What: "the size of one cooked node directory entry", Scoped: true},
		Name{Name: "NOT_MATERIALIZED", What: "the cooked directory sentinel", Scoped: true},
		Name{Name: "ORDER_LITTLE", What: "the little-endian cooked order", Scoped: true},
		Name{Name: "ORDER_BIG", What: "the big-endian cooked order", Scoped: true},
		Name{Name: "REGION_ALIGN_FLOOR", What: "the cooked region's minimum alignment", Scoped: true},
		Name{Name: "CookHandle", What: "an opened cooked region", Scoped: true},
		Name{Name: "open_cook", What: "open a cooked region in constant time", Scoped: true},
		Name{Name: "check_cook", What: "check the cooked region and its directory", Scoped: true},
		Name{Name: "dump_cook", What: "describe a cooked region's reachable records", Scoped: true},
		Name{Name: "cook_measure", What: "measure a cooked file", Scoped: true},
		Name{Name: "cook", What: "write a cooked file", Scoped: true},
		Name{Name: "ArrayView", What: "one opened out-of-line array's triple", Scoped: true},
		Name{Name: "Block", What: "an opened or built block", Scoped: true},
		Name{Name: "BlockOpen", What: "open a block or raise Refused", Scoped: true},
		Name{Name: "Dump", What: "the canonical row dump of an opened block", Scoped: true},
		Name{Name: "Begin", What: "write a block's prologue and triples", Scoped: true},
		Name{Name: "BlockMaxBytes", What: "a block's storage size over the array maxima", Scoped: true},
		Name{Name: "BlockBytes", What: "a built block's used extent", Scoped: true},
		Name{Name: "register", What: "register a generated table class by type id and name", Scoped: true},
		Name{Name: "by_id", What: "the table class a node record's type id names", Scoped: true},
		Name{Name: "by_name", What: "the table class a descriptor's target name names", Scoped: true},
		Name{Name: "new_by_name", What: "a fresh table of a descriptor's target name", Scoped: true},
		Name{Name: "new_union_by_name", What: "a fresh table-side union of a descriptor's target name", Scoped: true},
		Name{Name: "register_union", What: "register a generated table-side union class by name", Scoped: true},
		Name{Name: "Reader", What: "the form-1 byte reader", Scoped: true},
		Name{Name: "Ctx", What: "the load and save walk's state", Scoped: true},
		Name{Name: "buffer_view", What: "the bytes, bytearray or B memoryview a read takes", Scoped: true},
		Name{Name: "load_table", What: "form-1 load: the whole value or Refused", Scoped: true},
		Name{Name: "read_body", What: "read one nested body over the descriptors", Scoped: true},
		Name{Name: "Writer", What: "the form-1 byte writer and its counting sink", Scoped: true},
		Name{Name: "save_table", What: "form-1 save", Scoped: true},
		Name{Name: "measure_table", What: "the measured size of a form-1 save", Scoped: true},
		Name{Name: "write_body", What: "write one nested body over the descriptors", Scoped: true},
		Name{Name: "_widens", What: "array element widening predicate", Scoped: true},
		Name{Name: "_read_widened", What: "array element widening reader", Scoped: true},
		Name{Name: "crc32c", What: "CRC-32C of a file body", Scoped: true},
		Name{Name: "seal_crc", What: "append a root file checksum", Scoped: true},
		Name{Name: "open_crc", What: "verify and remove a root file checksum", Scoped: true},
		Name{Name: "kinds", What: "the field-kind dispatch table", Scoped: true},
		Name{Name: "FieldKind", What: "the field codec protocol", Scoped: true},
		Name{Name: "FORM", What: "the variable form's form byte (1)", Scoped: true},
		Name{Name: "NODE_WIRE_ID", What: "the reserved id the node table rides under", Scoped: true},
		Name{Name: "ID_BOUND", What: "the id table's 4096-entry bound", Scoped: true},
	)
}
