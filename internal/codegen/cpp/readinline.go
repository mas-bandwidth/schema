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

// emitReadInlineMacro emits the read-spine inlining switch every generated
// Read function is spelled with (SCHEMA_READ_INLINE). Default OFF:
// SCHEMA_READ_INLINE expands to plain `inline`, token-identical to what this
// emitter always produced, so an undefined switch changes nothing. Armed
// (-DSCHEMA_READ_SPINE_DEMAND), the generated read path demands inlining the
// way the serialize family's per-field spines do (serialize.c
// SERIALIZE_ALWAYS_INLINE; serialize.h's write spine).
//
// Why the demand exists (remark evidence, Apple clang 21, -O3, schema bench):
// a read chain is fallible, LLVM prices each Ok/Err split at ~even odds, so
// block frequency decays geometrically down the chain and later call sites
// are held to the cold-callsite inline threshold (45, vs 250+ hot). The
// batch-read dispatch loop shows it exactly: ReadMessage is refused into the
// timed loop at cost=1055 against threshold=45, while the C backend's
// read_message — static in one TU — inlines into its identical loop
// (last-call-to-static, cost=-13285) and carries no per-message call at all.
// The demand is a DEMAND, not a branch-weight hint: __builtin_expect-style
// cold hints were measured in this family to activate the machine outliner
// and shred hot bodies (bits write -25%) — do not swap this for hints.
// Guarded against redefinition because several wire headers can land in one
// translation unit.
func (g *gen) emitReadInlineMacro() {
	g.pf("#ifndef SCHEMA_READ_INLINE_DEFINED\n#define SCHEMA_READ_INLINE_DEFINED\n")
	g.pf("// SCHEMA_READ_INLINE — how every generated Read function is spelled.\n")
	g.pf("// Default: plain `inline`, exactly what this emitter always produced.\n")
	g.pf("// Define SCHEMA_READ_SPINE_DEMAND to make the generated read path DEMAND\n")
	g.pf("// inlining (always_inline / __forceinline), the serialize family's remedy\n")
	g.pf("// for LLVM's fallible-chain frequency decay: each Ok/Err split is priced\n")
	g.pf("// at ~even odds, block frequency decays geometrically down a read chain,\n")
	g.pf("// and later call sites are held to the cold-callsite inline threshold.\n")
	g.pf("// Branch-weight hints are NOT the fix here — measured in this family to\n")
	g.pf("// invite the machine outliner into the hot bodies. Do not add them.\n")
	g.pf("#if defined( SCHEMA_READ_SPINE_DEMAND )\n")
	g.pf("#if defined( _MSC_VER )\n")
	g.pf("#define SCHEMA_READ_INLINE __forceinline\n")
	g.pf("#elif defined( __GNUC__ ) || defined( __clang__ )\n")
	g.pf("#define SCHEMA_READ_INLINE inline __attribute__(( always_inline ))\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_READ_INLINE inline\n")
	g.pf("#endif\n")
	g.pf("#else\n")
	g.pf("#define SCHEMA_READ_INLINE inline\n")
	g.pf("#endif\n")
	g.pf("#endif // SCHEMA_READ_INLINE_DEFINED\n\n")
}
