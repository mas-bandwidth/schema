(:id "port/luau/vt/cook-write" :state :done
 :evidence ("test/conformance/luau/driver: cook-write, 142/142 with no absent or relaxed rows"
            "make conformance-luau-cook-write-negative-control"
            "internal/codegen/luautable/cookwrite_strict_test.go: TestLuauCookWriteStrict and TestLuauCookWriteStrictNegativeControl"
            "internal/codegen/luautable/cook_corpus_test.go: TestLuauCookWritesCommittedCorpus"
            "internal/codegen/luautable/cook_inline_bytes_test.go: TestLuauCookWritesInlineByteArrays and TestLuauCookWritesInlineByteArraysNegativeControl"
            "internal/codegen/luautable/cook_order_control_test.go: TestLuauCookOrderWordNegativeControl")
 :tested-revision "89a9efe2ba98d15915660d5634ffced0d303619e"
 :note "2026-11-16: on Linux x86-64, nova-reserve, pinned Luau 0.741, SCHEMA_SLOW=1 and SCHEMA_REQUIRE_CORPUS=1: the current full cook-write manifest passed 142/142 in both byte orders with no absent or relaxed rows at main 652a95ec4. At 89a9efe2b the named strict, corpus, inline-byte and negative-control tests pass with the test's uint64 stamp written in canonical JSON string form. The dead-slot sabotage turned the manifest gate red (132/142).")
