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

HOSTILE_CPP_UNITS = examples/PackTable.cpp examples/TablesTable.cpp \
	pointers/GraphTable.cpp messages/MessagesTable.cpp blobs/AssetsTable.cpp \
	scalars/ScalarsTable.cpp stream/StreamTable.cpp probe/probeTable.cpp

build/cpp-hostile/runner: $(HOSTILE_CPP_DIR)/.stamp $(HOSTILE_CPP_SERIALIZE)/serialize.h
	$(CXX) $(TABLES_CXXFLAGS) $(HOSTILE_CPP_INCLUDES) -I$(HOSTILE_CPP_SERIALIZE) \
		test/tables/hostile_corpus_main.cpp \
		$(addprefix $(HOSTILE_CPP_DIR)/,$(HOSTILE_CPP_UNITS)) -o $@

# THE PLANTED READER BUG (scratch only): a copy of the generated tree in which
# the generated wire reader (examples/PackTable.h) names previous_form for a
# form byte it does not carry, where the generated code names newer_form. The
# checked-in generator and the clean tree above are
# untouched; the grep fails the build if the plant no longer applies, so a
# generator change cannot turn the control vacuous.
HOSTILE_CPP_PLANT_DIR := build/cpp-hostile-plant
HOSTILE_CPP_PLANT_FROM := ? message_form_as_file : newer_form;
HOSTILE_CPP_PLANT_TO := ? message_form_as_file : previous_form;

build/cpp-hostile-plant/runner: $(HOSTILE_CPP_DIR)/.stamp $(HOSTILE_CPP_SERIALIZE)/serialize.h
	@rm -rf $(HOSTILE_CPP_PLANT_DIR)
	@mkdir -p $(HOSTILE_CPP_PLANT_DIR)
	cp -R $(HOSTILE_CPP_DIR)/examples $(HOSTILE_CPP_DIR)/pointers $(HOSTILE_CPP_DIR)/messages \
		$(HOSTILE_CPP_DIR)/blobs $(HOSTILE_CPP_DIR)/scalars $(HOSTILE_CPP_DIR)/stream \
		$(HOSTILE_CPP_DIR)/probe $(HOSTILE_CPP_PLANT_DIR)/
	sed -i.orig 's/$(HOSTILE_CPP_PLANT_FROM)/$(HOSTILE_CPP_PLANT_TO)/g' $(HOSTILE_CPP_PLANT_DIR)/examples/PackTable.h
	grep -q -F '$(HOSTILE_CPP_PLANT_TO)' $(HOSTILE_CPP_PLANT_DIR)/examples/PackTable.h
	$(CXX) $(TABLES_CXXFLAGS) -Itest/tables $(addprefix -I$(HOSTILE_CPP_PLANT_DIR)/,examples pointers messages blobs scalars stream probe) \
		-I$(HOSTILE_CPP_SERIALIZE) test/tables/hostile_corpus_main.cpp \
		$(addprefix $(HOSTILE_CPP_PLANT_DIR)/,$(HOSTILE_CPP_UNITS)) -o $@

# THE FAST GATE: every carrier in the committed corpus, in the port's unit tier.
.PHONY: schema-cpp-runs-hostile-corpus
schema-cpp-runs-hostile-corpus: build/cpp-hostile/runner
	SCHEMA_CPP_HOSTILE_RUNNER=$$PWD/build/cpp-hostile/runner \
	SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/ci -count=1 -timeout 600s \
		-run '^(TestParseCppManifest|TestSchemaCppRunsHostileCorpus)$$' -v
	@echo 'cpp hostile corpus: every verdict, named reason and decoded-value digest agrees'

# THE PLANTED-BAD-PATTERN CONTROL (nightly): the runner built from the planted
# scratch tree must disagree on wire-form-ff; the clean runner must not.
.PHONY: schema-cpp-runs-hostile-corpus-negative-control
schema-cpp-runs-hostile-corpus-negative-control: build/cpp-hostile/runner build/cpp-hostile-plant/runner
	SCHEMA_CPP_HOSTILE_RUNNER=$$PWD/build/cpp-hostile/runner \
	SCHEMA_CPP_HOSTILE_RUNNER_PLANTED=$$PWD/build/cpp-hostile-plant/runner \
	SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/ci -count=1 -timeout 600s \
		-run '^TestSchemaCppHostileCorpusNegativeControl$$' -v
	@echo 'cpp hostile corpus negative control: a planted reader bug is seen'

test: schema-cpp-runs-hostile-corpus
