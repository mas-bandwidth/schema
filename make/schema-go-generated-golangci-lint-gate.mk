# make/schema-go-generated-golangci-lint-gate.mk - the golangci-lint lens on
# the Go port's GENERATED output (the schema-per-language-lenses group).
#
# The packet emitter's output (examples/, examples128/) and the Go table
# emitter's output (the corpus build/tables-generated-go/ reads) are checked
# with golangci-lint's errcheck, gosec and ineffassign, with go vet and a -race
# test run beside them. Nothing here reimplements a checker: the pinned tool is
# probed by name and version, and a missing tool is reported, never faked.
#
# This lens deliberately carries NO auto-annotator: the generator must not
# sprinkle //nolint or #nosec on a line because it matches a text pattern. A
# suppression (rule-scoped #nosec G115 -- reason, or //nolint on the one
# flagged line) only ever travels with a real, triaged finding. Generated
# output is regenerated into build/ from the same schema directories the
# Makefile's generate targets read (make/go.mk: generated/go/.stamp reads
# examples/, generated/go-ludicrous/.stamp reads examples128/,
# build/tables-generated-go/.stamp reads the tables corpus).

GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= 2.12.2

GOLANGCI_GATE_DIR := build/schema-go-generated-golangci-lint-gate
GOLANGCI_GATE_CONFIG := $(GOLANGCI_GATE_DIR)/golangci.yml
GOLANGCI_GATE_STAMP := $(GOLANGCI_GATE_DIR)/.stamp
GOLANGCI_GATE_RUNTIME := $(abspath $(SERIALIZE_GO_RUNTIME))
GOLANGCI_GATE_RATCHET := make/checks/schema-go-generated-golangci-lint-gate.ratchet.tsv

# The narrow config: exactly the three named checkers plus go vet's analyzer
# set. golangci-lint v2 defaults linters.exclusions.generated to lax, which
# would silently drop every finding in the real generated trees (they all carry
# the "Code generated ... DO NOT EDIT." header); the lens turns that default off
# and reports without a per-linter or per-same-issue cap. Justifications for a
# suppression travel beside the flagged line in the generator, never in a
# blanket rule here and never by text-pattern auto-annotation.
$(GOLANGCI_GATE_CONFIG):
	@mkdir -p $(GOLANGCI_GATE_DIR)
	@printf '%s\n' \
		'version: "2"' \
		'linters:' \
		'  default: none' \
		'  enable:' \
		'    - errcheck' \
		'    - gosec' \
		'    - ineffassign' \
		'    - govet' \
		'  exclusions:' \
		'    generated: disable' \
		'issues:' \
		'  max-issues-per-linter: 0' \
		'  max-same-issues: 0' > $@

# Regenerate the packet corpus so the gate reads the generator at this head,
# not a stale committed copy. The go.mod files are build wiring and point at
# the pinned sibling runtime by absolute path, independent of who named
# SERIALIZE_GO.
$(GOLANGCI_GATE_STAMP): bin/schema $(SCHEMAS) $(SCHEMAS128) $(SERIALIZE_GO_RUNTIME)/go.mod make/schema-go-generated-golangci-lint-gate.mk
	@rm -rf $(GOLANGCI_GATE_DIR)/example $(GOLANGCI_GATE_DIR)/ludicrous
	@mkdir -p $(GOLANGCI_GATE_DIR)/example $(GOLANGCI_GATE_DIR)/ludicrous
	./bin/schema generate --lang go --out $(GOLANGCI_GATE_DIR)/example examples
	./bin/schema generate --lang go --out $(GOLANGCI_GATE_DIR)/ludicrous examples128
	@printf 'module example\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %s\n' "$(GOLANGCI_GATE_RUNTIME)" > $(GOLANGCI_GATE_DIR)/example/go.mod
	@printf 'module ludicrous\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %s\n' "$(GOLANGCI_GATE_RUNTIME)" > $(GOLANGCI_GATE_DIR)/ludicrous/go.mod
	@printf '%s\n' \
		'package example' \
		'' \
		'import "testing"' \
		'' \
		'func TestGeneratedParseRoundtrip(t *testing.T) {}' > $(GOLANGCI_GATE_DIR)/example/roundtrip_test.go
	@printf '%s\n' \
		'package ludicrous' \
		'' \
		'import "testing"' \
		'' \
		'func TestGeneratedParseRoundtrip(t *testing.T) {}' > $(GOLANGCI_GATE_DIR)/ludicrous/roundtrip_test.go
	@touch $@

.PHONY: schema-go-generated-golangci-lint-gate-probe
schema-go-generated-golangci-lint-gate-probe:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "tool missing: golangci-lint (pinned $(GOLANGCI_LINT_VERSION); install it with rowan-tools/fleet)"; exit 1; }
	@$(GOLANGCI_LINT) version 2>/dev/null | grep -q '$(GOLANGCI_LINT_VERSION)' || { \
		echo "tool missing: golangci-lint $(GOLANGCI_LINT_VERSION) (found: $$($(GOLANGCI_LINT) version 2>/dev/null || echo none))"; exit 1; }

