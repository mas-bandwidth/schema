# NIGHTLY FUZZING ON THE ELIXIR PORT (Glenn, 2026-10-18).
#
# Fuzz the generated readers with the port's own toolchain. The corpus is the
# schema corpus the Makefile's elixir generate target already reads
# (ELIXIR_TABLE_UNITS, make/elixir.mk), not a second list: every table of every
# unit in that set is enumerated from the generated module's own descriptors and
# given a VALID encoding through the public API before mutation.
#
# The readers are the three the card names: load (the id-table wire), strict
# (load, save back, load the saved bytes again) and the text/JSON parse. The
# oracle is a clean {:error, atom, report} tuple on malformed input; a raise, an
# exit, a throw or a silent success that cannot round-trip is a finding. A hang
# is the timeout wrapper's.
#
# The nightly budget is SCHEMA_ELIXIR_FUZZ_TIME (one port, default 10m). The
# fast parts (the planted-pattern test and the saved-input replay) run on every
# push; only the timed pass is scheduled.

SCHEMA_ELIXIR_FUZZ_ELIXIR_VERSION := 1.20.4
# The outer timeout is the budget the caller chose (10m by default, and
# workflow_dispatch passes its own). The harness deadline is DERIVED from it
# with a bounded shutdown margin: a fixed loop ignored a shorter `time=1m`
# dispatch and died on its own timeout with exit 124. The harness counts
# startup (including :cover instrumentation) inside the same deadline.
SCHEMA_ELIXIR_FUZZ_TIME            ?= 10m
SCHEMA_ELIXIR_FUZZ_SLACK_SECONDS   ?= 5
SCHEMA_ELIXIR_FUZZ_BUDGET          := internal/ci/testdata/schema-elixir-nightly-fuzz/budget.sh
SCHEMA_ELIXIR_FUZZ_SECONDS         := $(shell sh $(SCHEMA_ELIXIR_FUZZ_BUDGET) $(SCHEMA_ELIXIR_FUZZ_TIME) $(SCHEMA_ELIXIR_FUZZ_SLACK_SECONDS))
SCHEMA_ELIXIR_FUZZ_SEED            ?= 20261018
SCHEMA_ELIXIR_FUZZ_DIR             := build/schema-elixir-nightly-fuzz
SCHEMA_ELIXIR_FUZZ_CORPUS          := $(SCHEMA_ELIXIR_FUZZ_DIR)/corpus.txt
SCHEMA_ELIXIR_FUZZ_HARNESS         := $(SCHEMA_ELIXIR_FUZZ_DIR)/harness.exs
SCHEMA_ELIXIR_FUZZ_SAVED           := internal/ci/testdata/schema-elixir-nightly-fuzz/saved
SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE  := internal/ci/testdata/schema-elixir-nightly-fuzz/harness.exs
# The mutation library is StreamData, vendored at a pinned version so the
# nightly run needs no network and no Hex/mix project.
SCHEMA_ELIXIR_FUZZ_STREAM_DATA_VERSION := 1.4.0
SCHEMA_ELIXIR_FUZZ_STREAM_DATA_DIR     := internal/ci/testdata/schema-elixir-nightly-fuzz/vendor/stream_data
SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN    := $(SCHEMA_ELIXIR_FUZZ_DIR)/stream_data-ebin
SCHEMA_ELIXIR_FUZZ_STREAM_DATA_SOURCES := $(wildcard $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_DIR)/lib/*.ex $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_DIR)/lib/stream_data/*.ex)
# The Go fuzz corpus and the conformance vectors seed the mutator, as the card
# asks. They are raw bytes, not this wire's, so they are extra SPLICE sources
# and a small share of the mutant population, never a valid encoding.
SCHEMA_ELIXIR_FUZZ_SEED_DIRS       := internal/fuzz/testdata testdata/conformance/tables

ELIXIR_FUZZ_EBIN := build/elixir-tables-ebin

$(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN)/.stamp: $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_SOURCES)
	@rm -rf $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN)
	@mkdir -p $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN)
	$(ELIXIRC) -o $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN) $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_SOURCES)
	@touch $@

.PHONY: schema-elixir-nightly-fuzz-corpus
schema-elixir-nightly-fuzz-corpus: build/tables-generated-elixir/.stamp
	@mkdir -p $(SCHEMA_ELIXIR_FUZZ_DIR)
	@: > $(SCHEMA_ELIXIR_FUZZ_CORPUS).tmp
	@for unit in $(ELIXIR_TABLE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		[ -e "$$path" ] || { echo "schema-elixir-nightly-fuzz: corpus path $$path is missing"; exit 1; }; \
		mod=$$(sed -n 's/^defmodule \([^.]*\)\.Table do.*/\1/p' build/tables-generated-elixir/$$name/Table.ex | head -1); \
		[ -n "$$mod" ] || { echo "schema-elixir-nightly-fuzz: $$name has no generated Table module"; exit 1; }; \
		printf 'unit %s %s %s\n' "$$name" "$$path" "$$mod" >> $(SCHEMA_ELIXIR_FUZZ_CORPUS).tmp; \
	done
	@mv $(SCHEMA_ELIXIR_FUZZ_CORPUS).tmp $(SCHEMA_ELIXIR_FUZZ_CORPUS)
	@grep -q '^unit tabledemo ' $(SCHEMA_ELIXIR_FUZZ_CORPUS) || \
		{ echo "schema-elixir-nightly-fuzz: the corpus lost its first unit"; exit 1; }
	@n=$$(grep -c '^unit ' $(SCHEMA_ELIXIR_FUZZ_CORPUS)); \
		[ "$$n" -ge 14 ] || { echo "schema-elixir-nightly-fuzz: only $$n units, the corpus glob broke"; exit 1; }
	@echo "schema-elixir-nightly-fuzz corpus: $$(grep -c '^unit ' $(SCHEMA_ELIXIR_FUZZ_CORPUS)) units from ELIXIR_TABLE_UNITS"

