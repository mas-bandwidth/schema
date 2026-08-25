package c

import "github.com/mas-bandwidth/schema/ir"

// fileEmitsWire reports whether this wire file will emit any read/write wire
// function — struct or union wire pairs.
// file's message dispatch surface. Files that only carry consts/enums/flags
// emit no wire functions and no macro block.
func (g *gen) fileEmitsWire() bool {
	for _, d := range g.file.Decls {
		switch d.(type) {
		case *ir.Struct, *ir.Union:
			return true
		}
	}
	return false
}

// emitSpineInlineMacros emits the wire-spine inlining demand every generated
// read/write function is spelled with (SCHEMA_C_READ_INLINE /
// SCHEMA_C_WRITE_INLINE, beside `static SCHEMA_UNUSED`): always_inline /
// __forceinline, unconditionally, the way the serialize family's per-field
// spines demand it (serialize.c SERIALIZE_ALWAYS_INLINE — both halves; the
// C++ generated read spine's SCHEMA_READ_INLINE). Compilers with neither
// spelling get nothing — plain `static`, exactly what this emitter emitted
// before the demand existed (C89 has no inline keyword to fall back on).
//
// Why the demand exists (remark evidence, Apple clang 21, -O3, schema bench):
// the generated per-message entries are header-static functions in the
// consumer's own TU, and clang refuses to inline them into small-message call
// sites at marginal cost over the hot-callsite inline threshold (250) — read
// entries at cost=300 (read_probe_header) through 2345 (read_test_data),
// write entries at cost=270 (write_test, twenty over the line) through 2910
// (write_test_data). The identical generated C++ entries inline (thresholds
// 250-325 with the same costs priced lower on the C++ spellings), which is
// the measured C small-message call-boundary class: C rows 3-4.5x slower on
// probe_header-sized messages. Unlike the C++ read-spine case this is not
// the fallible-chain frequency decay — it is a plain cost-over-threshold
// refusal, the same in both directions, hence the demand covers both. Both
// halves swept their tournament A/Bs with zero regressions across 34 rows
// (tournament-air; the write half's rigidbody_moving write +1016%;
// reconfirmed confirmation-air r2). The demand is a DEMAND, not a
// branch-weight hint:
// __builtin_expect-style cold hints were measured in this family to activate
// the machine outliner and shred hot bodies (bits write -25%) — do not swap
// this for hints. Guarded against redefinition because several wire headers
// land in one translation unit.
func (g *gen) emitSpineInlineMacros() {
	g.pf("#ifndef SCHEMA_C_SPINE_INLINE_DEFINED\n#define SCHEMA_C_SPINE_INLINE_DEFINED\n")
	g.pf("/* SCHEMA_C_READ_INLINE / SCHEMA_C_WRITE_INLINE — how every generated wire\n")
	g.pf("   function is spelled, beside `static SCHEMA_UNUSED`: an inlining DEMAND\n")
	g.pf("   (always_inline / __forceinline), the serialize family's remedy for LLVM\n")
	g.pf("   refusing the header-static per-message entries into small-message call\n")
	g.pf("   sites at marginal cost over the hot-callsite inline threshold. Measured\n")
	g.pf("   and shipped; compilers with neither spelling get plain `static`.\n")
	g.pf("   Branch-weight hints are NOT the fix here — measured in this family to\n")
	g.pf("   invite the machine outliner into the hot bodies. Do not add them. */\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_C_READ_INLINE __forceinline\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_C_READ_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_C_READ_INLINE\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE\n")
	g.pf("#endif\n")
	g.pf("#endif /* SCHEMA_C_SPINE_INLINE_DEFINED */\n\n")
}
