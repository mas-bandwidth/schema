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
#     every corpus file, and tsc --listFiles must list every corpus file;
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
# TRIAGE of the checkJs findings at the pin (TypeScript 5.9.3, 41,082 over 360
# files; ESLint strict: 0). Each code was sampled at its source line.
#   code   count  REAL/NOT  what it is, and the action
#   7006   36708  NOT  parameter implicitly any: the emitter writes no JSDoc
#                      types; pinned, taken down by schema-js-emitter-jsdoc-types
#   18047   1095  NOT  "possibly null" on [reader, verdict] tuples the code
#                      guards by verdict first (TableMessageOpenFramed); pinned
#   7053    1000  NOT  index of an inferred (boolean | any[]) array: [value, ok]
#                      returns read as arrays, not tuples, without JSDoc; pinned
#   2339     674  NOT  the same tuple inference, number | TableMessageReader; pinned
#   7005/7034 447/213  NOT  `let x = []` accumulators implicitly any[]; pinned
#   2322/2345 303/202  NOT  literal doc-object shapes inferred narrower than the
#                      slot they fill (View descriptors, tuple results); pinned
#   7023/7008/7022 113/103/26  NOT  recursive inference without annotations; pinned
#   2554      94  NOT  a fixed decoder of an all-scalar struct takes (value, view,
#                      at) and the identity path passes a fourth `stage`
#                      (jstable/fixedmodule.go:455); JS ignores it and the
#                      stage.refused check after it stays 0 because that decoder
#                      cannot refuse. Dead argument, no wrong output; pinned for
#                      the schema-js-checkjs-* follow-ups
#   2365      50  NOT  `lo - s.Base[0]` with lo an untyped parameter: `any - x`
#                      types as number, then `& bigint` is flagged; callers pass
#                      bigint (tableMessageWriteScalar); pinned
#   4104      26  NOT  Object.freeze([]) assigned to an inferred mutable any[]
#                      (jstable/view.go:182); pinned
#   2353      25  NOT  entries.push of a blob entry whose shape differs from the
#                      first push the array was inferred from; pinned
#   2367       3  NOT  NUMBER_SCRATCH.value compared after inference fixed it to
#                      a literal (js/js.go:223); the reserved-zero check is right
#                      at runtime; pinned
# No REAL finding, so no generator fix rides on this card; the pin is the gate.
# OTHER PORTS sharing a flagged pattern: none of these is reachable in the typed
# ports (Go, C, C++, C#, Java, Rust, Dart), whose compilers refuse an arity
# mismatch, an implicit any or a tuple read as an array, and grep of
# internal/codegen for the 4-argument FixedDecode call, Object.freeze([]) and
# NUMBER_SCRATCH finds them only under js/ and jstable/. Elixir is dynamic with
# no checker in this lens.
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
	@empty=0; while read -r f; do [ -s "$$f" ] || { echo "schema-js gate: FAIL empty corpus file $$f"; empty=1; }; done < $(SCHEMA_JS_GATE_OUT)/files; \
	[ $$empty -eq 0 ] || exit 1
	@# One grep over every corpus file (no xargs batches), so its exit is the answer:
	@# 0 found, 1 none, 2 a read error, which is a failure too.
	@rc=0; grep -nHE '@ts-nocheck|@ts-ignore|eslint-env|/\*[[:space:]]*(global|globals|eslint)[[:space:]]' $$(cat $(SCHEMA_JS_GATE_OUT)/files) > $(SCHEMA_JS_GATE_OUT)/switches || rc=$$?; \
		[ $$rc -eq 1 ] || { cat $(SCHEMA_JS_GATE_OUT)/switches; echo "schema-js gate: FAIL a generated file turns a checker off, or grep could not read the corpus (grep exit $$rc)"; exit 1; }
	@rc=0; grep -nHE 'eslint-disable(-next-line|-line)?([[:space:]]*(\*/)?$$|[[:space:]]*\*/|[[:space:]]+--)|@ts-expect-error[[:space:]]*$$' $$(cat $(SCHEMA_JS_GATE_OUT)/files) > $(SCHEMA_JS_GATE_OUT)/bare || rc=$$?; \
		[ $$rc -eq 1 ] || { cat $(SCHEMA_JS_GATE_OUT)/bare; echo "schema-js gate: FAIL a disable names no rule, or grep could not read the corpus (grep exit $$rc)"; exit 1; }
	@rc=0; grep -nHE 'eslint-disable|@ts-expect-error' $$(cat $(SCHEMA_JS_GATE_OUT)/files) > $(SCHEMA_JS_GATE_OUT)/disables || rc=$$?; \
		[ $$rc -le 1 ] || { echo "schema-js gate: FAIL grep could not read the corpus (grep exit $$rc)"; exit 1; }; \
		if grep -v -- ' -- ' $(SCHEMA_JS_GATE_OUT)/disables > $(SCHEMA_JS_GATE_OUT)/unjustified; then \
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
SCHEMA_JS_CHECKJS_FLAGS := --noEmit --allowJs --checkJs --strict --target es2022 --module nodenext --moduleResolution nodenext --pretty false --listFiles
# THE RATCHET. The checkJs residue is not zero yet: schema-js-emitter-jsdoc-types
# and the schema-js-checkjs-* cards take it down, and
# schema-js-checkjs-ratchet-to-zero sets this pin to nothing. Until then the
# per-code counts are PINNED here, exactly as TypeScript 5.9.3 reports them over
# the complete 360-file corpus at sprint/schema-js 8ea51076 (receipt: the full
# gate at the w5 head, total below). The gate is RED when any code's count rises
# above its pin or a code appears that has no pin; that is what catches a new
# real bug in generated output. A count BELOW its pin is green and prints a
# RATCHET-DOWN line naming the new count: the commit that made it fall lowers
# the pin to that number, so the room a fix frees is never left for a new bug.
# The pin can only be lowered by hand; no code is excused by a disable, no file
# by @ts-nocheck (the corpus target refuses both).
override SCHEMA_JS_CHECKJS_BASELINE := 18047:1095,2322:303,2339:674,2345:202,2353:25,2365:50,2367:3,2554:94,4104:26,7005:447,7006:36708,7008:103,7022:26,7023:113,7034:213,7053:1000
override SCHEMA_JS_CHECKJS_BASELINE_TOTAL := 41082

