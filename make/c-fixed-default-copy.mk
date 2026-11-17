.PHONY: tables-c-fixed-default-copy-extents
tables-c-fixed-default-copy-extents:
	sh test/slowgate/proof tables-c-fixed-default-copy-extents TestCFixedDefaultCopyNativeExtents ./internal/codegen/ctable -run '^TestCFixedDefaultCopyNativeExtents$$' -count=1

test-c: tables-c-fixed-default-copy-extents
