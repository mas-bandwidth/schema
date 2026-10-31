# make/moo-fuzz.mk — the MOO leg's packet-wire fuzz differential
# (docs/PORT-MOO.md, THE LAYERS: layer 6). Included by the Makefile's wildcard
# include, after make/moo.mk.
#
# The differential is internal/codegen/moo/fuzz_test.go: random instances of
# every packet type and union of every packet corpus the leg generates, drawn
# inside their declared contracts, written by the generated moo codecs and by
# the Go target's (serialize.go, the sibling checkout) and required to be
# byte-identical; the Go images and mutants of them read by both readers, which
# must agree on accept/refuse, on the refusal reason the runtimes latch, and on
# the bytes each writes back. `make test-moo` runs it too (it is a go test of the
# package); these targets name it, with its seed and size, and its two reversed
# witnesses (a planted writer bug and a planted reader bug, each in a scratch
# copy of the generated moo, each required to turn the differential red).
#
# The budget is an instance count, never a clock: a seed and a count replay a
# round exactly. The nightly rounds are make/schema-moo-nightly-fuzz.mk.

MOO_FUZZ_SEED ?= 20261028
MOO_FUZZ_N    ?= 400

.PHONY: moo-wire-fuzz
moo-wire-fuzz: toolchain-moo
	SCHEMA_MOO_FUZZ_SEED=$(MOO_FUZZ_SEED) SCHEMA_MOO_FUZZ_N=$(MOO_FUZZ_N) SCHEMA_MOO_FUZZ_ROUNDS=1 \
		sh test/slowgate/proof moo-wire-fuzz \
		'TestMooWireFuzzAgreesWithTheGoTarget TestMooWireFuzzWriteNegativeControl TestMooWireFuzzReadNegativeControl' \
		-count=1 -timeout 600s ./internal/codegen/moo/ \
		-run '^TestMooWireFuzz(AgreesWithTheGoTarget|WriteNegativeControl|ReadNegativeControl)$$' -v
