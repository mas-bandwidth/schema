# make/lua-compressed-negzero.mk — the Lua leg's compressed -0 writer corpus
# (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-lua-compressed-negzero  every row of
#                                     testdata/conformance/feature-witnesses/compressed-negzero-go
#                                     run through the generated Lua unit: a
#                                     compressed float's -0 saves to its +0
#                                     twin's bytes in the variable and fixed
#                                     forms, and in every other Lua writer
#                                     (typed save, retain save, message form,
#                                     JSON, cook); the plain-float rows keep -0
#                                     bit-exact. internal/codegen/luatable's
#                                     TestLuaCompressedFloatStoresAreGuarded is
#                                     its fast twin in the ordinary go test run.
#                                     The run goes through test/slowgate/proof,
#                                     so a skipped gate is a failure.
.PHONY: conformance-lua-compressed-negzero
conformance-lua-compressed-negzero:
	@test -x "$(LUA)" || $(MAKE) dist-lua
	LUA="$(abspath $(LUA))" sh test/slowgate/proof conformance-lua-compressed-negzero 'TestLuaCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/luatable/ -run '^TestLuaCompressedNegZeroCorpus$$'

test-lua: conformance-lua-compressed-negzero
