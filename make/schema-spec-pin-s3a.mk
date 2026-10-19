# make/schema-spec-pin-s3a.mk — THE SPEC-PINS GUARD, SECTIONS 3 THROUGH 3.3
# (docs/SPEC-TABLES.md: the wire, pointers on the wire, enum-keyed arrays on
# the wire, and the message form). Every normative sentence in those sections
# carries a row id; every row is pinned to a test; the pins carry their row's
# text digest, so a reword fails until the pin is re-read. Unit tier, seconds.
#
# The pinned witness is TestSpecPinsS3ARealSpecPlant: it plants an unmarked
# normative sentence in §3.1 on the REAL spec, in memory, and requires the
# guard to go red; the unplanted spec must be green. The target goes through
# test/slowgate/proof, which requires a named --- PASS for each test (a skip
# is red), and keeps the log at
# build/slowgate/schema-spec-pin-s3a.log.

.PHONY: schema-spec-pin-s3a
schema-spec-pin-s3a:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s3a \
		'TestSpecPinsHoldOnTheSpec TestSpecPinsS3ARealSpecPlant' \
		./internal/ci/ -run '^TestSpecPins' -count=1 -timeout 600s
