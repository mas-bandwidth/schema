# Strict text-list bounds refuse the whole read in both file and text paths.
.PHONY: tables-js-text-list-strict tables-js-text-list-strict-negative-control
tables-js-text-list-strict: toolchain-js
	NODE=$(NODE) sh test/slowgate/proof tables-js-text-list-strict 'TestJavaScriptTextTotalTailConstruct TestJavaScriptTextListConstruct TestJavaScriptUnboundedTextConstruct' \
		-count=1 -timeout=120s -run '^(TestJavaScriptTextTotalTailConstruct|TestJavaScriptTextListConstruct|TestJavaScriptUnboundedTextConstruct)$$' ./internal/codegen/jstable/

tables-js-text-list-strict-negative-control: tables-js-text-list-strict
	NODE=$(NODE) sh test/slowgate/proof tables-js-text-list-strict-negative-control 'TestJavaScriptTextListStrictNegativeControl' \
		-count=1 -timeout=120s -run '^TestJavaScriptTextListStrictNegativeControl$$' ./internal/codegen/jstable/

test-js: tables-js-text-list-strict tables-js-text-list-strict-negative-control
