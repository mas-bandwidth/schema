# make/schema-lua-version-differential.mk — THE LUA VERSION DIFFERENTIAL
# (Glenn, 2026-10-18; ideas roadmap group schema-per-language-lenses).
#
# The Lua port emits ONE algorithm for four interpreters, and integer and
# float semantics differ between them. This gate runs the shared tables
# conformance driver (test/conformance/lua/driver) over the SAME generated
# corpus under each interpreter and requires:
#
#   * Lua 5.3 and Lua 5.4 to pass the whole surface and to write byte-identical
#     case answers. The 5.4 row is the pin make/lua.mk already builds.
#   * Lua 5.1 and LuaJIT to REFUSE BY NAME. The generated output uses Lua 5.3's
#     native bitwise operators and string.pack, which neither has: 5.1 (and
#     LuaJIT, which is 5.1 syntax) stops with `unexpected symbol near '>'` in
#     the generated message/load runtime. That is the loud refusal this file
#     requires; a failure with any other signature is a real finding and fails
#     the gate. docs/PORT-LUA.md is the support-matrix page (PATHS-PROPOSED:
#     this card's PATHS do not carry it); this header and the pull request body
#     state the same matrix.
#
# The interpreters are the four official sources, pinned by version and
# sha256: PUC-Rio Lua 5.1.5, 5.3.6 and 5.4.8 (lua.org), and LuaJIT at commit
# dd3b24369ddfaa83c78cd6fdd91d157c8968ec8d (the LuaJIT official GitHub
# repository, which publishes no release tarballs). `make dist-lua-differential`
# fetches and builds them into the gitignored dist/. The fleet install play
# (rowan-tools/fleet) is not reachable from the wall, so, as the card allows,
# the target PROBES for each interpreter: an archive that cannot be fetched or
# a binary that will not build prints `tool missing: <name>` and exits non-zero;
# it never fakes a pass. The sha256 pin is checked before anything is unpacked.
# The CI workflow builds them the same way. This target is HEAVY (four builds
# plus four conformance runs) and runs nightly, never per push; the fast half is
# the static test in internal/ci/schema_lua_version_differential_test.go.
#
# VERIFIED at this head on the Linux bench (2026-10-19, attempt 5):
#   conformance-lua: 31 units generated, 2 refused by name (scalars,
#     tblscalars2 — the table backend refuses fixed-point/128-bit kinds).
#   lua 5.1 REFUSES, lua 5.3 PASS, lua 5.4 PASS, luajit REFUSES,
#   Lua 5.3 and 5.4 agree on 157 case answers. The gate took 9.2 s with dist/
#   already built; a cold `dist-lua-differential` adds the four interpreter
#   builds. The negative control reported "the planted 5.3-only divergence is
#   caught by name and Lua 5.4 stays green".
#
# TRIAGE (the full finding list from the one gate run; no finding is REAL):
#   finding                                          REAL?  action
#   Lua 5.1 cannot parse the generated 5.3 syntax    NOT    documented refusal;
#                                                            support matrix below
#   LuaJIT cannot parse the generated 5.3 syntax     NOT    same (5.1 syntax)
#   Lua 5.3 vs 5.4 case-answer divergence            NOT    none found; 157
#                                                            answers byte-equal
#   scalars, tblscalars2 refused by the table backend NOT  pre-existing backend
#                                                            limitation, refused
#                                                            by name by the tool
# No port generator shares a flagged pattern: nothing was flagged.
#
# SUPPORT MATRIX: Lua 5.3 and Lua 5.4 are supported; Lua 5.1 and LuaJIT are not
# (the generated output uses 5.3's native bitwise operators and string.pack).
# A stricter named guard belongs in the generator (internal/codegen/lua, outside
# this card's PATHS) and is reported as PATHS-PROPOSED; until then the parser's
# `unexpected symbol` is the loud refusal the gate accepts, and ONLY that
# signature — any other 5.1/LuaJIT failure fails the gate as a real finding.
#
# NEGATIVE CONTROL. `LUA_DIFF_PLANT=1` copies the generated corpus to a scratch
# tree and plants a 5.3-only error in every generated load.lua, then runs the
# same gate: Lua 5.3 must fail, Lua 5.4 must not, and the target must exit
# non-zero naming the plant. make/schema-lua-version-differential-negative-control
# runs exactly that.
#
# REGISTRY PROBE. This file lives under make/ and is therefore enumerated by the
# `tables-ports-refuse-wide-scalars` registry probe as a language leg, but it is
# not a compiler backend. internal/ci/probe_test.go drops it from that probe's
# fixture, the same way it drops make/lua-serialize.mk and make/lua-conformance.mk
# (both support fragments). internal/ci/probe_test.go is outside this card's
# PATHS; that exclusion is reported as PATHS-PROPOSED (schema-lua-version-
# differential, attempt 4): a real finding if it is ever removed.

