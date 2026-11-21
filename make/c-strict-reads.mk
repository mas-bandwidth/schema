# make/c-strict-reads.mk — the C leg's strict-read probes from the 2026-11-21
# review (c.md C-1..C-4; verify-message.md and verify-values.md, the c rows of
# Across legs). Each compiles the generated C for a tiny declaration and runs
# hand-built inputs through it: every must-refuse input beside an accepted twin.
.PHONY: tables-c-strict-reads
tables-c-strict-reads:
	SCHEMA_SLOW=1 go test ./internal/codegen/ctable -count=1 -timeout 2m -run '^TestCStrictReads$$|^TestCMessageStrictReads$$|^TestCWriteContractDebug$$' -v

test-c: tables-c-strict-reads
_verify-c-conformance: tables-c-strict-reads
