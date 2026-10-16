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
#
# AND THE 128-BIT CRATE RIDES HERE BECAUSE s390x IS WHERE IT BREAKS. ir's model
# puts a 128-bit integer at SIXTEEN BYTES ALIGNED SIXTEEN — the `alignas( 16 )`
# the C++ reference spells — while Rust's own u128 takes the TARGET's C
# alignment, which is SIXTEEN on x86-64 and aarch64 and EIGHT on s390x. A bare
# u128 in a Row therefore lands at a different offset on this target only, and
# nothing on the developer's machine would ever say so. The paired bench unit
# (bench/corpus/Bench.schema's wide_key and flux) is the crate that carries the
# family, so it is the one checked: its Row's layout const asserts are what fail
# if the aligned wrapper ever goes away.
RUST_BE_TARGET ?= s390x-unknown-linux-gnu

.PHONY: tables-rust-big-endian
tables-rust-big-endian: build/tables-generated-rust/.stamp
	@if ! PATH="$(RUSTUP_BIN):$$PATH" rustup target list --installed 2>/dev/null | grep -qx "$(RUST_BE_TARGET)"; then \
		echo "SKIP tables-rust-big-endian: $(RUST_BE_TARGET) is not installed"; \
		echo "  rustup target add $(RUST_BE_TARGET)"; \
		exit 0; \
	fi; \
	( cd test/conformance/rust && PATH="$(RUSTUP_BIN):$$PATH" \
		cargo check --quiet --target $(RUST_BE_TARGET) ) || exit 1; \
	( cd build/tables-generated-rust/benchfixed && PATH="$(RUSTUP_BIN):$$PATH" \
		cargo check --quiet --target $(RUST_BE_TARGET) ) || exit 1; \
	echo "big-endian: the generated Rust table surface checks for $(RUST_BE_TARGET), the 128-bit family with it, every layout const assert evaluated"


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

