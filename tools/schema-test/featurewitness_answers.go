package main

// featureWitnessAnswers is, for each feature-witness corpus directory,
// each language whose own Go test answers it, by test name in the
// language's package (featureWitnessLangPackage). Read from the tests'
// bodies: each named test reads the directory it answers. A directory
// with no entry for a language is unanswered there, and its instance
// is red by name.
var featureWitnessAnswers = map[string]map[string][]string{
	"array-root": {
		"cs":   {"TestCSharpArrayRootClosureCorpus"},
		"dart": {"TestDartArrayRootNative"},
		"go":   {"TestArrayRootClosureNativeGo"},
		"js":   {"TestJavaScriptArrayRootClosureCorpus"},
		"rust": {"TestRustArrayRootNative"},
	},
	"array-root/closure": {
		"cs":   {"TestCSharpArrayRootClosureCorpus"},
		"dart": {"TestDartArrayRootSharedClosure"},
		"go":   {"TestArrayRootClosureNativeGo"},
		"js":   {"TestJavaScriptArrayRootClosureCorpus"},
		"rust": {"TestRustArrayRootNative"},
	},
	"blob-shared-report": {
		"java": {"TestJavaBlobSharedReportCensus"},
		"js":   {"TestJavaScriptBlobSharedReportCensus"},
	},
	"compressed-negzero-writers": {
		"cs":    {"TestCSharpVariableWriterStoresCompressedNegZeroAsPositive"},
		"unity": {"TestUnityVariableWriterStoresCompressedNegZeroAsPositive"},
	},
	"compressed-range-write": {
		"elixir": {"TestElixirCompressedRangeWriteCorpus", "TestElixirFixedCompressedStoresAreRanged"},
	},
	"counted-minimum-message": {
		"c":       {"TestCCountedMinimumMessageCorpus"},
		"lobster": {"TestLobsterCountedMinimumMessageCorpus"},
	},
	"counted-minimum-shapes": {
		"rust": {"TestRustCountedMinUnionAndPointerCarried"},
	},
	"declared-deprecated-enum": {
		"cs":    {"TestCSharpDeclaredDeprecatedEnumCookAssertsBeforeAnyByte"},
		"go":    {"TestDeclaredDeprecatedEnumCook"},
		"unity": {"TestUnityDeclaredDeprecatedEnumCookAssertsBeforeAnyByte"},
	},
	"fixed-load-capacity": {
		"go":     {"TestGoFixedLoadCapacityWitness"},
		"python": {"TestPythonFixedLoadCapacityWitness"},
	},
	"forms": {
		"rust": {"TestRustDeclaredFormsMixedClosure"},
	},
	"integrity-forms": {
		"dart": {"TestDartIntegrityNative"},
		"rust": {"TestRustIntegrityNative"},
	},
	"map-value-shape": {
		"js":   {"TestJavaScriptMapValueShapeCorpus"},
		"rust": {"TestRustMapValueShapeNative"},
	},
	"message-integrity": {},
	"one-sided-range": {
		"dart":   {"TestDartOneSidedRangeNative"},
		"elixir": {"TestOneSidedRangeConstructElixir"},
		"rust":   {"TestRustOneSidedRangeNative"},
	},
	"optional-map": {
		"dart":   {"TestDartOptionalMapSharedPins"},
		"elixir": {"TestOptionalMapConstructElixir"},
		"rust":   {"TestRustOptionalMapClosureNative", "TestRustOptionalMapNative", "TestRustOptionalMapTextRecoveryNative"},
	},
	"pointer-targets": {
		"cpp":      {"TestCppPointerTargetBlobKindRefused"},
		"elixir":   {"TestPointerTargetKindsElixir"},
		"goose":    {"TestGoosePointerTargetBlobKindRefused"},
		"js":       {"TestJsPointerTargetBlobKindRefused"},
		"moo":      {"TestMooPointerTargetBlobKindRefused"},
		"squirrel": {"TestSquirrelPointerTargetBlobKindRefused"},
	},
	"region-closure": {
		"elixir": {"TestRegionClosureElixir"},
	},
	"required": {
		"dart": {"TestDartRequiredSharedPins"},
	},
	"required/compressed-writers": {
		"rust": {"TestRustCompressedVariableNative"},
	},
	"required/cook-guard": {
		"elixir": {"TestRequiredCookGuardStorageElixir"},
	},
	"required/default-writers": {
		"rust": {"TestRustRequiredDefaultWritersNative"},
	},
	"required/nested-guards": {
		"dart":   {"TestDartRequiredNestedGuardsSharedClosure"},
		"elixir": {"TestRequiredNestedGuardCorpusElixir"},
		"js":     {"TestJavaScriptNestedRequiredGuards"},
		"rust":   {"TestRustRequiredJSONNative"},
	},
	"required/range-writers": {
		"rust": {"TestRustRequiredRangeWritersNative"},
	},
	"required/rust-json": {
		"rust": {"TestRustRequiredJSONNative"},
	},
	"required/wire-presence": {
		"rust": {"TestRustRequiredWireReadNative"},
	},
	"retained-replacement/fixed_table": {
		"dart":   {"TestDartRetainedInlineReplacement"},
		"elixir": {"TestRetainedReplacementCorpusElixir"},
	},
	"retained-replacement/fixed_union": {
		"dart":   {"TestDartRetainedInlineReplacement"},
		"elixir": {"TestRetainedReplacementCorpusElixir"},
	},
	"retained-replacement/keyed_table": {
		"dart":   {"TestDartRetainedInlineReplacement"},
		"elixir": {"TestRetainedReplacementCorpusElixir"},
	},
	"retained-replacement/table": {
		"dart":   {"TestDartRetainedInlineReplacement"},
		"elixir": {"TestRetainedReplacementCorpusElixir"},
	},
	"retained-replacement/union": {
		"dart":   {"TestDartRetainedInlineReplacement"},
		"elixir": {"TestRetainedReplacementCorpusElixir"},
	},
	"text-list-total": {
		"dart": {"TestDartTextListTotalNative"},
		"rust": {"TestRustTextTotalNative"},
	},
	"text-lists": {
		"rust": {"TestRustTextListsNative"},
	},
	"timestamp": {
		"dart":   {"TestDartTimestampNative"},
		"elixir": {"TestTimestampConstructElixir"},
		"rust":   {"TestRustTimestampFixedAndMessageNative", "TestRustTimestampNative", "TestRustTimestampUnknownMessageNative"},
	},
	"timestamp/rust-boundaries": {
		"elixir": {"TestTimestampConstructElixir"},
		"rust":   {"TestRustTimestampFixedAndMessageNative", "TestRustTimestampNative", "TestRustTimestampUnknownMessageNative"},
	},
	"unbounded-text/complete": {
		"rust": {"TestRustUnboundedTextNative"},
	},
	"unbounded-text/cross-file": {},
	"unbounded-text/list":       {},
	"unbounded-text/map":        {},
}
