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

# cmd, internal, AND the public API packages: ir/ and compiler/ are compiled
# into bin/schema like any other source, and leaving them out made an edit to
# the layout model or the build version silently NOT rebuild the compiler every
# gate below then measures.
GO_SOURCES   := $(shell find cmd internal ir compiler -name '*.go') go.mod
SCHEMAS      := $(wildcard examples/*.schema)
SCHEMAS128   := $(wildcard examples128/*.schema)
SCHEMAS_BENCH := $(wildcard bench/corpus/*.schema)
SCHEMAS_TABLES := $(wildcard tables/examples/*.schema)
SCHEMAS_TABLES_POINTERS := $(wildcard tables/pointers/*.schema)
SCHEMAS_TABLES_BLOCK := $(wildcard tables/block/*.schema) $(wildcard tables/blockhome/*.schema)

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

# The tables corpus (docs/SPEC-TABLES.md): the tabledemo unit plus the
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
	$(1) generate --lang cpp --out $(2)/blockhome tables/blockhome
	$(1) generate --lang cpp --out $(2)/v1 test/tables/V1.schema
	$(1) generate --lang cpp --out $(2)/v2 test/tables/V2.schema
	$(1) generate --lang cpp --out $(2)/p1 test/tables/P1.schema
	$(1) generate --lang cpp --out $(2)/p2 test/tables/P2.schema
	$(1) generate --lang cpp --out $(2)/p3 test/tables/P3.schema
	$(1) generate --lang cpp --out $(2)/jsonkeys test/tables/JsonKeys.schema
endef

tables_includes = -I$(1)/examples -I$(1)/pointers -I$(1)/block -I$(1)/blockhome -Itest/tables \
	-I$(1)/v1 -I$(1)/v2 -I$(1)/p1 -I$(1)/p2 -I$(1)/p3 -I$(1)/jsonkeys

build/tables-generated/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema
	@mkdir -p build/tables-generated
	$(call tables_generate,./bin/schema,build/tables-generated)
	@touch $@

# The ZERO-COST GATE (docs/SPEC-TABLES.md): a table with no pointer in its by-value
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
		if grep -nE "TableArena|TableSlot|TableWorker|TableRef|TableRegion|kTableSegment|kTableSlab|kTableMaxDepth|is_pointer|Builder|PackMeasure|LoadMeasure" $$f; then \
			echo "ZERO-COST GATE FAILED: pointer machinery leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables zero-cost gate: value-only tables carry no pointer machinery"

# The GENERIC-WALK GATE (docs/SPEC-TABLES.md §16): the text form is ONE walk over
# the reflection descriptors, not a per-table codec — that is the property
# which makes it schema's rather than a packer's. The walker's source must
# therefore be the SAME BYTES in every generated .cpp of the corpus, whose
# units disagree about packages, tables, kinds and pointer modes. The package
# name lives in the guard and the namespace, outside the markers, so this is a
# strict byte comparison with nothing normalised away. (It moved from the
# headers to the .cpp files with the walker itself — docs/SPEC-TABLES.md §16.1.)
# THE RUST GENERIC-WALK GATE (docs/SPEC-TABLES.md §16), the Rust twin of the
# one above: the text form is ONE walk over the reflection descriptors, and
# the Rust backend emits it as ONE MODULE PER UNIT. Its bytes must therefore
# not vary with what a unit declares — the units below disagree about
# packages, tables, kinds, keyed arrays and pointer modes — so this is a strict
# byte comparison of table_runtime.rs across the whole corpus, with nothing
# normalised away except the generated banner, which names the schema file.
.PHONY: tables-rust-walk
tables-rust-walk: build/tables-generated-rust/.stamp
	@rm -rf build/rust-walk && mkdir -p build/rust-walk
	@for f in build/tables-generated-rust/*/src/table_runtime.rs; do \
		out=build/rust-walk/$$(echo $$f | tr / _); \
		tail -n +6 $$f > $$out; \
		if [ ! -s $$out ]; then echo "RUST GENERIC-WALK GATE FAILED: no runtime in $$f"; exit 1; fi; \
	done
	@first=""; for f in build/rust-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "RUST GENERIC-WALK GATE FAILED: the runtime in $$f is not the runtime in $$first"; exit 1; }; \
		fi; \
	done
	@echo "rust generic-walk gate: one table runtime, the same bytes in every unit"

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

# The NEGATIVE CONTROL for the walk (docs/SPEC-TABLES.md §16.5). A green round-trip
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

# The GENERIC-WALK GATE, C# side (docs/SPEC-TABLES.md §16). The same property
# the C++ gate above holds, over the shape C# forces: a unit's files compile
# into ONE assembly, so the walk is emitted ONCE PER UNIT — into the file that
# already carries the unit's shared table runtime — rather than once per
# translation unit behind a guard. So the gate asserts two things: exactly one
# file per unit directory carries a walker, and every walker in the corpus is
# the same bytes. The package name never enters the markers, so nothing is
# normalised away here either.
.PHONY: tables-cs-json-walk
tables-cs-json-walk: build/tables-generated-cs/.stamp
	@rm -rf build/json-walk-cs && mkdir -p build/json-walk-cs
	@for d in build/tables-generated-cs/*/; do \
		unit=$$(basename $$d); n=0; \
		for f in $$d*Table.cs; do \
			[ -e "$$f" ] || continue; \
			out=build/json-walk-cs/$$unit.$$(basename $$f); \
			awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
			if [ -s $$out ]; then n=$$((n+1)); else rm -f $$out; fi; \
		done; \
		if [ -n "$$(ls $$d*Table.cs 2>/dev/null)" ] && [ $$n -ne 1 ]; then \
			echo "GENERIC-WALK GATE FAILED: unit $$unit carries $$n walkers, not one"; exit 1; \
		fi; \
	done
	@if [ -z "$$(ls build/json-walk-cs 2>/dev/null)" ]; then \
		echo "GENERIC-WALK GATE FAILED: no walker in any generated .cs"; exit 1; fi
	@first=""; for f in build/json-walk-cs/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables C# generic-walk gate: one walker per unit, byte-identical across $$(ls build/json-walk-cs | wc -l | tr -d ' ') units"

# The same corpus through the C# table backend (docs/SPEC-TABLES.md, schema#262):
# the tables corpus plus the evolution pair, generated at build time into
# build/ — test-only, never part of the committed generated/ tree. The full
# unit is generated (packet .cs + <Base>Table.cs), because a table's closure
# decodes into the packet emitter's own classes.
# THE GO TABLE LEG's generated packages. Each unit is its own MODULE, because
# a generated package names its schema's `package` and Go resolves an import by
# module path — so the conformance leg's go.mod replaces one path per unit,
# exactly as test/go/go.mod already does for the packet corpus.
build/tables-generated-go/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-go
	./bin/schema generate --lang go --out build/tables-generated-go/examples tables/examples
	# the POINTERED unit: its Go WIRE surface is refused by name (§11) and its
	# two ACCELERATORS are emitted all the same, because neither needs a codec
	# (§7, §19). This is where the cook's Go read side comes from.
	./bin/schema generate --lang go --out build/tables-generated-go/pointers tables/pointers
	./bin/schema generate --lang go --out build/tables-generated-go/block tables/block
	./bin/schema generate --lang go --out build/tables-generated-go/blockhome tables/blockhome
	./bin/schema generate --lang go --out build/tables-generated-go/v1 test/tables/V1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/v2 test/tables/V2.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p1 test/tables/P1.schema
	./bin/schema generate --lang go --out build/tables-generated-go/p3 test/tables/P3.schema
	$(call go_table_module,examples,tabledemo)
	$(call go_table_module,pointers,graphdemo)
	$(call go_table_module,block,blockdemo)
	$(call go_table_module,blockhome,blockhomedemo)
	$(call go_table_module,v1,tblv1)
	$(call go_table_module,v2,tblv2)
	$(call go_table_module,p1,tblp1)
	$(call go_table_module,p3,tblp3)
	@touch $@

# one generated unit's module wiring (build wiring, not schema output — the
# emitter writes only .go files)
define go_table_module
@printf 'module %s\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => ../../../$(SERIALIZE_GO)\n' $(2) > build/tables-generated-go/$(1)/go.mod
endef

build/tables-generated-cs/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-cs
	./bin/schema generate --lang cs --out build/tables-generated-cs/examples tables/examples
	# the POINTERED unit: its C# WIRE surface is refused by name (§11) and its
	# two ACCELERATORS are emitted all the same, because neither needs a codec
	# (§7, §19). This is where the cook's C# read side comes from.
	./bin/schema generate --lang cs --out build/tables-generated-cs/pointers tables/pointers
	./bin/schema generate --lang cs --out build/tables-generated-cs/block tables/block
	./bin/schema generate --lang cs --out build/tables-generated-cs/blockhome tables/blockhome
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

# The C# VARIABLE-CLASS REFUSAL (docs/SPEC-TABLES.md §2.2, §11), and it is a refusal
# of the WIRE SURFACE — which is the half the variable class is missing: the
# arena, the builder, the region and the node-table codec. The two ACCELERATORS
# need none of that, so a pointered unit's block (§19) and cook (§7) sources are
# emitted and its <Base>Table.cs is not.
#
# NAMED, NEVER SILENT is the property this gate holds: no Table source at all,
# and every emitted source of the unit opening with a banner that names each
# refused table and the follow-on. A consumer reaching for Save or Load gets a
# missing name from its own compiler, beside a file that says why.
.PHONY: tables-cs-refuses-pointers
tables-cs-refuses-pointers: bin/schema
	@rm -rf build/tables-cs-refusal && mkdir -p build
	./bin/schema generate --lang cs --out build/tables-cs-refusal tables/pointers
	@if ls build/tables-cs-refusal/*Table.cs >/dev/null 2>&1; then \
		echo "REFUSAL GATE FAILED: the C# backend emitted a wire surface for a pointered unit"; exit 1; \
	fi
	@for f in build/tables-cs-refusal/*Cook.cs build/tables-cs-refusal/*Block.cs; do \
		grep -q "THE C# WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not carry the refusal banner"; exit 1; }; \
		grep -q "is a named follow-on" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name the follow-on"; exit 1; }; \
		grep -q "Album, Depot, Layer, ListNode, Marker, Scene and TreeNode" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name every refused table"; exit 1; }; \
	done
	@n=$$(ls build/tables-cs-refusal/*Cook.cs | wc -l | tr -d ' '); \
		if [ "$$n" -lt 3 ]; then \
			echo "REFUSAL GATE FAILED: found $$n Cook sources for the pointered unit, expected 3 — the glob, not the property, is what broke"; exit 1; \
		fi
	@echo "tables C# refusal gate: a pointered unit's WIRE half is refused by name, in every source it does emit, and its cooks still open"

# ---------------------------------------------------------------------------
# The BLOCK FORM (docs/SPEC-TABLES.md §19). Nothing declares it: every fixed table
# has one, emitted on the side into <Base>Block.h/.cpp and <Base>Block.cs, and
# a consumer compiles those only if it uses the form.
# ---------------------------------------------------------------------------

# -Wshadow, on all three tables legs (BLOCK here, TABLES and PACK below): the
# POSIX gate for a class the POSIX legs were blind to. A shadowed name in an
# emitted function is a warning nobody sees under -Wall -Wextra — neither gcc
# nor clang says a word without this flag — and cl refuses it outright at
# /W4 /WX, so the estate's Visual C++ requirement turned a silent POSIX green
# into a Windows red (#286).
#
# The two POSIX compilers cover DIFFERENT halves of what cl refuses, and the
# matrix runs both, so the pair is the gate rather than either one:
#   clang -Wshadow  -> a local hiding a local          (cl C4456)
#   gcc   -Wshadow  -> the same, PLUS a constructor parameter hiding a member
#                      (cl C4458), which clang files under -Wshadow-all
# macOS runs clang and ubuntu runs gcc; the big-endian leg is a second gcc.
BLOCK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
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

# The THREADSANITIZER twin. §19.5 says the multi-threaded fill runs "under the
# sanitizer leg, where a race in the fill is what the leg exists to find" — and
# ASan is not that leg: it finds no races, and byte-identity between a serial
# fill and a DETERMINISTIC four-worker fill finds none either. This is the leg
# that can. It costs under a second, so it rides beside the other two rather
# than waiting for a nightly.
build/schema_test_block_tsan: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=thread -fno-sanitize-recover=all -g \
		$(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# Its NEGATIVE CONTROL, because a green TSan run otherwise proves only that the
# leg ran: --race has every worker write ONE row, after a start barrier, many
# times, and the leg must go red on the write-write race that creates.
#
# Overlapping RANGES would be a race too, and that is what this control did at
# first — but whether a sanitizer observes one then depends on how the machine
# happened to schedule four threads over seven thousand rows, and it passed on
# this bench and failed on CI's. Contending on one address from threads that
# start together is the same defect made certain, which is what a control has
# to be.
.PHONY: tables-block-race-negative-control
tables-block-race-negative-control: build/schema_test_block_tsan
	@if ./build/schema_test_block_tsan --race > build/block-race.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: ThreadSanitizer did not see overlapping workers writing one row"; exit 1; \
	fi
	@grep -q "ThreadSanitizer: data race" build/block-race.log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on a data race"; tail -20 build/block-race.log; exit 1; }
	@echo "block race negative control: overlapping workers turn the ThreadSanitizer leg red"

# THE TWO-LANGUAGE GATE (docs/SPEC-TABLES.md §19.5, §12.1): a C++ producer writes a
# block and pins its bytes; a C# consumer opens those very bytes and compares
# every field of every row, twice — through the generated blittable struct and
# through the block descriptors. Sizes and offsets are asserted by GENERATED
# code on both sides, so the pair proves the two agree on the BYTES and not
# merely on the constants.
.PHONY: tables-block
tables-block: build/schema_test_block build/schema_test_block_asan build/schema_test_block_tsan build/tables-generated-cs/.stamp
	./build/schema_test_block
	./build/schema_test_block_asan
	./build/schema_test_block_tsan
	cd test/cs-block && dotnet run

# ---------------------------------------------------------------------------
# THE FORGERY FUZZER (docs/SPEC-TABLES.md §19.2, §19.5). The hand-written battery in
# block_main.cpp and Program.cs is eleven forgeries, one per fact BlockOpen
# checks plus the one this fuzzer found. This is the standing gate beside it:
# valid blocks from the generated builder, mutated, and one oracle over every
# mutant — REFUSE, or OPEN and be WHOLE.
#
# The mutators are seeded and reproducible: every mutant is a pure function of
# ( seed, unit, count vector, pass, index ), and a failing case prints the one
# command that re-runs it alone. Override for a long local run:
#
#   make tables-block-fuzz N=5000000 SEED=1234
#
# N is the RANDOM budget per unit, spread over its count vectors; the
# enumerated passes — every named slot x every width x every boundary value,
# every truncation length, every unaligned base — run whatever N is set to,
# because they are what actually cover the boundaries and they cost nothing.
# ---------------------------------------------------------------------------

# N is chosen against the measurement that matters, which is CI's and not this
# bench's: ubuntu-latest runs the sanitized twin about 3.4x slower than arm64
# clang here, and the sanitized twin is the whole cost. At N=200000 the target
# measured 41.6 s there against a 60 s budget, which meets the bar with too
# little left for a busy runner; at N=100000 it is about 25 s, half the budget.
#
#   leg (at N=100000)                    here (arm64)   ubuntu-latest
#   schema_test_block_fuzz                     1.4 s        ~ 1.8 s
#   schema_test_block_fuzz_asan                6.0 s        ~ 16 s
#   test/cs-block -- --fuzz                    2.2 s        ~ 3 s
#
# The ENUMERATED passes are what cover the boundaries and they run whatever N
# is; N buys random mutants on top, and a long run is what the override is for.
N ?= 100000
SEED ?= 24845619678

BLOCK_FUZZ_INCLUDES := -Ibuild/tables-generated/block -Ibuild/tables-generated/blockhome
BLOCK_FUZZ_SOURCES = $$(ls build/tables-generated/block/*Block.cpp build/tables-generated/blockhome/*Block.cpp)

build/schema_test_block_fuzz: build/tables-generated/.stamp test/tables/block_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_FUZZ_INCLUDES) test/tables/block_fuzz_main.cpp $(BLOCK_FUZZ_SOURCES) -o $@

# The SANITIZED twin (#277's rule), and here it is the point rather than a
# companion: the oracle proves a mutant that OPENED stays inside the extent,
# and only the sanitizer can prove that a mutant BlockOpen refused read nothing
# outside it on the way to refusing. The region is allocated at exactly the
# bytes the caller claims, so the redzone begins at the extent's last byte.
build/schema_test_block_fuzz_asan: build/tables-generated/.stamp test/tables/block_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_FUZZ_INCLUDES) test/tables/block_fuzz_main.cpp \
		$(BLOCK_FUZZ_SOURCES) -o $@

# The seed blocks: one valid block per unit per count vector, written by the
# generated C++ BUILDER, because C# has only the read half of the form.
build/block-fuzz/.stamp: build/schema_test_block_fuzz
	@rm -rf build/block-fuzz && mkdir -p build/block-fuzz
	./build/schema_test_block_fuzz --dump build/block-fuzz
	@touch $@

.PHONY: tables-block-fuzz
tables-block-fuzz: build/schema_test_block_fuzz build/schema_test_block_fuzz_asan build/block-fuzz/.stamp build/tables-generated-cs/.stamp
	SEED=$(SEED) N=$(N) ./build/schema_test_block_fuzz
	SEED=$(SEED) N=$(N) ./build/schema_test_block_fuzz_asan
	cd test/cs-block && SEED=$(SEED) N=$(N) dotnet run -- --fuzz

# THE COOK FIXTURES the Rust fuzzer's cook half forges, written by test/cookgen
# with the same chains the conformance harness uses — the fixture generator's
# own facts, which the conformance data deliberately does not carry.
build/cook-fuzz/.stamp: $(wildcard test/cookgen/*.go) $(SCHEMAS_TABLES_POINTERS)
	@rm -rf build/cook-fuzz && mkdir -p build/cook-fuzz
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene    --ref head --chain ListNode --next next --out build/cook-fuzz/Scene.cook
	./build/cookgen --bytes 4096 --root Depot    --ref head --chain ListNode --next next --out build/cook-fuzz/Depot.cook
	./build/cookgen --bytes 4096 --root Album    --ref head --chain ListNode --next next --out build/cook-fuzz/Album.cook
	./build/cookgen --bytes 4096 --root TreeNode --ref left --chain TreeNode --next left --out build/cook-fuzz/TreeNode.cook
	./build/cookgen --bytes 4096 --root ListNode --ref next --chain ListNode --next next --out build/cook-fuzz/ListNode.cook
	@touch $@

# THE RUST FORGERY FUZZER (docs/SPEC-TABLES.md §19.5, §7): the block half over
# the C++ leg's seed blocks, and a COOK half over test/cookgen's fixtures. It
# runs TWICE, for the reason the C++ leg runs twice:
#
#   - the ORDINARY build proves a mutant that OPENED stays inside the extent,
#     because the oracle re-derives every bound and reads every row;
#   - the MIRI build proves a mutant that Open REFUSED read nothing outside
#     that extent on the way to refusing, which no oracle can prove from
#     inside. Every region is allocated at exactly the bytes the caller claims,
#     so the byte after it is off the end of a real allocation and Miri stops
#     there. It is this leg's address sanitizer, and it is the reason the
#     region is allocated to the CLAIM rather than to the file.
#
# Miri INTERPRETS, so it runs the ENUMERATED passes — every slot x width x
# boundary value, every truncation, every unaligned base, which are what cover
# the boundaries — with a token random budget on top (MIRI_N) and only over the
# SMALL count vectors (MIRI_MAX_SEED_BYTES). The cap is not a concession: what
# Miri is there to prove is that a REFUSED mutant read nothing outside the
# extent on the way to refusing, which is a property of Open and of the
# projection, and every count vector carries both. The oracle's per-byte row
# walk over the 7.5 MiB vector would cost hours and cover no check the small
# vectors do not — and the native leg runs it in full.
#
# It needs the nightly toolchain and the miri component:
# `rustup toolchain install nightly && rustup +nightly component add miri`.
#
# IT IS A BY-HAND GATE, like tables-cook-scale-1gb: 51,940 mutants at the
# defaults measured 478 s on arm64 macOS, which is not a per-push cost. What
# rides every push is the NATIVE leg (tables-rust-fuzz), 409,746 mutants in
# 4.5 s at N=100000.
MIRI_N ?= 8
MIRI_MAX_SEED_BYTES ?= 4224

# THE GENERATED RUST BUILDS UNDER A CONSUMER'S CLIPPY. A consumer who runs
# clippy over their own crate runs it over the generated modules too, and a
# DEFAULT-DENY lint there would fail their build for something they did not
# write. This runs plain `cargo clippy` over every generated unit of the
# corpus, which is exactly that question: it exits non-zero on a denied lint
# and zero on a warning.
#
# It is deliberately NOT `-D warnings`. A clippy release that adds a new
# warning must not turn this red — that is version drift breaking a gate, the
# thing the estate's pins exist to prevent — and a warning breaks no
# consumer's build. What a denied lint does is exactly what this catches.
# THE ACCELERATOR FEATURES (docs/SPEC-TABLES.md §19). §19's rule is that the
# block form costs nothing unless you reach for it — in C++ by not including
# the header, and here by not enabling the cargo feature. Both are ON by
# default, so a consumer that says nothing gets the whole surface and the
# saving is opt-in.
#
# The gate is that all four combinations BUILD: a wire-only consumer, a cook
# consumer, a block consumer, and everything. It is not a formality — the
# first cut of it found two real couplings, the unit's BUILD VERSION and its
# blittable RECORDS, each of which belongs to neither accelerator and had been
# sitting inside one of them.
.PHONY: tables-rust-features
tables-rust-features: build/tables-generated-rust/.stamp
	@for unit in build/tables-generated-rust/*/; do \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet ) || exit 1; \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --no-default-features ) || exit 1; \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --no-default-features --features cook ) || exit 1; \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --no-default-features --features block ) || exit 1; \
	done
	@echo "rust feature gate: wire-only, cook-only, block-only and everything all build"

