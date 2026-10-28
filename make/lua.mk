# make/lua.mk — the LUA leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-LUA.md is the design). Included by the Makefile's wildcard include;
# the Makefile names no language. The leg registers itself at the end of this
# file.
#
# Scaffolded by `schema new-leg lua`, then given its pinned interpreter and its
# two packages (schema-lua-port-design). Lua is a builtin target: the ordinary
# bin/schema is `schema generate --lang lua` and `schema testgen --lang lua`.

# THE PINNED INTERPRETER: PUC-Rio Lua 5.4.8, built from source into dist/
# (gitignored). Vanilla Lua 5.4 because it has the native 64-bit integer the
# port's value model stands on (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS)
# and string.pack/string.unpack for the byte buffers. LuaJIT (5.1) is not this
# leg. Populate with `make dist-lua`; any other sha256 is refused.
LUA_VERSION := 5.4.8
LUA_TARBALL := lua-$(LUA_VERSION).tar.gz
LUA_URL     := https://www.lua.org/ftp/$(LUA_TARBALL)
LUA_SHA256  := 4f18ddae154e793e46eeab727c59ef1c0c0c2b744e7b94219710d76f530629ae
LUA ?= $(CURDIR)/dist/lua-$(LUA_VERSION)/src/lua
# the conformance driver and the Go tests (internal/codegen/lua/luarun) read
# the pin from the environment
export LUA

.PHONY: dist-lua
dist-lua:
	@mkdir -p dist
	@if [ ! -f dist/$(LUA_TARBALL) ]; then curl -fsSL -o dist/$(LUA_TARBALL).part $(LUA_URL) && mv dist/$(LUA_TARBALL).part dist/$(LUA_TARBALL); fi
	@got=$$(sha256sum dist/$(LUA_TARBALL) 2>/dev/null | cut -d" " -f1); \
	if [ -z "$$got" ]; then got=$$(shasum -a 256 dist/$(LUA_TARBALL) | cut -d" " -f1); fi; \
	if [ "$$got" != "$(LUA_SHA256)" ]; then \
		echo "dist-lua REFUSES: dist/$(LUA_TARBALL) has sha256 $$got, the pin is $(LUA_SHA256)"; \
		echo "  delete dist/$(LUA_TARBALL) and run make dist-lua again; a tarball that does not match the pin is never built"; \
		exit 1; fi
	@rm -rf dist/lua-$(LUA_VERSION) && tar -xzf dist/$(LUA_TARBALL) -C dist
	$(MAKE) -C dist/lua-$(LUA_VERSION) $(if $(filter Darwin,$(shell uname -s)),macosx,linux)
	@echo "dist-lua: Lua $(LUA_VERSION) $(LUA_URL) sha256 $(LUA_SHA256)"
	@echo "dist-lua: $$(dist/lua-$(LUA_VERSION)/src/lua -v)"

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). `make test` runs this before the
# chain starts and refuses by name when the pin does not resolve.
.PHONY: toolchain-lua
toolchain-lua:
	@$(call toolchain_probe,lua,LUA,$(LUA))

# THE WIRE-FUZZ DIFFERENTIAL's SLOW-GATE RUN-PROOF (test/slowgate/proof): the
# in-package half of the card is gated by internal/slowtest and SKIPS unless
# SCHEMA_SLOW=1, so a bare `go test` proves nothing. `make test-lua` runs it
# THROUGH proof, exactly as make/go.mk's tables-go-textform runs its gated tests.
.PHONY: tables-lua-wire-fuzz-differential
tables-lua-wire-fuzz-differential:
	sh test/slowgate/proof tables-lua-wire-fuzz-differential \
		'TestLuaWireFuzzAgreesWithTheGoEngine TestLuaWireFuzzNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaWireFuzz' -count=1 -timeout 600s

