# make/rust.mk — the Rust leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.

# the serialize.rs runtime the generated Rust targets, a sibling checkout;
# test/rust/Cargo.toml and its ludicrous twin carry the same relative path
SERIALIZE_RS ?= ../serialize.rs

# cargo lives in the rustup keg, which is not on PATH by default
RUSTUP_BIN ?= /opt/homebrew/opt/rustup/bin

build/packet-text/rust/.stamp: bin/schema test/packet-text/Narrow.schema test/packet-text/rust/src/main.rs make/rust.mk
	./bin/schema generate --lang rust --out build/packet-text/rust/src test/packet-text/Narrow.schema
	cp test/packet-text/rust/src/main.rs build/packet-text/rust/src/main.rs
	@printf '[package]\nname = "packettext"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" > build/packet-text/rust/Cargo.toml
	@touch $@

.PHONY: packet-utf8-rust packet-utf8-rust-negative-control
packet-utf8-rust: build/packet-text/rust/.stamp build/packet-text/cpp/driver build/packet-text/harness
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --manifest-path build/packet-text/rust/Cargo.toml
	./build/packet-text/harness ./build/packet-text/rust/target/debug/packettext
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release --manifest-path build/packet-text/rust/Cargo.toml
	./build/packet-text/harness ./build/packet-text/rust/target/release/packettext

packet-utf8-rust-negative-control: packet-utf8-rust
	@mkdir -p build/packet-text/rust-negative
	go run ./tools/sabotage -name packet-utf8-rust-read -out build/packet-text/rust-negative/functions.gotext internal/codegen/rust/functions.go
	@printf '{"Replace":{"%s/internal/codegen/rust/functions.go":"%s/build/packet-text/rust-negative/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/rust-negative/overlay.json
	go run -overlay=build/packet-text/rust-negative/overlay.json ./cmd/schema generate --lang rust --out build/packet-text/rust-negative/src test/packet-text/Narrow.schema
	cp test/packet-text/rust/src/main.rs build/packet-text/rust-negative/src/main.rs
	cp build/packet-text/rust/Cargo.toml build/packet-text/rust-negative/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release --manifest-path build/packet-text/rust-negative/Cargo.toml
	@if ./build/packet-text/harness -mutations-only ./build/packet-text/rust-negative/target/release/packettext > build/packet-text/rust-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Rust UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/rust-negative/log || { cat build/packet-text/rust-negative/log; exit 1; }
	@echo 'packet UTF-8 Rust negative control: removed read validation fails bit-flip agreement'

test-rust: packet-utf8-rust packet-utf8-rust-negative-control

generated/rust-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang rust --out generated/rust-ludicrous/src examples128
	@printf '[package]\nname = "ludicrous"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../$(SERIALIZE_RS)" }\n' > generated/rust-ludicrous/Cargo.toml
	@touch $@

