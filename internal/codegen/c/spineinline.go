package c

import "github.com/mas-bandwidth/schema/internal/ir"

// fileEmitsWire reports whether this wire file will emit any read/write wire
// function — struct wire pairs, object deep/shallow view pairs, or the owner
// file's message dispatch surface. Files that only carry consts/enums/flags
// emit no wire functions and no macro block.
func (g *gen) fileEmitsWire() bool {
	for _, d := range g.file.Decls {
		switch d.(type) {
		case *ir.Struct, *ir.Object:
			return true
		}
	}
	return g.file.Base == g.msgOwner && len(g.unit.Messages) > 0
}

// emitSpineInlineMacros emits the wire-spine inlining switches every generated
// read/write function is spelled with (SCHEMA_C_READ_INLINE /
// SCHEMA_C_WRITE_INLINE, beside `static SCHEMA_UNUSED`). Default OFF: both
// expand to nothing, token-identical to what this emitter always produced, so
// an undefined switch changes nothing. Armed (-DSCHEMA_C_READ_SPINE_DEMAND /
// -DSCHEMA_C_WRITE_SPINE_DEMAND), the generated read / write spine demands
// inlining the way the serialize family's per-field spines do (serialize.c
// SERIALIZE_ALWAYS_INLINE — both halves; the C++ generated read spine's
// SCHEMA_READ_SPINE_DEMAND, schema 99120c2).
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
// refusal, the same in both directions, hence a write twin the C++ switch
// does not need. The demand is a DEMAND, not a branch-weight hint:
// __builtin_expect-style cold hints were measured in this family to activate
// the machine outliner and shred hot bodies (bits write -25%) — do not swap
// this for hints. Guarded against redefinition because several wire headers
// land in one translation unit.
func (g *gen) emitSpineInlineMacros() {
	g.pf("#ifndef SCHEMA_C_SPINE_INLINE_DEFINED\n#define SCHEMA_C_SPINE_INLINE_DEFINED\n")
	g.pf("/* SCHEMA_C_READ_INLINE / SCHEMA_C_WRITE_INLINE — how every generated wire\n")
	g.pf("   function is spelled, beside `static SCHEMA_UNUSED`. Default: both expand\n")
	g.pf("   to nothing — token-identical to what this emitter always produced.\n")
	g.pf("   Define SCHEMA_C_READ_SPINE_DEMAND / SCHEMA_C_WRITE_SPINE_DEMAND to make\n")
	g.pf("   the generated read / write spine DEMAND inlining (always_inline /\n")
	g.pf("   __forceinline), the serialize family's remedy for LLVM refusing the\n")
	g.pf("   header-static per-message entries into small-message call sites at\n")
	g.pf("   marginal cost over the hot-callsite inline threshold. Branch-weight\n")
	g.pf("   hints are NOT the fix here — measured in this family to invite the\n")
	g.pf("   machine outliner into the hot bodies. Do not add them. */\n")
	g.pf("#if defined( SCHEMA_C_READ_SPINE_DEMAND )\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_C_READ_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_C_READ_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_C_READ_INLINE\n")
	g.pf("#endif\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_C_READ_INLINE\n")
	g.pf("#endif\n")
	g.pf("#if defined( SCHEMA_C_WRITE_SPINE_DEMAND )\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE\n")
	g.pf("#endif\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_C_WRITE_INLINE\n")
	g.pf("#endif\n")
	g.pf("#endif /* SCHEMA_C_SPINE_INLINE_DEFINED */\n\n")
}
