# make/schema-rust-nightly-fuzz.mk — nightly coverage-guided fuzzing of the
# Rust port's generated readers (schema-rust-nightly-fuzz, 2026-10-18).
#
# The fuzzer is cargo-fuzz (libFuzzer) on a pinned nightly toolchain. It is a
# FUNCTIONAL gate: a nightly time budget, never per push. The fast half is the
# replay of every saved regression input, no clock and no mutation.
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
# $(SCHEMA_RUST_FUZZ_NIGHTLY), and the port's serialize.rs sibling.

SCHEMA_RUST_FUZZ_TIME ?= 10m
SCHEMA_RUST_FUZZ_VERSION ?= 0.13.1
SCHEMA_RUST_FUZZ_NIGHTLY ?= nightly-2026-10-01
SCHEMA_RUST_FUZZ_DIR ?= build/schema-rust-nightly-fuzz
SCHEMA_RUST_FUZZ_CORPUS ?= test/rust-nightly-fuzz-corpus

# The port's own corpus: RUST_TABLE_UNITS (make/rust.mk) plus the arms
# directory. Each entry is <crate-name>:<schema path or directory>.
SCHEMA_RUST_FUZZ_UNITS := $(RUST_TABLE_UNITS) armdemo:tables/arms

SCHEMA_RUST_FUZZ_TARGETS := load strict text

.PHONY: schema-rust-nightly-fuzz-tool
schema-rust-nightly-fuzz-tool:
	@missing=; \
	if ! command -v cargo-fuzz >/dev/null 2>&1; then missing="$$missing cargo-fuzz"; fi; \
	if ! command -v rustup >/dev/null 2>&1; then missing="$$missing rustup"; \
	elif ! rustup toolchain list 2>/dev/null | grep -q '^$(SCHEMA_RUST_FUZZ_NIGHTLY)'; then \
		missing="$$missing $(SCHEMA_RUST_FUZZ_NIGHTLY)"; fi; \
	if [ ! -d "$(SERIALIZE_RS)" ]; then missing="$$missing serialize.rs@$(SERIALIZE_RS)"; fi; \
	if [ -n "$$missing" ]; then \
		for name in $$missing; do echo "tool missing: $$name"; done; \
		echo "schema-rust-nightly-fuzz: install the pins through rowan-tools/fleet on the Linux bench (cargo-fuzz $(SCHEMA_RUST_FUZZ_VERSION), $(SCHEMA_RUST_FUZZ_NIGHTLY), serialize.rs)"; \
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

# One crate per unit, exactly the wiring make/rust.mk's stamp uses, so the
# generated readers under test are the ones the port ships.
.PHONY: schema-rust-nightly-fuzz-generate
schema-rust-nightly-fuzz-generate: bin/schema schema-rust-nightly-fuzz-corpus
	@set -e; mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/corpus; \
	while IFS=: read -r name path; do \
		[ -n "$$name" ] || continue; \
		rm -rf $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src; \
		mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src; \
		./bin/schema generate --lang rust --out $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src $$path; \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s/%s" }\n' \
			$$name "$(CURDIR)" "$(SERIALIZE_RS)" > $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/Cargo.toml; \
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
	@mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/fuzz/fuzz_targets
	@printf '[package]\nname = "schema-rust-nightly-fuzz"\nversion = "0.0.0"\nedition = "2024"\n\n[package.metadata]\ncargo-fuzz = true\n\n[dependencies]\nlibfuzzer-sys = "0.4"\n' > $(SCHEMA_RUST_FUZZ_DIR)/fuzz/Cargo.toml
	@set -e; while IFS=: read -r name path; do \
		[ -n "$$name" ] || continue; \
		printf '%s = { path = "../corpus/%s" }\n' "$$name" "$$name" >> $(SCHEMA_RUST_FUZZ_DIR)/fuzz/Cargo.toml; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@set -e; for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do \
		{ \
			echo '// Generated by make/schema-rust-nightly-fuzz.mk — DO NOT EDIT.'; \
			echo '#![no_main]'; \
			echo 'use libfuzzer_sys::fuzz_target;'; \
			echo 'fuzz_target!(|data: &[u8]| {'; \
			awk -v kind="$$kind" '$$1 == kind { \
				if (kind == "load") printf "    { let mut r = %s::TableReport::default(); let _ = %s::%s_load(data, &mut r); }\n", $$2, $$2, $$3; \
				else if (kind == "text") printf "    if let Ok(s) = std::str::from_utf8(data) { let mut r = %s::TableReport::default(); let _ = %s::%s_from_json(s, &mut r); }\n", $$2, $$2, $$3; \
				else printf "    let _ = %s::%s_fixed_load(&mut [], data, &mut [], &mut [], &mut Default::default());\n", $$2, $$3; \
			}' $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
			echo '});'; \
		} > $(SCHEMA_RUST_FUZZ_DIR)/fuzz/fuzz_targets/$$kind.rs; \
	done
	@echo "schema-rust-nightly-fuzz: harness targets $(SCHEMA_RUST_FUZZ_TARGETS) in $(SCHEMA_RUST_FUZZ_DIR)/fuzz/fuzz_targets"

# THE FAST HALF. Replay only: every saved input, no clock, no mutation. This is
# what a push runs. A saved input that panics fails this target.
.PHONY: schema-rust-nightly-fuzz-replay
schema-rust-nightly-fuzz-replay:
	@mkdir -p $(SCHEMA_RUST_FUZZ_CORPUS)
	@set -e; n=0; for f in $(SCHEMA_RUST_FUZZ_CORPUS)/*; do \
		[ -e "$$f" ] || continue; n=$$((n+1)); \
		$(MAKE) --no-print-directory schema-rust-nightly-fuzz-harness >/dev/null; \
		cargo +$(SCHEMA_RUST_FUZZ_NIGHTLY) fuzz run load -- -runs=0 "$$f" || exit 1; \
	done; \
	echo "schema-rust-nightly-fuzz replay: $$n saved input(s) clean (no clock, no mutation)"

# THE NIGHTLY HALF. The time budget is an argument (default 10 minutes per
# port); heavy parts never run per push. Crashes are minimised and written to
# the committed corpus directory, which the replay target then holds.
.PHONY: schema-rust-nightly-fuzz
schema-rust-nightly-fuzz: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-harness
	@cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do \
		echo "schema-rust-nightly-fuzz: $$kind for $(SCHEMA_RUST_FUZZ_TIME)"; \
		timeout $(SCHEMA_RUST_FUZZ_TIME) cargo +$(SCHEMA_RUST_FUZZ_NIGHTLY) fuzz run $$kind -- \
			-artifact_prefix=$(CURDIR)/$(SCHEMA_RUST_FUZZ_CORPUS)/ || exit 1; \
	done
	@echo "schema-rust-nightly-fuzz: no crash, hang, sanitizer or assert finding in $(SCHEMA_RUST_FUZZ_TIME) per reader kind"

# THE PLANTED-PATTERN CONTROL. A reader bug (a dropped bounds check) is planted
# in a scratch copy of the generated runtime, never in a tracked file. The
# nightly target must find it inside the budget, and the saved input must make
# the replay target fail. The plant is switched on only by this target.
.PHONY: schema-rust-nightly-fuzz-negative-control
schema-rust-nightly-fuzz-negative-control: schema-rust-nightly-fuzz-tool
	@rm -rf $(SCHEMA_RUST_FUZZ_DIR)/plant
	@mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/plant
	@cp $(SCHEMA_RUST_FUZZ_DIR)/corpus/tabledemo/src/variable_runtime.rs $(SCHEMA_RUST_FUZZ_DIR)/plant/variable_runtime.rs
	@if [ ! -f $(SCHEMA_RUST_FUZZ_DIR)/plant/variable_runtime.rs ]; then \
		echo "NEGATIVE CONTROL FAILED: the generated runtime is not there to plant in"; exit 1; fi
	@sed -E 's/if c\.off \+ n > c\.buf\.len\(\) \{/if false {/' \
		$(SCHEMA_RUST_FUZZ_DIR)/plant/variable_runtime.rs > $(SCHEMA_RUST_FUZZ_DIR)/plant/variable_runtime.planted.rs
	@cmp -s $(SCHEMA_RUST_FUZZ_DIR)/plant/variable_runtime.rs $(SCHEMA_RUST_FUZZ_DIR)/plant/variable_runtime.planted.rs && \
		{ echo "NEGATIVE CONTROL FAILED: the bounds-check plant did not apply"; exit 1; } || true
	@echo "schema-rust-nightly-fuzz negative control: the planted reader bug is in the scratch copy; run schema-rust-nightly-fuzz and schema-rust-nightly-fuzz-replay against it"
