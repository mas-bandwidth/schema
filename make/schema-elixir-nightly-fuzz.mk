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
SCHEMA_ELIXIR_FUZZ_TIME            ?= 10m
SCHEMA_ELIXIR_FUZZ_SECONDS         ?= 600
SCHEMA_ELIXIR_FUZZ_SEED            ?= 20261018
SCHEMA_ELIXIR_FUZZ_DIR             := build/schema-elixir-nightly-fuzz
SCHEMA_ELIXIR_FUZZ_CORPUS          := $(SCHEMA_ELIXIR_FUZZ_DIR)/corpus.txt
SCHEMA_ELIXIR_FUZZ_HARNESS         := $(SCHEMA_ELIXIR_FUZZ_DIR)/harness.exs
SCHEMA_ELIXIR_FUZZ_SAVED           := internal/ci/testdata/schema-elixir-nightly-fuzz/saved
SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE  := internal/ci/testdata/schema-elixir-nightly-fuzz/harness.exs
# The Go fuzz corpus and the conformance vectors seed the mutator, as the card
# asks. They are raw bytes, not this wire's, so they are extra SPLICE sources
# and a small share of the mutant population, never a valid encoding.
SCHEMA_ELIXIR_FUZZ_SEED_DIRS       := internal/fuzz/testdata testdata/conformance/tables

ELIXIR_FUZZ_EBIN := build/elixir-tables-ebin

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
schema-elixir-nightly-fuzz-prepare: bin/schema $(ELIXIR_FUZZ_EBIN)/.stamp $(SCHEMA_ELIXIR_FUZZ_CORPUS)
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
		timeout $(SCHEMA_ELIXIR_FUZZ_TIME) elixir -pa $(ELIXIR_FUZZ_EBIN) \
		$(SCHEMA_ELIXIR_FUZZ_HARNESS) $(SCHEMA_ELIXIR_FUZZ_CORPUS) \
		$(SCHEMA_ELIXIR_FUZZ_SECONDS) $(SCHEMA_ELIXIR_FUZZ_SAVED) \
		$(SCHEMA_ELIXIR_FUZZ_SEED_DIRS)
	@echo "schema-elixir-nightly-fuzz: $(SCHEMA_ELIXIR_FUZZ_TIME) over $(SCHEMA_ELIXIR_FUZZ_SECONDS)s, the three readers refused cleanly or round-tripped"

# THE FAST ARM: replay every saved input on every push. No clock, no mutation,
# no network — just the inputs a past run saved, through the same readers.
.PHONY: schema-elixir-nightly-fuzz-replay
schema-elixir-nightly-fuzz-replay: bin/schema $(SCHEMA_ELIXIR_FUZZ_CORPUS)
	@mkdir -p $(SCHEMA_ELIXIR_FUZZ_SAVED)
	@files=$$(ls -A $(SCHEMA_ELIXIR_FUZZ_SAVED) 2>/dev/null | grep -v -F -x '.gitkeep'); \
	if [ -z "$$files" ]; then \
		echo "schema-elixir-nightly-fuzz replay: no saved inputs (0)"; \
	else \
		$(MAKE) --no-print-directory $(ELIXIR_FUZZ_EBIN)/.stamp || exit 1; \
		elixir -pa $(ELIXIR_FUZZ_EBIN) $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) \
			--replay $(SCHEMA_ELIXIR_FUZZ_CORPUS) $(SCHEMA_ELIXIR_FUZZ_SAVED); \
	fi

# THE PLANTED-BAD-PATTERN CONTROL. The pattern test in internal/ci holds the
# harness to its three readers and four mutations; this plants a copy with the
# JSON reader removed and requires that test to go RED, then runs it on the
# real harness and requires GREEN. A scan nobody has watched fail is a green
# light with no bulb.
SCHEMA_ELIXIR_FUZZ_PLANT := build/schema-elixir-nightly-fuzz/planted
SCHEMA_ELIXIR_FUZZ_PLANT_SED := -e 's|module\.from_json(table, text)|module.form_json(table, text)|'

.PHONY: schema-elixir-nightly-fuzz-negative-control
schema-elixir-nightly-fuzz-negative-control:
	@rm -rf $(SCHEMA_ELIXIR_FUZZ_PLANT) && mkdir -p $(SCHEMA_ELIXIR_FUZZ_PLANT)
	@sed $(SCHEMA_ELIXIR_FUZZ_PLANT_SED) $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) > $(SCHEMA_ELIXIR_FUZZ_PLANT)/harness.exs
	@cmp -s $(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) $(SCHEMA_ELIXIR_FUZZ_PLANT)/harness.exs && \
		{ echo "NEGATIVE CONTROL FAILED: the plant matched nothing — the harness moved"; exit 1; } || true
	@if SCHEMA_ELIXIR_FUZZ_HARNESS=$(CURDIR)/$(SCHEMA_ELIXIR_FUZZ_PLANT)/harness.exs \
			go test ./internal/ci -count=1 -timeout 600s -run '^TestSchemaElixirNightlyFuzzPlantedBadPattern$$' \
			> $(SCHEMA_ELIXIR_FUZZ_PLANT)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the harness with its JSON reader removed left the planted-pattern test green"; \
		cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/log; exit 1; \
	fi
	@grep -q 'nightly-fuzz pattern missing: .*from_json' $(SCHEMA_ELIXIR_FUZZ_PLANT)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the pattern test went red, but not on the missing JSON reader"; \
		  cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/log; exit 1; }
	@SCHEMA_ELIXIR_FUZZ_HARNESS=$(CURDIR)/$(SCHEMA_ELIXIR_FUZZ_HARNESS_SOURCE) \
			go test ./internal/ci -count=1 -timeout 600s -run '^TestSchemaElixirNightlyFuzzPlantedBadPattern$$' \
			> $(SCHEMA_ELIXIR_FUZZ_PLANT)/log.green 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the real harness is not green"; cat $(SCHEMA_ELIXIR_FUZZ_PLANT)/log.green; exit 1; }
	@echo 'schema-elixir-nightly-fuzz negative control: removing from_json reds the planted-pattern test; the real harness is green'

# THE FAST CI ARM, inside the one-minute budget: the structural checks and the
# pattern test (no toolchain boot, no mutation) and the replay of saved inputs.
.PHONY: schema-elixir-nightly-fuzz-ci
schema-elixir-nightly-fuzz-ci: schema-elixir-nightly-fuzz-corpus schema-elixir-nightly-fuzz-replay
	go test ./internal/ci -count=1 -timeout 600s -run '^TestSchemaElixirNightlyFuzz'
	@echo 'schema-elixir-nightly-fuzz: fast arms green (contract, workflow, planted pattern, replay)'
