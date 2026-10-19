# make/checks/schema-spec-pin-design-row-ids.mk — THE SPEC-PINS GUARD
# (docs/SPEC-TABLES.md, "Row ids and pins"): every normative sentence of the
# spec carries a row id, every row is pinned in every port that carries its
# feature or owed by name, and every pin resolves to a test that runs. Unit
# tier, seconds. The guard's own reversed witnesses (TestSpecPinsRefusals,
# TestSpecPinsRealSpecPlants) run in the same go test, so the target is red
# when any plant stops firing.
#
# The Makefile includes this file by path: make/*.mk is the discovered-port
# registry (tables-ports-refuse-wide-scalars runs `schema generate --lang`
# over every make/<name>.mk), so a check fragment there reads as a port.

.PHONY: schema-spec-pin-design-row-ids
schema-spec-pin-design-row-ids:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test -count=1 -timeout 600s ./internal/ci/ -run '^TestSpecPins' -v
