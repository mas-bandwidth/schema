# make/squirrel-compressed-negzero.mk — the Squirrel leg's compressed -0 writer
# corpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-squirrel-compressed-negzero  every row of
#                                          testdata/conformance/feature-witnesses/compressed-negzero-go
#                                          run through the generated Squirrel
#                                          unit: a compressed float's -0 saves
#                                          to its +0 twin's bytes in the
#                                          variable and fixed forms, and in
#                                          every other Squirrel writer (retain
#                                          save, a fixed table's Save, message
#                                          form, JSON, cook); the plain-float
#                                          rows keep -0 bit-exact.
#                                          internal/codegen/squirreltable's
#                                          TestSquirrelCompressedFloatStoresAreGuarded
#                                          is its fast twin in the ordinary go
#                                          test run.
.PHONY: conformance-squirrel-compressed-negzero
conformance-squirrel-compressed-negzero:
	@if [ ! -x "$(SQ)" ]; then $(MAKE) dist-squirrel; fi
	SQ="$(abspath $(SQ))" SCHEMA_SLOW=1 $(GO) test -timeout=0 -count=1 ./internal/codegen/squirreltable -run '^TestSquirrelCompressedNegZeroCorpus$$'

test-squirrel: conformance-squirrel-compressed-negzero
