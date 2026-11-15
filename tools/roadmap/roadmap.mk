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
ROADMAP_DATA_DIR = build/roadmap-data-check

# A CARD NEVER COMMITS ROADMAP.md. Two cards' renders touch adjacent rows and
# the same "complete" line, so a card adds or edits only its own evidence file
# under docs/roadmap-evidence/ and runs `make roadmap-check`: the data
# renders (every id a task, every state and evidence well formed) into a copy
# of the page under $(ROADMAP_DATA_DIR), and the committed page and its hand
# text pass the block and authorship checks. It passes whether or not the
# committed page is current, so it is the gate on every pull request and
# every push to main (.github/workflows/roadmap.yml), and a card naming it in
# its DONE-WHEN is not red for the page main has yet to re-render.
# roadmap-data-check is the same target under the name schema#3967 gave it.
# The page itself is re-rendered on main in one commit (`make roadmap`);
# `make roadmap-page-check` holds the committed tables to the render, nightly,
# on dispatch, and for whoever re-renders the page.
.PHONY: roadmap roadmap-check roadmap-data-check roadmap-page-check roadmap-check-negative-control roadmap-original-nine-check roadmap-test
roadmap:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp docs/roadmap.sexp ROADMAP.md

roadmap-page-check:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-page-check REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	$(SBCL) --script tools/roadmap/render.lisp --check docs/roadmap.sexp ROADMAP.md
	$(ROADMAP_HANDTEXT) -page ROADMAP.md
	$(ROADMAP_HANDTEXT) -base $(ROADMAP_BASE)

roadmap-data-check: roadmap-check

roadmap-check:
	@command -v $(SBCL) >/dev/null 2>&1 || { echo "make roadmap-check REFUSES: sbcl is not installed (tools/roadmap/render.lisp needs it)"; exit 2; }
	@mkdir -p $(ROADMAP_DATA_DIR)
	cp ROADMAP.md $(ROADMAP_DATA_DIR)/ROADMAP.md
	$(SBCL) --script tools/roadmap/render.lisp docs/roadmap.sexp $(ROADMAP_DATA_DIR)/ROADMAP.md
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
