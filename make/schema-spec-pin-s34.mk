# make/schema-spec-pin-s34.mk — THE SPEC PINS OF §3.4
# (docs/SPEC-TABLES.md, "The fixed form"): every normative sentence carries a
# row id, every row is pinned in every port that carries its feature, and the
# guard's own witnesses plant each defect. Unit tier, seconds, so it rides every
# pull request that can move a row or a pinned test. The log is kept at
# build/slowgate/schema-spec-pin-s34.log.
#
# The Makefile includes this file through its wildcard over make/*.mk. A dashed
# stem is a gate or support fragment, not a --lang port: SCHEMA_PORTS filters
# it out of tables-ports-refuse-wide-scalars, so the target is defined here
# while the port probe sees only live ports.

.PHONY: schema-spec-pin-s34
schema-spec-pin-s34:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s34 \
		'TestSpecPinsSectionSizes TestSpecPinsSizingUsesRoadmapPorts TestSpecPinsSizingInheritsHeadingScope TestSpecPinsHoldOnTheSpec TestSpecPinsFixturePasses TestSpecPinsRefusals TestSpecPinsTLALineCommentHidesABlockOpen TestSpecPinsRealSpecPlants TestSpecPinsS34Plants' \
		./internal/ci/ -run '^TestSpecPins' -count=1 -timeout 600s