# The ratchet itself, in POSIX awk so the gate needs no file outside this one.
# Input: tsc's --pretty false --listFiles output. Every line is one of: a
# located diagnostic "file(line,col): error TSn: message"; one of the two
# continuation shapes TypeScript 5.9.3 writes under a diagnostic; a corpus file
# tsc listed; the gate's globals.d.ts; a TypeScript lib .d.ts. Anything else
# (a crash, a partial run, an unlocated error) is a FAIL, never skipped. Every
# corpus file must be listed (NOT ANALYSED otherwise); tsc's status must be 0
# with no diagnostic or 1/2 with at least one (a signal or other status FAILs).
define SCHEMA_JS_CHECKJS_RATCHET_AWK
BEGIN {
  ne = 0; parsed = 0; indiag = 0; nexp = 0
  while ((getline f < files) > 0) { if (f != "") { want[f] = 1; nexp++ } }
  close(files)
  if (nexp == 0) err[++ne] = "empty expected input set"
  if (baseline == "") err[++ne] = "empty diagnostic baseline"
  nb = split(baseline, pairs, ",")
  for (i = 1; i <= nb; i++) {
    if (pairs[i] !~ /^[0-9]+:[0-9]+$$/) { err[++ne] = "invalid baseline entry: " pairs[i]; continue }
    split(pairs[i], kv, ":")
    if (kv[1] in base) { err[++ne] = "duplicate baseline entry: " pairs[i]; continue }
    base[kv[1]] = kv[2] + 0; order[++no] = kv[1]
  }
}
$$0 == "" { next }
$$0 ~ /^[^ ].*\([0-9]+,[0-9]+\): error TS[0-9]+: ./ {
  code = substr($$0, index($$0, "): error TS") + 11); sub(/:.*/, "", code)
  if (!(code in counts)) extra[++nx] = code
  counts[code]++; parsed++; indiag = 1; next
}
indiag && $$0 ~ /^  (Property .+ does not exist on type .+|Type .+ is not assignable to type .+\.)$$/ { next }
{
  indiag = 0
  if ($$0 in want) { seen[$$0] = 1; next }
  if (index($$0, root "/") == 1 && (substr($$0, length(root) + 2) in want)) { seen[substr($$0, length(root) + 2)] = 1; next }
  if ($$0 == gdts || $$0 == root "/" gdts) next
  if ($$0 ~ /\/node_modules\/typescript\/lib\/lib\.[A-Za-z0-9.]+\.d\.ts$$/) next
  err[++ne] = "unparsed diagnostic or tool output: " $$0
}
END {
  for (f in want) if (!(f in seen)) err[++ne] = "NOT ANALYSED " f
  if (rc == 0) { if (parsed != 0) err[++ne] = "tsc returned success with diagnostic output" }
  else if (rc != 1 && rc != 2) err[++ne] = "unexpected tsc exit status " rc " (only 1 or 2 denote TypeScript diagnostics)"
  else if (parsed == 0) err[++ne] = "tsc returned a diagnostics status without parsed diagnostics"
  line = ""
  for (i = 1; i <= no; i++) line = line " TS" order[i] "=" (counts[order[i]] + 0)
  for (i = 1; i <= nx; i++) if (!(extra[i] in base)) line = line " TS" extra[i] "=" counts[extra[i]]
  print "checkJs diagnostic counts:" line "; total " parsed " (pinned " total "); tsc exit " rc
  for (i = 1; i <= nx; i++) { c = extra[i]; m = (c in base) ? base[c] : 0; if (counts[c] > m) err[++ne] = "TS" c " count " counts[c] " exceeds baseline " m }
  for (i = 1; i <= no; i++) { c = order[i]; if (counts[c] + 0 < base[c]) print "RATCHET-DOWN TS" c " count " (counts[c] + 0) " is below its pin " base[c] ": lower it in SCHEMA_JS_CHECKJS_BASELINE" }
  for (i = 1; i <= ne; i++) print "FAIL " err[i]
  exit (ne > 0 ? 1 : 0)
}
endef
export SCHEMA_JS_CHECKJS_RATCHET_AWK