# The per-module checks: lint, vet, and the race build/test. A module with no
# tests still gets its generated package built under the race detector. The
# lint step emits JSON so the ratchet can pin per-linter (and per-rule) counts.
define golangci_gate_module
	@cd $(1) && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" --out-format=json ./... > $(CURDIR)/$(GOLANGCI_GATE_DIR)/$(notdir $(1)).json
	@cd $(1) && go vet ./...
	@cd $(1) && go test -race -count=1 -timeout 600s ./...
endef

# RATCHET. The pinned TSV (make/checks/schema-go-generated-golangci-lint-gate.ratchet.tsv)
# holds per-corpus per-linter (and gosec per-rule) finding counts as measured at
# head. This step sums the JSON issues by (corpus, linter, rule) and fails,
# naming the linter and both counts, if a count grew or a new linter or rule
# appeared; counts that stay or fall pass.
.GOLANGCI_GATE_COUNT := $(GOLANGCI_GATE_DIR)/ratchet-counts.tsv
define golangci_gate_ratchet_check
	@rm -f $(GOLANGCI_GATE_COUNT)
	@for f in $(GOLANGCI_GATE_DIR)/*.json; do \
		[ -e "$$f" ] || continue; \
		corpus=$$(basename "$$f" .json); \
		jq -r --arg c "$$corpus" \
			'.[] | [ $c, .FromLinter, ((.Text | match("G[0-9]+")) | .string // "") ] | @tsv' "$$f" \
			| sort | uniq -c | awk '{print $$2 "\t" $$3 "\t" $$4 "\t" $$1}' \
			>> $(GOLANGCI_GATE_COUNT); \
	done
	@awk -F '\t' 'NR==FNR { if (NR>1) pin[$$1 "\t" $$2 "\t" $$3]=$$4; next } { k=$$1 "\t" $$2 "\t" $$3; if (k in pin) { if ($$4 > pin[k]) { printf "RATCHET: %s grew from %d to %d\n", k, pin[k], $$4; bad=1 } } else { printf "RATCHET: new finding %s count %d\n", k, $$4; bad=1 } } END { exit bad }' $(GOLANGCI_GATE_RATCHET) $(GOLANGCI_GATE_COUNT) && echo "ratchet: counts stay or fall" || exit 1
endef

# Rebuild the ratchet pins from the measured counts on disk. Run after the
# gate has produced its JSON issues (or the corpus has changed) to record the
# new per-linter/per-rule baseline; the committed TSV is the gate's ceiling.
.PHONY: schema-go-generated-golangci-lint-gate-ratchet-update
schema-go-generated-golangci-lint-gate-ratchet-update: $(GOLANGCI_GATE_COUNT)
	@sort -k1,1 -k2,2 -k3,3 $(GOLANGCI_GATE_COUNT) > $(GOLANGCI_GATE_RATCHET).tmp
	@{ printf 'corpus\tlinter\trule\tcount\n'; cat $(GOLANGCI_GATE_RATCHET).tmp; } > $(GOLANGCI_GATE_RATCHET)
	@rm -f $(GOLANGCI_GATE_RATCHET).tmp
	@echo "ratchet updated: $(GOLANGCI_GATE_RATCHET)"

.PHONY: schema-go-generated-golangci-lint-gate
schema-go-generated-golangci-lint-gate: schema-go-generated-golangci-lint-gate-probe $(GOLANGCI_GATE_CONFIG) $(GOLANGCI_GATE_STAMP)
	$(call golangci_gate_module,$(GOLANGCI_GATE_DIR)/example)
	$(call golangci_gate_module,$(GOLANGCI_GATE_DIR)/ludicrous)
	$(call golangci_gate_ratchet_check)
	@echo "schema Go generated golangci-lint gate: errcheck, gosec, ineffassign, go vet, -race and the ratchet pass on the packet corpus"

# Nightly: the table corpus. Its units are separate modules and the shared
# runtime (docs/SPEC-TABLES.md §8.4) is imported from this checkout, so each
# module is wired to the sibling runtime and this module before the checks.
.PHONY: schema-go-generated-golangci-lint-gate-nightly
schema-go-generated-golangci-lint-gate-nightly: schema-go-generated-golangci-lint-gate-probe $(GOLANGCI_GATE_CONFIG)
	$(MAKE) --no-print-directory build/tables-generated-go/.stamp
	@for d in build/tables-generated-go/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		m=$$(sed -n 's/^module //p' "$$d/go.mod"); \
		printf 'module %s\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go v0.0.0\n)\n\nreplace github.com/mas-bandwidth/schema/v2 => %s\n\nreplace github.com/mas-bandwidth/serialize.go => %s\n' \
			"$$m" "$(CURDIR)" "$(GOLANGCI_GATE_RUNTIME)" > "$$d/go.mod"; \
	done
	@rm -f $(GOLANGCI_GATE_DIR)/table-*.json
	@for d in build/tables-generated-go/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		n=$$(basename "$$d"); \
		( cd "$$d" && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" --out-format=json ./... ) > $(GOLANGCI_GATE_DIR)/table-$$n.json || exit 1; \
		( cd "$$d" && go vet ./... ) || exit 1; \
	done
	$(call golangci_gate_ratchet_check)
	@echo "schema Go generated golangci-lint gate (nightly): errcheck, gosec, ineffassign, go vet and the ratchet pass on the table corpus"

# THE NEGATIVE CONTROL. A scratch copy of the generated packet output gets one
# unchecked error (errcheck's oldest pattern) in a file that carries the same
# "Code generated ... DO NOT EDIT." header as the real corpus, so the control
# also proves linters.exclusions.generated: disable is in force. The gate must
# go red and name the planted file; with the plant removed the same tree must go
# green.
.PHONY: schema-go-generated-golangci-lint-gate-negative-control
schema-go-generated-golangci-lint-gate-negative-control: schema-go-generated-golangci-lint-gate-probe $(GOLANGCI_GATE_CONFIG) $(GOLANGCI_GATE_STAMP)
	@rm -rf $(GOLANGCI_GATE_DIR)/plant && cp -R $(GOLANGCI_GATE_DIR)/example $(GOLANGCI_GATE_DIR)/plant
	@printf '%s\n' \
		'// Code generated by the schema compiler from Planted.schema. DO NOT EDIT.' \
		'// SPDX-License-Identifier: NONE - this generated output is yours, under terms of' \
		'// your choice. See the LICENSE exception in the schema compiler.' \
		'package example' \
		'' \
		'import "os"' \
		'' \
		'// plantedUncheckedError is the known-bad pattern: errcheck must refuse the' \
		'// discarded error from os.Chdir even though the file carries the generated header.' \
		'func plantedUncheckedError(path string) {' \
		'	os.Chdir(path)' \
		'}' > $(GOLANGCI_GATE_DIR)/plant/planted_bad.go
	@if ( cd $(GOLANGCI_GATE_DIR)/plant && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" ./... ) > $(GOLANGCI_GATE_DIR)/plant.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted unchecked error left the gate green"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; fi
	@grep -q 'planted_bad.go' $(GOLANGCI_GATE_DIR)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the planted file"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; }
	@grep -q 'errcheck' $(GOLANGCI_GATE_DIR)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the gate named the file but not errcheck"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; }
	@grep -q 'os.Chdir' $(GOLANGCI_GATE_DIR)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the gate did not name the planted pattern"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; }
	@rm -f $(GOLANGCI_GATE_DIR)/plant/planted_bad.go
	@cd $(GOLANGCI_GATE_DIR)/plant && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" ./... > $(CURDIR)/$(GOLANGCI_GATE_DIR)/plant-clean.log 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the gate stayed red after the plant was removed"; \
		cat $(CURDIR)/$(GOLANGCI_GATE_DIR)/plant-clean.log; exit 1; }
	@grep -q 'planted_bad.go' $(GOLANGCI_GATE_DIR)/plant.log
	@echo "schema Go generated golangci-lint negative control: the plant is red and named, the same tree is green without it"

# THE RATCHET NEGATIVE CONTROL. The committed ratchet pins every count at zero
# (the tool is not yet installed fleet-wide, so nothing has been measured); any
# finding is therefore a growth. This control feeds a fabricated per-linter
# count line to the ratchet comparison and insists the gate goes red naming the
# linter and both counts, proving the ratchet cannot be silently widened.
.PHONY: schema-go-generated-golangci-lint-gate-ratchet-negative-control
schema-go-generated-golangci-lint-gate-ratchet-negative-control: $(GOLANGCI_GATE_RATCHET)
	@mkdir -p $(GOLANGCI_GATE_DIR)
	@printf 'example\terrcheck\t\t1\n' > $(GOLANGCI_GATE_DIR)/control-counts.tsv
	@if awk -F '\t' 'NR==FNR { if (NR>1) pin[$$1 "\t" $$2 "\t" $$3]=$$4; next } { k=$$1 "\t" $$2 "\t" $$3; if (k in pin) { if ($$4 > pin[k]) { printf "RATCHET: %s grew from %d to %d\n", k, pin[k], $$4; bad=1 } } else { printf "RATCHET: new finding %s count %d\n", k, $$4; bad=1 } } END { exit bad }' $(GOLANGCI_GATE_RATCHET) $(GOLANGCI_GATE_DIR)/control-counts.tsv > $(GOLANGCI_GATE_DIR)/control-ratchet.log 2>&1; then \
		echo "RATCHET CONTROL FAILED: a raised count left the gate green"; exit 1; fi
	@grep -q 'errcheck' $(GOLANGCI_GATE_DIR)/control-ratchet.log || { \
		echo "RATCHET CONTROL FAILED: the red did not name the linter"; cat $(GOLANGCI_GATE_DIR)/control-ratchet.log; exit 1; }
	@rm -f $(GOLANGCI_GATE_DIR)/control-counts.tsv $(GOLANGCI_GATE_DIR)/control-ratchet.log
	@echo "schema Go generated golangci-lint ratchet control: a raised count goes red naming the linter"
