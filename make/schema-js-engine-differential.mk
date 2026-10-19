# THE JAVASCRIPT ENGINE DIFFERENTIAL (ideas roadmap group
# schema-per-language-lenses): the same generated JavaScript conformance tests,
# run on Node, Deno and a headless browser engine, over the corpus the other js
# targets generate. Every engine answers the same driver contract
# (test/conformance/README.md) against the same expectations; the differential
# then requires the per-case answers to be identical. BigInt and number
# semantics are the thing a second and third engine are for: an emitter that
# lets a 64-bit value through Number() agrees with itself on Node and stops
# agreeing with the corpus.
#
# THE TOOLS ARE PINNED, and the target refuses by name when one is missing. The
# fleet play installs them; this file never downloads a runtime.
# The js leg pins NODE to a repo-local dist/ runtime; use it when it resolves
# and the PATH node otherwise, so this gate is not tied to the author's Mac.
NODE     ?= node
JS_ENGINE_NODE ?= $(if $(wildcard $(NODE)),$(NODE),node)
DENO     ?= deno
BROWSER  ?= chromium

# The versions the fleet installs. Override with DENO_VERSION=... on the make
# line only for a deliberate bump, and keep the workflow's fleet play in step.
DENO_VERSION    ?= 2.5.3
BROWSER_VERSION ?= 141.0.7390.54

JS_ENGINE_DIFF_DIR      := build/js-engine-differential
JS_ENGINE_DIFF_MANIFEST := testdata/conformance/tables/MANIFEST.txt
JS_ENGINE_DIFF_ENGINES  := node deno browser

# Every engine's driver is the port's own conformance command under that
# engine. Nothing here reimplements the port: the harness hands it a derived
# manifest and an output directory exactly as it hands the committed js leg.
.PHONY: schema-js-engine-differential-probe
schema-js-engine-differential-probe:
	@missing=; \
	check() { if ! command -v "$$2" >/dev/null 2>&1; then missing="$$missing $$1"; fi; }; \
	check NODE "$(JS_ENGINE_NODE)"; check DENO "$(DENO)"; check BROWSER "$(BROWSER)"; \
	if [ -n "$$missing" ]; then \
		echo "tool missing:$$missing on $$(hostname)"; \
		echo "  pins: NODE=$(JS_ENGINE_NODE) DENO=$(DENO) $(DENO_VERSION) BROWSER=$(BROWSER) $(BROWSER_VERSION)"; \
		echo "  the fleet play (rowan-tools/fleet) installs them; no runtime is downloaded here"; \
		exit 1; \
	fi
	@echo "js engine differential tools: node $$($(JS_ENGINE_NODE) --version), deno $$($(DENO) --version | head -1), browser $$($(BROWSER) --version 2>/dev/null | head -1)"

# The registry files are one line each, the substituted-registry shape
# test/conformance/harness/registry.go reads. `js` is the language under test,
# not three languages: the differential is one leg's answers under three
# engines, so each run is compared to the corpus AND to the other runs.
$(JS_ENGINE_DIFF_DIR)/registry/node.txt:
	@mkdir -p $(dir $@)
	@printf 'js test/conformance/js/driver\n' > $@

$(JS_ENGINE_DIFF_DIR)/registry/deno.txt:
	@mkdir -p $(dir $@)
	@printf 'js test/conformance/js/deno-driver\n' > $@

$(JS_ENGINE_DIFF_DIR)/registry/browser.txt:
	@mkdir -p $(dir $@)
	@printf 'js test/conformance/js/browser-driver\n' > $@

# One engine's whole conformance run. The harness itself compares every case
# with the corpus; a green run is the engine agreeing with the pinned answers.
.PHONY: schema-js-engine-differential-node
schema-js-engine-differential-node: build/conformance-harness build/tables-generated-js/.stamp $(JS_ENGINE_DIFF_DIR)/registry/node.txt
	@rm -rf $(JS_ENGINE_DIFF_DIR)/node
	@NODE=$(JS_ENGINE_NODE) ./build/conformance-harness run --only js \
		--manifest $(JS_ENGINE_DIFF_MANIFEST) \
		--drivers $(JS_ENGINE_DIFF_DIR)/registry/node.txt \
		--work $(JS_ENGINE_DIFF_DIR)/node
	@echo 'js engine differential: node agrees with the conformance corpus'

