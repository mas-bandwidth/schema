# Strict text-list bounds refuse the whole read in both file and text paths.
.PHONY: tables-js-text-list-strict tables-js-text-list-strict-negative-control
tables-js-text-list-strict: toolchain-js
		-count=1 -timeout=120s -run '^(TestJavaScriptTextTotalTailConstruct|TestJavaScriptTextListConstruct|TestJavaScriptUnboundedTextConstruct)$$' ./internal/codegen/jstable/

tables-js-text-list-strict-negative-control: tables-js-text-list-strict
		-count=1 -timeout=120s -run '^TestJavaScriptTextListStrictNegativeControl$$' ./internal/codegen/jstable/

test-js: tables-js-text-list-strict tables-js-text-list-strict-negative-control
