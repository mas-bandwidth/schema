# make/newleg-tagged.mk — the conformance harness's tagged half, for a leg
# `schema new-leg` is still writing. Included by the Makefile's wildcard
# include; a dashed stem, so the port registry never probes it as a --lang.
#
# Every shipped target is built in, in every build (Glenn, 2026-11-21: "all
# languages, by default, all the time."). The one tagged admission left is a
# new leg's: test/conformance/harness admits it only when built with
# schema_leg_<its name>. tagprobe is a made-up leg standing in for one, and
# implementation_tagprobe_on_test.go is the tagged half of the check that says
# so; no untagged run compiles it. This target runs the package under its tag.
.PHONY: test-tagged-harness
test-tagged-harness:
	SCHEMA_SLOW=1 go test -count=1 -tags schema_leg_tagprobe ./test/conformance/harness/
