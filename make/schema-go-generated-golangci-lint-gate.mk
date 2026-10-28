# make/schema-go-generated-golangci-lint-gate.mk - the golangci-lint lens on
# the Go port's GENERATED output (the schema-per-language-lenses group).
#
# The packet emitter's output (examples/, examples128/) and the Go table
# emitter's output (the corpus build/tables-generated-go/ reads) are checked
# with golangci-lint's errcheck, gosec and ineffassign, with go vet and a -race
# test run beside them. Nothing here reimplements a checker: the pinned tool is
# probed by name and version, and a missing tool is reported, never faked.
#
# ZERO IS NOT THE GOAL: the gate is a RATCHET. The per-linter (and per-gosec-
# rule) counts at head live in make/checks/schema-go-generated-golangci-lint-
# gate.ratchet.tsv. A finding may stay or fall; a count that grows, or a linter
# or G-rule that appears, fails the gate naming both counts. Fixing a finding
# lowers its pin in the same commit.
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
GOLANGCI_GATE_ACTUAL ?= $(GOLANGCI_GATE_DIR)/actual.tsv

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
	@printf 'module example\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n' "$(GOLANGCI_GATE_RUNTIME)" > $(GOLANGCI_GATE_DIR)/example/go.mod
	@printf 'module ludicrous\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n' "$(GOLANGCI_GATE_RUNTIME)" > $(GOLANGCI_GATE_DIR)/ludicrous/go.mod
	@touch $@

.PHONY: schema-go-generated-golangci-lint-gate-probe
schema-go-generated-golangci-lint-gate-probe:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "tool missing: golangci-lint (pinned $(GOLANGCI_LINT_VERSION); install it with rowan-tools/fleet)"; exit 1; }
	@$(GOLANGCI_LINT) version 2>/dev/null | grep -q '$(GOLANGCI_LINT_VERSION)' || { \
		echo "tool missing: golangci-lint $(GOLANGCI_LINT_VERSION) (found: $$($(GOLANGCI_LINT) version 2>/dev/null || echo none))"; exit 1; }

# golangci_gate_count DIR CORPUS: lint one generated module and append its
# findings to the measured TSV, one row per linter (gosec per G-rule). Exit 1
# is "findings exist" and is expected under a ratchet; any other status (tool,
# config or load failure) is a hard error, never folded into a count.
define golangci_gate_count
	@cd $(1) && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" \
		--output.text.colors=false --output.text.print-issued-lines=false ./... \
		> "$(CURDIR)/$(GOLANGCI_GATE_DIR)/$(2).lint.log" 2>&1; \
		rc=$$?; if [ $$rc -gt 1 ]; then cat "$(CURDIR)/$(GOLANGCI_GATE_DIR)/$(2).lint.log"; exit $$rc; fi
	@awk -v corpus="$(2)" '\
		/\([a-z]+\)$$/ { \
			n=$$0; sub(/.*\(/,"",n); sub(/\)$$/,"",n); \
			r="-"; if (n=="gosec" && match($$0,/G[0-9][0-9][0-9]/)) r=substr($$0,RSTART,RLENGTH); \
			c[corpus "\t" n "\t" r]++ \
		} \
		END { for(k in c) print k "\t" c[k] }' \
		"$(CURDIR)/$(GOLANGCI_GATE_DIR)/$(2).lint.log" >> "$(CURDIR)/$(GOLANGCI_GATE_ACTUAL)"
endef

# vet and the race build/test: a module with no tests still gets its generated
# package built under the race detector.
define golangci_gate_module_tests
	@cd $(1) && go vet ./...
	@cd $(1) && go test -race -count=1 -timeout 600s ./...
endef

