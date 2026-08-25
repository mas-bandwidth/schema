package cpp

// emitWriteInlineMacro emits the write-spine inlining demand every generated
// Write function is spelled with (SCHEMA_WRITE_INLINE): always_inline /
// __forceinline, unconditionally, the way the serialize family's per-field
// spines demand it (serialize.h's write spine; serialize.c
// SERIALIZE_ALWAYS_INLINE — both halves; the C generated spine's
// SCHEMA_C_WRITE_INLINE; the read twin here, SCHEMA_READ_INLINE).
// Compilers with neither spelling fall back to plain `inline` and lose only
// the optimization.
//
// Why the demand exists (remark evidence, Apple clang 21, -O3, schema bench,
// tournament pass bench/results/tournament-air): the generated Write
// functions are linkonce_odr header functions, and clang refuses them into
// the timed write loops at cost over BOTH thresholds a call site can be held
// to — WriteVec3 at cost=330 against the inline-hint threshold (325, five
// over the line) and against the cold-callsite threshold (45) at
// decay-priced sites, WriteProbeBits 445, WriteQuat 455, WriteInputPacket
// 565, WriteShipCreate 1075. No
// last-call-to-static bonus exists for a linkonce_odr function, so unlike
// C's static entries these sites never flatten on their own. The demand won
// the cross-architecture tournament (bench/results/tournament-air: blended
// geomean 81.8 vs 99.7 off on Zen 4 gcc, 100.8 vs 137.8 off on Apple
// M-series clang, % of the C baseline). The demand is a
// DEMAND, not a branch-weight hint: __builtin_expect-style cold hints were
// measured in this family to activate the machine outliner and shred hot
// bodies (bits write -25%) — do not swap this for hints. Guarded against
// redefinition because several wire headers can land in one translation
// unit.
func (g *gen) emitWriteInlineMacro() {
	g.pf("#ifndef SCHEMA_WRITE_INLINE_DEFINED\n#define SCHEMA_WRITE_INLINE_DEFINED\n")
	g.pf("// SCHEMA_WRITE_INLINE — how every generated Write function is spelled: an\n")
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
