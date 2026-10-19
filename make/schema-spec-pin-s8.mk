# make/schema-spec-pin-s8.mk — THE SECTION 8/9+ PIN GATE
# (docs/SPEC-TABLES.md, sections 8 through 21): every normative sentence of
# the section and every later section carries a row id, every row is pinned in
# every port that carries its feature or owed by name, and every pin resolves
# to a test that runs. Unit tier, seconds. The guard's own reversed witness
# (TestSpecPinsS8Plant) runs in the same go test, so the target is red when
# the plant stops firing. It goes through test/slowgate/proof, which
# requires a named --- PASS for each test listed, so a skip is red.
#
# The Makefile includes this file through its wildcard over make/*.mk. A dashed
# stem is a gate or support fragment, not a --lang port: SCHEMA_PORTS filters
# it out of tables-ports-refuse-wide-scalars, so the target is defined here
# while the port probe sees only live ports.

.PHONY: schema-spec-pin-s8
schema-spec-pin-s8:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s8 \
		'TestSpecPinsHoldOnTheSpec TestSpecPinsSectionSizes TestSpecPinsS8Registered TestSpecPinsS8Plant' \
		./internal/ci/ -run '^(TestSpecPinsHoldOnTheSpec|TestSpecPinsSectionSizes|TestSpecPinsS8Registered|TestSpecPinsS8Plant)$$' -count=1 -timeout 600s
