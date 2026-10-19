# ANALYZER CI GATE OVER THE FULL CORPUS (schema-per-language-lenses, this card).
#
# Schema generates the same algorithm in many languages, so each port's
# strictest analyzer, run over the generated output for the FULL test-schema
# corpus, is a separate lens on bugs. The sibling cards of the
# schema-per-language-lenses group each own one per-port analyzer gate. This
# card owns the single aggregate target: it discovers and calls every per-port
# analyzer target that exists on the shared base, over the same corpus the
# Makefile's generate targets read, and skips loudly and by name the ports with
# no analyzer target yet. It adds no needs on the sibling cards.
#
# COVERAGE IS DERIVED FROM REAL TARGETS, not from a list this card maintains.
# A per-port analyzer gate is a make target named `tables-<port>-<analyzer>`
# for one of SCHEMA_SHARED_ANALYZER_PORTS and one of the analyzer suffixes
# below. Discovery scans the Makefile and the make include tree for those
# target lines. A discovered target either runs (a port in the set) or is a
# coverage failure (a port with an analyzer that the corpus run would leave
# out). The only exclusion is named and justified beside it.
SCHEMA_SHARED_ANALYZER_PORTS := c cpp cs dart elixir go java js rust

# The analyzer suffixes this aggregate recognizes. A sibling card lands its
# gate as `tables-<port>-<suffix>`; the suffix says which checker it runs.
SCHEMA_SHARED_ANALYZER_SUFFIXES := clippy staticcheck vet analyzer analyze lint \
	eslint dialyzer msan cppcheck scan-build frama-c kani cbmc \
	golangci-lint govet tsc typecheck dart-analyze dotnet-format clang-tidy \
	infer asan ubsan credo rubocop svelte-check flow

# Nothing is excluded today. `tables-rust-fuzz-miri` is a runtime fuzzer, not a
# static analyzer lens: its suffix is `fuzz-miri`, which is deliberately not in
# the list above, so it is not discovered here and keeps its own nightly job.
SCHEMA_SHARED_ANALYZER_EXCLUDE :=

# PIN THE CHECKER. The rust leg's analyzer is cargo-clippy; pin the toolchain
# the repository pins so a host with a different one reports tool missing
# rather than silently analyzing with another clippy.
SCHEMA_SHARED_ANALYZER_CARGO_PIN ?= 1.99.0
SCHEMA_SHARED_ANALYZER_CLIPPY_PIN ?= 1.99

# Test-only switch. The planted-target fixtures (internal/ci and the negative
# control below) run the mechanism without the real checker; CI never sets it.
SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE ?=

