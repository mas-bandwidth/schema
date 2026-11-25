.PHONY: tables-c-node-table-last-valid
tables-c-node-table-last-valid:
	$(GO_TEST) ./internal/codegen/ctable -count=1 -timeout 2m -run '^TestCNodeTableLastValidRoundTrips$$' -v

test-c: tables-c-node-table-last-valid
