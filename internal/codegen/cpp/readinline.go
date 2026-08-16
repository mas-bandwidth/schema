package cpp

import "github.com/mas-bandwidth/schema/internal/ir"

// fileEmitsWire reports whether this wire file will emit any Write/Read wire
// function — struct/object wire pairs, or the owner files' message/object
// dispatch surfaces. Files that only carry consts/enums/flags emit no wire
// functions and no macro blocks. (Named after the C backend's twin: every
// file that emits one half of a wire pair emits the other, so one predicate
// guards both macro blocks.)
func (g *gen) fileEmitsWire() bool {
	for _, d := range g.file.Decls {
		switch d.(type) {
		case *ir.Struct, *ir.Object:
			return true
		}
	}
	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		return true
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		return true
	}
	return false
}

// emitReadInlineMacro emits the read-spine inlining demand every generated
// Read function is spelled with (SCHEMA_READ_INLINE): always_inline /
// __forceinline, unconditionally, the way the serialize family's per-field
// spines demand it (serialize.c SERIALIZE_ALWAYS_INLINE; serialize.h — both
// spines). Compilers with neither spelling fall back to plain `inline` and
// lose only the optimization.
//
// Why the demand exists (remark evidence, Apple clang 21, -O3, schema bench):
// a read chain is fallible, LLVM prices each Ok/Err split at ~even odds, so
// block frequency decays geometrically down the chain and later call sites
// are held to the cold-callsite inline threshold (45, vs 250+ hot). The
// batch-read dispatch loop shows it exactly: ReadMessage is refused into the
// timed loop at cost=1055 against threshold=45, while the C backend's
// read_message — static in one TU — inlines into its identical loop
// (last-call-to-static, cost=-13285) and carries no per-message call at all.
// The demand shipped first as a default-off switch, then won its tournament
// (tournament-air off→armed with the serialize read demand: batch read +68%,
// rigidbody_at_rest read +244%, testdata read +109%, zero regressions;
// reconfirmed confirmation-air r2), so per the feature lifecycle the winner
// became unconditional code and the switch was deleted. The demand is a
// DEMAND, not a branch-weight hint: __builtin_expect-style cold hints were
// measured in this family to activate the machine outliner and shred hot
// bodies (bits write -25%) — do not swap this for hints. Guarded against
// redefinition because several wire headers can land in one translation unit.
func (g *gen) emitReadInlineMacro() {
	g.pf("#ifndef SCHEMA_READ_INLINE_DEFINED\n#define SCHEMA_READ_INLINE_DEFINED\n")
	g.pf("// SCHEMA_READ_INLINE — how every generated Read function is spelled: an\n")
	g.pf("// inlining DEMAND (always_inline / __forceinline), the serialize family's\n")
	g.pf("// remedy for LLVM's fallible-chain frequency decay: each Ok/Err split is\n")
	g.pf("// priced at ~even odds, block frequency decays geometrically down a read\n")
	g.pf("// chain, and later call sites are held to the cold-callsite inline\n")
	g.pf("// threshold. Measured and shipped; branch-weight hints are NOT the fix\n")
	g.pf("// here — measured in this family to invite the machine outliner into the\n")
	g.pf("// hot bodies. Do not add them.\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_READ_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_READ_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_READ_INLINE inline\n")
	g.pf("#endif\n")
	g.pf("#endif // SCHEMA_READ_INLINE_DEFINED\n\n")
}
