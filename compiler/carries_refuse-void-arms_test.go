package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-void-arms",
		refuser:   "refuseVoidArms",
		name:      "payload-free arm",
		src:       "package probe\n\ntype Fire\n{\n    target uint16\n}\n\nunion Weapon\n{\n    laser Fire\n    ram\n}\n\ntable L\n{\n    fire Weapon\n}\n",
		field:     "Weapon",
	})
}
