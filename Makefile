# schema — build the compiler, generate the C++ headers from the corpus, and
# prove they compile and link (prints OK). `make` runs the whole chain.

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

GO_SOURCES   := $(shell find cmd internal -name '*.go') go.mod
SCHEMAS      := $(wildcard examples/*.schema)
SCHEMAS128   := $(wildcard examples128/*.schema)
SCHEMAS_BENCH := $(wildcard bench/corpus/*.schema)

all: test

# The version the built binary reports. `git describe` gives the exact tag on a
# release build and tag-commits-hash elsewhere; when this is not a git checkout
# (a source tarball, say) the stamp is left empty and internal/version falls
# back to the toolchain's own build info.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X github.com/mas-bandwidth/schema/internal/version.version=$(VERSION),)

bin/schema: $(GO_SOURCES)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/schema

# one generate run emits every header; the stamp carries the dependency
generated/cpp/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated/cpp examples
	@touch $@

# the fixed-point + 128-bit unit (examples128/) — all five targets since the
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
	@printf '[package]\nname = "ludicrous"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { path = "../../$(SERIALIZE_RS)" }\n' > generated/rust-ludicrous/Cargo.toml
	@touch $@

generated/cs-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cs --out generated/cs-ludicrous examples128
	@touch $@

# the opt-in variant dispatch surface, generated beside the default so both
# representations stay compiled and run (--cpp-message variant)
build/generated-variant/.stamp: bin/schema $(SCHEMAS)
	@mkdir -p build
	./bin/schema generate --lang cpp --cpp-message variant --out build/generated-variant examples
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
	@printf '[package]\nname = "example"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { path = "../../$(SERIALIZE_RS)" }\n' > generated/rust/Cargo.toml
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

build/schema_test: generated/cpp/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/main.cpp test/second.cpp -o $@

build/schema_test_variant: build/generated-variant/.stamp test/variant_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Ibuild/generated-variant -Itest test/variant_main.cpp -o $@

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
#   bench/corpus/Bench.schema      package bench      — the family `rt` shapes
#     (BENCH-STANDARD.md §1.3); goldens testdata/wire/bench_*.bin
#   bench/corpus/RealWorld.schema  package realworld  — the §1.7 realistic
#     snapshot (RealPacket); golden testdata/wire/real_packet.bin
# Both are generated into all five languages so the bench shapes have ONE
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
	@printf '[package]\nname = "benchcorpus"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { path = "../../../$(SERIALIZE_RS)" }\n' > generated/bench/rust/Cargo.toml
	@printf '[package]\nname = "realworldcorpus"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { path = "../../../$(SERIALIZE_RS)" }\n' > generated/bench/rust-realworld/Cargo.toml
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

# The C half of the fixed-point and 128-bit corpus. Its ABSENCE is why a C
# codec that wrote nothing for every fixed field passed every gate: the C
# target was generated from examples/ only, and examples/ has no `fixed(`.
build/schema_test_c_ludicrous: generated/c-ludicrous/.stamp test/c-ludicrous/main.c
	@mkdir -p build
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Igenerated/c-ludicrous -I$(SERIALIZE_C) \
		test/c-ludicrous/main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

test: build/schema_test build/schema_test_variant build/schema_test_random build/schema_test_ludicrous build/schema_test_c build/schema_test_c_ludicrous build/schema_test_bench build/schema_test_bench_c generated/go/.stamp generated/rust/.stamp generated/cs/.stamp generated/js/.stamp generated/go-ludicrous/.stamp generated/rust-ludicrous/.stamp generated/cs-ludicrous/.stamp generated/js-ludicrous/.stamp generated/bench/go/.stamp generated/bench/rust/.stamp generated/bench/cs/.stamp generated/bench/js/.stamp
	./build/schema_test
	./build/schema_test_variant
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
	go test ./...

# Re-pin the goldens DELIBERATELY (SPEC §7.2 gates 1, 2, 7). A wire golden
# breaking under an unchanged schema is stop-the-line, never a quiet re-pin
# (SPEC §3.1) — this target is for intentional emitter/schema changes only.
update-goldens: build/schema_test build/schema_test_variant build/schema_test_ludicrous build/schema_test_bench
	@mkdir -p testdata/golden testdata/wire
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_ludicrous
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_bench
	./build/schema_test_variant
	go test ./...

# the cross-language serialize profiling harness (bench/README.md): builds and
# runs whichever language runners are available, Release flags, results CSV
# under bench/results/
bench:
	bench/run.sh

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
	./bin/schema fmt bench/corpus/Bench.schema
	./bin/schema fmt bench/corpus/RealWorld.schema

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench generated-current
