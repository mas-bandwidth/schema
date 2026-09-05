// The per-table VARIABLE-LENGTH surface (docs/SPEC-TABLES.md §2, §6, §9): the
// allocation accessors, the pack walkers behind Lock, the wire sizing
// pre-pass behind Load, and the Builder itself.
//
// Nothing here is emitted for a FIXED-SIZE table. A table whose by-value
// closure holds no pointer gets its struct and its three free functions —
// <Name>Measure, <Name>Save, <Name>Load — and not one byte more, which is the
// point of deriving the mode instead of declaring it.
package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- mode helpers: what a call site must pass ----

func (g *tableGen) isVar(name string) bool { return g.variable[name] }

// ---- NO EDGE CHARGES WIRE DEPTH (docs/SPEC-TABLES.md §3.1) ----
//
// A pointer field rides as a u32 INDEX into the flat node table, so a pointer
// edge is not a nesting level: a chain's length is not a depth, and there is no
// depth cap on the wire in either direction. What nesting remains is BY-VALUE
// nesting, whose depth is fixed by the SCHEMA and cannot be driven by data,
// because §2 refuses by-value cycles.
//
// The walks that still recurse over pointer edges are the AUTHORING side's —
// the numbering and the pack — and what bounds those is the identity map, not a
// counter: a reference to an entry still open is a cycle, refused by name (§6.2).

// measureCall renders a nested MEASURE call on a closure member: a variable
// member takes the resolution context, a fixed one takes none — so a fixed
// table's codec is character-for-character what it was before pointers existed.
func (g *tableGen) measureCall(name, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sMeasureBody( ctx, numbering, ids, %s )", name, expr)
	}
	return fmt.Sprintf("%sMeasureBody( ids, %s )", name, expr)
}

func (g *tableGen) saveCall(name, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sSaveBody( ctx, numbering, w, ids, %s )", name, expr)
	}
	return fmt.Sprintf("%sSaveBody( w, ids, %s )", name, expr)
}

func (g *tableGen) loadCall(name, reader, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sLoadBody( %s, nodes, %s )", name, reader, expr)
	}
	return fmt.Sprintf("%sLoadBody( %s, %s )", name, reader, expr)
}

// walker returns true when a member needs the pointer-graph walkers: every
// variable member, plus every table some pointer targets (a pointed-at table
// may itself be pointer-free, and still needs to be allocated, packed,
// measured and bounds-walked).
func (g *tableGen) needsWalkers(name string) bool { return g.variable[name] || g.targets[name] }

// varMembers filters a file's closure members down to those needing the
// variable-length surface, in emission order.
func (g *tableGen) varMembers(members []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range members {
		if g.needsWalkers(st.Name) {
			out = append(out, st)
		}
	}
	return out
}

// blobWord is the keyword a byte buffer field was declared with.
func blobWord(f *ir.Field) string {
	if f.Type.Kind == ir.TString {
		return "string"
	}
	return "bytes"
}

// blobTerminated reports whether a byte buffer's node carries the zero byte a
// *string's does (docs/SPEC-TABLES.md §6.3).
func blobTerminated(f *ir.Field) bool { return f.Type.Kind == ir.TString }

// blobTypeIdConst is the generated runtime's name for a byte buffer's reserved
// type id (docs/SPEC-TABLES.md §3.1).
// blobTypeId is the RESERVED node type id a byte buffer's record rides under
// (docs/SPEC-TABLES.md §2.5, §3.1), as a value — what blobTypeIdConst spells
// as the emitted constant's name.
func blobTypeId(f *ir.Field) uint64 {
	if f.Type.Kind == ir.TString {
		return ir.StringWireTypeId
	}
	return ir.BytesWireTypeId
}

func blobTypeIdConst(f *ir.Field) string {
	if f.Type.Kind == ir.TString {
		return "kTableStringTypeId"
	}
	return "kTableBytesTypeId"
}

// pointeeName spells what a pointer field names, for a comment: the target
// table, or the buffer keyword.
func pointeeName(f *ir.Field) string {
	if f.Type.Blob() {
		return blobWord(f)
	}
	return f.Type.Name
}

// ---- THE WALK (docs/SPEC-TABLES.md §3.1): one shape, three emitters ----
//
// The numbering, the pack measure and the pack are ONE depth-first walk over a
// member's fields in DECLARATION ORDER that descends every by-value edge IN
// PLACE — a nested variable table, an element of a bounded or keyed array of
// them, a union's set arm — to reach the pointer fields inside it. The three
// emitters differ only in what they do at an edge, so they share the walk
// rather than each spelling its own: a node's first visit is then the same in
// all three by construction, and the region Lock lays out is in the order the
// wire numbers (§6.3).
//
// A BYTE BUFFER slot is an edge of the same walk (§2.5): its node takes an
// index where the field is declared, like any other, and reaches nothing, so
// its descent closes at once.
//
// A walk that GROUPS the edges — every pointer field, then every blob, then
// every nesting, then every arm — is a different order whenever an edge
// declared before another reaches a shared node first, and the tool
// (internal/tablewire) numbers by the page; the corpus's stream_arm_first is
// the value that tells the two apart.

// edgeExpr is ONE STORAGE PATH, spelled under each subject the walk runs over:
// `value` for the numbering and the pack measure, and the `src` / `dst` PAIR
// the pack copies between. The walk builds both by construction from one path,
// so no emitter derives a twin expression from the other's TEXT.
type edgeExpr struct {
	Src string // the path under the read subject
	Dst string // the same path under the write subject; "" when there is none
}

// edgeVisitor is what one emitter does at the kinds of edge the walk reaches.
// read is the expression the fields hang off; write is its twin where the
// emitter copies (the pack), "" where it does not.
type edgeVisitor struct {
	read  string
	write string
	// pointer is one live pointer slot: the field itself, one element of an
	// array of pointers (§2.1), or a POINTER ARM (§2.6)
	pointer func(f *ir.Field, slot edgeExpr)
	// blob is one byte buffer slot, `*bytes` or `*string` (§2.5)
	blob func(f *ir.Field, slot edgeExpr)
	// descend is one by-value edge into a VARIABLE table — a nested field, an
	// array element, a keyed slot, or a union's set arm — given the table's
	// name, the element expression and the indent the call sits at
	descend func(table string, expr edgeExpr, indent string)
	// mapField is one `map[K]V` whose entries the walk descends, in ASCENDING
	// key order (docs/SPEC-TABLES.md §2.8). Each emitter spells its own,
	// because the three differ in what they hold beside the sorted cursor —
	// and because a map's entries are not a path under the write subject: the
	// pack's twin is the array it already placed in the node's extent.
	mapField func(f *ir.Field)
}

// at spells one storage PATH — ".field", ".field[i]", ".body.chunk" — under
// both subjects.
func (v edgeVisitor) at(path string) edgeExpr {
	out := edgeExpr{Src: v.read + path}
	if v.write != "" {
		out.Dst = v.write + path
	}
	return out
}

// edgeKind is what one field is to the walk.
type edgeKind int

const (
	edgeNone    edgeKind = iota
	edgePointer          // *T, [N]*T, [..N]*T
	edgeBlob             // *bytes, *string (§2.5)
	edgeNested           // a VARIABLE table by value, alone or as an array
	edgeArm              // a union with at least one VARIABLE table arm (§2.6)
	// edgeMap is a `map[K]V` whose ENTRY reaches a node: the walk descends the
	// entries in ASCENDING KEY ORDER, the order the wire carries and the order
	// a region holds, so no walk needs a second one (docs/SPEC-TABLES.md §2.8).
	// A map whose entry reaches nothing is not an edge — its extent is the
	// extent walk's, not this one's.
	edgeMap
)

func (g *tableGen) edgeOf(f *ir.Field) edgeKind {
	if f.IsMap() {
		if g.noVariableEdges(f.MapEntry) {
			return edgeNone
		}
		return edgeMap
	}
	if f.Type.Blob() {
		return edgeBlob
	}
	if f.Type.Pointer {
		return edgePointer
	}
	if f.Type.Kind != ir.TNamed {
		return edgeNone
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		if g.isVar(ref.Name) {
			return edgeNested
		}
	case *ir.Union:
		if g.unionHasEdge(ref, map[*ir.Union]bool{}) {
			return edgeArm
		}
	}
	return edgeNone
}

// unionHasEdge reports whether a union's arms reach a node (§2.6): an arm that
// is a POINTER or a byte buffer is itself the edge, an arm that is a
// variable-length table by value carries edges inside it, and an arm that is
// another union asks the same question one level in.
func (g *tableGen) unionHasEdge(un *ir.Union, seen map[*ir.Union]bool) bool {
	if seen[un] {
		return false
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if v.F.Type.Pointer {
			return true
		}
		if v.Type != "" && g.isVar(v.Type) {
			return true
		}
		if inner, ok := v.F.Type.Ref.(*ir.Union); ok && g.unionHasEdge(inner, seen) {
			return true
		}
	}
	return false
}

// noVariableEdges reports a member with no node below it: no pointer field, no
// byte buffer, no by-value variable nesting and no union with a variable arm.
func (g *tableGen) noVariableEdges(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if g.edgeOf(f) != edgeNone {
			return false
		}
	}
	return true
}

// emitEdgeWalk emits the walk over one member for one emitter.
// A FIELD THE WRITER DOES NOT WRITE IS NOT AN EDGE (docs/SPEC-TABLES.md §3.1):
// a pointer under a false guard, or inside an absent optional, is not visited
// and its target takes no index, so a save never writes a record no written
// field names. It is the same walk `Lock` lays a region out in, so the region
// holds no such node either.
func (g *tableGen) emitEdgeWalk(st *ir.Struct, v edgeVisitor) {
	guards := guardWalk(st, v.read+".")
	for _, f := range st.Fields {
		if g.edgeOf(f) == edgeNone {
			continue
		}
		open := ""
		if cond, guarded := guards[f.Name]; guarded {
			open = cond
		}
		if f.Type.Optional {
			present := v.read + "." + f.Name + "_present"
			if open == "" {
				open = present
			} else {
				open = open + " && " + present
			}
		}
		if open != "" {
			g.pf("    if ( %s )\n    {\n", open)
		}
		g.emitEdgeOf(f, v)
		if open != "" {
			g.pf("    }\n")
		}
	}
}

func (g *tableGen) emitEdgeOf(f *ir.Field, v edgeVisitor) {
	switch g.edgeOf(f) {
	case edgePointer:
		g.emitPointerSlots(f, v, func(slot edgeExpr) { v.pointer(f, slot) })
	case edgeBlob:
		v.blob(f, v.at("."+f.Name))
	case edgeNested:
		g.emitVariableByValueWalk(f, v, func(expr edgeExpr) { v.descend(f.Type.Name, expr, "        ") })
	case edgeArm:
		g.emitVariableUnionWalk(f, v)
	case edgeMap:
		v.mapField(f)
	}
}

