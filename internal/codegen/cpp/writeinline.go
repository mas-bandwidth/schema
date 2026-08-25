package cpp

// emitWriteInlineMacro emits the write-spine inlining demand every generated
// Write function is spelled with (SCHEMA_WRITE_INLINE): always_inline /
// __forceinline, unconditionally, the way the serialize family's per-field
// spines demand it (serialize.h's write spine; serialize.c
// SERIALIZE_ALWAYS_INLINE — both halves; the C generated spine's
// SCHEMA_C_WRITE_INLINE; the read twin here, SCHEMA_READ_INLINE). Every one
// except WriteMessage, the dispatch surface, which emitWriteDispatchComment
// below documents and which is spelled plain `inline` in functions.go —
// that scoping is part of the winner. Compilers with neither spelling fall
// back to plain `inline` and lose only the optimization.
//
// Why the demand exists (remark evidence, Apple clang 21, -O3, schema bench,
// tournament pass bench/results/tournament-air): the generated Write
// functions are linkonce_odr header functions, and clang refuses them into
// the timed write loops at cost over BOTH thresholds a call site can be held
// to — WriteVec3 at cost=330 against the inline-hint threshold (325, five
// over the line) and against the cold-callsite threshold (45) at
// decay-priced sites, WriteProbeBits 445, WriteQuat 455, WriteInputPacket
// 565, WriteShipData_Shallow 885, WriteShipCreate 1075. No
// last-call-to-static bonus exists for a linkonce_odr function, so unlike
// C's static entries these sites never flatten on their own.
//
// The demand is SCOPED: blanket — WriteMessage included — it swept the
// per-message write rows but demanded the dispatcher (inline cost ~1555)
// whole into the batch build loop and regressed it; scoped — WriteMessage
// exempted, its callees still flattening into its outlined body — it won
// the cross-architecture tournament (bench/results/tournament-air: blended
// geomean 81.8 vs 99.7 off on Zen 4 gcc, 100.8 vs 137.8 off on Apple
// M-series clang, % of the C baseline). The accepted residual:
// message_batch write +7-16% vs no demand on both architectures — bounded,
// and bought back many times over by the per-message rows. The demand is a
// DEMAND, not a branch-weight hint: __builtin_expect-style cold hints were
// measured in this family to activate the machine outliner and shred hot
// bodies (bits write -25%) — do not swap this for hints. Guarded against
// redefinition because several wire headers can land in one translation
// unit.
func (g *gen) emitWriteInlineMacro() {
	g.pf("#ifndef SCHEMA_WRITE_INLINE_DEFINED\n#define SCHEMA_WRITE_INLINE_DEFINED\n")
	g.pf("// SCHEMA_WRITE_INLINE — how every generated Write function is spelled,\n")
	g.pf("// EXCEPT the WriteMessage dispatch surface (see the comment on it): an\n")
	g.pf("// inlining DEMAND (always_inline / __forceinline), the serialize family's\n")
	g.pf("// remedy for LLVM refusing the linkonce_odr Write entries into their call\n")
	g.pf("// sites at cost over threshold — no last-call-to-static bonus exists for a\n")
	g.pf("// header function, so unlike C's static entries these never flatten on\n")
	g.pf("// their own. Measured and shipped; branch-weight hints are NOT the fix\n")
	g.pf("// here — measured in this family to invite the machine outliner into the\n")
	g.pf("// hot bodies. Do not add them.\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_WRITE_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_WRITE_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_WRITE_INLINE inline\n")
	g.pf("#endif\n")
	g.pf("#endif // SCHEMA_WRITE_INLINE_DEFINED\n\n")
}

// emitWriteDispatchComment is the emitted rationale for why WriteMessage — the
// dispatch surface — is spelled plain `inline` and NOT SCHEMA_WRITE_INLINE.
// The scoping (write-spine-scoped, 2026-08-17): the blanket demand closed
// every per-message write row but regressed the batch dispatch loop
// (message_batch write +21%, Zen 4 gcc -O3) — WriteMessage at inline cost
// ~1555 forced whole into the loop body. Exempting the dispatcher keeps the
// per-message spines flattening INTO WriteMessage's outlined body (its
// callees carry the demand) while the loop keeps a call boundary.
func (g *gen) emitWriteDispatchComment() {
	g.pf("// WriteMessage is deliberately OUTSIDE the write-spine demand: plain `inline`,\n")
	g.pf("// not SCHEMA_WRITE_INLINE. Its callees (every per-message Write) flatten into\n")
	g.pf("// its body, but the dispatch switch itself stays a call boundary — demanding\n")
	g.pf("// it into a batch build loop was measured to slow that loop ~21%% while every\n")
	g.pf("// per-message row kept its win with the boundary in place. The compiler\n")
	g.pf("// remains free to inline it where that pays.\n")
}
