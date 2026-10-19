# schema-shared-differential-fuzz: nightly differential fuzzing of the shared
# port's generated readers. The harness is internal/ci's native coverage-guided
# Go fuzzer (FuzzSchemaSharedDifferential), fed the repo's Go fuzz corpus and
# the test-schema corpus the generate targets read. Every mutated input runs
# through every port and the verdicts must agree; a panic, hang or silent empty
# success on malformed input is a finding.
#
# The heavy search is nightly (never per push). The replay gate is fast and
# runs on every push. The planted-bad-pattern control proves the gate fires.
#
# Toolchain: go (go.mod) and make, on the Linux bench.

# time budget for the nightly search, per port (default 10 minutes)
SCHEMA_DIFF_FUZZ_TIME ?= 10m

.PHONY: schema-shared-differential-fuzz \
	schema-shared-differential-fuzz-replay \
	schema-shared-differential-fuzz-negative-control

# NIGHTLY: coverage-guided fuzzing of the whole read surface for the budget.
schema-shared-differential-fuzz:
	@command -v go >/dev/null 2>&1 || { echo 'tool missing: go'; exit 1; }
	go test ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' \
		-fuzz FuzzSchemaSharedDifferential -fuzztime $(SCHEMA_DIFF_FUZZ_TIME) \
		-count=1 -timeout 600s

# FAST: replay every saved regression input on every push. No real fuzz time.
schema-shared-differential-fuzz-replay:
	go test ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' \
		-count=1 -timeout 600s

# PROOF THE GATE FIRES: green with the plant out, red with the plant in.
schema-shared-differential-fuzz-negative-control:
	@SCHEMA_DIFF_FUZZ_PLANT=1 sh -c 'if go test ./internal/ci -run "^TestSchemaSharedDifferentialFuzzPlantControl$$" -count=1 -timeout 600s >/dev/null 2>&1; then echo "NEGATIVE CONTROL FAILED: placed reader bug stayed green"; exit 1; else echo "plant in: red (correct)"; fi'
	go test ./internal/ci -run '^TestSchemaSharedDifferentialFuzzPlantControl$$' \
		-count=1 -timeout 600s
	@echo 'schema-shared differential fuzz negative control: plant red in, green out'
