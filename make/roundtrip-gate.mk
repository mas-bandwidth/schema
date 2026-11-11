# make/roundtrip-gate.mk — THE ROUND-TRIP GATE (test/roundtrip). One row per
# language leg, `make roundtrip-<leg>`: the leg generates tables/examples, and a
# probe written against the generated code alone saves and loads every root's
# generated-test samples (docs/SPEC-TABLES.md §16.11) and is held to the Go
# engine's bytes (internal/tablewire.Encode): the plain pair round-trips, the
# fixed pair and the plain pair at a fixed root are the engine's form 3 both
# ways, the root past the record ceiling refuses its file save, and the
# variable root round-trips through every load, retention and JSON. The
# assertions are listed in test/roundtrip/roundtrip_test.go.
#
# THE RATCHET is test/roundtrip/known-red.tsv: a row fails on a red pair the
# list does not name AND on a listed pair that has gone green, so the list only
# shrinks. A row that compared nothing fails.
#
# Each row is its own CI job (ci-full.yml, `roundtrip`), cut to the per-commit
# two-minute rule. The negative controls plant one break in a scratch copy of
# the generated code and require the row to go red on it
# (make/negative-controls.json, the roundtrip-<leg> groups).
#
# The stem has a dash, so the port registry (the Makefile's SCHEMA_PORTS) never
# probes this file as a --lang.

ROUNDTRIP_LEGS := go js lua

ROUNDTRIP_PROOF = GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_REQUIRE_CORPUS=1 nice -n 19 sh test/slowgate/proof

# roundtrip_row,<leg>,<test>: the slow-gate proof over one subtest of <test>,
# named so the proof requires its own `--- PASS`.
roundtrip_row = $(ROUNDTRIP_PROOF) $(if $(filter TestRoundTrip,$(2)),roundtrip-$(1),roundtrip-$(1)-negative-control) \
	'$(2)/$(1)' ./test/roundtrip -run '^$(2)$$/^$(1)$$' -count=1 -timeout 600s

.PHONY: roundtrip $(addprefix roundtrip-,$(ROUNDTRIP_LEGS))
roundtrip: $(addprefix roundtrip-,$(ROUNDTRIP_LEGS))

roundtrip-go: bin/schema tables-go-sibling-runtime
	SERIALIZE_GO="$(abspath $(SERIALIZE_GO))" $(call roundtrip_row,go,TestRoundTrip)

roundtrip-js: bin/schema
	@$(call toolchain_probe,js,NODE,$(NODE))
	NODE="$(NODE)" $(call roundtrip_row,js,TestRoundTrip)

roundtrip-lua: bin/schema toolchain-lua
	LUA="$(LUA)" $(call roundtrip_row,lua,TestRoundTrip)

# THE NEGATIVE CONTROLS, one per row, each its own explicit target so
# tools/negativecontrols enumerates it. The plant, and the pair it must turn
# red, is the leg's entry in test/roundtrip/legs_test.go.
.PHONY: roundtrip-go-negative-control roundtrip-js-negative-control roundtrip-lua-negative-control
roundtrip-go-negative-control: bin/schema tables-go-sibling-runtime
	SERIALIZE_GO="$(abspath $(SERIALIZE_GO))" $(call roundtrip_row,go,TestRoundTripNegativeControl)

roundtrip-js-negative-control: bin/schema
	@$(call toolchain_probe,js,NODE,$(NODE))
	NODE="$(NODE)" $(call roundtrip_row,js,TestRoundTripNegativeControl)

roundtrip-lua-negative-control: bin/schema toolchain-lua
	LUA="$(LUA)" $(call roundtrip_row,lua,TestRoundTripNegativeControl)
