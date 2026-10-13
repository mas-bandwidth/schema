package tablenames

// Dart is the Dart table backend (internal/codegen/darttable).
//
// The Dart backend emits the BLOCK and COOK read halves, the VARIABLE wire
// (form byte 1) for the unit it owns, the FIXED wire (form byte 3) and the
// MESSAGE form's announcement (form byte 2), so this registry holds the
// accelerators' and the wire runtimes' library-scope names. Where a runtime
// name is a free function Dart spells it lowerCamelCase and the registry holds
// the PascalCase form: the two are a bijection (the packet emitter's
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
		Name{Name: "TableFixedLane", What: "the plan's eight int32 lanes, and the five of MY side of a layout"},
		Name{Name: "TableFixedLimits", What: "the layout's framing and the two bounds a reader holds an untrusted peer's layout to"},
		Name{Name: "TableFixedRefusal", What: "the fixed form's refusals, each one BY NAME"},
		Name{Name: "TableFixedReport", What: "the fixed form's read report — §4's ledger and this form's refusals"},
		Name{Name: "TableFixedLayout", What: "a parsed layout: the bytes, the entry count, the seven named rules and the fnv1a64 hash"},
		Name{Name: "TableFixedPlan", What: "the caller's plan storage: the entries, the canonical body image and the remap pool"},
		Name{Name: "TableFixedPlanCache", What: "the plan cache BY HASH, so a compile is paid once per peer and never once per record"},
		Name{Name: "TableFixedRun", What: "THE ONE READ LOOP: one plan over one record body"},
		Name{Name: "TableFixedFillRun", What: "the fixed form: copy the prefill's ranges from the default image"},
		Name{Name: "TableFixedCompiler", What: "the plan compiler, run once per peer and never once per record"},
		Name{Name: "TableFixedKnownLayout", What: "ONE LOCKED LAYOUT of a fixed table's lineage: the wire hash, the layout bytes, their length, the record size"},
		Name{Name: "TableFixedLineagePlan", What: "one lineage entry's plan, laid down from the lock's bytes, with its compile census and its refusal name"},
		Name{Name: "TableFixedLineagePlans", What: "the fixed form: one plan per lineage entry, built once from the lock's bytes and off every load path"},
		Name{Name: "TableFixedSelect", What: "the fixed form: the first lineage index whose hash is the file's, or -1 — a file is matched on this and nothing else"},
		Name{Name: "TableBuildVersion", What: "Dart's spelling of the unit's build version, at library scope in the block runtime home (the cook's, in a unit with no block form)"},
		// THE MESSAGE FORM's announcement runtime (docs/SPEC-TABLES.md §3.3),
		// form byte 2. It is emitted for a unit that declares a fixed root,
		// beside the fixed runtime, and it puts helper FUNCTIONS at library
		// scope — the same shape as the fixed form's free helpers above —
		// so each is claimed on this list's own rule: a schema declaring one
		// would otherwise generate Dart that does not compile. The C#, Go and
		// C++ legs register their announcement runtimes' spellings the same
		// way.
		Name{Name: "TableAnnouncement", What: "the announced message bit codec: the compiler-settled announcement bytes (§3.3)"},
		Name{Name: "TableAnnouncementLength", What: "the announced message bit codec: the announcement's length, emitted beside the bytes (§3.3)"},
		Name{Name: "TableMessageBits", What: "the announced message bit codec: the reference width of an n-entry vocabulary"},
		Name{Name: "TableMessageEntryEnd", What: "the announced message bit codec: step over one entry's id, kind and shape"},
		Name{Name: "TableMessageKnownKind", What: "the announced message bit codec: whether a kind is in the closed set"},
		Name{Name: "TableMessageKindWidth", What: "the announced message bit codec: the fixed width of a scalar kind's payload"},
		Name{Name: "TableMessageLeb", What: "the announced message bit codec: the canonical LEB128 length every length takes (§3)"},
		Name{Name: "TableMessageSameEntry", What: "the announced message bit codec: whether one entry's bytes repeat an earlier entry's (§3.3)"},
		Name{Name: "TableMessageShapeEnd", What: "the announced message bit codec: step over one shape's width facts"},
		Name{Name: "TableMessageSkipField", What: "the announced message bit codec: step over one unknown body field's payload by its kind (§3.3)"},
		// THE ANNOUNCEMENT SURFACE'S LIBRARY-SCOPE CLASSES. A class at
		// library scope claims a name exactly as the fixed form's classes do
		// (TableFixedReport, TableFixedPlan, ...), so TableVocabulary,
		// TableMessageEntry and TableReport are claimed here.
		//
		// THE THREE FREE-FUNCTION VERBS ARE DELIBERATELY NOT REGISTERED. The
		// page names them — announce, announceMeasure and announceRead
		// (docs/SPEC-TABLES.md §3.3, §6.1) — but the name scan in compiler/
		// tablesdart_test.go collects only identifiers the emitted source
		// spells table*/Table*, and a lowerCamelCase free function is neither,
		// so a registration here would be a claim the scan reports as
		// unemitted. The names stay claimed for every target by the Go, C++
		// and C legs (internal/tablenames/go.go, cpp.go, c.go), so the
		// checker's claim set is unchanged; registering them for Dart needs
		// the scan widened, which is not this card's file. The §11 sentence
		// that puts them in this registry is reported not done.
		Name{Name: "TableVocabulary", What: "one direction of one connection's announced id table (§3.3)"},
		Name{Name: "TableMessageEntry", What: "one announced entry: an id, a kind and a shape (§3.3)"},
		Name{Name: "TableReport", What: "the announcement read's report and this form's refusals (§3.3)"},
		Name{Name: "TableMessageBatchOpen", What: "the message form: open a batch read over an announced vocabulary (§3.3)"},
		Name{Name: "TableMessageBatchClose", What: "the message form: close a batch read and report its leftover (§3.3)"},
		Name{Name: "TableMessageRefBits", What: "the message form: the reference width of an n-entry vocabulary, emitted beside the vocabulary (§3.3)"},
		Name{Name: "TableFixedTextIllFormed", What: "the fixed form: whether a text payload's USED units are well-formed (§3.4)"},
	)
}
