(:id "port/squirrel/vt/cook-write" :state :done
 :evidence ("test/conformance/squirrel/driver: cook-write, 142/142 without absent or relaxed rows"
            "make conformance-squirrel-cook-write-negative-control"
            "internal/codegen/squirreltable: TestSquirrelVtCookCells, TestSquirrelVtCookCellsNegativeControl and TestSquirrelCookWriterMatchesTheCorpus")
 :tested-revision "652a95ec4884ab976e2e439a017b1abeb7c84fe2"
 :note "2026-11-16: Linux x86-64, nova-reserve, SCHEMA_SLOW=1 and SCHEMA_REQUIRE_CORPUS=1: the current manifest cook-write surface passed all 142 cases in both byte orders without absent or relaxed rows. The named native tests passed, and the planted dead-slot or absent-optional fault turned the cook-write gate red.")
