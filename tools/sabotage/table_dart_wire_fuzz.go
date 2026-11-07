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

// THE RELEASE WIRE-FUZZ GATE'S CONTROL (make/wire-fuzz-release.mk): #2400
// reverted. A map's header (element kind, pair count) is read against the
// PARENT and only its entries are bounded by the map's L (docs/SPEC-TABLES.md
// §2.8, §3); the sabotage reads the header from the body bounded by L, as Dart
// did before #2400, so a count LEB128 that runs past L reads as framing damage
// where the engine judges the element kind and counts one kind_mismatch.
func init() {
	sabotages["table-dart-map-header-bounded"] = []edit{{
		old: "\tg.pf(\"%s\\t\\tfinal elemKind = r.readByte();\\n\", ind)\n\tg.pf(\"%s\\t\\tfinal count = r.readLeb();\\n\", ind)\n\tg.pf(\"%s\\t\\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\\n\", ind)\n\tg.pf(\"%s\\t\\tfinal arr = TableWireReader.sub(r, r.pos, end < r.pos ? r.pos : end);\\n\", ind)",
		new: "\tg.pf(\"%s\\t\\tfinal arr = TableWireReader.sub(r, r.pos, end);\\n\", ind)\n\tg.pf(\"%s\\t\\t// SABOTAGED: the map header is bounded by L\\n\", ind)\n\tg.pf(\"%s\\t\\tfinal elemKind = arr.readByte();\\n\", ind)\n\tg.pf(\"%s\\t\\tfinal count = arr.readLeb();\\n\", ind)\n\tg.pf(\"%s\\t\\tif (count == null) { r.report.malformed = true; r.pos = end; break; }\\n\", ind)",
	}}
}
