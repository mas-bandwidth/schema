# make/schema-lua-generated-luacheck-gate.mk — the Lua leg's strict-checker
# lens (ideas roadmap group schema-per-language-lenses). Included by the
# Makefile's wildcard include; the Makefile itself names no language.
#
# THE CORPUS IS THE MAKEFILE'S OWN. The directories and files below are the
# ones the Makefile's generate targets read (SCHEMAS, SCHEMAS128, SCHEMAS_WIDE,
# SCHEMAS_BENCH, the SCHEMAS_TABLES_* family, and test/tables/*.schema, whose
# per-file generation the tables_generate macro names). No second corpus is
# invented here: this file only changes the target language to lua.
#
# THE CHECKER IS PINNED. luacheck 1.2.0 is the release this gate is written
# against and the version the fleet's toolchain installs. The target probes for
# it by name and version and refuses with "tool missing: luacheck" rather than
# passing with nothing run (issue #599's rule).
LUACHECK_VERSION := 1.2.0
LUACHECK ?= luacheck

LUA_LUACHECK_CORPUS := build/lua-luacheck-corpus
LUA_LUACHECK_CONFIG := build/lua-luacheck.rc

# every schema directory a Makefile generate target reads, plus the per-file
# units under test/tables (the tables_generate macro names each file) and the
# two-file guard unit.
LUA_LUACHECK_DIRS := examples examples128 examples-wide bench/corpus \
	tables/examples tables/pointers tables/block tables/blockhome \
	tables/messages tables/stream tables/blobs tables/scalars tables/maps \
	tables/lists tables/arms tables/backend tables/vocab tables/vocab9
