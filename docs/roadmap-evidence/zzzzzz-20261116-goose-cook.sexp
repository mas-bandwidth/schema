(:id "pitstop-2026-10-20/variable-tables/cook-write/goose" :state :done
 :evidence ("test/conformance/goose/accel.goose: cook-write, 142/142 with no absent or relaxed cook rows"
            "make tables-goose-accel-negative-control: cook-order and cook-numbering plants turn cook-write red"
            "internal/codegen/goosetable/cook_write_test.go: TestGooseCookWriterMatchesIndependentLayout, TestGooseCookRegionWriterHonoursDeclaredForms, TestGooseCookRegionWriterBoundsCallerIndices and TestGooseCookRegionWriterNegativeControl"
            "internal/codegen/goosetable/vtcells_strict_test.go: TestGooseVariableCellsStrict and TestGooseVariableCellsStrictNegativeControl")
 :tested-revision "652a95ec4884ab976e2e439a017b1abeb7c84fe2"
 :note "2026-11-16: Linux x86-64, nova-reserve, pinned Goose v2026.2, SCHEMA_SLOW=1 and SCHEMA_REQUIRE_CORPUS=1: the current full cook-write manifest passed 142/142 without absent or relaxed cook rows; the named native controls and accelerator sabotage passed. The whole Goose harness separately failed the framing frame_unreferenced_entry row; this cook proof does not assert that unrelated surface passes.")
