# Package exports and their generated-import negative control.
.PHONY: python-package-collision-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-package-collision-negative-control
python-package-collision-negative-control: python-toolchain-check

test-python: python-package-collision-negative-control