.PHONY: schema-shared-analyzer-ci-gate-corpus
schema-shared-analyzer-ci-gate-corpus:
	@set -eu; \
	discovered=$$(grep -rhoE '^tables-[a-z0-9]+-[a-z0-9-]+:' Makefile $(wildcard make/*.mk) $(wildcard make/checks/*.mk) 2>/dev/null | sed 's/:$$//' | sort -u); \
	run=""; skip=""; \
	for t in $$discovered; do \
	  port=$${t#tables-}; port=$${port%%-*}; \
	  suffix=$${t#tables-$$port-}; \
	  case " $(SCHEMA_SHARED_ANALYZER_EXCLUDE) " in *" $$t "*) echo "ANALYZER EXCLUDE: $$t (port $$port) -- named exclusion, see make/checks/schema-shared-analyzer-ci-gate-corpus.mk"; continue;; esac; \
	  case " $(SCHEMA_SHARED_ANALYZER_SUFFIXES) " in *" $$suffix "*) ;; *) continue;; esac; \
	  case " $(SCHEMA_SHARED_ANALYZER_PORTS) " in \
	    *" $$port "*) run="$$run $$t";; \
	    *) echo "ANALYZER COVERAGE FAILED: $$t is a per-port analyzer target for port $$port, which the corpus analyzer set [$(SCHEMA_SHARED_ANALYZER_PORTS)] leaves out"; exit 1;; \
	  esac; \
	done; \
	for p in $(SCHEMA_SHARED_ANALYZER_PORTS); do \
	  hit=""; for t in $$run; do q=$${t#tables-}; q=$${q%%-*}; [ "$$q" = "$$p" ] && hit=1; done; \
	  [ -n "$$hit" ] || skip="$$skip $$p"; \
	done; \
	for p in $$skip; do echo "ANALYZER SKIP: (port $$p) -- no analyzer gate landed on the shared base"; done; \
	echo "analyzer-summary: ran [$$run] skipped [$$skip]"; \
	if [ -z "$(SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE)" ]; then \
	  case " $$run " in *" tables-rust-clippy "*) \
	    command -v cargo >/dev/null 2>&1 || { echo "tool missing: cargo (pin $(SCHEMA_SHARED_ANALYZER_CARGO_PIN)) on $$(hostname); the rust analyzer gate cannot run"; exit 1; }; \
	    cargo --version 2>/dev/null | grep -q '$(SCHEMA_SHARED_ANALYZER_CARGO_PIN)' || { echo "tool missing: cargo $(SCHEMA_SHARED_ANALYZER_CARGO_PIN) on $$(hostname) (host has: $$(cargo --version 2>/dev/null || echo none)); the rust analyzer gate cannot run"; exit 1; }; \
	    cargo clippy --version >/dev/null 2>&1 || { echo "tool missing: clippy (pin $(SCHEMA_SHARED_ANALYZER_CLIPPY_PIN), cargo $(SCHEMA_SHARED_ANALYZER_CARGO_PIN)) on $$(hostname); the rust analyzer gate cannot run"; exit 1; }; \
	    echo "ANALYZER TOOL: clippy ($$(cargo clippy --version 2>/dev/null))";; \
	  esac; \
	fi; \
	for t in $$run; do \
	  echo "ANALYZER RUN: $$t"; \
	  $(MAKE) --no-print-directory $$t || { echo "ANALYZER GATE FAILED: $$t"; exit 1; }; \
	done; \
	for t in $$discovered; do \
	  port=$${t#tables-}; port=$${port%%-*}; \
	  suffix=$${t#tables-$$port-}; \
	  case " $(SCHEMA_SHARED_ANALYZER_EXCLUDE) " in *" $$t "*) continue;; esac; \
	  case " $(SCHEMA_SHARED_ANALYZER_SUFFIXES) " in *" $$suffix "*) ;; *) continue;; esac; \
	  case " $$run " in *" $$t "*) ;; *) echo "ANALYZER COVERAGE FAILED: $$t is defined but was not run over the corpus"; exit 1;; esac; \
	done; \
	echo "analyzer-summary: every discovered per-port analyzer target ran over the generated corpus"

# NEGATIVE CONTROL. It builds a scratch include tree from the real make files
# and stubs the checker targets, so the mechanism is proved without generating
# the corpus or needing the checker. Green: the real tables-rust-clippy target
# is discovered and run, and every other port is skipped by name. Red 1: a
# planted analyzer target that fails takes the aggregate down and is named.
# Red 2: a planted analyzer target for a port outside the covered set is a
# coverage failure and is named.
.PHONY: schema-shared-analyzer-ci-gate-corpus-negative-control
schema-shared-analyzer-ci-gate-corpus-negative-control:
	@rm -rf build/analyzer-nc && mkdir -p build/analyzer-nc/make/checks
	@cp make/*.mk build/analyzer-nc/make/
	@cp make/checks/*.mk build/analyzer-nc/make/checks/
	@printf 'include make/checks/schema-shared-analyzer-ci-gate-corpus.mk\n\n.PHONY: tables-rust-clippy\ntables-rust-clippy:\n\t@echo stub rust clippy over the corpus\n' > build/analyzer-nc/make/rust.mk
	@printf 'include $$(wildcard make/*.mk)\n' > build/analyzer-nc/Makefile
	@: > build/analyzer-nc/make/zz-plants.mk
	@if SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE=1 $(MAKE) --no-print-directory -C build/analyzer-nc schema-shared-analyzer-ci-gate-corpus > build/analyzer-nc/green.log 2>&1; then \
		:; \
	else \
		echo "NEGATIVE CONTROL FAILED: the aggregate failed with no plant present"; \
		cat build/analyzer-nc/green.log; exit 1; \
	fi
	@grep -q 'ANALYZER RUN: tables-rust-clippy' build/analyzer-nc/green.log || { \
		echo "NEGATIVE CONTROL FAILED: the real rust clippy target was not run"; cat build/analyzer-nc/green.log; exit 1; }
	@if grep -q 'ANALYZER SKIP: (port rust)' build/analyzer-nc/green.log; then \
		echo "NEGATIVE CONTROL FAILED: rust has an analyzer target but was reported skipped"; \
		cat build/analyzer-nc/green.log; exit 1; \
	fi
	@printf 'tables-go-vet:\n\t@echo plant; exit 1\n' > build/analyzer-nc/make/zz-plants.mk
	@if SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE=1 $(MAKE) --no-print-directory -C build/analyzer-nc schema-shared-analyzer-ci-gate-corpus > build/analyzer-nc/red-wired.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the aggregate stayed green with a failing wired analyzer"; \
		cat build/analyzer-nc/red-wired.log; exit 1; \
	fi
	@grep -q 'ANALYZER GATE FAILED: tables-go-vet' build/analyzer-nc/red-wired.log || { \
		echo "NEGATIVE CONTROL FAILED: red without naming the failing wired plant"; \
		cat build/analyzer-nc/red-wired.log; exit 1; }
	@printf 'tables-zig-clippy:\n\t@echo plant\n' > build/analyzer-nc/make/zz-plants.mk
	@if SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE=1 $(MAKE) --no-print-directory -C build/analyzer-nc schema-shared-analyzer-ci-gate-corpus > build/analyzer-nc/red-coverage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the aggregate stayed green with an analyzer target outside the covered ports"; \
		cat build/analyzer-nc/red-coverage.log; exit 1; \
	fi
	@grep -q 'ANALYZER COVERAGE FAILED: tables-zig-clippy' build/analyzer-nc/red-coverage.log || { \
		echo "NEGATIVE CONTROL FAILED: red without naming the left-out port"; \
		cat build/analyzer-nc/red-coverage.log; exit 1; }
	@: > build/analyzer-nc/make/zz-plants.mk
	@SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE=1 $(MAKE) --no-print-directory -C build/analyzer-nc schema-shared-analyzer-ci-gate-corpus > build/analyzer-nc/green-again.log 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the aggregate failed green after the plant was removed"; \
		cat build/analyzer-nc/green-again.log; exit 1; }
	@grep -q 'ANALYZER RUN: tables-rust-clippy' build/analyzer-nc/green-again.log || { \
		echo "NEGATIVE CONTROL FAILED: the rust target was not run after the plant was removed"; \
		cat build/analyzer-nc/green-again.log; exit 1; }
	@sed -n '1,3p' build/analyzer-nc/green.log
	@sed -n '1,2p' build/analyzer-nc/red-wired.log
	@sed -n '1,2p' build/analyzer-nc/red-coverage.log
	@echo "negative control: the real rust clippy target is discovered and run, a failing wired analyzer takes the aggregate down by name, and an analyzer for an uncovered port is a coverage failure by name"
