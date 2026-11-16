(:id "port/lua/vt/text-form" :state :done
 :evidence ("test/conformance/lua/driver: json-read 108/108, json-write 110/110, json-hostile 155/155 without absent or relaxed rows"
            "internal/codegen/luatable/json_strict_corpus_test.go: TestLuaFromJsonStrictAgreesWithTheCorpus and TestLuaFromJsonStrictAgreesWithTheCorpusNegativeControl"
            "internal/codegen/luatable/json_test.go: TestLuaJsonAgreesWithTheCorpus and TestLuaJsonNegativeControl"
            "internal/codegen/luatable/jsonstrict_test.go: TestLuaFromJsonIsStrict"
            "internal/codegen/luatable/textdifferential_test.go: TestLuaTextDifferential and TestLuaTextDifferentialNegativeControl")
 :tested-revision "be75329ea4bb93d8c0e7eceb1e1a2daacd87e51e"
 :note "2026-11-16: Linux x86-64, nova-reserve, pinned Lua 5.4.8, SCHEMA_SLOW=1 and SCHEMA_REQUIRE_CORPUS=1: full unrelaxed Lua conformance passed, including every current JSON read, write and hostile row. The manifest-driven strict corpus covers all 108 text-bearing instances and 155 hostile rows; the generated Go differential checks 239 non-wide rows, while the strict corpus and harness cover wide units. Named negative controls catch their specific refusal, unknown-object duplicate, field lookup, text walk and float spelling faults. The stale zero-escape relaxation was removed.")
