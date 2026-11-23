# Exercise the shared, accepted-only variable-table writer rows.
.PHONY: table-write-bounds-go
table-write-bounds-go: build/conformance-harness build/conformance-go
	./build/conformance-harness run --only go --surfaces write-refusal --complete \
		--manifest testdata/conformance/table-write-bounds/ACCEPTED-WRITE-BOUNDS.txt \
		--json testdata/conformance/table-write-bounds \
		--work build/conformance-write-bounds-go

test-go: table-write-bounds-go
