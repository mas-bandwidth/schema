package main

func init() {
	sabotages["table-cs-duplicate-ids"] = []edit{{
		old: `if (id == Read64(vocabulary, j))`,
		new: `if (id == Read64(vocabulary, j) && report.Refused) /* SABOTAGED: duplicate identities pass */`,
	}}
	sabotages["table-cs-json-field"] = []edit{{
		old: `TableFieldInfo f = info.Fields[index];`,
		new: `TableFieldInfo f = info.Fields[(index + 1) % info.NumFields]; // SABOTAGED: read through the adjacent descriptor`,
	}}
}
