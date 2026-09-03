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
		return fmt.Sprintf("%sMeasureBody( ctx, %s )", name, expr)
	}
	return fmt.Sprintf("%sMeasure( %s )", name, expr)
}

func (g *tableGen) saveCall(name, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sSaveBody( ctx, numbering, w, %s )", name, expr)
	}
	return fmt.Sprintf("%sSaveBody( w, %s )", name, expr)
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

// pointerFields returns a member's pointer fields, in declaration order.
func pointerFields(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if f.Type.Pointer {
			out = append(out, f)
		}
	}
	return out
}

// byValueVariableFields returns the fields that nest a VARIABLE table by
// value — directly or as a bounded array. Their pointer slots live inside the
// owner's storage, so every walk has to descend into them.
func (g *tableGen) byValueVariableFields(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if f.Type.Pointer || f.Type.Kind != ir.TNamed {
			continue
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && g.isVar(ref.Name) {
			out = append(out, f)
		}
	}
	return out
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
			g.pf("template <typename Ctx> inline int64_t %sMeasureBody( const Ctx & ctx, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBodyFields( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBody( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, const %s & value );\n", st.Name, st.Name)
			g.pf("inline bool %sLoadBody( TableReader & r, const TableNodeMap & nodes, %s & value );\n", st.Name, st.Name)
			continue
		}
		g.pf("inline int64_t %sMeasure( const %s & value );\n", st.Name, st.Name)
		g.pf("%s bool %sSaveBody( TableWriter & w, const %s & value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
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
			g.pf("template <typename Ctx> inline int64_t TableNodeMeasure( const Ctx &, const %s & value ) { return %sMeasure( value ); }\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool TableNodeSave( const Ctx &, const TableNumbering &, TableWriter & w, const %s & value ) { return %sSaveBody( w, value ); }\n", st.Name, st.Name)
			continue
		}
		g.pf("template <typename Ctx> inline int64_t TableNodeMeasure( const Ctx & ctx, const %s & value ) { return %sMeasureBody( ctx, value ); }\n", st.Name, st.Name)
		g.pf("template <typename Ctx> inline bool TableNodeSave( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, const %s & value ) { return %sSaveBody( ctx, numbering, w, value ); }\n", st.Name, st.Name)
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
	for _, st := range g.varMembers(members) {
		g.owner = st
		g.emitNumber(st)
		g.emitPackMeasure(st)
		g.emitPack(st)
	}
	for _, st := range members {
		if g.isVar(st.Name) {
			g.emitBuilderAndPublicSurface(st)
		}
	}
}

