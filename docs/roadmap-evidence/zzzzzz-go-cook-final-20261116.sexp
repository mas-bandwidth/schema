;; Whole unrelaxed Go cook surface and the generated runtime's reversed witness.
(:id "vt/cook-write/go" :state :done
 :evidence ("internal/codegen/gotable/cookwrite_test.go: TestCookWriterMatchesIndependentLayout"
            "internal/codegen/gotable/cookwrite_test.go: TestCookWriterMapExtentLayout"
            "internal/codegen/gotable/cookwrite_test.go: TestGoCookDeadSlotNegativeControl"
            "internal/codegen/gotable/cookwrite_test.go: TestGoCookDeadContainerMatchesTool"
            "internal/codegen/gotable/cookguardstorage_test.go: TestGoCookGuardedStorageMatchesTool"
            "internal/codegen/gotable/cookdeadslot_test.go: TestCookIgnoresDeadSlotPointers"
            "test/conformance/go/main.go: the cook-write surface")
 :tested-revision "93a3d59d27533bd8f8cd93f1cdb09f9a0b338692"
 :note "All 142 cook-write rows pass with no absence or relaxation, including the ten formerly relaxed dead-slot rows, in both byte orders. The complete Go leg passes. Independent tool-layout tests cover fixed and variable roots, maps, sharing, short-buffer preservation and fixed zero-allocation writes. Twelve direct-built tool cases distinguish false-guard inline storage and live-reference refusals, absent optional inline data from owned extents, and wholly ignored counted tails. NaN/reference/list/text-list/nested/pointer-list refusals and optional NaN/dead-tail acceptance controls pass. The reversed witness restores the capacity loop bound in the real generated Cook artifact and must fail the independent dead-slot oracle by name. Full regenerated fuzzunit, names, vet, generated-current and roadmap/evidence validation pass.")
