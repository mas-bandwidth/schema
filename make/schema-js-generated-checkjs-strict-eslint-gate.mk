# schema-js-generated-checkjs-strict-eslint-gate
#
# THE JAVASCRIPT LENS OVER THE GENERATED OUTPUT. Two checkers over the same
# generated readers, writers and runtime units:
#
#   eslint  the strict layer: eslint.config.mjs (eslint:recommended written
#           out) plus the equality, declaration and eval rules below.
#   tsc     TypeScript checkJs with --strict.
#
# THE CORPUS is the one make/js.mk's generate targets write, every .js file
# under each directory, recursively (generated/bench/js/realworld is a
# subdirectory): the committed trees (generated/js, generated/js-ludicrous,
# generated/bench/js, generated/bench/paired/js) and build/tables-generated-js
# (tables/, test/tables, examples-wide). No second corpus.
#
# A GATE THAT CHECKS NOTHING IS RED. Each of these is a failure, never a skip:
#   - a corpus directory that is missing or holds no .js file;
#   - a tool that is not on PATH, or not at the pinned version ("tool missing");
#   - a corpus file the tool did not analyse: eslint's JSON report must name
#     every corpus file, and tsc --listFilesOnly must list every corpus file;
#   - eslint's defaults: a file eslint ignores is a warning ("File ignored"),
#     and --max-warnings 0 makes any warning red; an unused disable directive
#     is an error;
#   - an inline switch in a generated file that turns a checker off for the
#     file or without naming a rule: @ts-nocheck, @ts-ignore, a bare
#     eslint-disable, eslint-env, /* global */ or /* eslint */ config. A
#     narrow disable the generator emits must name its rule and carry a
#     "-- why" justification; tsc's narrow form is @ts-expect-error with one.
#   - checkJs reads every .js file it is given, so no "// @ts-check" header is
#     needed; skipLibCheck is NOT passed, so the .d.ts the gate writes is
#     checked too.
#
# TOOL PINS. eslint is the fleet pin (make/js.mk TOOLCHAIN_PINS_js; nova-tools
# fleet/roles/toolchains nova_eslint_version). TypeScript is pinned here; it is
# probed on PATH and never fetched at gate time.

SCHEMA_JS_GATE_ESLINT_VERSION ?= 9.39.5
SCHEMA_JS_CHECKJS_TS_VERSION  ?= 5.9.3
SCHEMA_JS_GATE_ESLINT ?= eslint
SCHEMA_JS_GATE_TSC    ?= tsc
SCHEMA_JS_GATE_NODE   ?= node

# The strict ESLint layer: rules the recommended set does not include. eqeqeq
# allows `== null` because the generated code uses it to catch both null and
# undefined; nothing else is excused.
SCHEMA_JS_STRICT_ESLINT_RULES := \
	'{"eqeqeq":["error","always",{"null":"ignore"}],"no-var":"error","no-implicit-globals":"error","no-eval":"error","no-implied-eval":"error","no-new-func":"error","no-return-assign":"error","no-sequences":"error","no-throw-literal":"error","no-useless-call":"error","no-with":"error","radix":"error"}'

SCHEMA_JS_GATE_FAST_DIRS := generated/js generated/js-ludicrous generated/bench/js generated/bench/paired/js
SCHEMA_JS_GATE_FULL_DIRS := $(SCHEMA_JS_GATE_FAST_DIRS) build/tables-generated-js
# The directories one gate run reads; the full corpus unless a caller narrows it.
SCHEMA_JS_GATE_DIRS ?= $(SCHEMA_JS_GATE_FULL_DIRS)
SCHEMA_JS_GATE_OUT  ?= build/schema-js-gate

# The real generated file the negative control plants into, header kept.
SCHEMA_JS_GATE_PLANT_FILE := generated/js/Degenerate.js

