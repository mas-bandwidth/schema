package main

func init() {
	sabotages["table-cs-message-blob-header"] = []edit{{
		old: `NativePut(body+offset,length,4);`,
		new: `NativePut(body+offset,length,8); /* SABOTAGED: length consumes the reserved word */`,
	}}
	sabotages["table-cs-pack-short"] = []edit{{
		old: `extent=checked(extent+(long)count*f.NativeElementSize);`,
		new: `extent=checked(extent+(long)count*f.NativeElementSize); if(w.Bytes==null && count>0) { extent--; } // SABOTAGED: the probe consumes one byte too few`,
	}}
	sabotages["table-cs-duplicate-ids"] = []edit{{
		old: `if (id == Read64(vocabulary, j))`,
		new: `if (id == Read64(vocabulary, j) && report.Refused) /* SABOTAGED: duplicate identities pass */`,
	}}
	sabotages["table-cs-json-field"] = []edit{{
		old: `TableFieldInfo f = info.Fields[index];`,
		new: `TableFieldInfo f = info.Fields[info.Name == "WeaponConfig" && index == 0 ? 1 : index]; // SABOTAGED: read damage through the adjacent float descriptor`,
	}}
}

func init() {
	sabotages["table-cs-retain-capture"] = []edit{{
		old: "        retain->Used+=need; retain->Count++; report.Retained++; return true;",
		new: "        report.RetainLost++; return true; // SABOTAGED: the framed record is not admitted",
	}}
}
