PYTHON_CI_SUPPORT_GROUPS += python-one-sided-negative-control

.PHONY: python-one-sided-negative-control
python-one-sided-negative-control:
test-python: python-one-sided-negative-control
