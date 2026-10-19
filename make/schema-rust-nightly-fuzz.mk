# make/schema-rust-nightly-fuzz.mk — nightly coverage-guided fuzzing of the
# Rust port's generated readers (schema-rust-nightly-fuzz, 2026-10-18).
#
# The fuzzer is cargo-fuzz (libFuzzer) on a pinned nightly toolchain. It is a
# FUNCTIONAL gate: a nightly time budget, never per push. The fast half is the
# replay of every saved regression input, no clock and no mutation, built with
# the host cargo (it needs no nightly and no cargo-fuzz).
#
# THE CORPUS IS THE PORT'S OWN. $(SCHEMA_RUST_FUZZ_UNITS) is
# $(RUST_TABLE_UNITS) — the exact list make/rust.mk's generate target reads —
# plus tables/arms, a directory that target's siblings read. No second corpus
# is invented here: the list is written to $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt,
# and the harness reads that file.
#
# THREE TARGETS, ONE PER READER KIND:
#   load    <root>_load                 (variable wire)
#   strict  <root>_fixed_load           (fixed form, the strict read)
#   text    <root>_from_json            (text/JSON parse)
#
# THE ORACLE: no crash, no hang, no sanitizer or assert finding. A refusal is a
# clean typed answer (Option::None / a report), never a panic and never silent
# success on malformed input.
#
# THE TOOLS ARE FLEET-WIDE, INSTALLED THROUGH rowan-tools/fleet (Glenn's
# standing grant), never by hand. A missing tool is reported BY NAME and is a
# not-done, never a faked pass. Pins: cargo-fuzz $(SCHEMA_RUST_FUZZ_VERSION) on
# $(SCHEMA_RUST_FUZZ_NIGHTLY). The runtime sibling serialize.rs is fetched at
# $(SERIALIZE_RS_PIN) from its official repository when the bench has no
# ../serialize.rs, the same way the Makefile fetches the C and Go siblings.

SCHEMA_RUST_FUZZ_TIME ?= 10m
SCHEMA_RUST_FUZZ_VERSION ?= 0.13.1
SCHEMA_RUST_FUZZ_NIGHTLY ?= nightly-2026-10-01
SCHEMA_RUST_FUZZ_DIR ?= build/schema-rust-nightly-fuzz
SCHEMA_RUST_FUZZ_CORPUS ?= test/rust-nightly-fuzz-corpus
SCHEMA_RUST_FUZZ_SEEDS ?= $(SCHEMA_RUST_FUZZ_DIR)/seeds

SERIALIZE_RS ?= ../serialize.rs
SERIALIZE_RS_PIN ?= v2.4.0
SERIALIZE_RS_REPO ?= https://github.com/mas-bandwidth/serialize.rs.git
SERIALIZE_RS_FETCHED ?= build/serialize.rs
SERIALIZE_RS_RUNTIME := $(if $(wildcard $(SERIALIZE_RS)/Cargo.toml),$(SERIALIZE_RS),$(SERIALIZE_RS_FETCHED))

# The port's own corpus: RUST_TABLE_UNITS (make/rust.mk) plus the arms
# directory. Each entry is <crate-name>:<schema path or directory>.
SCHEMA_RUST_FUZZ_UNITS := $(RUST_TABLE_UNITS) armdemo:tables/arms

SCHEMA_RUST_FUZZ_TARGETS := load strict text

