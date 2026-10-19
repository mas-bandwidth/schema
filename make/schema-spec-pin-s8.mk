# make/schema-spec-pin-s8.mk — the section 8 pin gate
# (docs/SPEC-TABLES.md section 8 and every later section): every normative
# sentence carries a row id, every row is pinned in every port that carries
# its feature or owed by name, and every pin resolves to a test that runs. The
# drift guard is the design card's internal/ci/schema_spec_pin_design_row_ids_test.go;
# this card adds its registry in internal/ci/schema_spec_pin_s8_test.go and its
# own reversed witness. Unit tier, seconds. It goes through test/slowgate/proof,
# which requires a named --- PASS for each test listed (a skip is red), and
# keeps the log at build/slowgate/schema-spec-pin-s8.log.

.PHONY: schema-spec-pin-s8
schema-spec-pin-s8:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s8 \
		'TestSpecPinsHoldOnTheSpec TestSpecPinsS8Hold TestSpecPinsS8Plant' \
		./internal/ci/ -run '^TestSpecPins(HoldOnTheSpec|S8)' -count=1 -timeout 600s
