.PHONY: schema-go-spec-limits-two-way-bounds-fast schema-go-spec-limits-two-way-bounds

# Keep the pure size checks in the fast lane. Runtime probes and the table
# corpus compile in the named nightly/dispatch gate.
schema-go-spec-limits-two-way-bounds-fast:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test ./internal/codegen/gotable -run '^TestGoTableArenaRange' -count=1 -timeout 600s

ifeq ($(SCHEMA_GO_SPEC_LIMITS_PLANT),1)
SCHEMA_GO_SPEC_LIMITS_NEGATIVE_CONTROL :=
else
SCHEMA_GO_SPEC_LIMITS_NEGATIVE_CONTROL := sh test/go-tables/spec-limit-negative-control
endif

schema-go-spec-limits-two-way-bounds:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 $(MAKE) build/tables-generated-go/.stamp tables-go-sibling-runtime
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-go-spec-limits-two-way-bounds 'TestGoTableSpecLimitRuntimeBounds' -count=1 -timeout 600s ./internal/codegen/gotable
	cd test/go-tables && GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test $(if $(SCHEMA_GO_TABLES_OVERLAY),-overlay "$(SCHEMA_GO_TABLES_OVERLAY)",) -count=1 -timeout 600s .
	$(SCHEMA_GO_SPEC_LIMITS_NEGATIVE_CONTROL)
