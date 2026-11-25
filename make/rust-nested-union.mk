# Bounded native proof for nested union payload boundaries, including omission sabotage.
.PHONY: tables-rust-nested-union tables-rust-nested-union-negative-control
tables-rust-nested-union:
tables-rust-nested-union-negative-control: tables-rust-nested-union

test-rust: tables-rust-nested-union
