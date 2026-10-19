# make/schema-rust-runs-hostile-corpus.mk — the RUST PORT's leg of the shared
# hostile-input corpus (schema-shared-hostile-corpus, docs/HOSTILE-CORPUS.md).
#
# The corpus is committed at internal/ci/hostilecorpus/corpus: raw inputs plus
# one line-oriented manifest whose verdicts are the Go engine's. This target
# generates the port's readers for every unit the manifest names, builds the
# runner in test/rust-hostile, and holds every verdict to the manifest. The
# comparison and its planted-bad-pattern control live in
# internal/ci/schema_rust_runs_hostile_corpus_test.go.
#
# THE GENERATED TREES ARE BUILD OUTPUT. One crate per unit, because a unit is a
# Rust crate. Only the modules the carriers touch are compiled: the variable
# wire's text/wire readers and the message form's announcement and batch. The
# fixed/block/cook modules are not emitted into these crates — they are not
# reached by any carrier, and leaving them out keeps the unit tier free of the
# sibling serialize.rs checkout the fixed form needs.

HOSTILE_CORPUS_DIR ?= internal/ci/hostilecorpus/corpus
HOSTILE_CORPUS_MANIFEST := $(HOSTILE_CORPUS_DIR)/manifest.tsv
HOSTILE_CORPUS_INPUTS := $(shell find $(HOSTILE_CORPUS_DIR)/inputs -type f 2>/dev/null)
RUST_HOSTILE_DIR := build/rust-hostile

RUST_HOSTILE_UNITS := tabledemo:tables/examples graphdemo:tables/pointers \
	blobdemo:tables/blobs streamdemo:tables/stream messagedemo:tables/messages

# The probe schema is the one schema the corpus names that is not in the
# checkout: its variable root carries a collection count past the int32 storage
# cap so the engine's CountRefusal has an input. It is written here, into the
# build tree, and never tracked.
define RUST_HOSTILE_PROBE_SCHEMA
// probe.schema — a variable root whose one collection is what the engine's
// builder-path CountRefusal can name (internal/tablewire/decodenodes.go): a
// count past the int32 storage cap refuses the partial value before an entry
// is read. It exists only so the hostile corpus can carry that verdict.
package probe

table Root
{
    entries map[int32]int32
}
endef
export RUST_HOSTILE_PROBE_SCHEMA

