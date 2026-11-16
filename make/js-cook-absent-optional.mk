# Plant the former absent-record initialization and require its dead bytes red.
.PHONY: js-cook-absent-optional-negative-control
js-cook-absent-optional-negative-control:
	$(call js_jstable_control,js-cook-absent-defaults,internal/codegen/jstable/cookwrite.go,^TestCookWriteAnswersEveryCookWriteRowJavaScript$$,./internal/codegen/jstable/,keyed_config little-endian cook mismatch)
test-js: js-cook-absent-optional-negative-control
