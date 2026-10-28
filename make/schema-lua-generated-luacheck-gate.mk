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

# THE CONFIG IS LUACHECK'S DEFAULT SET, WHOLE, AT ZERO WARNINGS. std = "lua54"
# names the pinned interpreter's globals; nothing else is set for the emitted
# code or the table runtime: no ignore list, no line-length allowance, no
# pinned count of tolerated findings. A finding is fixed in the generator
# (internal/codegen/lua, internal/codegen/luatable) or in the runtime it
# emits, never here: a
# parameter a fixed signature never reads is spelled with luacheck's own
# "deliberately unread" underscore, and lua.Wrap reflows every emitted line to
# luacheck's 120 (W631).
#
# THE VENDORED serialize.lua RUNTIME (internal/codegen/lua/runtime/serialize*.lua,
# pinned by SERIALIZE_PIN) is held to its own repository's zero-warning lint,
# the .luacheckrc that serialize.lua's `make lint` gates on, and to nothing
# looser. Its two settings, verbatim in substance and scoped to those files:
#
#   self = false   WriteStream:isWriting() and its siblings answer for the
#                  type and never read self: the shape of the stream
#                  interface (serialize_stream.lua).
#   581            serialize_compressed_float.lua compares as `not (x >= y)`
#                  on purpose: the negated form is true for NaN and luacheck's
#                  suggested rewrite is not, so the rewrite would change
#                  behaviour.
#
# A warning in the vendored bytes is fixed upstream in serialize.lua and
# re-vendored (make vendor-serialize-lua), never edited here or ignored here.
#
# MEASURED (2026-10-28, luacheck 1.2.0 on Lua 5.4.8, macOS): 139 units
# generated, 339 distinct files, 0 warnings in the emitted code and the table
# runtime. The serialize.lua v2.1.1 runtime is 0 warnings under the two
# settings above; an older pin carries the unused locals 2.1 removed upstream.
$(LUA_LUACHECK_CONFIG): make/schema-lua-generated-luacheck-gate.mk
	@mkdir -p $(dir $@)
	@printf '%s\n' \
		'std = "lua54"' \
		'-- The vendored serialize.lua runtime, under its own .luacheckrc (see make/schema-lua-generated-luacheck-gate.mk).' \
		'files["$(LUA_LUACHECK_CORPUS)/unique/*-serialize_stream.lua"] = { self = false }' \
		'files["$(LUA_LUACHECK_CORPUS)/unique/*-serialize_compressed_float.lua"] = { ignore = { "581" } }' \
		> $@

# generate the corpus exactly as the other legs generate theirs: one directory
# (or one schema file) per unit, a refusal kept by name in <unit>.refused, and
# the count printed. A refused unit is not checked, and it says so.
$(LUA_LUACHECK_CORPUS)/.stamp: bin/schema make/schema-lua-generated-luacheck-gate.mk $(LUA_LUACHECK_UNITS) $(LUA_LUACHECK_SOURCES)
	@rm -rf $(LUA_LUACHECK_CORPUS) && mkdir -p $(LUA_LUACHECK_CORPUS)
	@set -e; ok=0; refused=0; \
	for u in $(LUA_LUACHECK_UNITS); do \
		key=$$(printf '%s' "$$u" | sed 's#[/.]#_#g'); \
		log=$(LUA_LUACHECK_CORPUS)/$$key.log; \
		if ./bin/schema generate --lang lua --out $(LUA_LUACHECK_CORPUS)/$$key $$u > $$log 2>&1; then \
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

# THE FORMATTER, PINNED. StyLua is the Lua formatting authority this gate holds
# the generated Lua to, the way clang-format is the C leg's (make/c.mk,
# tables-c-clean): the pinned release, fetched from its official GitHub release
# at the sha256 named per platform below into dist/ (gitignored), exactly as
# dist-lua fetches the interpreter; any other sha256 is refused. The style is
# make/lua-stylua.toml.
STYLUA_VERSION := 2.5.2
STYLUA_DIR     := dist/stylua-$(STYLUA_VERSION)
STYLUA         ?= $(CURDIR)/$(STYLUA_DIR)/stylua
STYLUA_CONFIG  := make/lua-stylua.toml
STYLUA_PLATFORM := $(if $(filter Darwin,$(shell uname -s)),macos,linux)-$(if $(filter arm64 aarch64,$(shell uname -m)),aarch64,x86_64)
STYLUA_SHA256_linux-x86_64  := bcb0d855e91f102f28a370e850f8566b3b44b79e6274d806ea5246837c0fd5ab
STYLUA_SHA256_linux-aarch64 := 0ef2ebf0b7e5a652b65c4cb96c6d9ffb3981a98547de3c764465bbf54a8d761a
STYLUA_SHA256_macos-x86_64  := 53c50a1605d0a6345d160a1a5a21db40bcf2bf9cd23c17f7c277a63a1bff3a7f
STYLUA_SHA256_macos-aarch64 := 92ff0889e16324801bc072692974bb67f8161e62010fc90f96c62a17f81f32c7
STYLUA_ZIP     := stylua-$(STYLUA_VERSION)-$(STYLUA_PLATFORM).zip
STYLUA_URL     := https://github.com/JohnnyMorganz/StyLua/releases/download/v$(STYLUA_VERSION)/stylua-$(STYLUA_PLATFORM).zip

