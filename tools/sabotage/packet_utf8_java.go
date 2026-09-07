package main

func init() {
	sabotages["packet-utf8-java-read"] = []edit{{
		old: `for (int utf8Index = 0; utf8Index < %s;)`,
		new: `for (int utf8Index = Integer.MAX_VALUE; utf8Index < %s;) // SABOTAGED`,
	}}
}
