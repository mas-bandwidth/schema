# make/checks/schema-spec-pin-s0.mk — THE SPEC-PINS GUARD, s0 (the opening of
# docs/SPEC-TABLES.md): the title section, "The three wires", "The performance
# ladder", "What allocates, and what never does" and §1, "Purpose". Unit tier,
# seconds. It runs the design card's guard (schema-spec-pin-design-row-ids),
# whose own reversed witnesses run in the same go test, so the target is red
# when any plant stops firing; test/slowgate/proof requires a named --- PASS
# for every test and witness listed (a skip is red). The log is kept at
# build/slowgate/schema-spec-pin-s0.log.
#
# The Makefile includes this file by path: make/*.mk is the discovered-port
# registry (tables-ports-refuse-wide-scalars runs `schema generate --lang`
# over every make/<name>.mk), so a check fragment there reads as a port.

.PHONY: schema-spec-pin-s0
schema-spec-pin-s0:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s0 \
		'TestSpecPinsSectionSizes TestSpecPinsSizingUsesRoadmapPorts TestSpecPinsSizingInheritsHeadingScope TestSpecPinsHoldOnTheSpec TestSpecPinsFixturePasses TestSpecPinsRefusals TestSpecPinsS0Plants TestSpecPinsRealSpecPlants TestSpecPinsTLALineCommentHidesABlockOpen TestSpecPinsRefusals/tla_definition_only_inside_a_block_comment TestSpecPinsRefusals/tla_definition_behind_a_line_comment_inside_a_block_comment TestSpecPinsRefusals/tla_definition_only_inside_a_nested_block_comment TestSpecPinsRefusals/tla_EXTENDS_only_inside_a_comment TestSpecPinsRealSpecPlants/stale_row_id_in_the_registry TestSpecPinsRealSpecPlants/the_worked_example_rule_reversed_with_its_marker_kept TestSpecPinsRealSpecPlants/a_new_rule_in_the_pinned_section_excused_by_an_invented_card TestSpecPinsRealSpecPlants/a_new_rule_in_the_pinned_section_excused_by_the_landed_design_card TestSpecPinsRefusals/pinned_row_reworded_with_its_marker_kept TestSpecPinsRefusals/pin_with_no_digest TestSpecPinsRefusals/owed_marker_naming_a_card_off_the_closed_list TestSpecPinsRefusals/owed_marker_naming_a_card_whose_pins_have_landed TestSpecPinsRefusals/pinned_row_code_span_renamed TestSpecPinsRefusals/owed_entry_under_a_reworded_row TestSpecPinsRealSpecPlants/a_new_rule_in_the_pinned_section_excused_by_a_listed_card_that_has_landed' \
		./internal/ci/ -run '^TestSpecPins' -count=1 -timeout 600s
