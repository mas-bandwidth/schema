package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryOneSidedRange,
		schema:    "package probe\ntable OneSided\n{\n    a int64 | min = 0\n}\n",
		refusal:   "OneSided.a",
	})
}
