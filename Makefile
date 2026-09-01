# schema — `make` builds the compiler (bin/schema) and nothing else: no
# serialize checkouts, no language toolchains, no generation. The full
# nine-language conformance chain is `make test`, and it needs the sibling
# runtime checkouts and toolchains documented below.

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Werror -ffp-contract=off

# the classic serialize runtime the generated C++ targets (header-only), the
# Go port the generated Go targets, the Rust port the generated Rust targets,
# and the C# port the generated C# targets (sibling checkouts; test/go/go.mod,
# test/rust/Cargo.toml, test/cs/schematest.csproj and their *-ludicrous twins
# carry the same relative paths). The JS runtime is a sibling checkout too
# (../serialize.js), but needs no variable: generated JS never imports the
# runtime, and the test legs import it by module-relative path directly.
SERIALIZE    ?= ../serialize
SERIALIZE_C  ?= ../serialize.c
SERIALIZE_GO ?= ../serialize.go
SERIALIZE_RS ?= ../serialize.rs
SERIALIZE_CS ?= ../serialize.cs
CXXFLAGS     += -I$(SERIALIZE)

# cargo lives in the rustup keg, which is not on PATH by default
RUSTUP_BIN ?= /opt/homebrew/opt/rustup/bin

# The Dart SDK, pinned per project (generated Dart is self-contained — no
# runtime checkout — so the SDK is the only Dart dependency). The default
# points at the repo-local unpacked SDK; CI installs the same version and
# overrides with DART=dart. To populate dist/ (gitignored):
#   Dart SDK 3.13.2 (stable, macos-arm64)
#   url:    https://storage.googleapis.com/dart-archive/channels/stable/release/3.13.2/sdk/dartsdk-macos-arm64-release.zip
#   sha256: 1e79f51341937f84cc1563a3fcad4a91706e35dee72bda69f4e955065c0e373a
#   unzip into dist/ and rename dart-sdk -> dart-sdk-3.13.2
DART ?= $(CURDIR)/dist/dart-sdk-3.13.2/bin/dart

# The JDK, pinned per project (generated Java is self-contained — no runtime
# checkout — so the JDK is the only Java dependency; same pin as the
# serialize.java port). The defaults point at the repo-local unpacked JDK;
# CI installs the same major and overrides with JAVA=java JAVAC=javac. To
# populate dist/ (gitignored):
#   Temurin JDK 21.0.12.1+1 (Eclipse Adoptium, macos aarch64)
#   url:    https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.12.1_1.tar.gz
#   sha256: 3623232f33a9c3baadf304480b2535f9a3cba8a58d42ecbb438ba267315d9998
#   untar into dist/ and rename to dist/jdk-21.0.12.1
# Generated code and the test legs compile with --release 17.
JAVA  ?= $(CURDIR)/dist/jdk-21.0.12.1/Contents/Home/bin/java
JAVAC ?= $(CURDIR)/dist/jdk-21.0.12.1/Contents/Home/bin/javac
# The BEAM toolchain, pinned per project (generated Elixir is self-contained —
# no runtime checkout — so Erlang/OTP + Elixir are the only dependencies).
# The defaults point at the repo-local unpacked toolchain; CI installs the
# same versions and overrides with ELIXIR=elixir MIX=mix. To populate dist/
# (gitignored):
#   Erlang/OTP 29.0.5 (erlef/otp_builds, signed macOS build, aarch64-apple-darwin)
#   url:    https://github.com/erlef/otp_builds/releases/download/OTP-29.0.5/otp-aarch64-apple-darwin.tar.gz
#   sha256: 24b9e00da2b9ad25b1f182e2efd73ff316e46ec4b143c0cc3c69dbd27d5a594d
#   untar into dist/otp-29.0.5
#   Elixir 1.20.4 (precompiled for OTP 29)
#   url:    https://github.com/elixir-lang/elixir/releases/download/v1.20.4/elixir-otp-29.zip
#   sha256: 7863c546cda13fecc949e562e326042451dacf8fd8698a36783cb71eeb223b46
#   unzip into dist/elixir-1.20.4
# The elixir/mix launchers find erl through PATH, so the pinned invocations
# carry both bin directories.
BEAM_PATH ?= $(CURDIR)/dist/otp-29.0.5/bin:$(CURDIR)/dist/elixir-1.20.4/bin
ELIXIR    ?= PATH="$(BEAM_PATH):$$PATH" elixir
MIX       ?= PATH="$(BEAM_PATH):$$PATH" mix