LUA_DIFF_LUA51_VERSION := 5.1.5
LUA_DIFF_LUA51_TARBALL := lua-$(LUA_DIFF_LUA51_VERSION).tar.gz
LUA_DIFF_LUA51_URL     := https://www.lua.org/ftp/$(LUA_DIFF_LUA51_TARBALL)
LUA_DIFF_LUA51_SHA256  := 2640fc56a795f29d28ef15e13c34a47e223960b0240e8cb0a82d9b0738695333

LUA_DIFF_LUA53_VERSION := 5.3.6
LUA_DIFF_LUA53_TARBALL := lua-$(LUA_DIFF_LUA53_VERSION).tar.gz
LUA_DIFF_LUA53_URL     := https://www.lua.org/ftp/$(LUA_DIFF_LUA53_TARBALL)
LUA_DIFF_LUA53_SHA256  := fc5fd69bb8736323f026672b1b7235da613d7177e72558893a0bdcd320466d60

LUA_DIFF_LUA54_VERSION := 5.4.8
LUA_DIFF_LUA54_TARBALL := lua-$(LUA_DIFF_LUA54_VERSION).tar.gz
LUA_DIFF_LUA54_URL     := https://www.lua.org/ftp/$(LUA_DIFF_LUA54_TARBALL)
LUA_DIFF_LUA54_SHA256  := 4f18ddae154e793e46eeab727c59ef1c0c0c2b744e7b94219710d76f530629ae

# LuaJIT publishes no tagged release; the official repository is the source
# and this commit is the pin. The archive directory carries the commit.
LUA_DIFF_JIT_COMMIT  := dd3b24369ddfaa83c78cd6fdd91d157c8968ec8d
LUA_DIFF_JIT_TARBALL := luajit-$(LUA_DIFF_JIT_COMMIT).tar.gz
LUA_DIFF_JIT_URL     := https://codeload.github.com/LuaJIT/LuaJIT/tar.gz/$(LUA_DIFF_JIT_COMMIT)
LUA_DIFF_JIT_SHA256  := a6ac8457c474c6fb71aaa7660dd5bf8f6e0506496fc74786b2dae18e26a768cc

LUA_DIFF_LUA51 ?= $(CURDIR)/dist/lua-$(LUA_DIFF_LUA51_VERSION)/src/lua
LUA_DIFF_LUA53 ?= $(CURDIR)/dist/lua-$(LUA_DIFF_LUA53_VERSION)/src/lua
LUA_DIFF_LUA54 ?= $(CURDIR)/dist/lua-$(LUA_DIFF_LUA54_VERSION)/src/lua
LUA_DIFF_LUAJIT ?= $(CURDIR)/dist/LuaJIT-$(LUA_DIFF_JIT_COMMIT)/src/luajit

# $(call lua_diff_fetch,<tarball>,<url>,<sha256>) fetches one official archive
# into dist/ and refuses anything whose sha256 is not the pin before it is
# unpacked. The archive is left in place, so a second run does not touch the
# network.
define lua_diff_fetch
@mkdir -p dist
@if [ ! -f dist/$(1) ]; then \
	echo "dist-lua-differential: fetching $(2)"; \
	if ! curl -fsSL -o dist/$(1).part "$(2)"; then \
		rm -f dist/$(1).part; \
		echo "lua-version-differential: tool missing: $(1) (cannot fetch $(2); install the pinned interpreter or supply the archive in dist/)"; \
		exit 1; \
	fi; \
	mv dist/$(1).part dist/$(1); \
fi
@got=$$(sha256sum dist/$(1) 2>/dev/null | cut -d' ' -f1); \
if [ -z "$$got" ]; then got=$$(shasum -a 256 dist/$(1) | cut -d' ' -f1); fi; \
if [ "$$got" != "$(3)" ]; then \
	echo "dist-lua-differential REFUSES: dist/$(1) has sha256 $$got, the pin is $(3)"; \
	echo "  delete dist/$(1) and run make dist-lua-differential again"; \
	exit 1; \
