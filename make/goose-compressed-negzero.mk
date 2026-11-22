# THE COMPRESSED -0 WRITER CORPUS, its Goose leg (docs/SPEC-TABLES.md §3.3;
# docs/rulings.json fixed-compressed-negzero: a writer stores +0 for a
# compressed float's -0). The corpus is the Go reference engine's
# (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
# internal/tablewire TestCompressedNegZeroGoCorpus); this runs its fifteen
# rows through the generated Goose code under the pinned compiler, and beside
# each row every other Goose writer the root has (the variable save and its
# retaining twin, the fixed save, the message form, the typed and value text
# writers, the typed and region cook writers) holds a -0 to its +0 twin's
# bytes, with the plain float's -0 rows as the control
# (internal/codegen/goosetable/compressed_negzero_writers_test.go). Its fast
# twin, TestGooseCompressedFloatStoresAreGuarded, runs in the package's
# ordinary go test.
.PHONY: conformance-goose-compressed-negzero
conformance-goose-compressed-negzero:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	sh test/slowgate/proof conformance-goose-compressed-negzero 'TestGooseCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/goosetable/ -run '^TestGooseCompressedNegZeroCorpus$$'

test-goose: conformance-goose-compressed-negzero
