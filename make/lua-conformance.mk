# make/lua-conformance.mk — the LUA leg's conformance controls
# (test/conformance/README.md; docs/PORT-LUA.md). Included by the Makefile's
# wildcard include. The reference leg is cpp; this is a discovered, non-reference
# leg, so its absences are ordinary.

# THE LUA LEG's NEGATIVE CONTROLS, on the same rule as the Go ones: a harness
# that has never gone red on a leg is watching that leg. Both sabotage the
# EMITTER (docs/PORTING.md I2): a sed over a copy of one runtime file the table
# emitter embeds, checked to have matched, `go build -overlay` into a private
# compiler, the units regenerated, and the harness required red on exactly the
# named surfaces with the neighbours green. test/conformance/lua/negative-control
# holds both and says what each one requires.
#
# conformance-negative-control-lua       the load report's unknown counter
#                                        (report and retain red, wire green)
# conformance-negative-control-lua-walk  the text walk's field-key match (I11:
#                                        json-read and json-hostile red,
#                                        json-write and wire green)
# conformance-negative-control-lua-framing  the id table's 4096-entry bound
#                                        lifted (lua / framing red on
#                                        frame_entries_4097, wire and report green)
.PHONY: conformance-negative-control-lua conformance-negative-control-lua-walk conformance-negative-control-lua-framing
conformance-negative-control-lua: build/conformance-harness
	sh test/conformance/lua/negative-control report

conformance-negative-control-lua-walk: build/conformance-harness
	sh test/conformance/lua/negative-control walk
conformance-negative-control-lua-framing: build/conformance-harness
	sh test/conformance/lua/negative-control framing
