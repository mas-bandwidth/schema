# make/python-packet-ranged-bits.mk — the Python packet leg's ranged integer
# and bits(N) kinds (docs/PORT-PYTHON.md, RANGED INTEGERS AND BITS(N)).
# Included by the Makefile's wildcard include; the stem has a dash, so it is a
# support fragment and never probed as a --lang port.
#
# python-packet-ranged-bits-negative-control: a COPY of the generated
# ProbeBits reader lands its first bits field one too low, and
# TestPythonPacketRangedBitsNegativeControl must find the write-back marker
# red.
#
# python-packet-ranged-bits-goref-negative-control: a COPY of the generated
# fixture module moves WithCore's int64 range minimum by one, and
# TestPythonPacketRangedBitsGoReferenceNegativeControl must find the Go
# reference marker red.
#
# TestPythonPacketRangedBits writes its own fixture with the generated Go code
# as the byte reference, so the package run and both controls name deps-go,
# which stages the pinned serialize.go sibling where none is present.
.PHONY: python-packet-ranged-bits-negative-control python-packet-ranged-bits-goref-negative-control
test-python: python-packet-ranged-bits-negative-control python-packet-ranged-bits-goref-negative-control deps-go

python-packet-ranged-bits-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonPacketRangedBitsNegativeControl$$' ./internal/codegen/python/

python-packet-ranged-bits-goref-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonPacketRangedBitsGoReferenceNegativeControl$$' ./internal/codegen/python/
