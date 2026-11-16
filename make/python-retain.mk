.PHONY: python-retain python-retain-negative-control
python-retain: dist-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonRetain' ./internal/codegen/pythontable/
python-retain-negative-control: dist-python
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPythonRetain(ResolverNegativeControl|PathIndexNegativeControl|NodeRemapNegativeControl|TailNegativeControl)$$' ./internal/codegen/pythontable/
test-python: python-retain python-retain-negative-control
