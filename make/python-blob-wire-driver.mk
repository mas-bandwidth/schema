# Strict shared-blob verdicts use the existing expected-refusal protocol.
PYTHON_CI_SUPPORT_GROUPS += python-ci-blob-wire-driver
.PHONY: python-ci-blob-wire-driver python-blob-wire-driver python-blob-wire-driver-negative-control
python-ci-blob-wire-driver: python-blob-wire-driver python-blob-wire-driver-negative-control
python-blob-wire-driver: dist-python
python-blob-wire-driver-negative-control: dist-python
test-python: python-blob-wire-driver python-blob-wire-driver-negative-control
