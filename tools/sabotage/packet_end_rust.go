package main

func init() {
	// Removes the public one-packet read's end check (SPEC §5 "Read
	// termination") from the Rust emitter, so a trailing byte and set padding
	// read as value. The per-type body and every field read stay as they are,
	// so only the packets with an end to check may go red.
	sabotages["packet-end-rust-end-check"] = []edit{{
		old: `g.pf("    stream.serialize_end()?;\n")`,
		new: `g.pf("    // SABOTAGED: no packet end check\n")`,
	}}
}
