# make/schema-moo-nightly-fuzz.mk — NIGHTLY FUZZING of the moo packet wire
# (docs/PORT-MOO.md, THE LAYERS: layer 6), keyed like the Dart and Luau
# nightlies: a seeded long run on the schedule, never on a push.
#
# THE FUZZER is the differential make/moo-fuzz.mk names
# (internal/codegen/moo/fuzz_test.go), run for MOO_NIGHTLY_FUZZ_ROUNDS rounds of
# MOO_NIGHTLY_FUZZ_N instances per corpus unit, round k on seed
# MOO_NIGHTLY_FUZZ_SEED + k. The corpus is every packet corpus the leg
# generates (analyzeCorpora in internal/codegen/moo/analyze_test.go: examples,
# examples128 and test/packet-*), so the list and the corpus cannot drift apart.
#
# THE BUDGET IS A CASE COUNT, NEVER A CLOCK. 100 rounds of 2000 instances a
# unit is 1.6 million instances and about 6.3 million reads, under three minutes
# on the studio; every case is a pure function of its seed, so a red night
# replays with SCHEMA_MOO_FUZZ_SEED=<the round's seed> SCHEMA_MOO_FUZZ_ROUNDS=1.
# The workflow passes the run id as the seed, so each night draws new cases
# and logs the seed it drew them from.
#
# THE FINDINGS: each failing case is written to $(MOO_NIGHTLY_FUZZ_BUILD)/findings
# (its seed, unit, root, the diverging lines, the moo statements and the Go
# set lines that built it), which the workflow keeps on a red night.
#
# THE PLANTED PATTERNS: the two reversed witnesses, a writer that drops the top
# bit of an 8-bit field and a union reader whose tag bound is dropped, each of
# which must turn the differential red. They also run on every push inside
# `make test-moo`; the nightly runs them again beside the long run so a night
# that is green is a night whose fuzzer could see.

MOO_NIGHTLY_FUZZ_ROUNDS ?= 100
MOO_NIGHTLY_FUZZ_N      ?= 2000
MOO_NIGHTLY_FUZZ_SEED   ?= 1
MOO_NIGHTLY_FUZZ_BUILD  ?= build/moo-nightly-fuzz

.PHONY: schema-moo-nightly-fuzz
schema-moo-nightly-fuzz: toolchain-moo
	@rm -rf $(MOO_NIGHTLY_FUZZ_BUILD) && mkdir -p $(MOO_NIGHTLY_FUZZ_BUILD)/findings
	@echo "moo nightly fuzz: seed $(MOO_NIGHTLY_FUZZ_SEED), $(MOO_NIGHTLY_FUZZ_ROUNDS) rounds of $(MOO_NIGHTLY_FUZZ_N) instances a unit"
	SCHEMA_MOO_FUZZ_SEED=$(MOO_NIGHTLY_FUZZ_SEED) SCHEMA_MOO_FUZZ_N=$(MOO_NIGHTLY_FUZZ_N) \
		SCHEMA_MOO_FUZZ_ROUNDS=$(MOO_NIGHTLY_FUZZ_ROUNDS) \
		SCHEMA_MOO_FUZZ_FAILURES=$(CURDIR)/$(MOO_NIGHTLY_FUZZ_BUILD)/findings \
		sh test/slowgate/proof schema-moo-nightly-fuzz 'TestMooWireFuzzAgreesWithTheGoTarget' \
		-count=1 -timeout 40m ./internal/codegen/moo/ \
		-run '^TestMooWireFuzzAgreesWithTheGoTarget$$' -v
	@grep -h 'no divergence' build/slowgate/schema-moo-nightly-fuzz.log

.PHONY: schema-moo-nightly-fuzz-negative-control
schema-moo-nightly-fuzz-negative-control: toolchain-moo
	SCHEMA_MOO_FUZZ_SEED=$(MOO_NIGHTLY_FUZZ_SEED) \
		sh test/slowgate/proof schema-moo-nightly-fuzz-negative-control \
		'TestMooWireFuzzWriteNegativeControl TestMooWireFuzzReadNegativeControl' \
		-count=1 -timeout 600s ./internal/codegen/moo/ \
		-run '^TestMooWireFuzz(Write|Read)NegativeControl$$' -v
