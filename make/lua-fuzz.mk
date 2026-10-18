# make/lua-fuzz.mk — the LUA leg's wire-fuzz differential (docs/PORT-LUA.md,
# THE CARD MAP; test/conformance/README.md, "The wire-fuzz driver"). The
# harness owns the mutants and the oracle; this leg's driver, one lua process
# on a pipe, binds the roster and answers per mutant. The target is the Lua
# leg's cell of the tolerant wire's fuzzer (docs/SPEC-TABLES.md §4.2).

.PHONY: tables-lua-wire-fuzz
tables-lua-wire-fuzz: build/conformance-harness build/conformance-lua
	./build/conformance-harness wire-fuzz --driver "$(LUA) test/conformance/lua/wirefuzz.lua build/conformance-lua" --seed 1 --n 2000