.PHONY: schema-elixir-nightly-fuzz-prepare
schema-elixir-nightly-fuzz-prepare: bin/schema $(ELIXIR_FUZZ_EBIN)/.stamp $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN)/.stamp schema-elixir-nightly-fuzz-corpus
	@mkdir -p $(SCHEMA_ELIXIR_FUZZ_DIR)
	@cp $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) $(SCHEMA_ELIXIR_FUZZ_HARNESS)

# THE TIMED PASS. It probes the pinned toolchain by name, never fake a pass: a
# missing Elixir is `tool missing: elixir` and not-done, and a version other
# than the pin is named the same way.
.PHONY: schema-elixir-nightly-fuzz
schema-elixir-nightly-fuzz: schema-elixir-nightly-fuzz-prepare
	@command -v elixir >/dev/null 2>&1 || { echo 'tool missing: elixir (schema-elixir-nightly-fuzz needs $(SCHEMA_ELIXIR_FUZZ_ELIXIR_VERSION))'; exit 1; }
	@v=$$(elixir --version 2>/dev/null | sed -n 's/^Elixir \([0-9][0-9.]*\).*/\1/p' | head -1); \
		[ "$$v" = "$(SCHEMA_ELIXIR_FUZZ_ELIXIR_VERSION)" ] || \
		{ echo "tool missing: elixir $(SCHEMA_ELIXIR_FUZZ_ELIXIR_VERSION) (found $${v:-unknown})"; exit 1; }
	SCHEMA_ELIXIR_FUZZ_SEED=$(SCHEMA_ELIXIR_FUZZ_SEED) \
		timeout $(SCHEMA_ELIXIR_FUZZ_TIME) elixir -pa $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN) -pa $(ELIXIR_FUZZ_EBIN) \
		$(SCHEMA_ELIXIR_FUZZ_HARNESS) $(SCHEMA_ELIXIR_FUZZ_CORPUS) \
		$(SCHEMA_ELIXIR_FUZZ_SECONDS) $(SCHEMA_ELIXIR_FUZZ_SAVED) \
		$(SCHEMA_ELIXIR_FUZZ_SEED_DIRS)
	@echo "schema-elixir-nightly-fuzz: $(SCHEMA_ELIXIR_FUZZ_TIME) over $(SCHEMA_ELIXIR_FUZZ_SECONDS)s, the nine reader entry points refused cleanly or round-tripped"

