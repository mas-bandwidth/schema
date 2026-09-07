package main

func init() {
	sabotages["packet-utf8-js-read"] = []edit{{
		old: `for (let utf8Index = 0; utf8Index < %s;)`,
		new: `for (let utf8Index = Infinity; utf8Index < %s;) // SABOTAGED`,
	}}
}
