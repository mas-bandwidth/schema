# make/schema-js-nightly-fuzz.mk — NIGHTLY FUZZING of the JavaScript port's
# generated readers, driven by Jazzer.js (the port's native coverage-guided
# fuzzer). Included by the Makefile's wildcard include, so no Makefile edit is
# needed; make/js.mk supplies NODE and the generator stamps.
#
# THE THREE READER KINDS, one Jazzer.js entry point each (gen_targets.mjs):
#   load    <Root>Load(bytes, report)                  variable wire load
#   strict  <Root>FixedLoad(values, cap, bytes, ...)   fixed form (no strict flag)
#   text    <Root>FromJson(text, report)               text/JSON parse
#
# THE CORPUS is test/js-nightly-fuzz/corpus.txt: every schema directory the
# Makefile's own JS generate targets read, not a second corpus invented here.
# THE SEEDS are the Go fuzz corpus (internal/fuzz/testdata) and the
# conformance wire vectors (testdata/wire), converted by seed.mjs.
#
# THE ORACLE (the card's): no crash, no hang, no sanitizer/assert finding; a
# refusal is a clean typed answer (null/-1 plus a report), never a throw,
# never a silent success on malformed input. replay.mjs treats an escaped
# exception as the finding and is what the push-time test runs.
#
# THE TOOL IS PROBED, NEVER FAKED. Jazzer.js is pinned below. On a machine
# where the fleet play has not installed it the nightly target refuses with
# "tool missing: jazzer@<pin>" and exits non-zero; it never reports a green
# run it did not make.

JAZZER_VERSION ?= 4.0.0
JAZZER ?= jazzer
# default 10 minutes per reader kind (the card's "10 minutes per port")
SCHEMA_JS_FUZZ_SECONDS ?= 600
SCHEMA_JS_FUZZ_SEED ?= 0xc00c1e5eed

JS_FUZZ_DIR := test/js-nightly-fuzz
JS_FUZZ_BUILD := build/js-nightly-fuzz
JS_FUZZ_GENERATED := $(JS_FUZZ_BUILD)/generated
JS_FUZZ_TARGETS := $(JS_FUZZ_BUILD)/targets
JS_FUZZ_SEEDS := $(JS_FUZZ_BUILD)/seeds
JS_FUZZ_CORPUS := $(JS_FUZZ_DIR)/corpus
JS_FUZZ_CORPUS_LIST := $(JS_FUZZ_DIR)/corpus.txt
JS_FUZZ_REPLAY_UNITS := $(JS_FUZZ_DIR)/replay-units.txt

# One unit per output directory, exactly as make/js.mk does it: two schema
# directories generated into one output can collide on a class name.
$(JS_FUZZ_GENERATED)/.stamp: bin/schema $(JS_FUZZ_CORPUS_LIST)
	@mkdir -p $(JS_FUZZ_GENERATED)
	@i=0; while IFS= read -r p; do \
		case "$$p" in ''|\#*) continue;; esac; \
		i=$$((i+1)); \
		./bin/schema generate --lang js --out "$(JS_FUZZ_GENERATED)/u$$i" "$$p" || exit 1; \
	done < $(JS_FUZZ_CORPUS_LIST)
	@touch $@

$(JS_FUZZ_TARGETS)/.stamp: $(JS_FUZZ_GENERATED)/.stamp $(JS_FUZZ_DIR)/gen_targets.mjs
	@rm -rf $(JS_FUZZ_TARGETS) && mkdir -p $(JS_FUZZ_TARGETS)
	$(NODE) $(JS_FUZZ_DIR)/gen_targets.mjs $(JS_FUZZ_GENERATED) $(JS_FUZZ_TARGETS)
	@touch $@

$(JS_FUZZ_SEEDS)/.stamp: $(JS_FUZZ_DIR)/seed.mjs
	@rm -rf $(JS_FUZZ_SEEDS) && mkdir -p $(JS_FUZZ_SEEDS)
	$(NODE) $(JS_FUZZ_DIR)/seed.mjs internal/fuzz/testdata/fuzz testdata/wire $(JS_FUZZ_SEEDS)
	@touch $@

# THE TOOL PROBE is its own target so a missing fuzzer is named BEFORE the
# corpus is generated: a refusal that first spends ten minutes generating is a
# refusal nobody reads.
.PHONY: schema-js-nightly-fuzz-tool
schema-js-nightly-fuzz-tool:
	@command -v $(JAZZER) >/dev/null 2>&1 || { \
		echo "tool missing: jazzer@$(JAZZER_VERSION)"; \
		echo "  machines lacking it: $$(hostname)"; \
		echo "  install it through the fleet play (rowan-tools/fleet), then re-run"; \
		echo "  or override JAZZER=... with the pinned version $(JAZZER_VERSION)"; \
		exit 1; }
	@echo "js nightly fuzz tool: $(JAZZER) (pinned @jazzer.js/core@$(JAZZER_VERSION))"

# THE NIGHTLY TARGET. Heavy parts only: ten minutes per reader kind by default.
.PHONY: schema-js-nightly-fuzz
schema-js-nightly-fuzz: schema-js-nightly-fuzz-tool $(JS_FUZZ_TARGETS)/.stamp $(JS_FUZZ_SEEDS)/.stamp
	@set -e; for kind in load strict text; do \
		echo "js nightly fuzz: $$kind ($(SCHEMA_JS_FUZZ_SECONDS)s)"; \
		$(JAZZER) --fuzz_time $(SCHEMA_JS_FUZZ_SECONDS) \
			--corpus_dir $(JS_FUZZ_SEEDS) \
			$(JS_FUZZ_TARGETS)/$$kind.mjs; \
	done

