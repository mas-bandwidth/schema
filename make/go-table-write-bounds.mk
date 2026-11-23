# Exercise the shared, accepted-only variable-table writer rows.
.PHONY: table-write-bounds-go
table-write-bounds-go: build/conformance-harness build/conformance-go
	./build/conformance-harness run --only go --surfaces write-refusal --complete \
		--manifest testdata/conformance/tables/ACCEPTED-WRITE-BOUNDS.txt \
		--work build/conformance-write-bounds-go

test-go conformance-corpus-go: table-write-bounds-go
