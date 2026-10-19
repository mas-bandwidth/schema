# make/checks/schema-spec-pin-s45.mk — THE SPEC-PINS GUARD, SECTIONS 4 AND 5
# (docs/SPEC-TABLES.md, "Row ids and pins"): every MUST, MUST NOT and refusal
# sentence of the versioning and identity sections carries a row id, every row
# is pinned in every port that carries its feature, and every pin resolves to a
# test or harness that runs. The guard is the design card's
# internal/ci/schema_spec_pin_design_row_ids_test.go; this file is the pin group
# for §4 and §5 (internal/ci/schema_spec_pin_s45_test.go).
#
# This is a check fragment, not a port: make/*.mk is the discovered-port
# registry. Until the Makefile includes this path beside the design card's
# check, make/schema-spec-pin-s45.mk is a one-line shim that the wildcard
# discovers, and this file holds the target.
#
# Unit tier, under a minute. It goes through test/slowgate/proof so a skip is
# red and a named --- PASS is required for the guard, its reversed witnesses
# and the planted-spec witnesses; the log is
# build/slowgate/schema-spec-pin-s45.log.
.PHONY: schema-spec-pin-s45
schema-spec-pin-s45:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s45 \
		'TestSpecPinsSectionSizes TestSpecPinsHoldOnTheSpec TestSpecPinsFixturePasses TestSpecPinsRefusals TestSpecPinsRealSpecPlants TestSpecPinsRefusals/pinned_row_reworded_with_its_marker_kept TestSpecPinsRefusals/pin_with_no_digest TestSpecPinsRefusals/pinned_row_code_span_renamed TestSpecPinsRefusals/owed_entry_under_a_reworded_row TestSpecPinsRealSpecPlants/stale_row_id_in_the_registry TestSpecPinsRealSpecPlants/a_pin_naming_a_row_the_spec_never_had TestSpecPinsRealSpecPlants/the_worked_example_rule_reversed_with_its_marker_kept' \
		./internal/ci/ -run '^TestSpecPins' -count=1 -timeout 600s
