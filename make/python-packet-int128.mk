# python-packet-int128-negative-control: the 128-bit integer kinds' control.
# It edits a COPY of the generated Raw reader so its uint128 lands one too
# high, and requires TestPythonPacketInt128NegativeControl to find the
# Go-reference read marker red. The reference is the generated Go unit over
# serialize.go, so the runtime is a prerequisite (SERIALIZE_GO_RUNTIME is set
# by make/go.mk, which the wildcard include reads before this file).
.PHONY: python-packet-int128-negative-control
test-python: python-packet-int128-negative-control
python-packet-int128-negative-control: python-toolchain-check $(SERIALIZE_GO_RUNTIME)/go.mod
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketInt128NegativeControl ./internal/codegen/python/
