TABLE_WRITE_BOUNDS_FIXTURE := testdata/conformance/table-write-bounds

.PHONY: table-write-bounds-c table-write-bounds-cpp
table-write-bounds-c: build/conformance-harness build/conformance-c
	./build/conformance-harness run --only c --surfaces write-refusal --complete \
		--manifest $(TABLE_WRITE_BOUNDS_FIXTURE)/WRITE-BOUNDS.txt --json $(TABLE_WRITE_BOUNDS_FIXTURE) \
		--work build/table-write-bounds/c

table-write-bounds-cpp: build/conformance-harness build/conformance-cpp
	./build/conformance-harness run --only cpp --surfaces write-refusal --complete \
		--manifest $(TABLE_WRITE_BOUNDS_FIXTURE)/WRITE-BOUNDS.txt --json $(TABLE_WRITE_BOUNDS_FIXTURE) \
		--work build/table-write-bounds/cpp

test-c: table-write-bounds-c
test-cpp: table-write-bounds-cpp
