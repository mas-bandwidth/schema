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
# The fixed-form tests read the reference's corpus, so it is built first.
.PHONY: test-moo-tables
test-moo-tables: toolchain-moo build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/mootable/...
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_moo -run 'Moo' ./compiler/

# the table surface is part of the moo leg's `make test` half: test-moo (make/moo.mk)
# takes it as a prerequisite, so the leg stays one leg (REGISTERED_LEGS)
test-moo: test-moo-tables

# THE FIXED FORM ON THE MOO LEG (docs/SPEC-TABLES.md §3.4,
# docs/FIXED-FORM-ALGORITHM.md): internal/codegen/mootable/fixedform_test.go
# reads the C++ reference's byte oracle out of build/fixedform-corpus — the nine
# corpus files (§7 row 1), every versioning row's two columns, the floor, and
# §5.3's refusals cut from fx1.bin. The harness SKIPS on a tree that never built
# the oracle, so this target builds it first and SCHEMA_REQUIRE_CORPUS=1 makes
# the skip a FAILURE.
.PHONY: tables-moo-fixed
tables-moo-fixed: toolchain-moo build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 ./internal/codegen/mootable/ -run 'TestMooFixed|TestMooReadStrict'

test-moo: tables-moo-fixed

# RETAIN-UNKNOWN's negative controls (docs/SPEC-TABLES.md §6.6; make/negative-controls.json,
# the `moo-tables` group). Each plants one thing out of the emitted runtime and
# requires the gate that holds it to go red for exactly that reason: the tail
# write of a kept record, and the fixed-class and nil-report refusals.
.PHONY: tables-moo-retain-negative-control tables-moo-retain-refusal-negative-control
tables-moo-retain-negative-control: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/mootable/ -run 'TestMooRetainNegativeControl$$'
tables-moo-retain-refusal-negative-control: toolchain-moo
	SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/mootable/ -run 'TestMooRetainRefusesWhatThePageRefuses$$'
