# make/checks/schema-spec-pin-design-row-ids.mk — THE SPEC-PINS GUARD
# (docs/SPEC-TABLES.md, "Row ids and pins"): every normative sentence of the
# spec carries a row id, every row is pinned in every port that carries its
# feature or owed by name, and every pin resolves to a test that runs. Unit
# tier, seconds. The guard's own reversed witnesses (TestSpecPinsRefusals,
# TestSpecPinsRealSpecPlants) run in the same go test, so the target is red
# when any plant stops firing. It goes through test/slowgate/proof, which
# requires a named --- PASS for each test and witness listed (a skip is red),
# and keeps the log at build/slowgate/schema-spec-pin-design-row-ids.log.
#
# The Makefile includes this file by path: make/*.mk is the discovered-port
# registry (tables-ports-refuse-wide-scalars runs `schema generate --lang`
# over every make/<name>.mk), so a check fragment there reads as a port.

.PHONY: schema-spec-pin-design-row-ids
schema-spec-pin-design-row-ids:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-design-row-ids \
		'TestSpecPinsSectionSizes TestSpecPinsSizingUsesRoadmapPorts TestSpecPinsSizingInheritsHeadingScope TestSpecPinsHoldOnTheSpec TestSpecPinsFixturePasses TestSpecPinsRefusals TestSpecPinsRealSpecPlants TestSpecPinsTLALineCommentHidesABlockOpen TestSpecPinsRefusals/tla_definition_only_inside_a_block_comment TestSpecPinsRefusals/tla_definition_behind_a_line_comment_inside_a_block_comment TestSpecPinsRefusals/tla_definition_only_inside_a_nested_block_comment TestSpecPinsRefusals/tla_EXTENDS_only_inside_a_comment TestSpecPinsRealSpecPlants/stale_row_id_in_the_registry' \
		./internal/ci/ -run '^TestSpecPins' -count=1 -timeout 600s
