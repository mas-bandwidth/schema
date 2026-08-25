package rust

// The Rust backend's inline taxonomy — the twin of the C++ backend's
// SCHEMA_WRITE_INLINE / SCHEMA_READ_INLINE macros (cpp/writeinline.go,
// cpp/readinline.go), spelled in Rust attributes because Rust needs no
// macro: the attribute is per-item already.
//
// Three names, two spellings, and the line between them was drawn by
// measurement on this backend rather than copied from the C++ one:
//
//   writeSpineInline = #[inline(always)] — the DEMAND. Every generated WRITE
//                      wire function: the per-type/per-message write_* spines,
//                      the object Deep/Shallow view writers, and both tag
//                      writers (write_message_type, write_object_type).
//   readSpineInline  = #[inline] — the HINT. Every generated READ wire
//                      function, per-type spines, view readers, tag readers,
//                      and both read dispatch surfaces (read_message,
//                      read_message_into).
//   dispatchInline   = #[inline] — the HINT. write_message, the write dispatch
//                      surface, mirroring the one exemption the C++ pass carved out of
//                      the C++ write demand.
//
// Why the write demand exists (from the sixlang-air-1 attribution):
// #[inline] is a HINT — it raises LLVM's inline threshold for the callee
// (275 -> 325 in the reference builds) but LLVM still declines when the
// callee's inline cost is over it, and the generated spines are far over:
// the rigidbody_moving write spine priced at cost 2285 against threshold
// 325, refused, leaving a real call per serialize where the C++ twin (whose
// always_inline is a DEMAND, not a price) has none.
//
// The lever is NOT LTO: the refused calls are intra-crate — generated spine
// to generated spine, and generated spine to a runtime that already demands
// inlining on its own hot path — and the reference C++/C legs run no-LTO
// themselves, so reaching for LTO would change the comparison rather than
// close the gap.
//
// WHY THE READ HALF IS DELIBERATELY NOT THE C++ SHAPE. The C++ backend
// demands inlining on its READ spines too (SCHEMA_READ_INLINE, blanket,
// ReadMessage included). That shape was ported here exactly, measured, and
// REFUSED by the evidence: on Apple M-series / rustc release, no LTO, the
// blanket read demand collapsed probearray read to 0.53x and shipcreate read
// to 0.71x of their hinted rates, in twin-gate-OK passes with 1.4-5.7%
// spreads, while the hand-written rt-family control rows held at 1.00-1.03x.
// An isolating pass returned every collapsed row to 1.00x by hinting the read
// spines alone, with every write win intact. It is the same failure class it
// attributed on the C++ write side — a large body forced whole into a timed
// loop — arriving on Rust's read side, and it is why the C++ write demand
// itself stayed default-off until its own regressions were attributed.
//
// This divergence from the reference is therefore evidence-driven and
// per-backend, the banked rule that refused to transplant a
// C/C++/Rust mechanism into Go sight-unseen. It is a deviation from the C++
// shape, recorded as such: raising the read half to the demand again is a
// measurement, not an edit.
//
// The measured cost of the write demand: +2.6% on the bench binary, +3.6% on
// its __TEXT, +2% clean build wall time. The generated crate's own rlib moves
// +40 bytes, because #[inline(always)] bodies are not codegen'd into it.
//
// Do NOT reach for branch-weight hints instead. Measured across this family
// (the C++ tournament passes), cold hints activate the machine outliner and
// shred the hot bodies; the C++ emitter carries the same standing warning.

// writeSpineInline is the inlining DEMAND every generated write wire function
// is spelled with.
const writeSpineInline = "#[inline(always)]\n"

// readSpineInline is the inlining HINT every generated read wire function
// keeps, including both read dispatch surfaces. See the read-half note above:
// the demand was ported from C++, measured, and refused.
const readSpineInline = "#[inline]\n"

// dispatchInline is the inlining HINT the write dispatch surface keeps.
const dispatchInline = "#[inline]\n"

// emitWriteDispatchComment is the emitted rationale for why write_message —
// the write dispatch surface — keeps the plain #[inline] hint while every
// write spine it calls carries the demand. Mirrors cpp's
// emitWriteDispatchComment: the blanket demand closed every
// per-message write row but regressed the batch dispatch loop, because the
// dispatcher itself was forced whole into the loop body. Exempting it keeps
// the per-message write spines flattening INTO the dispatcher's outlined body
// while the loop keeps a call boundary.
func (g *gen) emitWriteDispatchComment() {
	g.pf("// write_message is deliberately OUTSIDE the write-spine inlining demand:\n")
	g.pf("// plain #[inline], not #[inline(always)]. Its callees (every per-message\n")
	g.pf("// write_*) flatten into its body, but the dispatch match itself stays a\n")
	g.pf("// call boundary — demanding the C++ twin into a batch build loop was\n")
	g.pf("// measured to slow that loop ~21%% while every per-message row kept its\n")
	g.pf("// win with the boundary in place. The compiler remains free\n")
	g.pf("// to inline it where that pays.\n")
}
