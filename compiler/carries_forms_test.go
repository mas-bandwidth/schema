package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryForms,
		schema:    "package probe\ntable Shape | forms = \"wire\"\n{\n    x int32\n}\n",
		refusal:   "forms on table Shape",
	})
}
