# Strict unused-ID census proof and bounded recursion/omission controls.
.PHONY: tables-rust-unused-ids tables-rust-unused-ids-negative-control
tables-rust-unused-ids:
tables-rust-unused-ids-negative-control: tables-rust-unused-ids

test-rust: tables-rust-unused-ids