.PHONY: tables-rust-clippy
tables-rust-clippy: build/tables-generated-rust/.stamp
	@for unit in build/tables-generated-rust/*/; do \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo clippy --quiet ) || exit 1; \
	done
	@echo "rust clippy gate: every generated unit builds under a consumer's clippy"

# THE RUST TABLE SURFACE ON A BIG-ENDIAN TARGET (docs/SPEC-TABLES.md §7.1,
# §19.1, §20.3). The pinned toolchain cross-compiles to s390x — the same
# big-endian target the C++ leg uses — so this is a CHECK of the whole
# generated surface for that target, and it needs no linker and no emulator.
#
# WHAT IT PROVES, which is more than a compile: every cooked record's and every
# block projection's LAYOUT CONTRACT is a const assert over size_of and
# offset_of, so `cargo check` EVALUATES it for s390x. A record whose C ABI
# layout differed on a big-endian target — a padding rule, an alignment, a
# bool's width — would fail here rather than in a file nobody could read.
#
# WHAT IT DOES NOT PROVE, named rather than implied: that the wire's bytes
# cross the order at RUN time. That needs the cross linker and qemu the C++
# leg's job installs, and it is the next step rather than this one — a
# cross-and-emulate leg's first run belongs in a change that can iterate on it,
# not in a port branch that cannot exercise it locally. The code is
# order-neutral by construction (to_le_bytes / from_le_bytes everywhere, and
# the two accelerators carry cfg!(target_endian) order words that refuse a
# foreign file), so what the runtime leg would add is proof rather than
# suspicion.
RUST_BE_TARGET ?= s390x-unknown-linux-gnu

.PHONY: tables-rust-big-endian
tables-rust-big-endian: build/tables-generated-rust/.stamp
	@if ! PATH="$(RUSTUP_BIN):$$PATH" rustup target list --installed 2>/dev/null | grep -qx "$(RUST_BE_TARGET)"; then \
		echo "SKIP tables-rust-big-endian: $(RUST_BE_TARGET) is not installed"; \
		echo "  rustup target add $(RUST_BE_TARGET)"; \
		exit 0; \
	fi; \
	cd test/conformance/rust && PATH="$(RUSTUP_BIN):$$PATH" \
		cargo check --quiet --target $(RUST_BE_TARGET) && \
		echo "big-endian: the generated Rust table surface checks for $(RUST_BE_TARGET), every layout const assert with it"

# THE RUST SOAK (docs/SPEC-TABLES.md: "every read path allocates nothing").
# Every instance of the conformance corpus, wire-loaded and re-saved and
# text-read and text-written, in a loop, with the bytes compared every
# iteration — so a run that drifted stops rather than merely getting slower —
# and with LIVE ALLOCATED BYTES as the instrument.
#
# The instrument is the point. RSS answers a different question, and an
# allocator that grew by one byte per iteration would take a very long time to
# show up in it; the driver installs a counting global allocator, takes a
# baseline after a warm pass, and REFUSES the run if the number moved.
#
# It is a by-hand gate at an hour. SOAK_SECONDS shortens it for a working
# check; the number the port was landed on is 3600.
SOAK_SECONDS ?= 3600

# It reads the DERIVED manifest the harness writes, so `conformance` is the
# dependency: a soak over a corpus whose matrix is red would be timing a
# defect.
.PHONY: tables-rust-soak
tables-rust-soak: conformance
	./build/conformance-rust build/conformance/manifest.txt soak $(SOAK_SECONDS)

# THE ALLOCATION AUDIT: every instance of the corpus, every generated read and
# write path, counted at the global allocator with this driver's own buffers
# hoisted out of the measured region — so what the number measures is the
# CODEC. The claim it holds is docs/USAGE.md's: the generated code allocates
# nothing beyond the value and the buffers the caller passed.
.PHONY: tables-rust-alloc-audit
tables-rust-alloc-audit: conformance
	./build/conformance-rust build/conformance/manifest.txt alloc-audit

# ITS NEGATIVE CONTROL, and the soak's. A gate that has never fired proves
# nothing, and the LIVE-BYTE gate could not fire on this class at all: live
# bytes answer "does this leak", and a path that allocates and frees the same
# bytes every iteration reads +0 there forever — which is exactly how 74
# allocations per ToJson sat under a green soak. SOAK_SABOTAGE puts ONE
# allocation per iteration inside the measured region and both gates must go
# red on it.
.PHONY: tables-rust-alloc-negative-control
tables-rust-alloc-negative-control: conformance
	@if SOAK_SABOTAGE=1 ./build/conformance-rust build/conformance/manifest.txt alloc-audit \
			> build/rust-alloc-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the allocation audit stayed green with one allocation per iteration"; \
		exit 1; \
	fi
	@grep -q "allocates on a read or write path" build/rust-alloc-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the audit went red, but not on the allocation"; \
		  cat build/rust-alloc-control.log; exit 1; }
	@if SOAK_SABOTAGE=1 ./build/conformance-rust build/conformance/manifest.txt soak 5 \
			> build/rust-soak-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the soak stayed green with one allocation per iteration"; \
		exit 1; \
	fi
	@grep -q "allocation(s) on the read and write paths" build/rust-soak-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak went red, but not on the allocation"; \
		  cat build/rust-soak-control.log; exit 1; }
	@grep -m1 "allocation(s) on the read and write paths" build/rust-soak-control.log
	@echo "rust allocation negative control: one allocation per iteration turns BOTH gates red"

.PHONY: tables-rust-fuzz
tables-rust-fuzz: build/block-fuzz/.stamp build/cook-fuzz/.stamp build/tables-generated-rust/.stamp
	cd test/rust-fuzz && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	SEED=$(SEED) N=$(N) ./test/rust-fuzz/target/debug/rust-fuzz

.PHONY: tables-rust-fuzz-miri
tables-rust-fuzz-miri: build/block-fuzz/.stamp build/cook-fuzz/.stamp build/tables-generated-rust/.stamp
	cd test/rust-fuzz && PATH="$(RUSTUP_BIN):$$PATH" \
		MIRIFLAGS="-Zmiri-disable-isolation" SEED=$(SEED) N=$(MIRI_N) \
		MAX_SEED_BYTES=$(MIRI_MAX_SEED_BYTES) \
		BLOCK_SEEDS="$(CURDIR)/build/block-fuzz" COOK_FIXTURES="$(CURDIR)/build/cook-fuzz" \
		cargo +nightly miri run --quiet

# The NEGATIVE CONTROLS. A fuzzer that has never gone red proves nothing about
# the checks it is watching, so each of these REMOVES one of BlockOpen's checks
# from the EMITTER — from both backends at once — and requires the fuzzer to
# find it. The emitter is replaced through `go build -overlay`, so no tracked
# file is edited and the sabotage cannot survive the target that made it.
#
# The gate they hold is the ORACLE's independence: it re-derives every bound
# from the descriptors and from the triples in the instance, so it can disagree
# with BlockOpen. An oracle that shared BlockOpen's arithmetic would stay green
# through both of these.

# The sed program that removes each check from each emitter. They live in
# variables rather than in the $(call) below because a sed address RANGE
# carries a comma, and make would read it as another argument.
BLOCK_FUZZ_SED_CPP_extent := /rows > (uint64_t) bytes - offset_of/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_CS_extent := /rows > (ulong) bytes - offsetOf/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_CPP_maximum := /count > (uint64_t) %sBlock/,/overflow on a count the maximum does not bound/d
BLOCK_FUZZ_SED_CS_maximum := /count > (ulong) %sMax/d
BLOCK_FUZZ_SED_RUST_extent := /rows > bytes as u64 - offset_of/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_RUST_maximum := /count > Self::%s_MAX as u64/,/on a count the maximum does not bound/d

# how a sabotage is built: $(1) is its name, and the two sed programs above
# named by it are what come out of each emitter. The replacement files take a
# .go.txt suffix on purpose: `go build -overlay` does not care what a
# replacement is called, and `go test ./...` walks build/ and would otherwise
# find two packages sitting in one directory.
define block_fuzz_sabotage
	@rm -rf build/block-fuzz-$(1) && mkdir -p build/block-fuzz-$(1)
	@sed '$(BLOCK_FUZZ_SED_CPP_$(1))' internal/codegen/cpptable/block.go > build/block-fuzz-$(1)/cpptable-block.go.txt
	@sed '$(BLOCK_FUZZ_SED_CS_$(1))' internal/codegen/cstable/block.go > build/block-fuzz-$(1)/cstable-block.go.txt
	@sed '$(BLOCK_FUZZ_SED_RUST_$(1))' internal/codegen/rusttable/block.go > build/block-fuzz-$(1)/rusttable-block.go.txt
	@cmp -s internal/codegen/cpptable/block.go build/block-fuzz-$(1)/cpptable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the C++ emitter sabotage did not apply"; exit 1; } || true
	@cmp -s internal/codegen/cstable/block.go build/block-fuzz-$(1)/cstable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the C# emitter sabotage did not apply"; exit 1; } || true
	@cmp -s internal/codegen/rusttable/block.go build/block-fuzz-$(1)/rusttable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Rust emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/block.go":"%s/build/block-fuzz-$(1)/cpptable-block.go.txt","%s/internal/codegen/cstable/block.go":"%s/build/block-fuzz-$(1)/cstable-block.go.txt","%s/internal/codegen/rusttable/block.go":"%s/build/block-fuzz-$(1)/rusttable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > build/block-fuzz-$(1)/overlay.json
	go build -overlay build/block-fuzz-$(1)/overlay.json -o build/block-fuzz-$(1)/schema ./cmd/schema
	@rm -rf build/block-fuzz-$(1)/generated
	./build/block-fuzz-$(1)/schema generate --lang cpp --out build/block-fuzz-$(1)/generated/block tables/block
	./build/block-fuzz-$(1)/schema generate --lang cpp --out build/block-fuzz-$(1)/generated/blockhome tables/blockhome
	./build/block-fuzz-$(1)/schema generate --lang cs --out build/block-fuzz-$(1)/generated/block-cs tables/block
	./build/block-fuzz-$(1)/schema generate --lang cs --out build/block-fuzz-$(1)/generated/blockhome-cs tables/blockhome
	$(CXX) $(BLOCK_CXXFLAGS) -Ibuild/block-fuzz-$(1)/generated/block -Ibuild/block-fuzz-$(1)/generated/blockhome \
		test/tables/block_fuzz_main.cpp \
		$$(ls build/block-fuzz-$(1)/generated/block/*Block.cpp build/block-fuzz-$(1)/generated/blockhome/*Block.cpp) \
		-o build/block-fuzz-$(1)/fuzz
	@if SEED=$(SEED) N=$(N) ./build/block-fuzz-$(1)/fuzz > build/block-fuzz-$(1)/cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C++ fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/cpp.log || \
		{ echo "NEGATIVE CONTROL FAILED: the C++ leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/cpp.log; exit 1; }
	@if ( cd test/cs-block && SEED=$(SEED) N=$(N) dotnet run \
			-p:BlockGeneratedDir=../../build/block-fuzz-$(1)/generated/block-cs \
			-p:BlockHomeGeneratedDir=../../build/block-fuzz-$(1)/generated/blockhome-cs -- --fuzz ) \
			> build/block-fuzz-$(1)/cs.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/cs.log || \
		{ echo "NEGATIVE CONTROL FAILED: the C# leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/cs.log; exit 1; }
	@rm -f build/tables-generated-rust/.stamp
	./build/block-fuzz-$(1)/schema generate --lang rust --out build/tables-generated-rust/blockdemo/src tables/block
	./build/block-fuzz-$(1)/schema generate --lang rust --out build/tables-generated-rust/blockhome/src tables/blockhome
	cd test/rust-fuzz && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	@if SEED=$(SEED) N=$(N) ./test/rust-fuzz/target/debug/rust-fuzz > build/block-fuzz-$(1)/rust.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the Rust fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/rust.log || \
		{ echo "NEGATIVE CONTROL FAILED: the Rust leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/rust.log; exit 1; }
	@grep -m1 "^FAILED" build/block-fuzz-$(1)/cpp.log
	@grep -m1 "  mutation" build/block-fuzz-$(1)/cpp.log
	@echo "block fuzz $(1) negative control: removing that check from EVERY emitter turns the fuzzer red on every backend"
endef

# The EXTENT check: an array's rows must end inside the bytes the caller
# passed, and the used extent it reports must too. Both spellings go, because
# either one alone still refuses what the other would have.
.PHONY: tables-block-fuzz-extent-negative-control
tables-block-fuzz-extent-negative-control: build/block-fuzz/.stamp build/tables-generated-cs/.stamp build/tables-generated-rust/.stamp build/cook-fuzz/.stamp
	$(call block_fuzz_sabotage,extent)

# THE GO FUZZER, and its two negative controls on the same rule as the C++
# ones: it is the standing gate over the Go accelerators' Open, and a fuzzer
# that has never gone red proves nothing about the checks it is watching.
#
# It runs UNDER -race as well as plain, because a block and a cook are memory
# another language wrote and the Go readers point into it with `unsafe`: the
# race detector is what says the pointing itself is sound, and Go's bounds
# checks — which are on in every configuration — are what the oracle's own walk
# reads through.
.PHONY: tables-go-fuzz
tables-go-fuzz: build/tables-generated-go/.stamp build/conformance-harness
	./build/conformance-harness run --drivers /dev/null --work build/conformance > /dev/null 2>&1 || true
	cd test/go-tables && SEED=$(SEED) N=$(N) go test -run Fuzz -count 1 .
	cd test/go-tables && SEED=$(SEED) N=$(N) go test -race -run Fuzz -count 1 .

# The sed programs that remove one check from the GO emitter, and the sabotage
# that proves the fuzzer finds it. Same shape as the C++/C# pair above, through
# `go build -overlay`, so no tracked file is edited.
# The sabotage is a SUBSTITUTION and not a deletion, and the reason is that a
# deleted check takes its variables with it: removing the rows test leaves
# `rows` declared and unused, and the generated Go then does not compile, which
# proves nothing. Turning the condition into `false` removes exactly the check
# and nothing else.
GO_FUZZ_SED_extent := s|if rows > uint64(bytes)-offsetOf {|if false {|; s|if padding > bytes-used {|if false {|
GO_FUZZ_SED_maximum := s|if count > %d {|if false \&\& count > %d {|

# what the fuzzer must say when it goes red, so a control that turned some
# OTHER leg red is not mistaken for a pass
GO_FUZZ_EXPECT_extent := leave an extent|used bytes inside an extent
GO_FUZZ_EXPECT_maximum := past the declared maximum

define go_fuzz_sabotage
	@rm -rf build/go-fuzz-$(1) && mkdir -p build/go-fuzz-$(1)
	@sed '$(GO_FUZZ_SED_$(1))' internal/codegen/gotable/block.go > build/go-fuzz-$(1)/gotable-block.go.txt
	@cmp -s internal/codegen/gotable/block.go build/go-fuzz-$(1)/gotable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Go emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/gotable/block.go":"%s/build/go-fuzz-$(1)/gotable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/go-fuzz-$(1)/overlay.json
	go build -overlay build/go-fuzz-$(1)/overlay.json -o build/go-fuzz-$(1)/schema ./cmd/schema
	@rm -rf build/go-fuzz-$(1)/generated
	./build/go-fuzz-$(1)/schema generate --lang go --out build/go-fuzz-$(1)/generated/block tables/block
	./build/go-fuzz-$(1)/schema generate --lang go --out build/go-fuzz-$(1)/generated/pointers tables/pointers
	@printf 'module blockdemo\n\ngo 1.23\n' > build/go-fuzz-$(1)/generated/block/go.mod
	@printf 'module graphdemo\n\ngo 1.23\n' > build/go-fuzz-$(1)/generated/pointers/go.mod
	@sed -e 's|=> ../../build/tables-generated-go/block|=> $(CURDIR)/build/go-fuzz-$(1)/generated/block|' \
	     -e 's|=> ../../build/tables-generated-go/pointers|=> $(CURDIR)/build/go-fuzz-$(1)/generated/pointers|' \
	     test/go-tables/go.mod > build/go-fuzz-$(1)/go.mod.txt
	@printf '{"Replace":{"%s/test/go-tables/go.mod":"%s/build/go-fuzz-$(1)/go.mod.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/go-fuzz-$(1)/modoverlay.json
	@if ( cd test/go-tables && SEED=$(SEED) N=$(N) go test -overlay ../../build/go-fuzz-$(1)/modoverlay.json \
			-run BlockForgeryFuzz -count 1 . ) > build/go-fuzz-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the Go fuzzer stayed green with the $(1) check removed from the emitter"; \
		cat build/go-fuzz-$(1)/log; exit 1; \
	fi
	@grep -q "fuzz_test.go" build/go-fuzz-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the Go leg went red, but not on the oracle"; cat build/go-fuzz-$(1)/log; exit 1; }
	@grep -qE "$(GO_FUZZ_EXPECT_$(1))" build/go-fuzz-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the oracle went red on some other check, not the $(1) one"; \
		  cat build/go-fuzz-$(1)/log; exit 1; }
	@grep -m1 "fuzz_test.go" build/go-fuzz-$(1)/log
	@echo "go fuzz $(1) negative control: removing that check from the Go emitter turns the fuzzer red"
endef

.PHONY: tables-go-fuzz-extent-negative-control
tables-go-fuzz-extent-negative-control: build/tables-generated-go/.stamp
	$(call go_fuzz_sabotage,extent)

.PHONY: tables-go-fuzz-maximum-negative-control
tables-go-fuzz-maximum-negative-control: build/tables-generated-go/.stamp
	$(call go_fuzz_sabotage,maximum)

# The DECLARED MAXIMUM check: a count past the maximum Begin refuses on the
# producer side. This is §19.2's tenth forgery, the one a reader found OPEN,
# and the control is what keeps it closed.
.PHONY: tables-block-fuzz-maximum-negative-control
tables-block-fuzz-maximum-negative-control: build/block-fuzz/.stamp build/tables-generated-cs/.stamp build/tables-generated-rust/.stamp build/cook-fuzz/.stamp
	$(call block_fuzz_sabotage,maximum)

# THE COOK'S NEGATIVE CONTROL (docs/SPEC-TABLES.md §7.5). The hostile battery in
# internal/tablecook is a Go test and rides `go test ./...`; what a battery
# cannot prove about itself is that it would go RED, so this removes PASS ONE
# — `cook-check`'s directory scan — and requires it to.
#
# The sabotage is one inserted `return nil` at the top of checkDirectory, which
# is unambiguous in a way a deleted range is not, and it reaches the build
# through `go test -overlay`, so no tracked file is edited and the sabotage
# cannot survive the target that made it.
.PHONY: tables-cook-fuzz-negative-control
tables-cook-fuzz-negative-control:
	@rm -rf build/cook-fuzz-control && mkdir -p build/cook-fuzz-control
	@sed 's|^func checkDirectory(m \*tabletext.Model, h Header, dir \[\]DirectoryEntry) error {$$|&\n\treturn nil // NEGATIVE CONTROL|' \
		internal/tablecook/check.go > build/cook-fuzz-control/check.go.txt
	@cmp -s internal/tablecook/check.go build/cook-fuzz-control/check.go.txt && \
		{ echo "NEGATIVE CONTROL: the cook-check sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablecook/check.go":"%s/build/cook-fuzz-control/check.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-fuzz-control/overlay.json
	@if SEED=$(SEED) N=$(N) go test -count=1 -overlay=build/cook-fuzz-control/overlay.json \
			-run 'TestCookCheck' ./internal/tablecook/ > build/cook-fuzz-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cook battery stayed green with the directory scan removed"; \
		exit 1; \
	fi
	@grep -q "FAILED" build/cook-fuzz-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on the bar"; cat build/cook-fuzz-control/log; exit 1; }
	@grep -m1 "FAILED" build/cook-fuzz-control/log

# THE RUST NAME-CLAIM NEGATIVE CONTROL (docs/SPEC-TABLES.md §11). The claim
# that a schema declaration may not lower onto one of the table runtime's Rust
# CONSTANTS is a refusal, and a refusal that has never fired proves nothing —
# so this removes it from internal/check and requires the suite to go red.
#
# It is the control that the whole class needed and did not have: the first
# version of the registry scan was C#'s regex verbatim, blind to lowercase and
# to SCREAMING_SNAKE, so forty-four crate items went unregistered and three
# spellings of every runtime constant stayed legal. A green test over a blind
# scan is what let that happen.
#
# The sabotage reaches the build through `go test -overlay`, so no tracked file
# is edited and it cannot survive the target that made it.
.PHONY: tables-rust-names-negative-control
tables-rust-names-negative-control:
	@rm -rf build/rust-names-control && mkdir -p build/rust-names-control
	@sed 's|^\t\tfor _, gen := range tablenames.RustConstants() {$$|\t\tfor _, gen := range []string{} { _ = gen; // NEGATIVE CONTROL|' \
		internal/check/check.go > build/rust-names-control/check.go.txt
	@cmp -s internal/check/check.go build/rust-names-control/check.go.txt && \
		{ echo "NEGATIVE CONTROL: the name-claim sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/check/check.go":"%s/build/rust-names-control/check.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/rust-names-control/overlay.json
	@if go test -count=1 -overlay=build/rust-names-control/overlay.json \
			-run 'TestRustConstantSpaceIsClaimedForEveryRuntimeConstant|TestTableRefusals' \
			./internal/check/ > build/rust-names-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the suite stayed green with the Rust constant-space claim removed"; \
		exit 1; \
	fi
	@grep -q "was accepted beside a table" build/rust-names-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on the claim"; \
		  cat build/rust-names-control/log; exit 1; }
	@grep -m1 "was accepted beside a table" build/rust-names-control/log
	@echo "rust name-claim negative control: removing the mapped-space claim turns the suite RED on it"

# THE SCALE FIXTURES (docs/SPEC-TABLES.md §7.5). `test/cookgen` writes a synthetic
# region streaming, in O(1) memory, so the OPEN-COST gate the emitter owes —
# open time flat across 1 MB, 100 MB and 1 GB — has inputs the C++ worker can
# regenerate rather than a gigabyte in the tree.
#
# CI runs the first two, under the two-minute rule. THE GIGABYTE IS RUN BY
# HAND (`make tables-cook-scale-1gb`): it writes a 1.6 GB file, which is not
# something a CI runner's disk should meet on every push. What runs here is
# the TOOL's own scan, which is O(R + P log N) and not the O(1) the runtime
# owes; this target's job is to produce the fixtures and prove they check.
COOKGEN_UNIT := tables/pointers
.PHONY: tables-cook-scale
tables-cook-scale: bin/schema
	@mkdir -p build/cook
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1048576   --out build/cook/1mb.cook
	./build/cookgen --bytes 1048576   --byte-order big --out build/cook/1mb-be.cook
	./build/cookgen --bytes 104857600 --out build/cook/100mb.cook
	./bin/schema cook-check --verbose build/cook/1mb.cook    $(COOKGEN_UNIT)
	./bin/schema cook-check --verbose build/cook/1mb-be.cook $(COOKGEN_UNIT)
	./bin/schema cook-check --verbose build/cook/100mb.cook  $(COOKGEN_UNIT)

.PHONY: tables-cook-scale-1gb
tables-cook-scale-1gb: bin/schema
	@mkdir -p build/cook
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --out build/cook/1gb.cook
	./bin/schema cook-check --verbose build/cook/1gb.cook $(COOKGEN_UNIT)

# ---- the COOK's C++ READ SIDE (docs/SPEC-TABLES.md §7) --------------------------
#
# `schema cook` writes the file and the generated <Root>Open points at it, and
# the two were written from the page independently — the tool in Go, the C++
# side from §7 alone, neither reading the other. That is what makes the golden
# mode a CROSS-IMPLEMENTATION gate rather than one implementation agreeing with
# itself: every node the C++ side reaches through its own derefs must be a node
# the cook's ATTRIBUTION part names, at that offset, with that type id, and the
# two sets must be equal.
#
# THE FIXTURES come from test/cookgen, which streams a synthetic region in O(1)
# memory (§7.5), so the 100 MB arm of the O(1) gate is regenerated rather than
# committed. Five roots cover the shapes a region has: a pointer chain, a tree,
# a keyed array of variable tables beside an optional, a cross-file graph
# through a by-value variable table, and a bare chain node as its own root.
COOK_ROOTS := Scene Depot Album TreeNode ListNode
# The FIXED roots. A fixed table's cook is ONE REGION OF ONE NODE (§7) and the
# same header match, and it is where the VALUE crossing lives: a fixed table has
# no pointer, so it has no node table and no kind 17, so this backend's wire and
# the tool's are the same bytes — the C++ side writes a known instance, the tool
# cooks it, and the C++ side reads the values back out of the tool's layout.
# Settings is POINTED AT; Stamp is pointed at by nothing and is declared in a
# file with no variable table of its own, which is the shape a file-scoped
# emission rule forgets.
COOK_FIXED_ROOTS := Settings Stamp

COOK_INCLUDES := -Ibuild/tables-generated/pointers -Itest/tables
COOK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
# The SANITIZED twin is a gate and not a nicety here: Open is handed a hostile
# buffer by construction, the driver allocates each fixture at EXACTLY the
# length it claims so a redzone sits on the next byte, and
# -fno-sanitize-recover=all is what stops UBSan printing and continuing.
COOK_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/schema_test_cook: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(CXX) $(COOK_CXXFLAGS) $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

build/schema_test_cook_asan: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(CXX) $(COOK_CXXFLAGS) $(COOK_SANITIZE) $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

# The fixtures the C++ gate opens. Small ones per root for the golden lock, the
# battery and the fuzzer; a big-endian one for the byte-order leg; and the two
# scale arms for the O(1) gate. `schema cook-check` runs over every small one
# first, so a fixture the TOOL itself would refuse can never be what a green
# C++ run is standing on.
build/cook-open/.stamp: bin/schema $(SCHEMAS_TABLES_POINTERS)
	@rm -rf build/cook-open && mkdir -p build/cook-open
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene    --ref head --chain ListNode --next next --out build/cook-open/Scene.cook
	./build/cookgen --bytes 3000 --root Depot    --ref head --chain ListNode --next next --out build/cook-open/Depot.cook
	./build/cookgen --bytes 3000 --root Album    --ref head --chain ListNode --next next --out build/cook-open/Album.cook
	./build/cookgen --bytes 3000 --root TreeNode --ref left --chain TreeNode --next left --out build/cook-open/TreeNode.cook
	./build/cookgen --bytes 3000 --root ListNode --ref next --chain ListNode --next next --out build/cook-open/ListNode.cook
	./build/cookgen --bytes 4096 --root Scene    --byte-order big --out build/cook-open/Scene-be.cook
	./build/cookgen --bytes 1048576   --root Scene --out build/cook-open/1mb.cook
	./build/cookgen --bytes 104857600 --root Scene --out build/cook-open/100mb.cook
	@for r in $(COOK_ROOTS); do \
		./bin/schema cook-check --root $$r build/cook-open/$$r.cook tables/pointers || exit 1; \
	done
	./bin/schema cook-check --root Scene build/cook-open/Scene-be.cook tables/pointers
	@touch $@

# The FIXED-root fixtures, which the C++ side WRITES: it saves a known instance
# to the tolerant wire, the tool cooks that wire, and the value gate reads the
# instance back out of the cook. The tool's own read report must be SILENT over
# a wire this backend wrote — a crossing in the other direction, for free.
build/cook-open-fixed/.stamp: bin/schema build/schema_test_cook build/cook-open/.stamp
	@rm -rf build/cook-open-fixed && mkdir -p build/cook-open-fixed
	@for r in $(COOK_FIXED_ROOTS); do \
		./build/schema_test_cook write $$r build/cook-open-fixed/$$r.bin || exit 1; \
		./bin/schema cook --root $$r --in build/cook-open-fixed/$$r.bin \
			--out build/cook-open-fixed/$$r.cook --verbose tables/pointers || exit 1; \
		./bin/schema cook-check --root $$r build/cook-open-fixed/$$r.cook tables/pointers || exit 1; \
	done
	@touch $@

.PHONY: tables-cook-open tables-cook-valued
tables-cook-open: build/schema_test_cook build/schema_test_cook_asan build/cook-open/.stamp build/cook-open-fixed/.stamp
	@for r in $(COOK_ROOTS); do \
		./build/schema_test_cook      golden $$r build/cook-open/$$r.cook || exit 1; \
		./build/schema_test_cook_asan golden $$r build/cook-open/$$r.cook || exit 1; \
	done
	@for r in $(COOK_FIXED_ROOTS); do \
		./build/schema_test_cook      golden      $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan golden      $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook      fixedvalues $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan fixedvalues $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan forge       $$r build/cook-open-fixed/$$r.cook || exit 1; \
	done
	./build/schema_test_cook      usage Scene build/cook-open/Scene.cook
	./build/schema_test_cook      forge Scene build/cook-open/Scene.cook
	./build/schema_test_cook_asan forge Scene build/cook-open/Scene.cook
	./build/schema_test_cook_asan forge Depot build/cook-open/Depot.cook
	SEED=$(SEED) N=$(N) ./build/schema_test_cook_asan fuzz Scene build/cook-open/Scene.cook
	SEED=$(SEED) N=$(N) ./build/schema_test_cook_asan fuzz TreeNode build/cook-open/TreeNode.cook
	./build/schema_test_cook accept Scene build/cook-open/Scene.cook
	./build/schema_test_cook refuse Scene build/cook-open/Scene-be.cook
	./build/schema_test_cook time Scene build/cook-open/1mb.cook build/cook-open/100mb.cook

# THE VALUED FIXTURE, cross-checked by the ENGINE's own uncook (§7.5, §7.4).
#
# `test/cookgen --values` fills every non-pointer leaf, so the conformance
# harness's `SceneValued` dump locks what a reader READS out of a node and not
# only where the node is. That dump is pinned from the C++ leg and byte-compared
# by the C# one; this is the THIRD reader, and it is the Go engine's:
#
#   cookgen --values  ->  uncook  ->  cook   must land on the same bytes
#
# `uncook` walks the region and recovers the root's wire, `cook` lays a region
# out from that wire, and the two regions being byte-identical is every value
# surviving a round trip through an implementation that did not write the
# fixture. A value dropped, widened or misplaced by either half moves a byte
# here — and `cook-check` first, so a fixture the tool itself would refuse can
# never be what a green run is standing on.
.PHONY: tables-cook-valued
tables-cook-valued: bin/schema
	@rm -rf build/cook-valued && mkdir -p build/cook-valued
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene --ref head --chain ListNode --next next --values \
		--out build/cook-valued/SceneValued.cook
	./bin/schema cook-check --root Scene --verbose build/cook-valued/SceneValued.cook tables/pointers
	./bin/schema uncook --root Scene --in build/cook-valued/SceneValued.cook \
		--out build/cook-valued/Scene.wire --verbose tables/pointers
	./bin/schema cook --root Scene --in build/cook-valued/Scene.wire \
		--out build/cook-valued/SceneAgain.cook --verbose tables/pointers
	@cmp build/cook-valued/SceneValued.cook build/cook-valued/SceneAgain.cook || \
		{ echo "the engine's uncook did not recover every value the fixture carries"; exit 1; }
	@echo "valued cook: cookgen --values -> uncook -> cook is byte-identical, so every value survives the engine's own read"

# THE GIGABYTE ARM, by hand (§7.5): a 1.5 GB fixture is not something a CI
# runner's disk should meet on every push, and the two arms above already span
# two orders of magnitude.
.PHONY: tables-cook-open-1gb
tables-cook-open-1gb: build/schema_test_cook build/cook-open/.stamp
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --root Scene --out build/cook-open/1gb.cook
	./build/schema_test_cook time Scene build/cook-open/1mb.cook build/cook-open/1gb.cook

# THE NEGATIVE CONTROLS, and a battery whose battery has never gone red is
# watching nothing. Each removes ONE clause of TableCookOpen through a
# `go build -overlay` on the emitter — so no tracked file is ever written to,
# an interrupt cannot leave a sabotaged working tree, and a parallel `make -j`
# cannot compile the sabotage into something else — regenerates the corpus from
# the sabotaged emitter, and requires the C++ gate to go RED.
#
# The two clauses chosen are the two that decide whether Open can hand back
# storage the caller never gave it: the part-length equation, which is what
# refuses a truncated file, and the root-fits check, which is what refuses a
# data part too short to hold the root.
define cook_open_sabotage
	@mkdir -p build
	@rm -rf build/cook-open-$(1) && mkdir -p build/cook-open-$(1)
	@sed 's|^.*$(2).*$$|    $(3) // NEGATIVE CONTROL|' \
		internal/codegen/cpptable/cook.go > build/cook-open-$(1)/cook.gotext
	@cmp -s build/cook-open-$(1)/cook.gotext internal/codegen/cpptable/cook.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cook.go":"%s/build/cook-open-$(1)/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-$(1)/overlay.json
	@go build -overlay=build/cook-open-$(1)/overlay.json -o build/cook-open-$(1)/schema ./cmd/schema
	./build/cook-open-$(1)/schema generate --lang cpp --out build/cook-open-$(1)/gen tables/pointers
	$(CXX) $(COOK_CXXFLAGS) $(COOK_SANITIZE) -Ibuild/cook-open-$(1)/gen -Itest/tables \
		test/tables/cook_main.cpp -o build/cook-open-$(1)/test
	@if ./build/cook-open-$(1)/test forge Scene build/cook-open/Scene.cook > build/cook-open-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the battery stayed green with the $(1) check removed"; exit 1; \
	fi
	@grep -q "FAILED" build/cook-open-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on a forgery"; cat build/cook-open-$(1)/log; exit 1; }
	@grep -m1 -A3 "FAILED" build/cook-open-$(1)/log
	@echo "negative control: removing the $(1) check turns the cook forgery battery RED"
endef

# The sabotage keeps the local it defeats IN USE, because a generated C++ file
# with an unused local does not compile under -Werror and a control that fails
# to build is not a control that went red.
.PHONY: tables-cook-open-lengths-negative-control
tables-cook-open-lengths-negative-control: build/cook-open/.stamp
	$(call cook_open_sabotage,lengths,attribution_length != length - data_offset - data_length,if ( attribution_length == UINT64_MAX ) { return NULL; })

.PHONY: tables-cook-open-root-negative-control
tables-cook-open-root-negative-control: build/cook-open/.stamp
	$(call cook_open_sabotage,root,data_length < root_size,if ( root_size == UINT64_MAX ) { return NULL; })


# ---------------------------------------------------------------------------
# THE COOK's C# READ SIDE (docs/SPEC-TABLES.md §7) --------------------------------
#
# The third implementation of one page: `schema cook` writes the file in Go,
# the C++ <Root>Open points at it, and the C# <Root>Cook.Open points at the very
# same bytes — and none of the three was written from either of the others. Two
# gates come out of that and neither could exist with one implementation:
#
#   THE DIRECTORY LOCK, which the C++ leg also holds: every node the C# side
#   reaches through its OWN derefs is a node the cook's ATTRIBUTION part names,
#   at that offset, with that type id, and the two SETS are equal.
#
#   THE DUMP, which is new here and is what the directory lock cannot do: both
#   readers write their walk out as canonical text — one line per leaf, with the
#   value read at that offset — and the two files are BYTE-COMPARED. A record
#   laid out one byte differently INSIDE a node moves no node offset and no
#   directory entry, so the lock above cannot see it and this can.
#
# The fixtures are the C++ leg's own, deliberately: the same files, opened by
# two runtimes, is what makes the comparison mean anything.
#
# COOK_CS_N is this leg's fuzz budget, set by MEASUREMENT rather than by
# inheritance: the C# fuzzer runs without a sanitizer under it, so a mutant
# costs ~11.6 us here against the C++ leg's ASan-slowed pass, and two roots at a
# million mutants each is ~23 s — inside the 60 s this gate is allowed and ten
# times the shared N. `make ... N=<n>` still overrides it.
COOK_CS_N ?= 1000000
COOK_CS := cd test/cs-cook && dotnet run --no-build --

.PHONY: build-cs-cook
build-cs-cook: build/tables-generated-cs/.stamp
	cd test/cs-cook && dotnet build -v q --nologo

.PHONY: tables-cook-open-cs
tables-cook-open-cs: build-cs-cook build/schema_test_cook build/cook-open/.stamp build/cook-open-fixed/.stamp
	# §20.3's C# half, at START-UP rather than at the first open: every cooked
	# record's size and every field's offset against the compiler's own model
	$(COOK_CS) layout
	@for r in $(COOK_ROOTS) $(COOK_FIXED_ROOTS); do \
		d=build/cook-open; \
		case " $(COOK_FIXED_ROOTS) " in *" $$r "*) d=build/cook-open-fixed;; esac; \
		( $(COOK_CS) golden $$r ../../$$d/$$r.cook ) || exit 1; \
		./build/schema_test_cook dump $$r $$d/$$r.cook > build/$$r.cpp.dump || exit 1; \
		( $(COOK_CS) dump $$r ../../$$d/$$r.cook ) > build/$$r.cs.dump || exit 1; \
		cmp build/$$r.cpp.dump build/$$r.cs.dump || \
			{ echo "CROSS-IMPLEMENTATION DUMP FAILED: the C++ and C# walks of $$r differ"; exit 1; }; \
		echo "cook dump lock: $$r — the C++ and C# walks are byte-identical ($$(wc -l < build/$$r.cs.dump | tr -d ' ') lines)"; \
	done
	@for r in $(COOK_FIXED_ROOTS); do \
		( $(COOK_CS) fixedvalues $$r ../../build/cook-open-fixed/$$r.cook ) || exit 1; \
		( $(COOK_CS) forge $$r ../../build/cook-open-fixed/$$r.cook ) || exit 1; \
	done
	$(COOK_CS) usage Scene ../../build/cook-open/Scene.cook
	$(COOK_CS) forge Scene ../../build/cook-open/Scene.cook
	$(COOK_CS) forge Depot ../../build/cook-open/Depot.cook
	cd test/cs-cook && SEED=$(SEED) N=$(if $(filter-out 100000,$(N)),$(N),$(COOK_CS_N)) dotnet run --no-build -- fuzz Scene ../../build/cook-open/Scene.cook
	cd test/cs-cook && SEED=$(SEED) N=$(if $(filter-out 100000,$(N)),$(N),$(COOK_CS_N)) dotnet run --no-build -- fuzz TreeNode ../../build/cook-open/TreeNode.cook
	$(COOK_CS) accept Scene ../../build/cook-open/Scene.cook
	# THE BYTE-ORDER LEG's C# half, and it is HALF: a cook written --byte-order
	# big is refused by the MAGIC here, which is the refusal the page promises.
	# The other half — a big-endian consumer opening a big-endian cook natively —
	# is UNPROVEN in C# and stays so until a big-endian .NET exists; the C++ leg
	# proves it on s390x, and the page says which half each leg holds (§7.5).
	$(COOK_CS) refuse Scene ../../build/cook-open/Scene-be.cook
	$(COOK_CS) time Scene ../../build/cook-open/1mb.cook ../../build/cook-open/100mb.cook

# THE GIGABYTE ARM, by hand (§7.5).
.PHONY: tables-cook-open-cs-1gb
tables-cook-open-cs-1gb: build-cs-cook build/cook-open/.stamp
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --root Scene --out build/cook-open/1gb.cook
	$(COOK_CS) time Scene ../../build/cook-open/1mb.cook ../../build/cook-open/1gb.cook

# THE NEGATIVE CONTROLS for the C# Open, and a battery that has never gone red
# is watching nothing. Each removes ONE clause of the generated check through a
# `go build -overlay` on the emitter — so no tracked file is ever written to,
# an interrupt cannot leave a sabotaged working tree, and a parallel `make -j`
# cannot compile the sabotage into something else — regenerates the unit from
# the sabotaged emitter, and requires the C# battery to go RED.
#
# The two clauses are the C++ leg's two, for the same reason: they are the two
# that decide whether Open can hand back storage the caller never gave it.
#
# The sabotage keeps every local it defeats IN USE, because a generated C# file
# with an unused local does not compile under TreatWarningsAsErrors and a
# control that fails to BUILD is not a control that went red.
define cook_open_cs_sabotage
	@rm -rf build/cook-open-cs-$(1) && mkdir -p build/cook-open-cs-$(1)
	@sed 's|^.*$(2).*$$|	g.hf("        $(3)\\n")|' \
		internal/codegen/cstable/cook.go > build/cook-open-cs-$(1)/cook.gotext
	@cmp -s build/cook-open-cs-$(1)/cook.gotext internal/codegen/cstable/cook.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/cook.go":"%s/build/cook-open-cs-$(1)/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-cs-$(1)/overlay.json
	@go build -overlay=build/cook-open-cs-$(1)/overlay.json -o build/cook-open-cs-$(1)/schema ./cmd/schema
	./build/cook-open-cs-$(1)/schema generate --lang cs --out build/cook-open-cs-$(1)/gen tables/pointers
	@if ( cd test/cs-cook && dotnet build -v q --nologo \
			-p:CookGeneratedDir=../../build/cook-open-cs-$(1)/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-$(1)/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-$(1)/obj/ \
			> ../../build/cook-open-cs-$(1)/build.log 2>&1 ); then :; else \
		echo "NEGATIVE CONTROL FAILED: the sabotaged emitter's output does not compile"; \
		cat build/cook-open-cs-$(1)/build.log; exit 1; \
	fi
	@if ( cd test/cs-cook && dotnet run --no-build \
			-p:CookGeneratedDir=../../build/cook-open-cs-$(1)/gen \
			-p:BaseOutputPath=../../build/cook-open-cs-$(1)/bin/ \
			-p:BaseIntermediateOutputPath=../../build/cook-open-cs-$(1)/obj/ \
			-- forge Scene ../../build/cook-open/Scene.cook > ../../build/cook-open-cs-$(1)/log 2>&1 ); then \
		echo "NEGATIVE CONTROL FAILED: the battery stayed green with the $(1) check removed"; exit 1; \
	fi
	@grep -q "FAILED" build/cook-open-cs-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on a forgery"; cat build/cook-open-cs-$(1)/log; exit 1; }
	@grep -m1 -A2 "FAILED" build/cook-open-cs-$(1)/log
	@echo "negative control: removing the $(1) check turns the C# cook forgery battery RED"
endef

.PHONY: tables-cook-open-cs-lengths-negative-control
tables-cook-open-cs-lengths-negative-control: build/cook-open/.stamp
	$(call cook_open_cs_sabotage,lengths,dataOffset + dataLength + attribution != bytes,if (dataOffset == ulong.MaxValue \&\& attribution == ulong.MaxValue) { return false; } // NEGATIVE CONTROL)

.PHONY: tables-cook-open-cs-root-negative-control
tables-cook-open-cs-root-negative-control: build/cook-open/.stamp
	$(call cook_open_cs_sabotage,root,if (dataLength < %d) { return false; },if (dataLength == ulong.MaxValue) { return false; } // NEGATIVE CONTROL)


# THE COOK ROUND TRIP THROUGH THE CLI (docs/SPEC-TABLES.md §7.5). The Go tests hold
# the engine; this holds the three COMMANDS and their flags, over a pinned pack
# tree, in both byte orders and with the attribution written both ways.
.PHONY: tables-cook-cli
tables-cook-cli: bin/schema
	@rm -rf build/cook-cli && mkdir -p build/cook-cli
	./bin/schema pack --root PackConfig --out build/cook-cli/orig.bin tables/pack/pinned/PackConfig tables/examples
	./bin/schema cook --root PackConfig --in tables/pack/pinned/PackConfig --out build/cook-cli/tree.cook --verbose tables/examples
	./bin/schema cook-check --root PackConfig --verbose build/cook-cli/tree.cook tables/examples
	./bin/schema uncook --root PackConfig --in build/cook-cli/tree.cook --out build/cook-cli/tree.bin tables/examples
	cmp build/cook-cli/orig.bin build/cook-cli/tree.bin
	./bin/schema cook --root PackConfig --in build/cook-cli/orig.bin --out build/cook-cli/be.cook \
		--byte-order big --attribution build/cook-cli/be.attrib --verbose tables/examples
	@if ./bin/schema cook-check build/cook-cli/be.cook tables/examples 2>/dev/null; then \
		echo "FAILED: a cook carrying data alone was checked anyway"; exit 1; \
	fi
	./bin/schema cook-check --root PackConfig --attribution build/cook-cli/be.attrib --verbose build/cook-cli/be.cook tables/examples
	./bin/schema uncook --root PackConfig --in build/cook-cli/be.cook --attribution build/cook-cli/be.attrib \
		--out build/cook-cli/be.bin tables/examples
	cmp build/cook-cli/orig.bin build/cook-cli/be.bin

# THE BLOCK ZERO-COST GATE (docs/SPEC-TABLES.md §2.2, §19), in its two halves.
#
# The first asks "did any block symbol leak into a Table source?" — a grep.
# The second is the property §19 actually states: **the Table sources are
# BYTE-IDENTICAL with or without the Block files existing**, held by comparing
# 38 of them against frozen pins under testdata/golden/tables/.
#
# They are ordinary goldens: `make update-goldens` re-pins them when a TABLE
# emitter legitimately changes, and a move under an unchanged emitter is
# stop-the-line, exactly as it is for every other golden. What a frozen pin
# cannot do is survive a legitimate Table-emitter change without being
# re-pinned; schema#331 carries the follow-on that would replace it with a
# mechanical comparison against a block-less emitter, the way the negative
# controls below already build sabotaged ones.
.PHONY: tables-block-zero-cost
tables-block-zero-cost: build/tables-generated/.stamp build/tables-generated-cs/.stamp
	@for f in build/tables-generated/*/*Table.h build/tables-generated/*/*Table.cpp \
	          build/tables-generated-cs/*/*Table.cs; do \
		if grep -nE "TableBlock|[A-Za-z0-9_]Block" $$f; then \
			echo "BLOCK ZERO-COST GATE FAILED: the block form leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: no Table source carries one symbol of the block form"
