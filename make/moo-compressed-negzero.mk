# make/moo-compressed-negzero.mk — the moo leg's compressed -0 writer corpus
# (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-moo-compressed-negzero  every row of
#                                     testdata/conformance/feature-witnesses/compressed-negzero-go
#                                     run through the generated moo unit: a
#                                     compressed float's -0 saves to its +0
#                                     twin's bytes in the variable and fixed
#                                     forms, and in every other moo writer
#                                     (retain save, message form, JSON, cook);
#                                     the plain-float rows keep -0 bit-exact.
#                                     internal/codegen/mootable's
#                                     TestMooCompressedFloatStoresAreGuarded is
#                                     its fast twin in the ordinary go test run.
.PHONY: conformance-moo-compressed-negzero
conformance-moo-compressed-negzero:
	@if [ ! -x "$(MOO)" ]; then $(MAKE) dist-moo; fi
	MOO="$(abspath $(MOO))" SCHEMA_SLOW=1 $(GO) test -timeout=0 -count=1 ./internal/codegen/mootable -run '^TestMooCompressedNegZeroCorpus$$'

test-moo: conformance-moo-compressed-negzero
