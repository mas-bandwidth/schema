# make/schema-lua-resource-bounds.mk — the LUA port's bounded memory/time gate
# (docs/SPEC-TABLES.md §16.9.4, the depth cap of 128 and the text_limit refusal;
# docs/PORT-LUA.md, THE REGISTER: collectgarbage("count") is the allocation
# instrument). Included by the Makefile's wildcard include.
#
# Hostile inputs must cost bounded memory and time: huge counts, deep nesting,
# recursion, giant strings and many unknown fields. The generated textform
# reader runs under the pinned PUC-Rio Lua (make/lua.mk, 5.4.8) and every shape
# must refuse by its documented name, allocate at most F times the input plus a
# fixed slack (Lua's GC memstat, collectgarbage("count")), and cost a number of
# VM instructions linear in the input (debug.sethook's count hook, never wall
# clock). The tests live in internal/ci/schema_lua_resource_bounds_test.go;
# they call the emitted runtime and the port's own generator.

# THE HEAVY SWEEP and the planted-bad-pattern control both run under the
# slow-gate proof script: SCHEMA_SLOW=1 so the gate is not skipped, a skip in
# the log is a failure, and every gate this target names must print a named
# --- PASS. A skipped test is not a pass.
.PHONY: schema-lua-resource-bounds
schema-lua-resource-bounds: toolchain-lua
	@if [ ! -x "$(LUA)" ]; then \
		echo "tool missing: lua (pinned $(LUA_VERSION)) at $(LUA); run make dist-lua"; \
		exit 1; \
	fi
	@echo "lua resource-bounds: hostile shapes under the depth cap and text_limit (Lua $(LUA_VERSION))"
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 nice -n 19 sh test/slowgate/proof schema-lua-resource-bounds \
		'TestLuaAllocationCounterCounts TestLuaResourceBoundsHostileShapes' \
		./internal/ci -run '^(TestLuaAllocationCounterCounts|TestLuaResourceBoundsHostileShapes)$$' -count=1 -timeout 600s

# THE PLANTED-BAD-PATTERN CONTROL: with the depth cap raised in a copy of the
# emitted runtime (the plant) the deep-nesting shape stops refusing as
# text_limit, so the control is red with the plant in and green with it out.
.PHONY: schema-lua-resource-bounds-negative-control
schema-lua-resource-bounds-negative-control: toolchain-lua
	@if [ ! -x "$(LUA)" ]; then \
		echo "tool missing: lua (pinned $(LUA_VERSION)) at $(LUA); run make dist-lua"; \
		exit 1; \
	fi
	@echo "lua resource-bounds: planted-pattern control (Lua $(LUA_VERSION))"
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 nice -n 19 sh test/slowgate/proof schema-lua-resource-bounds-negative-control \
		'TestLuaResourceBoundsPlantedPattern' \
		./internal/ci -run '^TestLuaResourceBoundsPlantedPattern$$' -count=1 -timeout 600s
