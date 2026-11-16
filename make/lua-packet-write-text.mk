# ---------------------------------------------------------------- write text
# SPEC §5.1 row 8: a string(N) in the packet Write holds no zero byte and is
# well-formed UTF-8 in its used bytes, refused in every build (false, never a
# raise). The shared Text.schema cases carry ill-formed values and well-formed
# twins; the driver writes every case and reads each lawful one back byte-exact.
build/packet-write-text/lua/.stamp: bin/schema test/packet-write-text/Text.schema
	@rm -rf build/packet-write-text/lua && mkdir -p build/packet-write-text/lua
	./bin/schema generate --lang lua --out build/packet-write-text/lua/generated test/packet-write-text/Text.schema
	@touch $@

PACKET_WRITE_TEXT_LUA_DIR ?= build/packet-write-text/lua/generated
.PHONY: packet-write-text-lua packet-write-text-lua-negative-control
packet-write-text-lua: toolchain-lua build/packet-write-text/lua/.stamp
	$(LUA) test/packet-write-text/lua/main.lua $(PACKET_WRITE_TEXT_LUA_DIR) test/packet-write-text

packet-write-text-lua-negative-control: packet-write-text-lua
	@rm -rf build/packet-write-text/lua-negative && mkdir -p build/packet-write-text/lua-negative/generated
	@cp -R build/packet-write-text/lua/generated/. build/packet-write-text/lua-negative/generated/
	go run ./tools/sabotage -name packet-write-text-lua-utf8 -out build/packet-write-text/lua-negative/generated/Text.lua build/packet-write-text/lua/generated/Text.lua
	@if $(MAKE) packet-write-text-lua PACKET_WRITE_TEXT_LUA_DIR=build/packet-write-text/lua-negative/generated > build/packet-write-text/lua-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: packet-write-text-lua stayed green'; exit 1; fi
	@sed -n 's/^FAILED: \([^:]*\):.*/\1/p' build/packet-write-text/lua-negative/log | LC_ALL=C sort > build/packet-write-text/lua-negative/actual
	@diff -u test/packet-write-text/lua/refused.expected build/packet-write-text/lua-negative/actual || \
		{ echo 'NEGATIVE CONTROL FAILED: the removed text checks did not fail exactly the ill-formed cases'; cat build/packet-write-text/lua-negative/log; exit 1; }
	@echo 'packet-write-text-lua negative control: removed text checks refused by packet-write-text-lua'

test-lua: packet-write-text-lua packet-write-text-lua-negative-control
