package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-wide-text",
		refuser:   "refuseWideText",
		name:      "wide text",
		src:       "package probe\n\ntable L\n{\n    w wstring(8)\n}\n",
		field:     "L.w",
	})
}
