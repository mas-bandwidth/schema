# make/schema-engine-is-the-oracle.mk — verifies that the Go engine
# (internal/tablewire and internal/tabletext) is an independent oracle that
# shares no decoding code with any generated reader or port runtime, re-derives
# shared compile-time ir helpers from first principles, and verifies that every
# cross-port differential harness compares against the engine. The Makefile
# includes make/*.mk, so this lands without a Makefile edit.

.PHONY: schema-engine-is-the-oracle
schema-engine-is-the-oracle:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test -count=1 -timeout 600s ./internal/ci/ -run ^TestSchemaEngineIsTheOracle -v
