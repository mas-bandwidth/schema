# make/python-packet-layout.mk — the Python packet layout's control
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# python-packet-layout-negative-control: the layout gate's own control. In a
# COPY of the generated module it plants, one at a time, the const comparison,
# the reserved comparison, the alignment read and the untaken side's zeroing
# out of the reader, and requires TestPythonPacketLayoutNegativeControl to go
# red by name on each. TestPythonPacketLayout itself runs in test-python's
# whole-package go test.
.PHONY: python-packet-layout-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-packet-layout
python-ci-packet-layout: python-packet-layout-negative-control
python-packet-layout-negative-control: python-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketLayoutNegativeControl ./internal/codegen/python/
