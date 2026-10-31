// THE DART LEG'S WIRE-FUZZ CONTROL (docs/PORTING.md I15): the tolerant read of
// a counted list counts ONE `widened` for the field when the wire's element
// kind widens to the declared one (docs/SPEC-TABLES.md §4). Take the count out
// of the emitter and every regenerated unit reads a widened list without
// saying so, so the differential finds the report differing from the engine's
// on the first widened-list mutant while every other counter and every saved
// byte still agree.
package main

func init() {
	sabotages["table-dart-wire-widen"] = []edit{{
		old: "\t\t\tg.pf(\"%s\\t\\t\\tif (!_kindWidens(elemKind, %d)) { r.report.kindMismatch++; r.pos = end; break; }\\n\", ind, declKind)\n\t\t\tg.pf(\"%s\\t\\t\\tr.report.widened++;\\n\", ind)",
		new: "\t\t\tg.pf(\"%s\\t\\t\\tif (!_kindWidens(elemKind, %d)) { r.report.kindMismatch++; r.pos = end; break; }\\n\", ind, declKind)\n\t\t\tg.pf(\"%s\\t\\t\\t// SABOTAGED: the widened count is gone\\n\", ind)",
	}}
}

// THE DART LEG'S BLOCK-FORM CONTROL (docs/PORTING.md I11): the block Open
// compares each array's stored pitch with this build's own and refuses a
// forged image that disagrees. Take the comparison out of the emitter and the
// forged image `block_pitch` opens: the forgery battery goes red on that row
// while `block` and `block-dump`, whose two valid images carry this build's
// pitch, stay green, which is what says the reader CHECKS rather than that it
// is broken.
func init() {
	sabotages["table-dart-block-pitch"] = []edit{{
		old: "\t\tg.pf(\"      if (stride != %sStride) {\\n        return (null, TableRefuseReason.badLayout);\\n      }\\n\", field)",
		new: "\t\tg.pf(\"      // SABOTAGED: the pitch check is gone %s\\n\", field)",
	}}
}
