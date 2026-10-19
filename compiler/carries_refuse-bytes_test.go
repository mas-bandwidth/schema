package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-bytes",
		refuser:   "refuseBytes",
		name:      "byte buffer",
		src:       "package probe\n\ntable L\n{\n    b *bytes\n}\n",
		field:     "L.b",
	})
}
