# schema — build the compiler, generate the C++ headers from the corpus, and
# prove they compile and link (prints OK). `make` runs the whole chain.

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Werror -ffp-contract=off

GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod
SCHEMAS    := $(wildcard examples/*.schema)

all: test

bin/schema: $(GO_SOURCES)
	go build -o $@ ./cmd/schema

# one generate run emits every header; the stamp carries the dependency
generated/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated examples
	@touch $@

build/schema_test: generated/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated test/main.cpp test/second.cpp -o $@

test: build/schema_test
	./build/schema_test

check: bin/schema
	./bin/schema check examples

id: bin/schema
	./bin/schema id examples

clean:
	rm -rf bin build generated

.PHONY: all test check id clean
