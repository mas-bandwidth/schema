# Fixed-form shape proofs against the live C++ reference.
.PHONY: python-fixed-proof-shapes python-fixed-proof-shapes-negative-control
test-python: python-fixed-proof-shapes python-fixed-proof-shapes-negative-control
python-fixed-proof-shapes:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofShapes($$|Text$$|ArrayBounds$$)' ./internal/codegen/pythontable/
python-fixed-proof-shapes-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonFixedProofShapesNegativeControl$$' ./internal/codegen/pythontable/
