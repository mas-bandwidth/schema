# make/verify-compiled.mk — Fast (< 2m) per-language optimization verification
# gates for the 9 compiled / SDK targets:
#
#   cpp, c, cs, unity, go, rust, java, dart, elixir
#
# Each language exposes three independent sub-targets sized for ci-fast.yml's
# strict 2-minute job ceiling (cold-verified <= 85s), plus one umbrella target:
#
#   make verify-<lang>-conformance   all 21 table surfaces (--claims) + packet wire + 128-bit/UTF-8/defaults/finite
#   make verify-<lang>-bench         packet & table 64-variant bench gates + 9-surface cards conformance + UnitView/descriptors + packet write/end contracts
#   make verify-<lang>-fuzz          deterministic wire-fuzz smoke (plain + --retain, 6-step shrinking reuse + multi-body batches + all 64 roots)
#   make verify-<lang>               runs all three in a single parallel Make DAG

VERIFY_COMPILED_LEGS := cpp c cs unity go rust java dart elixir
VERIFY_JOBS          ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
VERIFY_JFLAG          = $(if $(findstring j,$(MAKEFLAGS)),,-j$(VERIFY_JOBS))
VERIFY_FUZZ_SEED     ?= 1
VERIFY_FUZZ_RANDOM_N ?= 2500
VERIFY_FUZZ_MAX      ?= 5000
VERIFY_JSON_FUZZ_N    ?= 360
VERIFY_MESSAGE_FUZZ_N ?= 1500
VERIFY_RESULTS_DIR   ?= build/verify/legs

# Uniform cards-<lang>-conformance alias for Go (make/go.mk names it cards-conformance-go).
.PHONY: cards-go-conformance
cards-go-conformance: cards-conformance-go

# Explicit per-leg wire-fuzz and json-fuzz build prerequisites (make/verify-compiled.mk
# sorts before make/wire-fuzz-release.mk in the Makefile's wildcard include, so target
# prerequisites must not rely on WFR_DEPS_* being defined at parse time).
VERIFY_FUZZ_DEPS_cpp    := build/wire-fuzz-cpp build/conformance-cpp
VERIFY_FUZZ_DEPS_c      := build/wire-fuzz-c build/conformance-c tables-c-view
VERIFY_FUZZ_DEPS_cs     := toolchain-cs build-conformance-cs tables-cs-view packet-end-cs packet-write-cfloat-cs packet-write-text-cs packet-write-refusal-cs
VERIFY_FUZZ_DEPS_unity  := toolchain-unity build/conformance-unity/.stamp
VERIFY_FUZZ_DEPS_go     := build/conformance-go
VERIFY_FUZZ_DEPS_rust   := build/conformance-rust-all build/conformance-rust
VERIFY_FUZZ_DEPS_java   := toolchain-java build-conformance-java
VERIFY_FUZZ_DEPS_dart   := toolchain-dart build/conformance-dart
VERIFY_FUZZ_DEPS_elixir := toolchain-elixir build/elixir-tables-ebin/.stamp build-conformance-elixir

VERIFY_FUZZ_EXTRA_go    := SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/golang

