# THE COMPRESSED -0 WRITER CORPUS, its Java leg (docs/SPEC-TABLES.md §3.3;
# docs/rulings.json fixed-compressed-negzero: a writer stores +0 for a
# compressed float's -0). The corpus is the Go reference engine's
# (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
# internal/tablewire TestCompressedNegZeroGoCorpus); this runs its fifteen
# rows through the generated Java, once without -ea and once with it, and
# beside each row every other Java writer the root has (the variable save and
# its retaining twin, the fixed save, the message form, the JSON writer, the
# cook) holds a -0 to its +0 twin's bytes, with the plain float's -0 rows as
# the control (internal/codegen/javatable/compressed_negzero_corpus_test.go).
# SCHEMA_REQUIRE_CORPUS=1 makes a missing JDK a failure, never a skip. Its
# fast twin, TestJavaCompressedFloatStoresAreGuarded, runs in the package's
# ordinary go test. The recipe uses $(JAVAC) and $(JAVA) from make/java.mk.
.PHONY: conformance-java-compressed-negzero
conformance-java-compressed-negzero:
	JAVAC="$(JAVAC)" JAVA="$(JAVA)" SCHEMA_REQUIRE_CORPUS=1 sh test/slowgate/proof conformance-java-compressed-negzero 'TestJavaCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/javatable/ -run '^TestJavaCompressedNegZeroCorpus$$'

test-java: conformance-java-compressed-negzero
