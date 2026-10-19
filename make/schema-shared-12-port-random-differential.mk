# schema-shared-12-port-random-differential (card). The 12-port random-input
# differential sends the same deterministic inputs through every generated
# reader/writer leg and compares verdicts (accept/refuse, refusal reason,
# decoded value), extending the conformance harness over the generated shared
# output. Heavy: nightly.
#
# The roster is DISCOVERED, not listed: every test/conformance/<lang>/driver is
# a leg, exactly as `make conformance` discovers them, and a leg that does not
# start is reported as "tool missing: <lang>" rather than faked. The inputs are
# generated once with a fixed seed from the block rows of the one corpus
# manifest (testdata/conformance/tables/MANIFEST.txt) and written into build/;
# no second corpus and no hand-written checker.
#
# The fast half is the readiness probe (toolchain presence, generated tree and
# a registered driver for every generated port) and runs per push. The heavy
# verdict-comparison body runs the whole conformance matrix first so every leg
# is built, then feeds the same mutants through every driver.
.PHONY: schema-shared-12-port-random-differential \
	schema-shared-12-port-random-differential-verdicts \
	schema-shared-12-port-random-differential-negative-control

schema-shared-12-port-random-differential:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test ./internal/ci \
		-run '^TestSchemaSharedTwelvePortRandomDifferential$$' -count=1 -timeout 600s

# Nightly: the same deterministic random inputs through every leg. `conformance`
# is the prerequisite so every discovered driver has its built leg; the body is
# named here so a red says which leg and which input.
schema-shared-12-port-random-differential-verdicts: conformance
	SCHEMA_SHARED_12_PORT_DIFFERENTIAL=1 GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 \
		go test ./internal/ci \
		-run '^TestSchemaSharedTwelvePortRandomDifferentialVerdicts$$' -count=1 -timeout 600s

# The planted-bad-pattern control: a known-bad answer (a leg that refuses what
# the reference accepts, and a leg that names a different refusal reason) must
# make the comparator red and name the plant. The plant is in-test, so the
# control needs no scratched generated tree and runs with the nightly body.
schema-shared-12-port-random-differential-negative-control:
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 go test ./internal/ci \
		-run '^TestSchemaSharedTwelvePortRandomDifferentialNegativeControl$$' -count=1 -timeout 600s
