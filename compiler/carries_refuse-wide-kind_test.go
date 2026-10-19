package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-wide-kind",
		refuser:   "ir.RefuseWideTableKinds",
		name:      "wide kind",
		src:       "package probe\n\ntable L\n{\n    big uint128\n}\n",
		field:     "L.big",
	})
}
