# make/schema-shared-resource-bounds-every-port.mk — the resource-bounds leg.
# Decides and holds the shared bounds every port subscribes to
# (docs/SPEC-TABLES.md, resource bounds): the allocation factor F, the nesting
# depth cap and the refusal name nesting_depth on the Go engine and the Go
# generated readers. The per-port cards apply the same bounds in each language.
# Heavy parts are nightly; this fast target stays inside the one-minute budget.

.PHONY: schema-shared-resource-bounds
schema-shared-resource-bounds:
	SCHEMA_SLOW=1 go test -parallel=1 ./internal/ci ./internal/codegen/gotable -run 'TestEngine|TestGeneratedReader(ResourceBounds|RootAllocationBytesMeasure)' -count=1 -timeout 600s
