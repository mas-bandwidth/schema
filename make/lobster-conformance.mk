# make/lobster-conformance.mk — the LOBSTER leg's conformance controls
# (test/conformance/README.md; docs/PORT-LOBSTER.md, THE HARNESS). Included by
# the Makefile's wildcard include. The reference leg is cpp; this is a
# discovered, non-reference leg, so its absences are ordinary.
#
# Each sabotages the EMITTER (docs/PORTING.md I2): a sed over a copy of one
# runtime file the table emitter embeds, checked to have matched,
# `go build -overlay` into a private compiler, the units regenerated, and the
# harness required red on exactly the named surfaces with the neighbours green.
# test/conformance/lobster/negative-control holds all three and says what each
# one requires.
#
# conformance-negative-control-lobster       the load report's unknown counter
#                                            (report, framing and retain red,
#                                            wire green)
# conformance-negative-control-lobster-walk  the text walk's field-key match
#                                            (I11: json-read and json-hostile
#                                            red, json-write and wire green)
# conformance-negative-control-lobster-framing the id table's 4096 entry bound
#                                            (docs/LAYERS/1-framing.md F6:
#                                            framing red alone)
.PHONY: conformance-negative-control-lobster conformance-negative-control-lobster-walk conformance-negative-control-lobster-framing
conformance-negative-control-lobster: lobster-dist-present build/conformance-harness
	sh test/conformance/lobster/negative-control report

conformance-negative-control-lobster-walk: lobster-dist-present build/conformance-harness
	sh test/conformance/lobster/negative-control walk

conformance-negative-control-lobster-framing: lobster-dist-present build/conformance-harness
	sh test/conformance/lobster/negative-control framing

# The controls ride the leg: `make test` reaches them through test-lobster.
test-lobster: conformance-negative-control-lobster conformance-negative-control-lobster-walk conformance-negative-control-lobster-framing
