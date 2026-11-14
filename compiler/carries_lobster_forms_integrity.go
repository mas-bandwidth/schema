package compiler

// The Lobster target carries a table's `forms` declaration (docs/SPEC-TABLES.md
// §2.12: the descriptor's json_form and cook_form, and no block layout for a
// table that leaves block out), the `integrity` trailer (§2.14: the CRC-32C
// the variable, fixed and message forms seal and check) and the JSON array
// root (§16.10). internal/codegen/lobstertable's TestLobsterTableHonoursTheFormsDeclaration
// and TestLobsterVariableWireIntegrity hold them, and the conformance leg
// answers the featurewitnesses unit, which declares all three, on every surface.
var (
	_ = carryLobster(carryForms)
	_ = carryLobster(carryIntegrity)
	_ = carryLobster(carryArrayRoot)
)
