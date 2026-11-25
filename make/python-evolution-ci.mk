# Keep the complete local Evolution test; CI delegates its existing subtests.
PYTHON_CI_SUPPORT_GROUPS += python-ci-vt-evolution-values python-ci-vt-evolution-wire python-ci-vt-evolution-write
.PHONY: python-ci-vt-evolution-values python-ci-vt-evolution-wire python-ci-vt-evolution-write
python-ci-vt-evolution-values: python-toolchain-check
python-ci-vt-evolution-wire: python-toolchain-check
python-ci-vt-evolution-write: python-toolchain-check
