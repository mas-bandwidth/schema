# make/lua.mk — the LUA leg (docs/CONTRIBUTING.md, "Adding a language";
# docs/PORT-LUA.md is the design). Included by the Makefile's wildcard include;
# the Makefile names no language. The leg registers itself at the end of this
# file.
#
# Scaffolded by `schema new-leg lua`, then given its pinned interpreter and its
# two packages (schema-lua-port-design). compiler/target_lua.go keeps its
# schema_leg_lua build tag until the wire surfaces land, so `schema generate
# --lang lua` is a binary built with that tag (build/schema-lua below).

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

.PHONY: test-lua update-goldens-lua
test-lua: toolchain-lua
	go test ./internal/codegen/lua/ ./internal/codegen/luatable/ -count=1 -timeout 600s
update-goldens-lua: ;

# build/schema-lua is the compiler WITH the lua target (its build tag), and
# build/conformance-lua is the tables manifest's units generated for lua, one
# directory per unit key. A unit the target refuses leaves its refusal, by
# name, in <unit>.refused instead of a directory — listed, never dropped.
build/schema-lua: $(GO_SOURCES)
	@mkdir -p build
	go build -tags schema_leg_lua -o $@ ./cmd/schema

build/conformance-lua: build/schema-lua testdata/conformance/tables/MANIFEST.txt make/lua.mk
	@rm -rf $@ && mkdir -p $@
	@awk '$$1 == "unit" { key = $$2; $$1 = ""; $$2 = ""; print key $$0 }' testdata/conformance/tables/MANIFEST.txt | \
	while read -r key paths; do \
		if ./build/schema-lua generate --lang lua --out $@/$$key $$paths > $@/$$key.log 2>&1; then \
			rm -f $@/$$key.log; \
		else \
			rm -rf $@/$$key; mv $@/$$key.log $@/$$key.refused; \
			echo "conformance-lua: $$key refused: $$(head -1 $@/$$key.refused)"; \
		fi; \
	done
	@echo "conformance-lua: $$(ls -d $@/*/ 2>/dev/null | wc -l | tr -d " ") units generated, $$(ls $@/*.refused 2>/dev/null | wc -l | tr -d " ") refused by name"

generated/bench/tables/lua/.stamp: ;

TEST_LEGS         += test-lua
CONFORMANCE_LEGS  += $(call unless_skipped,lua,build/conformance-lua)
BENCH_TABLES_LEGS += generated/bench/tables/lua/.stamp
GOLDENS_LEGS      += update-goldens-lua
TOOLCHAIN_LEGS    += lua
TOOLCHAIN_PINS_lua := LUA
