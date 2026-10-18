# ROADMAP.md is generated, end to end, from docs/roadmap.sexp and the evidence
# overlay docs/roadmap-evidence/*.sexp by nova-tools tools/roadmap, the one
# roadmap generator every repository uses (it began as this repository's
# render.lisp, ported to Go with the same data form and the same page).
# Edit the data, then `make roadmap`; never edit ROADMAP.md by hand.
# `make roadmap-check` is the drift gate: it exits 1 when the committed page is
# not what the data renders, and the fast CI lane runs it on every push.
# `go run $(ROADMAP_TOOL) --help` documents the form. This file is included by
# path (not from make/*.mk) so the port registry wildcard stays live compiler
# backends only.
ROADMAP_TOOL ?= github.com/mas-bandwidth/nova-tools/tools/roadmap@9d564eb81259c8a19c6515cd61c040030940671f

.PHONY: roadmap roadmap-check roadmap-test
roadmap:
	go run $(ROADMAP_TOOL)

roadmap-check:
	go run $(ROADMAP_TOOL) --check

roadmap-test:
	go test -count=1 ./tools/roadmap/