// emitNumber emits the NUMBERING WALK (docs/SPEC-TABLES.md §3.1): the
// first-visit order of a depth-first pre-order walk from the root over POINTER
// EDGES ONLY — fields in declaration order, array elements in index order, and
// descending through every by-value edge there is to reach the pointer fields
// inside them. A node takes its index the first time it is reached and never
// again.
//
// The numbering is DETERMINISTIC AND RE-DERIVED, NEVER CARRIED: measure derives
// it from the graph and save derives the same one from the same graph, and
// nothing passes between them. That is what makes measure == save hold across a
// pointer graph.
func (g *tableGen) emitNumber(st *ir.Struct) {
	g.pf("// %sNumber: number everything %s POINTS AT, in first-visit order.\n", st.Name, st.Name)
	g.pf("// A reference to an entry whose descent is still OPEN is a data cycle,\n")
	g.pf("// named here rather than recursed away (docs/SPEC-TABLES.md §3.1).\n")
	g.pf("template <typename Ctx>\ninline bool %sNumber( const Ctx & ctx, TableNumbering & numbering, const %s & value )\n{\n", st.Name, st.Name)
	if g.noVariableEdges(st) {
		g.pf("    (void) ctx; (void) numbering; (void) value; // no pointers below this node\n")
	}
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.emitPointerSlots(f, "value", func(slotExpr string) {
			g.pf("    {\n")
			g.pf("        const %s * pointee = %sAt( ctx, %s ); // %s\n", t, t, slotExpr, f.Name)
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
			g.pf("                node.type_id = 0x%016xull; // fnv1a64( \"%s\" )\n", ir.TableTypeId(t), t)
			g.pf("                node.measure = &TableNodeMeasureThunk<Ctx, %s>;\n", t)
			g.pf("                node.save = &TableNodeSaveThunk<Ctx, %s>;\n", t)
			g.pf("                if ( !TableNumberingAppend( numbering, node ) ) { return false; }\n")
			g.pf("                if ( !%sNumber( ctx, numbering, *pointee ) ) { return false; }\n", t)
			g.pf("                TablePackMapClose( numbering.seen, (const void *) pointee, slot );\n")
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		})
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalk(f, func(expr string) {
			g.pf("        if ( !%sNumber( ctx, numbering, %s ) ) { return false; }\n", f.Type.Name, expr)
		})
	}
	for _, f := range g.byValueVariableUnionFields(st) {
		g.emitVariableUnionWalk(f, "value", func(armType, armName string) {
			g.pf("            if ( !%sNumber( ctx, numbering, value.%s.%s ) ) { return false; }\n", armType, f.Name, armName)
		})
	}
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
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.emitPointerSlots(f, "value", func(slotExpr string) {
			g.pf("    {\n")
			g.pf("        const %s * pointee = %sAt( ctx, %s ); // %s\n", t, t, slotExpr, f.Name)
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
			g.pf("                bytes += TableAlignUp64( (int64_t) sizeof( %s ) ) + inner;\n", t)
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		})
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalk(f, func(expr string) {
			g.pf("        int64_t inner = %sPackMeasure( ctx, seen, %s );\n", f.Type.Name, expr)
			g.pf("        if ( inner < 0 ) { return -1; }\n")
			g.pf("        bytes += inner;\n")
		})
	}
	for _, f := range g.byValueVariableUnionFields(st) {
		g.emitVariableUnionWalk(f, "value", func(armType, armName string) {
			g.pf("            int64_t inner = %sPackMeasure( ctx, seen, value.%s.%s );\n", armType, f.Name, armName)
			g.pf("            if ( inner < 0 ) { return -1; }\n")
			g.pf("            bytes += inner;\n")
		})
	}
	g.pf("    return bytes;\n}\n\n")
}

// emitVariableByValueWalk emits the loop shape for descending into a by-value
// nested variable table, scalar or array, and calls body with the element
// expression.
func (g *tableGen) emitVariableByValueWalk(f *ir.Field, body func(expr string)) {
	switch f.Array {
	case ir.ArrayNone:
		g.pf("    { // %s (nested by value)\n", f.Name)
		body("value." + f.Name)
		g.pf("    }\n")
	case ir.ArrayCounted:
		g.pf("    for ( int32_t i = 0; i < value.%s_count && i < %d; i++ ) // %s\n    {\n", f.Name, f.ArrayBound, f.Name)
		body(fmt.Sprintf("value.%s[i]", f.Name))
		g.pf("    }\n")
	default:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s\n    {\n", f.ArrayBound, f.Name)
		body(g.arrayBase("value.", f) + "[i]")
		g.pf("    }\n")
	}
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
	g.pf("template <typename Ctx>\ninline bool %sPack( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used )\n{\n", st.Name, st.Name, st.Name)
	g.pf("    memcpy( (void *) &dst, (const void *) &src, sizeof( %s ) ); // trivially copyable, by construction\n", st.Name)
	if g.noVariableEdges(st) {
		g.pf("    (void) ctx; (void) seen; (void) base; (void) capacity; (void) used;\n")
	}
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.emitPointerSlots(f, "src", func(srcSlot string) {
			dstSlot := "dst" + srcSlot[len("src"):]
			g.pf("    {\n")
			g.pf("        %s.value = 0; // %s\n", dstSlot, f.Name)
			g.pf("        const %s * pointee = %sAt( ctx, %s );\n", t, t, srcSlot)
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
			g.pf("                if ( at + (int64_t) sizeof( %s ) > capacity ) { return false; }\n", t)
			g.pf("                used = at + TableAlignUp64( (int64_t) sizeof( %s ) );\n", t)
			g.pf("                %s * child = new ( base + at ) %s; // lifetime only: the Pack below memcpy's the whole node over it\n", t, t)
			g.pf("                %s.value = (int64_t) ( ( base + at ) - (const uint8_t *) &%s );\n", dstSlot, dstSlot)
			g.pf("                if ( !%sPack( ctx, seen, *pointee, *child, base, capacity, used ) ) { return false; }\n", t)
			g.pf("                TablePackMapClose( seen, (const void *) pointee, slot );\n")
			g.pf("            }\n")
			g.pf("        }\n    }\n")
		})
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalkPack(f)
	}
	for _, f := range g.byValueVariableUnionFields(st) {
		g.emitVariableUnionWalk(f, "src", func(armType, armName string) {
			g.pf("            if ( !%sPack( ctx, seen, src.%s.%s, dst.%s.%s, base, capacity, used ) ) { return false; }\n", armType, f.Name, armName, f.Name, armName)
		})
	}
	g.pf("    return true;\n}\n\n")
}

