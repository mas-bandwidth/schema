# Go-first coverage-guided fuzzing of the generated Go table reader. The shared
# harness owns the mutation oracles; the per-push gate only replays saved reds.
SCHEMA_DIFF_GO_VERSION ?= 1.26
SCHEMA_DIFF_FUZZ_TIME ?= 9m
SCHEMA_DIFF_FUZZ_CONTROL_TIME ?= 30s
SCHEMA_DIFF_FUZZ_DRIVER ?= build/conformance-go wire-fuzz
SCHEMA_DIFF_FUZZ_LOG ?= build/schema-shared-differential-fuzz.log
SCHEMA_DIFF_FUZZ_REPLAY_LOG ?= build/schema-shared-differential-fuzz-replay.log

.PHONY: schema-shared-differential-fuzz-toolchain schema-shared-differential-fuzz schema-shared-differential-fuzz-replay \
	schema-shared-differential-fuzz-negative-control

# Match the module's declared Go toolchain major/minor; the native fuzz engine is part of Go.
schema-shared-differential-fuzz-toolchain:
	@actual=$$(go env GOVERSION | sed 's/^go//'); case "$$actual" in $(SCHEMA_DIFF_GO_VERSION).*) ;; *) echo "Go $(SCHEMA_DIFF_GO_VERSION).x required, got $$actual"; exit 1;; esac

schema-shared-differential-fuzz: schema-shared-differential-fuzz-toolchain build/conformance-harness build/conformance-go
	@set +e; SCHEMA_SLOW=1 GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_DIFF_FUZZ_DRIVER='$(SCHEMA_DIFF_FUZZ_DRIVER)' \
		go test -parallel=2 ./internal/ci -run '^$$' \
		-fuzz '^FuzzSchemaSharedDifferential$$' -fuzztime '$(SCHEMA_DIFF_FUZZ_TIME)' \
		-timeout 600s > '$(SCHEMA_DIFF_FUZZ_LOG)' 2>&1; status=$$?; set -e; \
	cat '$(SCHEMA_DIFF_FUZZ_LOG)'; exit $$status

schema-shared-differential-fuzz-replay: schema-shared-differential-fuzz-toolchain build/conformance-harness build/conformance-go
	@set +e; SCHEMA_SLOW=1 GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_DIFF_FUZZ_DRIVER='$(SCHEMA_DIFF_FUZZ_DRIVER)' \
		go test -parallel=2 ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' \
		-count=1 -timeout 600s > '$(SCHEMA_DIFF_FUZZ_REPLAY_LOG)' 2>&1; status=$$?; set -e; \
	cat '$(SCHEMA_DIFF_FUZZ_REPLAY_LOG)'; exit $$status

schema-shared-differential-fuzz-negative-control: schema-shared-differential-fuzz-toolchain build/conformance-harness build/conformance-go
	sh test/conformance/go/negative-control leb
	@unit=$$(sed -n 's/.*corpus seed .* (\([^ .]*\)\..*), pass.*/\1/p' build/go-table-control-leb/log | head -1); \
	root=$$(sed -n 's/.*corpus seed .* ([^.]*\.\([^)]*\)), pass.*/\1/p' build/go-table-control-leb/log | head -1); \
	if [ -z "$$unit" ] || [ -z "$$root" ]; then echo 'negative control did not record the planted witness root'; cat build/go-table-control-leb/log; exit 1; fi; \
	set +e; SCHEMA_SLOW=1 GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 \
		SCHEMA_DIFF_FUZZ_DRIVER='build/go-table-control-leb/driver-bin wire-fuzz' \
		SCHEMA_DIFF_FUZZ_EXTRA_WIRE=build/go-table-control-leb/failed.bin \
		SCHEMA_DIFF_FUZZ_EXTRA_UNIT=$$unit SCHEMA_DIFF_FUZZ_EXTRA_ROOT=$$root \
		SCHEMA_DIFF_FUZZ_EXTRA_FORM=wire \
		go test -parallel=2 ./internal/ci -run '^$$' \
		-fuzz '^FuzzSchemaSharedDifferential$$' -fuzztime '$(SCHEMA_DIFF_FUZZ_CONTROL_TIME)' \
		-timeout 600s > build/schema-shared-differential-fuzz-plant.log 2>&1; status=$$?; set -e; \
	if [ $$status -eq 0 ] || ! grep -q 'reader oracle mismatch' build/schema-shared-differential-fuzz-plant.log; then \
		cat build/schema-shared-differential-fuzz-plant.log; echo 'negative control failed: Go fuzz gate did not catch the planted reader bug'; exit 1; \
	fi; cat build/schema-shared-differential-fuzz-plant.log
	@set +e; SCHEMA_SLOW=1 GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 \
		SCHEMA_DIFF_FUZZ_DRIVER='build/go-table-control-leb/driver-bin wire-fuzz' \
		SCHEMA_DIFF_FUZZ_EXTRA_WIRE=build/go-table-control-leb/failed.bin \
		SCHEMA_DIFF_FUZZ_EXTRA_UNIT=$$(sed -n 's/.*corpus seed .* (\([^ .]*\)\..*), pass.*/\1/p' build/go-table-control-leb/log | head -1) \
		SCHEMA_DIFF_FUZZ_EXTRA_ROOT=$$(sed -n 's/.*corpus seed .* ([^.]*\.\([^)]*\)), pass.*/\1/p' build/go-table-control-leb/log | head -1) \
		SCHEMA_DIFF_FUZZ_EXTRA_FORM=wire \
		go test -parallel=2 ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' \
		-count=1 -timeout 600s > build/schema-shared-differential-fuzz-replay-plant.log 2>&1; status=$$?; set -e; \
	if [ $$status -eq 0 ] || ! grep -q 'reader oracle mismatch' build/schema-shared-differential-fuzz-replay-plant.log; then \
		cat build/schema-shared-differential-fuzz-replay-plant.log; echo 'negative control failed: saved planted input did not fail replay'; exit 1; \
	fi; cat build/schema-shared-differential-fuzz-replay-plant.log
	@set +e; SCHEMA_SLOW=1 GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_DIFF_FUZZ_DRIVER='$(SCHEMA_DIFF_FUZZ_DRIVER)' \
		go test -parallel=2 ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' \
		-count=1 -timeout 600s > build/schema-shared-differential-fuzz-fixed-replay.log 2>&1; status=$$?; set -e; \
	cat build/schema-shared-differential-fuzz-fixed-replay.log; exit $$status
