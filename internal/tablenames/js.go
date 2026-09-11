package tablenames

// Js is the JavaScript table backend (internal/codegen/jstable). It is the
// READING TIER: no struct layout, so it reads both accelerators and
// produces neither.
const Js Backend = 1 << 7

func init() {
	define(Js,
		Name{Name: "TableBlockMagic", What: "the block prologue's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableBlockLayout", What: "the layout contract's check, run once. C# spells it a nested class of Schema; Java puts it at package scope and JavaScript at module scope, so the claim is the UNION"},
		Name{Name: "TableBlockByteOrder", What: "this build's byte order, as the prologue carries it", RustConst: true},
		Name{Name: "TableCookMagic", What: "the cooked header's magic, and the byte-order check with it", RustConst: true},
		Name{Name: "TableCookByteOrder", What: "this build's byte order, as a cooked header records it", RustConst: true},
		Name{Name: "TableCookMaxAlign", What: "the greatest region alignment a cooked header may name", RustConst: true},
		Name{Name: "TableCookHeaderBytes", What: "§7.1's 64-byte header. C# spells it a Schema member, which claims nothing; Rust puts it at module level, Go at package scope and JavaScript at module scope, so the claim is the UNION and the name is claimed", RustConst: true},
		Name{Name: "TableCookLayout", What: "the cook closure's layout contract, run once (§20.3)"},
		// ---- THE FIXED FORM (docs/SPEC-TABLES.md §3.4), which JavaScript carries
		// whole. Every one of these is a MODULE-SCOPE BINDING of the unit's
		// <Package>Table.js, so every one of them can collide with a declaration
		// and every one is claimed — the private helpers of the compiler
		// included, which is why they carry the family's own prefix instead of
		// the bare spellings a reader would reach for first. A runtime that
		// spelled its own scratch `push` or `coalesce` would be asking the
		// FRONT END to refuse those words in every schema on every target, for
		// a name nothing outside one generated module can see. Go's port made
		// the same call (tableFixedPush, tableFixedCompileEntry) and Dart's did
		// (TableFixedOp, TableFixedLane); this is that rule in JavaScript's
		// spelling. Nothing here is Scoped: an ES module is file-scoped, but
		// the file it scopes is the one the unit's own declarations are emitted
		// into, so module scope IS collision scope.

		// the form byte, its two neighbours in the ordered registry, and §3's
		// one header rule — the form byte at 0, seven reserved zero bytes, the
		// layout hash at 8, the body at 16
		Name{Name: "TableFixedForm", What: "the fixed form's form byte (form 3)"},
		Name{Name: "TableFixedVariableForm", What: "form 1, the variable form: named so a form byte BEFORE this one refuses as previous_form rather than as a newer form (§3)"},
		Name{Name: "TableFixedMessageForm", What: "form 2, the message form: named so a WIRE handed to a file reader refuses as message_form_as_file (§3)"},
		Name{Name: "TableFixedHeaderBytes", What: "§3's header: sixteen bytes, the body at 16"},
		Name{Name: "TableFixedHashAt", What: "§3's header: the layout hash at offset 8"},

		// THE LAYOUT — what form 1 calls the vocabulary block (§3.4) — its
		// framing, its view and the five reads a walk of it makes
		Name{Name: "TableFixedLayoutHeaderBytes", What: "the layout's own framing: the u32 entry count, and nothing else"},
		Name{Name: "TableFixedEntryBytes", What: "a layout entry is seventeen bytes, and the format never moves"},
		Name{Name: "TableFixedRecordMaxBytes", What: "the fixed form: §3.4's 65536-byte record ceiling"},
		Name{Name: "TableFixedMaxDepth", What: "the fixed form: a bound on the layout WALK, not on the wire"},
		Name{Name: "TableFixedLayoutView", What: "a parsed layout: the bytes, the offset, the entry count and the named refusal"},
		Name{Name: "TableFixedParseLayout", What: "a layout is refused WHOLE or not at all — each of §1.1's seven rules under its own name"},
		Name{Name: "TableFixedKnownKind", What: "whether a kind is in the closed set — a kind outside it is layout_kind_unknown, never stepped over"},
		Name{Name: "TableFixedLeafSize", What: "a leaf kind's admitted sizes — 1 ok, 0 size mismatch, -1 not a leaf"},
		Name{Name: "TableFixedOrdinalWidth", What: "whether n is an ordinal width: 1, 2, 4 or 8"},
		Name{Name: "TableFixedFail", What: "keep the FIRST layout-refusal reason and stop"},
		Name{Name: "TableFixedClearView", What: "a failed parse sets nothing: bytes, offset and count go back to empty"},
		Name{Name: "TableFixedCheckEntry", What: "validate the subtree at i: kind, size, shape, depth, then how many entries it occupies"},
		Name{Name: "TableFixedRefusalName", What: "the refusal's own name, matching the C++ reference's spellings"},
		Name{Name: "TableFixedIdLo", What: "an entry's wire id, low lane: an id is compared, never arithmetic, so it never becomes a BigInt"},
		Name{Name: "TableFixedIdHi", What: "an entry's wire id, high lane"},
		Name{Name: "TableFixedKind", What: "an entry's kind byte"},
		Name{Name: "TableFixedSize", What: "an entry's constant size"},
		Name{Name: "TableFixedChildren", What: "an entry's child count"},
		Name{Name: "TableFixedSubtree", What: "how many entries the subtree at i occupies — what makes SKIPPING an unknown field free"},
		Name{Name: "TableFixedUnionArmBytes", What: "the widest arm of a union entry"},
		Name{Name: "TableFixedTagBytes", What: "a union entry's tag width: its size less its widest arm"},
		Name{Name: "TableFixedSameId", What: "whether two entries carry one wire id, compared in two uint32 lanes"},
		Name{Name: "TableFixedGetU32", What: "the little-endian u32 read the framing itself needs"},
		Name{Name: "TableFixedTagAt", What: "the little-endian tag load at ArgW bytes, never a prefix — a two-byte 0x0101 is not arm 1"},

		// THE PLAN: nine int32 lanes of one flat Int32Array, and the lane
		// indices ONE BINDING PER LINE. A lane folded into a comma list beside
		// another is a lane nobody claimed.
		Name{Name: "TableFixedLanes", What: "a plan entry is nine int32 lanes, which is what keeps the read loop monomorphic"},
		Name{Name: "TableFixedLaneOp", What: "the plan lane holding the op"},
		Name{Name: "TableFixedLaneSrc", What: "the plan lane holding the source offset"},
		Name{Name: "TableFixedLaneDst", What: "the plan lane holding the destination offset"},
		Name{Name: "TableFixedLaneSize", What: "the plan lane holding the byte count"},
		Name{Name: "TableFixedLaneAux", What: "the plan lane holding an entry's second offset — a text payload, a remap table"},
		Name{Name: "TableFixedLaneGuard", What: "the plan lane holding the union tag an arm's entry runs under"},
		Name{Name: "TableFixedLaneArg", What: "the plan lane holding the guard's VALUE: the tag an arm's entry answers to"},
		Name{Name: "TableFixedLaneMeta", What: "the plan lane holding what an op needs beside its size — a widen's width and sign, a text entry's flavour"},
		Name{Name: "TableFixedLaneArgW", What: "the plan lane holding the guard's WIDTH IN BYTES: a union tag is one, two, four or eight"},
		Name{Name: "TableFixedNoGuard", What: "the guard a plan entry belonging to no union arm carries"},

		// THE OPS ARE THE WHOLE SET, one binding each, and the flavours a text
		// entry lands its units under
		Name{Name: "TableFixedOpCopy", What: "the plan op that moves size bytes — the identity plan's only one"},
		Name{Name: "TableFixedOpCount", What: "the plan op that clamps a count to the reader's own bound"},
		Name{Name: "TableFixedOpText", What: "the plan op that lands a length and then the units"},
		Name{Name: "TableFixedOpOrdinal", What: "the plan op that remaps a variant ordinal through the plan's own table"},
		Name{Name: "TableFixedOpWiden", What: "the plan op that widens a narrower source by its sign bit"},
		Name{Name: "TableFixedOpConst", What: "the plan op that lands a constant this reader's storage takes: a remapped union tag"},
		Name{Name: "TableFixedOpWidenF", What: "the plan op that widens an f32 into an f64, §4's float rung"},
		Name{Name: "TableFixedTextUtf8", What: "a text entry's utf-8 flavour"},
		Name{Name: "TableFixedTextWide", What: "a text entry's wide flavour, whose units are two bytes"},
		Name{Name: "TableFixedTextBytes", What: "a text entry's raw-bytes flavour, which terminates nothing"},

		// the plan's storage, the cache that pays a compile once per peer, and
		// §4's ledger with this form's refusals beside it
		Name{Name: "TableFixedPlan", What: "the caller's plan storage: the entries, the canonical body image and the remap pool"},
		Name{Name: "TableFixedPlanCache", What: "the plan cache BY HASH, so a compile is paid once per peer and never once per record"},
		Name{Name: "TableFixedReport", What: "the fixed form's read report — §4's six counters and the refusal beside them"},
		Name{Name: "TableFixedResetReport", What: "the report reset, which the caller owns and the codec never allocates"},
		Name{Name: "TableFixedRefusal", What: "the fixed form's refusals, each one BY NAME — the three directions a form byte can be wrong in, and the seven named layout rules"},

		// the hash, the ONE read loop, and the plan compiler with its own
		// private walk
		Name{Name: "TableFixedHashOf", What: "fnv1a64 over the layout's bytes, in two uint32 lanes because a BigInt on a per-record compare is an allocation per record"},
		Name{Name: "TableFixedHashOut", What: "the hash's two-lane out-parameter, consumed in the call that fills it"},
		Name{Name: "TableFixedRun", What: "THE ONE READ LOOP: one plan over one record body, and the only thing that differs between this build's own record and anybody else's is which plan it is handed"},
		Name{Name: "TableFixedLand", What: "mark one dest range the plan writes, so the complement is the holes"},
		Name{Name: "TableFixedHoles", What: "the complement of the unguarded dest writes — what the prefill copies into"},
		Name{Name: "TableFixedConv", What: "the sixteen-byte float conversion scratch, the flat packet tier's SC twin"},
		Name{Name: "TableFixedCompile", What: "the plan compiler, run once per peer and never once per record"},
		Name{Name: "TableFixedCompileEntry", What: "the compiler's per-entry walk, matching one of the writer's entries against one of mine"},
		Name{Name: "TableFixedMatchChildren", What: "the compiler's walk of a table's children on both sides, by id — and the count of every field of theirs I could not name"},
		Name{Name: "TableFixedPush", What: "the compiler's one plan-entry append, which refuses rather than grows"},
		Name{Name: "TableFixedCoalesce", What: "the only optimization a plan compiler performs: two neighbouring copies that advance together are one entry"},
		Name{Name: "TableFixedWidens", What: "§4's widening rungs: whether a kind that MOVED merely grew"},
		Name{Name: "TableFixedSignedKind", What: "whether a kind widens by its sign bit"},
		Name{Name: "TableFixedLayRemap", What: "an enum's remap table, laid into the plan's own pool"},
		Name{Name: "TableFixedRemapScratch", What: "an enum's remap, built here and then laid down"},
		Name{Name: "TableFixedDstLanes", What: "MY side of the layout is five int32 lanes per entry: the storage facts a layout entry cannot carry"},
		Name{Name: "TableFixedDstOff", What: "my side's lane holding the offset into the canonical image"},
		Name{Name: "TableFixedDstStride", What: "my side's lane holding an array element's stride"},
		Name{Name: "TableFixedDstAux", What: "my side's lane holding a second offset — a text payload, an optional's present byte"},
		Name{Name: "TableFixedDstCounted", What: "my side's lane saying whether an array carries a count ahead of its elements"},
		Name{Name: "TableFixedDstArg", What: "my side's lane holding a text field's flavour"},

		Name{Name: "BuildVersion", What: "the unit's build version (docs/SPEC-TABLES.md §20). C# spells it a member of Schema, which claims nothing; C++, Go, Rust, Java, Elixir and JavaScript put it at unit scope — Java in a file of its own name, Elixir as a module, JavaScript as a module-scope export — so the claim is the union. C does NOT emit this spelling: an object-like macro carrying a common PascalCase identifier rewrites it everywhere in the consumer's own translation unit, which no front end can refuse, so the C backend spells the value SCHEMA_<PKG>_BUILD_VERSION_VALUE under the reserved prefix (internal/check's cReservedMacros)", RustConst: true},
	)
}
