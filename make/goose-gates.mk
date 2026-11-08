# make/goose-gates.mk — the Goose leg's gates over what it EMITS
# (docs/PORT-GOOSE.md; docs/PORTING.md J2, J3, J4 and I2). make/goose.mk is the
# leg; this file is the gates that hold its output, each with a negative control
# that fails exactly its rows (make/negative-controls.json, group goose-gates).
# The stem has a dash, so the port list never probes it as a language.
#
# Every target here generates its own tree from the tree's sources with the
# compiler it builds, so a stale build/ never answers for the head.

GOOSE_GATES_DIR := build/goose-gates
GOOSE_GATES_SCHEMA := build/schema-goose-gates

# goose_gates_generate COMPILER OUTDIR: the strict gate's corpus, written by
# COMPILER — every unit of the conformance manifest the target generates (a
# unit it refuses is that target's refusal, recorded by build/conformance-goose,
# and is not Goose output), the packet corpora examples/ and examples128/, and
# the fixed form's own vectors (test/tables FX1, FX2, FU1, FU2, the unions and
# the nested records the manifest's units do not reach).
define goose_gates_generate
	@rm -rf $(2) && mkdir -p $(2)/units
	@grep '^unit ' $(GOOSE_MANIFEST) | while read -r _ key paths; do \
		$(1) generate --lang goose --out $(2)/units/$$key $$paths >/dev/null 2>&1 || rm -rf $(2)/units/$$key; \
	done
	@$(1) generate --lang goose --out $(2)/examples examples
	@$(1) generate --lang goose --out $(2)/examples128 examples128
	@for s in FX1 FX2 FU1 FU2; do \
		$(1) generate --lang goose --out $(2)/fixed-$$s test/tables/$$s.schema || exit 1; \
	done
endef

.PHONY: $(GOOSE_GATES_SCHEMA)
$(GOOSE_GATES_SCHEMA):
	@mkdir -p build
	go build -tags schema_leg_goose -o $@ ./cmd/schema

# THE STRICT GATE (docs/PORTING.md J4). Goose v2026.2 ships no formatter and
# no warnings-as-errors switch; its compiler's warnings are its analyzer (the
# redundant casts and the redundant `&`s of the pinned Goose spec, §6.3 and §4.1). So
# make/goose-strict.sh checks EVERY generated module — the type and table
# modules and the runtime modules emitted beside them (schema_rt, schema_tables,
# fixed) — on its own, from its own directory, with `goose check` under the
# pinned compiler, and any warning or any module that does not typecheck alone
# is red. (The conformance driver is not in the corpus: it imports the glue
# module its shell driver writes per unit, so it does not check on its own.)
# Its first run found 586 redundant casts across the emitted fixed form
# and its runtime, and every <Base>Table.goose module failing alone because it
# named schema_rt::FixedReport without importing fixed: both fixed in the
# emitter (internal/codegen/goosetable/fixedform.go, runtime/fixed.goose).
.PHONY: tables-goose-strict
tables-goose-strict: $(GOOSE_GATES_SCHEMA)
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	$(call goose_gates_generate,./$(GOOSE_GATES_SCHEMA),$(GOOSE_GATES_DIR)/strict)
	@sh make/goose-strict.sh "$(GOOSE)" $(GOOSE_GATES_DIR)/strict/units $(GOOSE_GATES_DIR)/strict/examples \
		$(GOOSE_GATES_DIR)/strict/examples128 $(GOOSE_GATES_DIR)/strict/fixed-FX1 $(GOOSE_GATES_DIR)/strict/fixed-FX2 \
		$(GOOSE_GATES_DIR)/strict/fixed-FU1 $(GOOSE_GATES_DIR)/strict/fixed-FU2

