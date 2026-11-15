package main

// SPEC §5.1 row 11 on the packet Write: a bare intN/uintN is checked inside
// its declared width before the mask. Remove the width check from the
// generated writer; only the out-of-width cases may go red.
func init() {
	sabotages["packet-write-bareint-dart-width"] = []edit{{
		old: `	g.emitBareIntWidthAssert(f, name, ind)
	g.emitWriteWide64(name, w, ind)`,
		new: `	// SABOTAGED: the bare integer's width is not checked
	g.emitWriteWide64(name, w, ind)`,
	}}
}
