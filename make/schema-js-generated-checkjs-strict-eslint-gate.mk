# schema-js-generated-checkjs-strict-eslint-gate
#
# THE JAVASCRIPT LENS OVER THE GENERATED OUTPUT. Two checkers, two questions:
#
#   eslint  — the generated readers, writers and runtime are lint-clean under a
#             rule set STRICTER than eslint:recommended (make/js.mk's
#             tables-js-clean is the recommended baseline; this file adds the
#             equality, declaration and eval rules that the baseline leaves
#             out). A planted `var`/`==` is red (the negative control below).
#
#   tsc     — `checkJs` with `--strict` over the same generated modules. This
#             target is the TRIAGE gate right now: the full strict run reports
#             findings (implicit-any in untyped generated JS, the node
#             `process` global, plus the empty-arm call-signature and
#             scratch-holder findings in the report). It is deliberately kept
#             red until the generator changes land; a gate that hides its own
#             findings would be a gate with no blade.
#
# The corpus is the SAME one make/js.mk's generate targets read: the committed
# generated/ trees (examples, examples128, bench) and, when it has been built,
# build/tables-generated-js (tables/, test/tables, examples-wide). No second
# corpus is invented.
#
# TOOL PINS. TypeScript is invoked through npx at the pinned version, from its
# official npm package, so a runner needs no global install and the version is
# fixed. eslint is the fleet's pin (make/js.mk TOOLCHAIN_PINS_js := NODE ESLINT).
# A missing tool refuses by name, never fakes a pass.

ESLINT     ?= eslint
NPX        ?= npx
SCHEMA_JS_CHECKJS_TS_VERSION ?= 5.9.3
SCHEMA_JS_CHECKJS_TSC := $(NPX) --yes --package typescript@$(SCHEMA_JS_CHECKJS_TS_VERSION) tsc

# The strict ESLint layer: rules the recommended set does not include. eqeqeq
# allows `== null` because the generated code deliberately uses it to catch
# both null and undefined (the TypeScript-idiomatic null test); nothing else
# is excused.
SCHEMA_JS_STRICT_ESLINT_RULES := \
	'{"eqeqeq":["error","always",{"null":"ignore"}],"no-var":"error","no-implicit-globals":"error","no-eval":"error","no-implied-eval":"error","no-new-func":"error","no-return-assign":"error","no-sequences":"error","no-throw-literal":"error","no-useless-call":"error","no-with":"error","radix":"error"}'

