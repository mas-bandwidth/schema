# make/squirrel-conformance.mk — the SQUIRREL leg's conformance negative control
# (docs/PORT-SQUIRREL.md, THE CARD MAP; test/conformance/README.md, "Seven
# negative controls"). The Makefile's wildcard include pulls this in; it names
# no language.
#
# The control overlays the emitted load runtime (tools/sabotage builds the copy,
# the tagged compiler is rebuilt under the overlay) and regenerates the corpus,
# then runs the SAME driver against the saboteur's tree via
# $SCHEMA_SQUIRREL_CONFORMANCE. The report surface must go red; the wire surface
# must stay green (test/conformance/squirrel/negative-control).

.PHONY: conformance-negative-control-squirrel
conformance-negative-control-squirrel: build/conformance-harness build/conformance-squirrel toolchain-squirrel
	sh test/conformance/squirrel/negative-control
