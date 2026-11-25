# Cook form's focused Python gate and planted-fault controls.
.PHONY: python-cook-test python-cook-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-cook-test python-cook-negative-control
test-python: python-cook-test python-cook-negative-control

python-cook-test:

python-cook-negative-control:
