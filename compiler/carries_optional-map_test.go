package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryOptionalMap,
		schema:    "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n",
		refusal:   "optional map on field M.tags",
	})
}