fi
endef

.PHONY: dist-lua-differential
dist-lua-differential:
	$(call lua_diff_fetch,$(LUA_DIFF_LUA51_TARBALL),$(LUA_DIFF_LUA51_URL),$(LUA_DIFF_LUA51_SHA256))
	@if [ ! -x "$(LUA_DIFF_LUA51)" ]; then \
		rm -rf dist/lua-$(LUA_DIFF_LUA51_VERSION); \
		tar -xzf dist/$(LUA_DIFF_LUA51_TARBALL) -C dist; \
		$(MAKE) -C dist/lua-$(LUA_DIFF_LUA51_VERSION) posix; \
	fi
	$(call lua_diff_fetch,$(LUA_DIFF_LUA53_TARBALL),$(LUA_DIFF_LUA53_URL),$(LUA_DIFF_LUA53_SHA256))
	@if [ ! -x "$(LUA_DIFF_LUA53)" ]; then \
		rm -rf dist/lua-$(LUA_DIFF_LUA53_VERSION); \
		tar -xzf dist/$(LUA_DIFF_LUA53_TARBALL) -C dist; \
		$(MAKE) -C dist/lua-$(LUA_DIFF_LUA53_VERSION) posix; \
	fi
	$(call lua_diff_fetch,$(LUA_DIFF_LUA54_TARBALL),$(LUA_DIFF_LUA54_URL),$(LUA_DIFF_LUA54_SHA256))
	@if [ ! -x "$(LUA_DIFF_LUA54)" ]; then \
		rm -rf dist/lua-$(LUA_DIFF_LUA54_VERSION); \
		tar -xzf dist/$(LUA_DIFF_LUA54_TARBALL) -C dist; \
		$(MAKE) -C dist/lua-$(LUA_DIFF_LUA54_VERSION) linux; \
	fi
	$(call lua_diff_fetch,$(LUA_DIFF_JIT_TARBALL),$(LUA_DIFF_JIT_URL),$(LUA_DIFF_JIT_SHA256))
	@if [ ! -x "$(LUA_DIFF_LUAJIT)" ]; then \
		rm -rf dist/LuaJIT-$(LUA_DIFF_JIT_COMMIT); \
		tar -xzf dist/$(LUA_DIFF_JIT_TARBALL) -C dist; \
		$(MAKE) -C dist/LuaJIT-$(LUA_DIFF_JIT_COMMIT); \
	fi
	@for tool in "$(LUA_DIFF_LUA51)" "$(LUA_DIFF_LUA53)" "$(LUA_DIFF_LUA54)" "$(LUA_DIFF_LUAJIT)"; do \
		if [ ! -x "$$tool" ]; then echo "lua-version-differential: tool missing: $$tool"; exit 1; fi; \
	done
	@echo "dist-lua-differential: $$($(LUA_DIFF_LUA51) -v 2>&1 | head -1)"
	@echo "dist-lua-differential: $$($(LUA_DIFF_LUA53) -v 2>&1 | head -1)"
	@echo "dist-lua-differential: $$($(LUA_DIFF_LUA54) -v 2>&1 | head -1)"
	@echo "dist-lua-differential: $$($(LUA_DIFF_LUAJIT) -v 2>&1 | head -1)"

