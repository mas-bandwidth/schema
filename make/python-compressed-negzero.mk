# make/python-compressed-negzero.mk — the Python leg's compressed -0 writer
# corpus (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-python-compressed-negzero  every row of
#                                        testdata/conformance/feature-witnesses/compressed-negzero-go
#                                        run through the generated Python
#                                        package under both pinned interpreters:
#                                        a compressed float's -0 saves to its +0
#                                        twin's bytes in the variable and fixed
#                                        forms, and in every other Python writer
#                                        (save(), retain save, message form,
#                                        JSON, cook); the plain-float rows keep
#                                        -0 bit-exact.
#                                        internal/codegen/pythontable's
#                                        TestPythonCompressedFloatStoresAreGuarded
#                                        is its fast twin in the ordinary go
#                                        test run. The run goes through
#                                        test/slowgate/proof, so a skipped gate
#                                        is a failure.
.PHONY: conformance-python-compressed-negzero
PYTHON_CI_SUPPORT_GROUPS += conformance-python-compressed-negzero

conformance-python-compressed-negzero: python-toolchain-check
	sh test/slowgate/proof conformance-python-compressed-negzero 'TestPythonCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/pythontable/ -run '^TestPythonCompressedNegZeroCorpus$$'

test-python: conformance-python-compressed-negzero
