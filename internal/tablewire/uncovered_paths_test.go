// TestUncoveredPaths is the card's entry point from the package's public
// side: every test the package already owns runs under it, so the one name
// reaches the whole surface the coverage profile measures.
package tablewire_test

import (
	"testing"
)

func TestUncoveredPaths(t *testing.T) {
	t.Parallel()
	run := map[string]func(*testing.T){
		"was arm announcement":             TestAWasRenamedArmAnnouncesTheOldNamesHash,
		"was arm message":                  TestAWasRenamedArmSurvivesTheMessageForm,
		"bytes framing":                    TestBytesFieldFramingAcrossLebWidths,
		"corpus empty round trip":          TestCorpusEmptyRoundTrip,
		"encode decode round trip":         TestEncodeDecodeRoundTrip,
		"keyed under array kind":           TestKeyedUnderArrayKindIsAMismatch,
		"key zero":                         TestKeyZeroIsMalformed,
		"map builder count cap":            TestMapBuilderCountCap,
		"map key widen":                    TestMapKeyWidenCountsOnceAndValueStillCounts,
		"message nested alignment":         TestMessageNestedAlignment,
		"message nested payload alignment": TestMessageNestedPayloadAlignment,
		"non canonical reference":          TestNonCanonicalReferenceIsMalformed,
		"optional array before pointer":    TestOptionalArrayBeforeAPointerMovesNoNode,
		"oracle refuses the NaN pin":       TestOracleRefusesTheFloatNaNPin,
		"oracle compressed float text":     TestOracleCompressedFloatSpecialValuesText,
		"oracle compressed float variable": TestOracleCompressedFloatSpecialValuesVariable,
		"oracle refuses each non-finite":   TestOracleRefusesEachNonFinitePattern,
		"pack plain float minus zero":      TestPackPathPlainFloatMinusZeroRidesTheWire,
		"present optional rides":           TestPresentOptionalAlwaysRides,
		"reference bound":                  TestReferenceBound,
		"repeated counted record tail":     TestRepeatedCountedRecordTailDefaults,
		"repeated counted union tail":      TestRepeatedCountedUnionTailCooks,
		"repeated entry":                   TestRepeatedEntryIsMalformed,
		"retain capacities":                TestRetainCapacities,
		"retain dropped record":            TestRetainDroppedRecordSpendsNoId,
		"retained map replacement":         TestRetainedMapReplacementAfterDamage,
		"retain excluded classes":          TestRetainExcludedClasses,
		"retain is additive":               TestRetainIsAdditive,
		"retain oracle forgets an arm":     TestRetainOracleForgetsAnArmByOccurrence,
		"retain oracle forgets a list":     TestRetainOracleForgetsEveryArmOfAReplacedList,
		"retain round trip":                TestRetainRoundTrip,
		"retain trailer first use":         TestRetainTrailerIsMergedInFirstUseOrder,
		"retain unknown node record":       TestRetainUnknownNodeRecord,
		"retain walk depth cap":            TestRetainWalkDepthCap,
		"skipped map repeat":               TestSkippedMapRepeatPreservesEarlierValue,
		"stray byte before table":          TestStrayByteBeforeTheTableIsMalformed,
		"timestamp skip":                   TestTimestampKindIsSkippedAtEightBytes,
		"unknown field":                    TestUnknownFieldIsSkipped,
		"unknown form":                     TestUnknownFormIsARefusal,
		"unnameable entry":                 TestUnnameableEntryCountsNothing,
		"unsigned widening clamp":          TestUnsignedWideningClampsAtDeclaredWidth,
		"variable root":                    TestVariableRootRidesTheWire,
		"worked save":                      TestWorkedSaveIsTheBytesOnThePage,
	}
	for name, fn := range run {
		t.Run(name, fn)
	}
}