func (g *tableGen) emitVariableByValueWalkPack(f *ir.Field) {
	t := f.Type.Name
	call := func(srcExpr, dstExpr string) {
		g.pf("        if ( !%sPack( ctx, seen, %s, %s, base, capacity, used ) ) { return false; }\n", t, srcExpr, dstExpr)
	}
	switch f.Array {
	case ir.ArrayNone:
		g.pf("    { // %s (nested by value)\n", f.Name)
		call("src."+f.Name, "dst."+f.Name)
		g.pf("    }\n")
	case ir.ArrayCounted:
		g.pf("    for ( int32_t i = 0; i < src.%s_count && i < %d; i++ ) // %s\n    {\n", f.Name, f.ArrayBound, f.Name)
		call(fmt.Sprintf("src.%s[i]", f.Name), fmt.Sprintf("dst.%s[i]", f.Name))
		g.pf("    }\n")
	default:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s\n    {\n", f.ArrayBound, f.Name)
		call(g.arrayBase("src.", f)+"[i]", g.arrayBase("dst.", f)+"[i]")
		g.pf("    }\n")
	}
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
	g.pf("    int64_t total = TableAlignUp64( (int64_t) sizeof( %s ) ) + below;\n", n)
	g.pf("    // the AUTHORING path may allocate (§6.5), and it does so through the\n")
	g.pf("    // builder's own pair. The region comes back ZEROED, which is the\n")
	g.pf("    // allocator's contract: a packed region carries node padding.\n")
	g.pf("    uint8_t * packed = (uint8_t *) arena.allocator.alloc( arena.allocator.context, total );\n")
	g.pf("    if ( packed == NULL ) { TablePackMapShutdown( seen ); return false; }\n")
	g.pf("    int64_t used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
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
	g.pf("        bytes = %sMeasureBody( ctx, root );\n", n)
	g.pf("        if ( bytes >= 0 )\n        {\n")
	g.pf("            int64_t table = TableNodeTableMeasure( ctx, numbering );\n")
	g.pf("            bytes = table < 0 ? -1 : bytes + table;\n")
	g.pf("        }\n    }\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return bytes;\n}\n\n")

	g.pf("template <typename Ctx>\ninline int64_t %sSaveWire( const Ctx & ctx, const %s & root, uint8_t * buffer, int64_t capacity, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    if ( !%sNumberFrom( ctx, numbering, root ) ) { TableNumberingShutdown( numbering ); return -1; }\n", n)
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    // the root's own fields, then the node table's fields, then the\n")
	g.pf("    // terminator: a reader that gives up inside the table has already\n")
	g.pf("    // decoded the ROOT'S OWN FIELDS (§3.1)\n")
	g.pf("    bool ok = %sSaveBodyFields( ctx, numbering, w, root ) && TableNodeTableSave( ctx, w, numbering );\n", n)
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    if ( !ok ) { return -1; }\n")
	g.pf("    w.put16( 0 );\n")
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

	// load
	g.pf("// %sLoadMeasure: the exact region bytes a wire buffer will need, and it is\n", n)
	g.pf("// ONE SCAN — a record's type id gives its storage size, its length gives the\n")
	g.pf("// next record — reading no field value at all, so the caller owns the\n")
	g.pf("// allocation and can refuse a number it did not expect (§6.5).\n")
	g.pf("//\n")
	g.pf("// It reports the DATA bytes and the ATTRIBUTION bytes separately, because the\n")
	g.pf("// attribution is the wire's numbering made resident (§6.3) and a caller may\n")
	g.pf("// release it once Load returns. The answer is their sum.\n")
	g.pf("inline int64_t %sLoadMeasure( const uint8_t * wire, int64_t wire_bytes, int64_t * attribution_bytes = NULL )\n{\n", n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &ignored );\n")
	g.pf("    int64_t data = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    int64_t records = 0;\n")
	g.pf("    uint64_t type_id = 0;\n")
	g.pf("    const uint8_t * body = NULL;\n")
	g.pf("    int64_t length = 0;\n")
	g.pf("    while ( TableNodeScanNext( scan, type_id, body, length ) )\n    {\n")
	g.pf("        records++;\n")
	g.pf("        int64_t storage = %sNodeStorage( type_id );\n", n)
	g.pf("        if ( storage > 0 ) { data += storage; } // a type id this build cannot name commands none\n")
	g.pf("    }\n")
	g.pf("    int64_t attribution = ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("    if ( attribution_bytes != NULL ) { *attribution_bytes = attribution; }\n")
	g.pf("    return data + attribution;\n}\n\n")

	g.pf("// %sLoad: decode the tolerant wire into the caller's exact-sized region and\n", n)
	g.pf("// return the root. LOAD IS A SCAN, and that is the whole of its bound: it\n")
	g.pf("// follows no reference, so there is no depth cap, no visited set and no\n")
	g.pf("// ordering rule on the indices. Partial results are kept, as everywhere on\n")
	g.pf("// this wire — the report says what happened. NULL means the CALLER's buffer\n")
	g.pf("// was wrong.\n")
	g.pf("inline const %s * %sLoad( uint8_t * region, int64_t region_bytes, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    if ( region == NULL || region_bytes < (int64_t) sizeof( %s ) ) { out->malformed = true; return NULL; }\n", n)
	g.pf("    if ( ( ( (uintptr_t) region ) & ( kTableAlign - 1 ) ) != 0 ) { out->malformed = true; return NULL; }\n")
	g.pf("    memset( region, 0, (size_t) region_bytes );\n")
	g.pf("    uint64_t type_id = 0;\n")
	g.pf("    const uint8_t * body = NULL;\n")
	g.pf("    int64_t length = 0;\n\n")
	g.pf("    // the record count and the data bytes, from the FRAMING alone\n")
	g.pf("    int64_t data = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    int64_t records = 0;\n")
	g.pf("    {\n")
	g.pf("        TableReport counting;\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &counting );\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            records++;\n")
	g.pf("            int64_t storage = %sNodeStorage( type_id );\n", n)
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
	g.pf("    directory[0].type_id = 0x%016xull;\n", ir.TableTypeId(st.Name))
	g.pf("    %s * root = new ( region ) %s; // lifetime only: LoadBody's first act is %sReset\n", n, n, n)
	g.pf("    %sReset( *root );\n\n", n)
	g.pf("    // PASS ONE: fill the numbering from the framing, so that an index\n")
	g.pf("    // resolves whichever way it points. It reads no body.\n")
	g.pf("    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out );\n")
	g.pf("        int64_t used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("        int64_t k = 0;\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            int64_t storage = %sNodeStorage( type_id );\n", n)
	g.pf("            if ( storage <= 0 )\n            {\n")
	g.pf("                // a record whose type id this build cannot name KEEPS ITS\n")
	g.pf("                // INDEX, is counted once here and not once per pointer, and\n")
	g.pf("                // every reference to it reads null (§3.1)\n")
	g.pf("                out->unknown++;\n")
	g.pf("                directory[k + 1].offset = kTableNodeAbsent;\n")
	g.pf("                directory[k + 1].type_id = type_id;\n")
	g.pf("            }\n            else\n            {\n")
	g.pf("                directory[k + 1].offset = (uint64_t) used;\n")
	g.pf("                directory[k + 1].type_id = type_id;\n")
	g.pf("                %sNodePlace( type_id, region + used );\n", n)
	g.pf("                used += storage;\n")
	g.pf("            }\n")
	g.pf("            k++;\n")
	g.pf("        }\n")
	g.pf("        nodes.good = TableNodeScanWhole( scan );\n")
	g.pf("        if ( !nodes.good ) { out->malformed = true; } // the table is whole or it is nothing\n")
	g.pf("    }\n\n")
	g.pf("    // PASS TWO: decode each body into its own storage. A forward index\n")
	g.pf("    // resolves without scratch, because pass one already placed every node.\n")
	g.pf("    if ( nodes.good )\n    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out );\n")
	g.pf("        int64_t k = 0;\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            if ( directory[k + 1].offset != kTableNodeAbsent )\n            {\n")
	g.pf("                TableReader sub( body, length, out );\n")
	g.pf("                %sNodeBody( type_id, sub, nodes, region + directory[k + 1].offset );\n", n)
	g.pf("            }\n")
	g.pf("            k++;\n")
	g.pf("        }\n    }\n\n")
	g.pf("    // and the ROOT's own body last, so every index it carries resolves\n")
	g.pf("    // against a numbering already known good or already known bad\n")
	g.pf("    TableReader r( wire, wire_bytes, out );\n")
	g.pf("    %sLoadBody( r, nodes, *root );\n", n)
	g.pf("    return root;\n}\n\n")

	g.pf("// %sLoadBuilder: the TOOL's path — the same tolerant decode into a fresh\n", n)
	g.pf("// builder, so loaded data can be edited and locked again. The numbering is\n")
	g.pf("// the same one; what differs is where a node lives and therefore what a\n")
	g.pf("// resolved slot holds — an arena offset here, a self-relative delta there.\n")
	g.pf("inline bool %sLoadBuilder( %sBuilder & builder, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    %s * root = builder.GetRoot();\n", n)
	g.pf("    if ( root == NULL ) { out->malformed = true; return false; }\n")
	g.pf("    uint64_t type_id = 0;\n")
	g.pf("    const uint8_t * body = NULL;\n")
	g.pf("    int64_t length = 0;\n")
	g.pf("    int64_t records = 0;\n")
	g.pf("    {\n")
	g.pf("        TableReport counting;\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &counting );\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) ) { records++; }\n")
	g.pf("    }\n")
	g.pf("    // the AUTHORING side may allocate (§6.5), and this is the tool's path.\n")
	g.pf("    // It goes through the builder's own pair, like everything else the\n")
	g.pf("    // builder reaches, and the entries come back zeroed.\n")
	g.pf("    const TableAllocator allocator = builder.arena.allocator;\n")
	g.pf("    TableNodeDirEntry * directory = (TableNodeDirEntry *) allocator.alloc( allocator.context, ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry ) );\n")
	g.pf("    if ( directory == NULL ) { out->malformed = true; return false; }\n")
	g.pf("    directory[0].offset = (uint64_t) builder.root_ref.value;\n")
	g.pf("    directory[0].type_id = 0x%016xull;\n", ir.TableTypeId(st.Name))
	g.pf("    TableNodeMap nodes;\n")
	g.pf("    nodes.base = NULL;\n")
	g.pf("    nodes.entries = directory;\n")
	g.pf("    nodes.count = records + 1;\n")
	g.pf("    nodes.arena = true; // a resolved slot holds the node's ARENA OFFSET here\n")
	g.pf("    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out );\n")
	g.pf("        int64_t k = 0;\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            uint32_t at = %sNodeAlloc( type_id, builder.main );\n", n)
	g.pf("            if ( at == 0 )\n            {\n")
	g.pf("                out->unknown++;\n")
	g.pf("                directory[k + 1].offset = kTableNodeAbsent;\n")
	g.pf("            }\n            else\n            {\n")
	g.pf("                directory[k + 1].offset = (uint64_t) at;\n")
	g.pf("            }\n")
	g.pf("            directory[k + 1].type_id = type_id;\n")
	g.pf("            k++;\n")
	g.pf("        }\n")
	g.pf("        nodes.good = TableNodeScanWhole( scan );\n")
	g.pf("        if ( !nodes.good ) { out->malformed = true; }\n")
	g.pf("    }\n")
	g.pf("    if ( nodes.good )\n    {\n")
	g.pf("        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, out );\n")
	g.pf("        int64_t k = 0;\n")
	g.pf("        while ( TableNodeScanNext( scan, type_id, body, length ) )\n        {\n")
	g.pf("            if ( directory[k + 1].offset != kTableNodeAbsent )\n            {\n")
	g.pf("                TableReader sub( body, length, out );\n")
	g.pf("                %sNodeBody( type_id, sub, nodes, TableArenaAt( builder.arena, (uint32_t) directory[k + 1].offset ) );\n", n)
	g.pf("            }\n")
	g.pf("            k++;\n")
	g.pf("        }\n    }\n")
	g.pf("    TableReader r( wire, wire_bytes, out );\n")
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

	g.pf("// %sNodeStorage: the region bytes one record commands, or -1 for a type id\n", n)
	g.pf("// this build cannot name — which keeps its index and reads null.\n")
	g.pf("inline int64_t %sNodeStorage( uint64_t type_id )\n{\n", n)
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: return TableAlignUp64( (int64_t) sizeof( %s ) ); // %s\n", ir.TableTypeId(t.Name), t.Name, t.Name)
	}
	g.pf("        default: break;\n    }\n")
	g.pf("    return -1;\n}\n\n")

	g.pf("// %sNodePlace: start one record's node's lifetime in the storage pass one\n", n)
	g.pf("// reserved for it, holding exactly the declared defaults.\n")
	g.pf("inline void %sNodePlace( uint64_t type_id, uint8_t * at )\n{\n", n)
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: { %s * node = new ( at ) %s; %sReset( *node ); break; } // %s\n",
			ir.TableTypeId(t.Name), t.Name, t.Name, t.Name, t.Name)
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("// %sNodeAlloc: the TOOL's path — one record's node in the builder's arena.\n", n)
	g.pf("// Zero is the arena's null, and it is also what a type id this build cannot\n")
	g.pf("// name answers.\n")
	g.pf("inline uint32_t %sNodeAlloc( uint64_t type_id, TableWorker & worker )\n{\n", n)
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: return (uint32_t) worker.Alloc<%s>().ref.value; // %s\n", ir.TableTypeId(t.Name), t.Name, t.Name)
	}
	g.pf("        default: break;\n    }\n")
	g.pf("    return 0;\n}\n\n")

	g.pf("// %sNodeBody: PASS TWO's half — decode one record's body into the storage it\n", n)
	g.pf("// already owns.\n")
	g.pf("inline void %sNodeBody( uint64_t type_id, TableReader & r, const TableNodeMap & nodes, uint8_t * at )\n{\n", n)
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
		g.pf("        case 0x%016xull: %s; break; // %s\n", ir.TableTypeId(t.Name), call, t.Name)
	}
	g.pf("        default: break;\n    }\n}\n\n")
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

// byValueVariableUnionFields returns the union fields with at least one
// VARIABLE table arm (docs/SPEC-TABLES.md §2.6): the mode runs through arms, so
// the set arm is a by-value edge every pointer walk descends.
func (g *tableGen) byValueVariableUnionFields(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		un, ok := f.Type.Ref.(*ir.Union)
		if !ok {
			continue
		}
		for _, v := range un.Variants {
			if g.isVar(v.Type) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// noVariableEdges reports a member with no pointer below it: no pointer
// field, no by-value variable nesting and no union with a variable arm.
func (g *tableGen) noVariableEdges(st *ir.Struct) bool {
	return len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 && len(g.byValueVariableUnionFields(st)) == 0
}

// emitVariableUnionWalk emits the switch over a union field's SET arm and calls
// body once per variable arm with the arm's table name and member name; an arm
// that is fixed, or a type, holds no pointer and takes no case.
func (g *tableGen) emitVariableUnionWalk(f *ir.Field, subject string, body func(armType, armName string)) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("    switch ( %s.%s.type ) // %s: the set arm is the by-value edge\n    {\n", subject, f.Name, f.Name)
	for _, v := range un.Variants {
		if !g.isVar(v.Type) {
			continue
		}
		g.pf("        case %sType::%s:\n        {\n", un.Name, ir.GoExportName(v.Name))
		body(v.Type, v.Name)
		g.pf("            break;\n        }\n")
	}
	g.pf("        default: break;\n    }\n")
}

// emitPointerSlots emits one block per POINTER SLOT of a field — the field
// itself, or each element of an array of pointers (docs/SPEC-TABLES.md §2.1),
// in index order, the live slots of a counted array only — and calls body with
// the slot's expression under the given subject (`value`, `src`).
func (g *tableGen) emitPointerSlots(f *ir.Field, subject string, body func(slot string)) {
	switch f.Array {
	case ir.ArrayCounted:
		g.pf("    for ( int32_t i = 0; i < %s.%s_count && i < %d; i++ ) // %s: [..%d]*%s\n    {\n", subject, f.Name, f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(fmt.Sprintf("%s.%s[i]", subject, f.Name))
		g.pf("    }\n")
	case ir.ArrayFixed:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s: [%d]*%s\n    {\n", f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(fmt.Sprintf("%s.%s[i]", subject, f.Name))
		g.pf("    }\n")
	default:
		body(fmt.Sprintf("%s.%s", subject, f.Name))
	}
}
