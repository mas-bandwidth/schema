# Form 1 optional counted array resize through the generated Go codec.
GO_OPTIONAL_ARRAY_RESIZE := build/table-optional-array-resize/go
OPTIONAL_ARRAY_RESIZE_FIXTURE := testdata/conformance/table-write-bounds

.PHONY: table-optional-array-resize-go
table-optional-array-resize-go: bin/schema tables-go-sibling-runtime
	./bin/schema generate --lang go --out $(GO_OPTIONAL_ARRAY_RESIZE) $(OPTIONAL_ARRAY_RESIZE_FIXTURE)/TableWriteBounds.schema
	@printf 'module writebounds\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => "%s"\nreplace github.com/mas-bandwidth/schema/v2 => "%s"\n' "$(abspath $(SERIALIZE_GO))" "$(CURDIR)" > $(GO_OPTIONAL_ARRAY_RESIZE)/go.mod
	cp test/go-tables/optional_array_resize_test.gotext $(GO_OPTIONAL_ARRAY_RESIZE)/resize_test.go
	cp $(OPTIONAL_ARRAY_RESIZE_FIXTURE)/torture_var_wref.bin $(OPTIONAL_ARRAY_RESIZE_FIXTURE)/expected/torture_var_wref_resized.bin $(GO_OPTIONAL_ARRAY_RESIZE)/
	cd $(GO_OPTIONAL_ARRAY_RESIZE) && go test -count=1 -v .

test-go: table-optional-array-resize-go
