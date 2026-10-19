# make/checks/schema-spec-pin-s7.mk — THE SPEC-PINS GUARD, §7
# (docs/SPEC-TABLES.md, "7. The cooked form", under "Row ids and pins"):
# every normative sentence of §7 carries a row id, every row is pinned in
# every port the variable-tables cook row names, and every pin resolves to a
# harness that runs. Unit tier, seconds.
#
# The Makefile includes this file by path: make/*.mk is the discovered-port
# registry (tables-ports-refuse-wide-scalars runs `schema generate --lang`
# over every make/<name>.mk), so a check fragment there reads as a port.

.PHONY: schema-spec-pin-s7
schema-spec-pin-s7:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s7 \
		'TestSpecPinsHoldOnTheSpec TestSpecPinsSectionSizes TestSpecPinsSection7PlantsAnUnpinnedRule' \
		./internal/ci/ -run '^(TestSpecPinsHoldOnTheSpec|TestSpecPinsSectionSizes|TestSpecPinsSection7PlantsAnUnpinnedRule)$$' -count=1 -timeout 600s
