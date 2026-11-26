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

# Retired ci-fast checks whose command is not a single-name rule the extract
# records. Each recipe is that command, and a red exit stays the command's.

.PHONY: check-go-vet
check-go-vet:
	go vet ./...

.PHONY: check-golangci-lint-fast
check-golangci-lint-fast:
	bash -e -o pipefail -c 'if [ -f /opt/nova/sdk/env.sh ]; then . /opt/nova/sdk/env.sh; fi; export PATH="$$HOME/sdk/bin:$$PATH"; command -v golangci-lint >/dev/null 2>&1 || { echo "TOOL-MISSING: golangci-lint 2.12.2 (not on PATH; the fleet install is ~/sdk/bin/golangci-lint)"; exit 1; }; golangci-lint version 2>/dev/null | grep "2.12.2" >/dev/null || { echo "TOOL-MISSING: golangci-lint 2.12.2 (found: $$(golangci-lint version 2>&1 | head -1))"; exit 1; }; GOLANGCI_LINT_CACHE="$${RUNNER_TEMP:?}/golangci-lint-cache" golangci-lint run --allow-parallel-runners'

.PHONY: check-verify-cpp-conformance-1
check-verify-cpp-conformance-1:
	$(MAKE) verify-cpp-conformance-1

.PHONY: check-verify-cpp-conformance-2
check-verify-cpp-conformance-2:
	$(MAKE) verify-cpp-conformance-2

.PHONY: check-dart-conformance-produce
check-dart-conformance-produce:
	python3 make/conformance-handoff.py produce "$${RUNNER_TEMP:?}/dart-conformance.tar.gz"

.PHONY: check-conformance-corpus-dart
check-conformance-corpus-dart:
	$(MAKE) _conformance-corpus-dart DART=dart

.PHONY: check-verify-dart-fuzz
check-verify-dart-fuzz:
	$(MAKE) _verify-dart-fuzz DART=dart

.PHONY: check-cpp-conformance-produce
check-cpp-conformance-produce:
	python3 make/conformance-handoff.py produce "$${RUNNER_TEMP:?}/cpp-conformance.tar.gz" --leg cpp --prebuilt-compiler

# Retired ci-fast elixir checks, measured under nova-reserve. A red exit stays the command's.

.PHONY: check-verify-elixir-conformance
check-verify-elixir-conformance:
	if [ -f /opt/nova/sdk/env.sh ]; then . /opt/nova/sdk/env.sh; fi; \
	export PATH="$$HOME/sdk/bin:$$PATH"; \
	beam="$$(dirname "$$(command -v erl)"):$$(dirname "$$(command -v elixir)")"; \
	$(MAKE) verify-elixir-conformance BEAM_PATH="$$beam"

.PHONY: check-conformance-rows-elixir
check-conformance-rows-elixir:
	if [ -f /opt/nova/sdk/env.sh ]; then . /opt/nova/sdk/env.sh; fi; \
	export PATH="$$HOME/sdk/bin:$$PATH"; \
	beam="$$(dirname "$$(command -v erl)"):$$(dirname "$$(command -v elixir)")"; \
	$(MAKE) conformance-rows-elixir BEAM_PATH="$$beam"

.PHONY: check-inputaccount-elixir
check-inputaccount-elixir:
	bash -e -o pipefail -c 'if [ -f /opt/nova/sdk/env.sh ]; then . /opt/nova/sdk/env.sh; fi; export PATH="$$HOME/sdk/bin:$$PATH"; beam="$$(dirname "$$(command -v erl)"):$$(dirname "$$(command -v elixir)")"; mkdir -p build; go build -o "$${RUNNER_TEMP:?}/inputaccount" ./tools/inputaccount; vars=("BEAM_PATH=$$beam" VERIFY_JOBS=4); "$$RUNNER_TEMP/inputaccount" -family conformance-corpus -leg elixir -check-extra "$${vars[@]}" >build/inputaccount-elixir-extra.txt 2>&1 || { cat build/inputaccount-elixir-extra.txt; exit 1; }; "$$RUNNER_TEMP/inputaccount" -family conformance-corpus -leg elixir -canary wrong "$${vars[@]}" >build/inputaccount-elixir-wrong.txt 2>&1 || { cat build/inputaccount-elixir-wrong.txt; exit 1; }'
