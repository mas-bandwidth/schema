# make/python-packet-unions.mk — the Python leg's tagged unions and
# payload-free arms (docs/PORT-PYTHON.md). Included by the Makefile's
# wildcard include.
#
# packet-void-python: the leg's half of test/packet-void (SPEC §4.8), the
# carried-kind union fixture read and written under both pinned interpreters
# with mypy --strict. Its control plants an extra void-arm bit in a copy of
# the generated code and must go red by name. Both compare against the Go
# reference, so both need the pinned serialize.go: deps-go stages it beside
# the checkout when no sibling is there (a hosted macOS runner has none).
.PHONY: packet-void-python packet-void-python-negative-control
packet-void-python: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonPacketUnions$$' ./internal/codegen/python/

packet-void-python-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonPacketUnionsNegativeControl$$' ./internal/codegen/python/

PYTHON_CI_SUPPORT_GROUPS += python-ci-packet-unions
python-ci-packet-unions: packet-void-python packet-void-python-negative-control