GO_SOURCES   := $(shell find cmd internal -name '*.go') go.mod
SCHEMAS      := $(wildcard examples/*.schema)
SCHEMAS128   := $(wildcard examples128/*.schema)
SCHEMAS_BENCH := $(wildcard bench/corpus/*.schema)
SCHEMAS_TABLES := $(wildcard tables/examples/*.schema)
SCHEMAS_TABLES_POINTERS := $(wildcard tables/pointers/*.schema)

all: bin/schema

# The version the built binary reports. `git describe` gives the exact tag on a
# release build and tag-commits-hash elsewhere; when this is not a git checkout
# (a source tarball, say) the stamp is left empty and internal/version falls
# back to the toolchain's own build info.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X github.com/mas-bandwidth/schema/v2/internal/version.version=$(VERSION),)

bin/schema: $(GO_SOURCES)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/schema

# one generate run emits every header; the stamp carries the dependency
generated/cpp/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated/cpp examples
	@touch $@

# the fixed-point + 128-bit unit (examples128/) — all eight targets since the
# serialize ports carry the phase-1 surface; each generated unit gets the same
# module/manifest wiring as its main-corpus twin
generated/cpp/ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cpp --out generated/cpp/ludicrous examples128
	@touch $@

generated/go-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang go --out generated/go-ludicrous examples128
	@printf 'module ludicrous\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../$(SERIALIZE_GO)\n' > generated/go-ludicrous/go.mod
	@touch $@

generated/rust-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang rust --out generated/rust-ludicrous/src examples128
	@printf '[package]\nname = "ludicrous"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../$(SERIALIZE_RS)" }\n' > generated/rust-ludicrous/Cargo.toml
	@touch $@

generated/cs-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cs --out generated/cs-ludicrous examples128
	@touch $@

# the Go target: generated package + module wiring (the go.mod is build
# wiring, not schema output — the emitter writes only .go files)
generated/go/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang go --out generated/go examples
	@printf 'module example\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../$(SERIALIZE_GO)\n' > generated/go/go.mod
	@touch $@

# the Rust target: generated crate + manifest wiring (the Cargo.toml is build
# wiring, not schema output — the emitter writes only .rs files; the manifest
# sits one level above src/, so the runtime path gains one more ../)
generated/rust/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang rust --out generated/rust/src examples
	@printf '[package]\nname = "example"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../$(SERIALIZE_RS)" }\n' > generated/rust/Cargo.toml
	@touch $@

# the C# target: generated sources only — test/cs/schematest.csproj compiles
# them beside the serialize.cs runtime via <Compile Include> items
generated/cs/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cs --out generated/cs examples
	@touch $@

# the JavaScript target: generated ES modules only, no wiring file at all —
# generated code never imports the runtime (every wire call is a method on
# the stream parameter), so the serialize.js sibling checkout is a test-leg
# concern, not a generation one
generated/js/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang js --out generated/js examples
	@touch $@

generated/js-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang js --out generated/js-ludicrous examples128
	@touch $@

# the Dart target: generated libraries only, no wiring file at all —
# generated Dart is self-contained (the bitpacker is inlined per issue #155),
# so there is no runtime checkout and no pubspec; the test legs import the
# generated files by relative path directly
generated/dart/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang dart --out generated/dart examples
	@touch $@

generated/dart-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang dart --out generated/dart-ludicrous examples128
	@touch $@

# the Java target: generated classes only, no wiring file at all — generated
# Java is self-contained (the bitpacker is inlined per issue #156), so there
# is no runtime checkout and no build file; the test legs compile the
# generated sources beside their Main.java directly
generated/java/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang java --out generated/java examples
	@touch $@

generated/java-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang java --out generated/java-ludicrous examples128
	@touch $@

# the Elixir target: generated modules only, no wiring file at all —
# generated Elixir is self-contained (the port's packing shapes are inlined
# per issue #167), so there is no runtime checkout and no mix project; the
# test legs Code.require_file the generated files by relative path directly
generated/elixir/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang elixir --out generated/elixir examples
	@touch $@

generated/elixir-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang elixir --out generated/elixir-ludicrous examples128
	@touch $@

build/schema_test: generated/cpp/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/main.cpp test/second.cpp -o $@

# The two-unit guard regression (issue #189): two packages, both with
# string(N) fields, included into ONE translation unit. Its corpus is
# test-only — generated at build time into build/, never part of the
# committed generated/ tree (one package per generate call, SPEC §3.2).
build/guard-generated/.stamp: bin/schema test/guard/Alpha.schema test/guard/Beta.schema
	@mkdir -p build/guard-generated
	./bin/schema generate --lang cpp --out build/guard-generated test/guard/Alpha.schema
	./bin/schema generate --lang cpp --out build/guard-generated test/guard/Beta.schema
	@touch $@

build/schema_test_guard: build/guard-generated/.stamp test/guard/main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Ibuild/guard-generated test/guard/main.cpp -o $@

# The tables corpus (SPEC-TABLES.md): the tabledemo unit plus the
# two-generation evolution pair (tblv1/tblv2), generated at build time into
# build/ — test-only, never part of the committed generated/ tree.
build/tables-generated/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema
	@mkdir -p build/tables-generated
	./bin/schema generate --lang cpp --out build/tables-generated/examples tables/examples
	./bin/schema generate --lang cpp --out build/tables-generated/pointers tables/pointers
	./bin/schema generate --lang cpp --out build/tables-generated/v1 test/tables/V1.schema
	./bin/schema generate --lang cpp --out build/tables-generated/v2 test/tables/V2.schema
	./bin/schema generate --lang cpp --out build/tables-generated/p1 test/tables/P1.schema
	./bin/schema generate --lang cpp --out build/tables-generated/p2 test/tables/P2.schema
	@touch $@

# The ZERO-COST GATE (SPEC-TABLES.md): a table with no pointer in its by-value
# closure must pay NOTHING for the pointer machinery — no builder, no arena, no
# handles, no lifecycle surface, no extra descriptor columns. The pointer-free
# corpus's generated headers must not contain one symbol of it. (The stronger
# one-time proof — byte-identical emission against the pre-pointer baseline —
# is recorded in the round log; this is the standing gate.)
.PHONY: tables-zero-cost
tables-zero-cost: build/tables-generated/.stamp
	@for f in build/tables-generated/examples/*Table.h build/tables-generated/v1/*Table.h \
	          build/tables-generated/v2/*Table.h build/tables-generated/p1/*Table.h; do \
		if grep -nE "TableArena|TableSlot|TableWorker|TableRef|TableRegion|kTableSegment|kTableSlab|kTableMaxDepth|is_pointer|Builder|LayoutId|OpenWalk|PackMeasure|LoadMeasure|Cook|Open\\(" $$f; then \
			echo "ZERO-COST GATE FAILED: pointer machinery leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables zero-cost gate: value-only tables carry no pointer machinery"

# Deliberately compiled WITHOUT -I$(SERIALIZE): the generated Table headers
# carry no serialize dependency, and this build proves it stays that way.
build/schema_test_tables: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -pthread \
		-Ibuild/tables-generated/examples -Ibuild/tables-generated/pointers \
		-Ibuild/tables-generated/v1 -Ibuild/tables-generated/v2 \
		-Ibuild/tables-generated/p1 -Ibuild/tables-generated/p2 \
		test/tables/main.cpp -o $@

build/schema_test_random: generated/cpp/.stamp test/random_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/random_main.cpp -o $@

build/schema_test_ludicrous: generated/cpp/ludicrous/.stamp test/ludicrous_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp/ludicrous test/ludicrous_main.cpp -o $@

generated/c/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang c --out generated/c examples
	@touch $@

# -Wtype-limits is where gcc reports a vacuous comparison and clang stays quiet,
# so it rides unconditionally. clang says the same thing under a flag gcc does
# not recognise, so that one is FEATURE TESTED rather than assumed -- hardcoding
# it broke the Linux leg once already, which is the argument for testing rather
# than guessing which compiler is which.
C_TAUTOLOGICAL := $(shell $(CC) -Wtautological-type-limit-compare -E - < /dev/null > /dev/null 2>&1 && echo -Wtautological-type-limit-compare)

# The C corpus test. -Werror on purpose: generated headers are included by the
# CONSUMER's translation units, so a warning here is a build failure in their
# tree, not ours.
build/schema_test_c: generated/c/.stamp test/c/main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/c -I$(SERIALIZE_C) \
		test/c/main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

generated/c-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang c --out generated/c-ludicrous examples128
	@touch $@

# the bench corpus — TWO units sharing one directory (SPEC §3.2: one package
# per unit, so every corpus command below names its unit's file, never the
# directory):
#   bench/corpus/Bench.schema      package bench      — the bench-corpus shapes
#     (BENCH-STANDARD.md §1.3); goldens testdata/wire/bench_*.bin
#   bench/corpus/RealWorld.schema  package realworld  — the §1.7 realistic
#     snapshot (RealPacket); golden testdata/wire/real_packet.bin
# Both are generated into all six languages so the bench shapes have ONE
# definition; the goldens come from the generated C++ (test/bench/main.cpp)
# and every bench runner is §1.5-gated against them. Languages whose module
# layout cannot hold two packages in one directory put the realworld unit in
# its own subdirectory (go, cs) or sibling crate
# (rust — one crate root per unit).
#
# COMPILE gates: generating a unit proves nothing about it compiling — the cs
# realworld unit shipped uncompilable (SerializeFixed had no 8-bit overload,
# issue #80's C# leg was its first consumer) while every gate stayed green.
# The test target builds generated/bench/go and both rust crates directly;
# C# has no unit-local build file, so `cd bench/cs && dotnet build` is the
# compile gate for the realworld unit (the runner is its one consumer). The
# cs Bench unit still has NO compile gate — nothing consumes it.
generated/bench/cpp/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang cpp --out generated/bench/cpp bench/corpus/Bench.schema
	./bin/schema generate --lang cpp --out generated/bench/cpp bench/corpus/RealWorld.schema
	@touch $@

generated/bench/c/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang c --out generated/bench/c bench/corpus/Bench.schema
	./bin/schema generate --lang c --out generated/bench/c bench/corpus/RealWorld.schema
	@touch $@

generated/bench/go/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang go --out generated/bench/go bench/corpus/Bench.schema
	./bin/schema generate --lang go --out generated/bench/go/realworld bench/corpus/RealWorld.schema
	@printf 'module bench\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../../$(SERIALIZE_GO)\n' > generated/bench/go/go.mod
	@touch $@

generated/bench/rust/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang rust --out generated/bench/rust/src bench/corpus/Bench.schema
	./bin/schema generate --lang rust --out generated/bench/rust-realworld/src bench/corpus/RealWorld.schema
	@printf '[package]\nname = "benchcorpus"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' > generated/bench/rust/Cargo.toml
	@printf '[package]\nname = "realworldcorpus"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' > generated/bench/rust-realworld/Cargo.toml
	@touch $@

generated/bench/cs/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang cs --out generated/bench/cs bench/corpus/Bench.schema
	./bin/schema generate --lang cs --out generated/bench/cs/realworld bench/corpus/RealWorld.schema
	@touch $@

# the realworld unit sits in its own subdirectory like go/cs, so the two
# units' outputs never collide
generated/bench/js/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang js --out generated/bench/js bench/corpus/Bench.schema
	./bin/schema generate --lang js --out generated/bench/js/realworld bench/corpus/RealWorld.schema
	@touch $@

generated/bench/dart/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang dart --out generated/bench/dart bench/corpus/Bench.schema
	./bin/schema generate --lang dart --out generated/bench/dart/realworld bench/corpus/RealWorld.schema
	@touch $@

generated/bench/java/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang java --out generated/bench/java bench/corpus/Bench.schema
	./bin/schema generate --lang java --out generated/bench/java/realworld bench/corpus/RealWorld.schema
generated/bench/elixir/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang elixir --out generated/bench/elixir bench/corpus/Bench.schema
	./bin/schema generate --lang elixir --out generated/bench/elixir/realworld bench/corpus/RealWorld.schema
	@touch $@

# the C++ producer/verifier of the bench-corpus goldens, and the C twin that
# proves the C emitter compiles under the strict flags AND matches those bytes
build/schema_test_bench: generated/bench/cpp/.stamp test/bench/main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/cpp test/bench/main.cpp -o $@

build/schema_test_bench_c: generated/bench/c/.stamp test/bench/c_main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/bench/c -I$(SERIALIZE_C) \
		test/bench/c_main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

# The Java test legs compile the generated sources beside their Main.java —
# -Xlint:all -Werror on purpose: generated sources are compiled by the
# CONSUMER's javac, so a warning here is a build failure in their tree, not
# ours. Both modes then run: -ea (the checked twin — writer contracts fire)
# and default (the release shape, issue #156's target).
build/java-test/.stamp: generated/java/.stamp generated/bench/java/.stamp test/java/Main.java
	@mkdir -p build/java-test
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-test \
		generated/java/*.java generated/bench/java/*.java \
		generated/bench/java/realworld/*.java test/java/Main.java
	@touch $@

build/java-test-ludicrous/.stamp: generated/java-ludicrous/.stamp test/java-ludicrous/Main.java
	@mkdir -p build/java-test-ludicrous
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-test-ludicrous \
		generated/java-ludicrous/*.java test/java-ludicrous/Main.java
	@touch $@

# the Java bench runner's compile gate (the twin of `dart analyze bench/dart`);
# the timed run is by hand — bench/java/Main.java documents it
build/java-bench/.stamp: generated/bench/java/.stamp bench/java/Main.java
	@mkdir -p build/java-bench
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-bench \
		generated/bench/java/*.java bench/java/Main.java
	@touch $@

# The C half of the fixed-point and 128-bit corpus. Its ABSENCE is why a C
# codec that wrote nothing for every fixed field passed every gate: the C
# target was generated from examples/ only, and examples/ has no `fixed(`.
build/schema_test_c_ludicrous: generated/c-ludicrous/.stamp test/c-ludicrous/main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/c-ludicrous -I$(SERIALIZE_C) \
		test/c-ludicrous/main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

test: build/schema_test build/schema_test_guard build/schema_test_tables build/schema_test_random build/schema_test_ludicrous build/schema_test_c build/schema_test_c_ludicrous build/schema_test_bench build/schema_test_bench_c generated/go/.stamp generated/rust/.stamp generated/cs/.stamp generated/js/.stamp generated/dart/.stamp generated/java/.stamp generated/elixir/.stamp generated/go-ludicrous/.stamp generated/rust-ludicrous/.stamp generated/cs-ludicrous/.stamp generated/js-ludicrous/.stamp generated/dart-ludicrous/.stamp generated/java-ludicrous/.stamp generated/elixir-ludicrous/.stamp generated/bench/go/.stamp generated/bench/rust/.stamp generated/bench/cs/.stamp generated/bench/js/.stamp generated/bench/dart/.stamp generated/bench/java/.stamp generated/bench/elixir/.stamp build/java-test/.stamp build/java-test-ludicrous/.stamp build/java-bench/.stamp
	./build/schema_test
	./build/schema_test_guard
	./build/schema_test_tables
	$(MAKE) tables-zero-cost
	./build/schema_test_random
	./build/schema_test_ludicrous
	cd test/c && ../../build/schema_test_c
	cd test/c-ludicrous && ../../build/schema_test_c_ludicrous
	./build/schema_test_bench
	./build/schema_test_bench_c
	cd generated/bench/go && go build ./...
	cd generated/bench/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	cd generated/bench/rust-realworld && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	cd bench/cs && dotnet build -c Release --nologo -v quiet
	cd test/go && go run .
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/cs && dotnet run
	cd test/js && node main.mjs && NODE_ENV=production node main.mjs
	cd test/go-ludicrous && go run .
	cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/cs-ludicrous && dotnet run
	cd test/js-ludicrous && node main.mjs && NODE_ENV=production node main.mjs
	$(DART) analyze generated/dart generated/dart-ludicrous generated/bench/dart test/dart test/dart-ludicrous bench/dart
	$(DART) format --set-exit-if-changed --output=none generated/dart generated/dart-ludicrous generated/bench/dart
	cd test/dart && $(DART) --enable-asserts main.dart
	@mkdir -p build
	cd test/dart && $(DART) compile exe -o ../../build/schema_test_dart main.dart >/dev/null && ../../build/schema_test_dart
	cd test/dart-ludicrous && $(DART) --enable-asserts main.dart
	cd test/dart-ludicrous && $(DART) compile exe -o ../../build/schema_test_dart_ludicrous main.dart >/dev/null && ../../build/schema_test_dart_ludicrous
	cd test/java && $(JAVA) -ea -cp ../../build/java-test Main
	cd test/java && $(JAVA) -cp ../../build/java-test Main
	cd test/java-ludicrous && $(JAVA) -ea -cp ../../build/java-test-ludicrous Main
	cd test/java-ludicrous && $(JAVA) -cp ../../build/java-test-ludicrous Main
	$(MIX) format --check-formatted generated/elixir/*.ex generated/elixir-ludicrous/*.ex generated/bench/elixir/*.ex generated/bench/elixir/realworld/*.ex
	cd test/elixir && $(ELIXIR) main.exs
	cd test/elixir-ludicrous && $(ELIXIR) main.exs
	go test ./...

# Re-pin the goldens DELIBERATELY (SPEC §7.2 gates 1, 2, 7). A wire golden
# breaking under an unchanged schema is stop-the-line, never a quiet re-pin
# (SPEC §3.1) — this target is for intentional emitter/schema changes only.
update-goldens: build/schema_test build/schema_test_ludicrous build/schema_test_bench
	@mkdir -p testdata/golden testdata/wire
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_ludicrous
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_bench
	go test ./...

# the cross-language serialize profiling harness (bench/README.md): builds and
# runs whichever language runners are available, Release flags, results CSV
# under bench/results/
bench:
	bench/run.sh

# The bench_mixed variant data (issue #191): 64 wire buffers the data-driven
# drivers bench, regenerated from bench/corpus/Bench.schema's generated Go
# codec. Deterministic — a regeneration that changes the committed file means
# the shape or the §2.7 LCG mapping moved, and the tool refuses outright if
# variant 0 stops equalling testdata/wire/bench_mixed.bin. Needs the
# serialize.go checkout ($(SERIALIZE_GO)); the committed data's own gate,
# bench/corpus/variants_test.go, needs nothing and runs in `make test`.
bench-variants: generated/bench/go/.stamp
	cd bench/tools/variantgen && go run .

# Prove the COMMITTED generated/ tree matches what the current compiler
# emits (issue #30). `make test` regenerates every tracked generated file in
# place, so staleness is precisely a dirty tree afterwards — a tracked file
# that changed, or a newly emitted file nobody committed. CI runs the same
# two checks after its make test step.
generated-current: test
	@git diff --exit-code generated/ || { \
		echo "committed generated/ tree is STALE — the current compiler emits different text."; \
		echo "review the diff above, then commit the regenerated files."; \
		exit 1; \
	}
	@untracked=$$(git status --porcelain generated/); \
	if [ -n "$$untracked" ]; then \
		echo "$$untracked"; \
		echo "the generator emitted files that are not committed under generated/ — add them."; \
		exit 1; \
	fi
	@echo "generated/ tree is current"

# bench/corpus holds two units (one package per unit, SPEC §3.2), so the
# corpus commands name each unit's file rather than the directory
check: bin/schema
	./bin/schema check examples
	./bin/schema check examples128
	./bin/schema check tables/examples
	./bin/schema check tables/pointers
	./bin/schema check test/tables/V1.schema
	./bin/schema check test/tables/V2.schema
	./bin/schema check test/tables/P1.schema
	./bin/schema check test/tables/P2.schema
	./bin/schema check bench/corpus/Bench.schema
	./bin/schema check bench/corpus/RealWorld.schema

id: bin/schema
	./bin/schema id examples
	./bin/schema id examples128
	./bin/schema id bench/corpus/Bench.schema
	./bin/schema id bench/corpus/RealWorld.schema

fmt: bin/schema
	./bin/schema fmt examples
	./bin/schema fmt examples128
	./bin/schema fmt tables/examples
	./bin/schema fmt tables/pointers
	./bin/schema fmt test/tables/V1.schema
	./bin/schema fmt test/tables/V2.schema
	./bin/schema fmt test/tables/P1.schema
	./bin/schema fmt test/tables/P2.schema
	./bin/schema fmt bench/corpus/Bench.schema
	./bin/schema fmt bench/corpus/RealWorld.schema

# The one-benchmark rule, made mechanical: no hand-coded measurement of a
# schema shape anywhere in this repo except what bench/SHAPE-GATE.allow names.
shape-gate:
	go run ./bench/tools/shapegate

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench bench-variants generated-current shape-gate
