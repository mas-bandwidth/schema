# make/schema-cpp-runs-hostile-corpus.mk — the C++ PORT's leg of the shared
# hostile-input corpus (schema-shared-hostile-corpus, docs/HOSTILE-CORPUS.md).
#
# The corpus is committed at internal/ci/hostilecorpus/corpus: raw inputs plus
# one line-oriented manifest whose verdicts are the Go engine's. This target
# generates the port's readers for every unit the manifest names, builds the
# runner in test/tables/hostile_corpus_main.cpp, and holds every verdict to the
# manifest. The comparison and its planted-bad-pattern control live in
# internal/ci/schema_cpp_runs_hostile_corpus_test.go.
#
# THE GENERATED TREES ARE BUILD OUTPUT. Only the units the carriers reach are
# generated, and only the translation units that define their text readers are
# compiled: the corpus never touches the fixed form or the soak.

HOSTILE_CPP_DIR := build/cpp-hostile
HOSTILE_CPP_SERIALIZE := $(if $(wildcard $(SERIALIZE)/serialize.h),$(SERIALIZE),build/serialize)
HOSTILE_CPP_CORPUS := internal/ci/hostilecorpus/corpus
HOSTILE_CPP_INCLUDES := -Itest/tables -I$(HOSTILE_CPP_DIR)/pointers -I$(HOSTILE_CPP_DIR)/examples \
	-I$(HOSTILE_CPP_DIR)/messages -I$(HOSTILE_CPP_DIR)/blobs \
	-I$(HOSTILE_CPP_DIR)/scalars -I$(HOSTILE_CPP_DIR)/stream -I$(HOSTILE_CPP_DIR)/probe

# The probe schema is the one schema the corpus names that is not in the
# checkout: a variable root whose collection count can pass the int32 storage
# cap. It is written here, into the build tree, and never tracked.
define HOSTILE_CPP_PROBE_SCHEMA
package probe

table Root
{
    entries map[int32]int32
}
endef
export HOSTILE_CPP_PROBE_SCHEMA

$(HOSTILE_CPP_DIR)/.stamp: bin/schema $(HOSTILE_CPP_CORPUS)/manifest.tsv \
		test/tables/hostile_corpus_main.cpp make/schema-cpp-runs-hostile-corpus.mk $(HOSTILE_CPP_SERIALIZE)/serialize.h
	@rm -rf $(HOSTILE_CPP_DIR)
	@mkdir -p $(HOSTILE_CPP_DIR)/examples $(HOSTILE_CPP_DIR)/pointers \
		$(HOSTILE_CPP_DIR)/messages $(HOSTILE_CPP_DIR)/blobs \
		$(HOSTILE_CPP_DIR)/scalars $(HOSTILE_CPP_DIR)/stream $(HOSTILE_CPP_DIR)/probe
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/examples tables/examples
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/pointers tables/pointers
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/messages tables/messages
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/blobs tables/blobs
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/scalars tables/scalars
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/stream tables/stream
	@printf '%s\n' "$$HOSTILE_CPP_PROBE_SCHEMA" > $(HOSTILE_CPP_DIR)/probe.schema
	./bin/schema generate --lang cpp --out $(HOSTILE_CPP_DIR)/probe $(HOSTILE_CPP_DIR)/probe.schema
	@touch $@

build/cpp-hostile/runner: $(HOSTILE_CPP_DIR)/.stamp $(HOSTILE_CPP_SERIALIZE)/serialize.h
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(HOSTILE_CPP_INCLUDES) -I$(HOSTILE_CPP_SERIALIZE) \
		test/tables/hostile_corpus_main.cpp \
		$(HOSTILE_CPP_DIR)/examples/PackTable.cpp $(HOSTILE_CPP_DIR)/examples/TablesTable.cpp \
		$(HOSTILE_CPP_DIR)/pointers/GraphTable.cpp \
		$(HOSTILE_CPP_DIR)/messages/MessagesTable.cpp \
		$(HOSTILE_CPP_DIR)/blobs/AssetsTable.cpp \
		$(HOSTILE_CPP_DIR)/scalars/ScalarsTable.cpp \
		$(HOSTILE_CPP_DIR)/stream/StreamTable.cpp \
		$(HOSTILE_CPP_DIR)/probe/probeTable.cpp -o $@

# THE FAST GATE: every carrier in the committed corpus, in the port's unit tier.
.PHONY: schema-cpp-runs-hostile-corpus
schema-cpp-runs-hostile-corpus: build/cpp-hostile/runner
	SCHEMA_CPP_HOSTILE_RUNNER=$$PWD/build/cpp-hostile/runner \
	SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/ci -count=1 -timeout 600s \
		-run '^TestSchemaCppRunsHostileCorpus$$' -v
	@echo 'cpp hostile corpus: every verdict, named reason and decoded-value digest agrees'

# THE PLANTED-BAD-PATTERN CONTROL (nightly): HOSTILE_CPP_PLANT=1 makes the
# runner answer 'damaged' for wire-form-ff; the test requires the comparison to
# see it. Without the plant the same case must answer its manifest verdict.
.PHONY: schema-cpp-runs-hostile-corpus-negative-control
schema-cpp-runs-hostile-corpus-negative-control: build/cpp-hostile/runner
	SCHEMA_CPP_HOSTILE_RUNNER=$$PWD/build/cpp-hostile/runner \
	SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/ci -count=1 -timeout 600s \
		-run '^TestSchemaCppHostileCorpusNegativeControl$$' -v
	@echo 'cpp hostile corpus negative control: a planted reader bug is seen'

test: schema-cpp-runs-hostile-corpus
