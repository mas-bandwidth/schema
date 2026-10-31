package compiler

// The Squirrel table surface carries `| required` on the variable wire
// (docs/SPEC-TABLES.md §2.3): load.nut refuses a record missing a required
// field, any depth down, with missing_required naming the field, and save.nut
// already writes the field always. Registering the construct lifts the central
// refusal in compiler/generate.go for this target only
// (docs/PORT-SQUIRREL.md, THE REGISTER).
func init() {
	registerCarried("squirrel", carryRequired) // §2.3
}