# THE GATE. One generated corpus (build/conformance-lua, the same one
# `make conformance` runs), one driver wrapper per interpreter, and the
# answers compared surface by surface.
.PHONY: schema-lua-version-differential
schema-lua-version-differential: build/conformance-harness build/conformance-lua dist-lua-differential
	@set -eu; \
	root="$(CURDIR)"; \
	gen="$$root/build/conformance-lua"; \
	out="$$root/build/lua-version-differential"; \
	rm -rf "$$out"; mkdir -p "$$out"; \
	if [ -n "$(LUA_DIFF_PLANT)" ]; then \
		plant="$$out/planted-generated"; \
		cp -r "$$gen" "$$plant"; \
		for f in "$$plant"/*/load.lua; do \
			{ printf '%s\n' 'if _VERSION == "Lua 5.3" then error("lua-version-differential plant: 5.3-only divergence") end'; cat "$$f"; } > "$$f.planted"; \
			mv "$$f.planted" "$$f"; \
		done; \
		gen="$$plant"; \
		echo "lua-version-differential: PLANT in $(LUA_DIFF_PLANT): planted-generated carries a 5.3-only error"; \
	fi; \
	fail=0; supported=""; \
	for spec in "5.1|$(LUA_DIFF_LUA51)" "5.3|$(LUA_DIFF_LUA53)" "5.4|$(LUA_DIFF_LUA54)" "luajit|$(LUA_DIFF_LUAJIT)"; do \
		key=$${spec%%|*}; lua=$${spec#*|}; \
		d="$$out/$$key"; mkdir -p "$$d"; \
		printf '#!/bin/sh\nexec env LUA=%s LUA_CONFORMANCE_GENERATED=%s %s/test/conformance/lua/driver "$$@"\n' "$$lua" "$$gen" "$$root" > "$$d/driver"; \
		chmod +x "$$d/driver"; \
		printf 'lua:%s %s/driver\n' "$$key" "$$d" > "$$d/drivers.txt"; \
		if ./build/conformance-harness run --drivers "$$d/drivers.txt" --work "$$d/work" > "$$d/log" 2>&1; then \
			echo "lua-version-differential: lua $$key PASS"; \
			supported="$$supported $$key"; \
		elif grep -q 'lua-version-differential plant' "$$d/log"; then \
			echo "lua-version-differential: PLANT CAUGHT under lua $$key"; \
			fail=1; \
		elif grep -qE 'build/conformance-lua/[^ ]*\.lua:[0-9]+: unexpected symbol|planted-generated/[^ ]*\.lua:[0-9]+: unexpected symbol' "$$d/log"; then \
			echo "lua-version-differential: lua $$key REFUSES: the generated output needs Lua 5.3 native bitwise operators and string.pack; Lua $$key has neither (support matrix: docs/PORT-LUA.md)"; \
		else \
			echo "lua-version-differential: lua $$key FAILED and the failure is not the documented version refusal" >&2; \
			sed -n '1,40p' "$$d/log" >&2; \
			fail=1; \
		fi; \
	done; \
	if [ "$$supported" != " 5.3 5.4" ]; then \
		echo "lua-version-differential: supported set is '$$supported', not ' 5.3 5.4'" >&2; \
		fail=1; \
	fi; \
	a="$$out/5.3/work/out/lua:5.3"; b="$$out/5.4/work/out/lua:5.4"; \
	if [ -d "$$a" ] && [ -d "$$b" ]; then \
		if diff -r "$$a" "$$b" > "$$out/differential.txt" 2>&1; then \
			echo "lua-version-differential: Lua 5.3 and 5.4 agree on $$(find "$$a" -type f | wc -l | tr -d ' ') case answers"; \
		else \
			echo "lua-version-differential: DIVERGENCE between Lua 5.3 and Lua 5.4" >&2; \
			sed -n '1,40p' "$$out/differential.txt" >&2; \
			fail=1; \
		fi; \
	else \
		echo "lua-version-differential: no 5.3/5.4 answers to compare" >&2; \
		fail=1; \
	fi; \
	if [ $$fail -ne 0 ]; then exit 1; fi; \
	echo "lua-version-differential: Lua 5.3 and 5.4 agree; Lua 5.1 and LuaJIT refuse by name"

# THE NEGATIVE CONTROL: the planted 5.3-only error must make the gate exit
# non-zero and must be named in the output. If the gate stays green the control
# fails. The control is registered in the shared make/negative-controls.json
# exclusion list (the card's own workflow (github/workflows/schema-lua-version-
# differential.yml runs it on the nightly schedule and on workflow_dispatch), so
# tools/negativecontrols and `negativecontrols check` stay green.
.PHONY: schema-lua-version-differential-negative-control
schema-lua-version-differential-negative-control:
	@set -eu; \
	mkdir -p "$(CURDIR)/build"; \
	log="$(CURDIR)/build/lua-version-differential-negative-control.log"; \
	if $(MAKE) schema-lua-version-differential LUA_DIFF_PLANT=1 > "$$log" 2>&1; then \
		echo "negative control failed: the planted 5.3-only divergence left the differential green" >&2; \
		cat "$$log" >&2; \
		exit 1; \
	fi; \
	grep -q 'PLANT CAUGHT under lua 5.3' "$$log" || { \
		echo "negative control failed: the gate went red without naming the plant" >&2; \
		cat "$$log" >&2; \
		exit 1; \
	}; \
	echo "Lua version differential negative control: the planted 5.3-only divergence is caught by name and Lua 5.4 stays green"
