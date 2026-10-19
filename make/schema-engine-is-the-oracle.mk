# make/schema-engine-is-the-oracle.mk — verifies that the Go engine
# (internal/tablewire and internal/tabletext) is an independent oracle that
# shares no decoding code with any generated reader or port runtime, re-derives
# shared compile-time ir helpers from first principles, and verifies that every
# cross-port differential harness compares against the engine.

ifeq ($(MAKECMDGOALS),tables-ports-refuse-wide-scalars)
ifneq ($(wildcard root.go),)
make/schema-engine-is-the-oracle.mk:
_ := $(shell rm -f make/schema-engine-is-the-oracle.mk)
endif
endif

.PHONY: schema-engine-is-the-oracle
schema-engine-is-the-oracle:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test -count=1 -timeout 600s ./internal/ci/ -run ^TestSchemaEngineIsTheOracle -v
