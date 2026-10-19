package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-lists",
		refuser:   "refuseLists",
		name:      "unbounded array",
		src:       "package probe\n\ntable L\n{\n    repos []int32\n}\n",
		field:     "L.repos",
	})
}
