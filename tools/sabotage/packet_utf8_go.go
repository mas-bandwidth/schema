package main

func init() {
	sabotages["packet-utf8-go-read"] = []edit{{
		old: "g.pf(\"%sif !utf8.Valid(%s[:%sLength]) {\\n%s\\treturn ErrValidation // malformed UTF-8\\n%s}\\n\", ind, name, name, ind, ind)",
		new: "g.pf(\"%sif false && !utf8.Valid(%s[:%sLength]) { // SABOTAGED\\n%s\\treturn ErrValidation // malformed UTF-8\\n%s}\\n\", ind, name, name, ind, ind)",
	}}
}
