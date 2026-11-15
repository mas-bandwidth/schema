package main

func init() {
	// Removes the C packet read's one end check (docs/SPEC.md §5, "Read
	// termination") from the emitter's public read wrapper. The per-type body
	// still decodes the top-level object; only the refusal of trailing data
	// and non-zero final padding is gone.
	sabotages["packet-end-c-read"] = []edit{{
		old: `	g.pf("    return serialize_read_end( stream ); /* one packet read: trailing data or non-zero final padding is refused (SPEC §5) */\n")`,
		new: `	g.pf("    return 1; /* SABOTAGED: no packet end check */\n")`,
	}}
}
