# The Python table text walk and its field-key negative control.
.PHONY: python-json python-json-negative-control

PYTHON_CI_SUPPORT_GROUPS += python-json python-json-negative-control

test-python: python-json python-json-negative-control

python-json: python-toolchain-check

python-json-negative-control: python-toolchain-check
