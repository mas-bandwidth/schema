# make/schema-spec-pin-s3a.mk — THE SPEC-PINS GUARD, SECTIONS 3 THROUGH 3.3
# (docs/SPEC-TABLES.md: the wire, pointers on the wire, enum-keyed arrays on
# the wire, and the message form). Every normative sentence in those sections
# carries a row id. A row/port without a verified behavioral test is explicitly
# Owed; a green drift guard does not mean those behaviors are pinned. Digests
# require every reword to be re-read. Unit tier for the drift guard.
#
# The structural witness is TestSpecPinsS3ARealSpecPlant: it plants an unmarked
# normative sentence in §3.1 on the REAL spec, in memory, and requires the
# guard to go red; the unplanted spec must be green. The target goes through
# test/slowgate/proof, which requires a named --- PASS for each test (a skip
# is red), and keeps the log at
# build/slowgate/schema-spec-pin-s3a.log.

.PHONY: schema-spec-pin-s3a
schema-spec-pin-s3a:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s3a \
		'TestSpecPinsHoldOnTheSpec TestSpecPinsS3ARealSpecPlant' \
		./internal/ci/ -run '^(TestSpecPinsHoldOnTheSpec|TestSpecPinsS3ARealSpecPlant)$$' -count=1 -timeout 600s