# FINITE TABLE FLOATS (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27: "Tables go
# finite-only too, same rule as packets"): every Lua read refuses a NaN or an
# infinity on its raw bits as non_finite_float (variable, retained, message,
# retaining message, fixed, cook check, uncook and block dump), every writer
# refuses one, and -0 and the subnormals ride exact. The gate is
# TestLuaPlainFloatSpecialValuesAgreeAcrossForms; its reversed witness turns
# each read's raw-bits test off in turn and the probe must red on exactly that
# read's rows.
.PHONY: tables-lua-finite-float tables-lua-finite-float-negative-control
tables-lua-finite-float: toolchain-lua
	sh test/slowgate/proof tables-lua-finite-float \
		'TestLuaPlainFloatSpecialValuesAgreeAcrossForms' \
		./internal/codegen/luatable -run '^TestLuaPlainFloatSpecialValuesAgreeAcrossForms$$' -count=1 -timeout 600s
tables-lua-finite-float-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-finite-float-negative-control \
		'TestLuaFiniteFloatNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaFiniteFloatNegativeControl$$' -count=1 -timeout 600s

# THE COMMITTED TREE LOADS: every module of generated/lua and
# generated/lua-ludicrous, the serialize runtime beside them included, is
# required under the pinned interpreter and returns its module table — the
# Lua counterpart of the compiled legs building their generated/<lang> tree.
.PHONY: lua-generated-loads
lua-generated-loads: toolchain-lua generated/lua/.stamp generated/lua-ludicrous/.stamp
	@set -e; n=0; for d in generated/lua generated/lua-ludicrous; do \
		for f in $$d/*.lua; do \
			m=$$(basename $$f .lua); \
			(cd $$d && $(LUA) -e "package.path = './?.lua;' .. package.path; assert(type(require('$$m')) == 'table', '$$m returned no module table')"); \
			n=$$((n + 1)); \
		done; \
	done; \
	echo "lua-generated-loads: $$n committed modules load under Lua $(LUA_VERSION)"

.PHONY: test-lua update-goldens-lua
# The fixed-form tests read the shared byte-oracle corpus, so it is built first,
# and SCHEMA_REQUIRE_CORPUS=1 makes its absence a FAILURE rather than a skip.
# The variable also arms internal/slowtest's gate (schema#988's slow-gate scan),
# so the gated luatable tests this leg's toolchain can run (the loadfix and
# variant probes) run here instead of skipping green. The wire-fuzz pair is
# -skip'd only because tables-lua-wire-fuzz-differential, a prerequisite above,
# has just run it through test/slowgate/proof.
test-lua: toolchain-lua lua-generated-loads conformance-negative-control-lua conformance-negative-control-lua-walk tables-lua-wire-fuzz-differential build/fixedform-corpus/.stamp packet-finite-lua packet-finite-lua-negative-control
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/lua/ ./internal/codegen/luatable/ -count=1 -timeout 600s -skip '^TestLuaWireFuzz'
update-goldens-lua: ;

# bin/schema embeds both Lua runtime directories by glob (//go:embed
# runtime/*.lua in internal/codegen/lua and internal/codegen/luatable), which
# GO_SOURCES (*.go only) does not see, so they are added to bin/schema's
# prerequisites here: a changed runtime rebuilds the binary every gate then
# measures, and the committed generated/lua tree with it.
#
# build/conformance-lua is the tables manifest's units generated for lua, one
# directory per unit key. A unit the target refuses leaves its refusal, by
# name, in <unit>.refused instead of a directory — listed, never dropped.
bin/schema: $(wildcard internal/codegen/luatable/runtime/*.lua) $(wildcard internal/codegen/lua/runtime/*.lua)

# the Lua target: the type surface of the examples units, with the serialize
# runtime the emitter writes beside every unit (generated Lua is self-contained:
# no rock, no wiring file). Committed and held by `make generated-current`.
generated/lua/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang lua --out generated/lua examples
	@touch $@

generated/lua-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang lua --out generated/lua-ludicrous examples128
	@touch $@

build/conformance-lua: bin/schema testdata/conformance/tables/MANIFEST.txt make/lua.mk
	@rm -rf $@ && mkdir -p $@
	@awk '$$1 == "unit" { key = $$2; $$1 = ""; $$2 = ""; print key $$0 }' testdata/conformance/tables/MANIFEST.txt | \
	while read -r key paths; do \
		if ./bin/schema generate --lang lua --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; \
			echo "conformance-lua: $$key refused: $$(head -1 $@/$$key.refused)"; \
		fi; \
	done
	@echo "conformance-lua: $$(ls -d $@/*/ 2>/dev/null | wc -l | tr -d " ") units generated, $$(ls $@/*.refused 2>/dev/null | wc -l | tr -d " ") refused by name"

generated/bench/tables/lua/.stamp: ;

# PACKET FINITE FLOATS (SPEC §4.3, serialize format 2.0). The Lua packet target
# enforces the rule through its vendored serialize.lua runtime: a NaN or an
# infinity traps on write naming non_finite_float, refuses on read on its raw
# bits, and -0 and the subnormals ride bit exact. test/packet-finite/lua/main.lua
# runs every bare-float position of Finite.schema at both widths, plus the
# quantized float's write refusal. The generator emits the runtime beside the
# module, so each control sabotages that emitted copy (tools/sabotage, one
# direction at a time) and must fail exactly the vectors its .expected names.
build/packet-finite/lua/.stamp: build/schema-lua test/packet-finite/lua/Finite.schema
	@rm -rf build/packet-finite/lua && mkdir -p build/packet-finite/lua
	./build/schema-lua generate --lang lua --out build/packet-finite/lua/generated test/packet-finite/lua/Finite.schema
	@touch $@

.PHONY: packet-finite-lua packet-finite-lua-negative-control
packet-finite-lua: toolchain-lua build/packet-finite/lua/.stamp
	$(LUA) test/packet-finite/lua/main.lua build/packet-finite/lua/generated

# $(1) is the direction: read or write.
define packet-finite-lua-control
	@rm -rf build/packet-finite/lua-negative-$(1) && mkdir -p build/packet-finite/lua-negative-$(1)
	@cp -R build/packet-finite/lua/generated build/packet-finite/lua-negative-$(1)/generated
	go run ./tools/sabotage -name packet-finite-lua-$(1) \
		-out build/packet-finite/lua-negative-$(1)/generated/serialize_float.lua build/packet-finite/lua/generated/serialize_float.lua
	@if $(LUA) test/packet-finite/lua/main.lua build/packet-finite/lua-negative-$(1)/generated > build/packet-finite/lua-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Lua runtime float $(1) check was removed and the finite gate stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' build/packet-finite/lua-negative-$(1)/log | LC_ALL=C sort > build/packet-finite/lua-negative-$(1)/failed
	@diff -u test/packet-finite/lua/$(1).expected build/packet-finite/lua-negative-$(1)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the Lua float $(1) sabotage did not fail exactly its vectors'; cat build/packet-finite/lua-negative-$(1)/log; exit 1; }
	@echo "packet finite Lua negative control ($(1)): the removed runtime check fails exactly $$(wc -l < build/packet-finite/lua-negative-$(1)/failed | tr -d ' ') vectors"
endef

packet-finite-lua-negative-control: packet-finite-lua
	$(call packet-finite-lua-control,read)
	$(call packet-finite-lua-control,write)

TEST_LEGS         += test-lua
CONFORMANCE_LEGS  += $(call unless_skipped,lua,build/conformance-lua)
BENCH_TABLES_LEGS += generated/bench/tables/lua/.stamp
GOLDENS_LEGS      += update-goldens-lua
TOOLCHAIN_LEGS    += lua
TOOLCHAIN_PINS_lua := LUA
