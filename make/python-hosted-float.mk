# Hosted packet floats use the fixed-class form-1 codecs.
.PHONY: python-hosted-float-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-hosted-float
python-ci-hosted-float: python-hosted-float-negative-control
python-hosted-float-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestPython(Hosted|Optional)FloatNegativeControls$$' ./compiler/
