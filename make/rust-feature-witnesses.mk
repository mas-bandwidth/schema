# THE FEATURE WITNESSES ON THE RUST LEG (testdata/conformance/feature-witnesses,
# test/conformance/README.md). The featurewitnesses unit is generated as its own
# crate beside the conformance crates and named in test/conformance/rust's
# Cargo.toml, so the Rust driver reaches the unit's two roots, Root and
# ArrayRoot, on every surface it implements: wire, report, json-read,
# json-write, json-hostile and cook-write. The gate runs the Rust leg and
# passes only when none of the unit's cases is answered absent and the wire and
# report surfaces pass whole (test/conformance/rust/feature-witnesses-verdict).
# The leg's other surfaces are gated by their own targets, so the run's exit
# status is not this target's verdict.
RUST_FEATURE_WITNESSES_CRATES := build/tables-generated-rust-witnesses
RUST_FEATURE_WITNESSES := build/conformance-rust-feature-witnesses
RUST_FEATURE_WITNESSES_CONTROL := build/conformance-rust-feature-witnesses-control

$(RUST_FEATURE_WITNESSES_CRATES)/.stamp: bin/schema testdata/conformance/feature-witnesses/full/Features.schema make/rust-feature-witnesses.mk
	@mkdir -p $(RUST_FEATURE_WITNESSES_CRATES)/featurewitnesses
	@rm -rf $(RUST_FEATURE_WITNESSES_CRATES)/featurewitnesses/src
	./bin/schema generate --lang rust --out $(RUST_FEATURE_WITNESSES_CRATES)/featurewitnesses/src testdata/conformance/feature-witnesses/full/Features.schema
	@printf '[package]\nname = "featurewitnesses"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../$(SERIALIZE_RS)" }\n' \
		> $(RUST_FEATURE_WITNESSES_CRATES)/featurewitnesses/Cargo.toml
	@touch $@

# Every build of test/conformance/rust (the driver, and each control that
# builds a copy of it) resolves the crate, so it is made wherever the other
# conformance crates are, and the driver is rebuilt when it changes.
build/tables-generated-rust/.stamp: | $(RUST_FEATURE_WITNESSES_CRATES)/.stamp
build/conformance-rust: $(RUST_FEATURE_WITNESSES_CRATES)/.stamp

.PHONY: conformance-rust-feature-witnesses conformance-rust-feature-witnesses-negative-control
conformance-rust-feature-witnesses: build/conformance-harness build/conformance-rust
	@rm -rf $(RUST_FEATURE_WITNESSES) && mkdir -p $(RUST_FEATURE_WITNESSES)
	@./build/conformance-harness run --only rust --results $(RUST_FEATURE_WITNESSES)/results.json \
		--work $(RUST_FEATURE_WITNESSES)/work > $(RUST_FEATURE_WITNESSES)/log 2>&1 || true
	@test -s $(RUST_FEATURE_WITNESSES)/results.json || \
		{ cat $(RUST_FEATURE_WITNESSES)/log; echo "conformance-rust-feature-witnesses: the run wrote no results"; exit 1; }
	sh test/conformance/rust/feature-witnesses-verdict $(RUST_FEATURE_WITNESSES)
	@echo "conformance-rust-feature-witnesses: the Rust leg answers every feature-witness case"

# The control builds the driver from a copy under build/ whose codec lookup
# for the unit's Root misses on every surface, so the cases at Root are
# answered absent; the same verdict must go red on it, naming feature_full.
conformance-rust-feature-witnesses-negative-control: build/conformance-harness build/tables-generated-rust/.stamp $(RUST_FEATURE_WITNESSES_CRATES)/.stamp
	@rm -rf $(RUST_FEATURE_WITNESSES_CONTROL) && mkdir -p $(RUST_FEATURE_WITNESSES_CONTROL)/driver/src
	@sed -e 's|("featurewitnesses", "Root")|("featurewitnesses", "SABOTAGED: the lookup misses")|' \
		test/conformance/rust/src/main.rs > $(RUST_FEATURE_WITNESSES_CONTROL)/driver/src/main.rs
	@[ "$$(grep -c 'SABOTAGED: the lookup misses' $(RUST_FEATURE_WITNESSES_CONTROL)/driver/src/main.rs)" = 2 ] || \
		{ echo "NEGATIVE CONTROL: the lookup sabotage did not reach both codec tables"; exit 1; }
	@sed -e 's|^name = "conformance-rust"|name = "conformance-rust-feature-witnesses-control"|' \
		test/conformance/rust/Cargo.toml > $(RUST_FEATURE_WITNESSES_CONTROL)/driver/Cargo.toml
	cd $(RUST_FEATURE_WITNESSES_CONTROL)/driver && PATH="$(RUSTUP_BIN):$$PATH" \
		CARGO_TARGET_DIR=$(CURDIR)/test/conformance/rust/target cargo build --quiet
	@printf '#!/bin/sh\nexec "%s/test/conformance/rust/target/debug/conformance-rust-feature-witnesses-control" "$$@"\n' "$(CURDIR)" \
		> $(RUST_FEATURE_WITNESSES_CONTROL)/driver/driver && chmod +x $(RUST_FEATURE_WITNESSES_CONTROL)/driver/driver
	@printf 'rust %s/driver/driver\n' "$(RUST_FEATURE_WITNESSES_CONTROL)" > $(RUST_FEATURE_WITNESSES_CONTROL)/drivers.txt
	@./build/conformance-harness run --drivers $(RUST_FEATURE_WITNESSES_CONTROL)/drivers.txt \
		--results $(RUST_FEATURE_WITNESSES_CONTROL)/results.json \
		--work $(RUST_FEATURE_WITNESSES_CONTROL)/work > $(RUST_FEATURE_WITNESSES_CONTROL)/log 2>&1 || true
	@test -s $(RUST_FEATURE_WITNESSES_CONTROL)/results.json || \
		{ cat $(RUST_FEATURE_WITNESSES_CONTROL)/log; echo "NEGATIVE CONTROL: the sabotaged run wrote no results"; exit 1; }
	@if sh test/conformance/rust/feature-witnesses-verdict $(RUST_FEATURE_WITNESSES_CONTROL) > $(RUST_FEATURE_WITNESSES_CONTROL)/verdict 2>&1; then \
		cat $(RUST_FEATURE_WITNESSES_CONTROL)/verdict; \
		echo "NEGATIVE CONTROL FAILED: a codec lookup that misses the unit left conformance-rust-feature-witnesses green"; exit 1; fi
	@grep -q '"feature_full": "absent"' $(RUST_FEATURE_WITNESSES_CONTROL)/verdict || \
		{ cat $(RUST_FEATURE_WITNESSES_CONTROL)/verdict; echo "NEGATIVE CONTROL FAILED: the verdict went red, but not naming feature_full"; exit 1; }
	@cat $(RUST_FEATURE_WITNESSES_CONTROL)/verdict
	@echo "conformance-rust-feature-witnesses negative control: a codec lookup that misses the unit turns the gate red"

test-rust: conformance-rust-feature-witnesses conformance-rust-feature-witnesses-negative-control
