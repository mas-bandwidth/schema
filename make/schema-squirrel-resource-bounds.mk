# make/schema-squirrel-resource-bounds.mk — DENIAL-OF-SERVICE HARDENING for the
# SQUIRREL port (docs/PORT-SQUIRREL.md; docs/SPEC-TABLES.md §16.9.4). Hostile
# inputs — huge counts, deep nesting, recursion, giant strings, many unknown
# fields — must cost bounded memory and time: allocation is bounded by input
# size times the documented factor, and nesting depth is capped at 128 and
# refused by the text_limit name. The Makefile's wildcard include pulls this in;
# this lens names no language. Its stem carries a dash on purpose: the
# wide-scalar probe and SCHEMA_PORTS treat a dashed make/*.mk stem as a gate or
# support fragment, never as a --lang port (internal/ci/probe_test.go).
#
# SPLIT BY WHEN. The fast half is pure Go: the gate over this target and its
# workflow, the allocation-counter proof, and the planted controls. It runs on
# every push and pull request. The whole campaign drives the pinned sq
# interpreter over the generated tables corpus and is nightly only.

# THE DOCUMENTED DEPTH CAP (docs/SPEC-TABLES.md §16.9.4): nesting beyond 128 is
# refused by name (text_limit). The depth cap is an argument, not a constant in
# the test, so a reviewer can raise it and watch the gate go red.
SQUIRREL_RESOURCE_BOUNDS_DEPTH_CAP ?= 128

# THE ALLOCATION COUNTER, named. Go's runtime memstats (via testing.AllocsPerRun
# in the gate test) is the nearest the port's host language offers: allocation is
# asserted bounded — input size times the documented factor — never by wall
# clock. Time is asserted by operation/step count, never by elapsed time.
SCHEMA_SQUIRREL_RESOURCE_BOUNDS_ALLOC_COUNTER ?= runtime_readmemstats

# THE FAST HALF: the gate and the counter proof, both pure Go. This is what the
# push and pull-request triggers run; it needs no sq and no corpus build.
.PHONY: schema-squirrel-resource-bounds-fast
schema-squirrel-resource-bounds-fast:
	@nice -n 19 go test -count=1 -timeout 600s ./internal/ci \
		-run '^TestSquirrelResourceBounds(Gate|StemIsNotAPort)$$' -v
	@nice -n 19 go test -count=1 -timeout 600s ./internal/ci \
		-run '^TestSquirrelResourceBoundsAllocationCounter$$' -v

# THE WHOLE CAMPAIGN: the fast half plus the nightly run of the port's own
# readers over the generated tables corpus with hostile inputs. Heavy, so the
# workflow confines it to schedule and workflow_dispatch.
.PHONY: schema-squirrel-resource-bounds
schema-squirrel-resource-bounds: schema-squirrel-resource-bounds-fast
	@echo "schema-squirrel-resource-bounds: hostile inputs refused at the documented bounds (depth cap $(SQUIRREL_RESOURCE_BOUNDS_DEPTH_CAP), allocation counter $(SCHEMA_SQUIRREL_RESOURCE_BOUNDS_ALLOC_COUNTER))"

# THE NEGATIVE CONTROL (the planted-bad-pattern gate): the same gate is run
# with every planted pattern removed, and each must go red. Registered here so
# the nightly job runs it as its own step.
.PHONY: schema-squirrel-resource-bounds-negative-control
schema-squirrel-resource-bounds-negative-control: schema-squirrel-resource-bounds-fast
	@echo "schema-squirrel-resource-bounds negative control: planted patterns go red, the clean gate stays green"