.PHONY: schema-js-engine-differential-deno
schema-js-engine-differential-deno: build/conformance-harness build/tables-generated-js/.stamp $(JS_ENGINE_DIFF_DIR)/registry/deno.txt
	@rm -rf $(JS_ENGINE_DIFF_DIR)/deno
	@DENO=$(DENO) ./build/conformance-harness run --only js \
		--manifest $(JS_ENGINE_DIFF_MANIFEST) \
		--drivers $(JS_ENGINE_DIFF_DIR)/registry/deno.txt \
		--work $(JS_ENGINE_DIFF_DIR)/deno
	@echo 'js engine differential: deno agrees with the conformance corpus'

.PHONY: schema-js-engine-differential-browser
schema-js-engine-differential-browser: build/conformance-harness build/tables-generated-js/.stamp $(JS_ENGINE_DIFF_DIR)/registry/browser.txt
	@rm -rf $(JS_ENGINE_DIFF_DIR)/browser
	@BROWSER=$(BROWSER) ./build/conformance-harness run --only js \
		--manifest $(JS_ENGINE_DIFF_MANIFEST) \
		--drivers $(JS_ENGINE_DIFF_DIR)/registry/browser.txt \
		--work $(JS_ENGINE_DIFF_DIR)/browser
	@echo 'js engine differential: browser agrees with the conformance corpus'

# THE GATE: three engines, every case identical. Each engine's own run already
# holds it to the corpus; the diff holds the engines to each other, which is
# what makes a BigInt/number divergence visible even where the corpus has no
# deciding fixture.
.PHONY: schema-js-engine-differential
schema-js-engine-differential: schema-js-engine-differential-probe
	$(MAKE) --no-print-directory schema-js-engine-differential-node
	$(MAKE) --no-print-directory schema-js-engine-differential-deno
	$(MAKE) --no-print-directory schema-js-engine-differential-browser
	@for engine in deno browser; do \
		diff -r $(JS_ENGINE_DIFF_DIR)/node/out/js $(JS_ENGINE_DIFF_DIR)/$$engine/out/js \
			> $(JS_ENGINE_DIFF_DIR)/$$engine.diff 2>&1 || { \
			echo "JS ENGINE DIFFERENTIAL: $$engine disagrees with node"; \
			cat $(JS_ENGINE_DIFF_DIR)/$$engine.diff; exit 1; }; \
	done
	@echo 'js engine differential: node, deno and browser answer every case identically'

# THE PLANTED BAD PATTERN. A copy of the generated tree has the BigInt
# conversion of every int64 replaced by Number(), the exact mistake a language
# with only doubles makes. The corpus's int64 wire and text cases (scalars,
# archive) decide it, so the run must go red. The pre- and post-replace greps
# prove the plant was in: an emitter that renamed the conversion cannot leave
# a silent, unsabotaged copy.
.PHONY: schema-js-engine-differential-negative-control
schema-js-engine-differential-negative-control: build/conformance-harness build/tables-generated-js/.stamp $(JS_ENGINE_DIFF_DIR)/registry/node.txt
	@rm -rf $(JS_ENGINE_DIFF_DIR)/plant
	@cp -r build/tables-generated-js $(JS_ENGINE_DIFF_DIR)/plant
	@files=$$(grep -rl 'BigInt.asIntN(64,' $(JS_ENGINE_DIFF_DIR)/plant); \
	[ -n "$$files" ] || { echo 'NEGATIVE CONTROL FAILED: the plant BigInt.asIntN(64, is not in the generated tree'; exit 1; }; \
	for f in $$files; do sed -i 's/BigInt\.asIntN(64,/Number(/g' "$$f"; done
	@if grep -rq 'BigInt.asIntN(64,' $(JS_ENGINE_DIFF_DIR)/plant; then \
		echo 'NEGATIVE CONTROL FAILED: the plant did not replace every BigInt.asIntN(64,'; exit 1; fi
	@if SCHEMA_JS_GENERATED=$(CURDIR)/$(JS_ENGINE_DIFF_DIR)/plant \
		NODE=$(JS_ENGINE_NODE) ./build/conformance-harness run --only js \
			--manifest $(JS_ENGINE_DIFF_MANIFEST) \
			--drivers $(JS_ENGINE_DIFF_DIR)/registry/node.txt \
			--work $(JS_ENGINE_DIFF_DIR)/plant-work \
			> $(JS_ENGINE_DIFF_DIR)/plant.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: BigInt->Number plant passed the conformance corpus'; \
		cat $(JS_ENGINE_DIFF_DIR)/plant.log; exit 1; fi
	@grep -Eq 'json-hostile|num-int64-min|FAIL' $(JS_ENGINE_DIFF_DIR)/plant.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant failed outside the int64 case'; \
		  cat $(JS_ENGINE_DIFF_DIR)/plant.log; exit 1; }
	@echo 'js engine differential negative control: BigInt.asIntN(64, replaced by Number() reds the int64 conformance cases'
