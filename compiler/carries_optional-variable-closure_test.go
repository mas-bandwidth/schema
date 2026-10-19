package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryOptionalVariableClosure,
		schema:    "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n",
		refusal:   "optional over a variable closure on field Outer.inner",
	})
}
