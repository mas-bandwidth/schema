package cpp

// emitWriteInlineMacro emits the write-spine inlining switch every generated
// Write function is spelled with (SCHEMA_WRITE_INLINE). Default OFF:
// SCHEMA_WRITE_INLINE expands to plain `inline`, token-identical to what this
// emitter always produced, so an undefined switch changes nothing. Armed
// (-DSCHEMA_WRITE_SPINE_DEMAND), the generated write path demands inlining
// the way the serialize family's per-field spines do (serialize.h's write
// spine; serialize.c SERIALIZE_ALWAYS_INLINE — both halves; the C generated
// spine's SCHEMA_C_WRITE_INLINE; the read twin here, SCHEMA_READ_INLINE —
// all of those now unconditional, their switches retired by the tournament).
//
// This switch alone stays a switch: the confirmation pass (confirmation-air
// r2) showed it sweeping the write rows (+33..+929%, batch write +71%, batch
// read +74% more) while collapsing three bits-heavy rows 73-88% armed —
// bench_bits write, bench_mixed write, probebits read — reproducible in both
// passes. A switch that loses any row 8x hasn't won yet: it stays default-OFF
// until that regression is attributed and resolved.
//
// Why the demand exists (remark evidence, Apple clang 21, -O3, schema bench,
// tournament pass bench/results/tournament-air): the generated Write
// functions are linkonce_odr header functions, and clang refuses them into
// the timed write loops at cost over BOTH thresholds a call site can be held
// to — WriteVec3 at cost=330 against the inline-hint threshold (325, five
// over the line) and against the cold-callsite threshold (45) at
// decay-priced sites, WriteProbeBits 445, WriteQuat 455, WriteInputPacket
// 565, WriteShipData_Shallow 885, WriteShipCreate 1075, and the dispatch
// surface itself, WriteMessage at cost=1555, into the batch build loop. The
// C backend's identical entries armed with their write demand carry no such
// boundary, which is the tournament's armed-vs-armed write gap (C ahead:
// write 65%, batch write 84% of C++ time). No last-call-to-static bonus
// exists for a linkonce_odr function, so unlike C's static entries these
// sites never flatten on their own. The demand is a DEMAND, not a
// branch-weight hint: __builtin_expect-style cold hints were measured in
// this family to activate the machine outliner and shred hot bodies (bits
// write -25%) — do not swap this for hints. Guarded against redefinition
// because several wire headers can land in one translation unit.
func (g *gen) emitWriteInlineMacro() {
	g.pf("#ifndef SCHEMA_WRITE_INLINE_DEFINED\n#define SCHEMA_WRITE_INLINE_DEFINED\n")
	g.pf("// SCHEMA_WRITE_INLINE — how every generated Write function is spelled.\n")
	g.pf("// Default: plain `inline`, exactly what this emitter always produced.\n")
	g.pf("// Define SCHEMA_WRITE_SPINE_DEMAND to make the generated write path DEMAND\n")
	g.pf("// inlining (always_inline / __forceinline), the serialize family's remedy\n")
	g.pf("// for LLVM refusing the linkonce_odr Write entries into their call sites\n")
	g.pf("// at cost over threshold — no last-call-to-static bonus exists for a\n")
	g.pf("// header function, so unlike C's static entries these never flatten on\n")
	g.pf("// their own. Branch-weight hints are NOT the fix here — measured in this\n")
	g.pf("// family to invite the machine outliner into the hot bodies. Do not add them.\n")
	g.pf("#if defined( SCHEMA_WRITE_SPINE_DEMAND )\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_WRITE_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_WRITE_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_WRITE_INLINE inline\n")
	g.pf("#endif\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_WRITE_INLINE inline\n")
	g.pf("#endif\n")
	g.pf("#endif // SCHEMA_WRITE_INLINE_DEFINED\n\n")
}
