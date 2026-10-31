# make/lua-alloc.mk — the LUA leg's caller-owned load, measured (docs/PORTING.md
# M1, I1, I9, I14; docs/PORT-LUA.md). Included by the Makefile's wildcard
# include. Every target runs its gate THROUGH test/slowgate/proof, so the gated
# tests it names must print a named --- PASS and a skip in the log is a failure.
#
# THE INSTRUMENT is the VM's own allocation counter, collectgarbage("count"),
# read with the collector stopped over a steady phase: exact bytes, and a path
# that allocates and frees the same bytes every iteration still counts. The
# rows are in internal/codegen/luatable/alloc_test.go: the rows that license
# nothing are held to EXACT ZERO and the rows that license a table per element,
# per node or per list to a pinned ceiling.
#
# tables-lua-alloc                         the floor, row by row, on the pinned Lua
# tables-lua-alloc-negative-control        one planted allocation per queued slot and
#                                          one per array body, each turning only the
#                                          rows that pass through it (I1)
# tables-lua-alloc-runtime-negative-control  another interpreter's version is refused by
#                                          name before anything is measured (I14);
#                                          SCHEMA_LUA_ALLOC_ANY_LUA=1 prints the rows
#                                          under NOT CERTIFIED and asserts nothing
# tables-lua-soak                          the soak, a PASS COUNT (never a clock):
#                                          TABLES_LUA_SOAK_ITERATIONS passes, gated on
#                                          the allocation count beside the live heap (I9)
# tables-lua-soak-negative-control         a table built and dropped per load: the drift
#                                          gate stays silent and the count goes red
.PHONY: tables-lua-alloc tables-lua-alloc-negative-control tables-lua-alloc-runtime-negative-control tables-lua-soak tables-lua-soak-negative-control

tables-lua-alloc: toolchain-lua
	sh test/slowgate/proof tables-lua-alloc \
		'TestLuaLoadAllocationFloor' \
		./internal/codegen/luatable -run '^TestLuaLoadAllocationFloor$$' -count=1 -timeout 600s

tables-lua-alloc-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-alloc-negative-control \
		'TestLuaLoadAllocationNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaLoadAllocationNegativeControl$$' -count=1 -timeout 600s

tables-lua-alloc-runtime-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-alloc-runtime-negative-control \
		'TestLuaLoadAllocationRefusesOffThePin' \
		./internal/codegen/luatable -run '^TestLuaLoadAllocationRefusesOffThePin$$' -count=1 -timeout 600s

tables-lua-soak: toolchain-lua
	sh test/slowgate/proof tables-lua-soak \
		'TestLuaLoadSoak' \
		./internal/codegen/luatable -run '^TestLuaLoadSoak$$' -count=1 -timeout 3600s

tables-lua-soak-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-soak-negative-control \
		'TestLuaLoadSoakNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaLoadSoakNegativeControl$$' -count=1 -timeout 600s
