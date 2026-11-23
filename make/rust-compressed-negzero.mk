# THE COMPRESSED -0 WRITER CORPUS, its Rust leg (docs/SPEC-TABLES.md §3.3;
# docs/rulings.json fixed-compressed-negzero: a writer stores +0 for a
# compressed float's -0). The corpus is the Go reference engine's
# (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
# internal/tablewire TestCompressedNegZeroGoCorpus); this runs its fifteen
# rows through the generated Rust crate, built with -O so no debug assert is
# on, and beside each row every other Rust writer the root has (the variable
# save and its retaining twin, the fixed save, the message form, the JSON
# writer, the cook) holds a -0 to its +0 twin's bytes, with the plain float's
# -0 rows as the control
# (internal/codegen/rusttable/compressed_negzero_corpus_test.go).
# SCHEMA_REQUIRE_CORPUS=1 makes a missing rustc a failure, never a skip. Its
# fast twin, TestRustCompressedFloatStoresAreGuarded, runs in the package's
# ordinary go test. The recipe uses $(RUSTUP_BIN) from make/rust.mk.
.PHONY: conformance-rust-compressed-negzero
conformance-rust-compressed-negzero:
	PATH="$$PATH:$(RUSTUP_BIN)" SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof conformance-rust-compressed-negzero 'TestRustCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/rusttable/ -run '^TestRustCompressedNegZeroCorpus$$'

test-rust: conformance-rust-compressed-negzero
