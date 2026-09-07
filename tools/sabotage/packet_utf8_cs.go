package main

func init() {
	sabotages["packet-utf8-cs-read"] = []edit{{
		old: `for (int utf8Index = 0; utf8Index < %s;)`,
		new: `for (int utf8Index = int.MaxValue; utf8Index < %s;) // SABOTAGED`,
	}}
}
