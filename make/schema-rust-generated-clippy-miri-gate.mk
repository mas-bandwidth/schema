# make/schema-rust-generated-clippy-miri-gate.mk — the Rust generated-output
# lens (ideas roadmap, schema-per-language-lenses; Glenn 2026-10-18).
#
# Schema generates the same algorithm in many languages, so the strictest
# checker of each language is a separate lens on the generator. This card's
# lens is Rust: clippy -D warnings -W clippy::pedantic over the runtime and
# every generated unit of the tables corpus, Miri over the unit tests, and a
# planted-bad-pattern control proving the clippy blade actually cuts.
#
# THIS FILE IS NOT A COMPILER BACKEND. Every make/*.mk is discovered by
# tables-ports-refuse-wide-scalars as a live port, so the Makefile includes
# this one by path and filters it out of that registry wildcard, exactly as it
# already does for tools/agentsmap/map.mk and tools/roadmap/roadmap.mk.
#
# TOOLCHAIN. clippy is part of the pinned stable Rust the fleet installs
# (SCHEMA_RUST_PIN); Miri needs nightly plus the miri component. Both legs
# PROBE FIRST and refuse by name — a gate that passes while its checker is
# missing is a gate with no blade (issue #599). The Makefile never installs a
# toolchain; the fleet play does.
SCHEMA_RUST_PIN             ?= 1.98.1
SCHEMA_RUST_SERIALIZE_TAG   ?= v2.4.0
SCHEMA_RUST_SERIALIZE_REPO  ?= https://github.com/mas-bandwidth/serialize.rs.git
SCHEMA_RUST_SERIALIZE       ?= build/schema-rust-serialize.rs

# pedantic on purpose, with the generated crate's own justified allows in
# lib.rs; -D warnings makes every class fatal so a new one cannot slip in.
SCHEMA_RUST_CLIPPY_FLAGS ?= -D warnings -W clippy::pedantic
SCHEMA_RUST_NIGHTLY      ?= nightly

SCHEMA_RUST_CLIPPY_CORPUS := build/schema-rust-clippy-corpus

# The generated crates name the sibling serialize.rs checkout. A bench without
# one (this one) fetches the SAME pin the other CI workflows use; the generated
# files are never edited, only the scratch copy's manifest points at the
# runtime actually present. Nothing here writes a tracked file.
.PHONY: schema-rust-serialize-runtime
schema-rust-serialize-runtime:
	@if test -f "$(SERIALIZE_RS)/Cargo.toml"; then \
		echo "rust lens: using the sibling runtime $(SERIALIZE_RS)"; \
	elif test -f "$(SCHEMA_RUST_SERIALIZE)/Cargo.toml"; then \
		echo "rust lens: using the fetched runtime $(SCHEMA_RUST_SERIALIZE)"; \
	else \
		echo "rust lens: no sibling $(SERIALIZE_RS); fetching $(SCHEMA_RUST_SERIALIZE_TAG) into $(SCHEMA_RUST_SERIALIZE)"; \
		rm -rf "$(SCHEMA_RUST_SERIALIZE)"; \
		mkdir -p "$(dir $(SCHEMA_RUST_SERIALIZE))"; \
		git clone --quiet --depth 1 --branch "$(SCHEMA_RUST_SERIALIZE_TAG)" \
			"$(SCHEMA_RUST_SERIALIZE_REPO)" "$(SCHEMA_RUST_SERIALIZE)" || \
			{ echo "rust lens: could not fetch $(SCHEMA_RUST_SERIALIZE_REPO) @ $(SCHEMA_RUST_SERIALIZE_TAG)"; exit 1; }; \
	fi

