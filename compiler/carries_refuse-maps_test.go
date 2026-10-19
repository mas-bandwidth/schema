package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-maps",
		refuser:   "refuseMaps",
		name:      "map",
		src:       "package probe\n\ntable L\n{\n    index map[string(8)]int32\n}\n",
		field:     "L.index",
		carried:   true,
	})
}
