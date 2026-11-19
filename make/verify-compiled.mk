# make/verify-compiled.mk — Fast (< 2m) per-language optimization verification
# gates for the 9 compiled / SDK targets:
#
#   cpp, c, cs, unity, go, rust, java, dart, elixir
#
# Each language exposes three independent sub-targets sized for ci-fast.yml's
# strict 2-minute job ceiling, plus one umbrella target:
#
#   make verify-<lang>-conformance   all 21 table surfaces (--claims) + packet wire + 128-bit/UTF-8/defaults
#   make verify-<lang>-bench         packet wire 64-variant byte-checked hot loop + table wire 64-variant gate
#   make verify-<lang>-fuzz          deterministic wire-fuzz smoke (plain + --retain, stateful reuse + all roots)
#   make verify-<lang>               runs all three in a single parallel Make DAG

VERIFY_COMPILED_LEGS := cpp c cs unity go rust java dart elixir
VERIFY_JOBS          ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
VERIFY_JFLAG          = $(if $(findstring j,$(MAKEFLAGS)),,-j$(VERIFY_JOBS))
VERIFY_FUZZ_SEED     ?= 1
VERIFY_FUZZ_RANDOM_N ?= 2500
VERIFY_FUZZ_MAX      ?= 5000

define verify_compiled_fuzz_rules
.PHONY: _verify-$(1)-fuzz verify-$(1)-fuzz
_verify-$(1)-fuzz: build/conformance-harness $$(WFR_DEPS_$(1))
	@mkdir -p build/verify
	$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz \
		--driver "$$(WFR_DRIVER_$(1))" $$(WFR_FLAGS_$(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_FUZZ_RANDOM_N) --max-mutants $$(VERIFY_FUZZ_MAX) \
		--failed build/verify/fuzz-$(1)-plain.bin
	$$(WFR_ENV_$(1)) ./build/conformance-harness wire-fuzz --retain \
		--driver "$$(WFR_DRIVER_$(1))" $$(WFR_FLAGS_$(1)) \
		--seed $$(VERIFY_FUZZ_SEED) --n $$(VERIFY_FUZZ_RANDOM_N) --max-mutants $$(VERIFY_FUZZ_MAX) \
		--failed build/verify/fuzz-$(1)-retain.bin

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
_verify-cpp-conformance: build/conformance-harness build/conformance-cpp build/schema_test build/schema_test_ludicrous
	./build/schema_test
	./build/schema_test_ludicrous
	@rm -rf build/verify/conformance-cpp && mkdir -p build/verify/conformance-cpp
	./build/conformance-harness run --only cpp --claims --work build/verify/conformance-cpp

_verify-cpp-bench: generated/bench/cpp/.stamp generated/bench/tables/cpp/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-cpp.csv
	bench/run.sh --quick --bare --only cpp --out build/verify/bench-cpp.csv
	@test -s build/verify/bench-cpp.csv
	$(MAKE) --no-print-directory tables-cpp-bench-gate

# ---- c ----

.PHONY: _verify-c-conformance _verify-c-bench
_verify-c-conformance: build/conformance-harness build/conformance-c build/schema_test_c build/schema_test_c_ludicrous packet-defaults-c packet-utf8-c
	cd test/c && ../../build/schema_test_c
	cd test/c-ludicrous && ../../build/schema_test_c_ludicrous
	@rm -rf build/verify/conformance-c && mkdir -p build/verify/conformance-c
	./build/conformance-harness run --only c --claims --work build/verify/conformance-c

_verify-c-bench: generated/bench/c/.stamp generated/bench/tables/c/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-c.csv
	bench/run.sh --quick --bare --only c --out build/verify/bench-c.csv
	@test -s build/verify/bench-c.csv
	$(MAKE) --no-print-directory tables-c-bench-gate

# ---- cs ----

.PHONY: _verify-cs-conformance _verify-cs-bench
_verify-cs-conformance: toolchain-cs build/conformance-harness build-conformance-cs generated/cs/.stamp generated/cs-ludicrous/.stamp packet-defaults-cs packet-utf8-cs
	cd test/cs && $(DOTNET) run --nologo
	cd test/cs && $(DOTNET) run -c Release --nologo
	cd test/cs-ludicrous && $(DOTNET) run --nologo
	@rm -rf build/verify/conformance-cs && mkdir -p build/verify/conformance-cs
	./build/conformance-harness run --only cs --claims --work build/verify/conformance-cs

_verify-cs-bench: toolchain-cs generated/bench/cs/.stamp generated/bench/tables/cs/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-cs.csv
	DOTNET="$(DOTNET)" bench/run.sh --quick --bare --only cs --out build/verify/bench-cs.csv
	@test -s build/verify/bench-cs.csv
	$(MAKE) --no-print-directory tables-cs-bench-gate

# ---- unity ----

.PHONY: _verify-unity-conformance _verify-unity-bench
_verify-unity-conformance: toolchain-unity build/conformance-harness build/conformance-unity/.stamp unity-wire packet-defaults-unity packet-utf8-unity
	@rm -rf build/verify/conformance-unity && mkdir -p build/verify/conformance-unity
	./build/conformance-harness run --only unity --claims --work build/verify/conformance-unity

_verify-unity-bench: toolchain-unity bin/schema unity-wire
	$(MAKE) --no-print-directory tables-unity-bench-gate

# ---- go ----

.PHONY: _verify-go-conformance _verify-go-bench
_verify-go-conformance: build/conformance-harness build/conformance-go generated/go/.stamp generated/go-ludicrous/.stamp packet-defaults-go packet-utf8-go
	cd test/go && go run .
	cd test/go-ludicrous && go run .
	@rm -rf build/verify/conformance-go && mkdir -p build/verify/conformance-go
	./build/conformance-harness run --only go --claims --work build/verify/conformance-go

_verify-go-bench: generated/bench/go/.stamp generated/bench/tables/go/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-go.csv
	bench/run.sh --quick --bare --only go --out build/verify/bench-go.csv
	@test -s build/verify/bench-go.csv
	$(MAKE) --no-print-directory tables-go-bench-gate

# ---- rust ----

.PHONY: _verify-rust-conformance _verify-rust-bench
_verify-rust-conformance: build/conformance-harness build/conformance-rust generated/rust/.stamp generated/rust-ludicrous/.stamp packet-defaults-rust packet-utf8-rust
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release
	cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release
	@rm -rf build/verify/conformance-rust && mkdir -p build/verify/conformance-rust
	./build/conformance-harness run --only rust --claims --work build/verify/conformance-rust

_verify-rust-bench: generated/bench/rust/.stamp generated/bench/rust-realworld/.stamp generated/bench/paired/rust/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-rust.csv
	PATH="$(RUSTUP_BIN):$$PATH" bench/run.sh --quick --bare --only rust --out build/verify/bench-rust.csv
	@test -s build/verify/bench-rust.csv
	PATH="$(RUSTUP_BIN):$$PATH" RUSTUP_BIN="$(RUSTUP_BIN)" go run ./bench/paired -mode gate -langs rust

# ---- java ----

.PHONY: _verify-java-conformance _verify-java-bench
_verify-java-conformance: toolchain-java build/conformance-harness build-conformance-java build/java-test/.stamp build/java-test-ludicrous/.stamp packet-defaults-java packet-utf8-java
	cd test/java && $(JAVA) -ea -cp ../../build/java-test Main
	cd test/java && $(JAVA) -cp ../../build/java-test Main
	cd test/java-ludicrous && $(JAVA) -ea -cp ../../build/java-test-ludicrous Main
	@rm -rf build/verify/conformance-java && mkdir -p build/verify/conformance-java
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" ./build/conformance-harness run --only java --claims --work build/verify/conformance-java

_verify-java-bench: toolchain-java generated/bench/java/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-java.csv
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" bench/run.sh --quick --bare --only java --out build/verify/bench-java.csv
	@test -s build/verify/bench-java.csv
	JAVA="$(JAVA)" JAVAC="$(JAVAC)" go run ./bench/paired -mode gate -langs java

# ---- dart ----

.PHONY: _verify-dart-conformance _verify-dart-bench
_verify-dart-conformance: toolchain-dart build/conformance-harness build/conformance-dart generated/dart/.stamp generated/dart-ludicrous/.stamp packet-defaults-dart packet-utf8-dart
	cd test/dart && $(DART) --enable-asserts main.dart
	cd test/dart-ludicrous && $(DART) --enable-asserts main.dart
	@rm -rf build/verify/conformance-dart && mkdir -p build/verify/conformance-dart
	DART="$(DART)" ./build/conformance-harness run --only dart --claims --work build/verify/conformance-dart

_verify-dart-bench: toolchain-dart generated/bench/dart/.stamp generated/bench/tables/dart/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-dart.csv
	DART="$(DART)" bench/run.sh --quick --bare --only dart --out build/verify/bench-dart.csv
	@test -s build/verify/bench-dart.csv
	$(MAKE) --no-print-directory tables-dart-bench-gate

# ---- elixir ----

.PHONY: _verify-elixir-conformance _verify-elixir-bench
_verify-elixir-conformance: toolchain-elixir build/conformance-harness build-conformance-elixir generated/elixir/.stamp generated/elixir-ludicrous/.stamp packet-defaults-elixir packet-utf8-elixir
	cd test/elixir && $(ELIXIR) main.exs
	cd test/elixir-ludicrous && $(ELIXIR) main.exs
	@rm -rf build/verify/conformance-elixir && mkdir -p build/verify/conformance-elixir
	BEAM_PATH="$(BEAM_PATH)" ELIXIR_TABLES_EBIN="$(CURDIR)/build/elixir-tables-ebin" ./build/conformance-harness run --only elixir --claims --work build/verify/conformance-elixir

_verify-elixir-bench: toolchain-elixir generated/bench/elixir/.stamp generated/bench/tables/elixir/.stamp
	@mkdir -p build/verify && rm -f build/verify/bench-elixir.csv
	BEAM_PATH="$(BEAM_PATH)" bench/run.sh --quick --bare --only elixir --out build/verify/bench-elixir.csv
	@test -s build/verify/bench-elixir.csv
	$(MAKE) --no-print-directory tables-elixir-bench-gate
