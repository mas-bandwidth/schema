# make/lua-open-gates.mk — six of the Lua port's open-gate entries
# (docs/PORT-LUA.md; docs/PORTING.md I3, I6, I10, I13, J1, J2, M11; the roadmap's
# port/lua/open/forgery-fuzz, runtime-name-scan, zero-cost, text-differential,
# runtime-home and accessor-agreement). Included by the Makefile's wildcard
# include; each gate and each reversed witness is a prerequisite of test-lua below,
# and each reversed witness is also registered in make/negative-controls.json.
#
# The gates run through test/slowgate/proof where the Go test shells out to the
# pinned interpreter (it sets SCHEMA_SLOW=1, refuses a gate skip and requires a
# named --- PASS), and as a plain go test where the test is pure Go.

# I3: the block and cook forgery fuzz oracle (internal/codegen/luatable/
# forgeryfuzz_test.go). A mutated block or cook REFUSES, or OPENS and is WHOLE:
# the block against the compiler's layout, the cook against the tool's
# cook-check, and a full walk of whatever opened reads inside the bytes. SEED
# and N are knobs (LUA_FORGERY_SEED, LUA_FORGERY_N); the run prints its seed.
# The control removes BOTH halves of the block's extent bound and the cook's
# region scan.
.PHONY: tables-lua-fuzz tables-lua-fuzz-negative-control
tables-lua-fuzz: toolchain-lua
	sh test/slowgate/proof tables-lua-fuzz \
		'TestLuaForgeryFuzz TestLuaCookCheckRefusesADataPartOffTheAlignment' \
		./internal/codegen/luatable -run '^(TestLuaForgeryFuzz|TestLuaCookCheckRefusesADataPartOffTheAlignment)$$' -count=1 -timeout 600s
tables-lua-fuzz-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-fuzz-negative-control \
		'TestLuaForgeryFuzzNegativeControl/block_extent_bounds_removed TestLuaForgeryFuzzNegativeControl/cook_scan_removed' \
		./internal/codegen/luatable -run '^TestLuaForgeryFuzzNegativeControl$$' -count=1 -timeout 600s

# I6: every module-table key the Lua table emitter spells is registered and every
# registered Lua name is spelled, and a schema file named for a runtime module is
# refused beside a table and kept in a table-free unit (compiler/
# tablesluanames_test.go). The control plants an unregistered key in each
# convention the scan reads and a registered name nothing emits.
.PHONY: tables-lua-names tables-lua-names-negative-control
tables-lua-names:
	sh test/slowgate/proof tables-lua-names \
		'TestLuaTableRuntimeNamesAreClaimed TestLuaRuntimeFileNamesAreClaimed' \
		./compiler -run '^(TestLuaTableRuntimeNamesAreClaimed|TestLuaRuntimeFileNamesAreClaimed)$$' -count=1 -timeout 600s
tables-lua-names-negative-control:
	sh test/slowgate/proof tables-lua-names-negative-control \
		'TestLuaRuntimeNameScanGoesRed' \
		./compiler -run '^TestLuaRuntimeNameScanGoesRed$$' -count=1 -timeout 600s

# The controls ride the leg with their gates: each is seconds, and a reversed
# witness that `make test` never ran is a witness nothing reaches.
test-lua: tables-lua-fuzz tables-lua-fuzz-negative-control
test-lua: tables-lua-names tables-lua-names-negative-control