# BuildVersion is NOT in that grep: it is not a block symbol — a build version
# answers "which build?" and not "which form?", both accelerators carry it
# (docs/SPEC-TABLES.md §20.6), and every C++ Table header carries it because every
# table cooks (§7). What holds the block form to zero cost is the line above —
# no Table source carries one BLOCK symbol — and the byte comparison below.
	@for f in build/tables-generated-cs/*/*Table.cs; do \
		if grep -n "BuildVersion" $$f; then \
			echo "BLOCK ZERO-COST GATE FAILED: the C# Table sources carry BuildVersion, which is the BLOCK file's there: $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: the C# Table sources still carry no build version — it is their Block file's"
	@n=0; d=0; \
	for f in testdata/golden/tables/examples/*Table.* testdata/golden/tables/pointers/*Table.* \
	         testdata/golden/tables/block/*Table.* testdata/golden/tables/blockhome/*Table.* ; do \
		dir=$$(basename $$(dirname $$f)); \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated/$$dir/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/examples-cs/*Table.cs; do \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-cs/examples/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/block-cs/*Table.cs; do \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-cs/block/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	for f in testdata/golden/tables/blockhome-cs/*Table.cs; do \
		n=$$(( n + 1 )); \
		cmp -s $$f build/tables-generated-cs/blockhome/$$(basename $$f) || \
			{ echo "ZERO-COST GATE FAILED: $$f moved"; d=$$(( d + 1 )); }; \
	done; \
	if [ "$$d" != "0" ]; then exit 1; fi; \
	if [ "$$n" -lt 38 ]; then echo "ZERO-COST GATE FAILED: compared $$n Table files, expected at least 38 — the glob, not the property, is what broke"; exit 1; fi; \
	echo "block zero-cost gate: $$n Table sources byte-identical to their pins"

# THE BUILD VERSION IS ONE NUMBER (docs/SPEC-TABLES.md §20.7): the constant each
# backend emits, and the number `schema build-version` prints, are the same
# number or the tuple a store is indexed by means two different things. The
# projection it hashes is pinned beside it as a golden, so a change to how it
# is computed breaks every pinned value loudly.
.PHONY: tables-block-build-version
tables-block-build-version: bin/schema build/tables-generated/.stamp build/tables-generated-cs/.stamp
	@v=$$(./bin/schema build-version tables/block); \
		grep -q "inline constexpr uint64_t BuildVersion = $${v}ull;" build/tables-generated/block/RenderBlock.h || \
			{ echo "BUILD VERSION GATE FAILED: the C++ constant is not $$v"; exit 1; }; \
		grep -q "public const ulong BuildVersion = $${v}UL;" build/tables-generated-cs/block/*Block.cs || \
			{ echo "BUILD VERSION GATE FAILED: the C# constant is not $$v"; exit 1; }; \
		echo "block build-version gate: schema build-version, the C++ constant and the C# constant are all $$v"

# THE FILL REFUSER (docs/SPEC-TABLES.md §19.1, §19.5). The multi-threaded fill is an
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
		regions=$$(grep -c "block fill path: begin ----" $$f || true); \
		ends=$$(grep -c "block fill path: end ----" $$f || true); \
		begins=$$(grep -c "^inline bool .*BlockBegin(" $$f || true); \
		lines=$$(wc -l < $$out | tr -d ' '); \
		if [ "$$regions" != "$$ends" ] || [ "$$regions" != "$$begins" ]; then \
			echo "FILL REFUSER FAILED: $$f has $$regions begin markers, $$ends end markers and $$begins Begin functions — the markers, not the property, are what broke"; exit 1; \
		fi; \
		if [ "$$lines" -lt $$(( 40 * $$regions )) ]; then \
			echo "FILL REFUSER FAILED: $$f extracted $$lines lines for $$regions fill paths — the markers, not the property, are what broke"; exit 1; \
		fi; \
	done
	@if grep -nE "$(BLOCK_FORBIDDEN)" build/block-fill/*; then \
		echo "FILL REFUSER FAILED: the generated fill path allocates, locks or takes an atomic (docs/SPEC-TABLES.md §19.1)"; exit 1; \
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

# §19.5's FIRST named negative control: "perturb one row type's pitch constant
# on one side only and the two-language test goes red". The constant is the C#
# side's ShipsStride, which the generated layout check asserts against this
# runtime's own sizeof — so a constant that drifted from the row it describes
# is caught where the drift would matter, and the control proves the assert is
# not inert.
.PHONY: tables-block-pitch-negative-control
tables-block-pitch-negative-control: build/tables-generated-cs/.stamp
	@rm -rf build/block-pitch-sabotage && cp -R build/tables-generated-cs/block build/block-pitch-sabotage
	@sed -i.bak 's|public const long ShipsStride = 88;|public const long ShipsStride = 96; // SABOTAGED|' \
		build/block-pitch-sabotage/RenderBlock.cs
	@grep -q "SABOTAGED" build/block-pitch-sabotage/RenderBlock.cs || \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@rm -f build/block-pitch-sabotage/*.bak
	@if ( cd test/cs-block && dotnet run -p:BlockGeneratedDir=../../build/block-pitch-sabotage ) \
		> build/block-pitch-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# side accepted a pitch constant that is not its row's sizeof"; \
		cat build/block-pitch-sabotage.log; exit 1; \
	fi
	@grep -q "ShipsStride" build/block-pitch-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the pitch constant"; cat build/block-pitch-sabotage.log; exit 1; }
	@echo "block pitch negative control: a pitch constant perturbed on ONE side turns the C# layout check red"

# §19.5's SECOND named negative control: "perturb one field's offset in the
# compiler's layout model and the generated asserts go red on both backends".
# This one reaches past the emitters into ir.layoutRecord, so it proves the
# thing the other controls cannot: that the asserts check the COMPILER's model
# rather than restating whatever the target compiler already said.
#
# The sabotaged compiler reaches the build through `go build -overlay`, so no
# tracked file is ever written to — the same mechanism the big-endian negative
# control uses, and for the same reason.
.PHONY: tables-block-layout-model-negative-control
tables-block-layout-model-negative-control: bin/schema
	@mkdir -p build
	@sed 's|ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start, Size: offset - start, Align: fieldAlign})|ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start + 8, Size: offset - start, Align: fieldAlign}) // SABOTAGED: one field, moved|' \
		ir/blocklayout.go > build/blocklayout-moved.gotext
	@cmp -s build/blocklayout-moved.gotext ir/blocklayout.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/ir/blocklayout.go":"%s/build/blocklayout-moved.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/blocklayout-overlay.json
	@go build -overlay=build/blocklayout-overlay.json -o build/schema-layout-moved ./cmd/schema
	@rm -rf build/block-model-sabotage && mkdir -p build/block-model-sabotage/cpp build/block-model-sabotage/cs
	@./build/schema-layout-moved generate --lang cpp --out build/block-model-sabotage/cpp tables/block
	@./build/schema-layout-moved generate --lang cs --out build/block-model-sabotage/cs tables/block
	@if $(CXX) $(BLOCK_CXXFLAGS) -fsyntax-only -Ibuild/block-model-sabotage/cpp \
		build/block-model-sabotage/cpp/RenderBlock.cpp > build/block-model-cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: C++ accepted a moved offset from the compiler's model"; exit 1; \
	fi
	@grep -q "static_assert" build/block-model-cpp.log || \
		{ echo "NEGATIVE CONTROL FAILED: C++ went red, but not on a layout static_assert"; cat build/block-model-cpp.log; exit 1; }
	@echo "block layout-model negative control (C++): a moved offset in the compiler's model turns the static_asserts red"
	@if ( cd test/cs-block && dotnet run -p:BlockGeneratedDir=../../build/block-model-sabotage/cs ) \
		> build/block-model-cs.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: C# accepted a moved offset from the compiler's model"; \
		cat build/block-model-cs.log; exit 1; \
	fi
	@grep -q "schema block layout" build/block-model-cs.log || \
		{ echo "NEGATIVE CONTROL FAILED: C# went red, but not on the layout check"; cat build/block-model-cs.log; exit 1; }
	@echo "block layout-model negative control (C#): the same moved offset turns the once-run layout check red"

# THE BLOCK HOME's NEGATIVE CONTROL (docs/SPEC-TABLES.md §19.2's C# surface). The
# dogfood found two defects with one root cause — a C# backend that emitted per
# DECLARING FILE and skipped a file with no `table` in it: the unit's shared
# runtime went to the PROTOCOL ID's home, which declares no table in any real
# unit, and a blittable record went to its declaring file's Block.cs, which is
# never written when that file declares only `type`s. Neither produced a
# diagnostic; both produced a unit that does not compile.
#
# tables/blockhome has exactly that shape, and test/cs-block compiles it.
#
# THE HOME IS <Package>Block.cs NOW (docs/SPEC-TABLES.md §19.2), emitted FOR THE UNIT
# when no file of the unit is named for the package — which is what makes the
# home unconditional and this class of defect structural rather than a rule to
# keep. The sabotage is therefore the one line that guarantees it: take away
# the emitted-for-the-unit home, and the runtime and every blittable record
# have nowhere to land. The compile must FAIL.
.PHONY: tables-block-home-negative-control
tables-block-home-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|if home != "" \&\& !runtimeWritten {|if false \&\& !runtimeWritten { // SABOTAGED: no home is emitted for the unit|' \
		internal/codegen/cstable/block.go > build/csblock-declaring-file.gotext
	@cmp -s build/csblock-declaring-file.gotext internal/codegen/cstable/block.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/block.go":"%s/build/csblock-declaring-file.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csblock-overlay.json
	@go build -overlay=build/csblock-overlay.json -o build/schema-csblock-sabotaged ./cmd/schema
	@rm -rf build/blockhome-sabotage && mkdir -p build/blockhome-sabotage
	@./build/schema-csblock-sabotaged generate --lang cs --out build/blockhome-sabotage tables/blockhome
	@if ( cd test/cs-block && dotnet build -v q --nologo -p:BlockHomeGeneratedDir=../../build/blockhome-sabotage ) \
		> build/blockhome-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the unit compiled with its block runtime emitted nowhere"; \
		cat build/blockhome-sabotage.log; exit 1; \
	fi
	@grep -qE "CS0103|CS0234|CS0246" build/blockhome-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on an undefined name"; tail -20 build/blockhome-sabotage.log; exit 1; }
	@echo "block home negative control: a unit with no home for its block runtime does not compile"

# THE RUNTIME HOME IS THE PACKAGE (docs/SPEC-TABLES.md §19.2). A unit's shared C#
# runtime — the table runtime and the text form's walk in <Package>Table.cs, the
# block runtime and its blittable records in <Package>Block.cs, the cook runtime
# in <Package>Cook.cs — is emitted ONCE, and every earlier rule picked WHERE off
# the file order. Adding a file that sorts earlier then relocated ~2,000 lines:
# correct output, and a diff nobody can read (issue #347).
#
# The gate adds exactly such a file to a COPY of tables/examples — Aaa.schema,
# ahead of Guarded.schema — and requires the homes not to move. The table
# runtime is byte-identical across the two trees as well as same-named: it
# carries no build version (the zero-cost gate above), so a unit gaining a table
# cannot move it. The block and cook runtimes DO carry the build version, so
# their names are what is checked.
.PHONY: tables-runtime-home
tables-runtime-home: bin/schema
	@rm -rf build/runtime-home && mkdir -p build/runtime-home/src
	@cp tables/examples/*.schema build/runtime-home/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home/src/Aaa.schema
	@./bin/schema generate --lang cs --out build/runtime-home/base tables/examples
	@./bin/schema generate --lang cs --out build/runtime-home/added build/runtime-home/src
	@for surface in Table Block Cook; do \
		base=$$(cd build/runtime-home/base && grep -l "the unit's shared runtime lives here" *$$surface.cs); \
		added=$$(cd build/runtime-home/added && grep -l "the unit's shared runtime lives here" *$$surface.cs); \
		if [ "$$base" != "Tabledemo$$surface.cs" ] || [ "$$added" != "Tabledemo$$surface.cs" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.cs both times"; exit 1; \
		fi; \
	done
	@cmp -s build/runtime-home/base/TabledemoTable.cs build/runtime-home/added/TabledemoTable.cs || \
		{ echo "RUNTIME HOME GATE FAILED: the table runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate: the table, block and cook runtimes stay in <Package><Surface>.cs when an earlier-sorting file joins the unit"

# The NEGATIVE CONTROL: put the file-order rule back — the table runtime to the
# protocol id's home — and the home must MOVE when the earlier-sorting file
# joins. A gate that has never gone red is watching nothing.
.PHONY: tables-runtime-home-negative-control
tables-runtime-home-negative-control: bin/schema tables-runtime-home
	@sed 's|home := capitalize(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/cstable/cstable.go > build/csruntime-fileorder.gotext
	@grep -q SABOTAGED build/csruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cstable/cstable.go":"%s/build/csruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csruntime-overlay.json
	@go build -overlay=build/csruntime-overlay.json -o build/schema-csruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home/base-sabotage build/runtime-home/added-sabotage
	@./build/schema-csruntime-sabotaged generate --lang cs --out build/runtime-home/base-sabotage tables/examples
	@./build/schema-csruntime-sabotaged generate --lang cs --out build/runtime-home/added-sabotage build/runtime-home/src
	@base=$$(cd build/runtime-home/base-sabotage && grep -l "the unit's shared runtime lives here" *Table.cs); \
	 added=$$(cd build/runtime-home/added-sabotage && grep -l "the unit's shared runtime lives here" *Table.cs); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control: the file-order rule moves the runtime from $$base to $$added"

# DEFECT B's NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.7's DEPTH ONE, BOUNDED ONLY).
# The dogfood found the C# blittable emitter projecting a bounded array INSIDE
# a nested record out of line — a sixteen-byte triple where C++ put the whole
# array, and every field after it somewhere else. Nothing said so until
# Verify() threw on the first Open, in the game.
#
# This puts the defect back — through `go build -overlay`, so no tracked file
# is written — and requires the gate to go red. The gate has TWO halves and
# either firing is it working: COMPILING every corpus unit's generated C# is
# half (a projected array is not the struct the rest of the unit indexes), and
# running each unit's Verify() is the other (a projected array moves the sizes
# and offsets). The control reports which one fired.
.PHONY: tables-block-inline-array-negative-control
tables-block-inline-array-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|if projection \&\& ir.BlockOutOfLine(f) {|if ir.BlockOutOfLine(f) { // SABOTAGED: project at every depth|' \
	     -e 's|inline := !projection \|\| !ir.BlockOutOfLine(f)|inline := !ir.BlockOutOfLine(f)|' \
		internal/codegen/cstable/block.go > build/csblock-depth.gotext
	@cmp -s build/csblock-depth.gotext internal/codegen/cstable/block.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/block.go":"%s/build/csblock-depth.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csblock-depth-overlay.json
	@go build -overlay=build/csblock-depth-overlay.json -o build/schema-depth-sabotaged ./cmd/schema
	@rm -rf build/blockhome-depth && mkdir -p build/blockhome-depth
	@./build/schema-depth-sabotaged generate --lang cs --out build/blockhome-depth tables/blockhome
	@if ( cd test/cs-block && dotnet run -p:BlockHomeGeneratedDir=../../build/blockhome-depth ) \
		> build/blockhome-depth.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a bounded array projected inside a nested record passed the layout gate"; \
		cat build/blockhome-depth.log; exit 1; \
	fi
	@if grep -q "schema block layout" build/blockhome-depth.log; then \
		echo "block inline-array negative control: the LAYOUT CHECK went red — a projected array inside a nested record moves the offsets"; \
	elif grep -qE "CS1061|CS0246|CS0117" build/blockhome-depth.log; then \
		echo "block inline-array negative control: the COMPILE went red — a projected array is not the struct the rest of the generated unit indexes"; \
	else \
		echo "NEGATIVE CONTROL FAILED: it went red, but not on either half of the gate"; tail -20 build/blockhome-depth.log; exit 1; \
	fi

# GATE 2 (docs/SPEC-TABLES.md §12.1): the MEASURED gate, two numbers, and it is not
# part of `make test` on purpose — a correctness suite whose verdict depends on
# the machine's mood is not a correctness suite, and the estate's bench rules
# want one bench at a time per machine and a quiet window.
#
# The C++ half writes the representative frame it measured into
# build/block_gate2.bin and the C# half reads THE SAME BYTES, so the two
# numbers are taken over one frame rather than over two descriptions of one.
# Each half runs its own golden gate first and REFUSES to bench on a mismatch.
build/schema_test_block_gate2: build/tables-generated/.stamp test/tables/block_gate2_main.cpp
	@mkdir -p build
	$(CXX) -O2 $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_gate2_main.cpp $(BLOCK_SOURCES) -o $@

.PHONY: tables-block-gate2
tables-block-gate2: build/schema_test_block_gate2 build/tables-generated-cs/.stamp
	./build/schema_test_block_gate2
	cd test/cs-block && dotnet run -c Release -- --gate2

# THE SMOKE, which is what CI runs (certification legs only, never a PR).
# The gate above is a MEASUREMENT and its verdict belongs on a quiet box; a
# gate nothing runs anywhere is a gate that rots, which is the other failure.
# So the correctness half — the two arms agreeing byte for byte across all
# nine sections, and the frame the C# half reads — runs on every push to main
# and nightly, at a small N, with the 5% band NOT enforced and both halves
# saying so. A drift in what the generated form WRITES goes red here; a slow
# runner does not.
.PHONY: tables-block-gate2-smoke
tables-block-gate2-smoke: build/schema_test_block_gate2 build/tables-generated-cs/.stamp
	./build/schema_test_block_gate2 --smoke
	cd test/cs-block && dotnet run -c Release -- --gate2-smoke
# The NEGATIVE CONTROL for a KEYED object's duplicate counting
# (docs/SPEC-TABLES.md §16.2). Last-wins inside a keyed object was already true, so
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

# The NEGATIVE CONTROL for a keyed array's ITERATION RANGE (docs/SPEC-TABLES.md
# §2.4). The iteration's whole promise is that it walks EVERY stored slot and
# yields the KEY it holds, 1..E.Max; an off-by-one at either end reads as an
# ordinary walk, because every untouched slot holds the same declared defaults.
#
# It sabotages the EMITTER's begin() past the first stored slot and requires
# THE TABLES SUITE ITSELF — the same test/tables/main.cpp the leg runs, against
# a whole corpus regenerated from the sabotaged compiler — to go red. A
# purpose-written fixture would only prove the sabotage is observable by a
# fixture written for it; what has to be shown is that the GUARDED test reddens.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to (the big-endian control's rule).
.PHONY: tables-keyed-iteration-negative-control
tables-keyed-iteration-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|Iterator begin() { return Iterator{ slots, 0 }; }|Iterator begin() { return Iterator{ slots, 1 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 0 }; }|ConstIterator begin() const { return ConstIterator{ slots, 1 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-first-slot.gotext
	@cmp -s build/cpptable-first-slot.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-first-slot.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-first-slot-overlay.json
	@go build -overlay=build/cpptable-first-slot-overlay.json -o build/schema-first-slot ./cmd/schema
	@rm -rf build/tables-first-slot && mkdir -p build/tables-first-slot
	$(call tables_generate,./build/schema-first-slot,build/tables-first-slot)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-first-slot) \
		test/tables/main.cpp $$(ls build/tables-first-slot/*/*Table.cpp) -o build/schema_test_tables_first_slot
	@if ./build/schema_test_tables_first_slot > build/tables-first-slot.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: begin() past the first stored slot left the tables suite GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/main.cpp" build/tables-first-slot.log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on a CHECK"; cat build/tables-first-slot.log; exit 1; }
	@echo "negative control: begin() past the first stored slot turns the TABLES SUITE red — $$(grep -c '^FAIL' build/tables-first-slot.log) failures"

