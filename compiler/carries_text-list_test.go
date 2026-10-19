package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryTextList,
		schema:    "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n",
		refusal:   "text list on field Lists.rules",
	})
}