# ---- the corpus, with every refusal named ----
# Writes $(SCHEMA_JS_GATE_OUT)/files: one corpus file a line, sorted.
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-corpus
schema-js-generated-checkjs-strict-eslint-gate-corpus:
	@mkdir -p $(SCHEMA_JS_GATE_OUT)
	@: > $(SCHEMA_JS_GATE_OUT)/files
	@for d in $(SCHEMA_JS_GATE_DIRS); do \
		[ -d "$$d" ] || { echo "schema-js gate: FAIL corpus directory $$d is missing (make/js.mk generates it)"; exit 1; }; \
		n=$$(find "$$d" -name '*.js' -type f | wc -l | tr -d ' '); \
		[ "$$n" -gt 0 ] || { echo "schema-js gate: FAIL corpus directory $$d holds no .js file"; exit 1; }; \
		find "$$d" -name '*.js' -type f >> $(SCHEMA_JS_GATE_OUT)/files; \
	done
	@sort -o $(SCHEMA_JS_GATE_OUT)/files $(SCHEMA_JS_GATE_OUT)/files
	@if xargs grep -nE '@ts-nocheck|@ts-ignore|eslint-env|/\*[[:space:]]*(global|globals|eslint)[[:space:]]' < $(SCHEMA_JS_GATE_OUT)/files > $(SCHEMA_JS_GATE_OUT)/switches; then \
		cat $(SCHEMA_JS_GATE_OUT)/switches; echo "schema-js gate: FAIL a generated file turns a checker off (above)"; exit 1; fi
	@if xargs grep -nE 'eslint-disable(-next-line|-line)?([[:space:]]*(\*/)?$$|[[:space:]]+--)|@ts-expect-error[[:space:]]*$$' < $(SCHEMA_JS_GATE_OUT)/files > $(SCHEMA_JS_GATE_OUT)/bare; then \
		cat $(SCHEMA_JS_GATE_OUT)/bare; echo "schema-js gate: FAIL a disable names no rule or no justification (above)"; exit 1; fi
	@if xargs grep -nE 'eslint-disable|@ts-expect-error' < $(SCHEMA_JS_GATE_OUT)/files | grep -v -- ' -- ' > $(SCHEMA_JS_GATE_OUT)/unjustified; then \
		cat $(SCHEMA_JS_GATE_OUT)/unjustified; echo "schema-js gate: FAIL a disable carries no '-- why' justification (above)"; exit 1; fi
	@echo "schema-js gate corpus: $$(wc -l < $(SCHEMA_JS_GATE_OUT)/files | tr -d ' ') files under $(SCHEMA_JS_GATE_DIRS)"

.PHONY: schema-js-generated-checkjs-strict-eslint-gate
schema-js-generated-checkjs-strict-eslint-gate: schema-js-generated-checkjs-strict-eslint-gate-eslint schema-js-generated-checkjs-strict-eslint-gate-checkjs

# ---- ESLint strict layer ----
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-eslint
schema-js-generated-checkjs-strict-eslint-gate-eslint: schema-js-generated-checkjs-strict-eslint-gate-corpus
	@command -v $(SCHEMA_JS_GATE_ESLINT) >/dev/null 2>&1 || { echo "tool missing: eslint $(SCHEMA_JS_GATE_ESLINT_VERSION)"; exit 1; }
	@v=$$($(SCHEMA_JS_GATE_ESLINT) --version 2>&1); [ "$$v" = "v$(SCHEMA_JS_GATE_ESLINT_VERSION)" ] || { echo "tool missing: eslint $(SCHEMA_JS_GATE_ESLINT_VERSION) (found $$v)"; exit 1; }
	@rc=0; xargs $(SCHEMA_JS_GATE_ESLINT) --max-warnings 0 --report-unused-disable-directives-severity error \
		--rule $(SCHEMA_JS_STRICT_ESLINT_RULES) --format json -o $(SCHEMA_JS_GATE_OUT)/eslint.json \
		< $(SCHEMA_JS_GATE_OUT)/files > $(SCHEMA_JS_GATE_OUT)/eslint.stderr 2>&1 || rc=$$?; \
	[ -s $(SCHEMA_JS_GATE_OUT)/eslint.json ] || { cat $(SCHEMA_JS_GATE_OUT)/eslint.stderr; echo "schema-js generated eslint-strict: FAIL eslint wrote no report (exit $$rc)"; exit 1; }; \
	$(SCHEMA_JS_GATE_NODE) -e 'const fs=require("fs"),p=require("path");const r=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));const want=fs.readFileSync(process.argv[2],"utf8").split("\n").filter(Boolean).map(f=>p.resolve(f));const got=new Set(r.map(x=>x.filePath));let bad=0;for(const f of want){if(!got.has(f)){console.log("NOT ANALYSED "+f);bad++;}}for(const x of r){for(const m of x.messages){console.log(p.relative(process.cwd(),x.filePath)+":"+m.line+":"+m.column+" "+(m.severity===2?"error":"warning")+" "+(m.ruleId||"fatal")+" "+m.message);bad++;}}console.log("eslint analysed "+r.length+" of "+want.length+" corpus files, "+bad+" findings");process.exit(bad?1:0);' \
		$(SCHEMA_JS_GATE_OUT)/eslint.json $(SCHEMA_JS_GATE_OUT)/files > $(SCHEMA_JS_GATE_OUT)/eslint.log 2>&1; nrc=$$?; \
	if [ $$rc -ne 0 ] || [ $$nrc -ne 0 ]; then cat $(SCHEMA_JS_GATE_OUT)/eslint.log; \
		echo "schema-js generated eslint-strict: RED (eslint exit $$rc)"; exit 1; fi; \
	tail -1 $(SCHEMA_JS_GATE_OUT)/eslint.log
	@echo 'schema-js generated eslint-strict: every corpus file analysed, clean under the strict layer'

