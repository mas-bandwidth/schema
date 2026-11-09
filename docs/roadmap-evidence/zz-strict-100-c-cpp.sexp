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
(:id "cpp/R17" :state :todo :note "2026-11-08 audit: the cited subtest asserts the retired normalisation (a bool or present byte that is not 0/1 reads as true and counts nothing). FIXED-FORM-ALGORITHM.md fix 2: that byte refuses wire_damaged. The C++ fixed runtime still normalises (d[i] = s[i] != 0).")
(:id "cpp/C12" :state :todo :note "2026-11-08 audit: a bool byte other than 0/1 must refuse wire_damaged (FIXED-FORM-ALGORITHM.md fix 2); the C++ fixed runtime normalises it, and the cited subtest checks the emitted text for the normalisation.")
(:id "cpp/C13" :state :todo :note "2026-11-08 audit: a present byte other than 0/1 must refuse wire_damaged (FIXED-FORM-ALGORITHM.md fix 2); the C++ fixed runtime normalises it, and the cited subtest checks the emitted text for the normalisation.")
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
(:id "vt/fixed-class/cpp" :state :todo :note "2026-11-08 audit: make tables-zero-cost fails (pointer, map or list machinery in build/tables-generated/v1/V1Table.h), and conformance (cpp) framing reads ten form-1 rows that must refuse (array_L0_inert, array_L1_inert, frame_repeat_last_wins, frame_unknown_id_length, frame_unknown_id_reference, keyed_L1_inert, map_L1_inert, node_index_null, node_table_twice, node_unknown_to_fixed_reader).")
(:id "vt/text-form/cpp" :state :todo :note "2026-11-08 audit: conformance (cpp) json-write is red on text_refuse_illformed and text_refuse_unpaired (main CI 42459510328): ToJson writes U+FFFD for storage that is not text instead of refusing (SPEC-TABLES §16.14 W0). PR #2966 fixes it.")
(:id "vt/wstring/cpp" :state :todo :note "2026-11-08 audit: ./build/schema_test_wide_table fails on main (test/wide/table_main.cpp:535-536, the unpaired-surrogate write), and the cited wide-table-clamp-negative-control no longer exists. PR #2966 fixes the write.")
(:id "vt/message-form/cpp" :state :todo :note "2026-11-08 audit: ./build/schema_test_tables has 6 failures in test/tables/message_form.h (test_message_form_findings, test_message_form_cold_read: a narrow offset out of range and a few_count past bound read instead of refusing), and make tables-message-form-negative-control fails.")
(:id "vt/widening/cpp" :state :todo :note "2026-11-08 audit: conformance (cpp) report list_scalar_widen reads where it must refuse, and the message_form.h tests the row cites share the binary that fails (6 failures).")
(:id "vt/maps/cpp" :state :todo :note "2026-11-08 audit: make tables-maps-negative-controls fails (tables-maps-duplicate on main CI 42459510328), and conformance (cpp) framing reads map_L1_inert.")
(:id "vt/unbounded-arrays/cpp" :state :todo :note "2026-11-08 audit: make tables-lists-negative-controls fails (tables-lists-element-kind on main CI 42459510328), and conformance (cpp) framing reads array_L0_inert and array_L1_inert.")
(:id "vt/id-table-wire/cpp" :state :todo :note "2026-11-08 audit: make tables-ref-ordinal-shared-negative-control fails, and conformance (cpp) framing reads node_index_null and node_table_twice.")
(:id "vt/retain-unknown/cpp" :state :todo :note "2026-11-08 audit: make tables-wire-fuzz-retain and its negative control fail, and the cited tables-message-form-retain-negative-control no longer exists.")
(:id "vt/doc-tags/cpp" :state :todo :note "2026-11-08 audit: make tables-doctags-negative-controls fails (main CI 42459510328).")
(:id "vt/wire-fuzzer/cpp" :state :todo :note "2026-11-08 audit: the cited run, docs/validation/2026-11-03-cpp-nightly.json, predates the strict cut; make tables-wire-fuzz N=20000 fails today (the leg accepts a feature_array_root mutant the oracle refuses), and main's nightly C++ corpus job is red.")

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
(:id "vt/wstring/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave fails on main.")
(:id "vt/unbounded-arrays/c" :state :todo :note "2026-11-08 audit: TestCTableUnboundedTextListsAndMapKeys and TestCTableFeatureWitnesses fail on main.")
(:id "vt/message-form/c" :state :todo :note "2026-11-08 audit: TestCTableMessageSave and TestCTableAnnouncementDistinctQuantizedShapes fail on main; the tables-c-message negative control is red (main CI 42459510328).")
(:id "vt/retain-unknown/c" :state :todo :note "2026-11-08 audit: TestCTableRetainFile and TestCTableRetainFramedDepth fail on main.")
(:id "vt/widening/c" :state :todo :note "2026-11-08 audit: TestCTableWireWidening and TestCTableMapWideningCount fail on main.")
(:id "vt/cards/c" :state :todo :note "2026-11-08 audit: make cards-c-test and cards-c-conformance do not compile (tables/cards/c/cards_test.c:401 and driver.c:132 read the retired kind_mismatch); the cards-driver-c negative control is red (main CI 42459510328).")

; ---- packet wire ----
(:id "packet/cpp/defaults" :state :todo :note "2026-11-08 audit: make packet-defaults-cpp passes, but no negative control holds the C++ defaults gate (only the other legs' controls exist).")
