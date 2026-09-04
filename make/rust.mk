# make/rust.mk — the Rust leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.

# the serialize.rs runtime the generated Rust targets, a sibling checkout;
# test/rust/Cargo.toml and its ludicrous twin carry the same relative path
SERIALIZE_RS ?= ../serialize.rs

# cargo lives in the rustup keg, which is not on PATH by default
RUSTUP_BIN ?= /opt/homebrew/opt/rustup/bin

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
# bytes every iteration reads +0 there forever, however many allocations it
# makes. SOAK_SABOTAGE puts ONE allocation per iteration inside the measured
# region and both gates must go red on it.
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
# It is a by-hand gate at an hour (the Makefile's SOAK_SECONDS); the number
# the port was landed on is 3600.

# THE RUST LEG of `make test`: the walk, clippy and feature gates, the names
# control, the allocation audit and its control, the big-endian check, the
# bench crates' compile gates, and the packet tests.
.PHONY: test-rust
test-rust: generated/rust/.stamp generated/rust-ludicrous/.stamp generated/bench/rust/.stamp
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
	cd generated/bench/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	cd generated/bench/rust-realworld && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	cd test/rust && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet
	cd test/rust-ludicrous && PATH="$(RUSTUP_BIN):$$PATH" cargo run --quiet

TEST_LEGS         += test-rust
CONFORMANCE_LEGS  += build/conformance-rust
BENCH_TABLES_LEGS += generated/bench/tables/rust/.stamp

# THE RUST NATIVE GATE (issue #547). rustfmt and clippy are the two
# instruments a Rust reader runs, and this holds the generated crates of both
# corpora to them: `cargo fmt --check`, which has no options to argue about,
# and `cargo clippy` with `-D warnings`, which is what "red on any finding"
# means here.
#
# IT IS NOT tables-rust-clippy, and neither replaces the other. That target
# asks whether a CONSUMER's build survives the generated code, so it runs
# clippy's default exit status and stays green on a lint; this one asks
# whether the emitted text is what a Rust author would have written, so a lint
# is the finding. The cost of the difference is named: clippy's lint set moves
# with the stable toolchain, so this leg can go red on a rustup release with
# no commit here, exactly as govulncheck can. That is the gate the law asks
# for, and the pin question belongs to the workflow that installs the
# toolchain.
.PHONY: native-rust
native-rust: generated/rust/.stamp generated/rust-ludicrous/.stamp build/tables-generated-rust/.stamp
	@for c in generated/rust generated/rust-ludicrous build/tables-generated-rust/*/; do \
		test -f $$c/Cargo.toml || continue; \
		echo "native-rust: $$c"; \
		( cd $$c && PATH="$(RUSTUP_BIN):$$PATH" cargo fmt --check ) || exit 1; \
		( cd $$c && PATH="$(RUSTUP_BIN):$$PATH" cargo clippy --quiet --all-targets -- -D warnings ) || exit 1; \
	done
	@echo "native Rust: rustfmt-canonical and clippy clean over the examples and tables corpora"

NATIVE_LEGS       += native-rust