# SCHEMA_JS_CHECKJS_RATCHET runs the ratchet: $(1) the tsc log, $(2) the corpus
# list, $(3) the globals.d.ts path, $(4) the baseline, $(5) tsc's status, $(6) the pinned total.
SCHEMA_JS_CHECKJS_RATCHET = awk -v files="$(2)" -v root="$(CURDIR)" -v gdts="$(3)" -v baseline="$(4)" -v rc="$(5)" -v total="$(6)" "$$SCHEMA_JS_CHECKJS_RATCHET_AWK" "$(1)"

# The ratchet's own refusals, on synthetic tsc output (no tool needed): status 2
# with the multiline shape, same-code growth, a new code, a partial or indented
# tool failure, an orphaned continuation, an unlocated diagnostic, a signal-like
# status, an unlisted corpus file, a success status with diagnostics, a count at
# its pin and one below it, and a clean run.
SCHEMA_JS_RATCHET_ST := build/schema-js-ratchet-self-test
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-ratchet-self-test
schema-js-generated-checkjs-strict-eslint-gate-ratchet-self-test:
	@command -v awk >/dev/null 2>&1 || { echo "tool missing: awk"; exit 1; }
	@rm -rf $(SCHEMA_JS_RATCHET_ST) && mkdir -p $(SCHEMA_JS_RATCHET_ST)
	@set -eu; d=$(SCHEMA_JS_RATCHET_ST); f=$$d/example.js; printf 'const example = 1;\n' > $$f; printf '%s\n' $$f > $$d/files; \
	one="$$f(1,1): error TS7006: implicit any"; \
	check() { name=$$1; rc=$$2; base=$$3; want=$$4; printf '%b\n' "$$5" > $$d/$$name.log; \
		got=0; $(call SCHEMA_JS_CHECKJS_RATCHET,$$d/$$name.log,$$d/files,$$d/none.d.ts,$$base,$$rc,0) > $$d/$$name.out 2>&1 || got=$$?; \
		if [ "$$want" = PASS ]; then [ $$got -eq 0 ] && ! grep -q '^FAIL' $$d/$$name.out || { cat $$d/$$name.out; echo "ratchet self-test FAILED: $$name should pass"; exit 1; }; \
		else [ $$got -eq 1 ] && grep -Fq "FAIL $$want" $$d/$$name.out || { cat $$d/$$name.out; echo "ratchet self-test FAILED: $$name should fail with: $$want"; exit 1; }; fi; }; \
	check status-2-multiline 2 7006:1 'TS2322 count 1 exceeds baseline 0' "$$f\n$$one\n$$f(2,1): error TS2322: type mismatch\n  Type 'boolean' is not assignable to type 'number'."; \
	check same-code-growth 1 7006:1 'TS7006 count 2 exceeds baseline 1' "$$f\n$$one\n$$f(2,1): error TS7006: second implicit any\n  Type 'boolean' is not assignable to type 'number'."; \
	check new-code 2 7006:5 'TS2741 count 1 exceeds baseline 0' "$$f\n$$f(3,1): error TS2741: missing property"; \
	check partial-tool-failure 2 7006:1 'unparsed diagnostic or tool output: TypeScript worker failed' "$$f\n$$one\nTypeScript worker failed after partial output"; \
	check indented-tool-failure 2 7006:1 'unparsed diagnostic or tool output:   TypeScript worker failed' "$$f\n$$one\n  TypeScript worker failed after partial output"; \
	check orphan-continuation 2 7006:1 'unparsed diagnostic or tool output:   TypeScript worker failed' "$$f\n  TypeScript worker failed"; \
	check unlocated 1 7006:1 'unparsed diagnostic or tool output: error TS7006' "$$f\nerror TS7006: malformed diagnostic"; \
	check signal-status 143 7006:1 'unexpected tsc exit status 143' "$$f\n$$one"; \
	check not-analysed 2 7006:1 "NOT ANALYSED $$f" "$$one"; \
	check success-with-diagnostics 0 7006:1 'tsc returned success with diagnostic output' "$$f\n$$one"; \
	check diagnostics-status-none 2 7006:1 'tsc returned a diagnostics status without parsed diagnostics' "$$f"; \
	check bad-baseline 2 7006:1,7006:2 'duplicate baseline entry: 7006:2' "$$f\n$$one"; \
	check at-pin 2 7006:1 PASS "$$f\n$$one"; \
	check below-pin 2 7006:2 PASS "$$f\n$$one"; grep -Fq 'RATCHET-DOWN TS7006 count 1 is below its pin 2' $$d/below-pin.out || { cat $$d/below-pin.out; echo 'ratchet self-test FAILED: below-pin printed no RATCHET-DOWN line'; exit 1; }; \
	check clean 0 7006:1 PASS "$$f"
	@echo 'schema-js checkJs ratchet self-test: status 2 multiline, same-code growth, new code, partial and indented tool failures, orphaned continuation, unlocated output, signal status, unlisted file, success-with-diagnostics, bad baseline, at-pin, below-pin (RATCHET-DOWN) and clean all behave'

