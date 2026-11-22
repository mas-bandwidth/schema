# make/luau-compressed-negzero.mk — the Luau leg's compressed -0 writer corpus
# (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-luau-compressed-negzero  every row of
#                                      testdata/conformance/feature-witnesses/compressed-negzero-go
#                                      run through the generated Luau unit: a
#                                      compressed float's -0 saves to its +0
#                                      twin's bytes in the variable and fixed
#                                      forms, and in every other Luau writer
#                                      (retain save, message form, JSON, cook);
#                                      the plain-float rows keep -0 bit-exact.
#                                      internal/codegen/luautable's
#                                      TestLuauCompressedFloatStoresAreGuarded is
#                                      its fast twin in the ordinary go test run.
.PHONY: conformance-luau-compressed-negzero
conformance-luau-compressed-negzero:
	@if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi
	LUAU="$(abspath $(LUAU))" SCHEMA_SLOW=1 $(GO) test -timeout=0 -count=1 ./internal/codegen/luautable -run '^TestLuauCompressedNegZeroCorpus$$'

test-luau: conformance-luau-compressed-negzero
