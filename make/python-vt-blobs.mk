# The byte buffers' controls: the save numbers two equal blobs as one (identity
# by content), the read accepts a blob two slots name, the root stops placing
# the *bytes kind its own slot names, and a *bytes slot naming an undeclared
# record refuses, each planted in a COPY of the generated runtime;
# TestPythonVtBlobsNegativeControl and TestPythonVtBlobsUnknownCountNegativeControl
# require the affected rows to go red by name.
.PHONY: python-vt-blobs-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-blobs
python-ci-vt-blobs: python-vt-blobs-negative-control
python-vt-blobs-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run 'TestPythonVtBlobs(UnknownCount)?NegativeControl' ./internal/codegen/pythontable/
