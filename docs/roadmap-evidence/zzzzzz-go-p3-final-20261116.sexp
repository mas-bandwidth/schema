;; The native sanitizer applies to both outer tests and generated subprocesses.
(:id "go/P3" :state :done
 :evidence ("internal/codegen/gotable/fixedform_test.go: TestFixedFormHostileByteSweep"
            "internal/codegen/gotable/fixedoracle_test.go: TestGoFixedSharedByteOracleAndHostileInput"
            "test/conformance/go/rows/P3_test.go: TestRowP3"
            "internal/codegen/gotable/fuzzunit/fixedagree_test.go: TestGeneratedFixedAgreesWithEngine"
            "internal/codegen/gotable/fuzzunit/fixedagree_test.go: TestGeneratedFixedSlackNegativeControl"
            "internal/codegen/gotable/fuzzunit/fixedoneverdict_test.go: TestGeneratedFixedOneVerdictEveryByte")
 :tested-revision "5a1ca581ffa2e07761b02a5d311606d2166f854b"
 :note "All gates run with actual Go AddressSanitizer (CGO_ENABLED=1 CC=clang GOFLAGS='-p=2 -asan'), inherited by generated-module test subprocesses, SCHEMA_SLOW=1 and required fresh C++ corpus. All eight current reference vectors pass layout/hash/round-trip and hostile named-refusal/destination-preservation controls. The record poison sweep and every bit of the whole file pass under ASAN. The unrelaxed 6000-mutant generated/engine comparison per seed/target and every-value-of-every-record-byte sweep pass, including dead spans; accepted values reencode identically. The reversed witness disables the real generated dead-span validator and must expose an admitted mutant the engine refuses. No sanitizer diagnostic or held mutant; exhaustive differential completes in384.967s.")
