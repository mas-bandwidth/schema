; STRICT 100 audit of the go leg (2026-11-08, main e254a5cba). Every entry here is a
; cell whose cited evidence does not hold under the one read: the test is missing,
; red on main, or its evidence predates the strict cut. Each goes back to :todo
; until a test that runs the row and its negative control holds it again.
; Run with the pinned runtimes (serialize v2.1.3, serialize.go v2.1.1) and the
; fixed-form corpus built: SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/gotable/.
(:id "go/R11" :state :todo :note "2026-11-08 audit: TestFixedVersioningWriterBoundCount fails on main (the forged writer_bound_count file refuses count_over_bound, and the test still pins the retired read-one-record answer), and TestFixedVersioningWriterBoundCountTwoPeers no longer exists.")
(:id "go/C14" :state :todo :note "2026-11-08 audit: the cited TestFixedFormClampLiveCount no longer exists (make/go.mk tables-go-fixedform still names it; tools/slowgatescan TestProofListNamesOnlyExistingTests is red on it).")
(:id "vt/fixed-class/go" :state :todo :note "2026-11-08 audit: TestWireLargeVocabularyAndZeroId (and TestFormVerdicts inside it) fails on main, and the report conformance surface is red on previous_form_wide_blob (main CI 42459510328, conformance (go)).")
(:id "vt/optional-fields/go" :state :todo :note "2026-11-08 audit: TestCookWriterMatchesIndependentLayout/fixed (the ?Child row) fails on main: the plain RootLoad at a declared fixed root reads form 1 and refuses the engine's form-3 file as newer_form.")
(:id "vt/text-form/go" :state :done :evidence ("internal/codegen/gotable/vt_text_cook_message_test.go: TestGoTextCookMessageCellsStrict" "internal/codegen/gotable/vt_text_cook_message_test.go: TestGoTextCookMessageCellsStrictNegativeControl"))
(:id "vt/cook-write/go" :state :done :evidence ("internal/codegen/gotable/vt_text_cook_message_test.go: TestGoTextCookMessageCellsStrict" "internal/codegen/gotable/vt_text_cook_message_test.go: TestGoTextCookMessageCellsStrictNegativeControl"))
(:id "vt/union-arrays/go" :state :todo :note "2026-11-08 audit: the cited TestMalformedRepeatedUnionKeepsPriorValue no longer exists (test/go-tables has TestDamagedRepeatedUnionRefuses); the evidence names a retired last-wins answer.")
(:id "vt/maps/go" :state :todo :note "2026-11-08 audit: the cited TestGoMapRepeatedKeyLastKindWins no longer exists; a repeated map key is damage under the one read and no Go test cited here holds that.")
(:id "vt/id-table-wire/go" :state :todo :note "2026-11-08 audit: TestWireLargeVocabularyAndZeroId (with TestFormVerdicts and TestLEBTenthByteOverflow inside it) fails on main.")
(:id "vt/wstring/go" :state :todo :note "2026-11-08 audit: TestCookWriterMatchesIndependentLayout/fixed (the words wstring(8) arm) fails on main: the plain RootLoad at a declared fixed root refuses the engine's form-3 file.")
(:id "vt/unbounded-arrays/go" :state :todo :note "2026-11-08 audit: TestBuilderCountRecovery fails on main (damage must discard the whole load; the builder answers count_over_length).")
(:id "vt/message-form/go" :state :done :evidence ("internal/codegen/gotable/vt_text_cook_message_test.go: TestGoTextCookMessageCellsStrict" "internal/codegen/gotable/vt_text_cook_message_test.go: TestGoTextCookMessageCellsStrictNegativeControl"))
(:id "vt/retain-unknown/go" :state :todo :note "2026-11-08 audit: the cited TestRetainReplacementAndShapeChanges no longer exists, and the retain and retain-save conformance surfaces are red on retain_node_index (main CI 42459510328, conformance (go)).")
(:id "vt/go/retain-gate-stale-names" :state :todo :note "2026-11-08 audit: tools/slowgatescan TestProofListNamesOnlyExistingTests is red on main: make/go.mk tables-go-retain names TestRetainReplacementAndShapeChanges and tables-go-fixedform names TestFixedFormClampLiveCount, neither of which exists.")
(:id "vt/widening/go" :state :todo :note "2026-11-08 audit: the cited report conformance surface is red on main (previous_form_wide_blob: got read, want refused; main CI 42459510328, conformance (go)).")
(:id "vt/wire-fuzzer/go" :state :todo :note "2026-11-08 audit: the cited run, docs/validation/2026-11-02-go-nightly.json, predates the strict cut, and tables-wire-fuzz-release-go has not finished green on main since (run 42466880953 failed before it ran).")
