# make/schema-rust-runs-hostile-corpus.mk — the Rust port runs the shared
# hostile-input corpus (schema-rust-runs-hostile-corpus).
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line.
# This target generates one Rust crate per schema the manifest names (the way
# make/rust.mk generates the table corpus, against the pinned serialize.rs),
# builds the runner (test/rust-hostile) over them, and hands every input to the
# port's own reader. Reads files only: no network, no clock beyond each case's
# budget.
#
# THE RUNNER RUNS TWICE: in the RELEASE profile, which is what ships and where
# integer overflow wraps, and in the DEBUG profile, where overflow checks and
# debug assertions are on. A hostile input can panic in one and hang or land a
# wrong value in the other, so each profile reports on its own and both must be
# green. Each case runs in a child process on a 512 KiB stack under
# catch_unwind with a 5 s budget (HOSTILE_CASE_BUDGET_MS overrides), so a
# panic, an abort, a stack overflow or a hang is that case's FAIL and the run
# goes on.
#
# A disagreement the port already owes is carried, by name, in
# test/rust-hostile/known-findings.tsv, and printed as a FIX-CARD (a name of
# the form <case>@release or <case>@debug is owed in that profile only). A
# disagreement no FIX-CARD names is a failure, and a FIX-CARD that has gone
# green is a failure too, so the list cannot rot into silence.
#
# The planted-bad-pattern control changes one committed verdict in a scratch
# copy of the manifest and requires the runner to go red on exactly that case
# before the unplanted target is green again.

RUST_HOSTILE_CORPUS   ?= internal/ci/hostilecorpus/corpus
RUST_HOSTILE_BUILD    ?= build/rust-hostile
RUST_HOSTILE_FINDINGS ?= test/rust-hostile/known-findings.tsv
RUST_HOSTILE_TARGET   ?= $(RUST_HOSTILE_BUILD)/target
RUST_HOSTILE_JOBS     ?= 4

# every schema the manifest names, read off its third column
RUST_HOSTILE_SCHEMAS = $(shell awk -F'\t' '!/^\#/ && NF == 9 { print $$3 }' $(RUST_HOSTILE_CORPUS)/manifest.tsv | sort -u)
# the one schema not in the checkout's tree (docs/HOSTILE-CORPUS.md) is the
# probe beside the runner
rust_hostile_source = $(if $(filter schemas/probe.schema,$(1)),test/rust-hostile/probe.schema,$(1))

RUST_HOSTILE_CARGO = PATH="$(RUSTUP_BIN):$$PATH" CARGO_TARGET_DIR="$(abspath $(RUST_HOSTILE_TARGET))" \
	RUST_HOSTILE_MANIFEST="$(abspath $(RUST_HOSTILE_CORPUS))/manifest.tsv" \
	RUST_HOSTILE_UNITS="$(abspath $(RUST_HOSTILE_BUILD))/units.tsv" \
	nice -n 10 cargo build --quiet -j $(RUST_HOSTILE_JOBS) --manifest-path $(RUST_HOSTILE_BUILD)/runner/Cargo.toml

