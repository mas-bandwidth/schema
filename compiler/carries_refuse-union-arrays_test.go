package compiler

func init() {
	addRefusalCase(refusalCase{
		construct: "refuse-union-arrays",
		refuser:   "refuseUnionArrays",
		name:      "array of unions",
		src:       "package probe\n\ntype Ping\n{\n    nonce uint32\n}\n\ntype Pong\n{\n    nonce uint32\n}\n\nunion Reply\n{\n    ping Ping\n    pong Pong\n}\n\nfixed table L\n{\n    history [..4]Reply\n}\n",
		field:     "L.history",
	})
}
