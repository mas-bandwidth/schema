package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryUnboundedText,
		schema:    "package probe\ntable Where\n{\n    rules []string\n}\n",
		refusal:   "unbounded text-list element on field Where.rules",
	})
}