# ---- TypeScript checkJs --strict ----
SCHEMA_JS_CHECKJS_FLAGS := --noEmit --allowJs --checkJs --strict --target es2022 --module nodenext --moduleResolution nodenext --pretty false

.PHONY: schema-js-generated-checkjs-strict-eslint-gate-checkjs
schema-js-generated-checkjs-strict-eslint-gate-checkjs: schema-js-generated-checkjs-strict-eslint-gate-corpus
	@command -v $(SCHEMA_JS_GATE_TSC) >/dev/null 2>&1 || { echo "tool missing: tsc (typescript $(SCHEMA_JS_CHECKJS_TS_VERSION))"; exit 1; }
	@v=$$($(SCHEMA_JS_GATE_TSC) --version 2>&1); [ "$$v" = "Version $(SCHEMA_JS_CHECKJS_TS_VERSION)" ] || { echo "tool missing: tsc (typescript $(SCHEMA_JS_CHECKJS_TS_VERSION), found $$v)"; exit 1; }
	@printf 'declare var process: { env: { NODE_ENV?: string } };\n' > $(SCHEMA_JS_GATE_OUT)/globals.d.ts
	@xargs $(SCHEMA_JS_GATE_TSC) $(SCHEMA_JS_CHECKJS_FLAGS) --listFilesOnly $(SCHEMA_JS_GATE_OUT)/globals.d.ts \
		< $(SCHEMA_JS_GATE_OUT)/files > $(SCHEMA_JS_GATE_OUT)/tsc.files 2>&1 || { cat $(SCHEMA_JS_GATE_OUT)/tsc.files; echo "schema-js generated checkJs --strict: FAIL tsc could not list its program"; exit 1; }
	@# tsc prints root files relative and imported ones absolute: one absolute form.
	@awk -v pwd="$$(pwd)" '{ print (substr($$0,1,1)=="/") ? $$0 : pwd "/" $$0 }' $(SCHEMA_JS_GATE_OUT)/tsc.files > $(SCHEMA_JS_GATE_OUT)/tsc.files.abs && mv $(SCHEMA_JS_GATE_OUT)/tsc.files.abs $(SCHEMA_JS_GATE_OUT)/tsc.files
	@miss=0; while read -r f; do a=$$(cd "$$(dirname "$$f")" && pwd)/$$(basename "$$f"); \
		grep -Fxq "$$a" $(SCHEMA_JS_GATE_OUT)/tsc.files || { echo "NOT ANALYSED $$f"; miss=$$((miss+1)); }; \
		done < $(SCHEMA_JS_GATE_OUT)/files; \
		[ $$miss -eq 0 ] || { echo "schema-js generated checkJs --strict: FAIL $$miss corpus files are not in tsc's program"; exit 1; }
	@rc=0; xargs $(SCHEMA_JS_GATE_TSC) $(SCHEMA_JS_CHECKJS_FLAGS) $(SCHEMA_JS_GATE_OUT)/globals.d.ts \
		< $(SCHEMA_JS_GATE_OUT)/files > $(SCHEMA_JS_GATE_OUT)/tsc.log 2>&1 || rc=$$?; \
	n=$$(grep -c ' error TS' $(SCHEMA_JS_GATE_OUT)/tsc.log); \
	if [ $$rc -ne 0 ] || [ $$n -ne 0 ]; then \
		echo "schema-js generated checkJs --strict: RED ($$n findings over $$(wc -l < $(SCHEMA_JS_GATE_OUT)/files | tr -d ' ') files, tsc exit $$rc); log: $(SCHEMA_JS_GATE_OUT)/tsc.log"; \
		grep -o 'error TS[0-9]*' $(SCHEMA_JS_GATE_OUT)/tsc.log | sort | uniq -c | sort -rn | sed 's/^/  /'; \
		exit 1; fi
	@echo "schema-js generated checkJs --strict: every corpus file analysed ($$(wc -l < $(SCHEMA_JS_GATE_OUT)/files | tr -d ' ')), clean"

