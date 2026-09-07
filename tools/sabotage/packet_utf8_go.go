package main

func init() {
	sabotages["packet-utf8-go-read"] = []edit{{
		old: `if !utf8.Valid(%s[:%sLength]) {`,
		new: `if false && !utf8.Valid(%s[:%sLength]) { // SABOTAGED`,
	}}
}
