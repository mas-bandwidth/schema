;; Lua cook-write: current unrelaxed whole-surface and planted controls.
(:id "port/lua/vt/cook-write" :state :done
 :evidence ("test/conformance/lua/driver: cook-write, 142/142 with no absent or relaxed rows"
            "internal/codegen/luatable/cookwrite_test.go: TestLuaCookWrite, TestLuaCookWriteNegativeControlsFunctional"
            "internal/codegen/luatable/cookdeadslots_test.go: TestLuaCookWriteDeadSlotsAreZero"
            "internal/codegen/luatable/cookabsentoptional_test.go: TestLuaCookWriteAbsentOptionalIsZero")
 :tested-revision "652a95ec4884ab976e2e439a017b1abeb7c84fe2"
 :note "2026-11-16: on Linux x86-64 under nova-reserve with pinned Lua 5.4.8, SCHEMA_SLOW=1 and SCHEMA_REQUIRE_CORPUS=1, the full current manifest cook-write surface passed 142/142 with no absent or relaxed cook rows; writer, dead-slot and absent-optional tests and planted writer negative controls passed. The overall language harness reported a separate stale JSON-hostile relaxation; that is not a cook failure.")
