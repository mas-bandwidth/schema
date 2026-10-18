# make/squirrel-fuzz.mk — the SQUIRREL leg's wire-fuzz target
# (test/conformance/README.md, "The wire-fuzz driver"; docs/SPEC-TABLES.md §4.2;
# docs/PORT-SQUIRREL.md, THE CARD MAP). The driver is
# test/conformance/squirrel/wirefuzz.nut under the pinned interpreter ($SQ, which
# make exports), the harness's wire-fuzz surface, form 1 and form 2, no retention.

.PHONY: tables-squirrel-wire-fuzz
tables-squirrel-wire-fuzz: build/conformance-harness
	./build/conformance-harness wire-fuzz --driver "$(SQ) test/conformance/squirrel/wirefuzz.nut" --seed 1 --n 2000