.PHONY: schema-js-generated-checkjs-strict-eslint-gate-checkjs
schema-js-generated-checkjs-strict-eslint-gate-checkjs: schema-js-generated-checkjs-strict-eslint-gate-corpus schema-js-generated-checkjs-strict-eslint-gate-ratchet-self-test
	@command -v $(SCHEMA_JS_GATE_TSC) >/dev/null 2>&1 || { echo "tool missing: tsc (typescript $(SCHEMA_JS_CHECKJS_TS_VERSION))"; exit 1; }
	@vrc=0; v=$$($(SCHEMA_JS_GATE_TSC) --version 2>&1) || vrc=$$?; [ $$vrc -eq 0 ] && [ "$$v" = "Version $(SCHEMA_JS_CHECKJS_TS_VERSION)" ] || { echo "tool missing: tsc (typescript $(SCHEMA_JS_CHECKJS_TS_VERSION), version probe exit $$vrc, found $$v)"; exit 1; }
	@printf 'declare var process: { env: { NODE_ENV?: string } };\n' > $(SCHEMA_JS_GATE_OUT)/globals.d.ts
	@{ printf '%s\n' $(SCHEMA_JS_CHECKJS_FLAGS) $(SCHEMA_JS_GATE_OUT)/globals.d.ts; cat $(SCHEMA_JS_GATE_OUT)/files; } > $(SCHEMA_JS_GATE_OUT)/tsc.rsp
	@rc=0; $(SCHEMA_JS_GATE_TSC) @$(SCHEMA_JS_GATE_OUT)/tsc.rsp > $(SCHEMA_JS_GATE_OUT)/tsc.log 2>&1 || rc=$$?; \
	$(call SCHEMA_JS_CHECKJS_RATCHET,$(SCHEMA_JS_GATE_OUT)/tsc.log,$(SCHEMA_JS_GATE_OUT)/files,$(SCHEMA_JS_GATE_OUT)/globals.d.ts,$(SCHEMA_JS_CHECKJS_BASELINE),$$rc,$(SCHEMA_JS_CHECKJS_BASELINE_TOTAL)) > $(SCHEMA_JS_GATE_OUT)/tsc.ratchet.log 2>&1 || { cat $(SCHEMA_JS_GATE_OUT)/tsc.ratchet.log; echo "schema-js generated checkJs --strict: RED; tsc exit $$rc; log: $(SCHEMA_JS_GATE_OUT)/tsc.log"; exit 1; }; \
	cat $(SCHEMA_JS_GATE_OUT)/tsc.ratchet.log
	@echo "schema-js generated checkJs --strict: every corpus file analysed ($$(wc -l < $(SCHEMA_JS_GATE_OUT)/files | tr -d ' ')); no code above its pin"