// emitVariableByValueWalk emits the loop shape for descending into a by-value
// nested variable table — a field, a bounded array or a keyed one — under the
// walk's subjects, and calls body with the element path.
func (g *tableGen) emitVariableByValueWalk(f *ir.Field, v edgeVisitor, body func(expr edgeExpr)) {
	switch f.Array {
	case ir.ArrayNone:
		g.pf("    { // %s (nested by value)\n", f.Name)
		body(v.at("." + f.Name))
		g.pf("    }\n")
	case ir.ArrayCounted:
		g.pf("    for ( int32_t i = 0; i < %s.%s_count && i < %d; i++ ) // %s\n    {\n", v.read, f.Name, f.ArrayBound, f.Name)
		body(v.at(fmt.Sprintf(".%s[i]", f.Name)))
		g.pf("    }\n")
	default:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s\n    {\n", f.ArrayBound, f.Name)
		body(v.at(g.arrayBase(".", f) + "[i]"))
		g.pf("    }\n")
	}
}

// ---- declarations ----

func (g *tableGen) emitCodecDeclarations(members []*ir.Struct) {
	g.emitArenaResetHook(members)
	if vars := g.varMembers(members); len(vars) > 0 {
		g.pf("// ---- pointer targets: allocation and resolution (docs/SPEC-TABLES.md §2) ----\n")
		g.pf("//\n")
		g.pf("// A reference resolves differently in the two forms, and the CONTEXT says\n")
		g.pf("// which: in the arena it is an offset; in a region it is a self-relative\n")
		g.pf("// delta, so the const deref below is one add and needs no base pointer.\n\n")
		for _, st := range members {
			if !g.targets[st.Name] {
				continue
			}
			g.emitPointerTargetSurface(st)
		}
	}
	g.pf("// ---- codecs: measure/save/load per closure member ----\n\n")
	for _, st := range members {
		if g.isVar(st.Name) {
			g.pf("template <typename Ctx> inline int64_t %sMeasureBody( const Ctx & ctx, const TableNumbering & numbering, TableIds & ids, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBodyFields( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, TableIds & ids, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBody( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, TableIds & ids, const %s & value );\n", st.Name, st.Name)
			g.pf("inline bool %sLoadBody( TableReader & r, const TableNodeMap & nodes, %s & value );\n", st.Name, st.Name)
			continue
		}
		g.pf("inline int64_t %sMeasureBody( TableIds & ids, const %s & value );\n", st.Name, st.Name)
		g.pf("%s bool %sSaveBody( TableWriter & w, TableIds & ids, const %s & value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
		g.pf("%s bool %sLoadBody( TableReader & r, %s & value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
	}
	g.pf("\n")
	if vars := g.varMembers(members); len(vars) > 0 {
		g.pf("// ---- pointer-graph walkers: number (measure/save), pack (Lock) ----\n\n")
		for _, st := range vars {
			g.pf("template <typename Ctx> inline bool %sNumber( const Ctx & ctx, TableNumbering & numbering, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline int64_t %sPackMeasure( const Ctx & ctx, TablePackMap & seen, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sPack( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used );\n", st.Name, st.Name, st.Name)
		}
		g.pf("\n")
		g.emitNodeThunkOverloads(vars)
	}
}

// emitNodeThunkOverloads bridges the numbering's type-erased entries back to
// each member's own codec (docs/SPEC-TABLES.md §3.1).
//
// The numbering stores an instantiation at the site it numbers a node, where
// the target's type is STATICALLY known, and these overloads are what the
// instantiation resolves through — reached by argument-dependent lookup on the
// member's own namespace, exactly as the arena's TableReset hook is. That is
// what lets ONE numbering span the files of a unit: a file names only the
// members it declares, and the file that nests them picks these up through the
// include it already has.
func (g *tableGen) emitNodeThunkOverloads(vars []*ir.Struct) {
	g.pf("// ---- the numbering's bridge to each member's codec (docs/SPEC-TABLES.md §3.1) ----\n\n")
	for _, st := range vars {
		if !g.isVar(st.Name) {
			// a pointer target may itself be pointer-free, and a FIXED table's
			// codec is character-for-character what it was before pointers
			// existed — it takes neither the context nor the numbering
			g.pf("template <typename Ctx> inline int64_t TableNodeMeasure( const Ctx &, const TableNumbering &, TableIds & ids, const %s & value ) { return %sMeasureBody( ids, value ); }\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool TableNodeSave( const Ctx &, const TableNumbering &, TableWriter & w, TableIds & ids, const %s & value ) { return %sSaveBody( w, ids, value ); }\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline int64_t TableNodeMessageMeasure( const Ctx &, const TableNumbering &, int64_t, int64_t at, const %s & value ) { return %sMeasureMessageBody( at, value ); }\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool TableNodeMessageSave( const Ctx &, const TableNumbering &, int64_t, TableBitWriter & w, const %s & value ) { return %sSaveMessageBody( w, value ); }\n", st.Name, st.Name)
			continue
		}
		g.pf("template <typename Ctx> inline int64_t TableNodeMeasure( const Ctx & ctx, const TableNumbering & numbering, TableIds & ids, const %s & value ) { return %sMeasureBody( ctx, numbering, ids, value ); }\n", st.Name, st.Name)
		g.pf("template <typename Ctx> inline bool TableNodeSave( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, TableIds & ids, const %s & value ) { return %sSaveBody( ctx, numbering, w, ids, value ); }\n", st.Name, st.Name)
		g.pf("template <typename Ctx> inline int64_t TableNodeMessageMeasure( const Ctx & ctx, const TableNumbering & numbering, int64_t index_bits, int64_t at, const %s & value ) { return %sMeasureMessageBody( ctx, numbering, index_bits, at, value ); }\n", st.Name, st.Name)
		g.pf("template <typename Ctx> inline bool TableNodeMessageSave( const Ctx & ctx, const TableNumbering & numbering, int64_t index_bits, TableBitWriter & w, const %s & value ) { return %sSaveMessageBody( ctx, numbering, index_bits, w, value ); }\n", st.Name, st.Name)
	}
	g.pf("\n")
}

// emitArenaResetHook gives the arena's generic Alloc a way to reach a node's
// declared defaults.
//
// TableWorker::Alloc is a TEMPLATE — it cannot spell `<Name>Reset` — and a
// node it hands back must hold exactly the declared defaults, so the two are
// bridged by an overload set the template finds by argument-dependent lookup
// on T's own namespace. One forwarding line per closure member; the arena is
// the only caller, so this is emitted only into a unit that HAS an arena and a
// pointer-free unit's header is byte-identical without it.
//
// Every closure member gets one, not only the pointer targets: a table gains
// and loses pointers as an edit, and `Alloc<T>` failing to compile for want of
// an overload is a worse answer than a line that costs nothing.
func (g *tableGen) emitArenaResetHook(members []*ir.Struct) {
	if !g.anyVariable || len(members) == 0 {
		return
	}
	g.pf("// ---- the arena's reset hook (docs/SPEC-TABLES.md §6) ----\n")
	g.pf("//\n")
	g.pf("// TableWorker::Alloc is a template and cannot name a member's Reset, so\n")
	g.pf("// the arena reaches it through this overload set by argument-dependent\n")
	g.pf("// lookup. It is how a node born in raw arena storage comes to hold the\n")
	g.pf("// declared defaults without value-initialising the whole aggregate.\n\n")
	for _, st := range members {
		g.pf("inline void TableReset( %s & value ) { %sReset( value ); }\n", st.Name, st.Name)
	}
	g.pf("\n")
}

// emitPointerTargetSurface emits one pointed-at table's resolution and
// allocation entries.
func (g *tableGen) emitPointerTargetSurface(st *ir.Struct) {
	n := st.Name
	g.pf("// %s is a pointer target.\n", n)
	g.pf("inline const %s * %sAt( const TableRef & ref ) // the const form's hot path: one add, no base\n{\n", n, n)
	g.pf("    return ref.value != 0 ? (const %s *) ( (const uint8_t *) &ref + ref.value ) : NULL;\n}\n", n)
	g.pf("inline %s * %sAt( TableRef & ref )\n{\n", n, n)
	g.pf("    return ref.value != 0 ? (%s *) ( (uint8_t *) &ref + ref.value ) : NULL;\n}\n", n)
	g.pf("inline const %s * %sAt( const TableRegionCtx &, const TableRef & ref ) { return %sAt( ref ); }\n", n, n, n)
	g.pf("inline const %s * %sAt( const TableArenaCtx & ctx, const TableRef & ref )\n{\n", n, n)
	g.pf("    return ref.value != 0 ? (const %s *) TableArenaAt( *ctx.arena, (uint32_t) ref.value ) : NULL;\n}\n", n)
	g.pf("// while the builder is mutable, resolve against the arena itself\n")
	g.pf("inline %s * %sAt( TableArena & arena, const TableRef & ref )\n{\n", n, n)
	g.pf("    return ref.value != 0 ? (%s *) TableArenaAt( arena, (uint32_t) ref.value ) : NULL;\n}\n", n)
	g.pf("inline const %s * %sAt( const TableArena & arena, const TableRef & ref )\n{\n", n, n)
	g.pf("    return ref.value != 0 ? (const %s *) TableArenaAt( arena, (uint32_t) ref.value ) : NULL;\n}\n", n)
	g.pf("// allocate one %s in the arena; the slot holds the arena offset\n", n)
	g.pf("inline %s * %sEmplace( TableWorker & worker, TableRef & slot )\n{\n", n, n)
	g.pf("    TableSlot<%s> allocated = worker.Alloc<%s>();\n", n, n)
	g.pf("    slot = allocated.ref;\n")
	g.pf("    return allocated.ptr;\n}\n\n")
}

// ---- the walkers and the public variable-length surface ----

func (g *tableGen) emitVariableSurface(members []*ir.Struct) {
	if !g.anyVariable {
		return
	}
	g.emitMapWalkSurface(members)
	g.emitMapBuilderSurfaces(members)
	for _, st := range g.varMembers(members) {
		g.owner = st
		g.emitNumber(st)
		g.emitPackMeasure(st)
		g.emitPack(st)
	}
	for _, st := range members {
		// a map's generated ENTRY takes no builder and no public surface: it
		// is §2.8's one exception to §7's "a root is any table", reached only
		// through the map that generates it, so it gets no Open, no Cook, no
		// Save and no Load of its own. Its walk — Number, PackMeasure, Pack
		// and the body codecs — is emitted above and is the whole of what it
		// carries.
		if g.isVar(st.Name) && !st.IsMapEntry() {
			g.emitBuilderAndPublicSurface(st)
		}
	}
}

// emitNumber emits the NUMBERING WALK (docs/SPEC-TABLES.md §3.1): the
// first-visit order of a depth-first pre-order walk from the root over POINTER
// EDGES ONLY — fields in declaration order, array elements in index order, and
// descending through every by-value edge there is, in place, to reach the
// pointer fields inside them. A node takes its index the first time it is
// reached and never again.
//
// The numbering is DETERMINISTIC AND RE-DERIVED, NEVER CARRIED: measure derives
// it from the graph and save derives the same one from the same graph, and
// nothing passes between them. That is what makes measure == save hold across a
// pointer graph.
func (g *tableGen) emitNumber(st *ir.Struct) {
	g.pf("// %sNumber: number everything %s POINTS AT, in first-visit order —\n", st.Name, st.Name)
	g.pf("// the fields in declaration order, a by-value edge descended in place.\n")
	g.pf("// A reference to an entry whose descent is still OPEN is a data cycle,\n")
	g.pf("// named here rather than recursed away (docs/SPEC-TABLES.md §3.1).\n")
	g.pf("template <typename Ctx>\ninline bool %sNumber( const Ctx & ctx, TableNumbering & numbering, const %s & value )\n{\n", st.Name, st.Name)
	if g.noVariableEdges(st) {
		g.pf("    (void) ctx; (void) numbering; (void) value; // no pointers below this node\n")
	}
	g.emitEdgeWalk(st, edgeVisitor{
		read: "value",
		pointer: func(f *ir.Field, slot edgeExpr) {
			t := f.Type.Name
			g.pf("    {\n")
			g.pf("        const %s * pointee = %sAt( ctx, %s ); // %s\n", t, t, slot.Src, f.Name)
			g.pf("        if ( pointee != NULL )\n        {\n")
			g.pf("            bool taken = false;\n")
			g.pf("            int64_t slot = 0;\n")
			g.pf("            const TablePackEntry * entry = TablePackMapReach( numbering.seen, (const void *) pointee,\n")
			g.pf("                (int64_t) ( numbering.count + 2 ), taken, slot ); // its index, if this is its first visit\n")
			g.pf("            if ( entry == NULL ) { return false; } // the map could not grow\n")
			g.pf("            if ( !taken )\n            {\n")
			g.pf("                if ( entry->open != 0 ) { return false; } // a data cycle\n")
			g.pf("            }\n            else\n            {\n")
			g.pf("                TableNodeEntry node;\n")
			g.pf("                node.node = (const void *) pointee;\n")
			g.pf("                node.type_id = 0x%016xull; // fnv1a64( \"%s\" )\n", ir.TableWireId(t), t)
			g.pf("                node.type_slot = %s; // its slot in the unit's vocabulary (§3.3)\n", g.slotOf(ir.TableWireId(t)))
			g.pf("                node.measure = &TableNodeMeasureThunk<Ctx, %s>;\n", t)
			g.pf("                node.save = &TableNodeSaveThunk<Ctx, %s>;\n", t)
			g.pf("                node.message_measure = &TableNodeMessageMeasureThunk<Ctx, %s>;\n", t)
			g.pf("                node.message_save = &TableNodeMessageSaveThunk<Ctx, %s>;\n", t)
			g.pf("                if ( !TableNumberingAppend( numbering, node ) ) { return false; }\n")
			g.pf("                if ( !%sNumber( ctx, numbering, *pointee ) ) { return false; }\n", t)
			g.pf("                TablePackMapClose( numbering.seen, (const void *) pointee, slot );\n")
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		},
		blob: func(f *ir.Field, slot edgeExpr) {
			// a BYTE BUFFER is numbered as every node is, where its slot is
			// declared, and it reaches nothing, so its descent closes at once
			// (docs/SPEC-TABLES.md §2.5, §3.1)
			g.pf("    {\n")
			g.pf("        const TableBlob * blob = TableBlobAt( ctx, %s ); // %s: a byte buffer\n", slot.Src, f.Name)
			g.pf("        if ( blob != NULL )\n        {\n")
			g.pf("            bool taken = false;\n")
			g.pf("            int64_t slot = 0;\n")
			g.pf("            const TablePackEntry * entry = TablePackMapReach( numbering.seen, (const void *) blob,\n")
			g.pf("                (int64_t) ( numbering.count + 2 ), taken, slot ); // its index, if this is its first visit\n")
			g.pf("            if ( entry == NULL ) { return false; } // the map could not grow\n")
			g.pf("            if ( taken )\n            {\n")
			g.pf("                TableNodeEntry node;\n")
			g.pf("                node.node = (const void *) blob;\n")
			g.pf("                node.type_id = %s;\n", blobTypeIdConst(f))
			g.pf("                node.type_slot = %s; // its slot in the unit's vocabulary (§3.3)\n", g.slotOf(blobTypeId(f)))
			g.pf("                node.measure = &TableBlobMeasureThunk<Ctx>;\n")
			g.pf("                node.save = &TableBlobSaveThunk<Ctx>;\n")
			g.pf("                node.message_measure = &TableBlobMessageMeasureThunk<Ctx>;\n")
			g.pf("                node.message_save = &TableBlobMessageSaveThunk<Ctx>;\n")
			g.pf("                if ( !TableNumberingAppend( numbering, node ) ) { return false; } // a blob reaches nothing: no descent\n")
			g.pf("                TablePackMapClose( numbering.seen, (const void *) blob, slot );\n")
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		},
		descend: func(table string, expr edgeExpr, indent string) {
			g.pf("%sif ( !%sNumber( ctx, numbering, %s ) ) { return false; }\n", indent, table, expr.Src)
		},
		mapField: g.mapNumberEdge,
	})
	g.pf("    return true;\n}\n\n")
}

// emitPackMeasure emits the exact byte count of a value's DESCENDANT nodes in
// the packed form — Lock's sizing half.
func (g *tableGen) emitPackMeasure(st *ir.Struct) {
	g.pf("// %sPackMeasure: the packed region bytes of everything %s POINTS AT.\n", st.Name, st.Name)
	g.pf("// ONE VISIT PER NODE: `seen` carries the first-visit numbering (§3.1), so a\n")
	g.pf("// node two references name is measured ONCE and packed once, and a\n")
	g.pf("// reference to a node whose descent is still open is a data cycle, refused.\n")
	g.pf("template <typename Ctx>\ninline int64_t %sPackMeasure( const Ctx & ctx, TablePackMap & seen, const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    int64_t bytes = 0;\n")
	if g.noVariableEdges(st) {
		g.pf("    (void) ctx; (void) seen; (void) value; // no pointers below this node\n")
	}
	g.emitEdgeWalk(st, edgeVisitor{
		read: "value",
		pointer: func(f *ir.Field, slot edgeExpr) {
			t := f.Type.Name
			g.pf("    {\n")
			g.pf("        const %s * pointee = %sAt( ctx, %s ); // %s\n", t, t, slot.Src, f.Name)
			g.pf("        if ( pointee != NULL )\n        {\n")
			// a pointer TARGET always has walkers (ir.PointerTargets feeds them), so
			// there is no walker-less arm to write here
			g.pf("            bool taken = false;\n")
			g.pf("            int64_t slot = 0;\n")
			g.pf("            const TablePackEntry * entry = TablePackMapReach( seen, (const void *) pointee, 0, taken, slot );\n")
			g.pf("            if ( entry == NULL ) { return -1; } // the map could not grow\n")
			g.pf("            if ( !taken )\n            {\n")
			g.pf("                if ( entry->open != 0 ) { return -1; } // a data cycle\n")
			g.pf("            }\n            else\n            {\n")
			g.pf("                int64_t inner = %sPackMeasure( ctx, seen, *pointee );\n", t)
			g.pf("                if ( inner < 0 ) { return -1; }\n")
			g.pf("                TablePackMapClose( seen, (const void *) pointee, slot );\n")
			g.emitNodeBytes(t, "*pointee", "                ", "return -1;",
				func(term string) { g.pf("                bytes += %s + inner;\n", term) },
				func(term string) { g.pf("                bytes += %s + inner;\n", term) })
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		},
		blob: func(f *ir.Field, slot edgeExpr) {
			// a byte buffer's node beside the table pointees, where its slot is
			// declared — the pack lays nodes out in first-visit order (§6.2)
			g.pf("    {\n")
			g.pf("        const TableBlob * blob = TableBlobAt( ctx, %s ); // %s: a byte buffer, packed at its used size\n", slot.Src, f.Name)
			g.pf("        if ( blob != NULL )\n        {\n")
			g.pf("            bool taken = false;\n")
			g.pf("            int64_t slot = 0;\n")
			g.pf("            const TablePackEntry * entry = TablePackMapReach( seen, (const void *) blob, 0, taken, slot );\n")
			g.pf("            if ( entry == NULL ) { return -1; } // the map could not grow\n")
			g.pf("            if ( taken )\n            {\n")
			g.pf("                TablePackMapClose( seen, (const void *) blob, slot );\n")
			g.pf("                bytes += TableBlobStorage( (int64_t) blob->length, %v );\n", blobTerminated(f))
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		},
		descend: func(table string, expr edgeExpr, indent string) {
			g.pf("%sint64_t inner = %sPackMeasure( ctx, seen, %s );\n", indent, table, expr.Src)
			g.pf("%sif ( inner < 0 ) { return -1; }\n", indent)
			g.pf("%sbytes += inner;\n", indent)
		},
		mapField: g.mapPackMeasureEdge,
	})
	g.pf("    return bytes;\n}\n\n")
}

// emitPack copies one node into the packed region and lays its pointees out
// depth-first behind it, rewriting every reference into the region's
// self-relative encoding.
func (g *tableGen) emitPack(st *ir.Struct) {
	g.pf("// %sPack: copy src into dst (already placed), then lay every pointee out\n", st.Name)
	g.pf("// depth-first behind it, in FIELD ORDER, by bump allocation.\n")
	g.pf("//\n")
	g.pf("// ONE NODE, ONE BODY (§6.2): `seen` holds every node already placed and\n")
	g.pf("// where it landed, so a node's FIRST reference lays it out and every later\n")
	g.pf("// reference points BACK at that one body. A region delta therefore has no\n")
	g.pf("// required sign (§6.3), and sharing and a back-reference are one fact. A\n")
	g.pf("// reference to a node whose descent is still OPEN is a cycle, and this\n")
	g.pf("// refuses it rather than packing one.\n")
	if g.anyMap {
		// THE NODE'S EXTENT IS CARVED BEFORE ANY POINTEE IS PLACED
		// (docs/SPEC-TABLES.md §2.8, §6.3): a node's extent runs to the next
		// directory entry, so a pointee laid between two of its map arrays
		// would break that. Pack is therefore two halves — the extent, then
		// the pointer descent — and a by-value nesting or a map entry descends
		// into the SECOND, because the first already reached it.
		g.pf("template <typename Ctx>\ninline bool %sPackEdges( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used );\n\n", st.Name, st.Name, st.Name)
		g.pf("template <typename Ctx>\ninline bool %sPack( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used )\n{\n", st.Name, st.Name, st.Name)
		g.pf("    memcpy( (void *) &dst, (const void *) &src, sizeof( %s ) ); // trivially copyable, by construction\n", st.Name)
		g.pf("    int64_t at = 0;\n")
		g.pf("    uint8_t * extent = (uint8_t *) &dst + TableAlignUp64( (int64_t) sizeof( %s ) );\n", st.Name)
		g.pf("    const int64_t room = capacity - ( (int64_t) ( extent - base ) );\n")
		g.pf("    if ( !%sMapPack( ctx, src, dst, extent, at, room ) ) { return false; }\n", st.Name)
		g.pf("    return %sPackEdges( ctx, seen, src, dst, base, capacity, used );\n}\n\n", st.Name)
		g.pf("template <typename Ctx>\ninline bool %sPackEdges( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used )\n{\n", st.Name, st.Name, st.Name)
		if g.noVariableEdges(st) {
			g.pf("    (void) ctx; (void) seen; (void) src; (void) dst; (void) base; (void) capacity; (void) used;\n")
		}
	} else {
		g.pf("template <typename Ctx>\ninline bool %sPack( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used )\n{\n", st.Name, st.Name, st.Name)
		g.pf("    memcpy( (void *) &dst, (const void *) &src, sizeof( %s ) ); // trivially copyable, by construction\n", st.Name)
		if g.noVariableEdges(st) {
			g.pf("    (void) ctx; (void) seen; (void) base; (void) capacity; (void) used;\n")
		}
	}
	g.emitEdgeWalk(st, edgeVisitor{
		read:  "src",
		write: "dst",
		pointer: func(f *ir.Field, slot edgeExpr) {
			t := f.Type.Name
			dstSlot := slot.Dst
			g.pf("    {\n")
			g.pf("        %s.value = 0; // %s\n", dstSlot, f.Name)
			g.pf("        const %s * pointee = %sAt( ctx, %s );\n", t, t, slot.Src)
			g.pf("        if ( pointee != NULL )\n        {\n")
			g.pf("            int64_t at = TableAlignUp64( used ); // where it WOULD land, if this is its first visit\n")
			g.pf("            bool taken = false;\n")
			g.pf("            int64_t slot = 0;\n")
			g.pf("            const TablePackEntry * entry = TablePackMapReach( seen, (const void *) pointee, at, taken, slot );\n")
			g.pf("            if ( entry == NULL ) { return false; } // the map could not grow\n")
			g.pf("            if ( !taken )\n            {\n")
			g.pf("                if ( entry->open != 0 ) { return false; } // a data cycle\n")
			g.pf("                %s.value = (int64_t) ( ( base + entry->offset ) - (const uint8_t *) &%s ); // the one body it already has\n", dstSlot, dstSlot)
			g.pf("            }\n            else\n            {\n")
			g.emitNodeBytes(t, "*pointee", "                ", "return false;",
				func(term string) {
					g.pf("                if ( at + (int64_t) sizeof( %s ) > capacity ) { return false; }\n", t)
					g.pf("                used = at + %s;\n", term)
				},
				func(term string) {
					g.pf("                const int64_t node_bytes = %s;\n", term)
					g.pf("                if ( at + node_bytes > capacity ) { return false; }\n")
					g.pf("                used = at + node_bytes;\n")
				})
			g.pf("                %s * child = new ( base + at ) %s; // lifetime only: the Pack below memcpy's the whole node over it\n", t, t)
			g.pf("                %s.value = (int64_t) ( ( base + at ) - (const uint8_t *) &%s );\n", dstSlot, dstSlot)
			g.pf("                if ( !%sPack( ctx, seen, *pointee, *child, base, capacity, used ) ) { return false; }\n", t)
			g.pf("                TablePackMapClose( seen, (const void *) pointee, slot );\n")
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		},
		blob: g.emitPackBlobField,
		descend: func(table string, expr edgeExpr, indent string) {
			call := "Pack"
			if g.anyMap {
				call = "PackEdges" // the extent walk already reached this nesting
			}
			g.pf("%sif ( !%s%s( ctx, seen, %s, %s, base, capacity, used ) ) { return false; }\n", indent, table, call, expr.Src, expr.Dst)
		},
		mapField: g.mapPackEdge,
	})
	g.pf("    return true;\n}\n\n")
}

// emitPackBlobField lays a BYTE BUFFER's node out beside the table pointees,
// where its slot is declared: the node is copied at its used size — the header
// and the bytes; the region's zeros are its tail and a string's terminator —
// and a blob two slots name is laid out once (docs/SPEC-TABLES.md §2.5).
func (g *tableGen) emitPackBlobField(f *ir.Field, slot edgeExpr) {
	g.pf("    {\n")
	g.pf("        %s.value = 0; // %s: a byte buffer\n", slot.Dst, f.Name)
	g.pf("        const TableBlob * blob = TableBlobAt( ctx, %s );\n", slot.Src)
	g.pf("        if ( blob != NULL )\n        {\n")
	g.pf("            int64_t at = TableAlignUp64( used ); // where it WOULD land, if this is its first visit\n")
	g.pf("            bool taken = false;\n")
	g.pf("            int64_t slot = 0;\n")
	g.pf("            const TablePackEntry * entry = TablePackMapReach( seen, (const void *) blob, at, taken, slot );\n")
	g.pf("            if ( entry == NULL ) { return false; } // the map could not grow\n")
	g.pf("            if ( !taken )\n            {\n")
	g.pf("                %s.value = (int64_t) ( ( base + entry->offset ) - (const uint8_t *) &%s ); // the one body it already has\n", slot.Dst, slot.Dst)
	g.pf("            }\n            else\n            {\n")
	g.pf("                int64_t storage = TableBlobStorage( (int64_t) blob->length, %v );\n", blobTerminated(f))
	g.pf("                if ( at + storage > capacity ) { return false; }\n")
	g.pf("                memcpy( base + at, (const void *) blob, (size_t) ( kTableBlobHeader + (int64_t) blob->length ) );\n")
	g.pf("                used = at + storage;\n")
	g.pf("                %s.value = (int64_t) ( ( base + at ) - (const uint8_t *) &%s );\n", slot.Dst, slot.Dst)
	g.pf("                TablePackMapClose( seen, (const void *) blob, slot );\n")
	g.pf("            }\n")
	g.pf("        }\n    }\n")
}

// ---- the builder and the public surface ----

func (g *tableGen) emitBuilderAndPublicSurface(st *ir.Struct) {
	n := st.Name
	g.pf("// ---- %s: the variable-length life (docs/SPEC-TABLES.md §2, §6, §9) ----\n", n)
	g.pf("//\n")
	g.pf("// MUTABLE: %sBuilder — allocate nodes, wire them together, then Lock.\n", n)
	g.pf("// CONST:   one packed region, root at its base. Lock produces it and Load\n")
	g.pf("//          produces it, so a locked structure and a loaded one are the\n")
	g.pf("//          SAME representation with one view API. There is no unlock:\n")
	g.pf("//          re-editing means loading the const form into a fresh builder.\n")
	g.pf("// %s is never held by value — a file-format-scale structure is a region\n", n)
	g.pf("// and a root pointer, not a struct you copy.\n\n")

	g.pf("struct %sBuilder\n{\n", n)
	g.pf("    TableArena arena;\n")
	g.pf("    TableWorker main;      // the calling thread's allocation front\n")
	g.pf("    TableRef root_ref;\n")
	g.pf("    uint8_t * region = NULL; // the packed const form, produced by Lock()\n")
	g.pf("    int64_t region_bytes = 0;\n\n")
	g.pf("    // THE ALLOCATOR IS THE BUILDER'S, and everything this structure ever\n")
	g.pf("    // allocates goes through it: the arena's segments, Lock's identity map,\n")
	g.pf("    // the packed region, the wire walks' numbering, and the tool path's node\n")
	g.pf("    // directory. Name your own and a profiler sees every byte under it.\n")
	g.pf("    %sBuilder( TableAllocator allocator = TableDefaultAllocator() )\n    {\n", n)
	g.pf("        TableArenaInit( arena, allocator );\n")
	g.pf("        main.arena = &arena;\n")
	g.pf("        TableSlot<%s> slot = main.Alloc<%s>();\n", n, n)
	g.pf("        root_ref = slot.ref;\n")
	g.pf("    }\n")
	g.pf("    ~%sBuilder() { TableArenaShutdown( arena ); arena.allocator.free( arena.allocator.context, region ); }\n", n)
	g.pf("    %sBuilder( const %sBuilder & ) = delete;\n", n, n)
	g.pf("    %sBuilder & operator=( const %sBuilder & ) = delete;\n\n", n, n)
	g.pf("    // Alloc a node in THIS thread's slab: no lock, no atomic per node.\n")
	g.pf("    // The result is usable both as the node pointer and as the reference\n")
	g.pf("    // to store in a pointer field.\n")
	g.pf("    template <typename T> TableSlot<T> Alloc() { return main.Alloc<T>(); }\n")
	g.pf("    // a BYTE BUFFER's node of exactly `length` bytes (docs/SPEC-TABLES.md §2.5):\n")
	g.pf("    // the bytes to write through, and the reference to store in a *bytes\n")
	g.pf("    // or *string slot; a blob past a slab takes a span of its own\n")
	g.pf("    TableBytesSlot AllocBytes( int64_t length ) { return main.AllocBytes( length ); }\n")
	g.pf("    TableStringSlot AllocString( int64_t length ) { return main.AllocString( length ); }\n")
	g.pf("    // one worker per thread; allocate on your own, and synchronize your own\n")
	g.pf("    // writes to nodes another worker allocated\n")
	g.pf("    TableWorker Worker() { TableWorker worker; worker.arena = &arena; return worker; }\n\n")
	g.pf("    // GetRoot/AsConst, not Root/Const: a member function hides the type\n")
	g.pf("    // name it shares, and `table Root` is this spec's own canonical\n")
	g.pf("    // example. The checker refuses a table named after any member here,\n")
	g.pf("    // so the remaining spellings cannot collide either.\n")
	g.pf("    %s * GetRoot() { return arena.locked ? NULL : (%s *) TableArenaAt( arena, (uint32_t) root_ref.value ); }\n", n, n)
	g.pf("    bool Locked() const { return arena.locked; }\n")
	g.pf("    const %s * AsConst() const { return (const %s *) region; }\n", n, n)
	g.pf("    const uint8_t * Region() const { return region; }\n")
	g.pf("    int64_t RegionBytes() const { return region_bytes; }\n\n")
	g.pf("    // Lock is ONE WAY and it is the compaction: the segmented arena becomes\n")
	g.pf("    // one exact-packed region with zero slack, references rewritten\n")
	g.pf("    // self-relative, and the mutable life released. Single-threaded: call\n")
	g.pf("    // it after the workers have joined.\n")
	g.pf("    bool Lock();\n")
	g.pf("};\n\n")

	g.pf("inline bool %sBuilder::Lock()\n{\n", n)
	g.pf("    if ( arena.locked ) { return region != NULL; }\n")
	g.pf("    if ( root_ref.null() ) { return false; }\n")
	g.pf("    TableArenaCtx ctx = { &arena };\n")
	g.pf("    const %s & root = *(const %s *) TableArenaAt( arena, (uint32_t) root_ref.value );\n", n, n)
	g.pf("    // The ROOT takes the map's first entry: it is packed at offset 0, and its\n")
	g.pf("    // descent is open for the whole walk (docs/SPEC-TABLES.md §3.1).\n")
	g.pf("    TablePackMap seen;\n")
	g.pf("    TablePackMapInit( seen, arena.allocator );\n")
	g.pf("    bool root_taken = false;\n")
	g.pf("    int64_t root_slot = 0;\n")
	g.pf("    int64_t below = -1;\n")
	g.pf("    if ( TablePackMapReach( seen, (const void *) &root, 0, root_taken, root_slot ) != NULL )\n    {\n")
	g.pf("        below = %sPackMeasure( ctx, seen, root );\n", n)
	g.pf("    }\n")
	g.pf("    if ( below < 0 ) { TablePackMapShutdown( seen ); return false; } // a data cycle, named at the reference that closes it\n")
	if g.anyMap {
		g.pf("    int64_t root_extent = %sMapExtent( ctx, root );\n", n)
		g.pf("    if ( root_extent < 0 ) { TablePackMapShutdown( seen ); return false; } // the sort could not run\n")
		g.pf("    int64_t total = TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent ) + below;\n", n)
	} else {
		g.pf("    int64_t total = TableAlignUp64( (int64_t) sizeof( %s ) ) + below;\n", n)
	}
	g.pf("    // the AUTHORING path may allocate (§6.5), and it does so through the\n")
	g.pf("    // builder's own pair. The region comes back ZEROED, which is the\n")
	g.pf("    // allocator's contract: a packed region carries node padding.\n")
	g.pf("    uint8_t * packed = (uint8_t *) arena.allocator.alloc( arena.allocator.context, total );\n")
	g.pf("    if ( packed == NULL ) { TablePackMapShutdown( seen ); return false; }\n")
	if g.anyMap {
		g.pf("    int64_t used = TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent );\n", n)
	} else {
		g.pf("    int64_t used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	}
	g.pf("    %s * destination = new ( packed ) %s; // lifetime only: the Pack below memcpy's the whole node over it\n", n, n)
	g.pf("    // The pack walk RE-DERIVES the same numbering rather than carrying the\n")
	g.pf("    // measure's — nothing passes between them, which is what makes\n")
	g.pf("    // `used == total` below a real check and not a tautology (§3.1). The\n")
	g.pf("    // map keeps the capacity the measure paid for, so the second walk\n")
	g.pf("    // rehashes nothing.\n")
	g.pf("    TablePackMapReset( seen );\n")
	g.pf("    if ( TablePackMapReach( seen, (const void *) &root, 0, root_taken, root_slot ) == NULL ||\n")
	g.pf("         !%sPack( ctx, seen, root, *destination, packed, total, used ) || used != total )\n    {\n", n)
	g.pf("        TablePackMapShutdown( seen );\n")
	g.pf("        arena.allocator.free( arena.allocator.context, packed );\n        return false;\n    }\n")
	g.pf("    TablePackMapShutdown( seen );\n")
	g.pf("    region = packed;\n")
	g.pf("    region_bytes = total;\n")
	g.pf("    arena.locked = true; // MONOTONIC: there is no unlock\n")
	g.pf("    TableArenaShutdown( arena );\n")
	g.pf("    return true;\n}\n\n")

	// wire out
	g.pf("// ---- %s on the wire: the FLAT NODE TABLE (docs/SPEC-TABLES.md §3.1) ----\n", n)
	g.pf("//\n")
	g.pf("// A pointered save writes every reachable node ONCE, into a node table under\n")
	g.pf("// the reserved id 0xFFFF, and a pointer field rides as a u32 INDEX into it\n")
	g.pf("// under kind 17. No pointer edge is a nesting level, so a chain's length is\n")
	g.pf("// not a depth and two references to one node are one node.\n\n")

	g.emitRootNodeDispatch(st)
	g.emitRootNodeMessageDispatch(st)

	g.pf("// The numbering both wire walks derive, and NEITHER CARRIES THE OTHER'S: the\n")
	g.pf("// root takes index 1 and its entry stays open for the whole walk, so a\n")
	g.pf("// reference back at it is the cycle it is (§3.1).\n")
	g.pf("template <typename Ctx>\ninline bool %sNumberFrom( const Ctx & ctx, TableNumbering & numbering, const %s & root )\n{\n", n, n)
	g.pf("    bool taken = false;\n")
	g.pf("    int64_t slot = 0;\n")
	g.pf("    if ( TablePackMapReach( numbering.seen, (const void *) &root, (int64_t) kTableNodeIndexRoot, taken, slot ) == NULL ) { return false; }\n")
	g.pf("    return %sNumber( ctx, numbering, root );\n}\n\n", n)

	g.pf("template <typename Ctx>\ninline int64_t %sMeasureWire( const Ctx & ctx, const %s & root, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    int64_t bytes = -1;\n")
	g.pf("    if ( %sNumberFrom( ctx, numbering, root ) )\n    {\n", n)
	g.pf("        TableIds ids;\n")
	g.pf("        bytes = %sMeasureBody( ctx, numbering, ids, root );\n", n)
	g.pf("        if ( bytes >= 0 )\n        {\n")
	g.pf("            const int64_t table = TableNodeTableMeasure( ctx, ids, numbering );\n")
	g.pf("            // the FORM BYTE, the ROOT BODY — its own fields, the node table\n")
	g.pf("            // and the terminator — and the ID TABLE (docs/SPEC-TABLES.md §3)\n")
	g.pf("            bytes = table < 0 || ids.overflow ? -1 : 1 + bytes + table + TableIdsBytes( ids );\n")
	g.pf("        }\n    }\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return bytes;\n}\n\n")

	g.pf("template <typename Ctx>\ninline int64_t %sSaveWire( const Ctx & ctx, const %s & root, uint8_t * buffer, int64_t capacity, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    if ( !%sNumberFrom( ctx, numbering, root ) ) { TableNumberingShutdown( numbering ); return -1; }\n", n)
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    TableIds ids;\n")
	g.pf("    w.put8( kTableWireForm ); // the FORM BYTE is the whole header (§3)\n")
	g.pf("    // the root's own fields, then the node table's field, then the\n")
	g.pf("    // terminator: a reader that gives up inside the table has already\n")
	g.pf("    // decoded the ROOT'S OWN FIELDS (§3.1)\n")
	g.pf("    bool ok = %sSaveBodyFields( ctx, numbering, w, ids, root ) && TableNodeTableSave( ctx, w, ids, numbering );\n", n)
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    if ( !ok || ids.overflow ) { return -1; }\n")
	g.pf("    w.put8( 0 ); // the ZERO REFERENCE that ends the root body\n")
	g.pf("    TableIdsWrite( w, ids );\n")
	g.pf("    if ( w.overflow ) { return -1; } // the caller's buffer was too small\n")
	g.pf("    return w.offset; // == %sMeasure( root )\n}\n\n", n)

	// The REGION overloads take the allocator the walk's numbering runs on —
	// measuring and saving a region is the one reading-side path that allocates,
	// because the numbering is proportional to nodes. It defaults to the C
	// library pair, so a call site that has no opinion writes none.
	g.pf("inline int64_t %sMeasure( const %s * root, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( root == NULL ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return %sMeasureWire( ctx, *root, allocator );\n}\n\n", n)
	g.pf("inline int64_t %sSave( const %s * root, uint8_t * buffer, int64_t capacity, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( root == NULL ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return %sSaveWire( ctx, *root, buffer, capacity, allocator );\n}\n\n", n)
	// the BUILDER overloads name no allocator: the builder already carries one
	g.pf("inline int64_t %sMeasure( const %sBuilder & builder )\n{\n", n, n)
	g.pf("    if ( builder.region != NULL ) { return %sMeasure( builder.AsConst(), builder.arena.allocator ); }\n", n)
	g.pf("    if ( builder.root_ref.null() ) { return -1; } // the root allocation failed\n")
	g.pf("    TableArenaCtx ctx = { &builder.arena };\n")
	g.pf("    return %sMeasureWire( ctx, *(const %s *) TableArenaAt( builder.arena, (uint32_t) builder.root_ref.value ), builder.arena.allocator );\n}\n\n", n, n)
	g.pf("inline int64_t %sSave( const %sBuilder & builder, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    if ( builder.region != NULL ) { return %sSave( builder.AsConst(), buffer, capacity, builder.arena.allocator ); }\n", n)
	g.pf("    if ( builder.root_ref.null() ) { return -1; } // the root allocation failed\n")
	g.pf("    TableArenaCtx ctx = { &builder.arena };\n")
	g.pf("    return %sSaveWire( ctx, *(const %s *) TableArenaAt( builder.arena, (uint32_t) builder.root_ref.value ), buffer, capacity, builder.arena.allocator );\n}\n\n", n, n)

	// THE MESSAGE FORM over a POINTERED root (docs/SPEC-TABLES.md §3.3): the
	// batch's three verbs over a region, beside the file form's.
	g.emitVariableMessageSurface(st)

	// load
	g.emitVariableLoadMeasure(st)
	g.emitVariableLoad(st)

	g.pf("// %sLoadBuilder: the TOOL's path — the same tolerant decode into a fresh\n", n)
	g.pf("// builder, so loaded data can be edited and locked again. The numbering is\n")
	g.pf("// the same one; what differs is where a node lives and therefore what a\n")
	g.pf("// resolved slot holds — an arena offset here, a self-relative delta there.\n")
	g.pf("inline bool %sLoadBuilder( %sBuilder & builder, const uint8_t * wire_file, int64_t wire_file_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    TableIdTable ids_table;\n")
	g.pf("    int64_t body_bytes = 0;\n")
	g.pf("    const TableOpenVerdict verdict = TableOpen( wire_file, wire_file_bytes, ids_table, body_bytes );\n")
	g.pf("    if ( verdict != TableOpenOk )\n    {\n")
	g.pf("        if ( verdict == TableOpenDamaged ) { out->malformed = true; } else { out->refused = true; }\n")
	g.pf("        return false;\n    }\n")
	g.pf("    if ( TableBodyEndsEarly( wire_file + 1, body_bytes, ids_table ) )\n    {\n")
	g.pf("        out->malformed = true; // a byte no field claims, before the table (§3)\n")
	g.pf("        return false;\n    }\n")
	g.pf("    const uint8_t * const wire = wire_file + 1;\n")
	g.pf("    const int64_t wire_bytes = body_bytes;\n")
	g.pf("    %s * root = builder.GetRoot();\n", n)
	g.pf("    if ( root == NULL ) { out->malformed = true; return false; }\n")
	g.pf("    uint64_t type_id = 0;\n")
	g.pf("    const uint8_t * body = NULL;\n")
	g.pf("    int64_t length = 0;\n")
	g.pf("    int64_t records = 0;\n")
	g.pf("    {\n")
	g.pf("        TableReport counting;\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &counting, &ids_table );\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) ) { records++; }\n")
	g.pf("    }\n")
	g.pf("    // the AUTHORING side may allocate (§6.5), and this is the tool's path.\n")
	g.pf("    // It goes through the builder's own pair, like everything else the\n")
	g.pf("    // builder reaches, and the entries come back zeroed.\n")
	g.pf("    const TableAllocator allocator = builder.arena.allocator;\n")
	g.pf("    TableNodeDirEntry * directory = (TableNodeDirEntry *) allocator.alloc( allocator.context, ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry ) );\n")
	g.pf("    if ( directory == NULL ) { out->malformed = true; return false; }\n")
	g.pf("    directory[0].offset = (uint64_t) builder.root_ref.value;\n")
	g.pf("    directory[0].type_id = 0x%016xull;\n", ir.TableWireId(st.Name))
	g.pf("    TableNodeMap nodes;\n")
	g.pf("    nodes.base = NULL;\n")
	g.pf("    nodes.entries = directory;\n")
	g.pf("    nodes.count = records + 1;\n")
	g.pf("    nodes.arena = true; // a resolved slot holds the node's ARENA OFFSET here\n")
	if g.anyMap {
		g.pf("    nodes.worker = &builder.main; // and a map's entries are the arena's, not a node extent's (§2.8)\n")
	}
	g.pf("    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out, &ids_table );\n")
	g.pf("        int64_t k = 0;\n")
	g.pf("        int32_t unknown_records = 0; // counted once the scan is known whole\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            uint32_t at = %sNodeAlloc( type_id, builder.main, length );\n", n)
	g.pf("            if ( at == 0 )\n            {\n")
	g.pf("                unknown_records++;\n")
	g.pf("                directory[k + 1].offset = kTableNodeAbsent;\n")
	g.pf("            }\n            else\n            {\n")
	g.pf("                directory[k + 1].offset = (uint64_t) at;\n")
	g.pf("            }\n")
	g.pf("            directory[k + 1].type_id = type_id;\n")
	g.pf("            k++;\n")
	g.pf("        }\n")
	g.pf("        nodes.good = TableNodeScanWhole( scan );\n")
	g.pf("        if ( nodes.good ) { out->unknown += unknown_records; } else { out->malformed = true; }\n")
	g.pf("    }\n")
	g.pf("    if ( nodes.good )\n    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out, &ids_table );\n")
	g.pf("        int64_t k = 0;\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            if ( directory[k + 1].offset != kTableNodeAbsent )\n            {\n")
	g.pf("                TableReader sub( body, length, out, &ids_table );\n")
	g.pf("                %sNodeBody( type_id, sub, nodes, TableArenaAt( builder.arena, (uint32_t) directory[k + 1].offset ) );\n", n)
	g.pf("            }\n")
	g.pf("            k++;\n")
	g.pf("        }\n    }\n")
	g.pf("    TableReader r( wire, wire_bytes, out, &ids_table );\n")
	g.pf("    r.nested = false; // the ROOT body, the one that carries the node table\n")
	if g.anyMap {
		g.pf("    TableMapCarve root_carve;\n")
		g.pf("    root_carve.worker = &builder.main;\n")
		g.pf("    nodes.carve = &root_carve;\n")
	}
	g.pf("    bool ok = %sLoadBody( r, nodes, *root );\n", n)
	g.pf("    allocator.free( allocator.context, directory );\n")
	g.pf("    return ok;\n}\n\n")
}

// emitRootNodeDispatch emits the three answers a LOAD needs about a wire type
// id, over the members this root's numbering can name (docs/SPEC-TABLES.md §3.1).
//
// It is emitted PER ROOT and over the pointer-REACHABLE set, which is exactly
// the set this root's own walkers already name — so every type it spells is one
// whose header this file already includes, and a unit whose files point at each
// other needs no registry and no cross-file cycle (§11).
func (g *tableGen) emitRootNodeDispatch(st *ir.Struct) {
	n := st.Name
	reachable := g.pointerReachable(st)
	blobs := g.reachableBlobs(st)

	g.pf("// %sNodeStorage: the region bytes one record commands, or -1 for a type id\n", n)
	g.pf("// this build cannot name — which keeps its index and reads null. A BYTE\n")
	g.pf("// BUFFER's record commands its header and its bytes (docs/SPEC-TABLES.md §2.5),\n")
	g.pf("// which is the one answer the record's LENGTH decides.\n")
	if g.anyMap {
		g.pf("// A MAP'S ENTRIES RIDE IN THEIR HOLDER'S EXTENT (docs/SPEC-TABLES.md §2.8),\n")
		g.pf("// so a record's storage is its type's PLUS N x sizeof( Entry ) at every\n")
		g.pf("// depth, summed from the FRAMING: N is framing and not a value, and this\n")
		g.pf("// reads no field. kTableNodeRefused is a wire whose N its L cannot carry.\n")
	}
	anyExtent := false
	for _, t := range reachable {
		if g.anyMap && g.hasMapExtent(t) {
			anyExtent = true
		}
	}
	// THE BODY IS ONLY A PARAMETER WHERE A MAP RIDES IN AN EXTENT: a root that
	// can name no such record answers from the type id and the length, exactly
	// as it did before the construct existed.
	g.pf("inline int64_t %sNodeStorage( uint64_t type_id, %sint64_t length )\n{\n", n, g.nodeStorageBody(anyExtent))
	if len(blobs) == 0 {
		g.pf("    (void) length; // no byte buffer below this root: every node's storage is its type's\n")
	}
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		if g.anyMap && g.hasMapExtent(t) {
			g.pf("        case 0x%016xull: // %s\n        {\n", ir.TableWireId(t.Name), t.Name)
			g.pf("            int64_t extent = 0;\n")
			g.pf("            if ( !%sWireExtent( body, length, extent ) ) { return kTableNodeRefused; }\n", t.Name)
			g.pf("            return TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + extent );\n        }\n", t.Name)
			continue
		}
		g.pf("        case 0x%016xull: return TableAlignUp64( (int64_t) sizeof( %s ) ); // %s\n", ir.TableWireId(t.Name), t.Name, t.Name)
	}
	for _, b := range blobs {
		g.pf("        case %s: return TableBlobStorage( length, %v ); // *%s\n", b.constant, b.terminated, b.word)
	}
	g.pf("        default: break;\n    }\n")
	g.pf("    return -1;\n}\n\n")

	g.pf("// %sNodePlace: start one record's node's lifetime in the storage pass one\n", n)
	g.pf("// reserved for it, holding exactly the declared defaults — a byte buffer's\n")
	g.pf("// header holds its length, and its bytes come in pass two.\n")
	g.pf("inline void %sNodePlace( uint64_t type_id, uint8_t * at, int64_t length )\n{\n", n)
	if len(blobs) == 0 {
		g.pf("    (void) length;\n")
	}
	if len(reachable) == 0 && len(blobs) == 0 {
		// a VARIABLE root whose maps reach no node: its numbering is always
		// empty, so nothing is ever placed (docs/SPEC-TABLES.md §2.8)
		g.pf("    (void) at;\n")
	}
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: { %s * node = new ( at ) %s; %sReset( *node ); break; } // %s\n",
			ir.TableWireId(t.Name), t.Name, t.Name, t.Name, t.Name)
	}
	for _, b := range blobs {
		g.pf("        case %s: { TableBlob * blob = (TableBlob *) at; blob->length = (uint32_t) length; blob->zero = 0; break; } // *%s\n", b.constant, b.word)
	}
	g.pf("        default: break;\n    }\n}\n\n")

	if g.anyMap {
		g.pf("// %sNodeRecordBytes: one record's OWN storage, before the extent its maps\n", n)
		g.pf("// take (docs/SPEC-TABLES.md §2.8) — where a node's extent begins.\n")
		g.pf("inline int64_t %sNodeRecordBytes( uint64_t type_id )\n{\n", n)
		g.pf("    switch ( type_id )\n    {\n")
		for _, t := range reachable {
			g.pf("        case 0x%016xull: return TableAlignUp64( (int64_t) sizeof( %s ) ); // %s\n", ir.TableWireId(t.Name), t.Name, t.Name)
		}
		g.pf("        default: break;\n    }\n")
		g.pf("    return 0;\n}\n\n")
	}

	g.pf("// %sNodeAlloc: the TOOL's path — one record's node in the builder's arena.\n", n)
	g.pf("// Zero is the arena's null, and it is also what a type id this build cannot\n")
	g.pf("// name answers.\n")
	g.pf("inline uint32_t %sNodeAlloc( uint64_t type_id, TableWorker & worker, int64_t length )\n{\n", n)
	if len(blobs) == 0 {
		g.pf("    (void) length;\n")
	}
	if len(reachable) == 0 && len(blobs) == 0 {
		g.pf("    (void) worker;\n")
	}
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: return (uint32_t) worker.Alloc<%s>().ref.value; // %s\n", ir.TableWireId(t.Name), t.Name, t.Name)
	}
	for _, b := range blobs {
		if b.terminated {
			g.pf("        case %s: return (uint32_t) worker.AllocString( length ).ref.value; // *string\n", b.constant)
		} else {
			g.pf("        case %s: return (uint32_t) worker.AllocBytes( length ).ref.value; // *bytes\n", b.constant)
		}
	}
	g.pf("        default: break;\n    }\n")
	g.pf("    return 0;\n}\n\n")

	g.pf("// %sNodeBody: PASS TWO's half — decode one record's body into the storage it\n", n)
	g.pf("// already owns.\n")
	g.pf("inline void %sNodeBody( uint64_t type_id, TableReader & r, const TableNodeMap & nodes, uint8_t * at )\n{\n", n)
	if g.anyMap {
		g.pf("    // the node's own EXTENT, where its maps' entry arrays are carved from,\n")
		g.pf("    // PRE-ORDER as the bodies decode (docs/SPEC-TABLES.md §2.8). The tool's\n")
		g.pf("    // path carries a worker instead: there the entries are the arena's.\n")
		g.pf("    TableMapCarve carve;\n")
		g.pf("    carve.worker = nodes.worker;\n")
		g.pf("    if ( carve.worker == NULL )\n    {\n")
		g.pf("        const int64_t storage = %sNodeStorage( type_id, %sr.size );\n", n, g.nodeStorageReader(st))
		g.pf("        const int64_t record = storage > 0 ? %sNodeRecordBytes( type_id ) : 0;\n", n)
		g.pf("        carve.at = at + record;\n")
		g.pf("        carve.left = storage > record ? storage - record : 0;\n")
		g.pf("    }\n")
		g.pf("    nodes.carve = &carve;\n")
	}
	anyVar := false
	for _, t := range reachable {
		if g.isVar(t.Name) {
			anyVar = true
		}
	}
	if !anyVar {
		g.pf("    (void) nodes; // every node this root can name is a FIXED table\n")
	}
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		call := fmt.Sprintf("%sLoadBody( r, nodes, *(%s *) at )", t.Name, t.Name)
		if !g.isVar(t.Name) {
			call = fmt.Sprintf("%sLoadBody( r, *(%s *) at )", t.Name, t.Name)
		}
		g.pf("        case 0x%016xull: %s; break; // %s\n", ir.TableWireId(t.Name), call, t.Name)
	}
	for _, b := range blobs {
		// the bytes verbatim; the storage's zeros are a string's terminator
		g.pf("        case %s: if ( r.size > 0 ) { memcpy( at + kTableBlobHeader, r.buffer, (size_t) r.size ); } break; // *%s\n", b.constant, b.word)
	}
	g.pf("        default: break;\n    }\n")
	if g.anyMap {
		g.pf("    nodes.carve = NULL; // the cursor is ONE node's, and this node's body is done\n")
	}
	g.pf("}\n\n")
}