# THE None REFUSAL, HELD UNDER -DNDEBUG (docs/SPEC-TABLES.md §2.4). The refusal is
# unconditional by ruling: indexing a keyed array by None is a program error in
# EVERY configuration, because the shifted storage has no slot for None and a
# build that let the index through would read one element BEFORE the array.
#
# The tables suite proves the refusal fires, but it compiles with asserts LIVE,
# so it cannot tell an unconditional refusal from an assert. This gate compiles
# ONE translation unit -DNDEBUG — the configuration a game ships, and the one
# that removes an assert — and requires the None index to end the program
# there too.
.PHONY: tables-keyed-none-refusal-ndebug
tables-keyed-none-refusal-ndebug: build/tables-generated/.stamp test/tables/keyed_none_ndebug_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-generated/examples \
		test/tables/keyed_none_ndebug_main.cpp -o build/schema_test_keyed_none_ndebug
	./build/schema_test_keyed_none_ndebug

# and its NEGATIVE CONTROL: put the refusal back to a bare assert — the shape
# the ruling replaced — and the gate above must go RED, because -DNDEBUG then
# removes it. A gate that only ever passes proves nothing about what it checks.
.PHONY: tables-keyed-none-refusal-negative-control
tables-keyed-none-refusal-negative-control: bin/schema test/tables/keyed_none_ndebug_main.cpp
	@mkdir -p build
	@sed -e 's|            abort();|            /* SABOTAGED: a debug-only guard again */|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-assert-only.gotext
	@grep -q SABOTAGED build/cpptable-assert-only.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-assert-only.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-assert-only-overlay.json
	@go build -overlay=build/cpptable-assert-only-overlay.json -o build/schema-assert-only ./cmd/schema
	@rm -rf build/tables-assert-only && mkdir -p build/tables-assert-only
	@./build/schema-assert-only generate --lang cpp --out build/tables-assert-only tables/examples
	@$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-assert-only \
		test/tables/keyed_none_ndebug_main.cpp -o build/schema_test_keyed_none_assert_only
	@if ./build/schema_test_keyed_none_assert_only > build/keyed-none-assert-only.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a debug-only guard left the -DNDEBUG gate GREEN"; exit 1; \
	fi
	@grep -q "the refusal was compiled out" build/keyed-none-assert-only.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the refusal"; cat build/keyed-none-assert-only.log; exit 1; }
	@echo "negative control: a debug-only guard turns the -DNDEBUG refusal gate red"

