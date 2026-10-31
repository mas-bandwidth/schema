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

# I10: the zero-cost gate (compiler/tableslua_test.go): adding a table moves not
# one byte of the non-table output, a table-free unit emits none of the table
# sources, and the unit's table module spells no accelerator symbol.
.PHONY: tables-lua-zero-cost tables-lua-zero-cost-negative-control
tables-lua-zero-cost:
	sh test/slowgate/proof tables-lua-zero-cost \
		'TestLuaTablesMoveNoGeneratedPacketByte' \
		./compiler -run '^TestLuaTablesMoveNoGeneratedPacketByte$$' -count=1 -timeout 600s
tables-lua-zero-cost-negative-control:
	sh test/slowgate/proof tables-lua-zero-cost-negative-control \
		'TestLuaZeroCostGateGoesRed' \
		./compiler -run '^TestLuaZeroCostGateGoesRed$$' -count=1 -timeout 600s

# I13: the random text differential against the Go engine (textdifferential_test.go).
.PHONY: tables-lua-json-differential tables-lua-json-differential-negative-control
tables-lua-json-differential: toolchain-lua
	sh test/slowgate/proof tables-lua-json-differential \
		'TestLuaTextDifferential' \
		./internal/codegen/luatable -run '^TestLuaTextDifferential$$' -count=1 -timeout 600s
tables-lua-json-differential-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-json-differential-negative-control \
		'TestLuaTextDifferentialNegativeControl' \
		./internal/codegen/luatable -run '^TestLuaTextDifferentialNegativeControl$$' -count=1 -timeout 600s

# J2: the runtime home. A unit's table module lives in one file named by the
# PACKAGE (<Package>Tables.lua), never by the schema file that sorts first, so a
# file that sorts earlier relocates nothing: the module keeps its name and the
# embedded runtime files (load.lua, block.lua, ...) are byte-identical across the
# two trees.
.PHONY: tables-lua-runtime-home
tables-lua-runtime-home: bin/schema
	@rm -rf build/runtime-home-lua && mkdir -p build/runtime-home-lua/src
	@cp tables/examples/*.schema build/runtime-home-lua/src/
	@printf 'package tabledemo\n\ntype AaaPacket\n{\n    tag uint8\n}\n' > build/runtime-home-lua/src/Aaa.schema
	@./bin/schema generate --lang lua --out build/runtime-home-lua/base tables/examples 2>/dev/null
	@./bin/schema generate --lang lua --out build/runtime-home-lua/added build/runtime-home-lua/src 2>/dev/null
	@for tree in base added; do \
		test -f build/runtime-home-lua/$$tree/TabledemoTables.lua || \
			{ echo "RUNTIME HOME GATE FAILED: the $$tree tree has no TabledemoTables.lua (the table module is named by the package)"; ls build/runtime-home-lua/$$tree/*Tables.lua; exit 1; }; \
	done
	@for f in internal/codegen/luatable/runtime/*.lua; do \
		n=$$(basename $$f); \
		cmp -s build/runtime-home-lua/base/$$n build/runtime-home-lua/added/$$n || \
			{ echo "RUNTIME HOME GATE FAILED: the table runtime file $$n moved when the unit gained an earlier-sorting file"; exit 1; }; \
	done
	@echo "runtime home gate (Lua): the table module stays TabledemoTables.lua and the table runtime files stay byte-identical when an earlier-sorting file joins the unit"

# Its NEGATIVE CONTROL: put the file-order rule back (the module named by the
# file that sorts first, ir.ProtocolIdHome) and the home must MOVE when the
# earlier-sorting file joins. A gate that has never gone red is watching nothing.
.PHONY: tables-lua-runtime-home-negative-control
tables-lua-runtime-home-negative-control: bin/schema tables-lua-runtime-home
	@sed 's|return strings.ToUpper(u.Package\[:1\]) + u.Package\[1:\] + "Tables"|return ir.ProtocolIdHome(u) + "Tables" // SABOTAGED: back to the file order|' \
		internal/codegen/luatable/luatable.go > build/lua-runtime-home-fileorder.gotext
	@grep -q SABOTAGED build/lua-runtime-home-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/luatable/luatable.go":"%s/build/lua-runtime-home-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/lua-runtime-home-overlay.json
	@go build -overlay=build/lua-runtime-home-overlay.json -o build/schema-lua-runtime-home-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-lua/base-sabotage build/runtime-home-lua/added-sabotage
	@./build/schema-lua-runtime-home-sabotaged generate --lang lua --out build/runtime-home-lua/base-sabotage tables/examples 2>/dev/null
	@./build/schema-lua-runtime-home-sabotaged generate --lang lua --out build/runtime-home-lua/added-sabotage build/runtime-home-lua/src 2>/dev/null
	@base=$$(cd build/runtime-home-lua/base-sabotage && ls *Tables.lua | grep -v '^Tables.lua$$'); \
	 added=$$(cd build/runtime-home-lua/added-sabotage && ls *Tables.lua | grep -v '^Tables.lua$$'); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the table module in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (Lua): the file-order rule moves the table module from $$base to $$added"

# J1 and M11: the Lua form of the accessor and descriptor agreement gate
# (layoutagree_test.go). Lua generates no accessor, so what is held is the
# layout's two independent derivations: the compiler's model against the Lua
# runtime's, for every block projection, block row and cook record of every unit
# of the corpus. The two controls move one scalar's block offset four bytes
# (RenderCamera.camera_id) and one cook pointer slot's descriptor offset eight
# (TreeNode.left); each must red on that field and on nothing else.
.PHONY: tables-lua-accessor-descriptor-agreement tables-lua-accessor-negative-control tables-lua-slot-negative-control
tables-lua-accessor-descriptor-agreement: toolchain-lua
	sh test/slowgate/proof tables-lua-accessor-descriptor-agreement \
		'TestLuaLayoutAgreesWithTheCompiler' \
		./internal/codegen/luatable -run '^TestLuaLayoutAgreesWithTheCompiler$$' -count=1 -timeout 600s
tables-lua-accessor-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-accessor-negative-control \
		'TestLuaLayoutAgreementNegativeControl/a_block_scalar_moved_four_bytes' \
		./internal/codegen/luatable -run '^TestLuaLayoutAgreementNegativeControl$$/^a_block_scalar_moved_four_bytes$$' -count=1 -timeout 600s
tables-lua-slot-negative-control: toolchain-lua
	sh test/slowgate/proof tables-lua-slot-negative-control \
		'TestLuaLayoutAgreementNegativeControl/a_cook_slot_moved_eight_bytes' \
		./internal/codegen/luatable -run '^TestLuaLayoutAgreementNegativeControl$$/^a_cook_slot_moved_eight_bytes$$' -count=1 -timeout 600s

# The controls ride the leg with their gates: each is seconds, and a reversed
# witness that `make test` never ran is a witness nothing reaches.
test-lua: tables-lua-fuzz tables-lua-fuzz-negative-control
test-lua: tables-lua-names tables-lua-names-negative-control
test-lua: tables-lua-zero-cost tables-lua-zero-cost-negative-control
test-lua: tables-lua-json-differential tables-lua-json-differential-negative-control
test-lua: tables-lua-runtime-home tables-lua-runtime-home-negative-control
test-lua: tables-lua-accessor-descriptor-agreement tables-lua-accessor-negative-control tables-lua-slot-negative-control