# THE FAST ARM: replay every saved input on every push. No clock, no mutation,
# no network — just the inputs a past run saved, through the same readers.
.PHONY: schema-elixir-nightly-fuzz-replay
schema-elixir-nightly-fuzz-replay: bin/schema $(ELIXIR_FUZZ_EBIN)/.stamp $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN)/.stamp schema-elixir-nightly-fuzz-corpus
	@[ -d $(SCHEMA_ELIXIR_FUZZ_SAVED) ] || \
		{ echo "schema-elixir-nightly-fuzz replay: saved-input directory $(SCHEMA_ELIXIR_FUZZ_SAVED) is missing"; exit 1; }
	@files=$$(ls -A $(SCHEMA_ELIXIR_FUZZ_SAVED) 2>/dev/null | grep -v -F -x '.gitkeep'); \
	if [ -z "$$files" ]; then \
		echo "schema-elixir-nightly-fuzz replay: no saved inputs (0)"; \
	else \
		$(ELIXIR) -pa $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN) -pa $(ELIXIR_FUZZ_EBIN) $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) \
			--replay $(SCHEMA_ELIXIR_FUZZ_CORPUS) $(SCHEMA_ELIXIR_FUZZ_SAVED); \
	fi

# THE PLANTED-READER CONTROL. This is not a grep over the harness: it copies a
# real generated reader (tabledemo), drops open/1's id-count bounds check in the
# scratch copy only, compiles that copy, and runs the fuzzer against it. The
# fuzzer must find the raise, save a minimised input, and the replay arm against
# the same planted reader must go RED on that input. The unplanted reader must
# then replay the same input GREEN: the plant, not the input, is the bug. A
# scan nobody has watched fail is a green light with no bulb.
SCHEMA_ELIXIR_FUZZ_PLANT := build/schema-elixir-nightly-fuzz/planted
SCHEMA_ELIXIR_FUZZ_PLANT_UNIT ?= tabledemo
SCHEMA_ELIXIR_FUZZ_PLANT_SECONDS ?= 45
SCHEMA_ELIXIR_FUZZ_PLANT_SED := -e 's/if count > div(size - 9, 8) do/if false do/'

