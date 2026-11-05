# make/moo-open-gates.mk — the MOO leg's gates for the docs/PORTING.md rows the
# other moo files do not hold (I12, J2, J3). Included by the Makefile's
# wildcard include; a dashed stem, so the port registry never probes it as a
# --lang. make/moo.mk is the leg and pins the toolchain; build/schema-moo is
# the compiler with the moo target (make/moo-conformance.mk).

# J2: the runtime home. A moo unit's shared runtime is the fixed set of modules
# the emitters embed (serialize*.moo, schema_tables.moo, table_*.moo, json.moo,
# textcarrier.moo), named by the runtime and never by the schema file that
# sorts first, and each schema file's modules are <Base>.moo and
# <Base>Table.moo, named by its own file. So a file that sorts earlier
# relocates nothing: every module keeps its name and every runtime module is
# byte-identical across the two trees.
MOO_HOME_DIR := build/runtime-home-moo
.PHONY: tables-moo-runtime-home tables-moo-runtime-home-negative-control
tables-moo-runtime-home: build/schema-moo
	@rm -rf $(MOO_HOME_DIR) && mkdir -p $(MOO_HOME_DIR)/base-src $(MOO_HOME_DIR)/src
	@cp tables/examples/*.schema $(MOO_HOME_DIR)/base-src/
	@cp tables/examples/*.schema $(MOO_HOME_DIR)/src/
	@printf 'package tabledemo\n\ntype AaaPacket\n{\n    tag uint8\n}\n' > $(MOO_HOME_DIR)/src/Aaa.schema
	@./build/schema-moo generate --lang moo --out $(MOO_HOME_DIR)/base $(MOO_HOME_DIR)/base-src
	@./build/schema-moo generate --lang moo --out $(MOO_HOME_DIR)/added $(MOO_HOME_DIR)/src
	@sh test/moo-runtime-home/check $(MOO_HOME_DIR)/base $(MOO_HOME_DIR)/added Aaa $(MOO_HOME_DIR)/src
	@echo "runtime home gate (moo): every schema module keeps its name and the runtime stays byte-identical when an earlier-sorting file joins the unit"

# Its NEGATIVE CONTROL: put the file-order rule back (the table runtime named
# by the file that sorts first, ir.ProtocolIdHome) through `go build -overlay`,
# and the home must MOVE when the earlier-sorting file joins.
tables-moo-runtime-home-negative-control: build/schema-moo tables-moo-runtime-home
	@sed 's|^\t\t\tout\[name\] = data$$|\t\t\tout[ir.ProtocolIdHome(u)+"_"+name] = data // SABOTAGED: back to the file order|' \
		internal/codegen/mootable/mootable.go > build/moo-runtime-home-fileorder.gotext
	@grep -q SABOTAGED build/moo-runtime-home-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/mootable/mootable.go":"%s/build/moo-runtime-home-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/moo-runtime-home-overlay.json
	@go build -tags schema_leg_moo -overlay=build/moo-runtime-home-overlay.json -o build/schema-moo-runtime-home-sabotaged ./cmd/schema
	@rm -rf $(MOO_HOME_DIR)/base-sabotage $(MOO_HOME_DIR)/added-sabotage
	@./build/schema-moo-runtime-home-sabotaged generate --lang moo --out $(MOO_HOME_DIR)/base-sabotage $(MOO_HOME_DIR)/base-src
	@./build/schema-moo-runtime-home-sabotaged generate --lang moo --out $(MOO_HOME_DIR)/added-sabotage $(MOO_HOME_DIR)/src
	@if sh test/moo-runtime-home/check $(MOO_HOME_DIR)/base-sabotage $(MOO_HOME_DIR)/added-sabotage Aaa $(MOO_HOME_DIR)/src > $(MOO_HOME_DIR)/control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule left the runtime home in place — the gate is watching nothing"; exit 1; \
	fi
	@grep -q 'RUNTIME HOME GATE FAILED' $(MOO_HOME_DIR)/control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate failed for another reason"; cat $(MOO_HOME_DIR)/control.log; exit 1; }
	@echo "runtime home negative control (moo): the file-order rule moves the runtime home, and the gate says so: $$(head -1 $(MOO_HOME_DIR)/control.log)"

test-moo: tables-moo-runtime-home tables-moo-runtime-home-negative-control

# I12: the documented surface. docs/USAGE.md's moo fragment (between the
# moo-table-usage markers) is run against the tables/examples unit under the
# pinned moo: a prelude gives it `use`, and the epilogue requires that it
# walked the three turrets and read back the damage it wrote (25.0, binary32).
USAGE_DOC ?= docs/USAGE.md
USAGE_MOO_DIR ?= build/moo-usage
.PHONY: tables-moo-usage tables-moo-usage-negative-control
tables-moo-usage: toolchain-moo build/schema-moo $(USAGE_DOC)
	@rm -rf $(USAGE_MOO_DIR) && mkdir -p $(USAGE_MOO_DIR)
	./build/schema-moo generate --lang moo --out $(USAGE_MOO_DIR) tables/examples
	printf 'local seen = {}\nlocal function use(x) seen[#seen + 1] = x end\n' > $(USAGE_MOO_DIR)/usage.moo
	sed -n '/<!-- moo-table-usage -->/,/<!-- \/moo-table-usage -->/p' $(USAGE_DOC) \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> $(USAGE_MOO_DIR)/usage.moo
	printf 'if #seen ~= 3 or seen[2] ~= 0x41C80000 then error("the walk read " .. #seen .. " turret(s), Missile " .. tostring(seen[2])) end\n' >> $(USAGE_MOO_DIR)/usage.moo
	printf 'print("usage: docs/USAGE.md moo example saved, read back and walked " .. #seen .. " turret(s)")\n' >> $(USAGE_MOO_DIR)/usage.moo
	grep -q 'S.keyed' $(USAGE_MOO_DIR)/usage.moo || \
		{ echo "MISSING: $(USAGE_DOC) carries no moo-table-usage example"; exit 1; }
	cd $(USAGE_MOO_DIR) && $(MOO) usage.moo

# THE REVERSED WITNESSES: two edited copies of the page, each checked to have
# changed, must each turn the gate red. The first renames the save the page
# calls (the surface moved: the run errors); the second walks nothing (the
# example runs but the epilogue's count refuses).
tables-moo-usage-negative-control:
	@rm -rf build/moo-usage-negative && mkdir -p build/moo-usage-negative
	sed 's/save\.save(hulls, hull)/save.sav(hulls, hull)/' $(USAGE_DOC) > build/moo-usage-negative/renamed.md
	sed '/<!-- moo-table-usage -->/,/<!-- \/moo-table-usage -->/s/^  use(turret\.Damage)$$/  local _ = turret.Damage/' $(USAGE_DOC) > build/moo-usage-negative/walks-nothing.md
	@for c in renamed walks-nothing; do \
		if cmp -s $(USAGE_DOC) build/moo-usage-negative/$$c.md; then \
			echo "NEGATIVE CONTROL: the $$c edit did not apply to $(USAGE_DOC)"; exit 1; fi; \
		if $(MAKE) tables-moo-usage USAGE_DOC=build/moo-usage-negative/$$c.md USAGE_MOO_DIR=build/moo-usage-negative/$$c > build/moo-usage-negative/$$c.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$c copy of the page passed the moo usage gate"; exit 1; fi; \
		echo "tables-moo-usage negative control: the $$c copy fails the gate"; \
	done

test-moo: tables-moo-usage tables-moo-usage-negative-control

# J3 — THE RELEASE TIER. The expensive half of the leg, behind one target
# certify.yml finds BY NAME: every instrument is the PR tier's scaled, none is
# new: the release wire-fuzz gate (make/wire-fuzz-release.mk: both arms, seeds
# 1-3, at the release count) with the pipe's own control beside it, and the
# documented surface with its reversed witnesses.
.PHONY: tables-moo-release
tables-moo-release: toolchain-moo build/conformance-harness build/conformance-moo
	$(MAKE) tables-wire-fuzz-release-moo
	$(MAKE) tables-moo-wire-fuzz-negative-control
	$(MAKE) tables-moo-usage
	$(MAKE) tables-moo-usage-negative-control
	@echo "tables moo release gate: the wire fuzz at release depth with its control, and the page's example with its witnesses"

# M18 — AN ARM IS A FIELD LINE (internal/codegen/mootable/armfield_test.go):
# table_load.moo's read_arm checks framing and reads through read_field, and
# read_union leaves every payload to it; the control plants a per-shape reader
# into each and requires the scan to name it. The behavior is the engine's: the
# conformance surfaces over the corpus's arms and the wire-fuzz differential.
.PHONY: tables-moo-arms tables-moo-arms-negative-control
tables-moo-arms:
	SCHEMA_SLOW=1 go test ./internal/codegen/mootable/ -run '^TestMooArmIsAFieldLine$$' -count=1

tables-moo-arms-negative-control:
	SCHEMA_SLOW=1 go test ./internal/codegen/mootable/ -run '^TestMooArmFieldLineNegativeControl$$' -count=1

test-moo: tables-moo-arms tables-moo-arms-negative-control

# I5: every block image at every lead 0..64, including the second aligned base.
.PHONY: tables-moo-block-lead tables-moo-block-lead-negative-control
tables-moo-block-lead: toolchain-moo
	SCHEMA_SLOW=1 go test ./internal/codegen/mootable -run '^TestMooBlockLead$$' -count=1 -v
tables-moo-block-lead-negative-control: tables-moo-block-lead
	SCHEMA_SLOW=1 go test ./internal/codegen/mootable -run '^TestMooBlockLeadNegativeControl$$' -count=1 -v

test-moo: tables-moo-block-lead