// reachableBlob is one BYTE BUFFER shape a root's numbering can name
// (docs/SPEC-TABLES.md §2.5): the record's reserved type id, spelled as the
// runtime's constant, and whether its node carries a string's terminator.
type reachableBlob struct {
	constant   string
	word       string
	terminated bool
}

// reachableBlobs lists the byte buffer shapes a root's numbering can name —
// `*bytes`, `*string`, or both — over the same closure pointerReachable
// walks: a blob inside a pointed-at table is this root's to load.
func (g *tableGen) reachableBlobs(root *ir.Struct) []reachableBlob {
	bytes, str := false, false
	visited := map[string]bool{}
	var descend func(st *ir.Struct)
	descend = func(st *ir.Struct) {
		if visited[st.Name] {
			return
		}
		visited[st.Name] = true
		for _, f := range st.Fields {
			if f.IsMap() {
				// a map's ENTRY is a by-value edge (docs/SPEC-TABLES.md §2.8):
				// a blob inside an entry's value is this root's to load
				descend(f.MapEntry)
				continue
			}
			if f.Type.Blob() {
				if f.Type.Kind == ir.TString {
					str = true
				} else {
					bytes = true
				}
				continue
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if un, isUnion := f.Type.Ref.(*ir.Union); isUnion {
				for _, v := range un.Variants {
					if v.Ref != nil {
						descend(v.Ref)
					}
				}
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				descend(ref)
			}
		}
	}
	descend(root)
	var out []reachableBlob
	if bytes {
		out = append(out, reachableBlob{constant: "kTableBytesTypeId", word: "bytes"})
	}
	if str {
		out = append(out, reachableBlob{constant: "kTableStringTypeId", word: "string", terminated: true})
	}
	return out
}

// pointerReachable returns the closure members this root's numbering can name:
// every table reached through a POINTER EDGE, directly or by descending through
// by-value nesting to reach the pointer fields inside it, in first-visit order.
func (g *tableGen) pointerReachable(root *ir.Struct) []*ir.Struct {
	named := map[string]bool{}
	visited := map[string]bool{}
	var out []*ir.Struct
	var descend func(st *ir.Struct)
	descend = func(st *ir.Struct) {
		if visited[st.Name] {
			return
		}
		visited[st.Name] = true
		for _, f := range st.Fields {
			if f.IsMap() {
				// a map's ENTRY is a by-value edge (docs/SPEC-TABLES.md §2.8),
				// so a *T VALUE names a node this root's numbering must resolve
				descend(f.MapEntry)
				continue
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if un, isUnion := f.Type.Ref.(*ir.Union); isUnion {
				// a union's arms are by-value edges (docs/SPEC-TABLES.md §2.6):
				// the pointers inside a table arm are this root's to name
				for _, v := range un.Variants {
					if v.Ref != nil {
						descend(v.Ref)
					}
				}
				continue
			}
			ref, ok := f.Type.Ref.(*ir.Struct)
			if !ok {
				continue
			}
			if f.Type.Pointer && !named[ref.Name] {
				named[ref.Name] = true
				out = append(out, ref)
			}
			descend(ref)
		}
	}
	descend(root)
	return out
}

// emitRelocatabilityPreamble writes the comment above the static asserts,
// which covers both forms.
func (g *tableGen) emitRelocatabilityPreamble() {
	g.pf("// ---- relocatability, enforced: the wire is a pure length-prefixed\n")
	g.pf("// stream AND the decoded storage is pointer-free — every closure type\n")
	g.pf("// must stay trivially copyable and standard-layout, so instances can be\n")
	g.pf("// memcpy'd, mmap'd, shared across processes, and walked through\n")
	g.pf("// descriptor offsets. A failure here means a pointer, virtual or\n")
	g.pf("// non-trivial member crept into generated storage.\n")
	g.pf("//\n")
	g.pf("// They ask the COMPILER ITSELF, which is what every C++ standard library\n")
	g.pf("// answers the same two questions with — and it costs this header no\n")
	g.pf("// include at all.\n")
	if g.anyVariable {
		g.pf("// A pointer FIELD is a TableRef — eight bytes and no address — so the\n")
		g.pf("// property holds in BOTH forms: a fixed-size table is one relocatable\n")
		g.pf("// struct, and a packed region is one relocatable block whose references\n")
		g.pf("// are self-relative and therefore survive a plain memcpy.\n")
	}
}

// modeColumn renders the descriptor's derived-mode column, present only in a
// unit that has pointers (the zero-cost gate).
func (g *tableGen) modeColumn(st *ir.Struct) string {
	if !g.anyVariable {
		return ""
	}
	return fmt.Sprintf(", %v", g.isVar(st.Name))
}

// emitVariableUnionWalk emits the switch over a union field's SET arm and calls
// body once per variable arm with the arm's table name and member name; an arm
// that is fixed, or a type, holds no pointer and takes no case.
func (g *tableGen) emitVariableUnionWalk(f *ir.Field, v edgeVisitor) {
	un := f.Type.Ref.(*ir.Union)
	// AN ARM IS AN EDGE OF ITS OWN KIND (§2.6): a variable table arm is
	// descended by value, a POINTER arm is the pointer edge itself, a byte
	// buffer arm is that node, and an arm that is another union asks the same
	// switch one level in — all in the union field's own declaration-order
	// position, so a node an arm reaches is numbered where the field sits.
	var each func(un *ir.Union, path, indent string)
	each = func(un *ir.Union, path, indent string) {
		g.pf("%sswitch ( %s.type ) // %s: the set arm is the edge\n%s{\n", indent, v.read+path, f.Name, indent)
		for _, arm := range un.Variants {
			if arm.F == nil {
				continue
			}
			armPath := path + "." + arm.Name
			switch {
			case arm.F.Type.Blob():
				g.pf("%s    case %sType::%s:\n%s    {\n", indent, un.Name, ir.GoExportName(arm.Name), indent)
				v.blob(arm.F, v.at(armPath))
				g.pf("%s        break;\n%s    }\n", indent, indent)
			case arm.F.Type.Pointer:
				g.pf("%s    case %sType::%s:\n%s    {\n", indent, un.Name, ir.GoExportName(arm.Name), indent)
				slots, count := armPath, ""
				if armCompanioned(arm) {
					slots, count = armPath+".value", v.read+armPath+".value_count"
				}
				g.emitPointerSlotsAt(arm.F, v, slots, count, func(slot edgeExpr) { v.pointer(arm.F, slot) })
				g.pf("%s        break;\n%s    }\n", indent, indent)
			case arm.Type != "" && g.isVar(arm.Type):
				g.pf("%s    case %sType::%s:\n%s    {\n", indent, un.Name, ir.GoExportName(arm.Name), indent)
				v.descend(arm.Type, v.at(armPath), indent+"        ")
				g.pf("%s        break;\n%s    }\n", indent, indent)
			default:
				inner, isUnion := arm.F.Type.Ref.(*ir.Union)
				if !isUnion || !g.unionHasEdge(inner, map[*ir.Union]bool{}) {
					continue
				}
				g.pf("%s    case %sType::%s:\n%s    {\n", indent, un.Name, ir.GoExportName(arm.Name), indent)
				each(inner, armPath, indent+"        ")
				g.pf("%s        break;\n%s    }\n", indent, indent)
			}
		}
		g.pf("%s    default: break;\n%s}\n", indent, indent)
	}
	// an ARRAY of unions (§2.6) is that switch per element, the live elements
	// of a counted array only — a slot past the count is not written (§3.1)
	switch f.Array {
	case ir.ArrayCounted:
		g.pf("    for ( int32_t i = 0; i < %s.%s_count && i < %d; i++ ) // %s: [..%d]%s\n    {\n", v.read, f.Name, f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		each(un, fmt.Sprintf(".%s[i]", f.Name), "    ")
		g.pf("    }\n")
	case ir.ArrayFixed:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s: [%d]%s\n    {\n", f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		each(un, fmt.Sprintf(".%s[i]", f.Name), "    ")
		g.pf("    }\n")
	default:
		each(un, "."+f.Name, "    ")
	}
}

// emitPointerSlots emits one block per POINTER SLOT of a field — the field
// itself, or each element of an array of pointers (docs/SPEC-TABLES.md §2.1),
// in index order, the live slots of a counted array only — and calls body with
// the slot's expression under the given subject (`value`, `src`).
func (g *tableGen) emitPointerSlots(f *ir.Field, v edgeVisitor, body func(slot edgeExpr)) {
	g.emitPointerSlotsAt(f, v, "."+f.Name, fmt.Sprintf("%s.%s_count", v.read, f.Name), body)
}

// emitPointerSlotsAt is that walk over an explicit storage PATH, which a
// pointer ARM needs: an arm's slots live in the overlay, and a counted array
// arm's count is the companion beside them (docs/SPEC-TABLES.md §2.6).
func (g *tableGen) emitPointerSlotsAt(f *ir.Field, v edgeVisitor, path, count string, body func(slot edgeExpr)) {
	switch f.Array {
	case ir.ArrayCounted:
		g.pf("    for ( int32_t i = 0; i < %s && i < %d; i++ ) // %s: [..%d]*%s\n    {\n", count, f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(v.at(path + "[i]"))
		g.pf("    }\n")
	case ir.ArrayFixed:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s: [%d]*%s\n    {\n", f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(v.at(path + "[i]"))
		g.pf("    }\n")
	default:
		body(v.at(path))
	}
}

// emitVariableLoadMeasure emits a pointered root's LoadMeasure over one FORM:
// the file's own trailer, or the connection's announced table (§3.3, §6.5).
func (g *tableGen) emitVariableLoadMeasure(st *ir.Struct) {
	n := st.Name
	g.pf("// %sLoadMeasure: the exact region bytes a wire buffer will need, and it is\n", n)
	g.pf("// ONE SCAN — a record's type id gives its storage size, its length gives the\n")
	g.pf("// next record — reading no field value at all, so the caller owns the\n")
	g.pf("// allocation and can refuse a number it did not expect (§6.5).\n")
	g.pf("//\n")
	g.pf("// It reports the DATA bytes and the ATTRIBUTION bytes separately, because the\n")
	g.pf("// attribution is the wire's numbering made resident (§6.3) and a caller may\n")
	g.pf("// release it once Load returns. The answer is their sum.\n")
	g.pf("inline int64_t %sLoadMeasure( const uint8_t * wire_file, int64_t wire_file_bytes, int64_t * attribution_bytes = NULL )\n{\n", n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableIdTable ids_table;\n")
	g.pf("    int64_t body_bytes = 0;\n")
	g.pf("    if ( TableOpen( wire_file, wire_file_bytes, ids_table, body_bytes ) != TableOpenOk ) { return -1; }\n")
	g.pf("    // ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY\n")
	g.pf("    // IS MALFORMED (docs/SPEC-TABLES.md §3): the two ends of the file have\n")
	g.pf("    // met, nothing is decoded, and no region is sized from it.\n")
	g.pf("    if ( TableBodyEndsEarly( wire_file + 1, body_bytes, ids_table ) ) { return -1; }\n")
	g.pf("    const uint8_t * const wire = wire_file + 1;\n")
	g.pf("    const int64_t wire_bytes = body_bytes;\n")
	g.pf("    TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &ignored, &ids_table );\n")
	g.emitRootDataBytes(st, "    ", "return -1;")
	g.pf("    int64_t records = 0;\n")
	g.pf("    uint64_t type_id = 0;\n")
	g.pf("    const uint8_t * body = NULL;\n")
	g.pf("    int64_t length = 0;\n")
	g.pf("    while ( TableNodeScanNext( scan, type_id, body, length ) )\n    {\n")
	g.pf("        records++;\n")
	g.pf("        int64_t storage = %sNodeStorage( type_id, %slength );\n", n, g.nodeStorageArg(st))
	if g.anyMap {
		g.pf("        if ( storage == kTableNodeRefused ) { return -1; } // an N the record's framing cannot carry (§2.8)\n")
	}
	g.pf("        if ( storage > 0 ) { data += storage; } // a type id this build cannot name commands none\n")
	g.pf("    }\n")
	g.pf("    int64_t attribution = ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("    if ( attribution_bytes != NULL ) { *attribution_bytes = attribution; }\n")
	g.pf("    return data + attribution;\n}\n\n")
}

// emitVariableLoad emits a pointered root's Load over one FORM. The scan, the
// numbering and the two passes are ONE walk: only where the id table comes
// from differs (docs/SPEC-TABLES.md §3.1, §3.3).
func (g *tableGen) emitVariableLoad(st *ir.Struct) {
	n := st.Name
	verb := "Load"
	g.pf("// %s%s: decode the tolerant wire into the caller's exact-sized region and\n", n, verb)
	g.pf("// return the root. LOAD IS A SCAN, and that is the whole of its bound: it\n")
	g.pf("// follows no reference, so there is no depth cap, no visited set and no\n")
	g.pf("// ordering rule on the indices. Partial results are kept, as everywhere on\n")
	g.pf("// this wire — the report says what happened. NULL means the CALLER's buffer\n")
	g.pf("// was wrong.\n")
	g.pf("inline const %s * %sLoad( uint8_t * region, int64_t region_bytes, const uint8_t * wire_file, int64_t wire_file_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    // THE FORM BYTE IS READ FIRST, then the trailer, and only then a body:\n")
	g.pf("    // a file that is both a newer form and damaged is a REFUSAL and never\n")
	g.pf("    // damage (docs/SPEC-TABLES.md §3).\n")
	g.pf("    TableIdTable ids_table;\n")
	g.pf("    int64_t body_bytes = 0;\n")
	g.pf("    const TableOpenVerdict verdict = TableOpen( wire_file, wire_file_bytes, ids_table, body_bytes );\n")
	g.pf("    if ( verdict != TableOpenOk )\n    {\n")
	g.pf("        if ( verdict == TableOpenDamaged ) { out->malformed = true; } else { out->refused = true; if ( wire_file_bytes > 0 && wire_file[0] == kTableWireMessageForm ) { out->reason = message_form_as_file; } else { out->reason = newer_form; } }\n")
	g.pf("        return NULL;\n    }\n")
	g.pf("    if ( TableBodyEndsEarly( wire_file + 1, body_bytes, ids_table ) )\n    {\n")
	g.pf("        out->malformed = true; // a byte no field claims, before the table (§3)\n")
	g.pf("        return NULL;\n    }\n")
	g.pf("    const uint8_t * const wire = wire_file + 1;\n")
	g.pf("    const int64_t wire_bytes = body_bytes;\n")
	g.pf("    if ( region == NULL || region_bytes < (int64_t) sizeof( %s ) ) { out->malformed = true; return NULL; }\n", n)
	g.pf("    if ( ( ( (uintptr_t) region ) & ( kTableAlign - 1 ) ) != 0 ) { out->malformed = true; return NULL; }\n")
	g.pf("    memset( region, 0, (size_t) region_bytes );\n")
	g.pf("    uint64_t type_id = 0;\n")
	g.pf("    const uint8_t * body = NULL;\n")
	g.pf("    int64_t length = 0;\n\n")
	g.pf("    // the record count and the data bytes, from the FRAMING alone\n")
	g.emitRootDataBytes(st, "    ", "out->malformed = true; return NULL;")
	g.pf("    int64_t records = 0;\n")
	g.pf("    {\n")
	g.pf("        TableReport counting;\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &counting, &ids_table );\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            records++;\n")
	g.pf("            int64_t storage = %sNodeStorage( type_id, %slength );\n", n, g.nodeStorageArg(st))
	if g.anyMap {
		g.pf("            if ( storage == kTableNodeRefused ) { out->malformed = true; return NULL; }\n")
	}
	g.pf("            if ( storage > 0 ) { data += storage; }\n")
	g.pf("        }\n    }\n")
	g.pf("    int64_t attribution = ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("    if ( data + attribution > region_bytes ) { out->malformed = true; return NULL; }\n\n")
	g.pf("    TableNodeMap nodes;\n")
	g.pf("    nodes.base = region;\n")
	g.pf("    nodes.entries = (const TableNodeDirEntry *) ( region + data );\n")
	g.pf("    nodes.count = records + 1;\n")
	g.pf("    TableNodeDirEntry * directory = (TableNodeDirEntry *) ( region + data );\n")
	g.pf("    directory[0].offset = 0; // position 0 is the ROOT, at offset 0 (§6.3)\n")
	g.pf("    directory[0].type_id = 0x%016xull;\n", ir.TableWireId(st.Name))
	g.pf("    %s * root = new ( region ) %s; // lifetime only: LoadBody's first act is %sReset\n", n, n, n)
	g.pf("    %sReset( *root );\n\n", n)
	g.pf("    // PASS ONE: fill the numbering from the framing, so that an index\n")
	g.pf("    // resolves whichever way it points. It reads no body.\n")
	g.pf("    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out, &ids_table );\n")
	if g.anyMap {
		g.pf("        int64_t used = TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent );\n", n)
	} else {
		g.pf("        int64_t used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	}
	g.pf("        int64_t k = 0;\n")
	g.pf("        int32_t unknown_records = 0; // counted once the scan is known whole\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            int64_t storage = %sNodeStorage( type_id, %slength );\n", n, g.nodeStorageArg(st))
	g.pf("            if ( storage <= 0 )\n            {\n")
	g.pf("                // a record whose type id this build cannot name KEEPS ITS\n")
	g.pf("                // INDEX, is counted once here and not once per pointer, and\n")
	g.pf("                // every reference to it reads null (§3.1)\n")
	g.pf("                unknown_records++;\n")
	g.pf("                directory[k + 1].offset = kTableNodeAbsent;\n")
	g.pf("                directory[k + 1].type_id = type_id;\n")
	g.pf("            }\n            else\n            {\n")
	g.pf("                directory[k + 1].offset = (uint64_t) used;\n")
	g.pf("                directory[k + 1].type_id = type_id;\n")
	g.pf("                %sNodePlace( type_id, region + used, length );\n", n)
	g.pf("                used += storage;\n")
	g.pf("            }\n")
	g.pf("            k++;\n")
	g.pf("        }\n")
	g.pf("        nodes.good = TableNodeScanWhole( scan );\n")
	g.pf("        // the table is whole or it is nothing: a scan that failed counts\n")
	g.pf("        // malformed and NOT the unknowns it met on the way, because the\n")
	g.pf("        // numbering they belonged to does not exist (§3.1)\n")
	g.pf("        if ( nodes.good ) { out->unknown += unknown_records; } else { out->malformed = true; }\n")
	g.pf("    }\n\n")
	g.pf("    // PASS TWO: decode each body into its own storage. A forward index\n")
	g.pf("    // resolves without scratch, because pass one already placed every node.\n")
	g.pf("    if ( nodes.good )\n    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out, &ids_table );\n")
	g.pf("        int64_t k = 0;\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            if ( directory[k + 1].offset != kTableNodeAbsent )\n            {\n")
	g.pf("                TableReader sub( body, length, out, &ids_table );\n")
	g.pf("                %sNodeBody( type_id, sub, nodes, region + directory[k + 1].offset );\n", n)
	g.pf("            }\n")
	g.pf("            k++;\n")
	g.pf("        }\n    }\n\n")
	g.pf("    // and the ROOT's own body last, so every index it carries resolves\n")
	g.pf("    // against a numbering already known good or already known bad\n")
	g.pf("    TableReader r( wire, wire_bytes, out, &ids_table );\n")
	g.pf("    r.nested = false; // the ROOT body, the one that carries the node table\n")
	if g.anyMap {
		g.pf("    TableMapCarve root_carve;\n")
		g.pf("    root_carve.at = region + TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
		g.pf("    root_carve.left = root_extent;\n")
		g.pf("    nodes.carve = &root_carve; // the ROOT's extent is its own, like every node's\n")
	}
	g.pf("    %sLoadBody( r, nodes, *root );\n", n)
	g.pf("    return root;\n}\n\n")
}
