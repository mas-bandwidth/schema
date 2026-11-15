package main

func init() {
	// SPEC §5.1 row 8 on the Goose packet Write: the text content checks go,
	// so the used bytes of string(N) may hold a zero byte or malformed UTF-8.
	// The length bound stays, so only the ill-formed text cases may go red.
	sabotages["packet-write-text-goose"] = []edit{{
		old: `		if t.Kind == ir.TString {
			// the used bytes hold the content the read accepts (SPEC §4.7,`,
		new: `		if false && t.Kind == ir.TString { // SABOTAGED: the write-side text checks removed
			// the used bytes hold the content the read accepts (SPEC §4.7,`,
	}}
}