# golangci_gate_ratchet: compare the measured TSV (second file) with the pins
# (first file). Growth and new keys fail; equal or lower counts pass. The
# failure names the corpus, the linter, the rule and both counts.
.PHONY: schema-go-generated-golangci-lint-gate-ratchet
schema-go-generated-golangci-lint-gate-ratchet:
	@awk -F'\t' ' \
		NR==FNR { if ($$0 ~ /^#/ || NF < 4) next; \
			pinned[$$1 SUBSEP $$2 SUBSEP $$3]=$$4; next } \
		NF >= 4 { measured[$$1 SUBSEP $$2 SUBSEP $$3]=$$4 } \
		END { \
			bad=0; \
			for (k in measured) { \
				split(k, a, SUBSEP); \
				p = (k in pinned) ? pinned[k] : 0; \
				if (measured[k] + 0 > p + 0) { \
					printf "RATCHET FAILED: %s: %s rule %s count %d > pinned %d\n", \
						a[1], a[2], a[3], measured[k], p; \
					bad=1; \
				} \
			} \
			if (bad) exit 1; \
		}' "$(GOLANGCI_GATE_RATCHET)" "$(GOLANGCI_GATE_ACTUAL)"

.PHONY: schema-go-generated-golangci-lint-gate
schema-go-generated-golangci-lint-gate: schema-go-generated-golangci-lint-gate-probe $(GOLANGCI_GATE_CONFIG) $(GOLANGCI_GATE_STAMP)
	@: > $(GOLANGCI_GATE_ACTUAL)
	$(call golangci_gate_count,$(GOLANGCI_GATE_DIR)/example,example)
	$(call golangci_gate_count,$(GOLANGCI_GATE_DIR)/ludicrous,ludicrous)
	@$(MAKE) --no-print-directory schema-go-generated-golangci-lint-gate-ratchet
	$(call golangci_gate_module_tests,$(GOLANGCI_GATE_DIR)/example)
	$(call golangci_gate_module_tests,$(GOLANGCI_GATE_DIR)/ludicrous)
	@echo "schema Go generated golangci-lint gate: errcheck, gosec, ineffassign, go vet and -race within the ratchet on the packet corpus"

# Nightly: the table corpus. Its units are separate modules and the shared
# runtime (docs/SPEC-TABLES.md §8.4) is imported from this checkout, so each
# module is wired to the sibling runtime and this module before the checks. All
# units are counted into one "tables" corpus and held to the same ratchet; vet
# runs beside the lint.
.PHONY: schema-go-generated-golangci-lint-gate-nightly
schema-go-generated-golangci-lint-gate-nightly: schema-go-generated-golangci-lint-gate-probe $(GOLANGCI_GATE_CONFIG)
	$(MAKE) --no-print-directory build/tables-generated-go/.stamp
	@for d in build/tables-generated-go/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		m=$$(sed -n 's/^module //p' "$$d/go.mod"); \
		printf 'module %s\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace github.com/mas-bandwidth/schema/v2 => %s\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n' \
			"$$m" "$(CURDIR)" "$(GOLANGCI_GATE_RUNTIME)" > "$$d/go.mod"; \
	done
	@: > $(GOLANGCI_GATE_DIR)/tables.lint.log
	@for d in build/tables-generated-go/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		( cd "$$d" && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" \
			--output.text.colors=false --output.text.print-issued-lines=false ./... ) \
			>> "$(CURDIR)/$(GOLANGCI_GATE_DIR)/tables.lint.log" 2>&1; \
		rc=$$?; if [ $$rc -gt 1 ]; then cat "$(CURDIR)/$(GOLANGCI_GATE_DIR)/tables.lint.log"; exit $$rc; fi; \
	done
	@awk -v corpus="tables" '\
		/\([a-z]+\)$$/ { \
			n=$$0; sub(/.*\(/,"",n); sub(/\)$$/,"",n); \
			r="-"; if (n=="gosec" && match($$0,/G[0-9][0-9][0-9]/)) r=substr($$0,RSTART,RLENGTH); \
			c[corpus "\t" n "\t" r]++ \
		} \
		END { for(k in c) print k "\t" c[k] }' \
		"$(CURDIR)/$(GOLANGCI_GATE_DIR)/tables.lint.log" > "$(CURDIR)/$(GOLANGCI_GATE_DIR)/tables.actual.tsv"
	@$(MAKE) --no-print-directory schema-go-generated-golangci-lint-gate-ratchet \
		GOLANGCI_GATE_ACTUAL=$(GOLANGCI_GATE_DIR)/tables.actual.tsv
	@for d in build/tables-generated-go/*/; do \
		[ -f "$$d/go.mod" ] || continue; \
		( cd "$$d" && go vet ./... ) || exit 1; \
	done
	@echo "schema Go generated golangci-lint gate (nightly): errcheck, gosec, ineffassign and go vet within the ratchet on the table corpus"

# THE NEGATIVE CONTROL. A scratch copy of the generated packet output gets one
# unchecked error (errcheck's oldest pattern) in a file that carries the same
# "Code generated ... DO NOT EDIT." header as the real corpus, so the control
# also proves linters.exclusions.generated: disable is in force. The ratchet
# goes red and names errcheck with both counts (403 > pinned 402); the same
# tree is green once the plant is removed.
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
	@cd $(GOLANGCI_GATE_DIR)/plant && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" \
		--output.text.colors=false --output.text.print-issued-lines=false ./... \
		> "$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant.log" 2>&1; \
		rc=$$?; if [ $$rc -ne 1 ]; then echo "NEGATIVE CONTROL FAILED: the planted unchecked error did not red the checker (status $$rc)"; cat "$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant.log"; exit 1; fi
	@grep -q 'planted_bad.go' $(GOLANGCI_GATE_DIR)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the checker went red, but not on the planted file"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; }
	@grep -q 'errcheck' $(GOLANGCI_GATE_DIR)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the checker named the file but not errcheck"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; }
	@grep -q 'os.Chdir' $(GOLANGCI_GATE_DIR)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the checker did not name the planted pattern"; \
		cat $(GOLANGCI_GATE_DIR)/plant.log; exit 1; }
	@awk -v corpus="example" '\
		/\([a-z]+\)$$/ { \
			n=$$0; sub(/.*\(/,"",n); sub(/\)$$/,"",n); \
			r="-"; if (n=="gosec" && match($$0,/G[0-9][0-9][0-9]/)) r=substr($$0,RSTART,RLENGTH); \
			c[corpus "\t" n "\t" r]++ \
		} \
		END { for(k in c) print k "\t" c[k] }' \
		"$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant.log" > "$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant.actual.tsv"
	@if $(MAKE) --no-print-directory schema-go-generated-golangci-lint-gate-ratchet \
		GOLANGCI_GATE_ACTUAL=$(GOLANGCI_GATE_DIR)/plant.actual.tsv > $(GOLANGCI_GATE_DIR)/plant.ratchet.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the ratchet stayed green on the planted unchecked error"; \
		cat $(GOLANGCI_GATE_DIR)/plant.ratchet.log; exit 1; fi
	@grep -q 'errcheck' $(GOLANGCI_GATE_DIR)/plant.ratchet.log || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet did not name errcheck"; cat $(GOLANGCI_GATE_DIR)/plant.ratchet.log; exit 1; }
	@grep -q 'count 403 > pinned 402' $(GOLANGCI_GATE_DIR)/plant.ratchet.log || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet did not name both counts"; cat $(GOLANGCI_GATE_DIR)/plant.ratchet.log; exit 1; }
	@rm -f $(GOLANGCI_GATE_DIR)/plant/planted_bad.go
	@cd $(GOLANGCI_GATE_DIR)/plant && $(GOLANGCI_LINT) run --config "$(CURDIR)/$(GOLANGCI_GATE_CONFIG)" \
		--output.text.colors=false --output.text.print-issued-lines=false ./... \
		> "$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant-clean.log" 2>&1 || :; \
		awk -v corpus="example" '\
			/\([a-z]+\)$$/ { \
				n=$$0; sub(/.*\(/,"",n); sub(/\)$$/,"",n); \
				r="-"; if (n=="gosec" && match($$0,/G[0-9][0-9][0-9]/)) r=substr($$0,RSTART,RLENGTH); \
				c[corpus "\t" n "\t" r]++ \
			} \
			END { for(k in c) print k "\t" c[k] }' \
			"$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant-clean.log" > "$(CURDIR)/$(GOLANGCI_GATE_DIR)/plant-clean.actual.tsv"
	@$(MAKE) --no-print-directory schema-go-generated-golangci-lint-gate-ratchet \
		GOLANGCI_GATE_ACTUAL=$(GOLANGCI_GATE_DIR)/plant-clean.actual.tsv > $(GOLANGCI_GATE_DIR)/plant-clean.ratchet.log 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet stayed red after the plant was removed"; \
		cat $(GOLANGCI_GATE_DIR)/plant-clean.ratchet.log; exit 1; }
	@echo "schema Go generated golangci-lint negative control: the plant is red and named, the same tree is green without it"

# THE RATCHET CONTROL. The pins themselves are perturbed by one (example
# errcheck 402 -> 401), simulating a finding growth with no code change; the
# ratchet must go red naming errcheck and both counts. The unperturbed pins
# must stay green, so the control cannot pass by failing everything.
.PHONY: schema-go-generated-golangci-lint-gate-negative-control-ratchet
schema-go-generated-golangci-lint-gate-negative-control-ratchet:
	@mkdir -p $(GOLANGCI_GATE_DIR)
	@awk -F'\t' 'BEGIN{OFS="\t"} $$0 ~ /^#/ || NF < 4 {print; next} \
		$$1=="example" && $$2=="errcheck" {$$4=401} {print}' \
		$(GOLANGCI_GATE_RATCHET) > $(GOLANGCI_GATE_DIR)/ratchet-minus-one.tsv
	@awk -F'\t' 'BEGIN{OFS="\t"} $$0 ~ /^#/ || NF < 4 {next} {print}' \
		$(GOLANGCI_GATE_RATCHET) > $(GOLANGCI_GATE_DIR)/actual-at-pin.tsv
	@if $(MAKE) --no-print-directory schema-go-generated-golangci-lint-gate-ratchet \
		GOLANGCI_GATE_ACTUAL=$(GOLANGCI_GATE_DIR)/actual-at-pin.tsv \
		GOLANGCI_GATE_RATCHET=$(GOLANGCI_GATE_DIR)/ratchet-minus-one.tsv \
		> $(GOLANGCI_GATE_DIR)/ratchet-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the ratchet stayed green when a pinned count was raised by one"; \
		cat $(GOLANGCI_GATE_DIR)/ratchet-control.log; exit 1; fi
	@grep -q 'errcheck' $(GOLANGCI_GATE_DIR)/ratchet-control.log || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet did not name errcheck"; cat $(GOLANGCI_GATE_DIR)/ratchet-control.log; exit 1; }
	@grep -q 'count 402 > pinned 401' $(GOLANGCI_GATE_DIR)/ratchet-control.log || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet did not name both counts"; cat $(GOLANGCI_GATE_DIR)/ratchet-control.log; exit 1; }
	@$(MAKE) --no-print-directory schema-go-generated-golangci-lint-gate-ratchet \
		GOLANGCI_GATE_ACTUAL=$(GOLANGCI_GATE_DIR)/actual-at-pin.tsv \
		> $(GOLANGCI_GATE_DIR)/ratchet-control-green.log 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the unperturbed pins did not pass"; \
		cat $(GOLANGCI_GATE_DIR)/ratchet-control-green.log; exit 1; }
	@echo "schema Go generated golangci-lint ratchet control: one raised pin is red and named, the pins themselves are green"
