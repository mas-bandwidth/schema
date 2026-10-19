package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-value-defaults",
		refuser:   "refuseValueDefaults",
		name:      "value default",
		src:       "package probe\n\ntable L\n{\n    s string(8) = \"hi\"\n}\n",
		field:     "L.s",
	})
}
