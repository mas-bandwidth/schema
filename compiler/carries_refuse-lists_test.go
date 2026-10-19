package compiler

func init() {
	// this construct is LIFTED (compiler/target_squirrel_lists.go): the
	// tagged target carries it, so the refuser accepts squirrel and still
	// refuses a target that has not registered (docs/SPEC-TABLES.md §2.9).
	addRefusalCase(refusalCase{
		construct: "refuse-lists",
		refuser:   "refuseLists",
		name:      "unbounded array",
		src:       "package probe\n\ntable L\n{\n    repos []int32\n}\n",
		field:     "L.repos",
		carried:   true,
	})
}