# THE FAST HALF, run on every push: replay the saved inputs only. No fuzzer,
# no clock, no mutation — the saved bytes are the whole of the work.
$(JS_FUZZ_BUILD)/replay/.stamp: bin/schema $(JS_FUZZ_REPLAY_UNITS) $(JS_FUZZ_DIR)/gen_targets.mjs
	@rm -rf $(JS_FUZZ_BUILD)/replay && mkdir -p $(JS_FUZZ_BUILD)/replay/generated
	@i=0; while IFS= read -r p; do \
		case "$$p" in ''|\#*) continue;; esac; \
		i=$$((i+1)); \
		./bin/schema generate --lang js --out "$(JS_FUZZ_BUILD)/replay/generated/u$$i" "$$p" || exit 1; \
	done < $(JS_FUZZ_REPLAY_UNITS)
	$(NODE) $(JS_FUZZ_DIR)/gen_targets.mjs $(JS_FUZZ_BUILD)/replay/generated $(JS_FUZZ_BUILD)/replay/targets
	@touch $@

.PHONY: schema-js-nightly-fuzz-replay
schema-js-nightly-fuzz-replay: $(JS_FUZZ_BUILD)/replay/.stamp
	$(NODE) $(JS_FUZZ_DIR)/replay.mjs $(JS_FUZZ_BUILD)/replay/targets $(JS_FUZZ_CORPUS)

# PROOF THE GATE FIRES (STEP 5). Plant a reader bug in a scratch build: the
# variable reader's short-wire bound is removed from the emitter through
# go build -overlay, so only this control's scratch copy is broken. The saved
# input corpus/truncated_variable_form.bin is a two-byte wire (selector 0,
# form byte 1) that the real reader refuses; with the bound gone the reader
# constructs a DataView and reads before its start, which throws. The control
# requires the replay to be GREEN unplanted and RED planted, with the throw
# named, so neither half can pass for the other's reason.
.PHONY: schema-js-nightly-fuzz-negative-control
schema-js-nightly-fuzz-negative-control: bin/schema $(JS_FUZZ_DIR)/gen_targets.mjs
	@rm -rf $(JS_FUZZ_BUILD)/negative-control && mkdir -p $(JS_FUZZ_BUILD)/negative-control
	@sed 's|  if (bytes.length < 9) { report.malformed = true; return null; }|  // SABOTAGED: no short-wire bound|' \
		internal/codegen/jstable/variableruntime.go > $(JS_FUZZ_BUILD)/negative-control/variableruntime.go.txt
	@grep -q SABOTAGED $(JS_FUZZ_BUILD)/negative-control/variableruntime.go.txt || \
		{ echo 'NEGATIVE CONTROL FAILED: the sabotage patched nothing'; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/jstable/variableruntime.go":"%s/build/js-nightly-fuzz/negative-control/variableruntime.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JS_FUZZ_BUILD)/negative-control/overlay.json
	go build -overlay=$(JS_FUZZ_BUILD)/negative-control/overlay.json \
		-o $(JS_FUZZ_BUILD)/negative-control/schema ./cmd/schema
	@./bin/schema generate --lang js --out $(JS_FUZZ_BUILD)/negative-control/normal/generated/u1 tables/examples
	@$(JS_FUZZ_BUILD)/negative-control/schema generate --lang js --out $(JS_FUZZ_BUILD)/negative-control/planted/generated/u1 tables/examples
	@$(NODE) $(JS_FUZZ_DIR)/gen_targets.mjs $(JS_FUZZ_BUILD)/negative-control/normal/generated $(JS_FUZZ_BUILD)/negative-control/normal/targets
	@$(NODE) $(JS_FUZZ_DIR)/gen_targets.mjs $(JS_FUZZ_BUILD)/negative-control/planted/generated $(JS_FUZZ_BUILD)/negative-control/planted/targets
	@if ! $(NODE) $(JS_FUZZ_DIR)/replay.mjs $(JS_FUZZ_BUILD)/negative-control/normal/targets $(JS_FUZZ_CORPUS) \
			> $(JS_FUZZ_BUILD)/negative-control/normal.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the unplanted replay is not green'; \
		cat $(JS_FUZZ_BUILD)/negative-control/normal.log; exit 1; \
	fi
	@if $(NODE) $(JS_FUZZ_DIR)/replay.mjs $(JS_FUZZ_BUILD)/negative-control/planted/targets $(JS_FUZZ_CORPUS) \
			> $(JS_FUZZ_BUILD)/negative-control/planted.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a reader with no short-wire bound left the replay green'; \
		cat $(JS_FUZZ_BUILD)/negative-control/planted.log; exit 1; \
	fi
	@grep -Eq 'FAIL .*truncated_variable_form.bin \(load\): (RangeError|Error)' \
		$(JS_FUZZ_BUILD)/negative-control/planted.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the replay went red, but not on an escaped read'; \
		  cat $(JS_FUZZ_BUILD)/negative-control/planted.log; exit 1; }
	@echo 'js nightly fuzz negative control: a reader with no short-wire bound turns the saved-input replay RED'