# the Rust target: generated crate + manifest wiring (the Cargo.toml is build
# wiring, not schema output — the emitter writes only .rs files; the manifest
# sits one level above src/, so the runtime path gains one more ../)
generated/rust/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang rust --out generated/rust/src examples
	@printf '[package]\nname = "example"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../$(SERIALIZE_RS)" }\n' > generated/rust/Cargo.toml
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
# consumer, a block consumer, and everything. It is not a formality — a fact
# that belongs to NEITHER accelerator, the unit's BUILD VERSION and its
# blittable RECORDS among them, is unreachable from a wire-only build unless it
# has an always-compiled home of its own, and only building that combination
# says so.
.PHONY: tables-rust-features
tables-rust-features: build/tables-generated-rust/.stamp
	@for unit in build/tables-generated-rust/*/; do \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet ) || exit 1; \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --no-default-features ) || exit 1; \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --no-default-features --features cook ) || exit 1; \
		( cd $$unit && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --no-default-features --features block ) || exit 1; \
	done
	@echo "rust feature gate: minimal, cook-only, block-only and everything all build"

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

generated/bench/rust/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang rust --out generated/bench/rust/src bench/corpus/Bench.schema
	./bin/schema generate --lang rust --out generated/bench/rust-realworld/src bench/corpus/RealWorld.schema
	@printf '[package]\nname = "benchcorpus"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' > generated/bench/rust/Cargo.toml
	@printf '[package]\nname = "realworldcorpus"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' > generated/bench/rust-realworld/Cargo.toml
	@touch $@

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

# THE RUST LEG of `make test`: the clippy and feature gates, the names
# control, the big-endian check, the bench crates' compile gates, and the
# packet tests — the corpus binaries in BOTH build modes (see below).
.PHONY: test-rust
test-rust: generated/rust/.stamp generated/rust-ludicrous/.stamp generated/bench/rust/.stamp
	$(MAKE) tables-rust-clippy
	$(MAKE) tables-rust-features
	$(MAKE) tables-rust-names-negative-control
	# the generated Rust table surface CHECKED for a big-endian target, layout
	# const asserts and all. It SKIPS cleanly where the target is not
	# installed, so it costs a machine without it nothing.
	$(MAKE) tables-rust-big-endian
	cd generated/bench/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	cd generated/bench/rust-realworld && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	# BOTH BUILD MODES. The generated writer holds its caller contracts with
	# `debug_assert!` (the 2026-09-07 ruling: "checks are *DEBUG ONLY*"), so
	# the debug run is the one that proves the contracts FIRE and the release
	# run is the one that proves they are GONE and the corpus still writes the
	# pinned wire. Java, Dart and the packet-wide Rust leg already run both.
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release
	cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release

TEST_LEGS         += test-rust
CONFORMANCE_LEGS  += build/conformance-rust
BENCH_TABLES_LEGS += generated/bench/tables/rust/.stamp

# Packet defaults share the C++ oracle. Both build modes consume its byte and
# bit pins, and the control removes only constructor byte copies.
build/packet-defaults/rust/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/rust.mk
	@mkdir -p build/packet-defaults/rust
	@for unit in defaults plain; do \
		if [ "$$unit" = defaults ]; then source=Defaults; else source=Plain; fi; \
		./bin/schema generate --lang rust --out build/packet-defaults/rust/$$unit/src test/packet-defaults/$$source.schema || exit 1; \
		printf '[package]\nname = "packet%s"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../../$(SERIALIZE_RS)" }\n' $$unit > build/packet-defaults/rust/$$unit/Cargo.toml; \
	done
	@touch $@

.PHONY: packet-defaults-rust packet-defaults-rust-negative-control
packet-defaults-rust: build/packet-defaults/rust/.stamp packet-defaults-cpp
	cd test/packet-defaults/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet -- ../../../testdata/wire/packet-defaults
	cd test/packet-defaults/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release -- ../../../testdata/wire/packet-defaults

packet-defaults-rust-negative-control: packet-defaults-rust
	@mkdir -p build/packet-defaults/rust-negative
	go run ./tools/sabotage -name packet-defaults-rust-constructor-bytes \
		-out build/packet-defaults/rust-negative/rust.gotext internal/codegen/rust/rust.go
	@printf '{"Replace":{"%s/internal/codegen/rust/rust.go":"%s/build/packet-defaults/rust-negative/rust.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/rust-negative/overlay.json
	go build -overlay=build/packet-defaults/rust-negative/overlay.json -o build/packet-defaults/rust-negative/schema ./cmd/schema
	./build/packet-defaults/rust-negative/schema generate --lang rust --out build/packet-defaults/rust-negative/generated/src test/packet-defaults/Defaults.schema
	@printf '[package]\nname = "packetdefaults"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" > build/packet-defaults/rust-negative/generated/Cargo.toml
	@mkdir -p build/packet-defaults/rust-negative/checker/src
	cp test/packet-defaults/rust/src/main.rs build/packet-defaults/rust-negative/checker/src/main.rs
	@sed -e 's|../../../build/packet-defaults/rust/defaults|../generated|' \
		-e 's|../../../build/packet-defaults/rust/plain|$(CURDIR)/build/packet-defaults/rust/plain|' \
		-e 's|../../../../serialize.rs|$(CURDIR)/$(SERIALIZE_RS)|' \
		test/packet-defaults/rust/Cargo.toml > build/packet-defaults/rust-negative/checker/Cargo.toml
	cd build/packet-defaults/rust-negative/checker && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	@if ./build/packet-defaults/rust-negative/checker/target/debug/packet-defaults-test testdata/wire/packet-defaults > build/packet-defaults/rust-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in Rust'; exit 1; fi
	@grep -Fq 'packet-default constructor bytes' build/packet-defaults/rust-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Rust failed for another reason'; cat build/packet-defaults/rust-negative/log; exit 1; }
	@echo 'packet defaults Rust negative control: missing constructor bytes fail the runtime check'

test-rust: packet-defaults-rust packet-defaults-rust-negative-control
# Wide packet code units, with independent bounds, tail and composition probes.
build/packet-wide/rust/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema test/packet-wide/rust/src/main.rs make/rust.mk
	./bin/schema generate --lang rust --out build/packet-wide/rust/src build/packet-wide/source/WideText.schema
	./bin/schema generate --lang rust --out build/packet-wide/rust/shapes/src test/packet-wide/Shapes.schema
	cp test/packet-wide/rust/src/main.rs build/packet-wide/rust/src/main.rs
	@printf '[package]\nname = "wide"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nwideprobe = { path = "shapes" }\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" > build/packet-wide/rust/Cargo.toml
	@printf '[package]\nname = "wideprobe"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" > build/packet-wide/rust/shapes/Cargo.toml
	@touch $@

.PHONY: packet-wide-rust packet-wide-rust-negative-control
packet-wide-rust: build/packet-wide/rust/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	PATH="$(RUSTUP_BIN):$$PATH" cargo test --quiet --manifest-path build/packet-wide/rust/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --manifest-path build/packet-wide/rust/Cargo.toml
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/rust/target/debug/wide
	PATH="$(RUSTUP_BIN):$$PATH" cargo test --quiet --release --manifest-path build/packet-wide/rust/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release --manifest-path build/packet-wide/rust/Cargo.toml
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/rust/target/release/wide

packet-wide-rust-negative-control: packet-wide-rust
	@mkdir -p build/packet-wide/rust-negative
	go run ./tools/sabotage -name packet-wide-rust-pairing -out build/packet-wide/rust-negative/wstring.gotext internal/codegen/rust/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/rust/wstring.go":"%s/build/packet-wide/rust-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/rust-negative/overlay.json
	go run -overlay=build/packet-wide/rust-negative/overlay.json ./cmd/schema generate --lang rust --out build/packet-wide/rust-negative/src build/packet-wide/source/WideText.schema
	cp test/packet-wide/rust/src/main.rs build/packet-wide/rust-negative/src/main.rs
	@sed 's|path = "shapes"|path = "../rust/shapes"|' build/packet-wide/rust/Cargo.toml > build/packet-wide/rust-negative/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release --manifest-path build/packet-wide/rust-negative/Cargo.toml
	@if ./build/packet-text/harness -wide -mutations-only -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver ./build/packet-wide/rust-negative/target/release/wide > build/packet-wide/rust-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Rust wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/rust-negative/log || { cat build/packet-wide/rust-negative/log; exit 1; }
	@echo 'packet wide Rust negative control: removed pairing fails bit-flip agreement'

test-rust: packet-wide-rust packet-wide-rust-negative-control