.PHONY: dist-stylua
dist-stylua:
	@mkdir -p dist
	@if [ ! -f dist/$(STYLUA_ZIP) ]; then curl -fsSL -o dist/$(STYLUA_ZIP).part $(STYLUA_URL) && mv dist/$(STYLUA_ZIP).part dist/$(STYLUA_ZIP); fi
	@got=$$(sha256sum dist/$(STYLUA_ZIP) 2>/dev/null | cut -d" " -f1); \
	if [ -z "$$got" ]; then got=$$(shasum -a 256 dist/$(STYLUA_ZIP) | cut -d" " -f1); fi; \
	if [ "$$got" != "$(STYLUA_SHA256_$(STYLUA_PLATFORM))" ]; then \
		echo "dist-stylua REFUSES: dist/$(STYLUA_ZIP) has sha256 $$got, the pin is $(STYLUA_SHA256_$(STYLUA_PLATFORM))"; \
		echo "  delete dist/$(STYLUA_ZIP) and run make dist-stylua again; a release that does not match the pin is never run"; \
		exit 1; fi
	@rm -rf $(STYLUA_DIR) && mkdir -p $(STYLUA_DIR) && unzip -q -o dist/$(STYLUA_ZIP) -d $(STYLUA_DIR) && chmod +x $(STYLUA_DIR)/stylua
	@v=$$($(STYLUA_DIR)/stylua --version); if [ "$$v" != "stylua $(STYLUA_VERSION)" ]; then \
		echo "dist-stylua REFUSES: the pinned release reports '$$v'"; exit 1; fi
	@echo "dist-stylua: StyLua $(STYLUA_VERSION) $(STYLUA_PLATFORM) sha256 $(STYLUA_SHA256_$(STYLUA_PLATFORM))"

# THE FORMAT GATE: every file of the generated corpus above is exactly what
# the pinned StyLua writes for it (--check: a diff is a failure, printed). The
# generator writes this style itself (internal/codegen/lua/format.go and the
# emitters), and the table runtime is committed in it, so there is no
# allowance file and nothing is reformatted here. The vendored serialize.lua
# runtime (the *-serialize*.lua copies) is upstream's bytes pinned by sha256 in
# SERIALIZE_PIN and is not this repository's to restyle; it is the only thing
# the gate does not read, by name.
.PHONY: tables-lua-format
tables-lua-format: dist-stylua $(LUA_LUACHECK_CORPUS)/.stamp
	@files="$$(find $(LUA_LUACHECK_CORPUS)/unique -name '*.lua' ! -name '*-serialize*.lua' | sort)"; \
	n=$$(printf '%s\n' "$$files" | wc -l | tr -d ' '); \
	if ! $(STYLUA) --config-path $(STYLUA_CONFIG) --check $$files; then \
		echo "tables-lua-format: the generated Lua above is not what StyLua $(STYLUA_VERSION) writes ($(STYLUA_CONFIG)); fix the generator"; \
		exit 1; \
	fi; \
	echo "lua-format gate: $$n generated files are StyLua $(STYLUA_VERSION)-canonical"

# THE FORMAT GATE's NEGATIVE CONTROL: one generated module, copied, with its
# first one-line early-out spelled as a block (the shape StyLua collapses),
# must fail --check and be named; the unplanted copy must pass.
.PHONY: tables-lua-format-negative-control
tables-lua-format-negative-control: dist-stylua $(LUA_LUACHECK_CORPUS)/.stamp
	@rm -rf build/lua-format-control && mkdir -p build/lua-format-control
	@set -e; clean=$$(grep -l '^  if not stream:serialize[A-Za-z0-9]*(BITS, [0-9]*) then return false end$$' $(LUA_LUACHECK_CORPUS)/unique/*.lua | head -1); \
	if [ -z "$$clean" ]; then echo "NEGATIVE CONTROL: the corpus has no one-line early-out to plant"; exit 1; fi; \
	cp "$$clean" build/lua-format-control/clean.lua; \
	if ! $(STYLUA) --config-path $(STYLUA_CONFIG) --check build/lua-format-control/clean.lua > build/lua-format-control/clean.log 2>&1; then \
		echo "NEGATIVE CONTROL: the unplanted control file is already red"; cat build/lua-format-control/clean.log; exit 1; fi; \
	awk 'done == 0 && /^  if not stream:serialize[A-Za-z0-9]*\(BITS, [0-9]*\) then return false end$$/ { sub(/ return false end$$/, ""); print; print "    return false"; print "  end"; done = 1; next } { print }' \
		build/lua-format-control/clean.lua > build/lua-format-control/planted.lua; \
	if cmp -s build/lua-format-control/clean.lua build/lua-format-control/planted.lua; then echo "NEGATIVE CONTROL: the plant did not apply"; exit 1; fi; \
	if $(STYLUA) --config-path $(STYLUA_CONFIG) --check build/lua-format-control/planted.lua > build/lua-format-control/planted.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted block left the format gate green"; exit 1; fi; \
	grep -q 'planted.lua' build/lua-format-control/planted.log || { \
		echo "NEGATIVE CONTROL FAILED: the format gate did not name the planted file"; cat build/lua-format-control/planted.log; exit 1; }
	@echo "lua-format negative control: a non-canonical early-out is named and turns the gate red"
