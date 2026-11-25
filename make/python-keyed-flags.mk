# The keyed flags codec and its strict-write negative control.
.PHONY: python-keyed-flags-negative-control python-ci-keyed-flags
python-keyed-flags-negative-control:
python-ci-keyed-flags:
PYTHON_CI_SUPPORT_GROUPS += python-ci-keyed-flags python-keyed-flags-negative-control
test-python: python-keyed-flags-negative-control