# ---- the negative control: plant in a REAL generated file ----
# The plant goes into generated/js/Degenerate.js itself, below its "Code
# generated ... DO NOT EDIT." header, and the SAME gate targets run over the
# committed corpus (the fast directories, so the control needs no regeneration).
# Red when the plant is in: eslint names the file on no-var and eqeqeq, and
# the per-code ratchet rejects its TS2741, which has no pin in SCHEMA_JS_CHECKJS_BASELINE.
# no-dupe-keys proves eslint.config.mjs's recommended layer reaches generated
# files. The fast-subset run is only a control smoke; production defaults to the
# complete corpus. Out: the file is restored, the fast smoke is green, and tsc
# no longer names the plant. The restore runs on every exit path (trap).
SCHEMA_JS_GATE_NC := build/schema-js-gate-negative-control
.PHONY: schema-js-generated-checkjs-strict-eslint-gate-negative-control
schema-js-generated-checkjs-strict-eslint-gate-negative-control:
	@rm -rf $(SCHEMA_JS_GATE_NC) && mkdir -p $(SCHEMA_JS_GATE_NC)
	@head -1 $(SCHEMA_JS_GATE_PLANT_FILE) | grep -q '^// Code generated .* DO NOT EDIT\.$$' || { echo 'NEGATIVE CONTROL FAILED: the plant file lost its generated header'; exit 1; }
	@cp $(SCHEMA_JS_GATE_PLANT_FILE) $(SCHEMA_JS_GATE_NC)/original.js
	@set -e; trap 'cp $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE)' EXIT; trap 'exit 1' INT TERM HUP; \
	line=$$(( $$(wc -l < $(SCHEMA_JS_GATE_PLANT_FILE)) + 6 )); \
	printf '\n// schema-js gate plant\n/** @type {number} */\nexport var SchemaJsGatePlant = 0;\nSchemaJsGatePlant = "plant";\n/** @type {{ a: number }} */ export const SchemaJsGatePlantMissing = {};\nif (SchemaJsGatePlant == 0) { SchemaJsGatePlant = 1; }\nexport const SchemaJsGatePlantKeys = { a: 1, a: 2 };\n' >> $(SCHEMA_JS_GATE_PLANT_FILE); \
	head -1 $(SCHEMA_JS_GATE_PLANT_FILE) | grep -q '^// Code generated .* DO NOT EDIT\.$$'; \
	if $(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-eslint SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/in-eslint > $(SCHEMA_JS_GATE_NC)/in-eslint.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the eslint gate stayed green with the plant in'; exit 1; fi; \
	grep -Eq '^$(SCHEMA_JS_GATE_PLANT_FILE):[0-9]+:[0-9]+ error no-var ' $(SCHEMA_JS_GATE_NC)/in-eslint.log || { cat $(SCHEMA_JS_GATE_NC)/in-eslint.log; echo 'NEGATIVE CONTROL FAILED: eslint did not name the plant on no-var'; exit 1; }; \
	grep -Eq '^$(SCHEMA_JS_GATE_PLANT_FILE):[0-9]+:[0-9]+ error eqeqeq ' $(SCHEMA_JS_GATE_NC)/in-eslint.log || { cat $(SCHEMA_JS_GATE_NC)/in-eslint.log; echo 'NEGATIVE CONTROL FAILED: eslint did not name the plant on eqeqeq'; exit 1; }; \
	grep -Eq '^$(SCHEMA_JS_GATE_PLANT_FILE):[0-9]+:[0-9]+ error no-dupe-keys ' $(SCHEMA_JS_GATE_NC)/in-eslint.log || { cat $(SCHEMA_JS_GATE_NC)/in-eslint.log; echo 'NEGATIVE CONTROL FAILED: eslint did not name the plant on no-dupe-keys (eslint.config.mjs recommended layer)'; exit 1; }; \
	if $(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-checkjs SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/in-tsc > $(SCHEMA_JS_GATE_NC)/in-tsc.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the checkJs ratchet stayed green with the TS2741 plant in'; exit 1; fi; \
	grep -Eq '^generated/js/Degenerate[.]js\('"$$line"',[0-9]+\): error TS2741' $(SCHEMA_JS_GATE_NC)/in-tsc/tsc.log || { cat $(SCHEMA_JS_GATE_NC)/in-tsc.log; echo "NEGATIVE CONTROL FAILED: tsc did not name the planted line $$line on TS2741"; exit 1; }; \
	grep -Fq 'FAIL TS2741 count 1 exceeds baseline 0' $(SCHEMA_JS_GATE_NC)/in-tsc/tsc.ratchet.log || { cat $(SCHEMA_JS_GATE_NC)/in-tsc/tsc.ratchet.log; echo 'NEGATIVE CONTROL FAILED: the per-code ratchet did not reject the new TS2741 code'; exit 1; }; \
	cp $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE); \
	cmp -s $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE); \
	$(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-eslint SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/out-eslint > $(SCHEMA_JS_GATE_NC)/out-eslint.log 2>&1 || { cat $(SCHEMA_JS_GATE_NC)/out-eslint.log; echo 'NEGATIVE CONTROL FAILED: the eslint gate is not green with the plant out'; exit 1; }; \
	$(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-checkjs SCHEMA_JS_GATE_DIRS="$(SCHEMA_JS_GATE_FAST_DIRS)" SCHEMA_JS_GATE_OUT=$(SCHEMA_JS_GATE_NC)/out-tsc > $(SCHEMA_JS_GATE_NC)/out-tsc.log 2>&1 || { cat $(SCHEMA_JS_GATE_NC)/out-tsc.log; echo 'NEGATIVE CONTROL FAILED: the checkJs ratchet is not green with the plant out'; exit 1; }; \
	if grep -Fq 'SchemaJsGatePlant' $(SCHEMA_JS_GATE_NC)/out-tsc/tsc.log || grep -Fq '$(SCHEMA_JS_GATE_PLANT_FILE)('"$$line"',' $(SCHEMA_JS_GATE_NC)/out-tsc/tsc.log; then \
		echo 'NEGATIVE CONTROL FAILED: tsc still names the plant after it was taken out'; exit 1; fi
	@cmp -s $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE) || { echo 'NEGATIVE CONTROL FAILED: the plant file was not restored'; exit 1; }
	@echo 'schema-js gate negative control: the plant in generated/js/Degenerate.js (header kept) is red on eslint no-var, eqeqeq and no-dupe-keys and on tsc TS2741 at its line; with it out the file is restored, eslint is green and tsc no longer names it'