# ---- the negative control: plant in a REAL generated file ----
# The plant goes into generated/js/Degenerate.js itself, below its "Code
# generated ... DO NOT EDIT." header, and the SAME gate targets run over the
# committed corpus (the fast directories, so the control needs no regeneration).
# Red when the plant is in: eslint names the file on no-var and eqeqeq, tsc
# names the planted line on TS2322. Out: the file is restored byte for byte, and
# the same eslint run is green while tsc no longer names the planted line. The
# restore runs on every exit path (trap).
SCHEMA_JS_GATE_NC := build/schema-js-gate-negative-control
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-negative-control
schema-js-generated-checkjs-strict-eslint-gate-negative-control:
	@rm -rf $(SCHEMA_JS_GATE_NC) && mkdir -p $(SCHEMA_JS_GATE_NC)
	@head -1 $(SCHEMA_JS_GATE_PLANT_FILE) | grep -q '^// Code generated .* DO NOT EDIT\.$$' || { echo 'NEGATIVE CONTROL FAILED: the plant file lost its generated header'; exit 1; }
	@cp $(SCHEMA_JS_GATE_PLANT_FILE) $(SCHEMA_JS_GATE_NC)/original.js
	@set -e; trap 'cp $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE)' EXIT; \
	line=$$(( $$(wc -l < $(SCHEMA_JS_GATE_PLANT_FILE)) + 5 )); \
	printf '\n// schema-js gate plant\n/** @type {number} */\nexport var SchemaJsGatePlant = 0;\nSchemaJsGatePlant = "plant";\nif (SchemaJsGatePlant == 0) { SchemaJsGatePlant = 1; }\n' >> $(SCHEMA_JS_GATE_PLANT_FILE); \
	head -1 $(SCHEMA_JS_GATE_PLANT_FILE) | grep -q '^// Code generated .* DO NOT EDIT\.$$'; \
	if $(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-eslint SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/in-eslint > $(SCHEMA_JS_GATE_NC)/in-eslint.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the eslint gate stayed green with the plant in'; exit 1; fi; \
	grep -Eq '^$(SCHEMA_JS_GATE_PLANT_FILE):[0-9]+:[0-9]+ error no-var ' $(SCHEMA_JS_GATE_NC)/in-eslint.log || { cat $(SCHEMA_JS_GATE_NC)/in-eslint.log; echo 'NEGATIVE CONTROL FAILED: eslint did not name the plant on no-var'; exit 1; }; \
	grep -Eq '^$(SCHEMA_JS_GATE_PLANT_FILE):[0-9]+:[0-9]+ error eqeqeq ' $(SCHEMA_JS_GATE_NC)/in-eslint.log || { cat $(SCHEMA_JS_GATE_NC)/in-eslint.log; echo 'NEGATIVE CONTROL FAILED: eslint did not name the plant on eqeqeq'; exit 1; }; \
	if $(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-checkjs SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/in-tsc > $(SCHEMA_JS_GATE_NC)/in-tsc.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the checkJs gate stayed green with the plant in'; exit 1; fi; \
	grep -Fq '$(SCHEMA_JS_GATE_PLANT_FILE)('"$$line"',1): error TS2322' $(SCHEMA_JS_GATE_NC)/in-tsc/tsc.log || { cat $(SCHEMA_JS_GATE_NC)/in-tsc.log; echo "NEGATIVE CONTROL FAILED: tsc did not name the planted line $$line on TS2322"; exit 1; }; \
	cp $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE); \
	cmp -s $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE); \
	$(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-eslint SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/out-eslint > $(SCHEMA_JS_GATE_NC)/out-eslint.log 2>&1 || { cat $(SCHEMA_JS_GATE_NC)/out-eslint.log; echo 'NEGATIVE CONTROL FAILED: the eslint gate is not green with the plant out'; exit 1; }; \
	$(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-checkjs SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/out-tsc > $(SCHEMA_JS_GATE_NC)/out-tsc.log 2>&1 || true; \
	[ -f $(SCHEMA_JS_GATE_NC)/out-tsc/tsc.log ] || { cat $(SCHEMA_JS_GATE_NC)/out-tsc.log; echo 'NEGATIVE CONTROL FAILED: the checkJs gate did not run with the plant out'; exit 1; }; \
	if grep -Fq 'SchemaJsGatePlant' $(SCHEMA_JS_GATE_NC)/out-tsc/tsc.log || grep -Fq '$(SCHEMA_JS_GATE_PLANT_FILE)('"$$line"',' $(SCHEMA_JS_GATE_NC)/out-tsc/tsc.log; then \
		echo 'NEGATIVE CONTROL FAILED: tsc still names the plant after it was taken out'; exit 1; fi
	@cmp -s $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE) || { echo 'NEGATIVE CONTROL FAILED: the plant file was not restored'; exit 1; }
	@echo 'schema-js gate negative control: the plant in generated/js/Degenerate.js (header kept) is red on eslint no-var and eqeqeq and on tsc TS2322 at its line; with it out the file is restored, eslint is green and tsc no longer names it'
