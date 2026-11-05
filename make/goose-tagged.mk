# make/goose-tagged.mk — the conformance harness's goose-tagged half.
# Included by the Makefile's wildcard include; a dashed stem, so the port
# registry never probes it as a --lang.
#
# test/conformance/harness admits the goose leg only in a schema_leg_goose
# build (implementation_goose.go), and implementation_goose_on_test.go is the
# tagged half of the check that says so; no untagged run compiles it. This
# target runs the package under the tag.
.PHONY: test-goose-tagged-harness
test-goose-tagged-harness:
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_goose ./test/conformance/harness/
