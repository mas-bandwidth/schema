# make/checks/schema-spec-pin-s34.mk — THE SPEC PINS OF §3.4
# (docs/SPEC-TABLES.md, "3.4 The fixed form"): every normative sentence carries
# a row id, every row is pinned in every port that carries its feature, and the
# guard's own witnesses plant each defect. Unit tier, seconds, so it rides every
# pull request that can move a row or a pinned test.
#
# The Makefile includes this file by path: make/*.mk is the discovered-port
# registry (tables-ports-refuse-wide-scalars runs `schema generate --lang` over
# every make/<name>.mk), so a check fragment there reads as a port.
.PHONY: schema-spec-pin-s34
schema-spec-pin-s34:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s34 \
		'TestSpecPinsS34HoldOnTheSpec TestSpecPinsS34SectionSizes TestSpecPinsS34FixturePasses TestSpecPinsS34Refusals TestSpecPinsS34RealSpecPlants' \
		./internal/ci/ -run '^TestSpecPinsS34' -count=1 -timeout 600s
