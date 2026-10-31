# make/schema-squirrel-nightly-fuzz.mk — the SQUIRREL leg's NIGHTLY FUZZ target
# (docs/PORT-SQUIRREL.md, THE CARD MAP; docs/SPEC-TABLES.md §4.2). The
# Makefile's wildcard include pulls this in.
#
# WHAT IT DRIVES. The port already owns one seeded mutation fuzzer, the wire
# differential in internal/codegen/squirreltable/fuzz_test.go: it mutates every
# instance wire of the tables conformance corpus and reads every mutant through
# the Squirrel leg's generated load (test/conformance/squirrel/wirefuzz.nut)
# and through the compiler's independent engine (internal/tablewire), and the
# two must agree on completion, the six counters, the refusal verdict and the
# re-saved bytes. This target gives that fuzzer a nightly budget, a corpus file
# and a registered negative control; it is the SAME fuzzer the pull request
# runs, not a second one.
#
# THE CORPUS is the schema set the Makefile's generate targets read: the
# SCHEMAS_TABLES* and SCHEMAS_WIDE wildcards below are the very variables
# tables_generate and the C++/wide targets consume (Makefile), so no corpus is
# invented here. They are written to a file the harness reads, one path a line.
SQUIRREL_FUZZ_CORPUS_FILE := build/squirrel-nightly-fuzz/corpus.txt

SQUIRREL_FUZZ_SCHEMAS := \
	$(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) \
	$(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_SCALARS) \
	$(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_ARMS) \
	$(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9) \
	$(SCHEMAS_WIDE)

# The budget, in seconds, inside the card's ten-minute nightly budget per port.
# It is go test's -timeout: the harness reads no clock (the search is a pure
# function of the seed and the mutant count), and a night that cannot finish in
# the budget is killed by go test, which names the test it was running.
SQUIRREL_FUZZ_SECONDS ?= 540
SQUIRREL_FUZZ_MUTANTS ?= 200
SQUIRREL_FUZZ_SEED     ?= 0x5351554952524c46

# THE TOOL GATE. sq is the pinned interpreter (make/squirrel.mk). A target
# that cannot find it says so by name and by machine; it never skips green.
.PHONY: schema-squirrel-nightly-fuzz-toolchain
schema-squirrel-nightly-fuzz-toolchain:
	@if [ ! -x "$(SQ)" ]; then \
		echo "tool missing: sq"; \
		echo "  SQ = $(SQ)"; \
		echo "  machines lacking it: $$(uname -n)"; \
		echo "  install it from the official source with 'make dist-squirrel' (pinned $(SQUIRREL_VERSION), sha256 $(SQUIRREL_SHA256))"; \
		exit 1; \
	fi
	@echo "schema-squirrel-nightly-fuzz toolchain: sq $(SQUIRREL_VERSION) at $(SQ)"

$(SQUIRREL_FUZZ_CORPUS_FILE): Makefile make/schema-squirrel-nightly-fuzz.mk
	@mkdir -p $(dir $@)
	@: > $@
	@for s in $(SQUIRREL_FUZZ_SCHEMAS); do printf '%s/%s\n' "$(CURDIR)" "$$s"; done >> $@
	@test -s $@ || { echo "schema-squirrel-nightly-fuzz: the corpus is empty"; exit 1; }
	@echo "schema-squirrel-nightly-fuzz corpus: $$(wc -l < $@) schemas -> $@"

# THE NIGHTLY CAMPAIGN. The mutation count and the seed are arguments, so a
# finding names its full recipe; the budget is the card's time variable, passed
# as go test's -timeout. All
# three reader kinds run: the tolerant load (the wire differential above), the
# strict read, and the text/JSON parse (nightly_fuzz_readers_test.go). They
# are parallel, so the budget is the whole campaign's, not per kind.
.PHONY: schema-squirrel-nightly-fuzz
schema-squirrel-nightly-fuzz: schema-squirrel-nightly-fuzz-toolchain $(SQUIRREL_FUZZ_CORPUS_FILE)
	SCHEMA_REQUIRE_CORPUS=1 SCHEMA_SLOW=1 \
	SCHEMA_SQUIRREL_FUZZ_CORPUS=$$PWD/$(SQUIRREL_FUZZ_CORPUS_FILE) \
	SCHEMA_SQUIRREL_FUZZ_MUTANTS=$(SQUIRREL_FUZZ_MUTANTS) \
	SCHEMA_SQUIRREL_FUZZ_SEED=$(SQUIRREL_FUZZ_SEED) \
	nice -n 19 go test -count=1 -timeout $(SQUIRREL_FUZZ_SECONDS)s ./internal/codegen/squirreltable \
		-run '^TestSquirrelNightlyFuzz($$|Strict$$|TextJSON$$)' -v

# THE NEGATIVE CONTROL (STEP 5): the existing red is reused rather than
# duplicated — TestSquirrelWireFuzzNegativeControl patches the emitted load's
# array-frame bound in memory, regenerates every unit under the patch, and
# requires the differential to go RED on a real disagreement between the two
# sides. Registered in make/negative-controls.json's nightly group.
.PHONY: schema-squirrel-nightly-fuzz-negative-control
schema-squirrel-nightly-fuzz-negative-control: schema-squirrel-nightly-fuzz-toolchain
	SCHEMA_REQUIRE_CORPUS=1 SCHEMA_SLOW=1 \
	nice -n 19 go test -count=1 -timeout 600s ./internal/codegen/squirreltable \
		-run '^TestSquirrelWireFuzzNegativeControl$$' -v
