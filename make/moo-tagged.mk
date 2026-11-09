# make/moo-tagged.mk — the MOO leg's Go packages under its build tag.
# Included by the Makefile's wildcard include; a dashed stem, so the port
# registry never probes it as a --lang.
#
# `-tags schema_leg_moo` registers the moo target (compiler/target_moo.go), and
# with it every cross-target gate in ./compiler meets one more target: the
# carrier lists, the defaults and was rows, the packet claims. The untagged
# runs never see it, and test-moo-tables runs only the compiler's tests named
# Moo, so this target runs both packages whole under the tag, with the
# conformance harness's tagged half (test/conformance/harness, mooTagged),
# in one `go test` that builds and runs the three in parallel. The
# fixed-form tests read the reference's corpus, so it is built first and a
# missing one is a failure.
.PHONY: test-moo-tagged
test-moo-tagged: toolchain-moo build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 60m -tags schema_leg_moo ./compiler/ ./internal/codegen/mootable/ ./test/conformance/harness/

# THE SPLIT HALVES ci-full.yml's leg-tagged MATRIX (2026-11-09). The whole
# tagged leg is ~100 s of wall time, which leaves no room for a row's own
# checkout and pin setup inside the fleet's 110 s per-commit budget. These two
# targets split the leg into the compiler alone, and the table and conformance
# packages together, so each matrix row runs in parallel and neither carries the
# whole leg plus its setup. Nothing moves between tiers: make/negative-controls.json
# is untouched, and `make test-moo-tagged` above still runs the whole leg for a
# workstation, certify.yml and `make test`.
.PHONY: test-moo-tagged-compiler test-moo-tagged-table
test-moo-tagged-compiler: toolchain-moo build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 60m -tags schema_leg_moo ./compiler/
test-moo-tagged-table: toolchain-moo build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 60m -tags schema_leg_moo ./internal/codegen/mootable/ ./test/conformance/harness/
