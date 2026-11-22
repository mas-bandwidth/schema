# make/js-compressed-negzero.mk — the JavaScript leg's compressed -0 writer
# corpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-js-compressed-negzero  every row of
#                                    testdata/conformance/feature-witnesses/compressed-negzero-go
#                                    run through the generated JavaScript unit
#                                    under the leg's node: a compressed float's
#                                    -0 saves to its +0 twin's bytes in the
#                                    variable and fixed forms, and in every
#                                    other JavaScript writer (retain save,
#                                    message form, JSON, cook); the plain-float
#                                    rows keep -0 bit-exact.
#                                    internal/codegen/jstable's
#                                    TestJsCompressedFloatStoresAreGuarded is
#                                    its fast twin in the ordinary go test run.
.PHONY: conformance-js-compressed-negzero
conformance-js-compressed-negzero:
	NODE=$(NODE) SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof conformance-js-compressed-negzero 'TestJsCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/jstable/ -run '^TestJsCompressedNegZeroCorpus$$'

test-js: conformance-js-compressed-negzero
