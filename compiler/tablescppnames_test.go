// The C++ leg's recorded name-claim baseline (schema #414). This is not the
// positive both-ways scan the other table backends carry; it is the honest
// remainder until one can land, and it says so out loud.
package compiler

import (
	"sort"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// cppUnregisteredBaseline is #414's measured cpp remainder at the sha that
// recorded it: every namespace-scope runtime spelling the C++ emitter writes
// for the runtimeSrc corpus that internal/tablenames does NOT register. The
// list is a ceiling and may only shrink — a name the emitter adds and nobody
// registers fails TestCppEmittedRuntimeNamesAreClaimedOrRecorded by NAME, and
// a name somebody registers must be struck from the list. The both-ways scan
// every other leg has is not here, and cannot be guessed at inside this file's
// paths: 161 emitted names are unregistered outright, a further 3 registered
// names never appear as an exact spelling (the emitter writes TableRegionCtx
// and kTableSlabBytes and the registry claims the family by prefix), and a
// further 20 are the refusal vocabulary cppRuntimeIdent cannot match by
// construction. TestCppRuntimeNameScanGoesRed is the control that keeps the
// collector this test reads above honest.
var cppUnregisteredBaseline = []string{
	"TableAlignBits",
	"TableBitsRequired",
	"TableBlockConstSpan",
	"TableBodyEndsEarly",
	"TableCookRefuse",
	"TableEnumNamed",
	"TableEnumRef",
	"TableEnumSlot",
	"TableFixedApply",
	"TableFixedCheckEntry",
	"TableFixedCompile",
	"TableFixedCompileEntry",
	"TableFixedCopyRun",
	"TableFixedDepth",
	"TableFixedEntryAt",
	"TableFixedFail",
	"TableFixedGet32",
	"TableFixedGet64",
	"TableFixedHashOf",
	"TableFixedKnownKind",
	"TableFixedLayTable",
	"TableFixedLeafSize",
	"TableFixedMatchChildren",
	"TableFixedOrdinalWidth",
	"TableFixedParseLayout",
	"TableFixedPush",
	"TableFixedPut16",
	"TableFixedPut32",
	"TableFixedPut64",
	"TableFixedPut8",
	"TableFixedPutF32",
	"TableFixedPutF64",
	"TableFixedRun",
	"TableFixedSignedKind",
	"TableFixedSubtree",
	"TableFixedTagBytes",
	"TableFixedUnionArmBytes",
	"TableFixedWidens",
	"TableIdTable",
	"TableIdsBytes",
	"TableIdsWrite",
	"TableJsonBase64Alphabet",
	"TableJsonBase64Value",
	"TableJsonCount",
	"TableJsonDecimalPoint",
	"TableJsonDecodeUtf8",
	"TableJsonElementShape",
	"TableJsonEncodeUtf8",
	"TableJsonFinite",
	"TableJsonGetRaw",
	"TableJsonGetSigned",
	"TableJsonGuardHolds",
	"TableJsonHex4",
	"TableJsonIntegerInDomain",
	"TableJsonIntegerOf",
	"TableJsonInterpret",
	"TableJsonInterpretExact",
	"TableJsonIsBytes",
	"TableJsonIsEnum",
	"TableJsonIsFlags",
	"TableJsonIsKeyed",
	"TableJsonIsList",
	"TableJsonIsMap",
	"TableJsonKeyedSlotKey",
	"TableJsonKeyedSlotValid",
	"TableJsonKindFixed",
	"TableJsonKindWide",
	"TableJsonKindWideSigned",
	"TableJsonLiteral",
	"TableJsonNamed",
	"TableJsonPeek",
	"TableJsonRead",
	"TableJsonReadField",
	"TableJsonReadList",
	"TableJsonReadMap",
	"TableJsonReadPointer",
	"TableJsonReadScalar",
	"TableJsonReadTable",
	"TableJsonReadTableKeys",
	"TableJsonReadWide",
	"TableJsonScanNumber",
	"TableJsonScanString",
	"TableJsonScanUnit",
	"TableJsonScanWString",
	"TableJsonSetCount",
	"TableJsonSetRaw",
	"TableJsonShape",
	"TableJsonSkipContainer",
	"TableJsonSkipValue",
	"TableJsonSkippedAmpersand",
	"TableJsonSpace",
	"TableJsonTokenDouble",
	"TableJsonUtf8",
	"TableJsonValueShape",
	"TableJsonWalkNumber",
	"TableJsonWideCompare",
	"TableJsonWideDiv",
	"TableJsonWideLoad",
	"TableJsonWideMulAdd",
	"TableJsonWideNeg",
	"TableJsonWideNegative",
	"TableJsonWideShl",
	"TableJsonWideShr",
	"TableJsonWideStore",
	"TableJsonWideZero",
	"TableJsonWrite",
	"TableJsonWriteBase64",
	"TableJsonWriteField",
	"TableJsonWriteFields",
	"TableJsonWriteFloat",
	"TableJsonWriteList",
	"TableJsonWriteMap",
	"TableJsonWritePointer",
	"TableJsonWriteScalar",
	"TableJsonWriteSigned",
	"TableJsonWriteString",
	"TableJsonWriteUnsigned",
	"TableJsonWriteValue",
	"TableJsonWriteWString",
	"TableJsonWriteWide",
	"TableKindWidens",
	"TableLebBytes",
	"TableMessageArmEntry",
	"TableMessageBatch",
	"TableMessageBatchBegin",
	"TableMessageBatchBytes",
	"TableMessageBatchClose",
	"TableMessageBatchEnd",
	"TableMessageBatchOpen",
	"TableMessageBatchReader",
	"TableMessageDequantize",
	"TableMessageElementRunBits",
	"TableMessageEntryRead",
	"TableMessageFixedKind",
	"TableMessageIntegerKind",
	"TableMessageKindBits",
	"TableMessageKnownKind",
	"TableMessageLeb",
	"TableMessageNameEntry",
	"TableMessageNodeTableOpen",
	"TableMessageQuantization",
	"TableMessageQuantize",
	"TableMessageRefuseBatch",
	"TableMessageReserved",
	"TableMessageShapeRead",
	"TableMessageSkip",
	"TableMessageSkipBody",
	"TableMessageSkipElement",
	"TableMessageSkipRun",
	"TableMessageSkipVariant",
	"TableMessageValueBits",
	"TableOpen",
	"TableReadSignedAt",
	"TableUtf8Clamp",
	"TableUtf8Valid",
	"TableVocabularyEntryAt",
	"table_cook_byteswap64",
	"table_float_force_round_slot",
	"table_message_byteswap64",
	"table_message_load64",
	"table_message_store64",
}

// TestCppEmittedRuntimeNamesAreClaimedOrRecorded runs the C++ leg's collector
// over the runtimeSrc corpus and partitions every name it sees on the
// registry. The registered names are what the checker already protects; the
// unregistered ones are #414's cpp remainder, and this test holds them to the
// recorded baseline so the gap can only shrink. The "OrRecorded" suffix is the
// honest half: this is not a both-ways ...AreClaimed scan, and it must not be
// mistaken for one.
func TestCppEmittedRuntimeNamesAreClaimedOrRecorded(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	emitted := cppEmittedNames(files)
	if len(emitted) == 0 {
		t.Fatal("the scan found no runtime identifier in the emitted C++ at all — the scan, not the registry, is what broke")
	}
	seen := make([]string, 0, len(emitted))
	for name := range emitted {
		seen = append(seen, name)
	}
	sort.Strings(seen)
	unregistered := make([]string, 0, len(seen))
	for _, name := range seen {
		if !tablenames.Registered(name) {
			unregistered = append(unregistered, name)
		}
	}
	baseline := make(map[string]bool, len(cppUnregisteredBaseline))
	for _, name := range cppUnregisteredBaseline {
		baseline[name] = true
	}
	unregSet := make(map[string]bool, len(unregistered))
	for _, name := range unregistered {
		unregSet[name] = true
	}
	for _, name := range unregistered {
		if !baseline[name] {
			t.Errorf("the C++ table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate C++ that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range cppUnregisteredBaseline {
		if !unregSet[name] {
			t.Errorf("the baseline records %q as unregistered but the scan no longer sees it that way — "+
				"either it was registered (strike it from the list) or it is no longer emitted", name)
		}
	}
	t.Logf("cpp emitted-name scan saw %d runtime identifiers, %d of them unregistered", len(seen), len(unregistered))
}
