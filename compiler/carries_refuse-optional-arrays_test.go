package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-optional-arrays",
		refuser:   "refuseOptionalArrays",
		name:      "optional array",
		src:       "package probe\n\ntable L\n{\n    maybe ?[..4]int32\n}\n",
		field:     "L.maybe",
	})
}
