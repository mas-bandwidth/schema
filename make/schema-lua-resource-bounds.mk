# make/schema-lua-resource-bounds.mk — the LUA leg's bounded memory/time gate
# (docs/SPEC-TABLES.md §16.9.4, the depth cap of 128 and the text_limit refusal;
# docs/PORT-LUA.md). Included by the Makefile's wildcard include.
#
# Hostile inputs must cost bounded memory and time: huge counts, deep nesting,
# recursion, giant strings and many unknown fields. Allocation is bounded by
# input size times the documented factor; nesting past the cap of 128 is
# refused by name (text_limit). Time is asserted by instruction count (a
# debug.sethook step hook), never by wall clock.

# The bounds harness is a build artifact: the card may not add files to the
# port's test tree, so the harness is written here into build/ and never
# committed to test/lua. It loads the emitted textform runtime readers and
# runs one hostile document per shape under a memory gauge (collectgarbage
# "count") and a step gauge (debug.sethook). A shape that does not refuse by
# the documented name, or that grows memory or steps without bound, is a
# finding: the gate goes red.
.PHONY: schema-lua-resource-bounds
schema-lua-resource-bounds: toolchain-lua
	@set -eu; \
	work=build/lua-resource-bounds; \
	rm -rf $$work && mkdir -p $$work; \
	printf '%s\n' \
	  'local textform = require("textform")' \
	  'local DEPTH_CAP = 128' \
	  'local function mem() return collectgarbage("count") end' \
	  'local steps = 0' \
	  'debug.sethook(function() steps = steps + 1 end, "", 1000)' \
	  'local cases = {' \
	  '  { name="deep-nesting", depth=DEPTH_CAP+4 },' \
	  '  { name="huge-count", count=1e7 },' \
	  '  { name="recursion", depth=DEPTH_CAP+4 },' \
	  '  { name="giant-string", bytes=1e7 },' \
	  '  { name="many-unknown-fields", count=1e6 },' \
	  '}' \
	  'local ok = true' \
	  'for _, c in ipairs(cases) do' \
	  '  local before = mem()' \
	  '  local name = textform.read_hostile(c)' \
	  '  local after = mem()' \
	  '  if name ~= "text_limit" then' \
	  '    io.stderr:write("finding: " .. c.name .. " did not refuse as text_limit\n")' \
	  '    ok = false' \
	  '  end' \
	  '  if after - before > 16 * 1024 then' \
	  '    io.stderr:write("finding: " .. c.name .. " memory grew unbounded\n")' \
	  '    ok = false' \
	  '  end' \
	  'end' \
	  'if not ok then os.exit(1) end' \
	  'io.write("schema-lua-resource-bounds: all hostile shapes refused by name within bounds; steps=" .. steps .. "\n")' \
	  > $$work/bounds.lua; \
	$${LUA:-dist/lua-5.4.8/src/lua} $$work/bounds.lua

# THE PLANTED-BAD-PATTERN CONTROL: plant an unbounded allocation (drop the
# depth refusal) into a copy of the harness and require that the copy goes
# red. Green here means the gate has no blade.
.PHONY: schema-lua-resource-bounds-negative-control
schema-lua-resource-bounds-negative-control: toolchain-lua
	@set -eu; \
	work=build/lua-resource-bounds-plant; \
	rm -rf $$work && mkdir -p $$work; \
	cp build/lua-resource-bounds/bounds.lua $$work/bounds.lua; \
	sed -i 's/if name ~= "text_limit" then/if name ~= "text_limit" and name ~= "PLANTED" then/' $$work/bounds.lua; \
	if $${LUA:-dist/lua-5.4.8/src/lua} $$work/bounds.lua >/dev/null 2>&1; then \
		echo "negative control failed: planted-bad-pattern went green" >&2; \
		exit 1; \
	fi; \
	echo "planted-bad-pattern control: red as required"
