# make/verify-compiled.mk — Fast (< 2m) per-language optimization verification
# gates for the 9 compiled / SDK targets:
#
#   cpp, c, cs, unity, go, rust, java, dart, elixir
#
# Each language exposes four independent sub-targets sized for ci-fast.yml's
# strict 2-minute job ceiling (cold-verified <= 85s), plus one umbrella target:
#
#   make verify-<lang>-conformance   all 21 table surfaces (--claims) + packet wire + 128-bit/UTF-8/defaults/finite
#   make verify-<lang>-bench         packet & table 64-variant bench gates + 9-surface cards conformance + UnitView/descriptors + packet write/end contracts
#   make verify-<lang>-fuzz          deterministic wire-fuzz smoke (plain + --retain, 6-step shrinking reuse + multi-body batches + all 64 roots)
#   make verify-<lang>-tablegen      generated-schema table round trips (make/verify-widen-18.mk)
#   make verify-<lang>               runs all four in a single parallel Make DAG

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
# Build both C++ fuzz binaries once from a single parallel compile pass:
# build/conformance-cpp and build/wire-fuzz-cpp in Makefile each invoke
# $(MAKE) $(CONFORMANCE_OBJECTS), so listing both as parallel prerequisites of
# _verify-cpp-fuzz compiled all 62 generated table objects twice simultaneously
# (racing on identical .o outputs) and delayed the two slowest main TUs until
# after the object pass finished. Compiling both main TUs first alongside one
# shared $(CONFORMANCE_OBJECTS) pass eliminates the duplicate work and file race
# and overlaps the main TUs with the generated objects.
build/conformance-cpp-objs/_main/conformance_main.o: test/conformance/cpp/main.cpp build/tables-generated/.stamp $(wildcard $(SERIALIZE)/*.h)
	@mkdir -p $(@D)
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) -c $< -o $@

.PHONY: _verify-cpp-fuzz-bins
_verify-cpp-fuzz-bins: build/tables-generated/.stamp test/conformance/cpp/main.cpp test/tables/wire_fuzz_main.cpp
	@mkdir -p build
	@$(MAKE) --no-print-directory \
		build/conformance-cpp-objs/_main/conformance_main.o \
		build/conformance-cpp-objs/_main/wire_fuzz_main.o \
		$(CONFORMANCE_OBJECTS)
	@set -e; \
		$(CXX) $(TABLES_CXXFLAGS) build/conformance-cpp-objs/_main/conformance_main.o \
			$(CONFORMANCE_OBJECTS) -o build/conformance-cpp & p1=$$!; \
		$(CXX) $(TABLES_CXXFLAGS) build/conformance-cpp-objs/_main/wire_fuzz_main.o \
			$(CONFORMANCE_OBJECTS) -o build/wire-fuzz-cpp & p2=$$!; \
		wait $$p1; wait $$p2

VERIFY_FUZZ_DEPS_cpp    := _verify-cpp-fuzz-bins
VERIFY_FUZZ_DEPS_c      := build/wire-fuzz-c build/conformance-c tables-c-view
VERIFY_FUZZ_DEPS_cs     := toolchain-cs build-conformance-cs tables-cs-view packet-end-cs packet-write-cfloat-cs packet-write-text-cs
VERIFY_FUZZ_DEPS_unity  := toolchain-unity build/conformance-unity/.stamp
VERIFY_FUZZ_DEPS_go     := build/conformance-go
VERIFY_FUZZ_DEPS_rust   := build/conformance-rust-all
VERIFY_FUZZ_DEPS_java   := toolchain-java build-conformance-java
VERIFY_FUZZ_DEPS_dart   := toolchain-dart build/conformance-dart
VERIFY_FUZZ_DEPS_elixir := toolchain-elixir build/elixir-tables-ebin/.stamp build-conformance-elixir

VERIFY_FUZZ_EXTRA_go    := SCHEMA_SLOW=1 go test -count=1 ./internal/codegen/golang

define verify_compiled_fuzz_rules
.PHONY: _verify-$(1)-fuzz _verify-$(1)-fuzz-extra _verify-$(1)-fuzz-plain _verify-$(1)-fuzz-retain _verify-$(1)-fuzz-json _verify-$(1)-fuzz-message verify-$(1)-fuzz
_verify-$(1)-fuzz-extra: build/conformance-harness $$(VERIFY_FUZZ_DEPS_$(1))
	$$(if $$(VERIFY_FUZZ_EXTRA_$(1)),$$(VERIFY_FUZZ_EXTRA_$(1)),@:)

_verify-$(1)-fuzz-plain: build/conformance-harness $$(VERIFY_FUZZ_DEPS_$(1))
	@mkdir -p build/verify
	$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz \
		--driver "$$(WFR_DRIVER_$(1))" $$(WFR_FLAGS_$(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_FUZZ_RANDOM_N) --max-mutants $$(VERIFY_FUZZ_MAX) \
		--failed build/verify/fuzz-$(1)-plain.bin

_verify-$(1)-fuzz-retain: build/conformance-harness $$(VERIFY_FUZZ_DEPS_$(1))
	@mkdir -p build/verify
	$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz --retain \
		--driver "$$(WFR_DRIVER_$(1))" $$(WFR_FLAGS_$(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_FUZZ_RANDOM_N) --max-mutants $$(VERIFY_FUZZ_MAX) \
		--failed build/verify/fuzz-$(1)-retain.bin

_verify-$(1)-fuzz-json: build/conformance-harness $$(VERIFY_FUZZ_DEPS_$(1))
	@mkdir -p build/verify
	./build/conformance-harness json-fuzz --only $(1) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_JSON_FUZZ_N) \
		--failed build/verify/json-fuzz-$(1)

_verify-$(1)-fuzz-message: build/conformance-harness $$(VERIFY_FUZZ_DEPS_$(1))
	@mkdir -p build/verify
	DOTNET="$$(DOTNET)" JAVA="$$(JAVA)" BEAM_PATH="$$(BEAM_PATH)" $$(WFR_ENV_$(1)) \
		./build/conformance-harness message-fuzz --only $(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_MESSAGE_FUZZ_N) \
		--failed build/verify/message-fuzz-$(1)

_verify-$(1)-fuzz: _verify-$(1)-fuzz-extra _verify-$(1)-fuzz-plain _verify-$(1)-fuzz-retain _verify-$(1)-fuzz-json _verify-$(1)-fuzz-message

verify-$(1)-fuzz:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-fuzz

.PHONY: _verify-$(1)-all verify-$(1)
_verify-$(1)-all: _verify-$(1)-conformance _verify-$(1)-bench _verify-$(1)-fuzz

verify-$(1):
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-all

.PHONY: verify-$(1)-conformance verify-$(1)-bench conformance-corpus-$(1)
verify-$(1)-conformance:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-conformance

verify-$(1)-bench:
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _verify-$(1)-bench

conformance-corpus-$(1):
	@$$(MAKE) $$(VERIFY_JFLAG) --no-print-directory _conformance-corpus-$(1)
endef

$(foreach leg,$(VERIFY_COMPILED_LEGS),$(eval $(call verify_compiled_fuzz_rules,$(leg))))

# ---- cpp ----

# THE C++ CONFORMANCE GATE IS TWO PARTS (2026-11-24). As one ci-fast.yml row
# it ran 108-131 s on fleet runners, unloaded ones too, and it is build-bound:
# run 45428504681 spent 16 s generating, 73 s on the single g++ call that
# builds build/schema_test_tables (main.cpp and every pinned table's .cpp in
# one command), and the conformance driver's 60-object pass and link beside
# it; the checks themselves took 8 s. Each part builds only what its own
# checks run, so the two halves of the build run on two runners:
#
#   part 1: the standalone C++ test binaries (schema_test, schema_test_tables,
#           schema_test_ludicrous, packet text, and the packet-defaults
#           and packet-void checks their targets run; the C++ conformance
#           rows run in ci-fast.yml's conformance-rows job);
#   part 2: the harness's --claims run and what test/conformance/cpp/ci.json
#           says it needs (the driver, schema_test_cook, table-base64-cpp).
#
# Nothing is dropped: _verify-cpp-conformance is both parts, and the split is
# by target, so it is the same every run. ci-fast.yml's verify-cpp-conformance
# job runs one part per row (internal/ci TestCppConformancePartsCoverTheGate).
.PHONY: _verify-cpp-conformance _verify-cpp-conformance-1 _verify-cpp-conformance-2 verify-cpp-conformance-1 verify-cpp-conformance-2 _verify-cpp-bench
_verify-cpp-conformance: _verify-cpp-conformance-1 _verify-cpp-conformance-2

_verify-cpp-conformance-1: build/schema_test build/schema_test_ludicrous packet-defaults-cpp packet-void-cpp build/packet-text/cpp/driver build/packet-text/harness build/schema_test_tables
	./build/schema_test
	./build/schema_test_tables
	./build/schema_test_ludicrous
	./build/packet-text/harness ./build/packet-text/cpp/driver

_verify-cpp-conformance-2: build/conformance-harness build/conformance-cpp build/schema_test_cook table-base64-cpp
	@rm -rf build/verify/conformance-cpp && mkdir -p build/verify/conformance-cpp $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only cpp --claims --work build/verify/conformance-cpp --results $(VERIFY_RESULTS_DIR)/cpp.json

verify-cpp-conformance-1 verify-cpp-conformance-2: verify-cpp-conformance-%:
	@$(MAKE) $(VERIFY_JFLAG) --no-print-directory _verify-cpp-conformance-$*

_verify-cpp-bench: bin/schema generated/bench/cpp/.stamp generated/bench/tables/cpp/.stamp tables-cpp-bench-gate cards-cpp-conformance tables-view packet-end-cpp packet-write-cfloat-cpp packet-write-text-cpp
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

_verify-c-bench: bin/schema generated/bench/c/.stamp generated/bench/tables/c/.stamp tables-c-bench-gate cards-c-conformance packet-end-c packet-write-cfloat-c packet-write-text-c
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

_verify-unity-bench: toolchain-unity bin/schema tables-unity-bench-gate cards-unity-conformance packet-defaults-unity packet-utf8-unity
	SCHEMA_SLOW=1 go test -count=1 -run '^TestUnityVTReflectionDocTagsAndUnitView$$' ./internal/codegen/unitytable

# ---- go ----

.PHONY: _verify-go-conformance _verify-go-bench
_verify-go-conformance: build/mustrun build/conformance-harness build/conformance-go generated/go/.stamp generated/go-ludicrous/.stamp cards-go-conformance tables-go-view tables-go-textform packet-defaults-go packet-utf8-go packet-finite-go table-base64-go
	cd test/go && "$(CURDIR)/$(MUSTRUN)" -name test-go-main -count $(GO_MAIN_CHECKS) -- go run .
	cd test/go-ludicrous && "$(CURDIR)/$(MUSTRUN)" -name test-go-ludicrous -count $(GO_LUDICROUS_CHECKS) -- go run .
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

# THE RUST FUZZ GATE IN TWO HALVES (2026-11-24). As the ci-fast.yml row
# (rust, fuzz) the gate ran 107-121 s and was cut at the two-minute cap on
# main's push run 45431445496: 17 s building build/conformance-rust-all, 79 s
# in `cargo build` of test/conformance/rust (build/conformance-rust, the
# conformance driver), and 4 s fuzzing. Only the JSON and message passes read
# that driver; the two wire passes read conformance-rust-all. The (rust,
# conformance) row builds build/conformance-rust anyway, so it runs the JSON
# and message passes (verify-rust-fuzz-driver) on that build, and the (rust,
# fuzz) row runs the extra step and both wire passes (verify-rust-fuzz-wire).
# The two halves are _verify-rust-fuzz, nothing dropped: same seed, same
# counts. internal/ci TestRustFuzzHalvesAreTheWholeGate holds the union.
_verify-rust-fuzz-json _verify-rust-fuzz-message: build/conformance-rust

.PHONY: verify-rust-fuzz-wire verify-rust-fuzz-driver
verify-rust-fuzz-wire:
	@$(MAKE) $(VERIFY_JFLAG) --no-print-directory _verify-rust-fuzz-extra _verify-rust-fuzz-plain _verify-rust-fuzz-retain

verify-rust-fuzz-driver:
	@$(MAKE) $(VERIFY_JFLAG) --no-print-directory _verify-rust-fuzz-json _verify-rust-fuzz-message

# the (rust, conformance) row: the conformance gate, the driver's fuzz half
# and the harness test that answers on the driver, in one make, so
# build/conformance-rust is built once
.PHONY: verify-rust-conformance-and-driver-fuzz
verify-rust-conformance-and-driver-fuzz:
	@$(MAKE) $(VERIFY_JFLAG) --no-print-directory _verify-rust-conformance _verify-rust-fuzz-json _verify-rust-fuzz-message _verify-rust-driver-write-refusal

# THE RUST WRITE-REFUSAL TEST RIDES THIS ROW (2026-11-24). As a go-test-touched
# row of its own, TestRustDriverAnswersWriteRefusal built the Rust driver cold:
# 105 s of a two-minute job (PR #5007, run 45437384482, row 18 of 23). Here the
# driver is already built, so the test's own `make build/conformance-rust` is
# up to date and the test is its four answers. .github/go-test-rows.txt names
# it in a `rides` line: no go-test-touched row runs it, and the plan selects
# this row whenever ./test/conformance/harness is selected (gotestrows
# -rides-out; TestEveryRideRunsInItsLegsRow holds this recipe to that line).
.PHONY: _verify-rust-driver-write-refusal
_verify-rust-driver-write-refusal: build/conformance-rust
	PATH="$(RUSTUP_BIN):$$PATH" go test -count=1 -run '^TestRustDriverAnswersWriteRefusal$$' ./test/conformance/harness

.PHONY: _verify-rust-conformance _verify-rust-bench
_verify-rust-conformance: build/conformance-harness build/conformance-rust generated/rust/.stamp generated/rust-ludicrous/.stamp packet-utf8-rust
	@rm -rf build/verify/conformance-rust && mkdir -p build/verify/conformance-rust $(VERIFY_RESULTS_DIR)
	@set -e; \
		(cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release) & p1=$$!; \
		(cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release) & p2=$$!; \
		./build/conformance-harness run --only rust --claims --work build/verify/conformance-rust --results $(VERIFY_RESULTS_DIR)/rust.json & p3=$$!; \
		wait $$p1; wait $$p2; wait $$p3

# The timed quick run and the paired gate are prerequisites of their own, as
# Go's are (_verify-go-bench-quick above): under the umbrella's -j they run
# beside the view and cards gates instead of after them, and the quick run
# takes one warmup and one measured run (--round 0). They write apart
# (build/bench, build/paired), so nothing they share is raced.
.PHONY: _verify-rust-bench-quick _verify-rust-bench-paired
_verify-rust-bench-quick: generated/bench/rust/.stamp
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-rust.csv
	PATH="$(RUSTUP_BIN):$$PATH" bench/run.sh --quick --round 0 --bare --only rust --out build/verify/bench-rust.csv
	@test -s build/verify/bench-rust.csv

_verify-rust-bench-paired: bin/schema generated/bench/paired/rust/.stamp
	PATH="$(RUSTUP_BIN):$$PATH" go run ./bench/paired -mode gate -langs rust

_verify-rust-bench: bin/schema generated/bench/rust/.stamp generated/bench/paired/rust/.stamp packet-defaults-rust packet-end-rust packet-write-cfloat-rust packet-write-text-rust packet-write-refusal-rust cards-rust-conformance tables-rust-view _verify-rust-bench-quick _verify-rust-bench-paired
	@test -s build/verify/bench-rust.csv

# ---- java ----

.PHONY: _verify-java-conformance _verify-java-packet-main-ea _verify-java-packet-main _verify-java-packet-ludicrous _verify-java-bench
_verify-java-packet-main-ea: toolchain-java build/java-test/.stamp
	cd test/java && $(JAVA) -ea -cp ../../build/java-test Main

_verify-java-packet-main: toolchain-java build/java-test/.stamp
	cd test/java && $(JAVA) -cp ../../build/java-test Main

_verify-java-packet-ludicrous: toolchain-java build/java-test-ludicrous/.stamp
	cd test/java-ludicrous && $(JAVA) -ea -cp ../../build/java-test-ludicrous Main

_verify-java-conformance: toolchain-java build/conformance-harness build-conformance-java _verify-java-packet-main-ea _verify-java-packet-main _verify-java-packet-ludicrous packet-defaults-java packet-utf8-java packet-finite-java
	@rm -rf build/verify/conformance-java && mkdir -p build/verify/conformance-java $(VERIFY_RESULTS_DIR)
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" ./build/conformance-harness run --only java --claims --work build/verify/conformance-java --results $(VERIFY_RESULTS_DIR)/java.json

# As Rust's: the quick run (--round 0) and the paired gate run beside the view
# and cards gates. On a vision slot the whole leg was cancelled at the cap in
# the paired gate (run 45028849060); on space tables-java-view alone is 45 s,
# the quick run 23 s and the paired gate 12 s, which ran one after another.
.PHONY: _verify-java-bench-quick _verify-java-bench-paired
_verify-java-bench-quick: toolchain-java generated/bench/java/.stamp
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-java.csv
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" bench/run.sh --quick --round 0 --bare --only java --out build/verify/bench-java.csv

_verify-java-bench-paired: toolchain-java bin/schema
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" go run ./bench/paired -mode gate -langs java

_verify-java-bench: toolchain-java bin/schema generated/bench/java/.stamp cards-java-conformance tables-java-view packet-end-java packet-write-cfloat-java packet-write-text-java _verify-java-bench-quick _verify-java-bench-paired
	@test -s build/verify/bench-java.csv

# ---- dart ----

.PHONY: _verify-dart-conformance _verify-dart-packets _verify-dart-bench
_verify-dart-packets: build/mustrun toolchain-dart generated/dart/.stamp generated/dart-ludicrous/.stamp packet-defaults-dart packet-utf8-dart packet-finite-dart
	cd test/dart && "$(CURDIR)/$(MUSTRUN)" -name test-dart-main -count $(DART_MAIN_CHECKS) -- $(DART) --enable-asserts main.dart
	cd test/dart-ludicrous && "$(CURDIR)/$(MUSTRUN)" -name test-dart-ludicrous -count $(DART_LUDICROUS_CHECKS) -- $(DART) --enable-asserts main.dart

_verify-dart-conformance: build/conformance-harness build/conformance-dart _verify-dart-packets
	@rm -rf build/verify/conformance-dart && mkdir -p build/verify/conformance-dart $(VERIFY_RESULTS_DIR)
	DART="$(DART)" ./build/conformance-harness run --only dart --claims --work build/verify/conformance-dart --results $(VERIFY_RESULTS_DIR)/dart.json

_verify-dart-bench: toolchain-dart bin/schema generated/bench/dart/.stamp generated/bench/tables/dart/.stamp tables-dart-bench-gate cards-dart-conformance tables-dart-view packet-end-dart packet-write-cfloat-dart packet-write-text-dart packet-write-bareint-dart
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-dart.csv
	DART="$(DART)" bench/run.sh --quick --bare --only dart --out build/verify/bench-dart.csv
	@test -s build/verify/bench-dart.csv

# ---- elixir ----

.PHONY: _verify-elixir-conformance _verify-elixir-bench _verify-elixir-packet-main _verify-elixir-packet-ludicrous
_verify-elixir-packet-main: build/mustrun toolchain-elixir generated/elixir/.stamp
	cd test/elixir && "$(CURDIR)/$(MUSTRUN)" -name test-elixir-main -count $(ELIXIR_MAIN_CHECKS) -- $(ELIXIR) main.exs

_verify-elixir-packet-ludicrous: build/mustrun toolchain-elixir generated/elixir-ludicrous/.stamp
	cd test/elixir-ludicrous && "$(CURDIR)/$(MUSTRUN)" -name test-elixir-ludicrous -count $(ELIXIR_LUDICROUS_CHECKS) -- $(ELIXIR) main.exs


VERIFY_ELIXIR_BUILD = $(if $(ELIXIR_AGREEMENT_ARTIFACTS),build-conformance-elixir-from-shards,build-conformance-elixir)
VERIFY_ELIXIR_EBIN = $(if $(ELIXIR_AGREEMENT_ARTIFACTS),$(ELIXIR_HANDOFF_EBIN),build/elixir-tables-ebin)

_verify-elixir-conformance: build/conformance-harness $(VERIFY_ELIXIR_BUILD) _verify-elixir-packet-main _verify-elixir-packet-ludicrous packet-defaults-elixir packet-utf8-elixir packet-finite-elixir
	@rm -rf build/verify/conformance-elixir && mkdir -p build/verify/conformance-elixir $(VERIFY_RESULTS_DIR)
	BEAM_PATH="$(BEAM_PATH)" ELIXIR_TABLES_EBIN="$(CURDIR)/$(VERIFY_ELIXIR_EBIN)" ./build/conformance-harness run --only elixir --claims --work build/verify/conformance-elixir --results $(VERIFY_RESULTS_DIR)/elixir.json

_verify-elixir-bench: toolchain-elixir bin/schema generated/bench/elixir/.stamp generated/bench/tables/elixir/.stamp tables-elixir-bench-gate cards-elixir-conformance tables-elixir-view packet-end-elixir packet-write-cfloat-elixir packet-write-text-elixir packet-write-bareint-elixir
	@mkdir -p build/verify build/bench && rm -f build/verify/bench-elixir.csv
# loud-ok: PATH additions for the bench; a missing elixir or erl fails toolchain-elixir (a prerequisite) and the bench run by name
	@beam_extra="$$(dirname "$$(PATH="$(BEAM_PATH):$$PATH" command -v elixir 2>/dev/null || echo /nonexistent)"):$$(dirname "$$(PATH="$(BEAM_PATH):$$PATH" command -v erl 2>/dev/null || echo /nonexistent)")"; \
		BEAM_PATH="$(BEAM_PATH):$$beam_extra" bench/run.sh --quick --bare --only elixir --out build/verify/bench-elixir.csv
	@test -s build/verify/bench-elixir.csv

# ---- conformance-corpus (tables conformance harness only, for input-family accounting) ----

.PHONY: _conformance-corpus-cpp _conformance-corpus-c _conformance-corpus-cs \
	_conformance-corpus-unity _conformance-corpus-go _conformance-corpus-rust \
	_conformance-corpus-java _conformance-corpus-dart _conformance-corpus-elixir

_conformance-corpus-cpp: build/conformance-harness build/conformance-cpp build/schema_test_cook
	@rm -rf build/verify/conformance-corpus-cpp && mkdir -p build/verify/conformance-corpus-cpp $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only cpp --claims --work build/verify/conformance-corpus-cpp --results $(VERIFY_RESULTS_DIR)/cpp.json

_conformance-corpus-c: build/conformance-harness build/conformance-c
	@rm -rf build/verify/conformance-corpus-c && mkdir -p build/verify/conformance-corpus-c $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only c --claims --work build/verify/conformance-corpus-c --results $(VERIFY_RESULTS_DIR)/c.json

_conformance-corpus-cs: toolchain-cs build/conformance-harness build-conformance-cs build-cs-cook
	@rm -rf build/verify/conformance-corpus-cs && mkdir -p build/verify/conformance-corpus-cs $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only cs --claims --work build/verify/conformance-corpus-cs --results $(VERIFY_RESULTS_DIR)/cs.json

_conformance-corpus-unity: toolchain-unity build/conformance-harness build/conformance-unity/.stamp
	@rm -rf build/verify/conformance-corpus-unity && mkdir -p build/verify/conformance-corpus-unity $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only unity --claims --work build/verify/conformance-corpus-unity --results $(VERIFY_RESULTS_DIR)/unity.json

_conformance-corpus-go: build/conformance-harness build/conformance-go
	@rm -rf build/verify/conformance-corpus-go && mkdir -p build/verify/conformance-corpus-go $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only go --claims --work build/verify/conformance-corpus-go --results $(VERIFY_RESULTS_DIR)/go.json

_conformance-corpus-rust: build/conformance-harness build/conformance-rust
	@rm -rf build/verify/conformance-corpus-rust && mkdir -p build/verify/conformance-corpus-rust $(VERIFY_RESULTS_DIR)
	./build/conformance-harness run --only rust --claims --work build/verify/conformance-corpus-rust --results $(VERIFY_RESULTS_DIR)/rust.json

_conformance-corpus-java: toolchain-java build/conformance-harness build-conformance-java
	@rm -rf build/verify/conformance-corpus-java && mkdir -p build/verify/conformance-corpus-java $(VERIFY_RESULTS_DIR)
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" ./build/conformance-harness run --only java --claims --work build/verify/conformance-corpus-java --results $(VERIFY_RESULTS_DIR)/java.json

_conformance-corpus-dart: toolchain-dart build/conformance-harness build/conformance-dart
	@rm -rf build/verify/conformance-corpus-dart && mkdir -p build/verify/conformance-corpus-dart $(VERIFY_RESULTS_DIR)
	DART="$(DART)" ./build/conformance-harness run --only dart --claims --work build/verify/conformance-corpus-dart --results $(VERIFY_RESULTS_DIR)/dart.json

_conformance-corpus-elixir: toolchain-elixir build/conformance-harness $(VERIFY_ELIXIR_BUILD)
	@rm -rf build/verify/conformance-corpus-elixir && mkdir -p build/verify/conformance-corpus-elixir $(VERIFY_RESULTS_DIR)
	BEAM_PATH="$(BEAM_PATH)" ELIXIR_TABLES_EBIN="$(CURDIR)/$(VERIFY_ELIXIR_EBIN)" ./build/conformance-harness run --only elixir --claims --work build/verify/conformance-corpus-elixir --results $(VERIFY_RESULTS_DIR)/elixir.json
