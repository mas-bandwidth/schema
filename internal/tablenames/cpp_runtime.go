package tablenames

// cppRuntimeNames is the rest of the C++ table backend's namespace-scope
// surface: every helper, constant and refusal value the generated Table and
// Block sources define beside the descriptor and storage types cpp.go lists by
// hand. They are claimed on the standing rule above — a name free today must
// not become a collision the day a unit grows the construct that emits it — and
// they are held to the emitter in BOTH directions by compiler's
// TestCppTableRuntimeNamesAreClaimed (schema#414): every name the C++ emitter
// spells at namespace scope is here, and every name here is spelled.
//
// A name another backend ALSO defines is listed once, in cppSharedNames, with
// no description: define keeps the description of the first definer, and the
// entry here only adds the Cpp bit, so the claim says which backends spell it.
func cppRuntimeNames() []Name {
	var names []Name
	for _, f := range []struct {
		what  string
		names []string
	}{
		{what: "a blob, bytes or string field's storage and its views (docs/SPEC-TABLES.md §2.5, §2.12)", names: []string{
			"TableBlobMeasureThunk", "TableBlobMessageMeasureThunk", "TableBlobMessageSaveThunk",
			"TableBlobSaveThunk", "TableBlobStorage", "TableBytesSlot", "TableBytesViewOf", "TableStringSlot",
			"TableStringViewOf", "kTableBlobHeader", "kTableBlobMaxLength",
		}},
		{what: "a bound of the text form's JSON walk (§16)", names: []string{
			"kTableJsonDecimalBand",
		}},
		{what: "a file's open verdict and the refusal path (docs/SPEC-TABLES.md §6.5, §7)", names: []string{
			"TableBlockConstSpan", "TableBodyEndsEarly", "TableOpen",
		}},
		{what: "a lowercase byte helper of the cook, block or message runtime", names: []string{
			"table_cook_byteswap64", "table_float_force_round_slot", "table_message_byteswap64",
			"table_message_load64", "table_message_store64",
		}},
		{what: "a map entry's accessors (docs/SPEC-TABLES.md §2.8)", names: []string{
			"TableEntryEach", "TableEntryFound", "TableEntryKey", "TableEntryOrder", "TableEntrySetKey",
			"TableEntryValue",
		}},
		{what: "a named constant of the fixed form (docs/SPEC-TABLES.md §3.4)", names: []string{
			"kTableFixedNoGuard",
		}},
		{what: "a named constant of the message form (docs/SPEC-TABLES.md §3.3)", names: []string{
			"kTableMessageListMax", "kTableMessageVocabularyFieldId",
		}},
		{what: "a shared runtime helper or constant of the table wire (docs/SPEC-TABLES.md §3, §6)", names: []string{
			"TableExtentUnreachedEmpty", "TableKeyLength", "TableKeyOrder", "TableResetMapValue",
			"TableWireExtentElements", "TableWireExtentFn", "TableWireExtentKeyed", "kTableCookHeaderBytes",
			"kTableProbeBound", "kTableProbeShift", "kTableProbeSlots", "kTableWireForm", "kTableWireMessageForm",
		}},
		{what: "a value of the one TableRefuseReason vocabulary (docs/SPEC-TABLES.md §6.5, §7, §19.2)", names: []string{
			"batch_too_large", "count_over_bound", "layout_count_mismatch", "layout_kind_invalid",
			"layout_kind_unknown", "layout_malformed", "layout_newer", "layout_record_too_large",
			"layout_size_mismatch", "layout_too_deep", "layout_tree_unclosed", "layout_unknown", "layout_unsupported",
			"length_over_bound", "no_layout", "plan_too_large", "text_ill_formed",
		}},
		{what: "an enum's wire identity helpers (docs/SPEC-TABLES.md §3)", names: []string{
			"TableEnumNamed", "TableEnumRef", "TableEnumSlot",
		}},
		{what: "the arena and the allocator hooks under it (docs/SPEC-TABLES.md §6.3)", names: []string{
			"TableArenaAt", "TableArenaCtx", "TableArenaGrabSlab", "TableArenaGrabSpan", "TableArenaInit",
			"TableArenaShutdown", "TableDefaultAllocator", "table_allocator_complete", "table_default_alloc",
			"table_default_free",
		}},
		{what: "the bit-position and alignment helpers the wire shares (docs/SPEC-TABLES.md §3)", names: []string{
			"TableAlignBits", "TableAlignUp", "TableAlignUp64", "TableBitsRequired",
		}},
		{what: "the fixed form's runtime: plan compile, layout read and the read loop (docs/SPEC-TABLES.md §3.4)", names: []string{
			"TableFixedPut128",
			"TableFixedApply", "TableFixedClampKnownRanges", "TableFixedCopyRun", "TableFixedDepth",
			"TableFixedEntryAt", "TableFixedEntryLands", "TableFixedFills", "TableFixedGet32", "TableFixedGet64",
			"TableFixedKnownRange", "TableFixedLayFills", "TableFixedLayTable", "TableFixedPut16", "TableFixedPut32",
			"TableFixedPut64", "TableFixedPut8", "TableFixedPutF32", "TableFixedPutF64", "TableFixedRefuseScan",
		}},
		{what: "the id vocabulary and the announcement (docs/SPEC-TABLES.md §3.3)", names: []string{
			"TableIdTable", "TableIdsBytes", "TableIdsWrite", "TableLebBytes", "TableVocabularyEntryAt",
			"kTableBuildVersionFieldId",
		}},
		{what: "the map's storage, cursors, segments and sort (docs/SPEC-TABLES.md §2.8)", names: []string{
			"TableMapAppend", "TableMapCursor", "TableMapEach", "TableMapEachOf", "TableMapErase", "TableMapFill",
			"TableMapFillBegin", "TableMapFillEnd", "TableMapFillLast", "TableMapFillNext", "TableMapFillReset",
			"TableMapHash", "TableMapHead", "TableMapIndexSlots", "TableMapKeyRef", "TableMapLive", "TableMapOrder",
			"TableMapPlace", "TableMapReach", "TableMapRelease", "TableMapScan", "TableMapSegment",
			"TableMapSegmentDead", "TableMapSort", "TableMapWireExtent", "kTableMapEntryFloor",
			"kTableMapSegmentEntries",
		}},
		{what: "the message form's runtime: batches, entries, quantization and the skip walk (docs/SPEC-TABLES.md §3.3)", names: []string{
			"TableMessageArmEntry", "TableMessageBatch", "TableMessageBatchBegin", "TableMessageBatchBytes",
			"TableMessageBatchEnd", "TableMessageDequantize", "TableMessageElementRunBits", "TableMessageFixedKind",
			"TableMessageIntegerKind", "TableMessageNameEntry", "TableMessageNodeTableMeasure",
			"TableMessageNodeTableOpen", "TableMessageNodeTableSave", "TableMessageQuantization",
			"TableMessageQuantize", "TableMessageRefuseBatch", "TableMessageReserved", "TableMessageRetainBody",
			"TableMessageRetainCapture", "TableMessageRetainContent", "TableMessageRetainElement",
			"TableMessageRetainElementMinimum", "TableMessageRetainElements", "TableMessageRetainFramed",
			"TableMessageRetainId", "TableMessageRetainIn", "TableMessageRetainOpaque", "TableMessageRetainPayload",
			"TableMessageRetainRef", "TableMessageRetainScalar", "TableMessageRetainSigned",
			"TableMessageRetainWidth", "TableMessageRetainWord", "TableMessageSkipElement", "TableMessageSkipRun",
			"TableMessageSkipVariant",
		}},
		{what: "the pointered cook's node numbering, node thunks and the pack map (docs/SPEC-TABLES.md §7.6)", names: []string{
			"TableDeclRef", "TableNodeMessageMeasure", "TableNodeMessageMeasureThunk", "TableNodeMessageSave",
			"TableNodeMessageSaveThunk", "TableNodeScanBegin", "TableNodeScanNext", "TableNodeScanOpen",
			"TableNodeScanWhole", "TableNodeTableMeasure", "TableNodeTableMeasureRetain", "TableNodeTablePayload",
			"TableNodeTablePayloadRetain", "TableNodeTableSave", "TableNodeTableSaveRetain", "TableNumberingAppend",
			"TableNumberingIndex", "TableNumberingInit", "TableNumberingShutdown", "TablePackMapClose",
			"TablePackMapFind", "TablePackMapGrow", "TablePackMapInit", "TablePackMapReach", "TablePackMapReset",
			"TablePackMapShutdown", "TablePackMapSlot", "kTableNodeAbsent", "kTableNodeIndexNull",
			"kTableNodeIndexRoot", "kTableNodeRefused", "kTableNodeTableFieldId", "kTableNodeTableFieldSlot",
		}},
		{what: "the pointered root's region context a blob view resolves through (docs/SPEC-TABLES.md §6.3)", names: []string{
			"TableRegionCtx",
		}},
		{what: "the pointered text form's graph walk: the node map, the labels and the builder-form read (§16.7)", names: []string{
			"TableJsonGraphMapFind", "TableJsonGraphMapGrow", "TableJsonGraphMapInit", "TableJsonGraphMapReach",
			"TableJsonGraphMapShutdown", "TableJsonGraphMapSlot",
		}},
		{what: "the retain-unknown surface: caller-owned storage, the record walk and the framed copy (docs/SPEC-TABLES.md §6.6)", names: []string{
			"TableRetainCapture", "TableRetainClearPlaced", "TableRetainCountLost", "TableRetainDiscard",
			"TableRetainDiscardBody", "TableRetainDiscardField", "TableRetainIdsBytes", "TableRetainIdsWrite",
			"TableRetainInContent", "TableRetainInFramed", "TableRetainInId", "TableRetainInLeb",
			"TableRetainInLebRead", "TableRetainInPatch", "TableRetainInPayload", "TableRetainInRaw",
			"TableRetainInRef", "TableRetainInSlot", "TableRetainNameable", "TableRetainOutContent",
			"TableRetainOutFramed", "TableRetainOutLeb", "TableRetainOutLebRead", "TableRetainOutPayload",
			"TableRetainOutRaw", "TableRetainOutRef", "TableRetainPathRoot", "TableRetainRead32", "TableRetainRead64",
			"TableRetainRecordAt", "TableRetainRecordBytes", "TableRetainRecordDepth", "TableRetainRecordHere",
			"TableRetainRecordId", "TableRetainRecordKind", "TableRetainRecordNode", "TableRetainRecordPayload",
			"TableRetainRecordPayloadBytes", "TableRetainRecordPlaced", "TableRetainRecordSteps",
			"TableRetainRecordWire", "TableRetainReservedId", "TableRetainReset", "TableRetainStepInto",
			"TableRetainTailMeasure", "TableRetainTailSave", "TableRetainUnder", "TableRetainWrite32",
			"TableRetainWrite64", "kTableRetainDepthMax", "kTableRetainKnown", "kTableRetainKnownIds",
			"kTableRetainRecordHeader", "kTableRetainSlotBytes", "kTableRetainWalkDepthMax",
		}},
		{what: "the text field helpers: UTF-8 and UTF-16 validity, clamping and the float rung (docs/SPEC-TABLES.md §4)", names: []string{
			"TableUtf16Clamp", "TableUtf16Unit", "TableUtf8Clamp",
		}},
		{what: "the text form's JSON layer: descriptor classification and raw storage access (§16)", names: []string{
			"TableJsonCount", "TableJsonDecimalPoint", "TableJsonElementShape", "TableJsonFinite", "TableJsonGetRaw",
			"TableJsonGetSigned", "TableJsonGuardHolds", "TableJsonIntegerInDomain", "TableJsonIntegerOf",
			"TableJsonInterpret", "TableJsonInterpretExact", "TableJsonIsBytes", "TableJsonIsEnum",
			"TableJsonIsFlags", "TableJsonIsKeyed", "TableJsonIsList", "TableJsonIsMap", "TableJsonKeyedSlotKey",
			"TableJsonKeyedSlotValid", "TableJsonKindFixed", "TableJsonKindWide", "TableJsonKindWideSigned",
			"TableJsonNamed", "TableJsonSetCount", "TableJsonSetRaw", "TableJsonShape", "TableJsonValueShape",
		}},
		{what: "the text form's JSON lexer: scanning, skipping and UTF-8 (§16.1)", names: []string{
			"TableJsonDecodeUtf8", "TableJsonEncodeUtf8", "TableJsonHex4", "TableJsonLiteral", "TableJsonPeek",
			"TableJsonScanLabel", "TableJsonScanNumber", "TableJsonScanString", "TableJsonScanUnit",
			"TableJsonScanWString", "TableJsonSkipContainer", "TableJsonSkipValue", "TableJsonSkippedAmpersand",
			"TableJsonSpace", "TableJsonTokenDouble", "TableJsonUtf8", "TableJsonWalkNumber",
		}},
		{what: "the text form's JSON reader and writer walk (§16)", names: []string{
			"TableJsonRead", "TableJsonReadBlob", "TableJsonReadField", "TableJsonReadGraph", "TableJsonReadList",
			"TableJsonReadMap", "TableJsonReadPointer", "TableJsonReadScalar", "TableJsonReadTable",
			"TableJsonReadTableKeys", "TableJsonReadWide", "TableJsonWrite", "TableJsonWriteBase64",
			"TableJsonWriteField", "TableJsonWriteFields", "TableJsonWriteFloat", "TableJsonWriteGraph",
			"TableJsonWriteList", "TableJsonWriteMap", "TableJsonWriteMapIntegerKey", "TableJsonWriteMapKey",
			"TableJsonWritePointer", "TableJsonWriteScalar", "TableJsonWriteSigned", "TableJsonWriteString",
			"TableJsonWriteTimestamp", "TableJsonWriteUnsigned", "TableJsonWriteValue", "TableJsonWriteWString",
			"TableJsonWriteWide",
		}},
		{what: "the text form's RFC 3339 timestamp walk (§16.5)", names: []string{
			"TableJsonCivilFromDays", "TableJsonTimestampDays", "TableJsonTimestampDigit", "TableJsonTimestampLeap",
			"TableJsonTimestampMonthDays", "TableJsonTimestampParse",
		}},
		{what: "the text form's base64 body (§16.2)", names: []string{
			"TableJsonBase64Alphabet", "TableJsonBase64Value",
		}},
		{what: "the text form's map key and value access (§16.8)", names: []string{
			"TableJsonMapKeyField", "TableJsonMapKeyIsString", "TableJsonMapKeySigned", "TableJsonMapKeyValue",
			"TableJsonMapValueField",
		}},
		{what: "the text form's unbounded-array extent access (§16.8)", names: []string{
			"TableJsonExtentCount", "TableJsonExtentElements",
		}},
		{what: "the text form's wide-integer arithmetic over the 128-bit storage (§16.3)", names: []string{
			"TableJsonWideCompare", "TableJsonWideDiv", "TableJsonWideLoad", "TableJsonWideMulAdd",
			"TableJsonWideNeg", "TableJsonWideNegative", "TableJsonWideShl", "TableJsonWideShr", "TableJsonWideStore",
			"TableJsonWideZero",
		}},
		{what: "the unbounded array's storage, cursors and segments (docs/SPEC-TABLES.md §2.9)", names: []string{
			"TableListAppend", "TableListConst", "TableListCursor", "TableListEach", "TableListEachOf",
			"TableListElements", "TableListErase", "TableListFill", "TableListFillBegin", "TableListFillDrop",
			"TableListFillEnd", "TableListFillNext", "TableListHead", "TableListPlace", "TableListReach",
			"TableListSegment", "TableListSegmentDead", "TableListWireExtent", "kTableListSegmentElements",
		}},
		{what: "the widening ladder's reads (docs/SPEC-TABLES.md §4)", names: []string{
			"TableReadSignedAt", "TableReadUnsignedAt",
		}},
	} {
		for _, n := range f.names {
			names = append(names, Name{Name: n, What: f.what})
		}
	}
	for _, n := range cppSharedNames {
		names = append(names, Name{Name: n})
	}
	return names
}