# THE STRICT GATE'S PLANT. One redundant cast — the warning class the gate
# found 586 of — planted at the end of a copy of one generated module
# (tblk2's K2.goose); the gate must go red with exactly ONE finding, on that
# module at the planted line, and the same copy without the plant must be
# green. A gate that went red on anything else, or on more, is refused.
.PHONY: tables-goose-strict-negative-control
tables-goose-strict-negative-control: $(GOOSE_GATES_SCHEMA)
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_GATES_DIR)/plant && mkdir -p $(GOOSE_GATES_DIR)/plant
	@./$(GOOSE_GATES_SCHEMA) generate --lang goose --out $(GOOSE_GATES_DIR)/plant/tblk2 test/tables/K2.schema
	@sh make/goose-strict.sh "$(GOOSE)" $(GOOSE_GATES_DIR)/plant >$(GOOSE_GATES_DIR)/plant-clean.log 2>&1 || { \
		cat $(GOOSE_GATES_DIR)/plant-clean.log; echo "NEGATIVE CONTROL FAILED: the copy is red before the plant"; exit 1; }
	@printf '\nfn _planted_redundant_cast(n: i32) -> i32 { return n as i32; }\n' >>$(GOOSE_GATES_DIR)/plant/tblk2/K2.goose
	@line=$$(wc -l <$(GOOSE_GATES_DIR)/plant/tblk2/K2.goose | tr -d ' '); \
	if sh make/goose-strict.sh "$(GOOSE)" $(GOOSE_GATES_DIR)/plant >$(GOOSE_GATES_DIR)/plant.log 2>&1; then \
		cat $(GOOSE_GATES_DIR)/plant.log; echo "NEGATIVE CONTROL FAILED: the strict gate stayed green on a planted redundant cast"; exit 1; fi; \
	grep -q "^$(GOOSE_GATES_DIR)/plant/tblk2/K2.goose:$$line: warning: redundant \`as i32\`" $(GOOSE_GATES_DIR)/plant.log || { \
		cat $(GOOSE_GATES_DIR)/plant.log; echo "NEGATIVE CONTROL FAILED: the gate went red but did not name K2.goose:$$line's redundant cast"; exit 1; }; \
	grep -q 'GOOSE STRICT GATE FAILED: 1 finding(s)' $(GOOSE_GATES_DIR)/plant.log || { \
		cat $(GOOSE_GATES_DIR)/plant.log; echo "NEGATIVE CONTROL FAILED: the gate named more than the one planted row"; exit 1; }; \
	echo "goose strict negative control: the planted redundant cast is red at K2.goose:$$line, and only it; the copy without it is green"

