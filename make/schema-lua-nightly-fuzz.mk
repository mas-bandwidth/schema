# make/schema-lua-nightly-fuzz.mk — the LUA port's nightly fuzzing leg
# (schema-lua-nightly-fuzz). Included by the Makefile's wildcard include; the
# Makefile needs no edit.
#
# THE SUBJECT: the generated readers of every table in the corpus set named in
# test/lua-fuzz/corpus.txt. That file lists the schema directories the
# Makefile's own tables_generate function reads — the repository's test-schema
# corpus, not a second one — and the harness generates each unit with
# build/schema-lua, the lua-tagged compiler make/lua.mk builds.
#
# THE FOUR READER KINDS, one entry point each in test/lua-fuzz/fuzz.lua:
# load (runtime/load.lua L.Load, the tolerant form-1 load), strict
# (runtime/refusal.lua R.ReadStrict), text (runtime/text.lua Text.LoadText,
# the base64 text body) and json (runtime/json.lua J.FromJson, the JSON text
# form). The oracle is: no crash, no hang, no assert; a refusal is a clean
# typed error (refusal.lua's names), never a raw Lua error and never a silent
# success.
#
# THE FUZZER IS test/lua-fuzz/fuzz.lua: a seeded mutation fuzzer (xorshift64*,
# Lua 5.4 integers), so a finding records its exact bytes and replays without a
# clock or a PRNG. It is pinned with the interpreter: PUC-Rio Lua 5.4.8, the
# pin make/lua.mk carries (LUA_VERSION / LUA_SHA256); LUA_FUZZ_HARNESS_REV is
# the fuzz.lua/harness revision, bumped by hand when the fuzzer's mutation or
# oracle changes.
LUA_FUZZ_HARNESS_REV := 1
LUA_FUZZ_SECONDS ?= 600
LUA_FUZZ_SEED    ?= 1
LUA_FUZZ_FINDINGS ?= build/lua-fuzz/findings
LUA_FUZZ_CORPUS  := test/lua-fuzz/corpus.txt
LUA_FUZZ_REGRESS := test/lua-fuzz/corpus
LUA_FUZZ_GENERATED := build/lua-fuzz/generated
LUA_FUZZ_SCRIPT  := test/lua-fuzz/fuzz.lua

# the pinned build when `make dist-lua` has populated it; otherwise the fleet
# Lua 5.4. The version is still 5.4, and the probe refuses by name when neither
# resolves.
LUA_FUZZ_LUA ?= $(if $(wildcard $(LUA)),$(LUA),$(shell command -v lua5.4 2>/dev/null || command -v lua 2>/dev/null))
LUA_FUZZ_SEEDS := --wire-seeds test/lua-fuzz/seeds:testdata/wire/tables:testdata/conformance/tables:internal/fuzz/testdata \
                  --json-seeds testdata/conformance/tables/json \
                  --text-seeds testdata/conformance/text

.PHONY: schema-lua-fuzz-toolchain
schema-lua-fuzz-toolchain:
	@if [ -z "$(LUA_FUZZ_LUA)" ] || [ ! -x "$(LUA_FUZZ_LUA)" ]; then \
		echo "tool missing: lua $(LUA_VERSION) (pin LUA=$(LUA), no lua5.4 on PATH)"; \
		exit 1; \
	fi
	@echo "schema-lua-fuzz: Lua $(LUA_VERSION) $(LUA_FUZZ_LUA) (harness rev $(LUA_FUZZ_HARNESS_REV))"

# one generated unit per corpus line; the tag-built compiler is the port's own
# emitter, so the fuzzer and the gate cannot disagree about what was generated.
$(LUA_FUZZ_GENERATED)/.stamp: build/schema-lua $(LUA_FUZZ_CORPUS) $(LUA_FUZZ_SCRIPT) make/schema-lua-nightly-fuzz.mk
	@rm -rf $(LUA_FUZZ_GENERATED) && mkdir -p $(LUA_FUZZ_GENERATED)
	@while read -r key dir; do \
		case "$$key" in ''|\#*) continue ;; esac; \
		if ! ./build/schema-lua generate --lang lua --out $(LUA_FUZZ_GENERATED)/$$key $$dir > $(LUA_FUZZ_GENERATED)/$$key.log 2>&1; then \
			rm -rf $(LUA_FUZZ_GENERATED)/$$key; \
			mv $(LUA_FUZZ_GENERATED)/$$key.log $(LUA_FUZZ_GENERATED)/$$key.refused; \
			echo "schema-lua-fuzz: $$key refused: $$(head -1 $(LUA_FUZZ_GENERATED)/$$key.refused)"; \
			continue; \
		fi; \
		rm -f $(LUA_FUZZ_GENERATED)/$$key.log; \
	done < $(LUA_FUZZ_CORPUS)
	@echo "schema-lua-fuzz: $$(ls -d $(LUA_FUZZ_GENERATED)/*/ | wc -l | tr -d ' ') units generated from $(LUA_FUZZ_CORPUS)"
	@touch $@