// cppSharedNames are the names the C++ emitter spells that another backend
// registered first, with that backend's description.
var cppSharedNames = []string{
	"TableAllocator", "TableBitReader", "TableBitWriter", "TableBlob", "TableBlobAt", "TableBlockConstRows",
	"TableBytesAt", "TableBytesEmplace", "TableBytesView", "TableCookRefuse", "TableExtentCarve",
	"TableFixedCheck", "TableFixedCheckEntry", "TableFixedCompile", "TableFixedCompileEntry",
	"TableFixedCompiler", "TableFixedDst", "TableFixedEntry", "TableFixedFail", "TableFixedFill",
	"TableFixedFillRun", "TableFixedGuardLink", "TableFixedHashOf", "TableFixedKnownKind",
	"TableFixedKnownLayout", "TableFixedLayoutEntry", "TableFixedLayoutView", "TableFixedLeafSize",
	"TableFixedMatchChildren", "TableFixedOrdinalWidth", "TableFixedParseLayout", "TableFixedPush",
	"TableFixedReason", "TableFixedRun", "TableFixedSignedKind", "TableFixedSubtree", "TableFixedTagAt",
	"TableFixedTagBytes", "TableFixedUnionArmBytes", "TableFixedWidens", "TableIds", "TableJsonGraphEntry",
	"TableJsonGraphIn", "TableJsonGraphMap", "TableJsonGraphOut", "TableJsonIn", "TableJsonInteger",
	"TableJsonOut", "TableJsonWide", "TableKindWidens", "TableKindWidth", "TableListStorage",
	"TableMessageBatchClose", "TableMessageBatchOpen", "TableMessageBatchReader", "TableMessageEntry",
	"TableMessageEntryRead", "TableMessageKindBits", "TableMessageKnownKind", "TableMessageLeb",
	"TableMessageShapeFacts", "TableMessageShapeRead", "TableMessageSkip", "TableMessageSkipBody",
	"TableMessageValueBits", "TableOpenBodyStopped", "TableOpenDamaged", "TableOpenOk", "TableOpenRefused",
	"TableOpenVerdict", "TableRetainIds", "TableRetainIn", "TableRetainOut", "TableRetainPath",
	"TableRetainStep", "TableStringAt", "TableStringEmplace", "TableStringView", "TableTextFormEdit",
	"TableUtf16Valid", "TableUtf8Valid", "kTableAlign", "kTableAllocFailed", "kTableAnnounce",
	"kTableAnnounceBytes", "kTableBytesTypeId", "kTableFixedBool", "kTableFixedConst", "kTableFixedCopy",
	"kTableFixedCount", "kTableFixedEntryBytes", "kTableFixedForm", "kTableFixedHashAt",
	"kTableFixedHeaderBytes", "kTableFixedLayoutHeaderBytes", "kTableFixedMaxDepth", "kTableFixedOrdinal",
	"kTableFixedPresent", "kTableFixedRecordMaxBytes", "kTableFixedText", "kTableFixedTextBytes",
	"kTableFixedTextUtf8", "kTableFixedTextWide", "kTableFixedWiden", "kTableFixedWidenF", "kTableJsonMaxDepth",
	"kTableJsonMaxKey", "kTableJsonMaxNumber", "kTableMaxSegments", "kTableMessageBatchMax",
	"kTableMessageEntriesHere", "kTableMessageRefBitsHere", "kTableSegmentBits", "kTableSegmentMask",
	"kTableSegmentSize", "kTableSlabBytes", "kTableStringTypeId", "table_allocate", "table_block_read32",
	"table_cook_put128", "table_cook_units", "table_release",
}