.PHONY: schema-elixir-nightly-fuzz-negative-control
schema-elixir-nightly-fuzz-negative-control: schema-elixir-nightly-fuzz-prepare
	@rm -rf $(SCHEMA_ELIXIR_FUZZ_PLANT) && mkdir -p $(SCHEMA_ELIXIR_FUZZ_PLANT)/ebin $(SCHEMA_ELIXIR_FUZZ_PLANT)/saved
	@cp -r build/tables-generated-elixir/$(SCHEMA_ELIXIR_FUZZ_PLANT_UNIT) $(SCHEMA_ELIXIR_FUZZ_PLANT)/src
	@sed -i $(SCHEMA_ELIXIR_FUZZ_PLANT_SED) $(SCHEMA_ELIXIR_FUZZ_PLANT)/src/Table.ex
	@grep -q 'if false do' $(SCHEMA_ELIXIR_FUZZ_PLANT)/src/Table.ex || \
		{ echo "NEGATIVE CONTROL FAILED: the open/1 count-check plant matched nothing"; exit 1; }
	@$(ELIXIRC) -o $(SCHEMA_ELIXIR_FUZZ_PLANT)/ebin $(SCHEMA_ELIXIR_FUZZ_PLANT)/src/*.ex
	@grep '^unit $(SCHEMA_ELIXIR_FUZZ_PLANT_UNIT) ' $(SCHEMA_ELIXIR_FUZZ_CORPUS) > $(SCHEMA_ELIXIR_FUZZ_PLANT)/corpus.txt
	@if SCHEMA_ELIXIR_FUZZ_SEED=$(SCHEMA_ELIXIR_FUZZ_SEED) \
			timeout $$(($(SCHEMA_ELIXIR_FUZZ_PLANT_SECONDS)+30))s elixir -pa $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN) -pa $(SCHEMA_ELIXIR_FUZZ_PLANT)/ebin \
			$(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) $(SCHEMA_ELIXIR_FUZZ_PLANT)/corpus.txt \
			$(SCHEMA_ELIXIR_FUZZ_PLANT_SECONDS) $(SCHEMA_ELIXIR_FUZZ_PLANT)/saved \
			> $(SCHEMA_ELIXIR_FUZZ_PLANT)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping open/1's count check left the fuzzer green"; \
		cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/log; exit 1; \
	fi
	@grep -q '^FINDING ' $(SCHEMA_ELIXIR_FUZZ_PLANT)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fuzzer failed without a FINDING line"; cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/log; exit 1; }
	@n=$$(ls -A $(SCHEMA_ELIXIR_FUZZ_PLANT)/saved | wc -l | tr -d ' '); \
		[ "$$n" -ge 1 ] || { echo "NEGATIVE CONTROL FAILED: the finding was not saved"; exit 1; }
	@if elixir -pa $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN) -pa $(SCHEMA_ELIXIR_FUZZ_PLANT)/ebin $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) \
			--replay $(SCHEMA_ELIXIR_FUZZ_PLANT)/corpus.txt $(SCHEMA_ELIXIR_FUZZ_PLANT)/saved \
			> $(SCHEMA_ELIXIR_FUZZ_PLANT)/replay.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted reader replayed the saved input green"; \
		cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/replay.log; exit 1; \
	fi
	@grep -q 'REPLAY FAILED' $(SCHEMA_ELIXIR_FUZZ_PLANT)/replay.log || \
		{ echo "NEGATIVE CONTROL FAILED: replay refused without naming the saved input"; \
		  cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/replay.log; exit 1; }
	@if ! elixir -pa $(SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN) -pa $(ELIXIR_FUZZ_EBIN) $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) \
			--replay $(SCHEMA_ELIXIR_FUZZ_PLANT)/corpus.txt $(SCHEMA_ELIXIR_FUZZ_PLANT)/saved \
			> $(SCHEMA_ELIXIR_FUZZ_PLANT)/replay.green.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the unplanted reader is red on the planted finding"; \
		cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/replay.green.log; exit 1; \
	fi
	@echo 'schema-elixir-nightly-fuzz negative control: dropped open/1 count check found, saved, and replayed red; the unplanted reader replays it green'
	# THE PLANTED-PATTERN CONTROL: a scratch harness loses its JSON reader; the
	# static contract must go red on that copy and green on the real one.
	@sed '/^    :json,$$/d' $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) > $(SCHEMA_ELIXIR_FUZZ_PLANT)/harness.bad.exs
	@if SCHEMA_ELIXIR_FUZZ_HARNESS=$(CURDIR)/$(SCHEMA_ELIXIR_FUZZ_PLANT)/harness.bad.exs \
			go test ./internal/ci -count=1 -timeout 600s -run '^TestSchemaElixirNightlyFuzzPlantedBadPattern$$' \
			> $(SCHEMA_ELIXIR_FUZZ_PLANT)/pattern.bad.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a harness with no JSON reader passed the planted-pattern test"; \
		cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/pattern.bad.log; exit 1; \
	fi
	@SCHEMA_ELIXIR_FUZZ_HARNESS=$(CURDIR)/$(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) \
		go test ./internal/ci -count=1 -timeout 600s -run '^TestSchemaElixirNightlyFuzzPlantedBadPattern$$' \
		> $(SCHEMA_ELIXIR_FUZZ_PLANT)/pattern.good.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the real harness did not pass the planted-pattern test"; \
		  cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/pattern.good.log; exit 1; }
	@echo 'schema-elixir-nightly-fuzz planted-pattern control: a harness with no JSON reader reds the test; the real harness is green'

# THE SMOKE, which is what CI runs on every push: the corpus is derived, every
# saved input is replayed through the readers, and the contract, workflow and
# planted-pattern tests run. No clock, no mutation, no network.
.PHONY: schema-elixir-nightly-fuzz-smoke
schema-elixir-nightly-fuzz-smoke: schema-elixir-nightly-fuzz-corpus schema-elixir-nightly-fuzz-replay
	go test ./internal/ci -count=1 -timeout 600s -run '^TestSchemaElixirNightlyFuzz'

# THE FAST CI ARM, inside the one-minute budget: the smoke plus the structural
# checks and the pattern test (no toolchain boot, no mutation).
.PHONY: schema-elixir-nightly-fuzz-ci
schema-elixir-nightly-fuzz-ci: schema-elixir-nightly-fuzz-smoke
	@echo 'schema-elixir-nightly-fuzz: fast arms green (contract, workflow, planted pattern, replay)'
