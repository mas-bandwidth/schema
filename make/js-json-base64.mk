# The control plants acceptance of an unpadded value in generated runtime.
.PHONY: js-json-base64-negative-control
js-json-base64-negative-control:
	SCHEMA_SLOW=1 NODE=$(NODE) go test -count=1 -timeout 600s -run '^TestJavaScriptTextBase64NegativeControl$$' ./internal/codegen/jstable/
test-js: js-json-base64-negative-control