# THE NIGHTLY TARGET: the time budget is the argument (default ten minutes).
.PHONY: schema-lua-nightly-fuzz
schema-lua-nightly-fuzz: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	@rm -rf $(LUA_FUZZ_FINDINGS) && mkdir -p $(LUA_FUZZ_FINDINGS)
	$(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --seconds $(LUA_FUZZ_SECONDS) \
		--seed $(LUA_FUZZ_SEED) --findings $(LUA_FUZZ_FINDINGS) $(LUA_FUZZ_SEEDS)
	@if [ -n "$$(ls $(LUA_FUZZ_FINDINGS)/*.case 2>/dev/null)" ]; then \
		echo "schema-lua-nightly-fuzz: findings under $(LUA_FUZZ_FINDINGS):"; \
		ls $(LUA_FUZZ_FINDINGS)/*.case; exit 1; \
	fi

# THE FAST PARTS: replay every saved input, no clock, no PRNG. This is what the
# pull-request lane runs.
.PHONY: schema-lua-fuzz-replay
schema-lua-fuzz-replay: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	$(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --replay $(LUA_FUZZ_REGRESS)

# THE PLANTED-BAD-PATTERN CONTROL. A scratch copy of the generated tree has the
# short-file guard in runtime/load.lua's open removed (`if #bytes < 9` becomes
# `if false`), which turns a truncated body into a raw string.unpack error.
# The fuzzer must find it inside a bounded run, the saved
# case must then FAIL replay against the sabotage, and the same case must PASS
# replay against the clean tree — so the control shows both the red line and
# the absence of a false red on the port.
.PHONY: schema-lua-fuzz-negative-control
schema-lua-fuzz-negative-control: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	@set -eu; \
	nc=build/lua-fuzz-negative-control; \
	rm -rf $$nc && mkdir -p $$nc/generated $$nc/findings; \
	cp -r $(LUA_FUZZ_GENERATED)/. $$nc/generated/; \
	sed -i 's/if #bytes < 9 then report.malformed = true; return nil end/if false then report.malformed = true; return nil end/' $$nc/generated/examples/load.lua; \
	grep -q 'if false then report.malformed = true; return nil end' $$nc/generated/examples/load.lua || \
		{ echo "negative control: the plant did not land"; exit 1; }; \
	if ! $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $$nc/generated \
		--corpus $(LUA_FUZZ_CORPUS) --iterations 4000 --seed 7 \
		--findings $$nc/findings --wire-seeds test/lua-fuzz/seeds \
		--json-seeds testdata/conformance/tables/json > $$nc/fuzz.log 2>&1; then \
		: ; \
	else \
		echo "NEGATIVE CONTROL FAILED: the planted missing bound survived 4000 iterations"; \
		cat $$nc/fuzz.log; exit 1; \
	fi; \
	case_file=$$(ls $$nc/findings/*.case 2>/dev/null | head -1); \
	[ -n "$$case_file" ] || { echo "NEGATIVE CONTROL FAILED: no saved input"; cat $$nc/fuzz.log; exit 1; }; \
	cp "$$case_file" $(LUA_FUZZ_REGRESS)/planted_missing_bound.case; \
	if $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $$nc/generated \
		--corpus $(LUA_FUZZ_CORPUS) --replay $$nc/findings > $$nc/replay-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the saved input did not fail replay on the sabotage"; \
		cat $$nc/replay-sabotage.log; exit 1; \
	fi; \
	if ! $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --replay $(LUA_FUZZ_REGRESS) > $$nc/replay-clean.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a clean tree failed the same saved input"; \
		cat $$nc/replay-clean.log; exit 1; \
	fi; \
	echo "negative control: removing the reader's length bound turns the Lua fuzz oracle RED and the saved input fails replay; the clean tree stays green"