$(RUST_HOSTILE_DIR)/.stamp: bin/schema $(HOSTILE_CORPUS_MANIFEST) test/rust-hostile/Cargo.toml test/rust-hostile/src/main.rs make/schema-rust-runs-hostile-corpus.mk
	@rm -rf $(RUST_HOSTILE_DIR)/tabledemo $(RUST_HOSTILE_DIR)/graphdemo $(RUST_HOSTILE_DIR)/blobdemo $(RUST_HOSTILE_DIR)/streamdemo $(RUST_HOSTILE_DIR)/messagedemo $(RUST_HOSTILE_DIR)/probe
	@mkdir -p $(RUST_HOSTILE_DIR)
	@for u in $(RUST_HOSTILE_UNITS); do \
		name=$${u%%:*}; path=$${u#*:}; \
		mkdir -p $(RUST_HOSTILE_DIR)/$$name/src; \
		./bin/schema generate --lang rust --out $(RUST_HOSTILE_DIR)/$$name/src $$path >/dev/null || exit 1; \
	done
	@mkdir -p $(RUST_HOSTILE_DIR)/probe/src
	@printf '%s\n' "$$RUST_HOSTILE_PROBE_SCHEMA" > $(RUST_HOSTILE_DIR)/probe.schema
	@./bin/schema generate --lang rust --out $(RUST_HOSTILE_DIR)/probe/src $(RUST_HOSTILE_DIR)/probe.schema >/dev/null
	@for u in $(RUST_HOSTILE_UNITS); do \
		name=$${u%%:*}; d=$(RUST_HOSTILE_DIR)/$$name; \
		{ \
			echo '#![allow(clippy::derivable_impls, clippy::field_reassign_with_default)]'; \
			echo 'mod variable_runtime;'; echo 'pub use variable_runtime::*;'; \
			if [ -f $$d/src/build_version.rs ]; then echo 'mod build_version;'; echo 'pub use build_version::*;'; fi; \
			if [ -f $$d/src/fixed_runtime.rs ]; then echo 'mod fixed_runtime;'; echo 'pub use fixed_runtime::*;'; fi; \
			if [ "$$name" = tabledemo ] || [ "$$name" = messagedemo ]; then \
				echo 'mod message_runtime;'; echo 'pub use message_runtime::*;'; \
			fi; \
			for f in $$d/src/*_records.rs $$d/src/*_fixed.rs $$d/src/*_table.rs; do \
				[ -e "$$f" ] || continue; m=$$(basename $$f .rs); \
				echo "mod $$m;"; echo "pub use $$m::*;"; \
			done; \
			if [ "$$name" = messagedemo ]; then \
				for f in $$d/src/*_message.rs; do \
					[ -e "$$f" ] || continue; m=$$(basename $$f .rs); \
					echo "mod $$m;"; echo "pub use $$m::*;"; \
				done; \
			fi; \
		} > $$d/src/lib.rs; \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[lib]\npath = "src/lib.rs"\n' $$name > $$d/Cargo.toml; \
	done
	@{ \
		echo '#![allow(clippy::derivable_impls, clippy::field_reassign_with_default)]'; \
		echo 'mod variable_runtime;'; echo 'pub use variable_runtime::*;'; \
		for f in $(RUST_HOSTILE_DIR)/probe/src/*_records.rs $(RUST_HOSTILE_DIR)/probe/src/*_table.rs; do \
			[ -e "$$f" ] || continue; m=$$(basename $$f .rs); \
			echo "mod $$m;"; echo "pub use $$m::*;"; \
		done; \
	} > $(RUST_HOSTILE_DIR)/probe/src/lib.rs
	@printf '[package]\nname = "probe"\nversion = "0.0.0"\nedition = "2024"\n\n[lib]\npath = "src/lib.rs"\n' > $(RUST_HOSTILE_DIR)/probe/Cargo.toml
	@touch $@

.PHONY: schema-rust-runs-hostile-corpus-build
schema-rust-runs-hostile-corpus-build: $(RUST_HOSTILE_DIR)/.stamp
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet \
		--manifest-path test/rust-hostile/Cargo.toml \
		--target-dir $(RUST_HOSTILE_DIR)/target

# THE FAST GATE: every carrier in the committed corpus, in the port's unit tier.
.PHONY: schema-rust-runs-hostile-corpus
schema-rust-runs-hostile-corpus: schema-rust-runs-hostile-corpus-build
	SCHEMA_RUST_HOSTILE_RUNNER=$$PWD/$(RUST_HOSTILE_DIR)/target/debug/rust-hostile \
		go test -count=1 -timeout 600s ./internal/ci \
		-run '^TestSchemaRustRunsHostileCorpus$$' -v

# THE PLANTED-BAD-PATTERN CONTROL (nightly): alter the generated Rust reader
# in a scratch copy of the generated tree and require the comparison to go red
# on it, with the unplanted corpus green beside it.
.PHONY: schema-rust-runs-hostile-corpus-negative-control
schema-rust-runs-hostile-corpus-negative-control: schema-rust-runs-hostile-corpus-build
	PATH="$(RUSTUP_BIN):$$PATH" SCHEMA_RUST_HOSTILE_RUNNER=$$PWD/$(RUST_HOSTILE_DIR)/target/debug/rust-hostile \
		go test -count=1 -timeout 600s ./internal/ci \
		-run '^TestSchemaRustRunsHostileCorpusPlanted$$' -v

# The port's normal unit target includes the corpus leg.
test-rust: schema-rust-runs-hostile-corpus
