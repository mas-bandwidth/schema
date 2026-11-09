; STRICT 100 audit of the C++ and C legs (2026-11-08, main e254a5cba, re-checked at
; 38d2b4b68). Every :todo entry here is a cell whose cited evidence does not hold
; under the one read: the test is missing, red on main, does not compile against the
; strict report, or asserts an answer the spec has retired. Each goes back to :todo
; until a test that runs the row and its negative control holds it again.
; The :done entries keep a cell that holds but whose citation had gone stale; they
; point at the test that holds it on main today.
; Run with the pinned runtimes (serialize.c v2.1.1) and the fixed-form corpus built:
; SCHEMA_SLOW=1 go test ./internal/codegen/cpptable/ ./internal/codegen/ctable/,
; go test ./compiler -run 'TestCTable|TestCPPTable', the C++ `make test` recipe one
; line at a time, and ./build/conformance-harness run --only cpp.
;
; Calibration (the same as the C# and Rust audits): a cited test that passes on main
; with real assertions holds, even when it checks the emitted source; a test that
; asserts an answer the spec has retired does not.

; ---- fixed tables, C++ ----
(:id "cpp/R17" :state :done :evidence ("internal/codegen/cpptable/fixed_normalization_strict_test.go: TestCppFixedNormalizationIsDamage, TestCppFixedNormalizationIsDamageNegativeControl"))
(:id "cpp/C12" :state :done :evidence ("internal/codegen/cpptable/fixed_normalization_strict_test.go: TestCppFixedNormalizationIsDamage, TestCppFixedNormalizationIsDamageNegativeControl"))
(:id "cpp/C13" :state :done :evidence ("internal/codegen/cpptable/fixed_normalization_strict_test.go: TestCppFixedNormalizationIsDamage, TestCppFixedNormalizationIsDamageNegativeControl"))
(:id "cpp/W2" :state :todo :note "2026-11-08 audit: make tables-fixedform passes only by carrying KNOWN-RED optional/absent-payload-residue-is-copied (test/tables/versioning_lists.cpp): an absent optional's payload residue is still copied, so 'absent optional skips store' is not true in the runtime yet.")
(:id "cpp/R11" :state :done :evidence ("internal/codegen/cpptable/fixedversioning_writer_bound_count_two_endpoints_test.go: TestFixedVersioningWriterBoundCountTwoRemotes") :note "2026-11-08 audit: holds; the old citation (fixedversioning_writer_bound_count_two_peers_test.go, TestFixedVersioningWriterBoundCountTwoPeers) no longer exists. The renamed test passes with SCHEMA_SLOW=1.")
(:id "text-bytes-flags-defaults/cpp/generation" :state :done :evidence ("compiler/valuedefaults_test.go: TestTableValueDefaultsCarriers, TestFixedTableValueDefaultsEveryLeg") :note "2026-11-08 audit: holds on main; the old citation was a line link at 8ea5ed8e.")

; ---- fixed tables, C ----
(:id "c/F1" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "c/F2" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "c/F3" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "c/F7" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "c/F8" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "c/F9" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "c/F10" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/F12" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl"))
(:id "c/R7" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/R8" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/R9" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/R11" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/R12" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/R13" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "c/R17" :state :done :evidence ("internal/codegen/ctable/fixed_normalization_strict_test.go: TestCFixedNormalizationIsDamage, TestCFixedNormalizationIsDamageNegativeControl"))
(:id "c/C12" :state :done :evidence ("internal/codegen/ctable/fixed_normalization_strict_test.go: TestCFixedNormalizationIsDamage, TestCFixedNormalizationIsDamageNegativeControl"))
(:id "c/C13" :state :done :evidence ("internal/codegen/ctable/fixed_normalization_strict_test.go: TestCFixedNormalizationIsDamage, TestCFixedNormalizationIsDamageNegativeControl"))
(:id "c/R30" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl"))
(:id "c/E3/fixed-array-element" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl"))
(:id "fixed-arrays/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl"))
(:id "bool-values/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fu1.c"))
(:id "ranged-integers/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_v1.c"))
(:id "utf8-values/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "nested-types/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fu1.c"))
(:id "nested-fixed-tables/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "counted-arrays/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fx1.c"))
(:id "union-values/c/valid-data" :state :done :evidence ("internal/codegen/ctable/fixedform_strict_test.go: TestCFixedFormSourcesStrict, TestCFixedFormSourcesStrictNegativeControl" "test/c-tables/fixedform_fu1.c"))
(:id "text-bytes-flags-defaults/c/generation" :state :done :evidence ("compiler/valuedefaults_test.go: TestTableValueDefaultsCarriers, TestFixedTableValueDefaultsEveryLeg") :note "2026-11-08 audit: holds on main; the old citation was a line link at 8ea5ed8e.")

; ---- variable tables, C++ ----
(:id "vt/fixed-class/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: the conformance driver gates its form-1 read on the strict framing walk (framing_strict_ok), so the ten inert/damaged rows refuse before any value is decoded; the gate holds the driver source and the negative control removes the walk spelling in a copy of the driver source and requires the cell to go red.")
(:id "vt/text-form/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: the JSON write refuses storage that is not text (§16.14 W0) by marking the sink invalid instead of writing U+FFFD; the gate reads the emitter source and the negative control removes the refusal spelling and requires the cell to go red.")
(:id "vt/message-form/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: the strict read refuses the whole load on an out_of_range value (count 0, the value reset to its default, no damage); the runtime probe drives the generated read and the negative control removes the whole-load refusal guard in a copy of the generated header and requires the probe to go red.")
(:id "vt/widening/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: a widened value that lands outside the declared range refuses out_of_range on the raw scale, never reading it; the gate reads the emitter source and the negative control removes the refusal spelling and requires the cell to go red.")
(:id "vt/maps/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: a repeated key is a malformed map and the strict read refuses the whole load (§2.8, §6.5), never choosing a winner; the gate reads the emitter source and the negative control removes the duplicate refusal spelling and requires the cell to go red.")
(:id "vt/unbounded-arrays/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: a list read under a changed element kind refuses the whole load as kind_mismatch (§6.5), never decoding elements under defaulted kinds; the gate reads the emitter source and the negative control removes the element-kind refusal spelling and requires the cell to go red.")
(:id "vt/id-table-wire/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: the conformance driver's form-1 framing walk refuses a repeated node table (R5) and a node record no pointer reaches (T1), so node_table_twice and node_index_null refuse the whole load before any value decodes; the gate reads the driver source and the negative control removes those spellings and requires the cell to go red.")
(:id "vt/retain-unknown/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: a retaining read that cannot capture a field discards the whole load (damage + refusal) rather than continuing with a partial value (§6.6); the gate reads the emitter source and the negative control removes the capture guard spelling and requires the cell to go red.")
(:id "vt/doc-tags/cpp" :state :done :evidence ("internal/codegen/cpptable/vtcells_strict_test.go: TestCppVariableCellsStrict, TestCppVariableCellsStrictNegativeControl") :note "2026-11-09: the doc/tags negative controls plant the retired per-row spellings in a copy of the generated descriptor and require the gate to fail, holding the shared TableDocNone string claim; the gate reads the real recipe and the negative control removes the sabotage spelling and requires the cell to go red.")
(:id "vt/wire-fuzzer/cpp" :state :todo :note "2026-11-08 audit: the cited run, docs/validation/2026-11-03-cpp-nightly.json, predates the strict cut; make tables-wire-fuzz N=20000 fails today (the leg accepts a feature_array_root mutant the oracle refuses), and main's nightly C++ corpus job is red. Re-checked 2026-11-09 at ffbb0b49: stays :todo; make tables-wire-fuzz N=20000 builds in this checkout but fails after 0 mutants on feature_full truncate #0 (the oracle refuses the wire; the leg's report does not name the refusal), so its cited fuzz test still fails and the cell is not flipped.")

; ---- variable tables, C ----
(:id "vt/c/generator-panic" :state :todo :note "2026-11-08 audit: the evidence claims make tables-c exits 0; make test-c does not build on main (test/c-tables/fixedform_fx1.c and others use the retired tolerant API and report fields).")
(:id "vt/c/strict-read" :state :todo :note "2026-11-08 audit: TestCTablePlainLoadIsStrict and TestCTextListsAndTotals fail on main.")
(:id "vt/c/report-reason" :state :todo :note "2026-11-08 audit: TestCJsonHostileTextRefusals fails on main.")
(:id "vt/c/surrogates" :state :todo :note "2026-11-08 audit: TestCJsonHostileTextRefusals fails on main.")
(:id "vt/fixed-class/c" :state :todo :note "2026-11-08 audit: TestCTableWireReferenceBoundary, TestCTableWireDeepFrames and TestCTableOpenDistinctnessVerdict fail on main; the tables-c-soak negative control is red (main CI 42459510328).")
(:id "vt/optional-fields/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave fails on main.")
(:id "vt/enum-keyed-arrays/c" :state :todo :note "2026-11-08 audit: TestCTableWireReferenceBoundary fails on main.")
(:id "vt/text-form/c" :state :todo :note "2026-11-08 audit: TestCTableWireJsonIntegerDomain fails on main.")
(:id "vt/text-form-variable/c" :state :todo :note "2026-11-08 audit: TestCTableUnboundedTextListsAndMapKeys, TestCTableFeatureWitnesses and TestCJsonHostileTextRefusals fail on main.")
(:id "vt/block-read/c" :state :todo :note "2026-11-08 audit: the C conformance surfaces the row cites are not run on main: conformance (c) is killed by the 2-minute cap, and the C test sources around it do not compile against the strict cut.")
(:id "vt/cook-open/c" :state :todo :note "2026-11-08 audit: the C conformance surfaces the row cites are not run on main: conformance (c) is killed by the 2-minute cap.")
(:id "vt/build-version/c" :state :todo :note "2026-11-08 audit: TestCTableCookCanonical fails on main.")
(:id "vt/variable-class/c" :state :todo :note "2026-11-08 audit: TestCTableWireGraphIdentity and TestCTableWireAllocatorFailures fail on main.")
(:id "vt/cook-write/c" :state :todo :note "2026-11-08 audit: TestCTableCookCanonical fails on main.")
(:id "vt/wide-scalars/c" :state :todo :note "2026-11-08 audit: the C conformance surfaces the row cites are not run on main: conformance (c) is killed by the 2-minute cap.")
(:id "vt/union-table-arms/c" :state :todo :note "2026-11-08 audit: the C conformance surfaces the row cites are not run on main: conformance (c) is killed by the 2-minute cap.")
(:id "vt/pointer-arrays/c" :state :todo :note "2026-11-08 audit: TestCTableWireGraphIdentity, TestCTableWireNestedLists and TestCTableCookCanonical fail on main.")
(:id "vt/union-arrays/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave fails on main.")
(:id "vt/optional-arrays/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave fails on main.")
(:id "vt/blobs/c" :state :todo :note "2026-11-08 audit: TestCTableWireBlobIdentity fails on main; the tables-c-blob-span negative control is red (main CI 42459510328).")
(:id "vt/wire-fuzzer/c" :state :todo :note "2026-11-08 audit: the cited nightly predates the strict cut, and the C fuzz sources (test/c-tables/collections_fuzz.cpp) do not compile against it.")
(:id "vt/maps/c" :state :todo :note "2026-11-08 audit: TestCMapRepeatedKeyLastKindWins fails on main and names a retired answer: a repeated map key is damage under the one read.")
(:id "vt/union-arms-any/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave fails on main.")
(:id "vt/defaults-and-table-rename/c" :state :todo :note "2026-11-08 audit: TestCTableWireDefaultRecovery fails on main.")
(:id "vt/renames/c" :state :todo :note "2026-11-08 audit: TestCTableWireReferenceBoundary fails on main.")
(:id "vt/id-table-wire/c" :state :todo :note "2026-11-08 audit: TestCTableWireReferenceBoundary and TestCTableOpenDistinctnessVerdict fail on main; the tables-c-ref-ordinal negative control is red (main CI 42459510328).")
(:id "vt/unbounded-arrays/c" :state :todo :note "2026-11-08 audit: TestCTableUnboundedTextListsAndMapKeys and TestCTableFeatureWitnesses fail on main.")
(:id "vt/message-form/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave and TestCTableAnnouncementDistinctQuantizedShapes fail on main; the tables-c-message negative control is red (main CI 42459510328).")
(:id "vt/retain-unknown/c" :state :todo :note "2026-11-08 audit: TestCTableRetainFile and TestCTableRetainFramedDepth fail on main.")
(:id "vt/widening/c" :state :todo :note "2026-11-08 audit: TestCTableWireWidening and TestCTableMapWideningCount fail on main.")
(:id "vt/cards/c" :state :todo :note "2026-11-08 audit: make cards-c-test and cards-c-conformance do not compile (tables/cards/c/cards_test.c:401 and driver.c:132 read the retired kind_mismatch); the cards-driver-c negative control is red (main CI 42459510328).")

; ---- packet wire ----
(:id "packet/cpp/defaults" :state :done :evidence ("internal/codegen/cpp/packet_defaults_control_test.go: TestCppPacketDefaults, TestCppPacketDefaultsNegativeControl" "test/packet-defaults/Defaults.schema and testdata/wire/packet-defaults") :note "2026-11-09: TestCppPacketDefaults generates and compiles the C++ unit, checks the constructors and all eight shared pins, reads each pin to its exact bit count and refuses a one-byte-short buffer; the negative control removes only the generated Sample flags default and requires the constructor-bytes marker. Run under SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 with the whole internal/codegen/cpp package green.")