# THE RUNTIME HOME IS THE PACKAGE (docs/SPEC-TABLES.md §19.2). A unit's shared
# Rust table runtimes — the block form's runtime in block_runtime.rs, the cooked
# form's in cook_runtime.rs, and the build version in build_version.rs — are
# emitted ONCE, keyed by the module CONSTANTS (rusttable.go:26-30), so no file's
# name can reach them. Every earlier rule picked WHERE off the file order:
# adding a file that sorts earlier relocated the whole runtime (issue #347) —
# correct output, and a diff nobody can read.
#
# A unit is a CRATE here (make/rust.mk:214-215), so the crate name already IS
# the package and a module does not repeat it; a module constant off the crate
# is this leg's spelling of "named by the PACKAGE", and cook.go:113-118 says so.
#
# The gate adds exactly such a file to a COPY of tables/examples — Aaa.schema,
# ahead of Guarded.schema — and requires the homes not to move, by name and by
# byte. The added file DECLARES NOTHING: a declared type would move the unit's
# protocol id, which the build version folds in, so a moved runtime would then
# be correct rather than a defect (rusttable_test.go:190-194). What it changes
# is the FILE ORDER and nothing else.
.PHONY: tables-rust-runtime-home
tables-rust-runtime-home: bin/schema
	@rm -rf build/runtime-home-rust && mkdir -p build/runtime-home-rust/src
	@cp tables/examples/*.schema build/runtime-home-rust/src/
	@printf 'package tabledemo\n\n// a file that declares nothing, so the only thing it changes is which basename sorts first\n' > build/runtime-home-rust/src/Aaa.schema
	@./bin/schema generate --lang rust --out build/runtime-home-rust/base tables/examples
	@./bin/schema generate --lang rust --out build/runtime-home-rust/added build/runtime-home-rust/src
	@for home in "block_runtime:shared runtime (docs/SPEC-TABLES.md §19)" "cook_runtime:shared runtime (docs/SPEC-TABLES.md §7)" "build_version:BUILD VERSION (docs/SPEC-TABLES.md §20)"; do \
		mod=$${home%%:*}; needle=$${home#*:}; \
		base=$$(cd build/runtime-home-rust/base && grep -l "$$needle" *.rs); \
		added=$$(cd build/runtime-home-rust/added && grep -l "$$needle" *.rs); \
		if [ "$$base" != "$$mod.rs" ] || [ "$$added" != "$$mod.rs" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$mod runtime is in $$base before the added file and $$added after — expected $$mod.rs both times"; exit 1; \
		fi; \
	done
	@for mod in block_runtime cook_runtime build_version; do \
		cmp -s build/runtime-home-rust/base/$$mod.rs build/runtime-home-rust/added/$$mod.rs || \
			{ echo "RUNTIME HOME GATE FAILED: $$mod.rs's bytes moved when the unit gained a file"; exit 1; }; \
	done
	@echo "runtime home gate (Rust): the block, cook and build-version runtimes stay in block_runtime.rs, cook_runtime.rs and build_version.rs when an earlier-sorting file joins the unit"

.PHONY: tables-rust-runtime-home-negative-control
tables-rust-runtime-home-negative-control: bin/schema tables-rust-runtime-home
	@rm -rf build/rust-runtime-home-control && mkdir -p build/rust-runtime-home-control
	@sed 's|out\[BlockRuntimeModule+".rs"\]|out[strings.ToLower(ir.ProtocolIdHome(u))+"_block_runtime.rs" /* SABOTAGED: back to the file order */]|' \
		internal/codegen/rusttable/block.go > build/rust-runtime-home-control/block.go.txt
	@[ "$$(grep -c SABOTAGED build/rust-runtime-home-control/block.go.txt)" = "1" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the file-order sabotage did not apply exactly once"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/rusttable/block.go":"%s/build/rust-runtime-home-control/block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/rust-runtime-home-control/overlay.json
	@go build -overlay=build/rust-runtime-home-control/overlay.json -o build/schema-rustruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-rust/base-sabotage build/runtime-home-rust/added-sabotage
	@./build/schema-rustruntime-sabotaged generate --lang rust --out build/runtime-home-rust/base-sabotage tables/examples
	@./build/schema-rustruntime-sabotaged generate --lang rust --out build/runtime-home-rust/added-sabotage build/runtime-home-rust/src
	@base=$$(cd build/runtime-home-rust/base-sabotage && grep -l "shared runtime (docs/SPEC-TABLES.md §19)" *.rs); \
	 added=$$(cd build/runtime-home-rust/added-sabotage && grep -l "shared runtime (docs/SPEC-TABLES.md §19)" *.rs); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the block runtime in $$base - the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "rust runtime home negative control: the file-order rule moves the block runtime from $$base to $$added"

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
	blobdemo:tables/blobs mapdemo:tables/maps scalars:tables/scalars \
	blockdemo:tables/block blockhome:tables/blockhome \
	tblv1:test/tables/V1.schema tblv2:test/tables/V2.schema \
	tblp1:test/tables/P1.schema tblp2:test/tables/P2.schema \
	tblp3:test/tables/P3.schema jsonkeys:test/tables/JsonKeys.schema \
	tblfx1:test/tables/FX1.schema tblfx2:test/tables/FX2.schema \
	tblfe1:test/tables/FE1.schema tblfe2:test/tables/FE2.schema \
	tblfu1:test/tables/FU1.schema tblfu2:test/tables/FU2.schema \
	tblw1:test/tables/W1.schema tblw2:test/tables/W2.schema \
	tblr1:test/tables/R1.schema tblr2:test/tables/R2.schema

# THE PAIRED BENCH UNIT is TWO SCHEMA FILES AS ONE UNIT
# (bench/corpus/FixedTable.schema wraps Bench.schema's own BenchMixed), so it
# cannot ride the name:path loop above and gets its own line. It is the unit
# that carries a UNION through the fixed form — BenchMixed's `game_event` — and
# the C++ reference has already written its 64 records to
# bench/paired/corpus/bench_fixed.bin, so this crate is what holds Rust to
# those bytes (docs/SPEC-TABLES.md §3.4, §15).
RUST_TABLE_BENCH_SCHEMAS := bench/corpus/Bench.schema bench/corpus/FixedTable.schema

build/tables-generated-rust/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_SCALARS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema test/tables/FX1.schema test/tables/FX2.schema test/tables/FE1.schema test/tables/FE2.schema test/tables/FU1.schema test/tables/FU2.schema test/tables/W1.schema test/tables/W2.schema test/tables/R1.schema test/tables/R2.schema $(RUST_TABLE_BENCH_SCHEMAS)
	@mkdir -p build/tables-generated-rust
	@for unit in $(RUST_TABLE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		rm -rf build/tables-generated-rust/$$name/src; \
		./bin/schema generate --lang rust --out build/tables-generated-rust/$$name/src $$path || exit 1; \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' $$name \
			> build/tables-generated-rust/$$name/Cargo.toml; \
	done
	@rm -rf build/tables-generated-rust/benchfixed/src
	./bin/schema generate --lang rust --out build/tables-generated-rust/benchfixed/src $(RUST_TABLE_BENCH_SCHEMAS)
	@printf '[package]\nname = "benchfixed"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' \
		> build/tables-generated-rust/benchfixed/Cargo.toml
	@touch $@

# THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4). The C++ reference
# writes the bytes (`make tables-fixedform-corpus`) and this leg holds Rust to
# them: every file read and saved back has to come out BYTE FOR BYTE the same,
# and every cross-schema case reads a record written under ANOTHER block
# through a plan compiled from it — a widened field, a rename under `was`, a
# field this reader cannot name, a field the writer does not carry, a whole
# nested TYPE stepped over by its block size, and an optional against a value.
# The negative controls ride in the same binary, because a leg that never
# watched the wrong plan fail never checked the right one worked.
#
# BOTH BUILD MODES, for the reason the packet corpus already runs both: the
# generated writer's caller contracts are debug-only, so debug proves they FIRE
# and release proves they are GONE and the bytes are still the reference's.
.PHONY: tables-rust-fixedform
tables-rust-fixedform: build/tables-generated-rust/.stamp build/fixedform-corpus/.stamp
	cd test/rust-fixedform && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet -- ../../build/fixedform-corpus ../../bench/paired/corpus
	cd test/rust-fixedform && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet --release -- ../../build/fixedform-corpus ../../bench/paired/corpus

# THE VERSIONING HALF OF THE FIXED FORM ON THE RUST LEG (§5 of
# docs/FIXED-FORM-ALGORITHM.md, the rows of docs/FIXED-FORM-VERSIONING-TESTS.md).
# internal/codegen/rusttable/fixedversioning_test.go reads the C++ reference's
# byte oracle out of build/fixedform-corpus — `old_<row>.bin`, `new_<row>.bin`
# and the floor, hash and lineage-merge files — generates ONE CARGO WORKSPACE of
# probe crates under build/rust-versioning-probes, and holds each row's two read
# columns: NEW-READS-OLD lands every old value with §5.4's counters, and
# OLD-REFUSES-NEW answers `layout_newer` before any record, on the file's hash
# alone (§5.3). The Go leg's `tables-go-versioning` is the same target on the
# same rows; this is its twin.
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE. The suite's
# harness SKIPS itself when build/fixedform-corpus is absent, which is right for
# a bare `go test ./...` on a tree that never built the oracle — and is exactly
# how a §5 regression would have ridden into CI green, since the go-test job
# runs nothing but `go test ./...`. So the target BUILDS THE ORACLE FIRST
# (`tables-fixedform-corpus`, the C++ reference's own dump, about 7 s) and then
# sets SCHEMA_REQUIRE_CORPUS=1, which turns that skip into a FAILURE: under this
# name a missing corpus can never pass silently.
#
# AND IT IS A GO TEST THAT SHELLS OUT TO CARGO, so the PATH carries $(RUSTUP_BIN)
# the way every other Rust row in this file does: the suite's one `cargo test
# --workspace` resolves cargo out of the environment it is handed and nothing
# else. One workspace and ONE cargo invocation is the two-minute rule — fifty
# separate `cargo run`s would serialize on the target directory's build lock and
# compile `serialize-official` behind each of them.
.PHONY: tables-rust-versioning
tables-rust-versioning: tables-fixedform-corpus
	PATH="$(RUSTUP_BIN):$$PATH" SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/codegen/rusttable/ -count=1 -timeout 20m -run 'TestFixedVersioning|TestFixedRoadmapRustT12FramingText'
	@echo 'tables Rust versioning: §5 read both columns of every row against the C++ reference bytes'

build/conformance-rust: build/tables-generated-rust/.stamp test/conformance/rust/src/main.rs test/conformance/rust/Cargo.toml
	@mkdir -p build
	cd test/conformance/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	@rm -f $@   # replace the inode: writing over a binary another process is
	            # running corrupts it in place, and a long soak runs this one
	cp test/conformance/rust/target/debug/conformance-rust $@

# THE PAIRED BENCH UNIT, for bench/tables/rust — the FIXED FORM's leg in
# `bench/paired` (bench/paired/main.go, bench/tables/rust/src/main.rs).
# Bench.schema and FixedTable.schema are ONE unit, exactly as the C, C++, Go
# and C# paired generations are: the fixed root reaches the packet type so the
# compiler emits its table codec. The manifest beside the modules is TRACKED
# build wiring rather than something this stamp writes — the paired unit is
# generated to the crate ROOT (that is the --out path `bench/paired` passes for
# every language), so its Cargo.toml names `[lib] path = "lib.rs"` and does not
# change when the emitter adds or drops a module. The driver regenerates the
# same .rs files itself, so the two agree byte for byte or `generated-current`
# says so.
#
# IT IS THE ONE RUST UNIT WHOSE MANIFEST IS NOT WRITTEN WITH $(SERIALIZE_RS)
# SUBSTITUTED IN, so it names build/paired/serialize.rs — a symlink — instead,
# and the two places that build the crate point that link first
# (`tables-rust-fixed-matched` below, and bench/paired's own build step). That
# is what keeps a tracked file from moving on a checkout that is not beside
# serialize.rs: the paired driver refuses a dirty checkpoint.
generated/bench/paired/rust/.stamp: bin/schema $(RUST_TABLE_BENCH_SCHEMAS)
	@mkdir -p generated/bench/paired/rust
	./bin/schema generate --lang rust --out generated/bench/paired/rust $(RUST_TABLE_BENCH_SCHEMAS)
	@touch $@

# THE FIXED FORM'S PAIRED LEG, gated and not timed: the same no-clock `--gate`
# the C++ and C legs answer in `tables-fixed-matched`, over the same corpus.
# The clock lives in `go run ./bench/paired`, and a clock does not gate a build.
#
# RELEASE, because that is the build the driver measures and the build whose
# write-side `debug_assert!`s are gone (the 2026-09-07 ruling) — the debug
# build's contracts are already proved by `tables-rust-fixedform`, which runs
# both modes over these same bytes.
.PHONY: tables-rust-fixed-matched
tables-rust-fixed-matched: generated/bench/paired/rust/.stamp
	@mkdir -p build/paired
	@ln -sfn "$(abspath $(SERIALIZE_RS))" build/paired/serialize.rs
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release \
		--manifest-path bench/tables/rust/Cargo.toml --target-dir build/paired/rust
	./build/paired/rust/release/table-rust --gate --indexed \
		--wire-dir bench/paired/corpus --variant-dir bench/paired/corpus

# THE RUST TABLES SOAK (docs/PORTING.md I9, issue #416), the twin of
# tables-c-soak over the same units: Load, the reads, Measure and Save on the
# root of every case for SOAK_SECONDS of real time, under a counting
# #[global_allocator] (test/rust-soak/src/main.rs). The GATE IS THE CALL COUNT,
# taken at every sample: it must not move after the warmup pass. The live-heap
# byte count is gated beside it (it sees retention), and the resident set is
# reported as the weaker instrument. It is a FUNCTIONAL soak — real seconds —
# so it is this target and never a unit test; `make test` runs 20 s through
# test-rust, and the hour is the release act (`make tables-rust-soak
# SOAK_SECONDS=3600`, the Makefile's default).
#
# THE FILES ARE THE FIXED FORM'S. This port's table surface for these units is
# form byte 3, and a fixed reader refuses the tolerant wire the C soak reads by
# name, so the soak reads the C++ reference's own dump where it writes the root
# (build/fixedform-corpus) and writes the rest itself, before the clock.
#
# THE CRATE IS COPIED INTO build/ AND BUILT THERE, so its Cargo.lock and target/
# land in the ignored tree and the checkout stays clean. Its path dependencies
# resolve the same from build/rust-soak as from test/rust-soak: both sit two
# levels below the root. RELEASE, because that is the build a caller ships and
# the one whose write-side debug_asserts are gone.
RUST_SOAK_SRCS := test/rust-soak/Cargo.toml test/rust-soak/src/main.rs

.PHONY: tables-rust-soak
tables-rust-soak: build/tables-generated-rust/.stamp build/fixedform-corpus/.stamp $(RUST_SOAK_SRCS)
	@mkdir -p build/rust-soak/src
	@cp test/rust-soak/Cargo.toml build/rust-soak/Cargo.toml
	@cp test/rust-soak/src/main.rs build/rust-soak/src/main.rs
	cd build/rust-soak && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release
	./build/rust-soak/target/release/rust-soak $(SOAK_SECONDS) build/fixedform-corpus

# THE SOAK'S NEGATIVE CONTROL, the twin of tables-c-soak-negative-control. The
# live-byte sample is a LEAK instrument: a matched alloc/free pair inside the
# loop returns it to where it was before it is ever read, so it reads flat
# forever. The CALL COUNT is what makes the claim, and a counter nobody has
# watched go red is a counter nobody can size.
#
# The pair is planted through the crate's `negative-control` feature (a cfg
# flag in the soak binary, never a change to generated code): one Box per case
# per iteration, freed at once, held past the optimiser by black_box. The run
# must exit non-zero ON THE COUNT, and the drift line must run and stay flat —
# that silence is the half being demonstrated.
.PHONY: tables-rust-soak-negative-control
tables-rust-soak-negative-control: build/tables-generated-rust/.stamp build/fixedform-corpus/.stamp $(RUST_SOAK_SRCS)
	@mkdir -p build/rust-soak-sabotage/src
	@cp test/rust-soak/Cargo.toml build/rust-soak-sabotage/Cargo.toml
	@cp test/rust-soak/src/main.rs build/rust-soak-sabotage/src/main.rs
	@grep -q '#\[cfg(feature = "negative-control")\]' build/rust-soak-sabotage/src/main.rs || \
		{ echo "NEGATIVE CONTROL: the soak carries no planted pair to turn on"; exit 1; }
	cd build/rust-soak-sabotage && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --release --features negative-control
	@if ./build/rust-soak-sabotage/target/release/rust-soak 2 build/fixedform-corpus > build/rust-soak-sabotage/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a matched alloc/free pair per iteration left the soak green"; \
		cat build/rust-soak-sabotage/log; exit 1; \
	fi
	@grep -q "SABOTAGED build" build/rust-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the negative-control feature did not reach the binary"; \
		  cat build/rust-soak-sabotage/log; exit 1; }
	@grep -q "SOAK FAILED: the allocator call count moved" build/rust-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak went red, but not on the call count"; \
		  cat build/rust-soak-sabotage/log; exit 1; }
	@grep -q "live allocation" build/rust-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the drift half did not run"; cat build/rust-soak-sabotage/log; exit 1; }
	@! grep -q "SOAK FAILED: live allocation moved" build/rust-soak-sabotage/log || \
		{ echo "NEGATIVE CONTROL FAILED: the drift gate went red, so the planted pair is not matched"; \
		  cat build/rust-soak-sabotage/log; exit 1; }
	@grep "live allocation" build/rust-soak-sabotage/log
	@grep -m1 "SOAK FAILED" build/rust-soak-sabotage/log
	@echo "negative control: a matched alloc/free pair per iteration is INVISIBLE to the drift gate and turns the Rust soak's CALL COUNT red"

# THE RUST LEG of `make test`: the clippy and feature gates, the names
# control, the big-endian check, the bench crates' compile gates, and the
# packet tests — the corpus binaries in BOTH build modes (see below).
.PHONY: test-rust
test-rust: generated/rust/.stamp generated/rust-ludicrous/.stamp generated/bench/rust/.stamp generated/bench/paired/rust/.stamp
	$(MAKE) tables-rust-clippy
	$(MAKE) tables-rust-features
	$(MAKE) tables-rust-names-negative-control
	# THE FIXED FORM against the C++ reference's own bytes (§3.4)
	$(MAKE) tables-rust-fixedform
	# AND ITS VERSIONING HALF (§5): every row of
	# docs/FIXED-FORM-VERSIONING-TESTS.md, both read columns, over the same
	# reference bytes. `tables-rust-fixedform` above proves this leg reads the
	# corpus; this proves it reads BACKWARD and refuses FORWARD.
	$(MAKE) tables-rust-versioning
	# THE PAIRED LEG's own no-clock gate (bench/tables/rust): the block is the
	# corpus's, the file loads, it saves back identical, reused storage
	# round-trips twice.
	$(MAKE) tables-rust-fixed-matched
	# THE SOAK (docs/PORTING.md I9): 20 s gated on the allocator call count,
	# and the control that watches the count go red on a planted pair
	$(MAKE) tables-rust-soak SOAK_SECONDS=20
	$(MAKE) tables-rust-soak-negative-control
	$(MAKE) tables-rust-runtime-home
	$(MAKE) tables-rust-runtime-home-negative-control
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

# THE RUST VARIABLE-CLASS GATE (docs/SPEC-TABLES.md §2.5, §2.9, §3, §3.1,
# §16.7): the constructs the card record does not use but the variable class
# carries — table pointers over the flat node table, tables nested by value,
# blobs, unbounded and bounded arrays of scalar, enum, table and pointer
# elements, the rest of the scalar kinds, bytes(N), the escape kind and the
# text form's &node sharing — held byte for byte against the Go oracle over
# tables/pointers, tables/blobs and tables/lists by
# TestVariableClassBeyondCardConstructsRust, and every construct the leg still
# refuses named by TestOtherVariableConstructsAreRefusedByName.
# SCHEMA_REQUIRE_CORPUS=1 turns the rustc skip into a failure, so the target
# never passes on a SKIP.
.PHONY: tables-rust-variable-class
tables-rust-variable-class:
	PATH="$(RUSTUP_BIN):$$PATH" SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/codegen/rusttable/ -count=1 -timeout 20m \
		-run 'TestVariableClassBeyondCardConstructsRust|TestOtherVariableConstructsAreRefusedByName' -v
	@echo 'tables Rust variable class: the class beyond the card constructs round-trips and the rest is refused by name'

test-rust: tables-rust-variable-class

# THE BLOCK BUILD AND THE COOK WRITE (docs/SPEC-TABLES.md §19.1, §7.6): the
# Rust leg's producer surfaces, pinned to the Go builder and Go's cook writer
# (gotable/blockbuild.go, gotable/cookwrite.go) by
# TestBlockBuildAndCookWriteRust, which recomputes every emitted constant from
# the same ir layouts the Go emitter holds. SCHEMA_REQUIRE_CORPUS=1 makes the
# functional test fail by name rather than skip, so this target never passes
# on a SKIP.
tables-rust-block-build:
	PATH="$(RUSTUP_BIN):$$PATH" SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/codegen/rusttable/ -count=1 -timeout 20m \
		-run TestBlockBuildAndCookWriteRust -v
	@echo 'tables Rust block build and cook write: the producer surfaces match the Go reference'

test-rust: tables-rust-block-build

# Maps and union arms on the variable wire (docs/SPEC-TABLES.md §2.6, §2.8, §3).
# SCHEMA_REQUIRE_CORPUS=1 turns the rustc skip into a failure, so the target
# never passes on a SKIP.
.PHONY: tables-rust-variable-arms
tables-rust-variable-arms:
	PATH="$(RUSTUP_BIN):$$PATH" SCHEMA_REQUIRE_CORPUS=1 \
		go test ./internal/codegen/rusttable/ -count=1 -timeout 20m \
		-run 'TestMapsAndUnionArmsOnTheTableWireRust|TestOtherVariableConstructsAreRefusedByName' -v
	@echo 'tables Rust variable arms: maps and union arms round-trip and the rest is refused by name'

test-rust: tables-rust-variable-arms

# THE FIXED CLASS ON THE TABLE WIRE (docs/SPEC-TABLES.md §2.2, §3, §4.2, §5):
# a `fixed table` rides the VARIABLE FORM as well as the fixed one, so a
# closure holding a fixed table gets the form-1 codec — load, save and the text
# verbs — byte for byte against the Go oracle (internal/tablewire, the bytes
# internal/codegen/gotable/wire.go writes): the V, W and R evolution pairs, the
# string, bytes and flags defaults, the table rename and the variant, arm and
# type-field renames under `was`, every report row equal to the oracle's.
# TestAcceleratorsEmitted holds that a fixed-table unit emits the form-1
# runtime. SCHEMA_REQUIRE_CORPUS=1 turns the rustc skip into a failure, so the
# target never passes on a SKIP.
.PHONY: tables-rust-variable-fixed-class
tables-rust-variable-fixed-class:
	PATH="$(RUSTUP_BIN):$$PATH" SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/rusttable/ -count=1 -timeout 20m -run 'TestFixedClassOnTableWireRust|TestAcceleratorsEmitted' -v
	@echo 'tables Rust variable fixed class: a fixed table carries the form-1 codec, its defaults and its renames, byte for byte against the oracle'

test-rust: tables-rust-variable-fixed-class

# THE RUST UNIT REGISTRY GATE (docs/SPEC-TABLES.md §8.7): the same registry
# listing oracle as the reference, through the Rust surface, mirroring
# tables-c-view. For each VIEW_CORPUS unit the target generates the unit with
# bin/schema into build/rust-view/unit, builds test/rust-view (copied beside
# it, so its Cargo.lock and target stay in build/) against that unit and
# nothing else, writes build/rust-view/<pkg>.listing, and the listing gate
# compares every listing with the one the compiler produces from its own IR.
#
# A UNIT THE RUST LEG REFUSES WHOLE is named, never skipped silently: its
# refusal must be the leg's own named follow-on (today `was` on an enum
# variant, a union arm or a type's field, docs/SPEC-TABLES.md §5, which is
# messages and wide), the gate compares every unit it carries, and any other
# generate failure fails the target.
.PHONY: tables-rust-view
tables-rust-view: bin/schema test/rust-view/src/main.rs test/rust-view/Cargo.toml
	@rm -rf build/rust-view
	@mkdir -p build/rust-view/prog/src
	@cp test/rust-view/Cargo.toml build/rust-view/prog/Cargo.toml
	@cp test/rust-view/src/main.rs build/rust-view/prog/src/main.rs
	@set -e; carried=; refused=; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		source=tables/$$dir; if [ "$$dir" = wide ]; then source=examples-wide; fi; \
		rm -rf build/rust-view/unit; mkdir -p build/rust-view/unit; \
		if ! ./bin/schema generate --lang rust --out build/rust-view/unit/src $$source > build/rust-view/$$pkg.generate.log 2>&1; then \
			if grep -q 'the rust form is a named follow-on' build/rust-view/$$pkg.generate.log; then \
				echo "tables-rust-view: $$pkg is refused whole by the Rust leg, by name: $$(grep -o 'unit declares was on [^;]*' build/rust-view/$$pkg.generate.log)"; \
				refused="$$refused $$pkg"; continue; \
			fi; \
			cat build/rust-view/$$pkg.generate.log; exit 1; \
		fi; \
		printf '[package]\nname = "viewunit"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" \
			> build/rust-view/unit/Cargo.toml; \
		PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet --manifest-path build/rust-view/prog/Cargo.toml; \
		./build/rust-view/prog/target/debug/rust-view > build/rust-view/$$pkg.listing; \
		carried="$$carried,$$pkg"; \
	done; \
	echo "$$carried" | cut -c2- > build/rust-view/carried; echo "$$refused" > build/rust-view/refused
	SCHEMA_VIEW_LISTING_DIR="$(CURDIR)/build/rust-view" SCHEMA_VIEW_LISTING_UNITS="$$(cat build/rust-view/carried)" \
		go test ./internal/viewlisting -count=1 -timeout 600s -run TestUnitViewListingMatchesTheIR -v
	@echo "Rust unit registry: $$(tr ',' ' ' < build/rust-view/carried | wc -w) of $(words $(VIEW_CORPUS)) VIEW_CORPUS units match the independent listing; refused whole by name:$$(cat build/rust-view/refused)"

test-rust: tables-rust-view

# THE CARDS UNIT'S RUST LEG (docs/SPEC-TABLES.md §2). These targets are the
# Rust side of the cards wire-fuzz gate: they mirror the Go leg's
# cards-generate and cards-wire-fuzz (make/go.mk) against the same oracle
# harness and the same seed, so the Rust Card codec is held to the identical
# byte-for-byte contract. They live in make/rust.mk (NOT a separate
# make/cards-rust.mk) because the wide-scalar refusal gate
# (tables-ports-refuse-wide-scalars) discovers every make/*.mk as a live port,
# and a fragment named cards-rust.mk would be refused there as a phantom port.
CARDS_RUST_FUZZ_TIME ?= 60s

# cards-rust-generate produces ONLY build/tables-generated-rust/cards, cleaning
# that owned output first so a deleted symbol cannot survive a regeneration.
.PHONY: cards-rust-generate
cards-rust-generate: bin/schema
	@rm -rf build/tables-generated-rust/cards
	@mkdir -p build/tables-generated-rust/cards/src
	./bin/schema generate --lang rust --out build/tables-generated-rust/cards/src tables/cards

# build/conformance-rust-card is the wire-fuzz driver: the hand-written main in
# test/conformance/rust/src/wire_fuzz.rs compiled beside the card's generated
# wire modules, exactly the way this port's tests run generated code.
build/conformance-rust-card: cards-rust-generate test/conformance/rust/src/wire_fuzz.rs
	@mkdir -p build/cards-rust-fuzz
	@cp build/tables-generated-rust/cards/src/variable_runtime.rs build/cards-rust-fuzz/
	@cp build/tables-generated-rust/cards/src/card_table.rs build/cards-rust-fuzz/
	@cp build/tables-generated-rust/cards/src/card_variable.rs build/cards-rust-fuzz/
	@cp test/conformance/rust/src/wire_fuzz.rs build/cards-rust-fuzz/main.rs
	@rustc --edition 2024 -O -o build/conformance-rust-card build/cards-rust-fuzz/main.rs

# THE CARD WIRE-FUZZ GATE: the oracle harness over the Card root only, the same
# shape as make/go.mk's cards-wire-fuzz. --vectors '' keeps the default pinned
# vectors out of this Card-only manifest. The greps hold the nonzero
# seed/mutant claim and the zero-divergence claim explicitly.
cards-rust-wire-fuzz: build/conformance-harness build/conformance-rust-card
	@timeout $(CARDS_RUST_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-rust-card wire-fuzz' --seed 24845619678 --n 100 \
		> build/cards-rust-wire-fuzz.log 2>&1; \
		status=$$?; cat build/cards-rust-wire-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-rust-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-rust-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-rust-wire-fuzz.log

# THE CARD WIRE-FUZZ GATE, RETAINED (docs/SPEC-TABLES.md §6.6): the same Card
# root through both engines' RETAINING paths — the driver answers --retain with
# card_load_retain and card_save_retain over the caller-owned stores — compared
# against the oracle's own retention. The seed, count and coverage greps are the
# wire-fuzz arm's above. Like cards-rust-wire-fuzz this lives in make/rust.mk
# rather than a separate fragment: the wide-scalar refusal gate
# (tables-ports-refuse-wide-scalars) discovers every make/*.mk as a live port,
# and a fragment named cards-rust-retain.mk would be refused there as a phantom
# port.
.PHONY: cards-rust-retain-fuzz
cards-rust-retain-fuzz: build/conformance-harness build/conformance-rust-card
	@timeout $(CARDS_RUST_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
		--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
		--driver 'build/conformance-rust-card wire-fuzz' --retain --seed 24845619678 --n 100 \
		> build/cards-rust-retain-fuzz.log 2>&1; \
		status=$$?; cat build/cards-rust-retain-fuzz.log; \
		if [ $$status -ne 0 ]; then exit $$status; fi
	@grep -q '0 divergences' build/cards-rust-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-rust-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-rust-retain-fuzz.log

# THE CARDS FACADE'S RUST LEG (docs/SPEC-TABLES.md §2): L1 is the owning
# facade and the customer bounds (table/cards/cards.go), L2 parse once and
# render many (parse.go/render.go). cards-rust-test builds and tests the facade
# beside the generated card modules copied into build/cards-rust/, with no
# crate dependency beyond the standard library and the generated modules;
# clippy runs over the facade with warnings denied.
.PHONY: cards-rust-test
cards-rust-test: cards-rust-generate
	@mkdir -p build/cards-rust
	@cp build/tables-generated-rust/cards/src/variable_runtime.rs build/cards-rust/
	@cp build/tables-generated-rust/cards/src/card_table.rs build/cards-rust/
	@cp build/tables-generated-rust/cards/src/card_variable.rs build/cards-rust/
	@cp build/tables-generated-rust/cards/src/card_records.rs build/cards-rust/
	@cp build/tables-generated-rust/cards/src/card.rs build/cards-rust/
	@cp build/tables-generated-rust/cards/src/card_descriptors.rs build/cards-rust/
	@cp tables/cards/rust/Cargo.toml build/cards-rust/
	@cp tables/cards/rust/src/lib.rs build/cards-rust/lib.rs
	@cp tables/cards/rust/tests/cards.rs build/cards-rust/tests.rs
	@mkdir -p build/cards-rust/src/bin
	@cp tables/cards/rust/src/bin/cards-driver.rs build/cards-rust/src/bin/cards-driver.rs
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --offline --manifest-path build/cards-rust/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo test --offline --manifest-path build/cards-rust/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo clippy --manifest-path build/cards-rust/Cargo.toml -- -D warnings

# THE CARD CONFORMANCE CHECK (tables/cards/conformance/FORMAT.md): the Rust
# leg's driver over all nine surfaces (load, save, render, parse, bounds,
# upgrade, edit, taskhash, view), compared byte for byte against the committed
# Go answers. The driver binary rides build/cards-rust/target/debug/cards-driver,
# built by the cards-rust-test cargo build above.
.PHONY: cards-rust-conformance
cards-rust-conformance: build/conformance-harness cards-rust-test
	./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/rust/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view

# THE NEGATIVE CONTROL: the same check with a test-only environment switch
# (CARDS_RUST_SABOTAGE_RENDER) that changes one rendered byte, required to FAIL
# on render while the other surfaces pass.
.PHONY: cards-rust-conformance-negative-control
cards-rust-conformance-negative-control: build/conformance-harness cards-rust-test
	@mkdir -p build/cards-rust-conformance-neg
	@if CARDS_RUST_SABOTAGE_RENDER=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/rust/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-rust-conformance-neg/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one rendered byte changed'; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-rust-conformance-neg/log || { echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-rust-conformance-neg/log; exit 1; }
	@grep -qE 'cards (load|save|parse|bounds|upgrade|edit|taskhash|view) pass' build/cards-rust-conformance-neg/log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-rust-conformance-neg/log; exit 1; }
	@echo 'cards Rust negative control: one changed rendered byte turns exactly render red'

# THE UPGRADE NEGATIVE CONTROL: a test-only switch (CARDS_RUST_SABOTAGE_UPGRADE)
# that changes one upgraded byte must FAIL upgrade alone while every other
# surface passes.
.PHONY: cards-rust-conformance-upgrade-negative-control
cards-rust-conformance-upgrade-negative-control: build/conformance-harness cards-rust-test
	@mkdir -p build/cards-rust-conformance-neg-upgrade
	@if CARDS_RUST_SABOTAGE_UPGRADE=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/rust/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-rust-conformance-neg-upgrade/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one upgraded byte changed'; exit 1; fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-rust-conformance-neg-upgrade/log || { echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-rust-conformance-neg-upgrade/log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|edit|taskhash|view) pass' build/cards-rust-conformance-neg-upgrade/log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-rust-conformance-neg-upgrade/log; exit 1; }
	@echo 'cards Rust upgrade negative control: one changed upgraded byte turns exactly upgrade red'

# THE TASKHASH NEGATIVE CONTROL: a test-only switch (CARDS_RUST_SABOTAGE_HASH)
# that adds a needs byte to the hash must FAIL taskhash alone while every other
# surface passes.
.PHONY: cards-rust-conformance-hash-negative-control
cards-rust-conformance-hash-negative-control: build/conformance-harness cards-rust-test
	@mkdir -p build/cards-rust-conformance-neg-hash
	@if CARDS_RUST_SABOTAGE_HASH=1 ./build/conformance-harness cards --corpus tables/cards/conformance --driver 'tables/cards/rust/driver' --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > build/cards-rust-conformance-neg-hash/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one hash digit changed'; exit 1; fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-rust-conformance-neg-hash/log || { echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-rust-conformance-neg-hash/log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|upgrade|edit|view) pass' build/cards-rust-conformance-neg-hash/log || { echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-rust-conformance-neg-hash/log; exit 1; }
	@echo 'cards Rust taskhash negative control: one changed hash digit turns exactly taskhash red'

test-rust: cards-rust-test cards-rust-conformance cards-rust-conformance-negative-control
test-rust: cards-rust-conformance-upgrade-negative-control cards-rust-conformance-hash-negative-control
test-rust: cards-rust-wire-fuzz cards-rust-retain-fuzz
