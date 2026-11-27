# Exercise the shared, variable-table writer boundary pairs.
.PHONY: table-write-bounds-go
table-write-bounds-go: build/conformance-harness build/conformance-go-writebounds
	@mkdir -p build
	@printf 'go ./build/conformance-go-writebounds\n' > build/conformance-write-bounds-go.drivers
	./build/conformance-harness run --drivers build/conformance-write-bounds-go.drivers \
		--only go --surfaces write-refusal,wire,json-read,json-write --complete \
		--manifest testdata/conformance/table-write-bounds/WRITE-BOUNDS.txt \
		--json testdata/conformance/table-write-bounds \
		--work build/conformance-write-bounds-go

test-go: table-write-bounds-go
