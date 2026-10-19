package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-strings",
		refuser:   "refuseStrings",
		name:      "string buffer",
		src:       "package probe\n\ntable L\n{\n    s *string\n}\n",
		field:     "L.s",
	})
}
