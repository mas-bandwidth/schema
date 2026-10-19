package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryTextListTotal,
		schema:    "package probe\ntable L\n{\n    names [..8]string(8) | total = 12\n}\n",
		refusal:   "text list total on field L.names",
	})
}
