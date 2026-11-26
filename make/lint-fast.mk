# The inline checks ci-fast.yml's lint-fast, ci-policy and
# input-family-cpp-conformance jobs ran, as make targets a registry row
# can run. Each recipe is that job's step, and it exits non-zero where
# the step did.

.PHONY: check-gofmt
check-gofmt:
	bash -e -o pipefail -c 'out=$$(gofmt -l .); if [ -n "$$out" ]; then printf "%s\n" "$$out"; echo "::error::gofmt differs — run gofmt -w on the files above"; exit 1; fi'

.PHONY: check-go-mod-tidy
check-go-mod-tidy:
	go mod tidy -diff

.PHONY: check-modernize
check-modernize:
	export PATH="$$HOME/sdk/bin:$$PATH"; \
	if ! bin=$$(command -v modernize) || ! readlink -f "$$bin" | grep 'modernize-0\.23\.0/' >/dev/null; then \
		echo "::error::tool missing: modernize 0.23.0 (the fleet install is ~/sdk/bin/modernize -> ~/sdk/modernize-0.23.0/modernize; found: $${bin:-nothing})"; \
		exit 1; \
	fi; \
	modernize ./...

.PHONY: check-wiredtargets
check-wiredtargets:
	go run ./tools/wiredtargets

.PHONY: check-bench-run-sh
check-bench-run-sh:
	bash -n bench/run.sh

.PHONY: check-inputaccount-cpp
check-inputaccount-cpp:
	bash -e -o pipefail -c 'archive="$${RUNNER_TEMP:?}/cpp-conformance/cpp-conformance.tar.gz"; rm -rf build/conformance-cpp build/schema_test_cook; python3 make/conformance-handoff.py consume "$$archive" --leg cpp; mkdir -p build; go build -o "$$RUNNER_TEMP/inputaccount" ./tools/inputaccount; "$$RUNNER_TEMP/inputaccount" -family conformance-corpus -leg cpp -check-extra "VERIFY_JFLAG=-j4 -o build/conformance-cpp -o build/schema_test_cook" >build/inputaccount-cpp-extra.txt 2>&1 || { cat build/inputaccount-cpp-extra.txt; exit 1; }; "$$RUNNER_TEMP/inputaccount" -family conformance-corpus -leg cpp -canary wrong "VERIFY_JFLAG=-j4 -o build/conformance-cpp -o build/schema_test_cook" >build/inputaccount-cpp-wrong.txt 2>&1 || { cat build/inputaccount-cpp-wrong.txt; exit 1; }'
