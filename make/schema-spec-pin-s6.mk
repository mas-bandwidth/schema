# make/schema-spec-pin-s6.mk — SECTION 6'S SPEC PINS
# (docs/SPEC-TABLES.md §6, "The two lives of a table", the two load paths and
# retain-unknown): the section's normative sentences carry row ids and every
# row is pinned to a test in each port that carries its feature, or owed by
# name. The guard is internal/ci/schema_spec_pin_design_row_ids_test.go; this
# card's pins are internal/ci/schema_spec_pin_s6_test.go. Unit tier, seconds.
# It goes through test/slowgate/proof, which requires a named --- PASS for
# every test and witness below (a skip is red), and keeps the log at
# build/slowgate/schema-spec-pin-s6.log.

.PHONY: schema-spec-pin-s6
schema-spec-pin-s6:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s6 \
		'TestSpecPinsHoldOnTheSpec TestSpecPinS6PlantedUnpinnedSentence TestSpecPinsRefusals TestSpecPinsRealSpecPlants' \
		./internal/ci/ -run '^(TestSpecPinsHoldOnTheSpec|TestSpecPinS6PlantedUnpinnedSentence|TestSpecPinsRefusals|TestSpecPinsRealSpecPlants)$$' -count=1 -timeout 600s
