package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-table-arms",
		refuser:   "refuseTableArms",
		name:      "union table arm",
		src:       "package probe\n\ntype Inner\n{\n    x int32\n}\nunion Pick\n{\n    alpha Inner\n    beta int32\n}\ntable L\n{\n    u Pick\n}\n",
		field:     "Pick",
	})
}
