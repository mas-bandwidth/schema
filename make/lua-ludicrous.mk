# make/lua-ludicrous.mk — the Lua leg's LUDICROUS PACKET PROGRAM, the twin of
# test/luau-ludicrous, test/js-ludicrous and test/ludicrous_main.cpp:
# examples128 generated for Lua, every pinned instance written byte for byte
# against the C++-pinned goldens in testdata/wire, read back, the defaults,
# branch zeroing and hostile rejections. The nightly readiness run
# (test/conformance/nightly-readiness lua) executes the program as its
# packet-ludicrous phase; `make test-lua` runs it with its control.
#
#   lua-ludicrous                   the program, then luacheck over it with the
#                                   generated-code gate's config
#   lua-ludicrous-negative-control  the same program over a copy of the goldens
#                                   with one byte of ludicrous_state flipped:
#                                   it must go red on exactly that golden

build/lua-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	@rm -rf build/lua-ludicrous && mkdir -p build/lua-ludicrous
	./bin/schema generate --lang lua --out build/lua-ludicrous examples128
	@touch $@

.PHONY: lua-ludicrous lua-ludicrous-negative-control
lua-ludicrous: toolchain-lua toolchain-luacheck build/lua-ludicrous/.stamp build/lua-luacheck.rc
	$(LUA) test/lua-ludicrous/main.lua build/lua-ludicrous testdata/wire
	$(LUACHECK) --config $(LUA_LUACHECK_CONFIG) --no-color --codes test/lua-ludicrous/main.lua

lua-ludicrous-negative-control: toolchain-lua build/lua-ludicrous/.stamp
	@rm -rf build/lua-ludicrous-negative && mkdir -p build/lua-ludicrous-negative/wire
	@cp testdata/wire/ludicrous_state.bin testdata/wire/ludicrous_state_untargeted.bin \
		testdata/wire/degenerate_probe.bin testdata/wire/unsigned_probe.bin build/lua-ludicrous-negative/wire/
	@printf '\377' | dd of=build/lua-ludicrous-negative/wire/ludicrous_state.bin bs=1 seek=5 conv=notrunc 2>/dev/null
	@if cmp -s testdata/wire/ludicrous_state.bin build/lua-ludicrous-negative/wire/ludicrous_state.bin; then \
		echo 'NEGATIVE CONTROL: the flipped golden byte changed nothing'; exit 1; fi
	@if $(LUA) test/lua-ludicrous/main.lua build/lua-ludicrous build/lua-ludicrous-negative/wire > build/lua-ludicrous-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a flipped ludicrous_state golden passed the Lua ludicrous program'; exit 1; fi
	@grep -Fq 'FAILED: wire golden ludicrous_state:' build/lua-ludicrous-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the Lua ludicrous program failed for another reason'; cat build/lua-ludicrous-negative/log; exit 1; }
	@if grep -F 'FAILED: ' build/lua-ludicrous-negative/log | grep -Fvq 'FAILED: wire golden ludicrous_state:'; then \
		cat build/lua-ludicrous-negative/log; echo 'NEGATIVE CONTROL FAILED: a check other than the flipped golden went red'; exit 1; fi
	@echo 'lua ludicrous negative control: a flipped C++-pinned golden byte fails exactly that golden'

test-lua: lua-ludicrous lua-ludicrous-negative-control