# THE STRICT GATE'S EMITTER SABOTAGE (docs/PORTING.md I2): a copy of
# internal/codegen/goosetable/fixedform.go whose fixed-root load names a report
# type no module declares (schema_rt::FixedReportUnreachable, the sed checked to
# have matched), built into a private compiler through `go build -overlay` — no
# tracked file is written — and the gate run over the units it writes. The
# binding is unreachable through every import path, direct or through another
# runtime module, so the check cannot be satisfied by an import the module
# happens to inherit. Red is required on EXACTLY the <Base>Table.goose modules
# that name it, each finding placed on a line that names it, and on nothing
# else: the runtime and type modules beside them stay green.
# A module that imports a sabotaged one (the unit module imports every Table
# module) exits on that module's planted line: the same finding, reached
# through the import, so its exit is required to cite a planted module.
GOOSE_GATES_SED_UNREACHABLE := s|report = schema_rt::FixedReport { .. };|report = schema_rt::FixedReportUnreachable { .. }; // SABOTAGED: a binding no module declares|
.PHONY: tables-goose-strict-emitter-negative-control
tables-goose-strict-emitter-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@mkdir -p $(GOOSE_GATES_DIR)
	@sed '$(GOOSE_GATES_SED_UNREACHABLE)' internal/codegen/goosetable/fixedform.go >$(GOOSE_GATES_DIR)/fixedform-unreachable.gotext
	@grep -q SABOTAGED $(GOOSE_GATES_DIR)/fixedform-unreachable.gotext || { \
		echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goosetable/fixedform.go":"%s/$(GOOSE_GATES_DIR)/fixedform-unreachable.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" >$(GOOSE_GATES_DIR)/fixedform-overlay.json
	@go build -tags schema_leg_goose -overlay=$(GOOSE_GATES_DIR)/fixedform-overlay.json -o build/schema-goose-unreachable ./cmd/schema
	@rm -rf $(GOOSE_GATES_DIR)/sabotage && mkdir -p $(GOOSE_GATES_DIR)/sabotage
	@./build/schema-goose-unreachable generate --lang goose --out $(GOOSE_GATES_DIR)/sabotage/tabledemo tables/examples >/dev/null 2>&1
	@./build/schema-goose-unreachable generate --lang goose --out $(GOOSE_GATES_DIR)/sabotage/tblk2 test/tables/K2.schema
	@if sh make/goose-strict.sh "$(GOOSE)" $(GOOSE_GATES_DIR)/sabotage >$(GOOSE_GATES_DIR)/sabotage.log 2>&1; then \
		cat $(GOOSE_GATES_DIR)/sabotage.log; echo "NEGATIVE CONTROL FAILED: the strict gate stayed green on an emitter that names an unreachable binding"; exit 1; fi
	@want=$$(cd $(GOOSE_GATES_DIR)/sabotage && grep -l 'schema_rt::FixedReportUnreachable' */*Table.goose | LC_ALL=C sort); \
	got=$$(grep -E '^$(GOOSE_GATES_DIR)/sabotage/[^:]+\.goose:[0-9]+: ' $(GOOSE_GATES_DIR)/sabotage.log | sed -E 's|^$(GOOSE_GATES_DIR)/sabotage/([^:]+):.*|\1|' | LC_ALL=C sort -u); \
	if [ -z "$$want" ]; then echo "NEGATIVE CONTROL FAILED: no Table module names the fixed runtime — the control watches nothing"; exit 1; fi; \
	if [ "$$want" != "$$got" ]; then \
		cat $(GOOSE_GATES_DIR)/sabotage.log; \
		echo "NEGATIVE CONTROL FAILED: the gate went red on [$$(echo $$got)], expected exactly [$$(echo $$want)]"; exit 1; fi; \
	exits=$$(grep -E '^$(GOOSE_GATES_DIR)/sabotage/[^:]+: goose check exits ' $(GOOSE_GATES_DIR)/sabotage.log | sed -E 's|^.*: goose check exits [0-9]+: ([^:]+):[0-9]+: .*$$|\1|' | LC_ALL=C sort -u); \
	for m in $$exits; do echo "$$want" | grep -q "/$$m$$" || { cat $(GOOSE_GATES_DIR)/sabotage.log; echo "NEGATIVE CONTROL FAILED: a module exits on $$m, which the sabotage did not plant"; exit 1; }; done; \
	other=$$(grep -E '^$(GOOSE_GATES_DIR)/sabotage/' $(GOOSE_GATES_DIR)/sabotage.log | grep -v 'unknown type: schema_rt::FixedReportUnreachable'); \
	if [ -n "$$other" ]; then echo "$$other"; echo "NEGATIVE CONTROL FAILED: the gate named a finding the sabotage did not plant"; exit 1; fi; \
	placed=$$(grep -E '^$(GOOSE_GATES_DIR)/sabotage/[^:]+\.goose:[0-9]+: error: ' $(GOOSE_GATES_DIR)/sabotage.log); \
	[ -n "$$placed" ] || { cat $(GOOSE_GATES_DIR)/sabotage.log; echo "NEGATIVE CONTROL FAILED: no finding is placed on a line"; exit 1; }; \
	echo "$$placed" | while IFS=: read -r file line rest; do \
		sed -n "$${line}p" "$$file" | grep -q 'schema_rt::FixedReportUnreachable' || { \
			echo "NEGATIVE CONTROL FAILED: $$file:$$line does not name the planted binding"; exit 1; }; \
	done || exit 1; \
	echo "goose strict emitter negative control: an unreachable binding in the emitter reds exactly $$(echo $$want | wc -w | tr -d ' ') Table modules, each on the line that names it, and nothing else"

# THE RUNTIME HOME (docs/PORTING.md J2). The Goose runtime modules are emitted
# once per unit under their own names (schema_rt.goose, schema_tables.goose,
# fixed.goose; docs/PORT-GOOSE.md, THE RUNTIME LAYOUT) and imported relative
# to the importing file, so no schema file is their home. The gate holds that:
# tables/examples generated, then again with an Aaa.schema that sorts before
# every file; every module that is not a schema file's own (<Base>.goose,
# <Base>Table.goose) must be present both times with identical bytes, and each
# original file's <Base>Table.goose must not move a byte. (The unit's
# ProtocolId constant rides with the packet surface's ProtocolIdHome in every
# port, so <Base>.goose is not held here.)
.PHONY: tables-goose-runtime-home
tables-goose-runtime-home: $(GOOSE_GATES_SCHEMA)
	@sh make/goose-runtime-home.sh ./$(GOOSE_GATES_SCHEMA) $(GOOSE_GATES_DIR)/runtime-home

# THE RUNTIME HOME'S CONTROL: the file-order rule put back. A copy of
# internal/codegen/goosetable/goosetable.go whose runtime modules are written
# under the name of the file that sorts first (ir.ProtocolIdHome), built
# through `go build -overlay`; the gate must go red naming the runtime modules
# that moved, and only them.
GOOSE_GATES_SED_HOME := s|^	maps.Copy(out, Runtime())$$|	for k, v := range Runtime() { maps.Copy(out, map[string][]byte{ir.ProtocolIdHome(u) + "_" + k: v}) } // SABOTAGED: the file-order rule|
.PHONY: tables-goose-runtime-home-negative-control
tables-goose-runtime-home-negative-control:
	@mkdir -p $(GOOSE_GATES_DIR)
	@sed '$(GOOSE_GATES_SED_HOME)' internal/codegen/goosetable/goosetable.go >$(GOOSE_GATES_DIR)/goosetable-fileorder.gotext
	@grep -q SABOTAGED $(GOOSE_GATES_DIR)/goosetable-fileorder.gotext || { \
		echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goosetable/goosetable.go":"%s/$(GOOSE_GATES_DIR)/goosetable-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" >$(GOOSE_GATES_DIR)/goosetable-overlay.json
	@go build -tags schema_leg_goose -overlay=$(GOOSE_GATES_DIR)/goosetable-overlay.json -o build/schema-goose-fileorder ./cmd/schema
	@if sh make/goose-runtime-home.sh ./build/schema-goose-fileorder $(GOOSE_GATES_DIR)/runtime-home-sabotage >$(GOOSE_GATES_DIR)/runtime-home-sabotage.log 2>&1; then \
		cat $(GOOSE_GATES_DIR)/runtime-home-sabotage.log; echo "NEGATIVE CONTROL FAILED: the runtime home gate stayed green under the file-order rule"; exit 1; fi
	@for m in schema_tables.goose fixed.goose; do \
		grep -q "runtime module .*_$$m moved" $(GOOSE_GATES_DIR)/runtime-home-sabotage.log || { \
			cat $(GOOSE_GATES_DIR)/runtime-home-sabotage.log; echo "NEGATIVE CONTROL FAILED: the gate did not name $$m's move"; exit 1; }; \
	done
	@if grep -q 'Table.goose moved' $(GOOSE_GATES_DIR)/runtime-home-sabotage.log; then \
		cat $(GOOSE_GATES_DIR)/runtime-home-sabotage.log; echo "NEGATIVE CONTROL FAILED: the gate named a table module the sabotage did not touch"; exit 1; fi
	@echo "goose runtime home negative control: the file-order rule moves the table runtime and the gate names each module it moved"

# THE UNIT MODULE'S CONTROL: the file-order rule put back for the unit's own
# module. A copy of internal/codegen/goosetable/goosetable.go whose
# PackageModule names the registry's module for the file that sorts first
# (ir.ProtocolIdHome) rather than the package, built through `go build -overlay`; the gate must go red naming the
# registry's file-order homes, and no table module.
GOOSE_GATES_SED_UNIT_HOME := s|^	p := goose.Namespace(u)$$|	p := goose.Namespace(u); if surface == "unit" { p = ir.ProtocolIdHome(u) } // SABOTAGED: the file-order rule|
.PHONY: tables-goose-unit-home-negative-control
tables-goose-unit-home-negative-control:
	@mkdir -p $(GOOSE_GATES_DIR)
	@sed '$(GOOSE_GATES_SED_UNIT_HOME)' internal/codegen/goosetable/goosetable.go >$(GOOSE_GATES_DIR)/goosetable-unitorder.gotext
	@grep -q SABOTAGED $(GOOSE_GATES_DIR)/goosetable-unitorder.gotext || { \
		echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goosetable/goosetable.go":"%s/$(GOOSE_GATES_DIR)/goosetable-unitorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" >$(GOOSE_GATES_DIR)/goosetable-unit-overlay.json
	@go build -tags schema_leg_goose -overlay=$(GOOSE_GATES_DIR)/goosetable-unit-overlay.json -o build/schema-goose-unitorder ./cmd/schema
	@if sh make/goose-runtime-home.sh ./build/schema-goose-unitorder $(GOOSE_GATES_DIR)/unit-home-sabotage >$(GOOSE_GATES_DIR)/unit-home-sabotage.log 2>&1; then \
		cat $(GOOSE_GATES_DIR)/unit-home-sabotage.log; echo "NEGATIVE CONTROL FAILED: the runtime home gate stayed green with the unit module on the file order"; exit 1; fi
	@for f in Guarded_unit.goose Aaa_unit.goose; do \
		grep -q "unit-wide TableUnit in $$f" $(GOOSE_GATES_DIR)/unit-home-sabotage.log || { \
			cat $(GOOSE_GATES_DIR)/unit-home-sabotage.log; echo "NEGATIVE CONTROL FAILED: the gate did not name the registry in $$f"; exit 1; }; \
	done
	@if grep -q 'Table.goose moved' $(GOOSE_GATES_DIR)/unit-home-sabotage.log; then \
		cat $(GOOSE_GATES_DIR)/unit-home-sabotage.log; echo "NEGATIVE CONTROL FAILED: the gate named a table module the sabotage did not touch"; exit 1; fi
	@echo "goose unit home negative control: the file-order rule moves the unit registry and the gate names each file-order home"

# THE RELEASE TIER (docs/PORTING.md J3). certify.yml derives every
# `tables-<lang>-release` target from the makefiles, so this target is all it
# takes to run there. Today it is the leg's whole PR tier in one place — both
# Goose packages against the reference's fixed-form corpus, both gates above —
# and every Goose control, the fixed form's included, on the certification
# schedule; the wire fuzz's long seed (make/goose-fuzz.mk) is the instrument
# it scales, and joins here with it.
.PHONY: tables-goose-release
tables-goose-release:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	$(MAKE) test-goose
	$(MAKE) tables-goose-fixedform-negative-control tables-goose-strict-negative-control \
		tables-goose-strict-emitter-negative-control tables-goose-runtime-home-negative-control \
		tables-goose-unit-home-negative-control
	$(MAKE) conformance-negative-control-goose tables-goose-accel-negative-control \
		tables-goose-order-negative-control tables-goose-fuzz-negative-control \
		tables-goose-usage-negative-control tables-goose-names-negative-control \
		tables-goose-accessor-negative-control

# test-goose carries both gates (make/goose.mk); their controls run in the
# goose-gates group of make/negative-controls.json.
test-goose: tables-goose-strict tables-goose-runtime-home

# THE CLAIMED NAMES (docs/PORTING.md I6), Goose's form: the runtime declares
# into schema_rt, which no package can take, and every name the target writes
# into the package's namespace is claimed through goose.Gen.Declare, which
# refuses a second declaration by name. The gate declares each emitted
# top-level name in turn as a schema type beside every manifest unit and
# requires the unit refused. Its control lets every <T>CookOpen through
# Declare unclaimed (go test -overlay of internal/codegen/goose/goose.go), and
# the gate must name a CookOpen it writes without claiming.
GOOSE_NAMES_DIR := build/goose-names-control
.PHONY: tables-goose-names tables-goose-names-negative-control
tables-goose-names:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^(TestGooseEveryPackageNameIsClaimed|TestGooseNamesScanSeesTheRuntime)$$' ./internal/codegen/goosetable/
tables-goose-names-negative-control:
	@rm -rf $(GOOSE_NAMES_DIR) && mkdir -p $(GOOSE_NAMES_DIR)
	@sed 's|^func (g \*Gen) Declare(name string) {$$|func (g *Gen) Declare(name string) { if strings.HasSuffix(name, "CookOpen") { return } // SABOTAGED: unclaimed|' \
		internal/codegen/goose/goose.go > $(GOOSE_NAMES_DIR)/goose.go.txt
	@grep -q SABOTAGED $(GOOSE_NAMES_DIR)/goose.go.txt || { echo "NEGATIVE CONTROL FAILED: the Declare sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goose/goose.go":"%s/$(GOOSE_NAMES_DIR)/goose.go.txt"}}\n' "$(CURDIR)" "$(CURDIR)" > $(GOOSE_NAMES_DIR)/overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -timeout 600s -overlay=$(GOOSE_NAMES_DIR)/overlay.json -run '^TestGooseEveryPackageNameIsClaimed$$' \
			./internal/codegen/goosetable/ > $(GOOSE_NAMES_DIR)/log 2>&1; then \
		cat $(GOOSE_NAMES_DIR)/log; echo "NEGATIVE CONTROL FAILED: CookOpen names went unclaimed and the gate stayed green"; exit 1; fi
	@grep -q 'CookOpen into namespace .* without claiming it' $(GOOSE_NAMES_DIR)/log || { \
		cat $(GOOSE_NAMES_DIR)/log; echo "NEGATIVE CONTROL FAILED: the gate went red, but not on an unclaimed CookOpen"; exit 1; }
	@if grep 'without claiming it' $(GOOSE_NAMES_DIR)/log | grep -v 'CookOpen into namespace' | grep -q .; then \
		cat $(GOOSE_NAMES_DIR)/log; echo "NEGATIVE CONTROL FAILED: the gate named a name the sabotage did not unclaim"; exit 1; fi
	@echo "goose names negative control: $$(grep -c 'without claiming it' $(GOOSE_NAMES_DIR)/log) unclaimed CookOpen name(s) named, nothing else"

test-goose: tables-goose-names