# The runtime sibling: use ../serialize.rs when it is a checkout, otherwise
# fetch the pin from the official repository (the mirror first, as the
# Makefile's runtime fetches do).
$(SERIALIZE_RS_FETCHED)/Cargo.toml:
	@rm -rf "$(SERIALIZE_RS_FETCHED)"
	@mkdir -p "$(dir $(SERIALIZE_RS_FETCHED))"
	@if test -n "$(NOVA_MIRRORS)" && test -d "$(NOVA_MIRRORS)/mas-bandwidth/serialize.rs.git" && \
		git clone --quiet --depth 1 --branch "$(SERIALIZE_RS_PIN)" "file://$(NOVA_MIRRORS)/mas-bandwidth/serialize.rs.git" "$(SERIALIZE_RS_FETCHED)"; then \
		echo "schema-rust-nightly-fuzz: fetched serialize.rs $(SERIALIZE_RS_PIN) from the mirror"; \
	else \
		rm -rf "$(SERIALIZE_RS_FETCHED)"; \
		git clone --quiet --depth 1 --branch "$(SERIALIZE_RS_PIN)" "$(SERIALIZE_RS_REPO)" "$(SERIALIZE_RS_FETCHED)" \
			|| { echo "tool missing: serialize.rs@$(SERIALIZE_RS_PIN) (no sibling $(SERIALIZE_RS) and the fetch of $(SERIALIZE_RS_REPO) failed)"; exit 2; }; \
		echo "schema-rust-nightly-fuzz: fetched serialize.rs $(SERIALIZE_RS_PIN)"; \
	fi
	@test -f "$@" || { echo "tool missing: serialize.rs@$(SERIALIZE_RS_PIN) (the fetch produced no $@)"; exit 2; }

.PHONY: schema-rust-nightly-fuzz-runtime
schema-rust-nightly-fuzz-runtime:
	@if [ ! -f "$(SERIALIZE_RS)/Cargo.toml" ] && [ ! -f "$(SERIALIZE_RS_FETCHED)/Cargo.toml" ]; then \
		$(MAKE) --no-print-directory "$(SERIALIZE_RS_FETCHED)/Cargo.toml" || exit 2; \
	fi

.PHONY: schema-rust-nightly-fuzz-tool
schema-rust-nightly-fuzz-tool: schema-rust-nightly-fuzz-runtime
	@missing=; \
	if ! command -v cargo-fuzz >/dev/null 2>&1; then missing="$$missing cargo-fuzz"; fi; \
	if ! command -v rustup >/dev/null 2>&1; then missing="$$missing rustup"; \
	elif ! rustup toolchain list 2>/dev/null | grep -q '^$(SCHEMA_RUST_FUZZ_NIGHTLY)'; then \
		missing="$$missing $(SCHEMA_RUST_FUZZ_NIGHTLY)"; fi; \
	if [ ! -f "$(SERIALIZE_RS_RUNTIME)/Cargo.toml" ]; then \
		missing="$$missing serialize.rs@$(SERIALIZE_RS_PIN)"; fi; \
	if [ -n "$$missing" ]; then \
		for name in $$missing; do echo "tool missing: $$name"; done; \
		echo "schema-rust-nightly-fuzz: install the pins through rowan-tools/fleet on the Linux bench (cargo-fuzz $(SCHEMA_RUST_FUZZ_VERSION), $(SCHEMA_RUST_FUZZ_NIGHTLY)); serialize.rs is fetched at $(SERIALIZE_RS_PIN)"; \
		exit 2; \
	fi

# The harness reads this file. It is generated, not a second hand-written list.
.PHONY: schema-rust-nightly-fuzz-corpus
schema-rust-nightly-fuzz-corpus:
	@mkdir -p $(SCHEMA_RUST_FUZZ_DIR) $(SCHEMA_RUST_FUZZ_CORPUS)
	@printf '%s\n' $(SCHEMA_RUST_FUZZ_UNITS) > $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@set -e; while IFS=: read -r name path; do \
		[ -n "$$name" ] || continue; \
		[ -e "$$path" ] || { echo "corpus entry $$name names missing path $$path"; exit 1; }; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@echo "schema-rust-nightly-fuzz: corpus $(words $(SCHEMA_RUST_FUZZ_UNITS)) units -> $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt"

# Seed the nightly fuzzer from the Go fuzz corpus (internal/fuzz/testdata) and
# the conformance/wire vectors. Seeds are copied, never referenced, so a saved
# regression stays in this port's own corpus directory.
.PHONY: schema-rust-nightly-fuzz-seeds
schema-rust-nightly-fuzz-seeds:
	@mkdir -p $(SCHEMA_RUST_FUZZ_SEEDS)
	@set -e; find internal/fuzz/testdata testdata/wire test/conformance -type f \
		\( -name '*.bin' -o -path '*/fuzz/*' \) 2>/dev/null | while read -r f; do \
		case "$$f" in *.go) continue;; esac; \
		cp "$$f" "$(SCHEMA_RUST_FUZZ_SEEDS)/$$(printf '%s' "$$f" | tr '/ ' '__')"; \
	done
	@echo "schema-rust-nightly-fuzz: $$(find $(SCHEMA_RUST_FUZZ_SEEDS) -type f | wc -l) seed(s) from the Go fuzz corpus and the conformance vectors"

