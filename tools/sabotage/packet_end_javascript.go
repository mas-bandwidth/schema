package main

func init() {
	// Removes the flat tier's end check from the emitter, so a generated read
	// accepts trailing bytes and non-zero padding. The runtime tier keeps its
	// own end check; the driver reports the flat tier's trailing vector.
	sabotages["packet-end-js-read"] = []edit{{
		old: `	g.emitReadEnd("  ")`,
		new: `	// SABOTAGED: the flat-read end check is not emitted`,
	}}
}
