# make/python-packet-arrays.mk — the Python packet array kind's gate
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# python-packet-arrays-negative-control: the array kind's own control. It
# drops the counted array's minimum from a COPY of a generated reader, so the
# count no longer rides offset, and requires TestPythonPacketArraysNegativeControl
# to find the hand-checked vector red by name. A Go test under the slow gate:
# the probe runs under the pinned interpreter beside the Go reference writer,
# which needs serialize.go (deps-go); test-python needs it for the same test.
.PHONY: python-packet-arrays-negative-control
PYTHON_CI_SUPPORT_GROUPS += python-ci-packet-arrays
python-ci-packet-arrays: deps-go python-packet-arrays-negative-control
python-packet-arrays-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketArraysNegativeControl ./internal/codegen/python/
