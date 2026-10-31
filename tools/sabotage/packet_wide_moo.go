package main

// Packet wide text, the moo leg (SPEC §4.12): a high surrogate no longer
// needs a low one after it. The emitted validWide helper is the one pairing
// check, so a read accepts an unpaired high surrogate, which the mutation
// sweep must catch against the C++ oracle.
func init() {
	sabotages["packet-wide-moo-pairing"] = []edit{{
		old: `if lo < 0xDC00 or lo > 0xDFFF then`,
		new: `if lo < 0 --[[ SABOTAGED: lo < 0xDC00 or lo > 0xDFFF ]] then`,
	}}
}
