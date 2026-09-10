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
		// Target Load's verdict, where report lives, rather than a comparison
		// inside the vocabulary helper's small or hashed validation path.
		old: `if (!DistinctVocabulary(vocabulary)) { Damage(report); return Finish(report, Verdict.Damaged); }`,
		new: `if (!DistinctVocabulary(vocabulary) && report.Refused) { Damage(report); return Finish(report, Verdict.Damaged); } /* SABOTAGED: duplicate identities pass */`,
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

func init() {
	sabotages["fixed-form-cs-arm-text-flavour"] = []edit{
		{
			old: "                    TableFixedEntry e = new TableFixedEntry(\n" +
				"                        their_at, at, units, aux_at, guard, Text, arg, 0, 0, d.Arg);",
			new: "                    TableFixedEntry e = new TableFixedEntry(\n" +
				"                        their_at, at, units, aux_at, guard, Text, d.Arg, 0, 0); // SABOTAGED: the flavour over the arm ordinal",
		},
		{
			old: "                        uint unit = (p.Meta == TextWide) ? 2u : 1u;",
			new: "                        uint unit = (p.Arg == TextWide) ? 2u : 1u; // SABOTAGED: the flavour read back off the guard's lane",
		},
	}
}

