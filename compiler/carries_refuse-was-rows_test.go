package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-was-rows",
		refuser:   "refuseWasRows",
		name:      "was on a variant",
		src:       "package probe\n\nenum Grade\n{\n    Bronze,\n    Argent | was = \"Silver\"\n}\n\nfixed table L\n{\n    grade Grade\n}\n",
		field:     "Grade.Argent",
	})
}
