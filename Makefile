# schema — build the compiler, generate the C++ headers from the corpus, and
# prove they compile and link (prints OK). `make` runs the whole chain.

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Werror -ffp-contract=off

# the classic serialize runtime the generated C++ targets (header-only)
SERIALIZE ?= ../serialize-cs-port/serialize
CXXFLAGS  += -I$(SERIALIZE)

GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod
SCHEMAS    := $(wildcard examples/*.schema)

all: test

bin/schema: $(GO_SOURCES)
	go build -o $@ ./cmd/schema

# one generate run emits every header; the stamp carries the dependency
generated/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated examples
	@touch $@

# the opt-in variant dispatch surface, generated beside the default so both
# representations stay compiled and run (--cpp-message variant)
build/generated-variant/.stamp: bin/schema $(SCHEMAS)
	@mkdir -p build
	./bin/schema generate --lang cpp --cpp-message variant --out build/generated-variant examples
	@touch $@

build/schema_test: generated/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated test/main.cpp test/second.cpp -o $@

build/schema_test_variant: build/generated-variant/.stamp test/variant_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Ibuild/generated-variant test/variant_main.cpp -o $@

test: build/schema_test build/schema_test_variant
	./build/schema_test
	./build/schema_test_variant
	go test ./...

# Re-pin the goldens DELIBERATELY (SPEC §7.2 gates 1, 2, 7). A wire golden
# breaking under an unchanged schema is stop-the-line, never a quiet re-pin
# (SPEC §3.1) — this target is for intentional emitter/schema changes only.
update-goldens: build/schema_test build/schema_test_variant
	@mkdir -p testdata/golden testdata/wire
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	./build/schema_test_variant
	go test ./...

check: bin/schema
	./bin/schema check examples

id: bin/schema
	./bin/schema id examples

fmt: bin/schema
	./bin/schema fmt examples

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens
