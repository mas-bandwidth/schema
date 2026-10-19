# ANALYZER CI GATE OVER THE FULL CORPUS (schema-per-language-lenses, this card).
#
# Schema generates the same algorithm in every language, so each port's
# strictest analyzer, run over the generated output for the FULL test-schema
# corpus, is a separate lens on bugs. The sibling cards of the
# schema-per-language-lenses group each own one per-port analyzer gate. This
# card owns the single aggregate target: it calls every per-port gate that
# exists on the shared base and skips, loudly and by name, those not yet
# landed. It adds no needs on the sibling cards.
#
# The contract with a sibling card is one variable: a sibling sets
#   SCHEMA_SHARED_ANALYZER_GATE_<port> := its-analyzer-target
# in the same include tree and the aggregate runs it. Until a sibling lands,
# its port is skipped here, named, and the gate still passes. The coverage
# check fails only when a gate EXISTS but was left out of the corpus run.

SCHEMA_SHARED_ANALYZER_PORTS := c cpp cs dart elixir go java js rust

ANALYZER_RUNS = $(foreach p,$(SCHEMA_SHARED_ANALYZER_PORTS),$(if $(SCHEMA_SHARED_ANALYZER_GATE_$(p)),$(p)=$(SCHEMA_SHARED_ANALYZER_GATE_$(p))))
ANALYZER_SKIPS = $(foreach p,$(SCHEMA_SHARED_ANALYZER_PORTS),$(if $(SCHEMA_SHARED_ANALYZER_GATE_$(p)),,$(p)))

.PHONY: schema-shared-analyzer-ci-gate-corpus
schema-shared-analyzer-ci-gate-corpus:
	@set -e; run=""; \
	for row in $(ANALYZER_RUNS); do \
	  p=$${row%%=*}; g=$${row##*=}; \
	  run="$$run $$g"; \
	  echo "ANALYZER RUN: $$g (port $$p)"; \
	  $(MAKE) --no-print-directory $$g || exit 1; \
	done; \
	skip="$(ANALYZER_SKIPS)"; \
	for p in $$skip; do echo "ANALYZER SKIP: (port $$p) -- no analyzer gate landed on the shared base"; done; \
	echo "analyzer-summary: ran [$$run] skipped [$$skip]"; \
	for s in $(SCHEMA_SHARED_ANALYZER_STRAY_GATES); do \
	  case " $$run " in *" $$s "*) ;; *) echo "ANALYZER COVERAGE FAILED: $$s is defined but was not run over the corpus"; exit 1;; esac; \
	done

# NEGATIVE CONTROL. Green case: no stray gate is planted, so the aggregate
# passes and names every not-yet-landed port. Red case: a stray analyzer gate
# is planted outside the port set; the coverage check must take the aggregate
# down and name the plant.
.PHONY: schema-shared-analyzer-ci-gate-corpus-negative-control
schema-shared-analyzer-ci-gate-corpus-negative-control:
	@rm -rf build/analyzer-nc && mkdir -p build/analyzer-nc
	@if $(MAKE) --no-print-directory schema-shared-analyzer-ci-gate-corpus > build/analyzer-nc/green.log 2>&1; then \
		:; \
	else \
		echo "NEGATIVE CONTROL FAILED: the aggregate failed with no plant present"; \
		cat build/analyzer-nc/green.log; exit 1; \
	fi
	@printf 'SCHEMA_SHARED_ANALYZER_STRAY_GATES += schema-shared-analyzer-gate-stray\nschema-shared-analyzer-gate-stray:\n\t@echo stray plant\n' > build/analyzer-nc/stray.mk
	@if $(MAKE) --no-print-directory -f Makefile -f build/analyzer-nc/stray.mk schema-shared-analyzer-ci-gate-corpus > build/analyzer-nc/red.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the aggregate stayed green with a stray analyzer gate planted"; \
		cat build/analyzer-nc/red.log; exit 1; \
	fi; \
	grep -q "schema-shared-analyzer-gate-stray" build/analyzer-nc/red.log || { \
		echo "NEGATIVE CONTROL FAILED: red without naming the plant"; \
		cat build/analyzer-nc/red.log; exit 1; }
	@sed -n '1,2p' build/analyzer-nc/green.log
	@sed -n '1,2p' build/analyzer-nc/red.log
	@echo "negative control: a stray analyzer gate planted outside the port set takes the aggregate down and names it; without the plant the aggregate passes naming every not-yet-landed port"
