# schema — build the compiler, generate the C++ headers from the corpus, and
# prove they compile and link (prints OK). `make` runs the whole chain.

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Werror -ffp-contract=off

# the classic serialize runtime the generated C++ targets (header-only), the
# Go port the generated Go targets, the Rust port the generated Rust targets,
# and the C# port the generated C# targets (sibling checkouts; test/go/go.mod,
# test/rust/Cargo.toml and test/cs/schematest.csproj carry the same relative
# paths)
SERIALIZE    ?= ../serialize-cs-port/serialize
SERIALIZE_GO ?= ../serialize-cs-port/serialize.go
SERIALIZE_RS ?= ../serialize-cs-port/serialize.rs
SERIALIZE_CS ?= ../serialize-cs-port/serialize.cs
CXXFLAGS     += -I$(SERIALIZE)

# cargo lives in the rustup keg, which is not on PATH by default
RUSTUP_BIN ?= /opt/homebrew/opt/rustup/bin

GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod
SCHEMAS    := $(wildcard examples/*.schema)
SCHEMAS128 := $(wildcard examples128/*.schema)

all: test

bin/schema: $(GO_SOURCES)
	go build -o $@ ./cmd/schema

# one generate run emits every header; the stamp carries the dependency
generated/cpp/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated/cpp examples
	@touch $@

# the fixed-point + 128-bit unit (examples128/) generates C++ ONLY until the
# go/rs/cs runtime ports carry the phase-1 surface — those backends refuse it
# by field name. It compiles against serialize's fixed-point surface: until
# that merges into the sibling checkout, build with SERIALIZE=../serialize
generated/cpp/ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cpp --out generated/cpp/ludicrous examples128
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

build/schema_test: generated/cpp/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp test/main.cpp test/second.cpp -o $@

build/schema_test_variant: build/generated-variant/.stamp test/variant_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Ibuild/generated-variant test/variant_main.cpp -o $@

build/schema_test_random: generated/cpp/.stamp test/random_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp test/random_main.cpp -o $@

build/schema_test_ludicrous: generated/cpp/ludicrous/.stamp test/ludicrous_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp/ludicrous test/ludicrous_main.cpp -o $@

test: build/schema_test build/schema_test_variant build/schema_test_random build/schema_test_ludicrous generated/go/.stamp generated/rust/.stamp generated/cs/.stamp
	./build/schema_test
	./build/schema_test_variant
	./build/schema_test_random
	./build/schema_test_ludicrous
	cd test/go && go run .
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/cs && dotnet run
	go test ./...

# Re-pin the goldens DELIBERATELY (SPEC §7.2 gates 1, 2, 7). A wire golden
# breaking under an unchanged schema is stop-the-line, never a quiet re-pin
# (SPEC §3.1) — this target is for intentional emitter/schema changes only.
update-goldens: build/schema_test build/schema_test_variant build/schema_test_ludicrous
	@mkdir -p testdata/golden testdata/wire
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_ludicrous
	./build/schema_test_variant
	go test ./...

# the cross-language serialize profiling harness (bench/README.md): builds and
# runs whichever language runners are available, Release flags, results CSV
# under bench/results/
bench:
	bench/run.sh

check: bin/schema
	./bin/schema check examples
	./bin/schema check examples128

id: bin/schema
	./bin/schema id examples
	./bin/schema id examples128

fmt: bin/schema
	./bin/schema fmt examples
	./bin/schema fmt examples128

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench
