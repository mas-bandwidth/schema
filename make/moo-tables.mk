# make/moo-tables.mk — the MOO leg's TABLE surface (internal/codegen/mootable;
# docs/PORT-MOO.md, THE LAYERS, layer 3). Included by the Makefile's wildcard
# include, beside make/moo.mk, whose toolchain (dist-moo, $(MOO),
# $(MOO_ANALYZE)) it uses.
#
# test-moo-tables runs the table runtime under the pinned moo: the descriptors
# loaded and held to fresh records (moo-analyze silent over every generated
# unit), the variable wire's load against the conformance reports
# (testdata/conformance/tables/reports.txt), the save byte for byte against
# every instance wire, each construct round-tripped from a wire the Go
# reference engine wrote, each gate's negative control; then the target's
# compiler claims under its build tag (compiler/tablesmoo_test.go). No real
# time, no socket, no network: inputs go in as source (moorun).
.PHONY: test-moo-tables
test-moo-tables: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/mootable/...
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_moo -run 'Moo' ./compiler/

# the table surface is part of the moo leg's `make test` half: test-moo (make/moo.mk)
# takes it as a prerequisite, so the leg stays one leg (REGISTERED_LEGS)
test-moo: test-moo-tables
