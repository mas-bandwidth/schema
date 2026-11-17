.PHONY: tables-c-node-table-last-valid
tables-c-node-table-last-valid:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/ctable -count=1 -timeout 2m -run '^TestCNodeTableLastValidRoundTrips$$' -v

test-c: tables-c-node-table-last-valid