define verify_compiled_fuzz_rules
.PHONY: _verify-$(1)-fuzz verify-$(1)-fuzz
_verify-$(1)-fuzz: build/conformance-harness $$(VERIFY_FUZZ_DEPS_$(1))
	@mkdir -p build/verify
	$$(if $$(VERIFY_FUZZ_EXTRA_$(1)),$$(VERIFY_FUZZ_EXTRA_$(1)),@:)
	$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz \
		--driver "$$(WFR_DRIVER_$(1))" $$(WFR_FLAGS_$(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_FUZZ_RANDOM_N) --max-mutants $$(VERIFY_FUZZ_MAX) \
		--failed build/verify/fuzz-$(1)-plain.bin
	$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz --retain \
		--driver "$$(WFR_DRIVER_$(1))" $$(WFR_FLAGS_$(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_FUZZ_RANDOM_N) --max-mutants $$(VERIFY_FUZZ_MAX) \
		--failed build/verify/fuzz-$(1)-retain.bin
	./build/conformance-harness json-fuzz --only $(1) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-$(1)
	DOTNET="$$(DOTNET)" JAVA="$$(JAVA)" BEAM_PATH="$$(BEAM_PATH)" $$(WFR_ENV_$(1)) \
		./build/conformance-harness message-fuzz --only $(1) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_MESSAGE_FUZZ_N) \
		--failed build/verify/message-fuzz-$(1)

verify-$(1)-fuzz:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-fuzz

.PHONY: _verify-$(1)-all verify-$(1)
_verify-$(1)-all: _verify-$(1)-conformance _verify-$(1)-bench _verify-$(1)-fuzz

verify-$(1):
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-all

.PHONY: verify-$(1)-conformance verify-$(1)-bench
verify-$(1)-conformance:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-conformance

verify-$(1)-bench:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-bench
endef

$(foreach leg,$(VERIFY_COMPILED_LEGS),$(eval $(call verify_compiled_fuzz_rules,$(leg))))

# ---- cpp ----

.PHONY: _verify-cpp-conformance _verify-cpp-bench
_verify-cpp-conformance: build/conformance-harness build/conformance-cpp build/schema_test_cook build/schema_test build/schema_test_ludicrous packet-defaults-cpp packet-void-cpp table-base64-cpp build/packet-text/cpp/driver build/packet-text/harness
	./build/schema_test
	./build/schema_test_ludicrous
	./build/packet-text/harness ./build/packet-text/cpp/driver
	@rm -rf build/verify/conformance-cpp && mkdir -p build/verify/conformance-cpp $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only cpp --claims --work build/verify/conformance-cpp --results $(VERIFY_RESULTS_DIR)/cpp.json

_verify-cpp-bench: bin/schema generated/bench/cpp/.stamp generated/bench/tables/cpp/.stamp tables-cpp-bench-gate cards-cpp-conformance tables-view packet-end-cpp packet-write-cfloat-cpp packet-write-text-cpp packet-write-refusal-cpp
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-cpp.csv
	bench/run.sh --quick --bare --only cpp --out build/verify/bench-cpp.csv
	@test -s build/verify/bench-cpp.csv

# ---- c ----

.PHONY: _verify-c-conformance _verify-c-bench
_verify-c-conformance: build/conformance-harness build/conformance-c build/schema_test_c build/schema_test_c_ludicrous packet-defaults-c packet-utf8-c packet-void-c table-base64-c
	cd test/c && ../../build/schema_test_c
	cd test/c-ludicrous && ../../build/schema_test_c_ludicrous
	@rm -rf build/verify/conformance-c && mkdir -p build/verify/conformance-c $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only c --claims --work build/verify/conformance-c --results $(VERIFY_RESULTS_DIR)/c.json

_verify-c-bench: bin/schema generated/bench/c/.stamp generated/bench/tables/c/.stamp tables-c-bench-gate cards-c-conformance packet-end-c packet-write-cfloat-c packet-write-text-c packet-write-refusal-c
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-c.csv
	bench/run.sh --quick --bare --only c --out build/verify/bench-c.csv
	@test -s build/verify/bench-c.csv

# ---- cs ----

.PHONY: _verify-cs-conformance _verify-cs-bench
_verify-cs-conformance: toolchain-cs build/conformance-harness build-conformance-cs build-cs-cook generated/cs/.stamp generated/cs-ludicrous/.stamp
	@rm -rf build/verify/conformance-cs && mkdir -p build/verify/conformance-cs $(VERIFY_RESULTS_DIR)
	@set -e; \
		(cd test/cs && $(DOTNET) run -c Release --nologo) & p1=$$!; \
		(cd test/cs-ludicrous && $(DOTNET) run --nologo) & p2=$$!; \
		./build/conformance-harness run --only cs --claims --work build/verify/conformance-cs --results $(VERIFY_RESULTS_DIR)/cs.json & p3=$$!; \
		wait $$p1; wait $$p2; wait $$p3

_verify-cs-bench: toolchain-cs bin/schema generated/bench/cs/.stamp tables-cs-bench-gate cards-cs-conformance table-base64-cs packet-defaults-cs packet-utf8-cs
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-cs.csv
	DOTNET="$(DOTNET)" bench/run.sh --quick --bare --only cs --out build/verify/bench-cs.csv
	@test -s build/verify/bench-cs.csv

# ---- unity ----

.PHONY: _verify-unity-conformance _verify-unity-bench
_verify-unity-conformance: toolchain-unity build/conformance-harness build/conformance-unity/.stamp unity-wire
	@rm -rf build/verify/conformance-unity && mkdir -p build/verify/conformance-unity $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only unity --claims --work build/verify/conformance-unity --results $(VERIFY_RESULTS_DIR)/unity.json

_verify-unity-bench: toolchain-unity bin/schema tables-unity-bench-gate cards-unity-conformance packet-defaults-unity packet-utf8-unity packet-write-refusal-unity
	SCHEMA_SLOW=1 go test -count=1 -run '^TestUnityVTReflectionDocTagsAndUnitView$$' ./internal/codegen/unitytable

# ---- go ----

.PHONY: _verify-go-conformance _verify-go-bench
_verify-go-conformance: build/conformance-harness build/conformance-go generated/go/.stamp generated/go-ludicrous/.stamp cards-go-conformance tables-go-view tables-go-textform packet-defaults-go packet-utf8-go packet-finite-go table-base64-go
	cd test/go && go run .
	cd test/go-ludicrous && go run .
	@rm -rf build/verify/conformance-go && mkdir -p build/verify/conformance-go $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only go --claims --work build/verify/conformance-go --results $(VERIFY_RESULTS_DIR)/go.json

# The timed quick run is a prerequisite of its own, so under the umbrella's -j
# it runs beside the gates instead of after them, and it takes one warmup and
# one measured run (--round 0) instead of one and three: in CI the figure is an
# iteration instrument read by nobody, the golden and variant gates inside the
# runner are what can go red, and they run in full either way. The whole of
# verify-leg (go, bench) was cancelled at the two-minute cap on a loaded runner
# with the quick run alone at 84 s (run 44911874471); on a loaded amd-3 slot
# the quick run is 38.7 s as before and 19.3 s with --round 0.
.PHONY: _verify-go-bench-quick
_verify-go-bench-quick: generated/bench/go/.stamp
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-go.csv
	bench/run.sh --quick --round 0 --bare --only go --out build/verify/bench-go.csv

_verify-go-bench: bin/schema generated/bench/go/.stamp generated/bench/tables/go/.stamp tables-go-bench-gate _verify-go-bench-quick
	@test -s build/verify/bench-go.csv

# ---- rust ----

.PHONY: _verify-rust-conformance _verify-rust-bench
_verify-rust-conformance: build/conformance-harness build/conformance-rust generated/rust/.stamp generated/rust-ludicrous/.stamp packet-utf8-rust
	@rm -rf build/verify/conformance-rust && mkdir -p build/verify/conformance-rust $(VERIFY_RESULTS_DIR)
	@set -e; \
		(cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release) & p1=$$!; \
		(cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release) & p2=$$!; \
		./build/conformance-harness run --only rust --claims --work build/verify/conformance-rust --results $(VERIFY_RESULTS_DIR)/rust.json & p3=$$!; \
		wait $$p1; wait $$p2; wait $$p3

_verify-rust-bench: bin/schema generated/bench/rust/.stamp generated/bench/paired/rust/.stamp packet-defaults-rust packet-end-rust packet-write-cfloat-rust packet-write-text-rust packet-write-refusal-rust cards-rust-conformance tables-rust-view
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-rust.csv
	PATH="$(RUSTUP_BIN):$$PATH" bench/run.sh --quick --bare --only rust --out build/verify/bench-rust.csv
	@test -s build/verify/bench-rust.csv
	PATH="$(RUSTUP_BIN):$$PATH" go run ./bench/paired -mode gate -langs rust

# ---- java ----

.PHONY: _verify-java-conformance _verify-java-bench
_verify-java-conformance: toolchain-java build/conformance-harness build-conformance-java build/java-test/.stamp build/java-test-ludicrous/.stamp packet-defaults-java packet-utf8-java packet-finite-java
	cd test/java && $(JAVA) -ea -cp ../../build/java-test Main
	cd test/java && $(JAVA) -cp ../../build/java-test Main
	cd test/java-ludicrous && $(JAVA) -ea -cp ../../build/java-test-ludicrous Main
	@rm -rf build/verify/conformance-java && mkdir -p build/verify/conformance-java $(VERIFY_RESULTS_DIR)
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" ./build/conformance-harness run --only java --claims --work build/verify/conformance-java --results $(VERIFY_RESULTS_DIR)/java.json

_verify-java-bench: toolchain-java bin/schema generated/bench/java/.stamp cards-java-conformance tables-java-view packet-end-java packet-write-cfloat-java packet-write-text-java
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-java.csv
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" bench/run.sh --quick --bare --only java --out build/verify/bench-java.csv
	@test -s build/verify/bench-java.csv
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" go run ./bench/paired -mode gate -langs java

# ---- dart ----

.PHONY: _verify-dart-conformance _verify-dart-bench
_verify-dart-conformance: toolchain-dart build/conformance-harness build/conformance-dart generated/dart/.stamp generated/dart-ludicrous/.stamp packet-defaults-dart packet-utf8-dart packet-finite-dart
	cd test/dart && $(DART) --enable-asserts main.dart
	cd test/dart-ludicrous && $(DART) --enable-asserts main.dart
	@rm -rf build/verify/conformance-dart && mkdir -p build/verify/conformance-dart $(VERIFY_RESULTS_DIR)
	DART="$(DART)" ./build/conformance-harness run --only dart --claims --work build/verify/conformance-dart --results $(VERIFY_RESULTS_DIR)/dart.json

_verify-dart-bench: toolchain-dart bin/schema generated/bench/dart/.stamp generated/bench/tables/dart/.stamp tables-dart-bench-gate cards-dart-conformance tables-dart-view packet-end-dart packet-write-cfloat-dart packet-write-text-dart packet-write-bareint-dart
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-dart.csv
	DART="$(DART)" bench/run.sh --quick --bare --only dart --out build/verify/bench-dart.csv
	@test -s build/verify/bench-dart.csv

# ---- elixir ----

.PHONY: _verify-elixir-conformance _verify-elixir-bench
_verify-elixir-conformance: toolchain-elixir build/conformance-harness build-conformance-elixir generated/elixir/.stamp generated/elixir-ludicrous/.stamp packet-defaults-elixir packet-utf8-elixir packet-finite-elixir
	cd test/elixir && $(ELIXIR) main.exs
	cd test/elixir-ludicrous && $(ELIXIR) main.exs
	@rm -rf build/verify/conformance-elixir && mkdir -p build/verify/conformance-elixir $(VERIFY_RESULTS_DIR)
	BEAM_PATH="$(BEAM_PATH)" ELIXIR_TABLES_EBIN="$(CURDIR)/build/elixir-tables-ebin" ./build/conformance-harness run --only elixir --claims --work build/verify/conformance-elixir --results $(VERIFY_RESULTS_DIR)/elixir.json

_verify-elixir-bench: toolchain-elixir bin/schema generated/bench/elixir/.stamp generated/bench/tables/elixir/.stamp tables-elixir-bench-gate cards-elixir-conformance tables-elixir-view packet-end-elixir packet-write-cfloat-elixir packet-write-text-elixir packet-write-bareint-elixir
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-elixir.csv
	@beam_extra="$$(dirname "$$(PATH="$(BEAM_PATH):$$PATH" command -v elixir 2>/dev/null || echo /nonexistent)"):$$(dirname "$$(PATH="$(BEAM_PATH):$$PATH" command -v erl 2>/dev/null || echo /nonexistent)")"; \
		BEAM_PATH="$(BEAM_PATH):$$beam_extra" bench/run.sh --quick --bare --only elixir --out build/verify/bench-elixir.csv
	@test -s build/verify/bench-elixir.csv
