# make/goose-versioning.mk — THE VERSIONING HALF OF THE GOOSE LEG's fixed form
# (docs/FIXED-FORM-ALGORITHM.md §5, the rows of
# docs/FIXED-FORM-VERSIONING-TESTS.md; docs/PORT-GOOSE.md, THE CARD MAP:
# schema-goose-fixed-form-evolution). Included by the Makefile's wildcard
# include (before make/goose.mk by name; GOOSE, which goose.mk sets, is read only
# inside recipes, so the order does not matter).
#
# internal/codegen/goosetable/fixedversioning_test.go reads the C++ reference's
# byte oracle out of build/fixedform-corpus — `old_<row>.bin`, `new_<row>.bin`,
# the floor, hash, lineage-merge and census files — and holds every row's two
# columns, each a generated Goose probe run under the pinned compiler:
# NEW-READS-OLD lands every value the corpus MANIFEST names for the old file
# (compared as record images against a fresh new value carrying them), with
# §5.4's counters; OLD-REFUSES-NEW answers `layout_unknown` before any record,
# on the file's hash alone, writing nothing. Then the floor's three, the hash
# cases, the lineage merge, and §5.8's probes (rows 4, 9, 11, 12).
#
# SCHEMA_REQUIRE_CORPUS=1 turns the suite's corpus skip into a FAILURE, so the
# gate cannot pass on a tree that never built the oracle.
.PHONY: tables-goose-versioning
tables-goose-versioning: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s \
		-run '^TestGooseFixed(Versioning|FormNamesEveryRefusal$$|FormKindMismatchCensus$$)' ./internal/codegen/goosetable/
	@echo 'tables Goose versioning: §5 read both columns of every row against the C++ reference bytes'

# THE VERSIONING GATE's NEGATIVE CONTROL (§5.9 #39; make/negative-controls.json,
# group goose): the wrong refusal name on a file outside the lineage. The
# emitter is overlaid so a hash no entry holds refuses `layout_newer` instead
# of `layout_unknown`, and the control requires the gate to go red on EXACTLY
# the rows that read such a file — every OLD-REFUSES-NEW row whose two hashes
# differ (25: the 28 rows less the three same-hash rows, which read both ways)
# and hash_unknown — and on nothing else: no NEW-READS-OLD row, no floor, no
# probe.
GOOSE_VERSIONING_NC := build/goose-versioning-nc
.PHONY: tables-goose-versioning-negative-control
tables-goose-versioning-negative-control: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_VERSIONING_NC) && mkdir -p $(GOOSE_VERSIONING_NC)
	@sed 's/schema_rt::FixedRefuseLayoutUnknown, false/schema_rt::FixedRefuseLayoutNewer, false/' \
		internal/codegen/goosetable/fixedevolve.go > $(GOOSE_VERSIONING_NC)/fixedevolve.go.txt
	@! cmp -s internal/codegen/goosetable/fixedevolve.go $(GOOSE_VERSIONING_NC)/fixedevolve.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not apply to fixedevolve.go"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goosetable/fixedevolve.go":"%s/$(GOOSE_VERSIONING_NC)/fixedevolve.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(GOOSE_VERSIONING_NC)/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -v -timeout 600s -overlay $(GOOSE_VERSIONING_NC)/overlay.json \
			-run '^TestGooseFixedVersioning' ./internal/codegen/goosetable/ > $(GOOSE_VERSIONING_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the wrong refusal name left the Goose versioning gate green"; \
		cat $(GOOSE_VERSIONING_NC)/log; exit 1; \
	fi
	@grep -E -- '--- FAIL: TestGooseFixedVersioning[A-Za-z]+/' $(GOOSE_VERSIONING_NC)/log | awk '{print $$3}' | sort > $(GOOSE_VERSIONING_NC)/failed
	@if grep -v -E '^TestGooseFixedVersioning(OldRefusesNew/[a-zA-Z_]+|Hash/hash_unknown)$$' $(GOOSE_VERSIONING_NC)/failed; then \
		echo "NEGATIVE CONTROL FAILED: the sabotage reddened a row it does not touch (above)"; exit 1; fi
	@n=$$(grep -c '^TestGooseFixedVersioningOldRefusesNew/' $(GOOSE_VERSIONING_NC)/failed); \
	if [ "$$n" -ne 25 ] || ! grep -qx 'TestGooseFixedVersioningHash/hash_unknown' $(GOOSE_VERSIONING_NC)/failed; then \
		echo "NEGATIVE CONTROL FAILED: $$n OLD-REFUSES-NEW rows went red, want 25, and hash_unknown with them"; \
		cat $(GOOSE_VERSIONING_NC)/failed; exit 1; fi
	@grep -q 'owes layout_unknown, answered n=-1 layout_newer' $(GOOSE_VERSIONING_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the versioning gate went red for another reason"; \
		  cat $(GOOSE_VERSIONING_NC)/log; exit 1; }
	@echo 'negative control: the wrong name for a hash outside the lineage reds exactly the 25 OLD-REFUSES-NEW rows and hash_unknown'
