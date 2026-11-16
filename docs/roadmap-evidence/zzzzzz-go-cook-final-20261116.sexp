;; Whole unrelaxed Go cook surface and the generated runtime's reversed witness.
(:id "vt/cook-write/go" :state :done
 :evidence ("internal/codegen/gotable/cookwrite_test.go: TestCookWriterMatchesIndependentLayout"
            "internal/codegen/gotable/cookwrite_test.go: TestCookWriterMapExtentLayout"
            "internal/codegen/gotable/cookwrite_test.go: TestGoCookDeadSlotNegativeControl"
            "internal/codegen/gotable/cookwrite_test.go: TestGoCookDeadContainerMatchesTool"
            "internal/codegen/gotable/cookdeadslot_test.go: TestCookIgnoresDeadSlotPointers"
            "test/conformance/go/main.go: the cook-write surface")
 :tested-revision "5412c20f3e1f0dc2289cc72eb5dfc916c7e90e0a"
 :note "All 142 cook-write rows pass with no absence or relaxation, including the ten formerly relaxed dead-slot rows, in both byte orders. The complete Go leg passes. Independent tool-layout tests cover fixed and variable roots, map extents, sharing, short-buffer preservation and fixed zero-allocation writes. Dead counted pointers and containers are ignored and match the tool; live controls and the separate Lock refusal remain exercised. The reversed witness removes both dead-storage guards from the real generated Cook artifact and must fail the independent byte oracle by name.")
