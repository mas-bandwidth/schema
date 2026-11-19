# make/json-fuzz.mk — Differential JSON & Text Fuzzer across all 18 ports
# (docs/SPEC-TABLES.md §16).
#
#   make conformance-json-fuzz-nightly              5,000 cases across all 18 ports
#   make conformance-json-fuzz-nightly JSON_FUZZ_ONLY=<lang>
#   make verify-json-fuzz-negative-control          1-character ToJson sabotage control

JSON_FUZZ_SEED ?= 1
JSON_FUZZ_N    ?= 5000
JSON_FUZZ_ONLY ?=

JSON_FUZZ_ALL_DEPS := \
	build/conformance-harness-vm \
	build/conformance-cpp build/conformance-c build-conformance-cs build/conformance-unity/.stamp \
	build/conformance-go build/conformance-rust build-conformance-java build/conformance-dart \
	build-conformance-elixir verify-toolchain-js build/tables-generated-js/.stamp \
	verify-toolchain-python build/conformance-python verify-toolchain-ruby build/conformance-ruby \
	verify-toolchain-lua build/conformance-lua verify-toolchain-luau build/conformance-luau \
	verify-toolchain-squirrel build/conformance-squirrel verify-toolchain-moo build/conformance-moo \
	verify-toolchain-goose build/conformance-goose verify-toolchain-lobster build/conformance-lobster

.PHONY: conformance-json-fuzz-nightly verify-json-fuzz-negative-control
conformance-json-fuzz-nightly: build/conformance-harness-vm
	@if [ -z "$(JSON_FUZZ_ONLY)" ]; then \
		$(MAKE) $(VERIFY_JFLAG) --no-print-directory $(JSON_FUZZ_ALL_DEPS); \
	fi
	NODE="$(NODE)" PYTHON="$(PYTHON)" RUBY="$(RUBY)" LUA="$(LUA)" LUAU="$(LUAU)" \
	SQ="$(SQ)" MOO="$(MOO)" MOO_ANALYZE="$(MOO_ANALYZE)" GOOSE="$(GOOSE)" LOBSTER="$(LOBSTER)" \
	./build/conformance-harness-vm json-fuzz $(if $(JSON_FUZZ_ONLY),--only $(JSON_FUZZ_ONLY),) \
		--seed $(JSON_FUZZ_SEED) --n $(JSON_FUZZ_N) \
		--failed build/json-fuzz-nightly-failures

# Proves that a 1-character divergence in generated ToJson formatting causes
# `conformance-harness json-fuzz` to fail immediately naming the exact diff.
verify-json-fuzz-negative-control: verify-toolchain-js build/conformance-harness-vm build/tables-generated-js/.stamp
	@rm -rf build/verify-json-fuzz-nc && mkdir -p build/verify-json-fuzz-nc
	@python3 -c 'import pathlib; src = pathlib.Path("internal/codegen/jstable/variableruntime.go").read_text(); target = "out.push(ind + fd.q + \": \");"; assert target in src, "missing ToJson colon anchor"; pathlib.Path("build/verify-json-fuzz-nc/variableruntime.go").write_text(src.replace(target, "out.push(ind + fd.q + \":  \");", 1))'
	@printf '{"Replace":{"%s/internal/codegen/jstable/variableruntime.go":"%s/build/verify-json-fuzz-nc/variableruntime.go"}}\n' "$(CURDIR)" "$(CURDIR)" > build/verify-json-fuzz-nc/overlay.json
	go build -overlay=build/verify-json-fuzz-nc/overlay.json -o build/verify-json-fuzz-nc/schema ./cmd/schema
	@cp -a build/tables-generated-js build/verify-json-fuzz-nc/conformance-js
	./build/verify-json-fuzz-nc/schema generate --lang js --out build/verify-json-fuzz-nc/conformance-js/features testdata/conformance/feature-witnesses/full/Features.schema
	@if SCHEMA_JS_GENERATED=build/verify-json-fuzz-nc/conformance-js NODE="$(NODE)" ./build/conformance-harness-vm json-fuzz --only js --seed 1 --n 300 > build/verify-json-fuzz-nc/fuzz.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: 1-char sabotaged JS ToJson emitter passed json-fuzz" >&2; exit 1; fi
	@grep -q 'FromJson -> ToJson differs from oracle' build/verify-json-fuzz-nc/fuzz.log || \
		{ echo "NEGATIVE CONTROL FAILED: unexpected json-fuzz failure log:" >&2; cat build/verify-json-fuzz-nc/fuzz.log >&2; exit 1; }
	@echo "verify-json-fuzz-negative-control: 1-character ToJson divergence caught immediately by json-fuzz"
