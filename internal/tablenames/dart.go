package tablenames

// Dart is the Dart table backend (internal/codegen/darttable).
//
// The Dart backend emits the BLOCK and COOK read halves only (the table wire
// in its previous form was removed; schema#514 brings the id-table wire), so
// this registry holds the two accelerator runtimes' library-scope names. Where a runtime name is a free
// function Dart spells it lowerCamelCase and the registry holds the
// PascalCase form: the two are a bijection (the packet emitter's
// dartName), so one registration covers both.
const Dart Backend = 1 << 8

func init() {
	define(Dart,
		Name{Name: "TableBlockFieldInfo", What: "a block field's reflection descriptor"},
		Name{Name: "TableBlockInfo", What: "a block's reflection descriptor"},
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockRead64", What: "the prologue read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "TableCookRead64", What: "the cooked header read BYTEWISE. C# spells it a Schema member, which claims nothing; DART puts it at library scope, so the claim is the union"},
		Name{Name: "TableCookInfo", What: "a cooked record's reflection descriptor"},
		Name{Name: "TableCookFieldInfo", What: "a cooked field's reflection descriptor"},
		Name{Name: "TableCookStorage", What: "what a cooked slot HOLDS, which is not always what the wire carries (§7.2)"},
		Name{Name: "TableCookRef", What: "what a Dart deref answers when it is not an offset: a null (a delta of zero) and a delta that leaves the region"},
		// THE FIXED FORM's runtime (docs/SPEC-TABLES.md §3.4), form byte 3.
		// Emitted for a unit that carries a fixed root, and claimed beside
		// the accelerators on this list's own rule: a name free today is a
		// collision the day a table in that unit becomes a fixed root. Dart's
		// privacy is per LIBRARY and a schema identifier may begin with an
		// underscore, so this backend spells NO private library-scope name at
		// all and every helper of the runtime is a static MEMBER of one of the
		// classes below — which is why the list is short.
		Name{Name: "TableFixedForm", What: "the fixed form's form byte"},
		Name{Name: "TableFixedNoGuard", What: "the guard offset a plan entry that belongs to no union arm carries"},
		Name{Name: "TableFixedOp", What: "the fixed form's plan ops — the whole set — and a text entry's flavours"},
		Name{Name: "TableFixedLane", What: "the plan's nine int32 lanes, and the five of MY side of a layout"},
		Name{Name: "TableFixedTagAt", What: "the little-endian tag load at ArgW bytes, never a prefix — a two-byte 0x0101 is not arm 1"},
		Name{Name: "TableFixedLimits", What: "the layout's framing and the two bounds a reader holds an untrusted peer's layout to"},
		Name{Name: "TableFixedRefusal", What: "the fixed form's refusals, each one BY NAME"},
		Name{Name: "TableFixedReport", What: "the fixed form's read report — §4's ledger and this form's refusals"},
		Name{Name: "TableFixedLayout", What: "a parsed layout: the bytes, the entry count, the seven named rules and the fnv1a64 hash"},
		Name{Name: "TableFixedPlan", What: "the caller's plan storage: the entries, the canonical body image and the remap pool"},
		Name{Name: "TableFixedPlanCache", What: "the plan cache BY HASH, so a compile is paid once per peer and never once per record"},
		Name{Name: "TableFixedRun", What: "THE ONE READ LOOP: one plan over one record body"},
		Name{Name: "TableFixedCompiler", What: "the plan compiler, run once per peer and never once per record"},
		Name{Name: "TableBuildVersion", What: "Dart's spelling of the unit's build version, at library scope in the block runtime home (the cook's, in a unit with no block form)"},
	)
}