# The NEGATIVE CONTROL for the SHIFT itself (docs/SPEC-TABLES.md §2.4, owner ruling
# 2026-09-03). The storage holds E.Max slots with the key k at index k-1 and
# nothing for None. Putting the None slot BACK — E.Max + 1 slots, no shift —
# is the exact edit the ruling reversed, and it must not be able to pass
# quietly: the compiler computes the layout from E.Max and both backends assert
# it (§19.3), so a storage type one element wider than the compiler says
# must fail to COMPILE, and the corpus's own sizeof checks must fail too.
#
# The whole point is that this is caught by the GATES already in the tree — the
# block projection's static_asserts and the tables suite — rather than by a
# fixture written to notice it.
.PHONY: tables-keyed-shift-negative-control
tables-keyed-shift-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|static constexpr int32_t kSlots = (int32_t) E::Max;|static constexpr int32_t kSlots = (int32_t) E::Max + 1; // SABOTAGED: None'"'"'s slot back|' \
	     -e 's|return slots\[ (int32_t) key - 1 \];|return slots[ (int32_t) key ]; // SABOTAGED: no shift|' \
	     -e 's|Entry operator\*() const { return Entry{ (E) ( index + 1 ), slots\[index\] }; }|Entry operator*() const { return Entry{ (E) index, slots[index] }; } // SABOTAGED|' \
	     -e 's|ConstEntry operator\*() const { return ConstEntry{ (E) ( index + 1 ), slots\[index\] }; }|ConstEntry operator*() const { return ConstEntry{ (E) index, slots[index] }; } // SABOTAGED|' \
	     -e 's|Iterator begin() { return Iterator{ slots, 0 }; }|Iterator begin() { return Iterator{ slots, 1 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 0 }; }|ConstIterator begin() const { return ConstIterator{ slots, 1 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-none-slot.gotext
	@test $$(grep -c SABOTAGED build/cpptable-none-slot.gotext) -eq 7 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched $$(grep -c SABOTAGED build/cpptable-none-slot.gotext) of 7 sites"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-none-slot.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-none-slot-overlay.json
	@go build -overlay=build/cpptable-none-slot-overlay.json -o build/schema-none-slot ./cmd/schema
	@rm -rf build/tables-none-slot && mkdir -p build/tables-none-slot
	$(call tables_generate,./build/schema-none-slot,build/tables-none-slot)
	@# the LAYOUT GATE: the block corpus asserts the compiler's own offsets and
	@# sizes, and a keyed array one element wider moves them
	@if $(CXX) $(BLOCK_CXXFLAGS) -Ibuild/tables-none-slot/block -Ibuild/tables-none-slot/blockhome \
			-fsyntax-only build/tables-none-slot/block/PaddedBlock.cpp > build/tables-none-slot-layout.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the None slot back left the BLOCK LAYOUT GATE green"; exit 1; \
	fi
	@grep -q "projection" build/tables-none-slot-layout.log || \
		{ echo "NEGATIVE CONTROL FAILED: the block build went red, but not on a layout assert"; cat build/tables-none-slot-layout.log; exit 1; }
	@# and the SIZEOF assertions in the tables suite itself
	@if $(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-none-slot) \
			test/tables/main.cpp $$(ls build/tables-none-slot/*/*Table.cpp) -o build/schema_test_tables_none_slot \
			> build/tables-none-slot-build.log 2>&1 && \
		./build/schema_test_tables_none_slot > build/tables-none-slot.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the None slot back left the tables suite GREEN"; exit 1; \
	fi
	@echo "negative control: the None slot back turns the BLOCK LAYOUT GATE and the tables suite red"

# THE CLAMP-AT-STORAGE-LIMITS GATE (docs/SPEC-TABLES.md §4). A read clamps a
# value to the field's declared range; a bound sitting ON its storage type's
# own limit describes a check with no false case, and the emitter does not
# write it — the same test that already drops a bits(N) width clamp when N is
# the storage width.
#
# That rule is a BUILD rule and not only a shape: a tautological comparison is
# an ERROR under the repo's own flags, and the two compilers split the work.
# gcc reds the unsigned half — `decoded_v < 0ull`, "comparison of unsigned
# expression in '< 0' is always false", -Wtype-limits, which -Wextra implies
# and this names anyway. clang reds the signed half — `decoded_v < -128`,
# -Wtautological-type-limit-compare. So a tree carrying such a field can read
# clean on one platform and fail on the other, which is how it stayed
# invisible until the tables bench corpus met it.
#
# tables/examples/Ranges.schema declares every integer width four ways — both
# bounds at the limits, each limit alone, and one value off each end — so all
# four emission shapes are compiled here under both diagnostics.
CLAMP_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wtype-limits -ffp-contract=off

.PHONY: tables-clamp-limits
tables-clamp-limits: build/tables-generated/.stamp
	@mkdir -p build
	@printf '#include "RangesTable.h"\n' > build/clamp-limits-tu.cpp
	$(CXX) $(CLAMP_CXXFLAGS) -Ibuild/tables-generated/examples -fsyntax-only build/clamp-limits-tu.cpp
	@echo "clamp-at-storage-limits gate: the bounded corpus carries no comparison that cannot fire"

# The negative control puts the PRE-FIX emission back through `go build
# -overlay`, so no tracked file is written: the two comparisons that decide
# which ends are live become non-strict, which is always true given the
# checker already refuses a bound outside its storage — every declared end
# written, storage limit or not.
.PHONY: tables-clamp-limits-negative-control
tables-clamp-limits-negative-control: bin/schema
	@mkdir -p build
	@sed 's|return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0|return f.IntMin.Cmp(lo) >= 0, f.IntMax.Cmp(hi) <= 0 // SABOTAGED: both ends always written|' \
		internal/codegen/cpptable/codecs.go > build/cpptable-always-clamp.gotext
	@grep -q SABOTAGED build/cpptable-always-clamp.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/codecs.go":"%s/build/cpptable-always-clamp.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-always-clamp-overlay.json
	@go build -overlay=build/cpptable-always-clamp-overlay.json -o build/schema-always-clamp ./cmd/schema
	@rm -rf build/clamp-sabotage && mkdir -p build/clamp-sabotage
	@./build/schema-always-clamp generate --lang cpp --out build/clamp-sabotage tables/examples
	@# the rest of the corpus is UNMOVED by the sabotage — no other declared
	@# bound in it sits on a storage limit, which is why the defect survived
	@cmp -s build/clamp-sabotage/TablesTable.h build/tables-generated/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage moved a file it has no business moving"; exit 1; }
	@printf '#include "RangesTable.h"\n' > build/clamp-limits-tu.cpp
	@if $(CXX) $(CLAMP_CXXFLAGS) -Ibuild/clamp-sabotage -fsyntax-only build/clamp-limits-tu.cpp \
			> build/clamp-limits-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the pre-fix emission compiled clean — this compiler diagnoses neither half"; exit 1; \
	fi
	@grep -q "always false" build/clamp-limits-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the build went red, but not on a comparison that cannot fire"; \
		  cat build/clamp-limits-negative.log; exit 1; }
	@echo "negative control: the storage-limit clamps back turn the build red — $$(grep -c 'always false' build/clamp-limits-negative.log) comparisons that cannot fire"

# THE ZERO-RANGE GATE and its NEGATIVE CONTROL (SPEC §4.6). A field whose
# declared range excludes zero and declares no default is born outside its own
# range: zero initialization is the language's rule, so the fresh value is one
# the wire cannot carry. The checker refuses that shape; the refusal's corpus
# case lives in the break-the-language suite.
#
# The control shows the refusal is load-bearing. It takes the gate back out
# through `go build -overlay` (no tracked file is written), generates the
# refused unit, and compiles it against NDEBUG — the shipping configuration,
# where the write-side range assert is gone and the defect is silent. A
# freshly constructed value must fail to survive its own wire.
.PHONY: check-zero-range-negative-control
check-zero-range-negative-control: bin/schema test/zero_range_negative_main.cpp
	@mkdir -p build/zero-range
	@printf 'package rangezero\n\ntype Probe\n{\n    x    uint8 | min = 1, max = 255\n    tail uint8\n}\n' > build/zero-range/RangeZero.schema
	@if ./bin/schema check build/zero-range > build/zero-range-check.log 2>&1; then \
		echo "GATE FAILED: the checker accepted a [1, 255] field with no declared default"; exit 1; \
	fi
	@grep -q "excludes zero" build/zero-range-check.log || \
		{ echo "GATE FAILED: it was refused, but not for excluding zero"; cat build/zero-range-check.log; exit 1; }
	@sed 's|c.requireDefaultInRange(f, out)|// SABOTAGED: the zero-range gate removed|' \
		internal/check/check.go > build/check-no-zero-gate.gotext
	@grep -q SABOTAGED build/check-no-zero-gate.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/check/check.go":"%s/build/check-no-zero-gate.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/check-no-zero-gate-overlay.json
	@go build -overlay=build/check-no-zero-gate-overlay.json -o build/schema-no-zero-gate ./cmd/schema
	@rm -rf build/zero-range-sabotage && mkdir -p build/zero-range-sabotage
	@./build/schema-no-zero-gate generate --lang cpp --out build/zero-range-sabotage build/zero-range
	@grep -q 'uint8_t x = 0; // wire \[1, 255\]' build/zero-range-sabotage/RangeZero.h || \
		{ echo "NEGATIVE CONTROL FAILED: the gateless compiler did not emit the out-of-range initializer"; exit 1; }
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -DNDEBUG \
		-I$(SERIALIZE) -Ibuild/zero-range-sabotage \
		test/zero_range_negative_main.cpp -o build/schema_test_zero_range_negative
	./build/schema_test_zero_range_negative

# Deliberately compiled WITHOUT -I$(SERIALIZE): the generated Table headers
# carry no serialize dependency, and this build proves it stays that way.
#
# TABLES_INCLUDES is shared with the sanitized twin below, so the two builds
# can never drift into covering different code.
TABLES_INCLUDES := $(call tables_includes,build/tables-generated)
TABLES_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread

# The text form's runtime is a generated TRANSLATION UNIT now, not header
# content (docs/SPEC-TABLES.md §16.1): a consumer that calls FromJson/ToJson
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

# ---- the BIG-ENDIAN leg (docs/SPEC-TABLES.md §3 and §19.1) ----------------------
#
# The wire is little-endian and byte-oriented (§3), and a block is produced in
# the byte order of the build that wrote it (§19.1). Both were rules on a page:
# every host this repo builds on is little-endian, so nothing ever read a
# golden the other way round. This leg builds the tables battery for a
# BIG-ENDIAN target and runs it under an emulator, which turns the two rules
# into gates — the goldens a little-endian host wrote are loaded, re-written
# and byte-compared by a big-endian reader, and a block that crosses the byte
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

# The COOK's read side, for a BIG-ENDIAN target. A cook is produced in the byte
# order of the build it is cooked for (docs/SPEC-TABLES.md §7), so this is where that
# stops being a sentence: the big-endian build opens the big-endian cook
# NATIVELY — magic, header words, deltas and all, with no fix-up pass anywhere —
# and refuses the little-endian one, and this host does the mirror image.
build/schema_test_cook_be: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(BE_CXX) $(COOK_CXXFLAGS) -static $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

# The cross-endian BLOCK driver, built BOTH ways: a block is produced in the
# byte order of the build that wrote it, and every other block test in this
# tree runs producer and consumer in one process. This is the one part of
# §19.1 that needs two builds and a file between them.
build/schema_test_block_endian: build/tables-generated/.stamp test/tables/block_endian_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_endian_main.cpp $(BLOCK_SOURCES) -o $@

build/schema_test_block_endian_be: build/tables-generated/.stamp test/tables/block_endian_main.cpp
	@mkdir -p build
	$(BE_CXX) $(BLOCK_CXXFLAGS) -static $(BLOCK_INCLUDES) test/tables/block_endian_main.cpp $(BLOCK_SOURCES) -o $@

.PHONY: tables-big-endian
tables-big-endian: build/schema_test_tables_be build/schema_test_block_endian build/schema_test_block_endian_be build/schema_test_cook build/schema_test_cook_be build/cook-open/.stamp
	$(BE_RUN) ./build/schema_test_tables_be
	@echo "big-endian leg: the wire crosses the byte order"
	./build/schema_test_block_endian write build/block-host.bin
	$(BE_RUN) ./build/schema_test_block_endian_be write build/block-target.bin
	$(BE_RUN) ./build/schema_test_block_endian_be accept build/block-target.bin
	$(BE_RUN) ./build/schema_test_block_endian_be refuse build/block-host.bin
	./build/schema_test_block_endian accept build/block-host.bin
	./build/schema_test_block_endian refuse build/block-target.bin
	@echo "big-endian leg: a block does not cross the byte order either — the magic refuses it and the prologue's word names the order that wrote it"
	$(BE_RUN) ./build/schema_test_cook_be accept Scene build/cook-open/Scene-be.cook
	$(BE_RUN) ./build/schema_test_cook_be golden Scene build/cook-open/Scene-be.cook
	$(BE_RUN) ./build/schema_test_cook_be refuse Scene build/cook-open/Scene.cook
	./build/schema_test_cook accept Scene build/cook-open/Scene.cook
	./build/schema_test_cook refuse Scene build/cook-open/Scene-be.cook
	@echo "big-endian leg: a cook opens NATIVELY in the order it was cooked for, whole graph and all, and a cook of the other order is refused by the magic"
	$(MAKE) tables-cook-endian

# THE COOK IS THE HOST'S BUSINESS IN NEITHER DIRECTION (docs/SPEC-TABLES.md §7).
# The byte order is settled AT COOK TIME for the TARGET build, so what a cook
# holds must depend on `--byte-order` and on nothing else — least of all on the
# order of the machine that ran the tool. Every host this repo builds on is
# little-endian, so that is an assertion on a page until a big-endian host runs
# the same command: a cooker that reached for host order anywhere would have
# produced byte-identical files on every other leg in this file.
#
# Go cross-compiles, so the target binary is one env var and the pinned
# emulator is already installed for the legs above. The gate is BYTE IDENTITY
# ACROSS HOSTS, both orders, over a fixed root and a pointered one: four files
# from this host and four from s390x, compared pairwise.
.PHONY: tables-cook-endian
tables-cook-endian: bin/schema
	@rm -rf build/cook-endian && mkdir -p build/cook-endian
	GOOS=linux GOARCH=s390x go build -o build/cook-endian/schema-be ./cmd/schema
	GOOS=linux GOARCH=s390x go build -o build/cook-endian/cookgen-be ./test/cookgen
	go build -o build/cook-endian/cookgen ./test/cookgen
	./bin/schema pack --root PackConfig --out build/cook-endian/fixed.bin tables/pack/pinned/PackConfig tables/examples
	@for order in little big; do \
		./bin/schema cook --root PackConfig --in build/cook-endian/fixed.bin \
			--out build/cook-endian/host-$$order.cook --byte-order $$order tables/examples || exit 1; \
		$(BE_RUN) ./build/cook-endian/schema-be cook --root PackConfig --in build/cook-endian/fixed.bin \
			--out build/cook-endian/target-$$order.cook --byte-order $$order tables/examples || exit 1; \
		cmp build/cook-endian/host-$$order.cook build/cook-endian/target-$$order.cook || \
			{ echo "FAILED: a $$order-endian cook differs between a little-endian host and a big-endian one"; exit 1; }; \
		./build/cook-endian/cookgen --bytes 65536 --byte-order $$order --out build/cook-endian/host-gen-$$order.cook || exit 1; \
		$(BE_RUN) ./build/cook-endian/cookgen-be --bytes 65536 --byte-order $$order --out build/cook-endian/target-gen-$$order.cook || exit 1; \
		cmp build/cook-endian/host-gen-$$order.cook build/cook-endian/target-gen-$$order.cook || \
			{ echo "FAILED: a $$order-endian synthetic region differs between hosts"; exit 1; }; \
		$(BE_RUN) ./build/cook-endian/schema-be cook-check --root PackConfig \
			build/cook-endian/host-$$order.cook tables/examples || exit 1; \
		./bin/schema cook-check --root PackConfig build/cook-endian/target-$$order.cook tables/examples || exit 1; \
	done
	@echo "big-endian leg: a cook's bytes are the TARGET's order and never the host's, and either host checks either file"

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

# The PACK GOLDEN (docs/SPEC-TABLES.md §17.4, issue #257). `schema pack` carries an
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
# unit that holds it (docs/SPEC-TABLES.md §16.1) — the same rule any consumer follows
PACK_JSON_SOURCES = $$(ls build/tables-generated/examples/*Table.cpp)
PACK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off
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

# The HOSTILE-VALUE gate (docs/SPEC-TABLES.md §16.2, §16.3, §17.5). One tree per rule
# the text form states — malformed number tokens, a value past a bits(N) width,
# a lone surrogate, `null` on a `?T`, a "None" key, duplicate keys — packed by
# the Go engine and then READ BY THE GENERATED BACKEND. The manifest says which
# trees refuse and what report the rest produce; the backend half asserts the
# invariant the report is a promise about, that bytes the engine called clean
# load clean and re-save byte-identically. The Go half lives in
# internal/tablepack's tests and reads the same manifest.
#
# THE CORPUS AND ITS MANIFEST LIVE IN THE CONFORMANCE HARNESS's DATA
# (testdata/conformance/tables): the battery was always data, so it moved there
# whole rather than keeping a registry of its own, and the harness's
# `json-hostile` surface reads the same rows this gate does. One corpus, one set
# of expectations, two gates asking different things of it.
HOSTILE_MANIFEST := testdata/conformance/tables/MANIFEST.txt
HOSTILE_TREES := $(shell find testdata/conformance/tables/json-hostile -type f 2>/dev/null)

build/hostile-values/.stamp: bin/schema $(HOSTILE_MANIFEST) $(HOSTILE_TREES)
	@rm -rf build/hostile-values
	@mkdir -p build/hostile-values
	@grep '^json-hostile ' $(HOSTILE_MANIFEST) | grep -v ' refused$$' | \
	while read -r kind name unit root tree verdict; do \
		./bin/schema pack --root $$root --out build/hostile-values/$$name.bin --tolerate \
			$$tree tables/examples || exit 1; \
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
	./build/schema_test_hostile $(HOSTILE_MANIFEST) build/hostile-values
	./build/schema_test_hostile_asan $(HOSTILE_MANIFEST) build/hostile-values

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
		testdata/conformance/tables/json-hostile/num-leading-plus tables/examples > /dev/null 2>&1; then \
		echo "pack hostile-value negative control: a leading + packs once the grammar is relaxed"; \
	else \
		echo "NEGATIVE CONTROL FAILED: relaxing the number grammar left num-leading-plus refused"; exit 1; \
	fi
	@./bin/schema pack --root RootConfig --out build/loose.bin --tolerate \
		testdata/conformance/tables/json-hostile/num-leading-plus tables/examples > /dev/null 2>&1 && \
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

# The TABLES bench corpus (bench/tables/README.md): bench/corpus/BenchTable.schema,
# one representative fixed table mirroring BenchMixed, generated for the
# backends that CARRY tables. It goes into its own directory rather than
# beside the type corpus because every other backend REFUSES a unit that
# declares tables, by name (docs/SPEC-TABLES.md §11) — generating it into
# generated/bench/<lang> would break seven legs' generation the day it landed.
# A port adds its stamp here in the same change that adds its leg to
# bench/tables/legs.txt.
generated/bench/tables/cpp/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/cpp
	./bin/schema generate --lang cpp --out generated/bench/tables/cpp bench/corpus/BenchTable.schema
	@touch $@

generated/bench/tables/cs/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/cs
	./bin/schema generate --lang cs --out generated/bench/tables/cs bench/corpus/BenchTable.schema
	@touch $@

# the Rust leg's generated crate: a unit is a Rust CRATE, so the corpus needs a
# Cargo.toml beside its modules. The TABLE modules name no runtime — the
# generated table surface carries no serialize dependency, which is the leg's
# recorded linkage fact — but the unit's PACKET module does, because a table
# closure's types are the packet backend's own and they carry their type-wire
# codecs whether or not this bench calls one.
generated/bench/tables/rust/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/rust/src
	./bin/schema generate --lang rust --out generated/bench/tables/rust/src bench/corpus/BenchTable.schema
	@printf '[package]\nname = "benchtable"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../../$(SERIALIZE_RS)" }\n' > generated/bench/tables/rust/Cargo.toml
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

# the tables bench corpus's producer and oracle (bench/tables/README.md): the
# ONE place that names a field of BenchTable.schema, so no language leg does
build/schema_test_bench_table: generated/bench/tables/cpp/.stamp test/bench/table_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/tables/cpp test/bench/table_main.cpp -o $@

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

test: build/schema_test build/schema_test_guard build/schema_test_tables build/schema_test_block build/schema_test_block_asan build/schema_test_block_fuzz build/schema_test_block_fuzz_asan build/pack-text/.stamp build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/schema_test_tables_asan build/tables-generated-cs/.stamp build/schema_test_random build/schema_test_ludicrous build/schema_test_c build/schema_test_c_ludicrous build/schema_test_bench build/schema_test_bench_c build/schema_test_bench_table generated/bench/tables/cs/.stamp generated/go/.stamp generated/rust/.stamp generated/cs/.stamp generated/js/.stamp generated/dart/.stamp generated/java/.stamp generated/elixir/.stamp generated/go-ludicrous/.stamp generated/rust-ludicrous/.stamp generated/cs-ludicrous/.stamp generated/js-ludicrous/.stamp generated/dart-ludicrous/.stamp generated/java-ludicrous/.stamp generated/elixir-ludicrous/.stamp generated/bench/go/.stamp generated/bench/rust/.stamp generated/bench/cs/.stamp generated/bench/js/.stamp generated/bench/dart/.stamp generated/bench/java/.stamp generated/bench/elixir/.stamp build/java-test/.stamp build/java-test-ludicrous/.stamp build/java-bench/.stamp
	./build/schema_test
	./build/schema_test_guard
	$(MAKE) check-zero-range-negative-control
	./build/schema_test_tables
	./build/schema_test_tables_asan
	$(MAKE) tables-zero-cost
	$(MAKE) tables-json-walk
	$(MAKE) tables-rust-walk
	$(MAKE) tables-rust-clippy
	$(MAKE) tables-rust-features
	$(MAKE) tables-rust-names-negative-control
	$(MAKE) tables-rust-alloc-audit
	$(MAKE) tables-rust-alloc-negative-control
	# the generated Rust table surface CHECKED for a big-endian target, layout
	# const asserts and all. It SKIPS cleanly where the target is not
	# installed, so it costs a machine without it nothing.
	$(MAKE) tables-rust-big-endian
	$(MAKE) tables-json-negative-control
	$(MAKE) tables-cs-json-walk
	$(MAKE) tables-cs-standalone
	$(MAKE) tables-cs-refuses-pointers
	cd test/cs-tables && dotnet run
	# THE CONFORMANCE HARNESS (test/conformance/README.md): the same corpus as
	# data, one driver per language, and the matrix that says which surfaces a
	# backend has. It rides here rather than at the end because the two legs it
	# registers are the two this section has just built.
	$(MAKE) conformance
	$(MAKE) conformance-negative-control
	$(MAKE) conformance-negative-control-block-dump
	$(MAKE) conformance-negative-control-cs
	$(MAKE) conformance-negative-control-go
	$(MAKE) tables-json-keyed-dup-negative-control
	$(MAKE) tables-keyed-iteration-negative-control
	$(MAKE) tables-keyed-none-refusal-ndebug
	$(MAKE) tables-keyed-none-refusal-negative-control
	$(MAKE) tables-keyed-shift-negative-control
	$(MAKE) tables-clamp-limits
	$(MAKE) tables-clamp-limits-negative-control
	$(MAKE) tables-block
	$(MAKE) tables-block-fuzz
	$(MAKE) tables-block-fuzz-extent-negative-control
	$(MAKE) tables-block-fuzz-maximum-negative-control
	$(MAKE) tables-cook-cli
	$(MAKE) tables-cook-scale
	$(MAKE) tables-cook-fuzz-negative-control
	$(MAKE) tables-cook-open
	$(MAKE) tables-cook-valued
	$(MAKE) tables-cook-open-lengths-negative-control
	$(MAKE) tables-cook-open-root-negative-control
	$(MAKE) tables-cook-open-cs
	$(MAKE) tables-cook-open-cs-lengths-negative-control
	$(MAKE) tables-cook-open-cs-root-negative-control
	$(MAKE) tables-block-zero-cost
	$(MAKE) tables-block-build-version
	$(MAKE) tables-block-fill-refuser
	$(MAKE) tables-block-fill-refuser-negative-control
	$(MAKE) tables-block-padding-negative-control
	$(MAKE) tables-block-pitch-negative-control
	$(MAKE) tables-block-layout-model-negative-control
	$(MAKE) tables-block-race-negative-control
	$(MAKE) tables-block-home-negative-control
	$(MAKE) tables-runtime-home
	$(MAKE) tables-runtime-home-negative-control
	$(MAKE) tables-block-inline-array-negative-control
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
	# the tables bench corpus's oracle, and the C# leg's compile gate — the
	# generated table unit has no other consumer under `make test`, and a
	# unit that generates but does not compile is issue #80's lesson
	./build/schema_test_bench_table
	dotnet build bench/tables/cs -c Release --nologo -v quiet
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
update-goldens: build/schema_test build/schema_test_ludicrous build/schema_test_bench build/schema_test_bench_table build/schema_test_tables build/schema_test_block
	@mkdir -p testdata/golden testdata/wire testdata/wire/tables
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_tables
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_block
	@for d in examples pointers block blockhome; do \
		mkdir -p testdata/golden/tables/$$d; \
		cp build/tables-generated/$$d/*Table.h build/tables-generated/$$d/*Table.cpp testdata/golden/tables/$$d/ 2>/dev/null || true; \
	done
	@mkdir -p testdata/golden/tables/examples-cs testdata/golden/tables/block-cs testdata/golden/tables/blockhome-cs
	@cp build/tables-generated-cs/examples/*Table.cs testdata/golden/tables/examples-cs/
	@cp build/tables-generated-cs/block/*Table.cs testdata/golden/tables/block-cs/
	@cp build/tables-generated-cs/blockhome/*Table.cs testdata/golden/tables/blockhome-cs/
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_ludicrous
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_bench
	./build/schema_test_bench_table pin
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

# ---------------------------------------------------------------------------
# THE TABLES BENCH (bench/tables/README.md) ---------------------------------
#
# One measured shape: a representative fixed table written and read on the
# TOLERANT WIRE (docs/SPEC-TABLES.md §3) — the tables layer's per-language
# release gate, and the number a reader who knows protobuf or flatbuffers
# already has a comparison for. Block-form and cook numbers are NOT here: the
# owner's scope ruling on issue #330 keeps those C++/C# and takes them in the
# game on real render data.
#
# The corpus DATA is produced and verified by build/schema_test_bench_table,
# which `make test` runs. `bench-table-corpus` re-pins it: deterministic, so a
# re-pin that changes the committed files means the shape or the vary mapping
# moved — the same stop-the-line rule the wire goldens carry.
bench-table-corpus: build/schema_test_bench_table
	./build/schema_test_bench_table pin

bench-table-check: build/schema_test_bench_table
	./build/schema_test_bench_table verify

# The pass. Every leg in bench/tables/legs.txt, results under
# bench/tables/results/. A PUBLISHABLE number is a box sitting under the
# estate's bench rules — core 15, server stopped, not live, blessed per run;
# a run on a shared interactive machine is a pairing check and the board says
# which one it is.
bench-tables: generated/bench/tables/cpp/.stamp generated/bench/tables/cs/.stamp generated/bench/tables/rust/.stamp bench-table-check
	bench/tables/run.sh

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
	./bin/schema check tables/blockhome
	./bin/schema check test/tables/V1.schema
	./bin/schema check test/tables/V2.schema
	./bin/schema check test/tables/P1.schema
	./bin/schema check test/tables/P2.schema
	./bin/schema check test/tables/P3.schema
	./bin/schema check bench/corpus/Bench.schema
	./bin/schema check bench/corpus/RealWorld.schema
	./bin/schema check bench/corpus/BenchTable.schema

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
	./bin/schema fmt tables/blockhome
	./bin/schema fmt test/tables/V1.schema
	./bin/schema fmt test/tables/V2.schema
	./bin/schema fmt test/tables/P1.schema
	./bin/schema fmt test/tables/P2.schema
	./bin/schema fmt test/tables/P3.schema
	./bin/schema fmt bench/corpus/Bench.schema
	./bin/schema fmt bench/corpus/RealWorld.schema
	./bin/schema fmt bench/corpus/BenchTable.schema

# The one-benchmark rule, made mechanical: no hand-coded measurement of a
# schema shape anywhere in this repo except what bench/SHAPE-GATE.allow names.
shape-gate:
	go run ./bench/tools/shapegate

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench bench-variants bench-tables bench-table-corpus bench-table-check generated-current shape-gate

# ---------------------------------------------------------------------------
# THE TABLES CONFORMANCE HARNESS (test/conformance/README.md) ----------------
#
# A port of the tables layer is "make the driver pass". The DATA lives under
# testdata/conformance/tables and names no language; the CONTRACT lives in
# test/conformance/README.md; the harness runs every registered driver over
# every surface and prints the matrix. Registering a port is one line in
# test/conformance/drivers.txt plus one driver.
#
# The rule this target lives under is the two-minute one (#320). Measured on
# arm64 macOS at the landing, everything already built, median of three:
#
#   both legs, 116 cases       5.07 s
#   the cpp leg alone          0.41 s   (10 native execs, plus materialising)
#   the cs leg alone           4.92 s   (12 `dotnet run` start-ups)
#
# The cost is per-PROCESS, not per-case: the C# leg starts a runtime thirteen
# times — `list`, six surfaces, and one per cook, because test/cs-cook's dump
# takes one root per invocation. So the budget left for seven more languages is
# nearly the whole of it, and nine languages each starting a runtime per surface
# lands near 20 s. Sharding per language leg, the way the type wire's nine legs
# already are, is what the numbers say to do if that stops holding; it is not
# needed at this size.
CONFORMANCE_INCLUDES := -Ibuild/tables-generated/examples -Ibuild/tables-generated/v1 \
	-Ibuild/tables-generated/v2 -Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	-Ibuild/tables-generated/block
CONFORMANCE_SOURCES = build/tables-generated/examples/TablesTable.cpp \
	build/tables-generated/examples/WideTable.cpp build/tables-generated/examples/NestedTable.cpp \
	build/tables-generated/examples/KeyedTable.cpp build/tables-generated/examples/PackTable.cpp build/tables-generated/v1/V1Table.cpp \
	build/tables-generated/v2/V2Table.cpp build/tables-generated/p1/P1Table.cpp \
	build/tables-generated/p3/P3Table.cpp build/tables-generated/block/RenderBlock.cpp \
	build/tables-generated/block/PaddedBlock.cpp

build/conformance-harness: $(wildcard test/conformance/harness/*.go)
	@mkdir -p build
	go build -o $@ ./test/conformance/harness

build/conformance-cpp: build/tables-generated/.stamp test/conformance/cpp/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) test/conformance/cpp/main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

.PHONY: build-conformance-cs
build-conformance-cs: build/tables-generated-cs/.stamp
	cd test/conformance/cs && dotnet build -v q --nologo


# THE RUST TABLE CORPUS: one CRATE per unit, because a unit is a Rust crate the
# way it is a C++ namespace — its own package, its own protocol id, its own
# table runtime. The crates carry a generated Cargo.toml each; nothing here is
# checked in.
RUST_TABLE_UNITS := tabledemo:tables/examples graphdemo:tables/pointers \
	blockdemo:tables/block blockhome:tables/blockhome \
	tblv1:test/tables/V1.schema tblv2:test/tables/V2.schema \
	tblp1:test/tables/P1.schema tblp2:test/tables/P2.schema \
	tblp3:test/tables/P3.schema jsonkeys:test/tables/JsonKeys.schema

build/tables-generated-rust/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema
	@mkdir -p build/tables-generated-rust
	@for unit in $(RUST_TABLE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		rm -rf build/tables-generated-rust/$$name/src; \
		./bin/schema generate --lang rust --out build/tables-generated-rust/$$name/src $$path || exit 1; \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' $$name \
			> build/tables-generated-rust/$$name/Cargo.toml; \
	done
	@touch $@

build/conformance-rust: build/tables-generated-rust/.stamp test/conformance/rust/src/main.rs test/conformance/rust/Cargo.toml
	@mkdir -p build
	cd test/conformance/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	@rm -f $@   # replace the inode: writing over a binary another process is
	            # running corrupts it in place, and a long soak runs this one
	cp test/conformance/rust/target/debug/conformance-rust $@

build/conformance-go: build/tables-generated-go/.stamp $(wildcard test/conformance/go/*.go) test/conformance/go/go.mod
	@mkdir -p build
	cd test/conformance/go && go build -o ../../../$@ .

# THE GO LEG, CROSS-BUILT BIG-ENDIAN (docs/SPEC-TABLES.md §3, §19.1, §7). Go
# cross-compiles, and the pinned emulator is already installed for the C++
# big-endian legs above, so the table WIRE's own claim — every field id, every
# length and every scalar rides little-endian whatever the host is — becomes a
# gate rather than a sentence: a big-endian reader loads the goldens a
# little-endian host wrote, writes them back and is byte-compared.
#
# The driver lists the byte-order-NEUTRAL surfaces and no others, and the reason
# is in test/conformance/go/driver-be: a block and a cook are produced in the
# order of the build that wrote them, so a big-endian reader is CORRECT to
# refuse this corpus's fixtures and has no neutral verdict to give.
build/conformance-go-be: build/tables-generated-go/.stamp $(wildcard test/conformance/go/*.go) test/conformance/go/go.mod
	@mkdir -p build
	cd test/conformance/go && GOOS=linux GOARCH=s390x go build -o ../../../$@ .

# THE TABLE-WIRE BENCH PAIR (docs/SPEC-TABLES.md, the performance ladder): the
# C++ reference and the Go port over the SAME golden, the same three operations
# and the same warm buffer, so the ratio between them says whether a Go consumer
# of this format pays for the language or for the format.
#
# ITERATION instruments, not certification ones (BENCH-STANDARD.md): they run on
# a workstation to compare two ports on one machine at one moment, they are not
# a gate, and nothing in `make test` reads them. -O3 -DNDEBUG on the C++ side is
# the configuration a game ships, which is what makes the comparison fair.
build/schema_test_tables_bench: build/tables-generated/.stamp test/tables/bench_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O3 -DNDEBUG $(TABLES_INCLUDES) test/tables/bench_main.cpp -o $@

.PHONY: tables-bench
tables-bench: build/schema_test_tables_bench build/tables-generated-go/.stamp
	@echo "--- C++ (the reference) ---"
	./build/schema_test_tables_bench
	@echo
	@echo "--- Go (this port) ---"
	cd test/go-tables && go test -run XXX -bench . -benchtime 2s .

.PHONY: conformance-big-endian
conformance-big-endian: build/conformance-harness build/conformance-go-be
	@printf 'go-be test/conformance/go/driver-be\n' > build/conformance-be-drivers.txt
	./build/conformance-harness run --drivers build/conformance-be-drivers.txt --work build/conformance-be-work
	@echo "big-endian leg: the Go table wire, the read report and the text form cross the byte order"

.PHONY: conformance
conformance: build/conformance-harness build/conformance-cpp build/schema_test_cook \
		build-conformance-cs build-cs-cook build/conformance-go build/conformance-rust
	./build/conformance-harness run

# The GENERATED half of the data: the JSON text of every instance and the read
# report of every evolution case, both from the compiler's own engine.
.PHONY: conformance-generate
conformance-generate: build/conformance-harness
	./build/conformance-harness generate

# The PINNED half: the cook's canonical node dump and the block forgery battery
# resolved to byte offsets, both from the reference leg — C++ writes the pins,
# every other leg compares, exactly as the wire goldens work.
.PHONY: conformance-pin
conformance-pin: build/conformance-harness build/conformance-cpp build/schema_test_cook
	./build/conformance-harness pin

# THE NEGATIVE CONTROL, and a harness that has never gone red is watching
# nothing. One byte of ONE dump is flipped in a COPY of the C++ driver — no
# tracked file is written to, so an interrupt cannot leave a sabotaged working
# tree — and the harness must go red, on that surface and on no other. The
# second half is the point: a matrix whose every cell went red would be saying
# "something broke" rather than "the text form broke".
.PHONY: conformance-negative-control
conformance-negative-control: build/conformance-harness build/conformance-cpp
	@rm -rf build/conformance-negative && mkdir -p build/conformance-negative
	@sed 's|if ( !spill( out, f\[1\] + ".json", text.data(), (size_t) size ) ) return 1;|text[0] = (char) ( text[0] ^ 1 ); if ( !spill( out, f[1] + ".json", text.data(), (size_t) size ) ) return 1; // SABOTAGED|' \
		test/conformance/cpp/main.cpp > build/conformance-negative/main.cpp
	@cmp -s build/conformance-negative/main.cpp test/conformance/cpp/main.cpp && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) build/conformance-negative/main.cpp \
		$(CONFORMANCE_SOURCES) -o build/conformance-negative/driver-bin
	@printf '#!/bin/sh\nexec build/conformance-negative/driver-bin "$$@"\n' > build/conformance-negative/driver
	@chmod +x build/conformance-negative/driver
	@printf 'cpp build/conformance-negative/driver\n' > build/conformance-negative/drivers.txt
	@if ./build/conformance-harness run --drivers build/conformance-negative/drivers.txt \
			--work build/conformance-negative/work > build/conformance-negative/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off in a dump left the harness green"; \
		cat build/conformance-negative/log; exit 1; \
	fi
	@grep -q "cpp / json-write" build/conformance-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat build/conformance-negative/log; exit 1; }
	@grep -q "wire          pass" build/conformance-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat build/conformance-negative/log; exit 1; }
	@grep -m1 "cpp / json-write" build/conformance-negative/log
	@echo "negative control: one byte off in one dump turns the harness RED on that surface alone"

# THE NEGATIVE CONTROL FOR THE BLOCK ROW DUMP (docs/SPEC-TABLES.md §19.2), and it
# sabotages neither driver: it flips one byte INSIDE A ROW of the block image
# itself. That is the whole reason the surface exists. `Open` reads the prologue
# and the triples and nothing else (§19.2's O(1) promise), so a byte inside a
# row cannot move its answer — `block` must stay GREEN and `block-dump` must go
# RED, which is the harness saying "the reader opened it and then read it
# wrong". A control that turned both red would be proving the image was
# corrupted, not that anyone reads rows.
#
# The byte is ships row 0's `object_id`: the array starts at 320 and the field
# sits at 64 inside a row, both facts the pinned dump states. If the fixture's
# layout ever moves under it the `cmp` below fails loudly rather than the
# control quietly checking nothing.
CONFORMANCE_NEGATIVE_BLOCK := build/conformance-negative-block
.PHONY: conformance-negative-control-block-dump
conformance-negative-control-block-dump: build/conformance-harness build/conformance-cpp
	@rm -rf $(CONFORMANCE_NEGATIVE_BLOCK) && mkdir -p $(CONFORMANCE_NEGATIVE_BLOCK)
	@cp testdata/wire/tables/block_render.bin $(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin
	@printf '\001' | dd of=$(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin bs=1 seek=384 count=1 conv=notrunc 2>/dev/null
	@cmp -s testdata/wire/tables/block_render.bin $(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage changed no byte of the image"; exit 1; } || true
	@sed 's|testdata/wire/tables/block_render.bin|$(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin|' \
		testdata/conformance/tables/MANIFEST.txt > $(CONFORMANCE_NEGATIVE_BLOCK)/MANIFEST.txt
	@printf 'cpp test/conformance/cpp/driver\n' > $(CONFORMANCE_NEGATIVE_BLOCK)/drivers.txt
	@if ./build/conformance-harness run --manifest $(CONFORMANCE_NEGATIVE_BLOCK)/MANIFEST.txt \
			--drivers $(CONFORMANCE_NEGATIVE_BLOCK)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_BLOCK)/work > $(CONFORMANCE_NEGATIVE_BLOCK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off inside a row left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; \
	fi
	@grep -q "cpp / block-dump" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on block-dump"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -q "^block         pass" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block went red too, so the control does not localise the ROW READ"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -q "^forgery       pass" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -m1 "cpp / block-dump" $(CONFORMANCE_NEGATIVE_BLOCK)/log
	@echo "negative control: one byte off INSIDE A ROW turns the harness RED on block-dump alone — block still opens"

# THE NEGATIVE CONTROL FOR THE C# WALK (docs/SPEC-TABLES.md §16.5), and it is a
# different sabotage from the C++ one above on purpose. That one flips a byte of
# a DUMP and proves the harness can see a wrong ANSWER; this one breaks the
# WALKER and proves the harness can see a wrong WALK.
#
# It is sabotaged IN THE EMITTER and generated afresh, the way the block fuzz's
# controls are (block_fuzz_sabotage above), because the walker IS emitter
# source — one constant in internal/codegen/cstable/json.go — so patching the
# emitter is patching the walk itself rather than an artifact of it. No tracked
# file is written to: the sed lands in build/, a Go build overlay points the
# compiler at it, and the csproj's TablesGeneratedDir points the leg at what
# that compiler generated.
#
# THE SABOTAGE. C++ shifts a field's STORAGE OFFSET by one field width
# (tables-json-negative-control). A C# field has no offset — the descriptor
# carries accessors instead (§8.1) — so the twin of that arithmetic is the FIELD
# INDEX the read path looks a descriptor up by: one key's value lands in its
# neighbour's field. It is bounded on purpose, so a table with an odd field
# count cannot turn the control into an exception rather than a wrong answer,
# and it touches the READ path only.
#
# The second half is the point, as it is for every control here: json-read must
# go RED and every other surface must stay GREEN. A matrix whose every cell went
# red would be saying "something broke" rather than "the C# text form broke" —
# and json-write staying green is what says the break is the READER's.
CONFORMANCE_NEGATIVE_CS := build/conformance-negative-cs
CONFORMANCE_NEGATIVE_CS_SED := s|TableFieldInfo f = info.Fields\[index\];|TableFieldInfo f = info.Fields[(index ^ 1) < info.NumFields ? (index ^ 1) : index]; // SABOTAGED|
.PHONY: conformance-negative-control-cs
conformance-negative-control-cs: build/conformance-harness
	@rm -rf $(CONFORMANCE_NEGATIVE_CS) && mkdir -p $(CONFORMANCE_NEGATIVE_CS)
	@sed '$(CONFORMANCE_NEGATIVE_CS_SED)' internal/codegen/cstable/json.go > $(CONFORMANCE_NEGATIVE_CS)/cstable-json.go.txt
	@cmp -s internal/codegen/cstable/json.go $(CONFORMANCE_NEGATIVE_CS)/cstable-json.go.txt && \
		{ echo "NEGATIVE CONTROL: the C# emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/json.go":"%s/$(CONFORMANCE_NEGATIVE_CS)/cstable-json.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_CS)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_CS)/overlay.json -o $(CONFORMANCE_NEGATIVE_CS)/schema ./cmd/schema
	$(CONFORMANCE_NEGATIVE_CS)/schema generate --lang cs --out $(CONFORMANCE_NEGATIVE_CS)/generated/examples tables/examples
	$(CONFORMANCE_NEGATIVE_CS)/schema generate --lang cs --out $(CONFORMANCE_NEGATIVE_CS)/generated/block tables/block
	$(CONFORMANCE_NEGATIVE_CS)/schema generate --lang cs --out $(CONFORMANCE_NEGATIVE_CS)/generated/v1 test/tables/V1.schema
	$(CONFORMANCE_NEGATIVE_CS)/schema generate --lang cs --out $(CONFORMANCE_NEGATIVE_CS)/generated/v2 test/tables/V2.schema
	$(CONFORMANCE_NEGATIVE_CS)/schema generate --lang cs --out $(CONFORMANCE_NEGATIVE_CS)/generated/p1 test/tables/P1.schema
	$(CONFORMANCE_NEGATIVE_CS)/schema generate --lang cs --out $(CONFORMANCE_NEGATIVE_CS)/generated/p3 test/tables/P3.schema
	@grep -lq SABOTAGED $(CONFORMANCE_NEGATIVE_CS)/generated/*/*Table.cs || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged walk"; exit 1; }
	cd test/conformance/cs && dotnet build -v q --nologo \
		-p:TablesGeneratedDir=$(CURDIR)/$(CONFORMANCE_NEGATIVE_CS)/generated \
		-p:BaseOutputPath=$(CURDIR)/$(CONFORMANCE_NEGATIVE_CS)/bin/ \
		-p:BaseIntermediateOutputPath=$(CURDIR)/$(CONFORMANCE_NEGATIVE_CS)/obj/
	@printf '#!/bin/sh\nexec dotnet %s/bin/Debug/net10.0/schemaconformance.dll "$$@"\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_CS)" > $(CONFORMANCE_NEGATIVE_CS)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_CS)/driver
	@printf 'cs %s/driver\n' "$(CONFORMANCE_NEGATIVE_CS)" > $(CONFORMANCE_NEGATIVE_CS)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_CS)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_CS)/work > $(CONFORMANCE_NEGATIVE_CS)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a sabotaged C# walker left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_CS)/log; exit 1; \
	fi
	@grep -q "cs / json-read" $(CONFORMANCE_NEGATIVE_CS)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat $(CONFORMANCE_NEGATIVE_CS)/log; exit 1; }
	@grep -q "json-write    pass" $(CONFORMANCE_NEGATIVE_CS)/log || \
		{ echo "NEGATIVE CONTROL FAILED: json-write went red too, so the control does not localise the READER"; \
		  cat $(CONFORMANCE_NEGATIVE_CS)/log; exit 1; }
	@grep -q "wire          pass" $(CONFORMANCE_NEGATIVE_CS)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_CS)/log; exit 1; }
	@grep -m1 "cs / json-read" $(CONFORMANCE_NEGATIVE_CS)/log
	@echo "negative control: one field index off in the C# walk turns the harness RED on json-read alone"

