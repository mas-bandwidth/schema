# make/schema-spec-pin-s34.mk — THE SPEC PINS OF §3.4
# (docs/SPEC-TABLES.md, "3.4 The fixed form"): every normative sentence carries
# a row id, every row is pinned in every port that carries its feature, and the
# guard's own witnesses plant each defect. Unit tier, seconds, so it rides every
# pull request that can move a row or a pinned test.
#
# The Makefile's `include $(wildcard make/*.mk)` picks this fragment up, so no
# Makefile edit is needed. NOTE: that same wildcard is the discovered-port
# registry (tables-ports-refuse-wide-scalars runs `schema generate --lang` over
# every make/<name>.mk), so that gate now also probes "schema-spec-pin-s34" and
# fails because it is not a backend. The card named this path and forbade a
# Makefile edit; the design card's own fragment lives in make/checks/, included
# by path. Reported as a PATHS/PLACEMENT conflict in RESULT.md.
.PHONY: schema-spec-pin-s34
schema-spec-pin-s34:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 sh test/slowgate/proof schema-spec-pin-s34 \
		'TestSpecPinsS34HoldOnTheSpec TestSpecPinsS34SectionSizes TestSpecPinsS34FixturePasses TestSpecPinsS34Refusals TestSpecPinsS34RealSpecPlants' \
		./internal/ci/ -run '^TestSpecPinsS34' -count=1 -timeout 600s
