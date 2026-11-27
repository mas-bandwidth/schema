TABLE_WRITE_BOUNDS_FIXTURE := testdata/conformance/table-write-bounds

.PHONY: table-write-bounds-c table-write-bounds-cpp
table-write-bounds-c: build/conformance-harness build/conformance-c-writebounds
	@mkdir -p build/table-write-bounds
	@printf 'c ./build/conformance-c-writebounds\n' > build/table-write-bounds/c.drivers
	./build/conformance-harness run --drivers build/table-write-bounds/c.drivers \
		--only c --surfaces write-refusal,wire,json-read,json-write --complete \
		--manifest $(TABLE_WRITE_BOUNDS_FIXTURE)/WRITE-BOUNDS.txt --json $(TABLE_WRITE_BOUNDS_FIXTURE) \
		--work build/table-write-bounds/c

table-write-bounds-cpp: build/conformance-harness build/conformance-cpp-writebounds
	@mkdir -p build/table-write-bounds
	@printf 'cpp ./build/conformance-cpp-writebounds\n' > build/table-write-bounds/cpp.drivers
	./build/conformance-harness run --drivers build/table-write-bounds/cpp.drivers \
		--only cpp --surfaces write-refusal,wire,json-read,json-write --complete \
		--manifest $(TABLE_WRITE_BOUNDS_FIXTURE)/WRITE-BOUNDS.txt --json $(TABLE_WRITE_BOUNDS_FIXTURE) \
		--work build/table-write-bounds/cpp

test-c: table-write-bounds-c
test-cpp: table-write-bounds-cpp
