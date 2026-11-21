# make/lua-conformance.mk — the LUA leg's conformance controls
# (test/conformance/README.md; docs/PORT-LUA.md). Included by the Makefile's
# wildcard include. The reference leg is cpp; this is a discovered, non-reference
# leg, so its absences are ordinary.

# THE LUA LEG's NEGATIVE CONTROLS, on the same rule as the Go ones: a harness
# that has never gone red on a leg is watching that leg. Both sabotage the
# EMITTER (docs/PORTING.md I2): a sed over a copy of one runtime file the table
# emitter embeds, checked to have matched, `go build -overlay` into a private
# compiler, the units regenerated, and the harness required red on exactly the
# named surfaces with the neighbours green. test/conformance/lua/negative-control
# holds both and says what each one requires.
#
# conformance-negative-control-lua       the load report's unknown counter
#                                        (report and retain red, wire green)
# conformance-negative-control-lua-walk  the text walk's field-key match (I11:
#                                        json-read and json-hostile red,
#                                        json-write and wire green)
# conformance-negative-control-lua-framing  the id table's 4096-entry bound
#                                        lifted (lua / framing red on
#                                        frame_entries_4097, wire and report green)
# conformance-negative-control-lua-cycle  the cycle check taken out (lua /
#                                        framing red on node_cycle_*, wire and
#                                        report green)
# conformance-negative-control-lua-write  the write-contract walk answers nil
#                                        (write-refusal, -fixed and -message
#                                        red; wire, report and -json green)
.PHONY: conformance-negative-control-lua conformance-negative-control-lua-walk conformance-negative-control-lua-framing conformance-negative-control-lua-cycle conformance-negative-control-lua-write
conformance-negative-control-lua: build/conformance-harness
	sh test/conformance/lua/negative-control report

conformance-negative-control-lua-walk: build/conformance-harness
	sh test/conformance/lua/negative-control walk
conformance-negative-control-lua-framing: build/conformance-harness
	sh test/conformance/lua/negative-control framing
conformance-negative-control-lua-cycle: build/conformance-harness
	sh test/conformance/lua/negative-control cycle
conformance-negative-control-lua-write: build/conformance-harness
	sh test/conformance/lua/negative-control write

# THE WHOLE CORPUS OVER AN EMPTY TREE (the Ruby and Python legs' rule, review
# F-086/F-087): every surface, every row, through the harness, with no Lua
# codec at all. The run is green (a missing codec is absence, never a failure
# or a pass) and every answer the driver leaves is an EMPTY <case>.absent.
# Before, the block, cook and forgery surfaces aborted on the first unit not
# generated ("unit not generated for lua").
.PHONY: conformance-lua-empty-tree
conformance-lua-empty-tree: toolchain-lua build/conformance-harness build/conformance-cpp build/schema_test_cook
	@mkdir -p build/conformance-lua-empty-gen
	@test -z "$$(ls -A build/conformance-lua-empty-gen)" || { echo 'build/conformance-lua-empty-gen is not empty'; exit 1; }
	@LUA=$(LUA) LUA_CONFORMANCE_GENERATED=build/conformance-lua-empty-gen \
		./build/conformance-harness run --only lua --work build/conformance-lua-empty > build/conformance-lua-empty.log 2>&1 || \
		{ echo 'conformance-lua-empty-tree: the run over an empty tree is not green'; tail -40 build/conformance-lua-empty.log; exit 1; }
	@answers=$$(find build/conformance-lua-empty/out/lua -type f | wc -l | tr -d ' '); \
	stray=$$(find build/conformance-lua-empty/out/lua -type f \( ! -name '*.absent' -o -size +0 \) | head -5); \
	test "$$answers" -gt 0 || { echo 'conformance-lua-empty-tree: the driver answered nothing'; exit 1; }; \
	test -z "$$stray" || { echo "conformance-lua-empty-tree: answers beside a missing codec that are not an EMPTY .absent: $$stray"; exit 1; }; \
	echo "conformance-lua-empty-tree: $$answers cases, every one an EMPTY .absent, run green"

test-lua: conformance-lua-empty-tree
