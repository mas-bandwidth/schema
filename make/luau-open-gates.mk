# make/luau-open-gates.mk — the LUAU leg's gates for the PORTING.md rows the
# other files do not hold (docs/PORTING.md M5, J2). Included by the Makefile's
# wildcard include.

# J2: the runtime home. A Luau unit's shared table runtime is the fixed set of
# schema_*.luau modules (docs/PORT-LUAU.md, THE RUNTIME LAYOUT), named by the
# runtime and never by the schema file that sorts first, and each schema file's
# table module is <Base>Table.luau, named by its own file. So a file that sorts
# earlier relocates nothing: every table module keeps its name and every
# runtime module is byte-identical across the two trees. schema_view.luau is the
# one runtime module that carries the unit's own declarations (UnitView,
# docs/SPEC-TABLES.md §8.2), so the joining file changes its text by design; it
# is held to its NAME only.
.PHONY: tables-luau-runtime-home tables-luau-runtime-home-negative-control
tables-luau-runtime-home: bin/schema
	@rm -rf build/runtime-home-luau && mkdir -p build/runtime-home-luau/src
	@cp tables/examples/*.schema build/runtime-home-luau/src/
	@printf 'package tabledemo\n\ntype AaaPacket\n{\n    tag uint8\n}\n' > build/runtime-home-luau/src/Aaa.schema
	@./bin/schema generate --lang luau --out build/runtime-home-luau/base tables/examples 2>/dev/null
	@./bin/schema generate --lang luau --out build/runtime-home-luau/added build/runtime-home-luau/src 2>/dev/null
	@sh test/luau-runtime-home/check build/runtime-home-luau/base build/runtime-home-luau/added Aaa
	@echo "runtime home gate (Luau): every <Base>Table.luau keeps its name and the schema_*.luau runtime stays byte-identical when an earlier-sorting file joins the unit"

# Its NEGATIVE CONTROL: put the file-order rule back (the runtime named by the
# file that sorts first, ir.ProtocolIdHome) and the home must MOVE when the
# earlier-sorting file joins. A gate that has never gone red is watching nothing.
tables-luau-runtime-home-negative-control: bin/schema tables-luau-runtime-home
	@sed 's|^\t\tout\[name\] = data$$|\t\tout[ir.ProtocolIdHome(u)+"_"+name] = data // SABOTAGED: back to the file order|' \
		internal/codegen/luautable/luautable.go > build/luau-runtime-home-fileorder.gotext
	@grep -q SABOTAGED build/luau-runtime-home-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/luautable/luautable.go":"%s/build/luau-runtime-home-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/luau-runtime-home-overlay.json
	@go build -overlay=build/luau-runtime-home-overlay.json -o build/schema-luau-runtime-home-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-luau/base-sabotage build/runtime-home-luau/added-sabotage
	@./build/schema-luau-runtime-home-sabotaged generate --lang luau --out build/runtime-home-luau/base-sabotage tables/examples 2>/dev/null
	@./build/schema-luau-runtime-home-sabotaged generate --lang luau --out build/runtime-home-luau/added-sabotage build/runtime-home-luau/src 2>/dev/null
	@if sh test/luau-runtime-home/check build/runtime-home-luau/base-sabotage build/runtime-home-luau/added-sabotage Aaa > build/runtime-home-luau/control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule left the runtime home in place — the gate is watching nothing"; exit 1; \
	fi
	@grep -q 'RUNTIME HOME GATE FAILED' build/runtime-home-luau/control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate failed for another reason"; cat build/runtime-home-luau/control.log; exit 1; }
	@echo "runtime home negative control (Luau): the file-order rule moves the runtime home, and the gate says so: $$(head -1 build/runtime-home-luau/control.log)"

# M5: the keyed accessor (schema_tables.luau S.keyed) and its three reversed
# witnesses, one plant per rule (the None guard, the past-Max guard, the shift).
.PHONY: tables-luau-keyed tables-luau-keyed-negative-control
tables-luau-keyed: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauKeyedAccessorCoversBothEnds$$' -count=1 -timeout 600s
tables-luau-keyed-negative-control: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauKeyedNegativeControls$$' -count=1 -timeout 600s

test-luau: tables-luau-runtime-home tables-luau-runtime-home-negative-control
test-luau: tables-luau-keyed tables-luau-keyed-negative-control

# I12: the documented surface. docs/USAGE.md's Luau fragment (between the
# luau-table-usage markers) is run against the tables/examples unit under the
# pinned interpreter: a prelude gives it `use`, and the epilogue requires that
# it walked the three turrets and read back the damage it wrote.
USAGE_LUAU_DIR ?= build/luau-usage
.PHONY: tables-luau-usage tables-luau-usage-negative-control
tables-luau-usage: toolchain-luau bin/schema $(USAGE_DOC)
	@rm -rf $(USAGE_LUAU_DIR) && mkdir -p $(USAGE_LUAU_DIR)
	./bin/schema generate --lang luau --out $(USAGE_LUAU_DIR) tables/examples
	printf 'local seen = {}\nlocal function use(x) seen[#seen + 1] = x end\n' > $(USAGE_LUAU_DIR)/usage.luau
	sed -n '/<!-- luau-table-usage -->/,/<!-- \/luau-table-usage -->/p' $(USAGE_DOC) \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> $(USAGE_LUAU_DIR)/usage.luau
	printf 'if #seen ~= 3 or seen[2] ~= 25 then error("the walk read " .. #seen .. " turret(s), Missile " .. tostring(seen[2])) end\n' >> $(USAGE_LUAU_DIR)/usage.luau
	printf 'print("usage: docs/USAGE.md Luau example saved, read back and walked " .. #seen .. " turret(s)")\n' >> $(USAGE_LUAU_DIR)/usage.luau
	grep -q 'S.keyed' $(USAGE_LUAU_DIR)/usage.luau || \
		{ echo "MISSING: $(USAGE_DOC) carries no luau-table-usage example"; exit 1; }
	cd $(USAGE_LUAU_DIR) && $(LUAU) usage.luau

# THE REVERSED WITNESSES: two edited copies of the page, each checked to have
# changed, must each turn the gate red. The first renames the save the page
# calls (the surface moved: the run errors); the second walks nothing (the
# example runs but the epilogue's count refuses).
tables-luau-usage-negative-control:
	@rm -rf build/luau-usage-negative && mkdir -p build/luau-usage-negative
	sed 's/save\.save(hulls, hull)/save.sav(hulls, hull)/' docs/USAGE.md > build/luau-usage-negative/renamed.md
	sed 's/^  use(turret\.Damage)$$/  local _ = turret.Damage/' docs/USAGE.md > build/luau-usage-negative/walks-nothing.md
	@for c in renamed walks-nothing; do \
		if cmp -s docs/USAGE.md build/luau-usage-negative/$$c.md; then \
			echo "NEGATIVE CONTROL: the $$c edit did not apply to docs/USAGE.md"; exit 1; fi; \
		if $(MAKE) tables-luau-usage USAGE_DOC=build/luau-usage-negative/$$c.md USAGE_LUAU_DIR=build/luau-usage-negative/$$c > build/luau-usage-negative/$$c.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$c copy of the page passed the Luau usage gate"; exit 1; fi; \
		echo "tables-luau-usage negative control: the $$c copy fails the gate"; \
	done

test-luau: tables-luau-usage tables-luau-usage-negative-control

# J3 — THE RELEASE TIER. The expensive half of the leg, behind one target
# certify.yml finds BY NAME (its release-targets job derives the list from the
# Makefile): every instrument is the PR tier's scaled, none is new.
#
#   the wire-fuzz differential against the Go engine at ten times the PR count
#   and on a second seed, because a divergence escapes by depth of search, and
#   the pipe's own control beside it;
#   the documented surface (tables-luau-usage) and its reversed witnesses.
LUAU_RELEASE_FUZZ_N ?= 20000

.PHONY: tables-luau-release
tables-luau-release: toolchain-luau build/conformance-harness build/conformance-luau
	$(MAKE) tables-luau-wire-fuzz LUAU_FUZZ_N=$(LUAU_RELEASE_FUZZ_N)
	$(MAKE) tables-luau-wire-fuzz LUAU_FUZZ_N=$(LUAU_RELEASE_FUZZ_N) LUAU_FUZZ_SEED=2
	$(MAKE) tables-luau-wire-fuzz-negative-control
	$(MAKE) tables-luau-usage
	$(MAKE) tables-luau-usage-negative-control
	@echo "tables Luau release gate: the wire fuzz at depth on two seeds with its control, and the page's example with its witnesses"

# THE RELEASE TIER's CONTROL: the release gate runs over a scratch copy of the
# units with the wire-fuzz control's reader bug planted (a string frame past its
# body not counted malformed, test/conformance/luau/wire-fuzz-negative-control)
# and must go RED on the report, at the release count, on its first seed: the
# tier is not blind to what the PR tier catches.
.PHONY: tables-luau-release-negative-control
tables-luau-release-negative-control: toolchain-luau build/conformance-harness build/conformance-luau build/luau-driver/units.luau build/luaufuzzhost/luaufuzzhost
	sh test/conformance/luau/wire-fuzz-negative-control
	@if $(MAKE) --no-print-directory tables-luau-release \
		LUAU_WIREFUZZ_DRIVER="build/luaufuzzhost/luaufuzzhost build/luau-wire-fuzz-nc/test/conformance/luau/wirefuzz.luau" \
		LUAU_WIRE_FUZZ_LOG=build/wire-fuzz/luau-release-nc.log LUAU_WIRE_FUZZ_ABSENT=build/wire-fuzz/luau-release-nc.absent \
		LUAU_WIRE_FUZZ_FAILED=build/wire-fuzz/luau-release-nc.failed.bin \
		> build/luau-release-nc.log 2>&1; then \
		cat build/luau-release-nc.log; \
		echo "NEGATIVE CONTROL FAILED: the release gate stayed green over a reader that does not count a string frame past its body"; exit 1; fi
	@grep -q 'the report differs' build/wire-fuzz/luau-release-nc.log || { cat build/luau-release-nc.log; \
		echo "NEGATIVE CONTROL FAILED: the release gate went red, but not on the report the plant moves"; exit 1; }
	@echo "luau release negative control: the planted reader turns the release gate RED on the report"

# M19: the writer holds the map sort (map_sort_test.go): every mapdemo instance
# saved as read and saved with every map's entries reversed must be the same
# committed bytes; the control removes the sort from the emitted save and
# requires rows to turn. The reader's ascending, duplicate and key-kind rules
# are held against the engine by the wire-fuzz differential (make/luau-fuzz.mk).
.PHONY: tables-luau-maps tables-luau-maps-negative-control
tables-luau-maps: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauMapWriterHoldsTheSort$$' -count=1 -timeout 600s
tables-luau-maps-negative-control: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauMapWriterSortNegativeControl$$' -count=1 -timeout 600s -v

test-luau: tables-luau-maps tables-luau-maps-negative-control

# J1: the accessor and descriptor agreement, in its Luau form
# (layoutagree_test.go): the cook reader's own layout arithmetic against the
# compiler's ir.RecordLayout for every record of every cook-carrying unit; the
# control widens the pointer slot by eight and requires exactly the records
# that hold one to turn.
.PHONY: tables-luau-accessor-descriptor-agreement tables-luau-accessor-negative-control
tables-luau-accessor-descriptor-agreement: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauLayoutAgreesWithTheCompiler$$' -count=1 -timeout 600s -v
tables-luau-accessor-negative-control: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauLayoutAgreementNegativeControl$$' -count=1 -timeout 600s -v

test-luau: tables-luau-accessor-descriptor-agreement tables-luau-accessor-negative-control

# I6: every module-table key the Luau table emitter spells is registered and
# every registered Luau key is spelled (compiler/tablesluaunames_test.go); the
# control plants an unregistered key in each convention the scan reads and a
# registered name nothing emits.
.PHONY: tables-luau-names tables-luau-names-negative-control
tables-luau-names:
	go test ./compiler/ -run '^TestLuauTableRuntimeNamesAreClaimed$$' -count=1 -timeout 600s
tables-luau-names-negative-control:
	go test ./compiler/ -run '^TestLuauRuntimeNameScanGoesRed$$' -count=1 -timeout 600s

test-luau: tables-luau-names tables-luau-names-negative-control

# M18: an arm is a field line (armfield_test.go): read_arm checks framing and
# reads through read_field, and read_union leaves every payload to it; the
# control plants a per-shape reader into each and requires the scan to name it.
# The behaviour is the engine's: TestLuauLoadUnionFramingAgreesWithTheEngine
# and the wire-fuzz differential.
.PHONY: tables-luau-arms tables-luau-arms-negative-control
tables-luau-arms: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauArmIsAFieldLine$$' -count=1 -timeout 600s
	sh test/slowgate/proof tables-luau-arms 'TestLuauLoadUnionFramingAgreesWithTheEngine' \
		./internal/codegen/luautable -run '^TestLuauLoadUnionFramingAgreesWithTheEngine$$' -count=1 -timeout 600s
tables-luau-arms-negative-control: toolchain-luau
	go test ./internal/codegen/luautable/ -run '^TestLuauArmFieldLineNegativeControl$$' -count=1 -timeout 600s

test-luau: tables-luau-arms tables-luau-arms-negative-control
test-luau: conformance-negative-control-luau conformance-negative-control-luau-emitter conformance-negative-control-luau-walk
