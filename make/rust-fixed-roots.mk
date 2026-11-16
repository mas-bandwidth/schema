# Fixed records cross into the existing text and cook value paths (§3.4,
# §7.6, §16 of docs/SPEC-TABLES.md). Other Rust surfaces retain their own gates.
RUST_FIXED_ROOTS := build/conformance-rust-fixed-roots
RUST_FIXED_ROOTS_CONTROL := build/conformance-rust-fixed-roots-control

.PHONY: conformance-rust-fixed-roots conformance-rust-fixed-roots-negative-control
conformance-rust-fixed-roots: build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-rust
	@rm -rf $(RUST_FIXED_ROOTS) && mkdir -p $(RUST_FIXED_ROOTS)
	@./build/conformance-harness run --only rust --results $(RUST_FIXED_ROOTS)/results.json \
		--work $(RUST_FIXED_ROOTS)/work > $(RUST_FIXED_ROOTS)/log 2>&1 || true
	sh test/conformance/rust/fixed-roots-verdict $(RUST_FIXED_ROOTS)
	@echo 'conformance-rust-fixed-roots: fixed values answer text and both cook byte orders'

# Drop one fixed root field from the text emitted under build/. The same
# verdict must reject that value, naming root_full, while json-read stays green.
conformance-rust-fixed-roots-negative-control: build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-rust
	@rm -rf $(RUST_FIXED_ROOTS_CONTROL) && mkdir -p $(RUST_FIXED_ROOTS_CONTROL)
	@printf '#!/bin/sh\nset -eu\n"%s/build/conformance-rust" "$$@"\nif [ "$$2" = json-write ]; then\n  file="$$3/root_full.json"\n  test -s "$$file"\n  sed '\''/^  "version_note":/d'\'' "$$file" > "$$file.tmp"\n  if cmp -s "$$file" "$$file.tmp"; then echo "field-drop control changed nothing" >&2; exit 1; fi\n  mv "$$file.tmp" "$$file"\nfi\n' "$(CURDIR)" > $(RUST_FIXED_ROOTS_CONTROL)/driver
	@chmod +x $(RUST_FIXED_ROOTS_CONTROL)/driver
	@printf 'rust %s/driver\n' "$(CURDIR)/$(RUST_FIXED_ROOTS_CONTROL)" > $(RUST_FIXED_ROOTS_CONTROL)/drivers.txt
	@./build/conformance-harness run --drivers $(RUST_FIXED_ROOTS_CONTROL)/drivers.txt \
		--results $(RUST_FIXED_ROOTS_CONTROL)/results.json --work $(RUST_FIXED_ROOTS_CONTROL)/work \
		> $(RUST_FIXED_ROOTS_CONTROL)/log 2>&1 || true
	@if sh test/conformance/rust/fixed-roots-verdict $(RUST_FIXED_ROOTS_CONTROL) > $(RUST_FIXED_ROOTS_CONTROL)/verdict 2>&1; then \
		cat $(RUST_FIXED_ROOTS_CONTROL)/verdict; echo 'NEGATIVE CONTROL FAILED: field drop passed'; exit 1; fi
	@grep -qE '^json-read +pass ([1-9][0-9]*)/\1 *$$' $(RUST_FIXED_ROOTS_CONTROL)/log || \
		{ cat $(RUST_FIXED_ROOTS_CONTROL)/log; echo 'NEGATIVE CONTROL FAILED: json-read changed'; exit 1; }
	@grep 'json-write: root_full.json:' $(RUST_FIXED_ROOTS_CONTROL)/log || \
		{ cat $(RUST_FIXED_ROOTS_CONTROL)/log; echo 'NEGATIVE CONTROL FAILED: root_full was not the failing case'; exit 1; }
	@echo 'negative control: dropping a fixed root text field turns the Rust fixed-root gate red'

test-rust: conformance-rust-fixed-roots conformance-rust-fixed-roots-negative-control
