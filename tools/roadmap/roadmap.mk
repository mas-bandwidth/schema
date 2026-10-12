# ROADMAP.md is generated, end to end, from docs/roadmap.sexp and the evidence
# overlay docs/roadmap-evidence/*.sexp by tools/roadmap/render.lisp (SBCL).
# Edit the data, then `make roadmap`; never edit ROADMAP.md by hand.
# `make roadmap-check` is the drift gate: it exits 1 when the committed page is
# not what the data renders. tools/roadmap/tally_test.go runs the same check
# wherever sbcl is installed, and refuses a table outside a generated block
# everywhere. This file is included by path (not from make/*.mk) so the port
# registry wildcard stays live compiler backends only.
SBCL ?= sbcl

.PHONY: roadmap roadmap-check roadmap-test
roadmap:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp docs/roadmap.sexp ROADMAP.md

roadmap-check:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-check REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp --check docs/roadmap.sexp ROADMAP.md

roadmap-test:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-test REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/test.lisp
	go test -count=1 ./tools/roadmap/