# THE GO LEG's NEGATIVE CONTROL, on the same rule as the C++ one: a harness
# that has never gone red on a leg is watching that leg. One byte of ONE wire
# answer is flipped in a COPY of the Go driver — no tracked file is written to,
# so an interrupt cannot leave a sabotaged working tree — and the matrix must go
# red, on that surface and on no other.
.PHONY: conformance-negative-control-go
conformance-negative-control-go: build/conformance-harness build/tables-generated-go/.stamp
	@rm -rf build/conformance-negative-go && mkdir -p build/conformance-negative-go
	@cp test/conformance/go/*.go build/conformance-negative-go/
	@printf 'module schemaconformance\n\ngo 1.23\n' > build/conformance-negative-go/go.mod
	@for m in tabledemo:examples tblv1:v1 tblv2:v2 tblp1:p1 tblp3:p3 blockdemo:block graphdemo:pointers; do \
		printf 'require %s v0.0.0\nreplace %s => $(CURDIR)/build/tables-generated-go/%s\n' \
			"$${m%%:*}" "$${m%%:*}" "$${m##*:}" >> build/conformance-negative-go/go.mod; \
	done
	@printf 'require github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => $(CURDIR)/$(SERIALIZE_GO)\n' >> build/conformance-negative-go/go.mod
	@sed -i.bak 's|if err := spill(out, f\[1\], scratch); err != nil {|scratch[0] ^= 1 // SABOTAGED\n\t\tif err := spill(out, f[1], scratch); err != nil {|' build/conformance-negative-go/main.go
	@grep -q SABOTAGED build/conformance-negative-go/main.go || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	cd build/conformance-negative-go && go build -o ../conformance-negative-go-bin .
	@printf '#!/bin/sh\nexec build/conformance-negative-go-bin "$$@"\n' > build/conformance-negative-go/driver
	@chmod +x build/conformance-negative-go/driver
	@printf 'go build/conformance-negative-go/driver\n' > build/conformance-negative-go/drivers.txt
	@if ./build/conformance-harness run --drivers build/conformance-negative-go/drivers.txt \
			--work build/conformance-negative-go/work > build/conformance-negative-go/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off in a wire answer left the harness green"; \
		cat build/conformance-negative-go/log; exit 1; \
	fi
	@grep -q "go / wire" build/conformance-negative-go/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat build/conformance-negative-go/log; exit 1; }
	@grep -q "report        pass" build/conformance-negative-go/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat build/conformance-negative-go/log; exit 1; }
	@grep -m1 "go / wire" build/conformance-negative-go/log
	@echo "negative control (go): one byte off in one wire answer turns the harness RED on that surface alone"

.PHONY: conformance conformance-generate conformance-pin conformance-negative-control conformance-negative-control-block-dump conformance-negative-control-cs conformance-negative-control-go build-conformance-cs
