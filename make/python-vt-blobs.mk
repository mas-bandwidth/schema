# The byte buffers' control: the save numbers two equal blobs as one (identity
# by content), and the read accepts a blob two slots name, each planted in a
# COPY of the generated runtime; TestPythonVtBlobsNegativeControl requires the
# affected rows to go red by name.
.PHONY: python-vt-blobs-negative-control
test-python: python-vt-blobs-negative-control
python-vt-blobs-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtBlobsNegativeControl' ./internal/codegen/pythontable/
