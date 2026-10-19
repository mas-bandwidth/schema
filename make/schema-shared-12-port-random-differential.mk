# schema-shared-12-port-random-differential (card). The 12-port random-input
# differential sends the same deterministic inputs through every generated
# reader/writer leg and compares verdicts (accept/refuse, refusal reason,
# decoded value), extending the conformance harness over the generated shared
# output. Heavy: nightly. The fast probe (toolchain presence and generated-tree
# readiness) runs on push.
#
# Toolchain probe: the differential needs the port toolchains. The checker is
# the repository's own differential harness (no external checker is named by
# the card); each leg's toolchain is probed inside the Go test, and a missing
# tool is reported as "tool missing: <name>" rather than faked.
.PHONY: schema-shared-12-port-random-differential \
	schema-shared-12-port-random-differential-negative-control

schema-shared-12-port-random-differential:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test ./internal/ci \
		-run '^TestSchemaSharedTwelvePortRandomDifferential$$' -count=1 -timeout 600s

# The planted-bad-pattern control: a test-only switch that flips one rendered
# byte in a scratch copy of one leg's generated output; the differential must
# go red and name the plant.
schema-shared-12-port-random-differential-negative-control:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test ./internal/ci \
		-run '^TestSchemaSharedTwelvePortRandomDifferentialNegativeControl$$' -count=1 -timeout 600s
