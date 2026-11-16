# make/python-table-packet-types.mk — the Python leg's packet types carried by
# value in tables (docs/PORT-PYTHON.md, THE CARD MAP: P4 in the table frame).
# Included by the Makefile's wildcard include.
#
# The packet-type controls: the nested body's length the generated table writer
# emits is moved in a copy of the runtime, and the probe's byte compare against
# the Go engine's wire must go red by name; and a packet type no table reaches
# has its generated writer move a field in a copy of the module, and the probe's
# byte compare against the Go reference's packet wire must go red by name; and
# the nested-body length is moved for a packet type carried as a map value, and
# the probe's byte compare against the Go engine's wire must go red by name.
# Registered in make/negative-controls.d/python-table-packet-types-negative-control.json.

.PHONY: python-table-packet-types-negative-control
python-table-packet-types-negative-control:
	SCHEMA_SLOW=1 go test -tags schema_leg_python -run '^TestPythonTablePacketTypes(Loose|MapValue)?NegativeControl$$' ./internal/codegen/pythontable/ -count=1

PYTHON_CI_SUPPORT_GROUPS += python-ci-table-packet-types
python-ci-table-packet-types: python-table-packet-types-negative-control
