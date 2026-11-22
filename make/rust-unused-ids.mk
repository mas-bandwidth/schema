# Strict unused-ID census proof and bounded recursion/omission controls.
.PHONY: tables-rust-unused-ids tables-rust-unused-ids-negative-control
tables-rust-unused-ids:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) ./internal/codegen/rusttable -count=1 -timeout 2m -run '^Test(RustUnusedIDsRefuse|UnusedIDNonFiniteOracle|RustUnusedIDUnionDepth|RustUnusedIDDiagnosticControls)$$' -v
tables-rust-unused-ids-negative-control: tables-rust-unused-ids
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) ./internal/codegen/rusttable -count=1 -timeout 2m -run '^Test(RustUnusedIDsNegativeControl|RustUnusedIDUnionDepthNegativeControl)$$' -v

test-rust: tables-rust-unused-ids
