# make/lua-fuzz.mk — the LUA leg's wire-fuzz gate (docs/PORT-LUA.md, THE CARD
# MAP: schema-lua-fuzz-differential; test/conformance/README.md, "The wire-fuzz
# driver"; docs/SPEC-TABLES.md §4.2). Included by the Makefile's wildcard
# include.
#
# The harness owns the mutants and the oracle (internal/tablewire); the leg is
# ONE lua process on a pipe, test/conformance/lua/wirefuzz.lua, over the units
# build/conformance-lua generated. The in-package half of the same claim is
# internal/codegen/luatable TestLuaWireFuzzAgreesWithTheGoEngine and its
# negative control, TestLuaWireFuzzNegativeControl.
#
# The run must say it fuzzed something: the harness's own summary lines are
# required, exactly as the Go and JavaScript card gates require them
# (make/go.mk cards-wire-fuzz-run, make/js.mk cards-js-wire-fuzz).

LUA_FUZZ_SEED ?= 1
LUA_FUZZ_N    ?= 2000

.PHONY: tables-lua-wire-fuzz
tables-lua-wire-fuzz: build/conformance-harness build/conformance-lua
	@mkdir -p build
	@./build/conformance-harness wire-fuzz \
		--driver '$(LUA) test/conformance/lua/wirefuzz.lua build/conformance-lua' \
		--seed $(LUA_FUZZ_SEED) --n $(LUA_FUZZ_N) \
		> build/tables-lua-wire-fuzz.log 2>&1; \
		status=$$?; cat build/tables-lua-wire-fuzz.log; \
		test $$status -eq 0
	@grep -q '0 divergences' build/tables-lua-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/tables-lua-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/tables-lua-wire-fuzz.log

# The harness-driven differential is part of the leg's certification run, as
# the Go, JavaScript, Java and Dart card wire-fuzz gates are part of theirs.
test-lua: tables-lua-wire-fuzz