# One crate per unit, exactly the wiring make/rust.mk's stamp uses, so the
# generated readers under test are the ones the port ships.
.PHONY: schema-rust-nightly-fuzz-generate
schema-rust-nightly-fuzz-generate: bin/schema schema-rust-nightly-fuzz-corpus schema-rust-nightly-fuzz-runtime
	@set -e; mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/corpus; \
	runtime="$(abspath $(SERIALIZE_RS_RUNTIME))"; \
	test -f "$$runtime/Cargo.toml" || { echo "tool missing: serialize.rs@$(SERIALIZE_RS_PIN)"; exit 2; }; \
	while IFS=: read -r name path; do \
		[ -n "$$name" ] || continue; \
		rm -rf $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src; \
		mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src; \
		./bin/schema generate --lang rust --out $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src $$path; \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s" }\n' \
			$$name "$$runtime" > $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/Cargo.toml; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@# The dispatch manifest: one row per (kind, unit, function), read by the
	@# three fuzz targets and by the no-clock replay binary.
	@: > $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
	while IFS=: read -r name path; do \
		[ -n "$$name" ] || continue; \
		for f in $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src/*_table.rs; do \
			[ -e "$$f" ] || continue; \
			grep -oE '^pub fn [a-z0-9_]+_load\(' "$$f" | sed -E 's/^pub fn ([a-z0-9_]+)_load\(/load '"$$name"' \1/' >> $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
			grep -oE '^pub fn [a-z0-9_]+_from_json\(' "$$f" | sed -E 's/^pub fn ([a-z0-9_]+)_from_json\(/text '"$$name"' \1/' >> $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
		done; \
		for f in $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src/*_fixed.rs; do \
			[ -e "$$f" ] || continue; \
			grep -oE '^pub fn [a-z0-9_]+_fixed_load\(' "$$f" | sed -E 's/^pub fn ([a-z0-9_]+)_fixed_load\(/strict '"$$name"' \1/' >> $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
		done; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt; \
	test -s $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt || { echo "schema-rust-nightly-fuzz: no readers found in the corpus"; exit 1; }
	@echo "schema-rust-nightly-fuzz: $$(wc -l < $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt) reader entry points across $(words $(SCHEMA_RUST_FUZZ_TARGETS)) reader kinds"

# The fuzz crate and one fuzz_target per reader kind. The target files are
# generated from READERS.txt so a reader added to the corpus is fuzzed without
# a second list.
.PHONY: schema-rust-nightly-fuzz-harness
schema-rust-nightly-fuzz-harness: schema-rust-nightly-fuzz-generate
	@mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/fuzz/fuzz_targets $(SCHEMA_RUST_FUZZ_DIR)/fuzz/src
	@printf '[package]\nname = "schema-rust-nightly-fuzz"\nversion = "0.0.0"\nedition = "2024"\n\n[package.metadata]\ncargo-fuzz = true\n\n[dependencies]\nlibfuzzer-sys = "0.4"\n' > $(SCHEMA_RUST_FUZZ_DIR)/fuzz/Cargo.toml
	@set -e; while IFS=: read -r name path; do \
		[ -n "$$name" ] || continue; \
		printf '%s = { path = "../corpus/%s" }\n' "$$name" "$$name" >> $(SCHEMA_RUST_FUZZ_DIR)/fuzz/Cargo.toml; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@# cargo-fuzz discovers targets from explicit [[bin]] entries, not from the
	@# fuzz_targets directory alone.
	@set -e; for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do \
		printf '\n[[bin]]\nname = "%s"\npath = "fuzz_targets/%s.rs"\ntest = false\ndoc = false\nbench = false\n' "$$kind" "$$kind" >> $(SCHEMA_RUST_FUZZ_DIR)/fuzz/Cargo.toml; \
		{ \
			echo '// Generated by make/schema-rust-nightly-fuzz.mk — DO NOT EDIT.'; \
			echo '#![no_main]'; \
			echo 'use libfuzzer_sys::fuzz_target;'; \
			echo 'fuzz_target!(|data: &[u8]| {'; \
			awk -v kind="$$kind" '$$1 == kind { \
				if (kind == "load") printf "    { let mut r = %s::TableReport::default(); let _ = %s::%s_load(data, &mut r); }\n", $$2, $$2, $$3; \
				else if (kind == "text") printf "    if let Ok(s) = std::str::from_utf8(data) { let mut r = %s::TableReport::default(); let _ = %s::%s_from_json(s, &mut r); }\n", $$2, $$2, $$3; \
				else printf "    { let mut r = %s::TableFixedReport::default(); let _ = %s::%s_fixed_load(&mut [], data, &mut [], &mut [], &mut r); }\n", $$2, $$2, $$3; \
			}' $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
			echo '});'; \
		} > $(SCHEMA_RUST_FUZZ_DIR)/fuzz/fuzz_targets/$$kind.rs; \
	done
	@# The replay binary: the same calls, over every saved input, once. No
	@# libFuzzer, no clock, no mutation; the host cargo builds it.
	@printf '\n[[bin]]\nname = "replay"\npath = "src/replay.rs"\ntest = false\ndoc = false\nbench = false\n' >> $(SCHEMA_RUST_FUZZ_DIR)/fuzz/Cargo.toml
	@{ \
		echo '// Generated by make/schema-rust-nightly-fuzz.mk — DO NOT EDIT.'; \
		echo 'fn load_all(data: &[u8]) {'; \
		awk '$$1 == "load" { printf "    { let mut r = %s::TableReport::default(); let _ = %s::%s_load(data, &mut r); }\n", $$2, $$2, $$3 }' $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
		echo '}'; \
		echo 'fn strict_all(data: &[u8]) {'; \
		awk '$$1 == "strict" { printf "    { let mut r = %s::TableFixedReport::default(); let _ = %s::%s_fixed_load(&mut [], data, &mut [], &mut [], &mut r); }\n", $$2, $$2, $$3 }' $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
		echo '}'; \
		echo 'fn text_all(data: &str) {'; \
		awk '$$1 == "text" { printf "    { let mut r = %s::TableReport::default(); let _ = %s::%s_from_json(data, &mut r); }\n", $$2, $$2, $$3 }' $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
		echo '}'; \
		echo 'fn main() {'; \
		echo '    let dir = std::env::args().nth(1).unwrap_or_else(|| "corpus".to_string());'; \
		echo '    let mut files: Vec<std::path::PathBuf> = match std::fs::read_dir(&dir) {'; \
		echo '        Ok(entries) => entries.filter_map(|e| e.ok()).map(|e| e.path()).filter(|p| p.is_file()).collect(),'; \
		echo '        Err(_) => Vec::new(),'; \
		echo '    };'; \
		echo '    files.sort();'; \
		echo '    for file in &files {'; \
		echo '        let data = std::fs::read(file).expect("read a saved input");'; \
		echo '        load_all(&data);'; \
		echo '        strict_all(&data);'; \
		echo '        if let Ok(text) = std::str::from_utf8(&data) { text_all(text); }'; \
		echo '    }'; \
		echo '    println!("replay: {} saved input(s) clean (no clock, no mutation)", files.len());'; \
		echo '}'; \
	} > $(SCHEMA_RUST_FUZZ_DIR)/fuzz/src/replay.rs
	@echo "schema-rust-nightly-fuzz: harness targets $(SCHEMA_RUST_FUZZ_TARGETS) plus replay in $(SCHEMA_RUST_FUZZ_DIR)/fuzz"

# THE FAST HALF. Replay only: every saved input, no clock, no mutation. This is
# what a push runs. A saved input that panics fails this target. It is built by
# the host cargo, so the fast half runs where the nightly fuzzer is absent.
.PHONY: schema-rust-nightly-fuzz-replay
schema-rust-nightly-fuzz-replay: schema-rust-nightly-fuzz-harness
	@mkdir -p $(SCHEMA_RUST_FUZZ_CORPUS)
	@cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && cargo run --quiet --bin replay -- $(abspath $(SCHEMA_RUST_FUZZ_CORPUS))

# THE NIGHTLY HALF. The time budget is an argument (default 10 minutes per
# port); heavy parts never run per push. Crashes are minimised and written to
# the committed corpus directory, which the replay target then holds.
.PHONY: schema-rust-nightly-fuzz
schema-rust-nightly-fuzz: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-harness schema-rust-nightly-fuzz-seeds
	@mkdir -p $(SCHEMA_RUST_FUZZ_CORPUS)
	@cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do \
		echo "schema-rust-nightly-fuzz: $$kind for $(SCHEMA_RUST_FUZZ_TIME)"; \
		timeout $(SCHEMA_RUST_FUZZ_TIME) cargo +$(SCHEMA_RUST_FUZZ_NIGHTLY) fuzz run $$kind $(SCHEMA_RUST_FUZZ_SEEDS) -- \
			-artifact_prefix=$(abspath $(SCHEMA_RUST_FUZZ_CORPUS))/ || exit 1; \
	done
	@echo "schema-rust-nightly-fuzz: no crash, hang, sanitizer or assert finding in $(SCHEMA_RUST_FUZZ_TIME) per reader kind"

# THE PLANTED-PATTERN CONTROL. A reader bug (the variable runtime's bounds
# check) is dropped in the scratch generated tree, never in a tracked file.
# The nightly target must find it inside the budget; the saved input must make
# the replay target fail against the planted tree; then the tree is regenerated
# and the saved input is kept as a regression.
.PHONY: schema-rust-nightly-fuzz-negative-control
schema-rust-nightly-fuzz-negative-control: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-harness
	@set -e; runtime=$(SCHEMA_RUST_FUZZ_DIR)/corpus/tabledemo/src/variable_runtime.rs; \
	test -f "$$runtime" || { echo "NEGATIVE CONTROL FAILED: no generated runtime to plant in"; exit 1; }; \
	cp "$$runtime" "$$runtime.orig"; \
	restore() { mv "$$runtime.orig" "$$runtime"; }; trap restore EXIT INT TERM; \
	sed -E 's/if c\.off \+ n > c\.buf\.len\(\) \{/if false {/' "$$runtime" > "$$runtime.planted"; \
	cmp -s "$$runtime" "$$runtime.planted" && { echo "NEGATIVE CONTROL FAILED: the bounds-check plant did not apply"; exit 1; } || true; \
	mv "$$runtime.planted" "$$runtime"; \
	work=$(abspath $(SCHEMA_RUST_FUZZ_DIR))/plant-corpus; rm -rf "$$work"; mkdir -p "$$work"; \
	echo "schema-rust-nightly-fuzz negative control: the bounds check is dropped; the fuzzer has $(SCHEMA_RUST_FUZZ_TIME)"; \
	if (cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && timeout $(SCHEMA_RUST_FUZZ_TIME) cargo +$(SCHEMA_RUST_FUZZ_NIGHTLY) fuzz run load $(SCHEMA_RUST_FUZZ_SEEDS) -- -artifact_prefix="$$work"/); then \
		echo "NEGATIVE CONTROL FAILED: the planted bounds-check removal passed the nightly budget"; exit 1; fi; \
	test -n "$$(find "$$work" -type f -print -quit)" || { echo "NEGATIVE CONTROL FAILED: the fuzzer found the plant but saved no input"; exit 1; }; \
	echo "schema-rust-nightly-fuzz negative control: the fuzzer found the plant and saved $$(find "$$work" -type f | wc -l) input(s)"; \
	if (cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && cargo run --quiet --bin replay -- "$$work"); then \
		echo "NEGATIVE CONTROL FAILED: the saved input did not fail the replay on the planted tree"; exit 1; fi; \
	echo "schema-rust-nightly-fuzz negative control: the saved input fails the replay on the planted tree"; \
	restore; trap - EXIT INT TERM; \
	for f in "$$work"/*; do [ -f "$$f" ] || continue; cp "$$f" "$(SCHEMA_RUST_FUZZ_CORPUS)/plant-load-$$(basename "$$f")"; done; \
	$(MAKE) --no-print-directory schema-rust-nightly-fuzz-harness >/dev/null; \
	$(MAKE) --no-print-directory schema-rust-nightly-fuzz-replay
	@echo "schema-rust-nightly-fuzz negative control: red with the plant in, green with it out"