LUA_LUACHECK_FILES := $(wildcard test/tables/*.schema) $(wildcard test/guard/*.schema)
LUA_LUACHECK_UNITS := $(LUA_LUACHECK_DIRS) $(LUA_LUACHECK_FILES)
# A directory's mtime does not change when an existing schema file is edited.
# Track its member files so a local gate cannot reuse stale generated Lua.
LUA_LUACHECK_SOURCES := $(foreach dir,$(LUA_LUACHECK_DIRS),$(wildcard $(dir)/*.schema)) $(LUA_LUACHECK_FILES)

.PHONY: toolchain-luacheck
toolchain-luacheck:
	@if ! command -v $(LUACHECK) >/dev/null 2>&1; then \
		echo "tool missing: luacheck"; \
		echo "  the Lua luacheck gate needs luacheck $(LUACHECK_VERSION) from its official source"; \
		exit 1; \
	fi
	@v=$$($(LUACHECK) --version 2>/dev/null | sed -n 's/^Luacheck: //p'); \
	if [ "$$v" != "$(LUACHECK_VERSION)" ]; then \
		echo "tool missing: luacheck $(LUACHECK_VERSION) (found '$$v')"; \
		exit 1; \
	fi

# THE CONFIG, AND EVERY IGNORE IN IT. Strict means luacheck's own default set,
# not a hand-picked subset. The four below are the only suppressions, each one
# a shape the emitter produces on purpose rather than a defect:
#
#   max_line_length = false   a descriptor row is one declaration per line;
#                             wrapping it would make the data unreadable.
#   212 unused argument       every emitted reader/writer keeps its fixed
#                             (stream, value) signature even in an arm that
#                             reads neither; the caller shape is the contract.
#   213 unused loop variable  generated positional loops spell the index even
#                             when the body does not read it.
#   581 negated condition     generated guards are written `if not x then`.
#
# Real defects are NOT in this list. W111/W113 (an undefined global), W211
# (dead local), W311/W412 (a value or argument silently replaced) and W542 (an
# empty branch) stay visible, which is why the gate can go red on this corpus.
#
# THE TRIAGE LEDGER. The prior attempt reported 977 diagnostics. The first
# exact 415a0637 Linux run reported 50 warnings over 326 distinct files:
# W211(19), W311(7), W113(7), W542(6), W411(5), W111(3), W431(2),
# W412(1). The permitted source repair follows those 50 lines. A fresh gate at
# this tip (the attempt's commits re-carried onto base 6dbb9ef6f) is green:
# 0 warnings / 0 errors over 324 distinct files (131 units generated, 9 refused
# by name), and the negative control names the planted dead local. Measured on
# the Linux bench: cold corpus build + check 30 s, warm 2 s; luacheck itself is
# seconds-scale.
# The serialize*.lua files are pinned
# vendored runtime: any warning there needs a justified per-file override and
# a named follow-up FIX-CARD, never an edit to the vendored bytes.
# FIX-CARD: serialize_bytes.lua — drop unused string_format import in upstream vendor source, then update SERIALIZE_PIN.
# FIX-CARD: serialize_compressed_float.lua — remove unused derive results and precomputed min, then update SERIALIZE_PIN.
# FIX-CARD: serialize_float.lua — drop unused type import and rename mantissa M to avoid module M shadow, then update SERIALIZE_PIN.
$(LUA_LUACHECK_CONFIG): make/schema-lua-generated-luacheck-gate.mk
	@mkdir -p $(dir $@)
	@printf '%s\n' \
		'std = "lua54"' \
		'max_line_length = false' \
		'ignore = { "212", "213", "581" }' \
		'-- Vendored serialize bytes: unused string_format import, pinned by SERIALIZE_PIN.' \
		'files["build/lua-luacheck-corpus/unique/*-serialize_bytes.lua"] = { ignore = { "212", "213", "581", "211/string_format" } }' \
		'-- Vendored compressed float: unused derivation results and precomputed min, pinned by SERIALIZE_PIN.' \
		'files["build/lua-luacheck-corpus/unique/*-serialize_compressed_float.lua"] = { ignore = { "212", "213", "581", "211/maxInt", "211/delta", "211/mn" } }' \
		'-- Vendored float: unused type alias and intentional mantissa M shadow of module M, pinned by SERIALIZE_PIN.' \
		'files["build/lua-luacheck-corpus/unique/*-serialize_float.lua"] = { ignore = { "212", "213", "581", "211/type", "431/M" } }' \
		> $@

# generate the corpus exactly as the other legs generate theirs: one directory
# (or one schema file) per unit, a refusal kept by name in <unit>.refused, and
# the count printed. A refused unit is not checked, and it says so.
$(LUA_LUACHECK_CORPUS)/.stamp: build/schema-lua make/schema-lua-generated-luacheck-gate.mk $(LUA_LUACHECK_UNITS) $(LUA_LUACHECK_SOURCES)
	@rm -rf $(LUA_LUACHECK_CORPUS) && mkdir -p $(LUA_LUACHECK_CORPUS)
	@set -e; ok=0; refused=0; \
	for u in $(LUA_LUACHECK_UNITS); do \
		key=$$(printf '%s' "$$u" | sed 's#[/.]#_#g'); \
		log=$(LUA_LUACHECK_CORPUS)/$$key.log; \
		if ./build/schema-lua generate --lang lua --out $(LUA_LUACHECK_CORPUS)/$$key $$u > $$log 2>&1; then \
			rm -f $$log; ok=$$((ok + 1)); \
		else \
			rm -rf $(LUA_LUACHECK_CORPUS)/$$key; \
			mv $$log $(LUA_LUACHECK_CORPUS)/$$key.refused; \
			refused=$$((refused + 1)); \
			echo "luacheck-lua: $$u refused: $$(head -1 $(LUA_LUACHECK_CORPUS)/$$key.refused)"; \
		fi; \
	done; \
	echo "luacheck-lua corpus: $$ok units generated, $$refused refused by name"
	@mkdir -p $(LUA_LUACHECK_CORPUS)/unique; \
	seen=$(LUA_LUACHECK_CORPUS)/.seen; : > $$seen; n=0; \
	for f in $$(find $(LUA_LUACHECK_CORPUS) -name '*.lua' | sort); do \
		h=$$(sha256sum "$$f" | cut -d' ' -f1); \
		if grep -qx "$$h" $$seen; then continue; fi; \
		echo "$$h" >> $$seen; n=$$((n + 1)); \
		cp "$$f" "$(LUA_LUACHECK_CORPUS)/unique/$$n-$$(basename $$f)"; \
	done; \
	rm -f $$seen; \
	echo "luacheck-lua corpus: $$n distinct files (the runtime repeats in every unit and is checked once)"
	@touch $@

# THE GATE. One run over the whole generated corpus and the runtime emitted
# beside it, with the config above.
.PHONY: tables-lua-luacheck
tables-lua-luacheck: toolchain-luacheck $(LUA_LUACHECK_CORPUS)/.stamp $(LUA_LUACHECK_CONFIG)
	@$(LUACHECK) --config $(LUA_LUACHECK_CONFIG) --no-color --codes $(LUA_LUACHECK_CORPUS)/unique
	@echo "luacheck-lua gate: the generated Lua and its runtime are clean at luacheck $(LUACHECK_VERSION)"

# THE NEGATIVE CONTROL (the repository's negative-control convention): the
# gate must go red on a planted dead local and name it, and be green with the
# plant removed. The plant is the nearest neighbour of a real W211 finding:
# `local lua_luacheck_plant = 1`, appended to a runtime file the corpus already
# emits. Nothing tracked is written to.
.PHONY: tables-lua-luacheck-negative-control
tables-lua-luacheck-negative-control: toolchain-luacheck $(LUA_LUACHECK_CORPUS)/.stamp $(LUA_LUACHECK_CONFIG)
	@rm -rf build/lua-luacheck-control && mkdir -p build/lua-luacheck-control
	@set -e; clean=$$(find $(LUA_LUACHECK_CORPUS) -name serialize_int.lua | head -1); \
	if [ -z "$$clean" ]; then echo "NEGATIVE CONTROL: the corpus has no serialize_int.lua"; exit 1; fi; \
	cp "$$clean" build/lua-luacheck-control/clean.lua; \
	if ! $(LUACHECK) --config $(LUA_LUACHECK_CONFIG) --no-color build/lua-luacheck-control/clean.lua > build/lua-luacheck-control/clean.log 2>&1; then \
		echo "NEGATIVE CONTROL: the unplanted control file is already red"; cat build/lua-luacheck-control/clean.log; exit 1; \
	fi; \
	sed -i -e 's/^return M$$/local lua_luacheck_plant = 1/' -e 's/^return _M$$/local lua_luacheck_plant = 1/' build/lua-luacheck-control/clean.lua; \
	grep -q lua_luacheck_plant build/lua-luacheck-control/clean.lua || { echo "NEGATIVE CONTROL: the plant did not apply"; exit 1; }; \
	if $(LUACHECK) --config $(LUA_LUACHECK_CONFIG) --no-color build/lua-luacheck-control/clean.lua > build/lua-luacheck-control/plant.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted dead local left luacheck green"; exit 1; \
	fi; \
	grep -q lua_luacheck_plant build/lua-luacheck-control/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: luacheck did not name the plant"; cat build/lua-luacheck-control/plant.log; exit 1; }
	@grep -n lua_luacheck_plant build/lua-luacheck-control/plant.log
	@echo "luacheck-lua negative control: a planted dead local is named and turns the gate red"
