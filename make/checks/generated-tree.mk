# THE GENERATED-TREE GATE AND ITS NEGATIVE CONTROL (issue #898, Fixed Tables
# gate G3). test/generated-tree/verify proves the committed generated/ tree is
# what the compiler at this sha emits, from an EMPTY directory — the only way to
# see a file the emitter stopped writing, which an in-place regeneration and a
# `git diff` cannot. `generated-current` in the Makefile is its entry point; the
# `generated` job in .github/workflows/ci-full.yml and the certify workflow run
# the same script.
#
# The controls are hermetic: they drive test/generated-tree/compare, the
# verifier's whole verdict, with prepared directories. No regeneration, so they
# belong in `make test` — a gate nobody has seen go red is a decoration.

GENERATED_TREE_CONTROLS := changed stale retired-rule missing \
	exception-absent exception-emitted exception-ignored exception-honoured \
	ignored clean

.PHONY: generated-tree-negative-controls $(GENERATED_TREE_CONTROLS:%=generated-tree-control-%)

$(GENERATED_TREE_CONTROLS:%=generated-tree-control-%): generated-tree-control-%:
	@test/generated-tree/negative-control $*

generated-tree-negative-controls: $(GENERATED_TREE_CONTROLS:%=generated-tree-control-%)
	@echo "generated-tree controls: $(words $(GENERATED_TREE_CONTROLS)) planted verdicts, each named"

# THE VERIFIER'S LIFECYCLE WITNESSES. The controls above hand prepared
# directories to compare; these drive the real verifier in a disposable Git
# fixture with one generation rule, so they can prove the two things a prepared
# directory cannot: that a previous run's preserved original tree is RECOVERED
# and never destroyed (even across a real SIGKILL of the whole verifier process
# group), and that a staged edit does not pass as committed.

TREELOCK_SRCS := tools/treelock/main.go tools/treelock/lock_unix.go tools/treelock/lock_windows.go go.mod

build/treelock: $(TREELOCK_SRCS)
	@mkdir -p build
	@go build -o build/treelock ./tools/treelock

GENERATED_TREE_VERIFY_CONTROLS := kill-retry kill-before-emission simultaneous-contender ambiguous-marker two-reclaimers acquisition-before-owner regeneration-fails second-invocation staged-difference

.PHONY: generated-tree-verify-controls $(GENERATED_TREE_VERIFY_CONTROLS:%=generated-tree-verify-control-%)

$(GENERATED_TREE_VERIFY_CONTROLS:%=generated-tree-verify-control-%): generated-tree-verify-control-%: build/treelock
	@test/generated-tree/verify-control $*

generated-tree-verify-controls: $(GENERATED_TREE_VERIFY_CONTROLS:%=generated-tree-verify-control-%)
	@echo "generated-tree verify controls: $(words $(GENERATED_TREE_VERIFY_CONTROLS)) lifecycle witnesses, each named"

test: generated-tree-negative-controls generated-tree-verify-controls
