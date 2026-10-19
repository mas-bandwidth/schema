# Section 2 of docs/SPEC-TABLES.md: row ids, per-port pins, and a planted
# unpinned rule. This check is deliberately outside make/*.mk, whose wildcard
# is the language-port registry.
.PHONY: schema-spec-pin-s2
schema-spec-pin-s2:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s2 \
		'TestSpecPinsHoldOnTheSpec TestSpecPinsS2Plant' \
		./internal/ci/ -run '^TestSpecPins(HoldOnTheSpec|S2Plant)$$' -count=1 -timeout 600s
