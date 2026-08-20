package cpp

// emitWriteInlineMacro emits the write-spine inlining demand every generated
// Write function is spelled with (SCHEMA_WRITE_INLINE): always_inline /
// __forceinline, unconditionally, the way the serialize family's per-field
// spines demand it (serialize.h's write spine; serialize.c
// SERIALIZE_ALWAYS_INLINE — both halves; the C generated spine's
// SCHEMA_C_WRITE_INLINE; the read twin here, SCHEMA_READ_INLINE). Every one
// except the two pieces of the message dispatch surface — WriteMessage
// itself and its per-message arm forwarders, both spelled plain `inline` in
// functions.go and documented by emitWriteDispatchComment /
// emitWriteArmComment below. That scoping is part of the winner. Compilers
// with neither spelling fall back to plain `inline` and lose only the
// optimization.
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
// The demand shipped first as a default-off switch
// (SCHEMA_WRITE_SPINE_DEMAND, #55). Blanket — WriteMessage included — it
// swept the per-message write rows but demanded the dispatcher (inline cost
// ~1555) whole into the batch build loop and regressed it. Scoped —
// WriteMessage exempted — it won the cross-architecture tournament (eras
// space-55-scoped / air-55-scoped: blended geomean 81.8 vs 99.7 off on Zen 4
// gcc, 100.8 vs 137.8 off on Apple M-series clang, % of the C baseline), so
// per the feature lifecycle the winner became unconditional code and the
// switch was deleted. The scoping is now BOTH pieces of the dispatch
// surface: exempting WriteMessage alone still let the demand propagate
// through it into its own body, and the arm forwarders
// (emitWriteArmComment) close that. The demand is a DEMAND, not a
// branch-weight hint:
// __builtin_expect-style cold hints were measured in this family to
// activate the machine outliner and shred hot bodies (bits write -25%) — do
// not swap this for hints. Guarded against redefinition because several
// wire headers can land in one translation unit.
func (g *gen) emitWriteInlineMacro() {
	g.pf("#ifndef SCHEMA_WRITE_INLINE_DEFINED\n#define SCHEMA_WRITE_INLINE_DEFINED\n")
	g.pf("// SCHEMA_WRITE_INLINE — how every generated Write function is spelled,\n")
	g.pf("// EXCEPT the message dispatch surface: WriteMessage and its per-message arm\n")
	g.pf("// forwarders (see the comments on them). It is an\n")
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
// loop's call boundary; the arm forwarders below (emitWriteArmComment) keep
// the spine bodies from being FORCED into the dispatch body behind it.
func (g *gen) emitWriteDispatchComment() {
	g.pf("// WriteMessage is deliberately OUTSIDE the write-spine demand: plain `inline`,\n")
	g.pf("// not SCHEMA_WRITE_INLINE — demanding it into a batch build loop was measured\n")
	g.pf("// to slow that loop ~21%% while every per-message row kept its win with the\n")
	g.pf("// boundary in place. It reaches each message through the WriteMessageArm*\n")
	g.pf("// forwarders above rather than calling the demanded spines directly, so no\n")
	g.pf("// spine body is forced into this one. The compiler remains free to inline\n")
	g.pf("// this — and any arm — where that pays.\n")
}

// emitWriteArmComment is the emitted rationale for the dispatch arms: the
// per-message forwarders WriteMessage calls instead of the demanded spines.
// The residual #60 shipped and accepted (message_batch write +7-16% vs the
// pre-demand state on both architectures) was the demand propagating THROUGH
// the dispatch: always_inline is transitive across a call, so exempting
// WriteMessage bought a call boundary for the loop but still dragged every
// message body into WriteMessage's own body. Measured on the schema bench
// (Apple clang -O3, examples corpus): the dispatch body ran 1832 bytes armed
// against 1288 pre-demand, the 544-byte difference being exactly the two
// spines — WriteTest 324, WriteTimescale 332 — the compiler had declined on
// cost and the demand then forced in. A plain-`inline` forwarder per arm
// breaks the propagation without weakening the demand anywhere: the spine
// still flattens into its arm, and the arm is then an ordinary inline
// candidate the compiler judges on cost at the dispatch site, exactly as it
// judged the spine itself before the demand existed. It reconstructs the
// pre-demand dispatch body to the byte.
func (g *gen) emitWriteArmComment() {
	g.pf("// The dispatch arms: one forwarder per message, standing between WriteMessage\n")
	g.pf("// and that message's Write spine. Spelled plain `inline`, NOT\n")
	g.pf("// SCHEMA_WRITE_INLINE, and that is the whole point — an inlining demand is\n")
	g.pf("// transitive across a call, so a dispatch calling the spines directly would\n")
	g.pf("// drag every message body into its own body whatever the cost, which is what\n")
	g.pf("// made the batch build loop pay for the demand. Behind a plain-inline\n")
	g.pf("// forwarder the demand still flattens the spine into the arm, and the\n")
	g.pf("// compiler then decides per arm — on cost, the way it decided before the\n")
	g.pf("// demand existed — whether that arm lands inside the dispatch body or stays\n")
	g.pf("// a call. Do not spell these SCHEMA_WRITE_INLINE; that is the shape this\n")
	g.pf("// exists to avoid.\n")
}
