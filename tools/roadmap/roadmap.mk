# The tables in ROADMAP.md are generated from docs/roadmap.sexp and the
# evidence overlay docs/roadmap-evidence/*.sexp by tools/roadmap/render.lisp
# (SBCL). Everything else on the page (title, headings, prose) is Glenn's,
# written by hand: `make roadmap` rewrites only the bytes between each pair of
# nova-work markers and leaves the rest exactly as it is.
# `make roadmap-check` is the gate: the tables match the data, every block
# holds only a table and its source link, and no commit since
# $(ROADMAP_BASE) by anyone but Glenn Fiedler changes the page outside the
# markers. tools/roadmap/tally_test.go runs the same checks under go test.
# This file is included by path (not from make/*.mk) so the port registry
# wildcard stays live compiler backends only.
SBCL ?= sbcl
ROADMAP_BASE ?= origin/main
ROADMAP_HANDTEXT = go run ./tools/roadmap/cmd/roadmap-handtext
ROADMAP_NC_DIR = build/roadmap-negative-control

.PHONY: roadmap roadmap-check roadmap-check-negative-control roadmap-original-nine-check roadmap-test
roadmap:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp docs/roadmap.sexp ROADMAP.md

roadmap-check:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-check REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp --check docs/roadmap.sexp ROADMAP.md
	$(ROADMAP_HANDTEXT) -page ROADMAP.md
	$(ROADMAP_HANDTEXT) -base $(ROADMAP_BASE)

# The gates fire on an "X update:" paragraph put back: outside the markers by
# an author other than Glenn, and inside a block by anyone.
roadmap-check-negative-control:
	@set -eu; mkdir -p $(ROADMAP_NC_DIR); \
	awk '{print} /^## Variable tables$$/{print ""; print "Rust update: the full uncapped fleet nightly passed."}' ROADMAP.md > $(ROADMAP_NC_DIR)/outside.md; \
	awk '{print} /^<!-- nova-work:variable-tables:start -->$$/{print ""; print "Rust update: the full uncapped fleet nightly passed."}' ROADMAP.md > $(ROADMAP_NC_DIR)/inside.md; \
	if $(ROADMAP_HANDTEXT) -before ROADMAP.md -after $(ROADMAP_NC_DIR)/outside.md -author Rowan; then echo "NEGATIVE CONTROL FAILED: a paragraph outside the markers by Rowan passed"; exit 1; fi; \
	$(ROADMAP_HANDTEXT) -before ROADMAP.md -after $(ROADMAP_NC_DIR)/outside.md -author "Glenn Fiedler" || { echo "NEGATIVE CONTROL FAILED: Glenn's own edit was refused"; exit 1; }; \
	if $(ROADMAP_HANDTEXT) -page $(ROADMAP_NC_DIR)/inside.md; then echo "NEGATIVE CONTROL FAILED: a paragraph inside a block passed"; exit 1; fi; \
	if $(SBCL) --script tools/roadmap/render.lisp --check docs/roadmap.sexp $(ROADMAP_NC_DIR)/inside.md >/dev/null 2>&1; then echo "NEGATIVE CONTROL FAILED: the drift check passed a paragraph inside a block"; exit 1; fi; \
	echo "OK: roadmap-check refuses an \"X update:\" paragraph put back, inside a block and outside by anyone but Glenn"

# Release/conformance acceptance: the original nine must be complete in all
# three rendered sections. Ordinary roadmap-check still permits honest TODOs.
roadmap-original-nine-check:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-original-nine-check REFUSES: sbcl is not installed"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp --check --require-original-nine docs/roadmap.sexp ROADMAP.md

roadmap-test:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-test REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/test.lisp
	go test -count=1 ./tools/roadmap/...