$(RUST_HOSTILE_BUILD)/.stamp: bin/schema make/schema-rust-runs-hostile-corpus.mk $(RUST_HOSTILE_CORPUS)/manifest.tsv \
		$(foreach s,$(RUST_HOSTILE_SCHEMAS),$(wildcard $(call rust_hostile_source,$(s)) $(call rust_hostile_source,$(s))/*.schema))
	@rm -rf $(RUST_HOSTILE_BUILD)/units $(RUST_HOSTILE_BUILD)/runner && mkdir -p $(RUST_HOSTILE_BUILD)/units $(RUST_HOSTILE_BUILD)/runner
	@printf '# crate\tschema\tgenerated src\n' > $(RUST_HOSTILE_BUILD)/units.tsv
	@printf '[package]\nname = "rust-hostile"\nversion = "0.0.0"\nedition = "2024"\nbuild = "%s"\n\n[[bin]]\nname = "rust-hostile"\npath = "%s"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s" }\n' \
		"$(abspath test/rust-hostile/build.rs)" "$(abspath test/rust-hostile/src/main.rs)" "$(abspath $(SERIALIZE_RS))" \
		> $(RUST_HOSTILE_BUILD)/runner/Cargo.toml
	@for schema in $(RUST_HOSTILE_SCHEMAS); do \
		case "$$schema" in schemas/probe.schema) src=test/rust-hostile/probe.schema ;; *) src=$$schema ;; esac; \
		crate=hc_$$(printf '%s' "$$schema" | tr -c 'A-Za-z0-9' '_' | tr 'A-Z' 'a-z'); \
		./bin/schema generate --lang rust --out $(RUST_HOSTILE_BUILD)/units/$$crate/src $$src > /dev/null || exit 1; \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s" }\n' \
			$$crate "$(abspath $(SERIALIZE_RS))" > $(RUST_HOSTILE_BUILD)/units/$$crate/Cargo.toml; \
		printf '%s\t%s\t%s\n' $$crate $$schema "$(abspath $(RUST_HOSTILE_BUILD))/units/$$crate/src" >> $(RUST_HOSTILE_BUILD)/units.tsv; \
		printf '%s = { path = "../units/%s" }\n' $$crate $$crate >> $(RUST_HOSTILE_BUILD)/runner/Cargo.toml; \
	done
	@touch $@

.PHONY: schema-rust-runs-hostile-corpus-build
schema-rust-runs-hostile-corpus-build: $(RUST_HOSTILE_BUILD)/.stamp
	$(RUST_HOSTILE_CARGO) --release
	$(RUST_HOSTILE_CARGO)

# Both profiles run and report before the target decides, so a red release run
# never hides what the debug run says.
.PHONY: schema-rust-runs-hostile-corpus
schema-rust-runs-hostile-corpus: schema-rust-runs-hostile-corpus-build
	@status=0; \
	$(RUST_HOSTILE_TARGET)/release/rust-hostile "$(RUST_HOSTILE_CORPUS)" "$(RUST_HOSTILE_FINDINGS)" release || status=1; \
	$(RUST_HOSTILE_TARGET)/debug/rust-hostile "$(RUST_HOSTILE_CORPUS)" "$(RUST_HOSTILE_FINDINGS)" debug || status=1; \
	exit $$status

# The port's normal unit target runs the corpus: this file is included after
# make/rust.mk by the Makefile's wildcard, so the prerequisite lands here and
# make/rust.mk itself is untouched.
test-rust: schema-rust-runs-hostile-corpus

# The plant: wire-valid's committed digest replaced in a scratch copy of the
# manifest (the inputs are the committed ones), so a runner that compares
# nothing, or compares the wrong thing, stays green and fails this control.
.PHONY: schema-rust-runs-hostile-corpus-negative-control
schema-rust-runs-hostile-corpus-negative-control: schema-rust-runs-hostile-corpus-build
	@rm -rf $(RUST_HOSTILE_BUILD)/plant && mkdir -p $(RUST_HOSTILE_BUILD)/plant/corpus
	@ln -s "$(abspath $(RUST_HOSTILE_CORPUS))/inputs" $(RUST_HOSTILE_BUILD)/plant/corpus/inputs
	@awk -F'\t' 'BEGIN { OFS = "\t" } $$1 == "wire-valid" { $$9 = "0000000000000000000000000000000000000000000000000000000000000000" } { print }' \
		$(RUST_HOSTILE_CORPUS)/manifest.tsv > $(RUST_HOSTILE_BUILD)/plant/corpus/manifest.tsv
	@if cmp -s $(RUST_HOSTILE_CORPUS)/manifest.tsv $(RUST_HOSTILE_BUILD)/plant/corpus/manifest.tsv; then \
		echo 'NEGATIVE CONTROL FAILED: the planted verdict did not apply'; exit 1; fi
	@for profile in release debug; do \
		if $(RUST_HOSTILE_TARGET)/$$profile/rust-hostile $(RUST_HOSTILE_BUILD)/plant/corpus "$(RUST_HOSTILE_FINDINGS)" $$profile \
				> $(RUST_HOSTILE_BUILD)/plant/log-$$profile 2>&1; then \
			echo "NEGATIVE CONTROL FAILED ($$profile): a changed committed verdict left the corpus green"; \
			cat $(RUST_HOSTILE_BUILD)/plant/log-$$profile; exit 1; fi; \
		grep -q '^FAIL wire-valid ' $(RUST_HOSTILE_BUILD)/plant/log-$$profile || \
			{ echo "NEGATIVE CONTROL FAILED ($$profile): the runner went red without naming the planted case"; \
			  cat $(RUST_HOSTILE_BUILD)/plant/log-$$profile; exit 1; }; \
	done
	@$(MAKE) --no-print-directory schema-rust-runs-hostile-corpus
	@echo 'rust hostile-corpus negative control: a changed committed verdict reds the corpus in both profiles; unplanted it is green'
