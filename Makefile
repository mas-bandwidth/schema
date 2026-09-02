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
SCHEMAS_TABLES_BLOCK := $(wildcard tables/block/*.schema)

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
#
# The generate step and the include path are parameterised by generator binary
# and output root, because the big-endian negative control below regenerates
# the WHOLE corpus from a sabotaged emitter: a second copy of these lists would
# be a second corpus, and the gate would stop covering what the leg covers.
define tables_generate
	$(1) generate --lang cpp --out $(2)/examples tables/examples
	$(1) generate --lang cpp --out $(2)/pointers tables/pointers
	$(1) generate --lang cpp --out $(2)/block tables/block
	$(1) generate --lang cpp --out $(2)/v1 test/tables/V1.schema
	$(1) generate --lang cpp --out $(2)/v2 test/tables/V2.schema
	$(1) generate --lang cpp --out $(2)/p1 test/tables/P1.schema
	$(1) generate --lang cpp --out $(2)/p2 test/tables/P2.schema
	$(1) generate --lang cpp --out $(2)/p3 test/tables/P3.schema
	$(1) generate --lang cpp --out $(2)/jsonkeys test/tables/JsonKeys.schema
endef

tables_includes = -I$(1)/examples -I$(1)/pointers -I$(1)/block -Itest/tables \
	-I$(1)/v1 -I$(1)/v2 -I$(1)/p1 -I$(1)/p2 -I$(1)/p3 -I$(1)/jsonkeys

build/tables-generated/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema
	@mkdir -p build/tables-generated
	$(call tables_generate,./bin/schema,build/tables-generated)
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
	          build/tables-generated/v2/*Table.h build/tables-generated/p1/*Table.h \
	          build/tables-generated/p3/*Table.h; do \
		if grep -nE "TableArena|TableSlot|TableWorker|TableRef|TableRegion|kTableSegment|kTableSlab|kTableMaxDepth|is_pointer|Builder|LayoutId|OpenWalk|PackMeasure|LoadMeasure|Cook|Open\\(" $$f; then \
			echo "ZERO-COST GATE FAILED: pointer machinery leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables zero-cost gate: value-only tables carry no pointer machinery"

# The GENERIC-WALK GATE (SPEC-TABLES.md §16): the text form is ONE walk over
# the reflection descriptors, not a per-table codec — that is the property
# which makes it schema's rather than a packer's. The walker's source must
# therefore be the SAME BYTES in every generated .cpp of the corpus, whose
# units disagree about packages, tables, kinds and pointer modes. The package
# name lives in the guard and the namespace, outside the markers, so this is a
# strict byte comparison with nothing normalised away. (It moved from the
# headers to the .cpp files with the walker itself — SPEC-TABLES.md §16.1.)
.PHONY: tables-json-walk
tables-json-walk: build/tables-generated/.stamp
	@rm -rf build/json-walk && mkdir -p build/json-walk
	@for f in build/tables-generated/*/*Table.cpp; do \
		out=build/json-walk/$$(echo $$f | tr / _); \
		awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then echo "GENERIC-WALK GATE FAILED: no walker in $$f"; exit 1; fi; \
	done
	@first=""; for f in build/json-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables generic-walk gate: one walker, byte-identical in $$(ls build/json-walk | wc -l | tr -d ' ') generated .cpp files"

# The NEGATIVE CONTROL for the walk (SPEC-TABLES.md §16.5). A green round-trip
# suite proves nothing until the suite is shown capable of going red: the
# walker's field-offset arithmetic is sabotaged by one field width, and the
# round trip must FAIL on the first table with two fields. Attachment is that
# table — two four-byte fields at offsets 0 and 4 — so the sabotage swaps them
# and stays inside the struct.
.PHONY: tables-json-negative-control
tables-json-negative-control: bin/schema test/tables/json_negative_main.cpp
	@rm -rf build/json-sabotage && mkdir -p build/json-sabotage
	./bin/schema generate --lang cpp --out build/json-sabotage tables/examples
	@sed -i.bak 's|const uint8_t \* storage = (const uint8_t \*) base + f->offset;|const uint8_t * storage = (const uint8_t *) base + ( f->offset ^ 4 );|' build/json-sabotage/TablesTable.cpp
	@grep -q 'f->offset ^ 4' build/json-sabotage/TablesTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-sabotage test/tables/json_negative_main.cpp build/json-sabotage/TablesTable.cpp -o build/schema_test_json_negative
	./build/schema_test_json_negative

# The same corpus through the C# table backend (SPEC-TABLES.md, schema#262):
# the tables corpus plus the evolution pair, generated at build time into
# build/ — test-only, never part of the committed generated/ tree. The full
# unit is generated (packet .cs + <Base>Table.cs), because a table's closure
# decodes into the packet emitter's own classes.
build/tables-generated-cs/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-cs
	./bin/schema generate --lang cs --out build/tables-generated-cs/examples tables/examples
	./bin/schema generate --lang cs --out build/tables-generated-cs/block tables/block
	./bin/schema generate --lang cs --out build/tables-generated-cs/v1 test/tables/V1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/v2 test/tables/V2.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/p1 test/tables/P1.schema
	./bin/schema generate --lang cs --out build/tables-generated-cs/p3 test/tables/P3.schema
	@touch $@

# The C# twin of the C++ "no serialize include path" build: a generated
# <Base>Table.cs must stand alone on the BCL, so nothing in it may name the
# serialize runtime. (C# has no include paths to withhold, so the property is
# gated by inspection rather than by the compiler.)
.PHONY: tables-cs-standalone
tables-cs-standalone: build/tables-generated-cs/.stamp
	@n=$$(ls build/tables-generated-cs/*/*Table.cs 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated Table sources, expected 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-cs/*/*Table.cs; do \
		if grep -n "Serialize" $$f; then \
			echo "STANDALONE GATE FAILED: the serialize runtime leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables C# standalone gate: generated Table sources name no runtime"

# The C# VARIABLE-CLASS REFUSAL (SPEC-TABLES.md §2.2): the C# backend emits the
# fixed class, and a pointered unit is refused BY NAME rather than emitted with
# its pointered tables missing.
.PHONY: tables-cs-refuses-pointers
tables-cs-refuses-pointers: bin/schema
	@mkdir -p build
	@if ./bin/schema generate --lang cs --out build/tables-cs-refusal tables/pointers > build/tables-cs-refusal.log 2>&1; then \
		echo "REFUSAL GATE FAILED: the C# backend generated a pointered unit"; exit 1; \
	fi
	@grep -q "variable class is a named follow-on" build/tables-cs-refusal.log || \
		{ echo "REFUSAL GATE FAILED: the refusal does not name the follow-on"; cat build/tables-cs-refusal.log; exit 1; }
	@echo "tables C# refusal gate: a pointered unit is refused by name"

# ---------------------------------------------------------------------------
# The BLOCK FORM (SPEC-TABLES.md §19). Nothing declares it: every fixed table
# has one, emitted on the side into <Base>Block.h/.cpp and <Base>Block.cs, and
# a consumer compiles those only if it uses the form.
# ---------------------------------------------------------------------------

BLOCK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -pthread
BLOCK_INCLUDES := -Ibuild/tables-generated/block
BLOCK_SOURCES = $$(ls build/tables-generated/block/*Block.cpp)

build/schema_test_block: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# The SANITIZED twin (#277's rule). The block form is where a multi-threaded
# fill over disjoint index ranges lives, and a race in that fill is precisely
# what a sanitizer leg exists to find — -Werror cannot see one.
# -fno-sanitize-recover=all is what makes it a GATE.
build/schema_test_block_asan: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# THE TWO-LANGUAGE GATE (SPEC-TABLES.md §19.5, §12.1): a C++ producer writes a
# block and pins its bytes; a C# consumer opens those very bytes and compares
# every field of every row, twice — through the generated blittable struct and
# through the block descriptors. Sizes and offsets are asserted by GENERATED
# code on both sides, so the pair proves the two agree on the BYTES and not
# merely on the constants.
.PHONY: tables-block
tables-block: build/schema_test_block build/schema_test_block_asan build/tables-generated-cs/.stamp
	./build/schema_test_block
	./build/schema_test_block_asan
	cd test/cs-block && dotnet run

# THE BLOCK ZERO-COST GATE (SPEC-TABLES.md §2.2, §19). Nothing declares the
# block form and it lives entirely on the side: a unit's <Base>Table.h and
# <Base>Table.cs must carry NOT ONE SYMBOL of it, so a consumer that never
# reads a block pays nothing — not a type, not a constant, not an include.
.PHONY: tables-block-zero-cost
tables-block-zero-cost: build/tables-generated/.stamp build/tables-generated-cs/.stamp
	@for f in build/tables-generated/*/*Table.h build/tables-generated/*/*Table.cpp \
	          build/tables-generated-cs/*/*Table.cs; do \
		if grep -nE "TableBlock|[A-Za-z0-9_]Block|BuildVersion" $$f; then \
			echo "BLOCK ZERO-COST GATE FAILED: the block form leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: no Table source carries one symbol of the block form"

# THE FILL REFUSER (SPEC-TABLES.md §19.1, §19.5). The multi-threaded fill is an
# OBLIGATION on the implementation, not a permission to the caller: the
# generated fill path — Begin, the array accessors and the row storage they
# hand back — contains no allocation, no lock and no atomic, and the BUILD
# FAILS if one appears. This reads the generated SOURCE between the fill path's
# own markers; test/tables/block_main.cpp watches the RUNNING program with a
# global operator-new counter, and the two together are what the obligation
# stands on.
BLOCK_FORBIDDEN := malloc|calloc|realloc|operator new|[^_a-zA-Z]new |std::mutex|std::lock_guard|std::unique_lock|std::atomic|std::thread|pthread_|__atomic

.PHONY: tables-block-fill-refuser
tables-block-fill-refuser: build/tables-generated/.stamp
	@rm -rf build/block-fill && mkdir -p build/block-fill
	@for f in build/tables-generated/*/*Block.h; do \
		out=build/block-fill/$$(echo $$f | tr / _); \
		awk '/---- block fill path: begin ----/,/---- block fill path: end ----/' $$f > $$out; \
	done
	@n=$$(cat build/block-fill/* | wc -l | tr -d ' '); \
		if [ "$$n" -lt 100 ]; then \
			echo "FILL REFUSER FAILED: found $$n lines of fill path — the markers, not the property, are what broke"; exit 1; \
		fi
	@if grep -nE "$(BLOCK_FORBIDDEN)" build/block-fill/*; then \
		echo "FILL REFUSER FAILED: the generated fill path allocates, locks or takes an atomic (SPEC-TABLES.md §19.1)"; exit 1; \
	fi
	@echo "block fill refuser: the generated fill path allocates nothing, locks nothing, takes no atomic"

# NEGATIVE CONTROL ONE (the brief's (a)): the fill refuser above must be
# CAPABLE of going red. An allocation and a lock are injected into a generated
# fill path, and the gate must refuse. A green gate proves nothing until it has
# been shown able to fail.
.PHONY: tables-block-fill-refuser-negative-control
tables-block-fill-refuser-negative-control: build/tables-generated/.stamp
	@rm -rf build/block-fill-sabotage && mkdir -p build/block-fill-sabotage
	@cp build/tables-generated/block/RenderBlock.h build/block-fill-sabotage/
	@sed -i.bak 's|// ---- block fill path: begin ----|// ---- block fill path: begin ----\n// sabotaged: static std::mutex lock; void * p = malloc( 1 );|' \
		build/block-fill-sabotage/RenderBlock.h
	@grep -q "sabotaged: static std::mutex" build/block-fill-sabotage/RenderBlock.h || \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@awk '/---- block fill path: begin ----/,/---- block fill path: end ----/' \
		build/block-fill-sabotage/RenderBlock.h > build/block-fill-sabotage/region
	@if grep -qE "$(BLOCK_FORBIDDEN)" build/block-fill-sabotage/region; then \
		echo "block fill refuser negative control: the gate goes red on an injected lock and allocation"; \
	else \
		echo "NEGATIVE CONTROL FAILED: the fill refuser did not see an injected lock and allocation"; exit 1; \
	fi

# NEGATIVE CONTROL THREE (the brief's (c)): the C# side's GENERATED PADDING
# FIELDS are load-bearing, not decoration. Delete them from one padded record
# and the generated layout check must go red — every field after the hole lands
# at the wrong offset, which is exactly what §19.3 says Size alone cannot pin.
.PHONY: tables-block-padding-negative-control
tables-block-padding-negative-control: build/tables-generated-cs/.stamp
	@rm -rf build/block-padding-sabotage && cp -R build/tables-generated-cs/block build/block-padding-sabotage
	@sed -i.bak '/private byte _pad/d' build/block-padding-sabotage/PaddedBlock.cs
	@grep -q "private byte _pad" build/block-padding-sabotage/PaddedBlock.cs && \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; } || true
	@rm -f build/block-padding-sabotage/*.bak
	@if ( cd test/cs-block && dotnet run -p:BlockGeneratedDir=../../build/block-padding-sabotage ) \
		> build/block-padding-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# layout check passed with the padding fields deleted"; \
		cat build/block-padding-sabotage.log; exit 1; \
	fi
	@grep -q "schema block layout" build/block-padding-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: the failure was not the layout check"; cat build/block-padding-sabotage.log; exit 1; }
	@echo "block padding negative control: deleting the generated padding fields turns the layout check red"
# The NEGATIVE CONTROL for a KEYED object's duplicate counting
# (SPEC-TABLES.md §16.2). Last-wins inside a keyed object was already true, so
# the missing count was invisible to every round-trip test — the value was
# right and only the ledger was wrong. This sabotage removes the increment and
# the fixture must go red; without it, nothing in the suite could see the
# defect the pack engine's two-sided gate found.
.PHONY: tables-json-keyed-dup-negative-control
tables-json-keyed-dup-negative-control: bin/schema test/tables/json_keyed_dup_negative_main.cpp
	@rm -rf build/json-dup-sabotage && mkdir -p build/json-dup-sabotage
	./bin/schema generate --lang cpp --out build/json-dup-sabotage tables/examples
	@sed -i.bak 's|if ( ( seen\[slot >> 6\] \& bit ) != 0 ) { in.report->duplicate++; }|if ( ( seen[slot >> 6] \& bit ) != 0 ) { /* sabotaged: no count */ }|' build/json-dup-sabotage/KeyedTable.cpp
	@grep -q 'sabotaged: no count' build/json-dup-sabotage/KeyedTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-dup-sabotage test/tables/json_keyed_dup_negative_main.cpp build/json-dup-sabotage/KeyedTable.cpp -o build/schema_test_json_keyed_dup_negative
	./build/schema_test_json_keyed_dup_negative

# The NEGATIVE CONTROL for a keyed array's ITERATION RANGE (SPEC-TABLES.md
# §2.4). The iteration's whole promise is that slot 0 — None's — is not in the
# range, and an untouched slot 0 holds the same declared defaults every other
# untouched slot does, so a walk that visited it would look identical to a walk
# that did not.
#
# It sabotages the EMITTER's begin() to slot 0 and requires THE TABLES SUITE
# ITSELF — the same test/tables/main.cpp the leg runs, against a whole corpus
# regenerated from the sabotaged compiler — to go red. A purpose-written
# fixture would only prove the sabotage is observable by a fixture written for
# it; what has to be shown is that the GUARDED test reddens.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to (the big-endian control's rule).
.PHONY: tables-keyed-iteration-negative-control
tables-keyed-iteration-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|Iterator begin() { return Iterator{ slots, 1 }; }|Iterator begin() { return Iterator{ slots, 0 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 1 }; }|ConstIterator begin() const { return ConstIterator{ slots, 0 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-slot-zero.gotext
	@cmp -s build/cpptable-slot-zero.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-slot-zero.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-slot-zero-overlay.json
	@go build -overlay=build/cpptable-slot-zero-overlay.json -o build/schema-slot-zero ./cmd/schema
	@rm -rf build/tables-slot-zero && mkdir -p build/tables-slot-zero
	$(call tables_generate,./build/schema-slot-zero,build/tables-slot-zero)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-slot-zero) \
		test/tables/main.cpp $$(ls build/tables-slot-zero/*/*Table.cpp) -o build/schema_test_tables_slot_zero
	@if ./build/schema_test_tables_slot_zero > build/tables-slot-zero.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: begin() reaching slot 0 left the tables suite GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/main.cpp" build/tables-slot-zero.log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on a CHECK"; cat build/tables-slot-zero.log; exit 1; }
	@echo "negative control: begin() at slot 0 turns the TABLES SUITE red — $$(grep -c '^FAIL' build/tables-slot-zero.log) failures"

# Deliberately compiled WITHOUT -I$(SERIALIZE): the generated Table headers
# carry no serialize dependency, and this build proves it stays that way.
#
# TABLES_INCLUDES is shared with the sanitized twin below, so the two builds
# can never drift into covering different code.
TABLES_INCLUDES := $(call tables_includes,build/tables-generated)
TABLES_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -pthread

# The text form's runtime is a generated TRANSLATION UNIT now, not header
# content (SPEC-TABLES.md §16.1): a consumer that calls FromJson/ToJson
# compiles the generated <Base>Table.cpp, and one that never does compiles
# nothing for it. Expanded in the recipe because these are build-time output.
TABLES_JSON_SOURCES = $$(ls build/tables-generated/*/*Table.cpp)

build/schema_test_tables: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# The SANITIZED twin (issue #277). The tables leg is where the pointer
# machinery lives — an arena whose Lock() frees it one way, a packed region
# read through self-relative deltas, a cooked file Open validates by walking
# an offset graph, a builder that goes wide across threads — and those are
# lifetime and bounds questions -Werror cannot see. A heap-use-after-free
# lived here with CI green over it, because the assertion it corrupted
# happened to compare equal every time (#276).
#
# -fno-sanitize-recover=all is what makes it a GATE: UBSan's default is to
# print and continue, which exits 0 and proves nothing.
#
# Both builds run, and that is not duplication: ASan replaces the allocator,
# so the two see different addresses and alignments — and this suite refuses
# unaligned bases and misaligned region references by design. Each build
# reaches states the other does not.
build/schema_test_tables_asan: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# ---- the BIG-ENDIAN leg (SPEC-TABLES.md §3 and §7) -------------------------
#
# The wire is little-endian and byte-oriented (§3), and a cook is produced in
# the byte order of the build it is cooked for (§7). Both were rules on a page:
# every host this repo builds on is little-endian, so nothing ever read a
# golden the other way round. This leg builds the tables battery for a
# BIG-ENDIAN target and runs it under an emulator, which turns the two rules
# into gates — the goldens a little-endian host wrote are loaded, re-written
# and byte-compared by a big-endian reader, and a cook that crosses the byte
# order is proven to refuse rather than to garble.
#
# The toolchain is not a system binary and is not assumed: CI installs an
# exact pinned version (.github/workflows/ci.yml) and these two variables name
# what it installed, so the leg runs anywhere the same pair is on PATH.
BE_CXX ?= s390x-linux-gnu-g++
BE_RUN ?= qemu-s390x

# Same flags and includes as the plain leg, shared through the same variables
# for the same reason the sanitized twin shares them (#278): a twin that
# covers different code is not a twin. -static so the runner needs no sysroot
# and the emulator invocation is just the binary.
build/schema_test_tables_be: build/tables-generated/.stamp test/tables/main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# The cross-endian COOK driver, built BOTH ways: the rule it holds — a cook
# does not cross a byte order — needs two builds and a file between them, which
# is the one part of §7 no single-process test can reach.
build/schema_test_cook_endian: build/tables-generated/.stamp test/tables/cook_endian_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/cook_endian_main.cpp -o $@

build/schema_test_cook_endian_be: build/tables-generated/.stamp test/tables/cook_endian_main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/cook_endian_main.cpp -o $@

.PHONY: tables-big-endian
tables-big-endian: build/schema_test_tables_be build/schema_test_cook_endian build/schema_test_cook_endian_be
	$(BE_RUN) ./build/schema_test_tables_be
	./build/schema_test_cook_endian write build/cook-host.bin
	$(BE_RUN) ./build/schema_test_cook_endian_be write build/cook-target.bin
	$(BE_RUN) ./build/schema_test_cook_endian_be accept build/cook-target.bin
	$(BE_RUN) ./build/schema_test_cook_endian_be refuse build/cook-host.bin
	./build/schema_test_cook_endian accept build/cook-host.bin
	./build/schema_test_cook_endian refuse build/cook-target.bin
	@echo "big-endian leg: the wire crosses the byte order, the cook refuses to"

# Its NEGATIVE CONTROL. Put ONE of the wire's byte-order-neutral stores back to
# a host-order copy — put16, which writes every field id, every enum value and
# every union arm id — and the leg must go RED on the big-endian target. The
# same sabotage stays GREEN on this little-endian host, and that pair is the
# whole argument for the leg: this is precisely the defect class no
# little-endian CI can see.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to: an interrupt cannot leave a sabotaged
# working tree, and a parallel `make -j` cannot compile the sabotage into
# something else.
.PHONY: tables-big-endian-negative
tables-big-endian-negative: tables-big-endian
	@mkdir -p build
	@sed 's|void put16( uint16_t v ) { uint8_t b\[2\] = { uint8_t( v ), uint8_t( v >> 8 ) }; raw( b, 2 ); }|void put16( uint16_t v ) { raw( \&v, 2 ); } // SABOTAGED: host order|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-host-order.gotext
	@cmp -s build/cpptable-host-order.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-host-order.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-overlay.json
	@go build -overlay=build/cpptable-overlay.json -o build/schema-host-order ./cmd/schema
	@rm -rf build/tables-host-order && mkdir -p build/tables-host-order
	$(call tables_generate,./build/schema-host-order,build/tables-host-order)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-host-order) \
		test/tables/main.cpp $$(ls build/tables-host-order/*/*Table.cpp) -o build/schema_test_tables_host_order
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(call tables_includes,build/tables-host-order) \
		test/tables/main.cpp $$(ls build/tables-host-order/*/*Table.cpp) -o build/schema_test_tables_host_order_be
	@./build/schema_test_tables_host_order > /dev/null || \
		{ echo "NEGATIVE CONTROL FAILED: a host-order put16 is visible on a little-endian host — this host is not little-endian, or the sabotage is not the one described"; exit 1; }
	@echo "negative control: a host-order put16 leaves the LITTLE-ENDIAN leg green"
	@if $(BE_RUN) ./build/schema_test_tables_host_order_be > build/host-order-be.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a host-order put16 left the BIG-ENDIAN leg green"; exit 1; \
	fi
	@grep -q "table wire golden" build/host-order-be.log || \
		{ echo "NEGATIVE CONTROL FAILED: the big-endian leg went red, but not on a wire golden"; cat build/host-order-be.log; exit 1; }
	@echo "negative control: the same put16 turns the BIG-ENDIAN leg red, on the wire goldens"

# The PACK GOLDEN (SPEC-TABLES.md §17.4, issue #257). `schema pack` carries an
# IR-driven engine in Go — the compiler cannot run the code it emits — so the
# gate that makes the two ONE WIRE is a byte comparison: the bytes pack builds
# from the directory tree at tables/pack/config must equal PackConfigSave of
# the same instance built by hand in C++, and a C++ Load of pack's bytes must
# report nothing at all.
PACK_TREE := $(shell find tables/pack/config tables/pack/root -type f 2>/dev/null)

build/tables-pack.bin: bin/schema tables/examples/Pack.schema $(PACK_TREE)
	@mkdir -p build
	./bin/schema pack --root PackConfig --out $@ tables/pack/config tables/examples

build/tables-pack-root.bin: bin/schema $(PACK_TREE)
	@mkdir -p build
	./bin/schema pack --root RootConfig --out $@ tables/pack/root tables/examples

# PACK_INCLUDES is shared with the sanitized twins below, so a build and its
# twin can never drift into covering different code (#278's rule).
PACK_INCLUDES := -Ibuild/tables-generated/examples
# these drivers CALL the text form, so they compile the generated translation
# unit that holds it (SPEC-TABLES.md §16.1) — the same rule any consumer follows
PACK_JSON_SOURCES = $$(ls build/tables-generated/examples/*Table.cpp)
PACK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -ffp-contract=off
PACK_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/schema_test_pack: build/tables-generated/.stamp test/tables/pack_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_INCLUDES) test/tables/pack_main.cpp $(PACK_JSON_SOURCES) -o $@

# The SANITIZED twins (#278, applied to this PR's drivers). Both read file
# sizes off disk and size their own buffers from them, and both compare
# byte ranges whose lengths came out of a file or a manifest — bounds and
# lifetime questions -Werror cannot see, and exactly the class #278 stood the
# tables leg up against. -fno-sanitize-recover=all is what makes it a GATE.
build/schema_test_pack_asan: build/tables-generated/.stamp test/tables/pack_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_SANITIZE) $(PACK_INCLUDES) test/tables/pack_main.cpp $(PACK_JSON_SOURCES) -o $@

# §17.1's THIRD golden needs the engine's own text of each root, so unpack
# writes the one-file form beside the bins and the driver compares ToJson to it.
build/pack-text/.stamp: bin/schema build/tables-pack.bin build/tables-pack-root.bin
	@rm -rf build/pack-text
	@mkdir -p build/pack-text
	./bin/schema unpack --one-file --root PackConfig --in build/tables-pack.bin build/pack-text tables/examples
	./bin/schema unpack --one-file --root RootConfig --in build/tables-pack-root.bin build/pack-text tables/examples
	@touch $@

.PHONY: tables-pack
tables-pack: build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/pack-text/.stamp
	./build/schema_test_pack build/tables-pack.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json
	./build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json

# The HOSTILE-VALUE gate (SPEC-TABLES.md §16.2, §16.3, §17.5). One tree per rule
# the text form states — malformed number tokens, a value past a bits(N) width,
# a lone surrogate, `null` on a `?T`, a "None" key, duplicate keys — packed by
# the Go engine and then READ BY THE GENERATED BACKEND. The manifest says which
# trees refuse and what report the rest produce; the backend half asserts the
# invariant the report is a promise about, that bytes the engine called clean
# load clean and re-save byte-identically. The Go half lives in
# internal/tablepack's tests and reads the same manifest.
HOSTILE_TREES := $(shell find tables/pack/hostile-values -type f 2>/dev/null)

build/hostile-values/.stamp: bin/schema $(HOSTILE_TREES)
	@rm -rf build/hostile-values
	@mkdir -p build/hostile-values
	@grep -v '^#' tables/pack/hostile-values/cases.txt | grep ' packs ' | \
	while read -r name root outcome counts; do \
		./bin/schema pack --root $$root --out build/hostile-values/$$name.bin --tolerate \
			tables/pack/hostile-values/$$name tables/examples || exit 1; \
	done
	@touch $@

build/schema_test_hostile: build/tables-generated/.stamp test/tables/hostile_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_INCLUDES) test/tables/hostile_main.cpp $(PACK_JSON_SOURCES) -o $@

build/schema_test_hostile_asan: build/tables-generated/.stamp test/tables/hostile_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_SANITIZE) $(PACK_INCLUDES) test/tables/hostile_main.cpp $(PACK_JSON_SOURCES) -o $@

.PHONY: tables-hostile-values
tables-hostile-values: build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp
	./build/schema_test_hostile tables/pack/hostile-values/cases.txt \
		tables/pack/hostile-values build/hostile-values
	./build/schema_test_hostile_asan tables/pack/hostile-values/cases.txt \
		tables/pack/hostile-values build/hostile-values

# Its NEGATIVE CONTROL: relax ONE rule of the number grammar — accept a leading
# `+`, which RFC 8259 does not — and the gate must go red, because a tree the
# manifest says is REFUSED starts packing. Same overlay mechanism as the wire
# negative control: no tracked file is ever written to.
.PHONY: tables-hostile-negative
tables-hostile-negative: tables-hostile-values
	@mkdir -p build
	@sed "s/in.text\[in.pos\] == '-' {/in.text[in.pos] == '-' || in.text[in.pos] == '+' { \/\/ SABOTAGED/" \
		internal/tabletext/read.go > build/read-sabotaged.gotext
	@cmp -s build/read-sabotaged.gotext internal/tabletext/read.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tabletext/read.go":"%s/build/read-sabotaged.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/read-overlay.json
	@go build -overlay=build/read-overlay.json -o build/schema-loose-numbers ./cmd/schema
	@if ./build/schema-loose-numbers pack --root RootConfig --out build/loose.bin --tolerate \
		tables/pack/hostile-values/num-leading-plus tables/examples > /dev/null 2>&1; then \
		echo "pack hostile-value negative control: a leading + packs once the grammar is relaxed"; \
	else \
		echo "NEGATIVE CONTROL FAILED: relaxing the number grammar left num-leading-plus refused"; exit 1; \
	fi
	@./bin/schema pack --root RootConfig --out build/loose.bin --tolerate \
		tables/pack/hostile-values/num-leading-plus tables/examples > /dev/null 2>&1 && \
		{ echo "NEGATIVE CONTROL FAILED: the real engine accepts a leading + too"; exit 1; } || true

# The NEGATIVE CONTROL for that golden: break ONE framing rule in the Go
# encoder — elide a present `?T`, which §2.3 forbids — and the golden must go
# red. A gate that cannot be made to fail is not a gate, so this target runs the
# POSITIVE one first: a red golden must not be able to pass as a successful
# sabotage.
#
# The sabotaged source lives under build/ and reaches the compiler through
# `go build -overlay`, so no tracked file is ever written to: an interrupt in
# the middle of this target cannot leave a sabotaged working tree, and a
# parallel `make -j` cannot compile the sabotage into something else.
.PHONY: tables-pack-negative
tables-pack-negative: tables-pack
	@mkdir -p build
	@sed 's/if !fv\.Present {/if true { \/\/ SABOTAGED: a present ?T elides/' \
		internal/tablewire/encode.go > build/encode-sabotaged.gotext
	@cmp -s build/encode-sabotaged.gotext internal/tablewire/encode.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/encode.go":"%s/build/encode-sabotaged.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/sabotage-overlay.json
	@go build -overlay=build/sabotage-overlay.json -o build/schema-sabotaged ./cmd/schema
	@./build/schema-sabotaged pack --root PackConfig --out build/tables-pack-sabotaged.bin \
		tables/pack/config tables/examples
	@if ./build/schema_test_pack build/tables-pack-sabotaged.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: eliding a present ?T left the golden green"; exit 1; \
	fi
	@echo "pack negative control: eliding a present ?T turns the golden red"

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

test: build/schema_test build/schema_test_guard build/schema_test_tables build/schema_test_block build/schema_test_block_asan build/pack-text/.stamp build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/schema_test_tables_asan build/tables-generated-cs/.stamp build/schema_test_random build/schema_test_ludicrous build/schema_test_c build/schema_test_c_ludicrous build/schema_test_bench build/schema_test_bench_c generated/go/.stamp generated/rust/.stamp generated/cs/.stamp generated/js/.stamp generated/dart/.stamp generated/java/.stamp generated/elixir/.stamp generated/go-ludicrous/.stamp generated/rust-ludicrous/.stamp generated/cs-ludicrous/.stamp generated/js-ludicrous/.stamp generated/dart-ludicrous/.stamp generated/java-ludicrous/.stamp generated/elixir-ludicrous/.stamp generated/bench/go/.stamp generated/bench/rust/.stamp generated/bench/cs/.stamp generated/bench/js/.stamp generated/bench/dart/.stamp generated/bench/java/.stamp generated/bench/elixir/.stamp build/java-test/.stamp build/java-test-ludicrous/.stamp build/java-bench/.stamp
	./build/schema_test
	./build/schema_test_guard
	./build/schema_test_tables
	./build/schema_test_tables_asan
	$(MAKE) tables-zero-cost
	$(MAKE) tables-json-walk
	$(MAKE) tables-json-negative-control
	$(MAKE) tables-cs-standalone
	$(MAKE) tables-cs-refuses-pointers
	cd test/cs-tables && dotnet run
	$(MAKE) tables-json-keyed-dup-negative-control
	$(MAKE) tables-keyed-iteration-negative-control
	$(MAKE) tables-block
	$(MAKE) tables-block-zero-cost
	$(MAKE) tables-block-fill-refuser
	$(MAKE) tables-block-fill-refuser-negative-control
	$(MAKE) tables-block-padding-negative-control
	$(MAKE) tables-pack
	$(MAKE) tables-pack-negative
	$(MAKE) tables-hostile-values
	$(MAKE) tables-hostile-negative
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
update-goldens: build/schema_test build/schema_test_ludicrous build/schema_test_bench build/schema_test_tables build/schema_test_block
	@mkdir -p testdata/golden testdata/wire testdata/wire/tables
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_tables
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_block
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
	./bin/schema check tables/block
	./bin/schema check test/tables/V1.schema
	./bin/schema check test/tables/V2.schema
	./bin/schema check test/tables/P1.schema
	./bin/schema check test/tables/P2.schema
	./bin/schema check test/tables/P3.schema
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
	./bin/schema fmt tables/block
	./bin/schema fmt test/tables/V1.schema
	./bin/schema fmt test/tables/V2.schema
	./bin/schema fmt test/tables/P1.schema
	./bin/schema fmt test/tables/P2.schema
	./bin/schema fmt test/tables/P3.schema
	./bin/schema fmt bench/corpus/Bench.schema
	./bin/schema fmt bench/corpus/RealWorld.schema

# The one-benchmark rule, made mechanical: no hand-coded measurement of a
# schema shape anywhere in this repo except what bench/SHAPE-GATE.allow names.
shape-gate:
	go run ./bench/tools/shapegate

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench bench-variants generated-current shape-gate
