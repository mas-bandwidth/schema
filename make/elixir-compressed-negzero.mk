# THE COMPRESSED -0 WRITER CORPUS ON THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3,
# docs/rulings.json fixed-compressed-negzero): a compressed float's -0 is
# written as +0 by every Elixir writer, and a plain float's -0 rides bit-exact.
# testdata/conformance/feature-witnesses/compressed-negzero-go/cases.tsv names
# the rows; TestElixirCompressedNegZeroCorpus generates its Probe.schema for
# Elixir, loads each row's base, sets -0 in the native value, and holds the
# variable and fixed saves to the corpus's bytes and the text, cook, retaining
# and message writers to the +0 twin. SCHEMA_REQUIRE_CORPUS=1 makes a missing
# toolchain a failure here, never a skip. Its fast twin,
# TestElixirCompressedFloatStoresAreGuarded, runs in every go test.
GO ?= go

.PHONY: conformance-elixir-compressed-negzero
conformance-elixir-compressed-negzero:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GO) test -count=1 -timeout=0 ./internal/codegen/elixirtable -run '^TestElixirCompressedNegZeroCorpus$$'

test-elixir: conformance-elixir-compressed-negzero
