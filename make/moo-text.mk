# make/moo-text.mk — the MOO leg's TEXT FORMS (docs/SPEC-TABLES.md §16, §16.8;
# docs/PORT-MOO.md, THE LAYERS, layer 4). Included by the Makefile's wildcard
# include; make/moo.mk is the leg and pins the toolchain.
#
# runtime/json.moo is the descriptor-driven JSON walk and its exact number
# conversions (moo has no float); runtime/textcarrier.moo the base64 carrier.
# Every gate runs the generated modules under the pinned moo, inputs in as
# source, no real time, no socket, no network (internal/codegen/mootable/
# json_*_test.go, text_*_test.go).
#
#   tables-moo-json            the float writer and reader against the engine,
#                              the golden round trip, the json-hostile verdicts,
#                              json-read and json-write as two gates, JSONTestSuite,
#                              the base64 carrier
#   tables-moo-json-differential
#                              I13: random draws, moo's text against the engine's,
#                              both directions
#   tables-moo-json-negative-controls
#                              every gate's control, each red for its one reason:
#                              the walk control (json-read red, json-write green),
#                              the float tie rule both ways, the I13 tie-break, the
#                              JSONTestSuite end-of-text rule, the carrier pad bits
MOO_TEXT_GATES    := TestMooJsonFloat(Write|Read)MatchesEngine|TestMooJsonGoldensRoundTrip|TestMooJsonHostileVerdicts|TestMooJsonWriteAndReadGates|TestMooJSONTestSuite$$|TestMooTextCarrier$$|TestMooTextCarrierSurface|TestMooJsonWireSurfaces|TestMooJsonOneWalk
MOO_TEXT_CONTROLS := NegativeControl$$

.PHONY: tables-moo-json tables-moo-json-differential tables-moo-json-negative-controls
tables-moo-json: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 -run '$(MOO_TEXT_GATES)' ./internal/codegen/mootable/
tables-moo-json-differential: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 -run 'TestMooJsonTextDifferential$$' ./internal/codegen/mootable/
tables-moo-json-negative-controls: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 -run 'TestMoo(Json|JSON|Text).*$(MOO_TEXT_CONTROLS)' ./internal/codegen/mootable/

test-moo: tables-moo-json tables-moo-json-differential tables-moo-json-negative-controls
