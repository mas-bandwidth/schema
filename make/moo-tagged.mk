# make/moo-tagged.mk — the MOO leg's Go packages whole, under the slow gate.
# Included by the Makefile's wildcard include; a dashed stem, so the port
# registry never probes it as a --lang.
#
# Moo is a built-in target of the default build (compiler/target_moo.go), so
# every cross-target gate in ./compiler meets it: the carrier lists, the
# defaults and was rows, the packet claims. The target keeps its name from
# when the leg sat behind a build tag; it now runs, with no tag, both packages
# whole under SCHEMA_SLOW=1 with the pinned interpreter, with the conformance
# harness's moo admission (test/conformance/harness/implementation_moo.go),
# in one `go test` that builds and runs the three in parallel. The
# fixed-form tests read the reference's corpus, so it is built first and a
# missing one is a failure.
.PHONY: test-moo-tagged
test-moo-tagged: toolchain-moo build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 60m ./compiler/ ./internal/codegen/mootable/ ./test/conformance/harness/
