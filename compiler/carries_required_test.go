package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryRequired,
		schema:    "package probe\ntable Ship\n{\n    name string(32) | required\n}\n",
		refusal:   "refuses | required",
	})
}
