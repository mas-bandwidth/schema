# make/luau-fuzz.mk — the LUAU leg's wire-fuzz gate (docs/PORT-LUAU.md, THE
# CARD MAP: schema-luau-fuzz-differential; test/conformance/README.md, "The
# wire-fuzz driver"; docs/SPEC-TABLES.md §4.2). Included by the Makefile's
# wildcard include.
#
# The pinned Luau has no io library, so the harness's little-endian pipe cannot
# be spoken by wirefuzz.luau itself; the host shim that frames it is the one
# part of this card still owed. The in-package differential and its negative
# control run here in the meantime, over the same corpus and the same pinned
# interpreter; both are the DONE-WHEN gate.

LUAU_FUZZ_SEED ?= 1
LUAU_FUZZ_N    ?= 2000

.PHONY: tables-luau-wire-fuzz
tables-luau-wire-fuzz: build/conformance-harness build/conformance-luau
	@mkdir -p build
	@SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/luautable/ \
		-run '^TestLuauWireFuzzAgreesWithTheGoEngine$$' -v \
		> build/tables-luau-wire-fuzz.log 2>&1; \
		status=$$?; cat build/tables-luau-wire-fuzz.log; \
		test $$status -eq 0
	@grep -q -- '--- PASS' build/tables-luau-wire-fuzz.log
	@grep -q 'Luau wire fuzz:' build/tables-luau-wire-fuzz.log
	@SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/luautable/ \
		-run '^TestLuauWireFuzzNegativeControl$$' -v \
		> build/tables-luau-wire-fuzz-negative-control.log 2>&1; \
		status=$$?; cat build/tables-luau-wire-fuzz-negative-control.log; \
		test $$status -eq 0
	@grep -q -- '--- PASS' build/tables-luau-wire-fuzz-negative-control.log