# The committed generated trees are the fast corpus. build/tables-generated-js
# joins when the tree is present (the nightly workflow builds it with the
# stamps below).
SCHEMA_JS_GATE_FAST_DIRS := generated/js generated/js-ludicrous generated/bench/js generated/bench/paired/js
SCHEMA_JS_GATE_FULL_DIRS := $(SCHEMA_JS_GATE_FAST_DIRS) $(wildcard build/tables-generated-js)
SCHEMA_JS_GATE_FULL_FILES := $(foreach d,$(SCHEMA_JS_GATE_FULL_DIRS),$(wildcard $(d)/*.js))

.PHONY: schema-js-generated-checkjs-strict-eslint-gate
schema-js-generated-checkjs-strict-eslint-gate: schema-js-generated-checkjs-strict-eslint-gate-eslint schema-js-generated-checkjs-strict-eslint-gate-checkjs

# ---- ESLint strict layer ----
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-eslint
schema-js-generated-checkjs-strict-eslint-gate-eslint:
	@command -v $(ESLINT) >/dev/null 2>&1 || { echo "tool missing: eslint"; exit 2; }
	@mkdir -p build
	@$(ESLINT) --rule $(SCHEMA_JS_STRICT_ESLINT_RULES) $(SCHEMA_JS_GATE_FAST_DIRS) > build/schema-js-checkjs-strict-eslint.log 2>&1 || { \
		rc=$$?; cat build/schema-js-checkjs-strict-eslint.log; \
		echo "schema-js generated eslint-strict: RED ($$rc)"; exit $$rc; }
	@echo 'schema-js generated eslint-strict: generated readers, writers and runtime are clean under the strict layer'

# ---- checkJs --strict triage gate (kept red on purpose, see header) ----
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-checkjs
schema-js-generated-checkjs-strict-eslint-gate-checkjs:
	@command -v $(NPX) >/dev/null 2>&1 || { echo "tool missing: npx"; exit 2; }
	@mkdir -p build/schema-js-checkjs
	@printf 'declare var process: { env: { NODE_ENV?: string } };\n' > build/schema-js-checkjs/globals.d.ts
	@$(SCHEMA_JS_CHECKJS_TSC) --version >/dev/null 2>&1 || { echo "tool missing: tsc ($(SCHEMA_JS_CHECKJS_TS_VERSION))"; exit 2; }
	@$(SCHEMA_JS_CHECKJS_TSC) --noEmit --allowJs --checkJs --strict \
		--target es2022 --module nodenext --moduleResolution nodenext --skipLibCheck \
		build/schema-js-checkjs/globals.d.ts $(SCHEMA_JS_GATE_FULL_FILES) \
		> build/schema-js-checkjs/strict.log 2>&1 || { \
		rc=$$?; \
		echo "schema-js generated checkJs --strict: RED ($$rc findings); log: build/schema-js-checkjs/strict.log"; \
		sed -E 's/.*error (TS[0-9]+).*/\1/' build/schema-js-checkjs/strict.log | sort | uniq -c | sort -rn | sed 's/^/  /'; \
		exit $$rc; }
	@echo 'schema-js generated checkJs --strict: clean over the generated corpus'

# The negative control for the ESLint half: a planted `var` with a loose `==`
# must be red, and the same tree without the plant must be green. This is the
# proof-the-gate-fires test the card asks for; the card's own Go test runs it.
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-eslint-negative-control
schema-js-generated-checkjs-strict-eslint-gate-eslint-negative-control: schema-js-generated-checkjs-strict-eslint-gate-eslint
	@rm -rf build/schema-js-checkjs-eslint-control && mkdir -p build/schema-js-checkjs-eslint-control/clean build/schema-js-checkjs-eslint-control/planted
	@cp generated/js/Enums.js build/schema-js-checkjs-eslint-control/clean/Enums.js
	@cp generated/js/Enums.js build/schema-js-checkjs-eslint-control/planted/Enums.js
	@printf '\nvar plantedBad = 1;\nif (plantedBad == 1) { plantedBad = 2; }\n' >> build/schema-js-checkjs-eslint-control/planted/Enums.js
	@$(ESLINT) --rule $(SCHEMA_JS_STRICT_ESLINT_RULES) build/schema-js-checkjs-eslint-control/clean > build/schema-js-checkjs-eslint-control/clean.log 2>&1 || { \
		echo 'NEGATIVE CONTROL FAILED: the unplanted copy is not clean'; cat build/schema-js-checkjs-eslint-control/clean.log; exit 1; }
	@if $(ESLINT) --rule $(SCHEMA_JS_STRICT_ESLINT_RULES) build/schema-js-checkjs-eslint-control/planted > build/schema-js-checkjs-eslint-control/planted.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the planted var/== stayed green'; exit 1; fi
	@grep -q 'no-var' build/schema-js-checkjs-eslint-control/planted.log || \
		{ echo 'NEGATIVE CONTROL FAILED: no-var did not name the plant'; cat build/schema-js-checkjs-eslint-control/planted.log; exit 1; }
	@grep -q 'eqeqeq' build/schema-js-checkjs-eslint-control/planted.log || \
		{ echo 'NEGATIVE CONTROL FAILED: eqeqeq did not name the plant'; cat build/schema-js-checkjs-eslint-control/planted.log; exit 1; }
	@echo 'schema-js eslint-strict negative control: the planted var/== is red on no-var and eqeqeq, the clean copy green'
