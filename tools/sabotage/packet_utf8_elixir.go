package main

func init() {
	sabotages["packet-utf8-elixir-read"] = []edit{{
		old: `		g.throwIf(fmt.Sprintf("not String.valid?(%s)", lv), "malformed UTF-8 is content the read refuses (SPEC §4.7)", ind)`,
		new: `		// SABOTAGED: omit only the UTF-8 read refusal`,
	}}
}
