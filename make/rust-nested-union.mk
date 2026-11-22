# Bounded native proof for nested union payload boundaries, including omission sabotage.
.PHONY: tables-rust-nested-union tables-rust-nested-union-negative-control
tables-rust-nested-union:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) ./internal/codegen/rusttable -count=1 -timeout 2m -run '^TestRustNestedUnionArmExtent$$' -v
tables-rust-nested-union-negative-control: tables-rust-nested-union
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) ./internal/codegen/rusttable -count=1 -timeout 2m -run '^TestRustNestedUnionArmExtentNegativeControl$$' -v

test-rust: tables-rust-nested-union