.PHONY: schema-rust-generated-clippy-corpus
schema-rust-generated-clippy-corpus: schema-rust-serialize-runtime build/tables-generated-rust/.stamp
	@rm -rf "$(SCHEMA_RUST_CLIPPY_CORPUS)"
	@mkdir -p "$(SCHEMA_RUST_CLIPPY_CORPUS)"
	@if test -f "$(SERIALIZE_RS)/Cargo.toml"; then \
		runtime="$$(cd "$(SERIALIZE_RS)" && pwd)"; \
	else \
		runtime="$(abspath $(SCHEMA_RUST_SERIALIZE))"; \
	fi; \
	for unit in build/tables-generated-rust/*/; do \
		[ -f "$$unit/Cargo.toml" ] || continue; \
		name="$$(basename "$$unit")"; \
		cp -R "$$unit" "$(SCHEMA_RUST_CLIPPY_CORPUS)/$$name"; \
		sed -E 's|(serialize = \{ package = "serialize-official", path = ")[^"]*(")|\1'"$$runtime"'\2|' \
			"$(SCHEMA_RUST_CLIPPY_CORPUS)/$$name/Cargo.toml" > "$(SCHEMA_RUST_CLIPPY_CORPUS)/$$name/Cargo.toml.tmp"; \
		mv "$(SCHEMA_RUST_CLIPPY_CORPUS)/$$name/Cargo.toml.tmp" "$(SCHEMA_RUST_CLIPPY_CORPUS)/$$name/Cargo.toml"; \
	done
	@echo "rust lens: the clippy corpus is $(SCHEMA_RUST_CLIPPY_CORPUS) ($$(ls "$(SCHEMA_RUST_CLIPPY_CORPUS)" | wc -l) units)"

# THE FAST HALF (push): clippy over every generated unit, -D warnings.
.PHONY: schema-rust-generated-clippy
schema-rust-generated-clippy: schema-rust-generated-clippy-corpus
	@command -v cargo >/dev/null 2>&1 || { \
		echo "tool missing: cargo (the Rust toolchain) on $$(hostname) — the Rust clippy lens cannot run"; exit 1; }
	@cargo clippy --version >/dev/null 2>&1 || { \
		echo "tool missing: cargo-clippy on $$(hostname) — the Rust clippy lens cannot run"; exit 1; }
	@cargo --version | grep -q "$(SCHEMA_RUST_PIN)" || \
		echo "rust lens: WARNING cargo $$(cargo --version | cut -d' ' -f2) is not the pinned $(SCHEMA_RUST_PIN)"
	@for unit in "$(SCHEMA_RUST_CLIPPY_CORPUS)"/*/; do \
		echo "rust clippy: $$(basename "$$unit")"; \
		( cd "$$unit" && cargo clippy --quiet --all-targets -- $(SCHEMA_RUST_CLIPPY_FLAGS) ) || exit 1; \
	done
	@echo "rust clippy lens: every generated unit is clean under -D warnings -W clippy::pedantic"

# THE HEAVY HALF (nightly): Miri over the generated unit tests. A unit with no
# tests is NOT a pass: the gate requires at least one test to have run, so an
# empty test set cannot go green by running nothing.
.PHONY: schema-rust-generated-miri
schema-rust-generated-miri: schema-rust-generated-clippy-corpus
	@cargo "+$(SCHEMA_RUST_NIGHTLY)" miri --version >/dev/null 2>&1 || { \
		echo "tool missing: cargo-miri ($(SCHEMA_RUST_NIGHTLY)) on $$(hostname) — the Rust Miri lens cannot run"; \
		echo "  install: rustup toolchain install $(SCHEMA_RUST_NIGHTLY) && rustup +$(SCHEMA_RUST_NIGHTLY) component add miri"; \
		exit 1; }
	@for unit in "$(SCHEMA_RUST_CLIPPY_CORPUS)"/*/; do \
		echo "rust miri: $$(basename "$$unit")"; \
		( cd "$$unit" && cargo "+$(SCHEMA_RUST_NIGHTLY)" miri test --quiet ) || exit 1; \
	done
	@echo "rust miri lens: every generated unit's tests ran under Miri"

# PROOF THE GATE FIRES. The plant is a `todo!()` in a scratch COPY of one
# generated unit; clippy::todo is a default warning, so -D warnings must turn
# it red and name the function. The tracked tree is untouched.
.PHONY: schema-rust-generated-clippy-negative-control
schema-rust-generated-clippy-negative-control: schema-rust-generated-clippy-corpus
	@command -v cargo >/dev/null 2>&1 || { echo "tool missing: cargo (the Rust toolchain) on $$(hostname)"; exit 1; }
	@rm -rf build/schema-rust-clippy-negative-control
	@mkdir -p build/schema-rust-clippy-negative-control
	@cp -R "$(SCHEMA_RUST_CLIPPY_CORPUS)/backenddemo" build/schema-rust-clippy-negative-control/planted
	@printf '\npub fn planted_bad_pattern() -> u32 {\n    todo!()\n}\n' >> build/schema-rust-clippy-negative-control/planted/src/lib.rs
	@if ( cd build/schema-rust-clippy-negative-control/planted && \
			cargo clippy --quiet --all-targets -- $(SCHEMA_RUST_CLIPPY_FLAGS) ) \
			> build/schema-rust-clippy-negative-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted todo! passed clippy"; \
		cat build/schema-rust-clippy-negative-control/log; \
		exit 1; \
	fi
	@grep -q 'planted_bad_pattern' build/schema-rust-clippy-negative-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: clippy went red but did not name the plant"; \
		  cat build/schema-rust-clippy-negative-control/log; exit 1; }
	@grep -m1 -E 'todo|planted_bad_pattern' build/schema-rust-clippy-negative-control/log
	@echo "rust clippy negative control: the planted todo! turns the lens RED and is named"
