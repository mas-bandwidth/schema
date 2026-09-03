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

// ---- what the depth cap counts (docs/SPEC-TABLES.md §3.1) ----
//
// ONLY POINTER EDGES CHARGE DEPTH. By-value nesting — a table inside a table,
// or a bounded array of them — charges nothing, because by-value composition is
// already finite: the checker refuses by-value cycles, so its nesting is
// bounded by the schema itself and cannot be driven by data.
//
// This has to hold in ALL FOUR walks — measure, save, load and pack — or the
// forms disagree about which structures are legal, and a structure that Locks
// is refused by the wire. The two constants below are the whole rule.
const (
	depthSame = "depth"     // by-value nesting: the schema already bounds it
	depthDown = "depth + 1" // a pointer edge: only DATA can make this deep
)

// measureCall renders a nested MEASURE call on a closure member: a variable
// member takes the resolution context and the depth, a fixed one takes
// neither — so a fixed table's codec is character-for-character what it was
// before pointers existed.
func (g *tableGen) measureCall(name, expr, depth string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sMeasureBody( ctx, %s, %s )", name, expr, depth)
	}
	return fmt.Sprintf("%sMeasure( %s )", name, expr)
}

func (g *tableGen) saveCall(name, expr, depth string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sSaveBody( ctx, w, %s, %s )", name, expr, depth)
	}
	return fmt.Sprintf("%sSaveBody( w, %s )", name, expr)
}

func (g *tableGen) loadCall(name, reader, expr, depth string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sLoadBody( %s, sink, %s, %s )", name, reader, expr, depth)
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
			g.pf("template <typename Ctx> inline int64_t %sMeasureBody( const Ctx & ctx, const %s & value, int32_t depth );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBody( const Ctx & ctx, TableWriter & w, const %s & value, int32_t depth );\n", st.Name, st.Name)
			g.pf("template <typename Sink> inline bool %sLoadBody( TableReader & r, Sink & sink, %s & value, int32_t depth );\n", st.Name, st.Name)
			continue
		}
		g.pf("inline int64_t %sMeasure( const %s & value );\n", st.Name, st.Name)
		g.pf("%s bool %sSaveBody( TableWriter & w, const %s & value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
		g.pf("%s bool %sLoadBody( TableReader & r, %s & value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
	}
	g.pf("\n")
	if vars := g.varMembers(members); len(vars) > 0 {
		g.pf("// ---- pointer-graph walkers: pack (Lock), size (Load) ----\n\n")
		for _, st := range vars {
			g.pf("template <typename Ctx> inline int64_t %sPackMeasure( const Ctx & ctx, TablePackMap & seen, const %s & value, int32_t depth );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sPack( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used, int32_t depth );\n", st.Name, st.Name, st.Name)
			g.pf("inline int64_t %sLoadMeasureBody( TableReader & r, int32_t depth );\n", st.Name)
		}
		g.pf("\n")
	}
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
	g.pf("// bump one %s into the caller's exact region; the slot comes out self-relative\n", n)
	g.pf("inline %s * %sEmplace( TableRegionSink & sink, TableRef & slot )\n{\n", n, n)
	g.pf("    int64_t at = TableAlignUp64( sink.used );\n")
	g.pf("    if ( at + (int64_t) sizeof( %s ) > sink.capacity ) { return NULL; }\n", n)
	g.pf("    sink.used = at + TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    %s * node = new ( sink.base + at ) %s; // the lifetime; the defaults are the line below\n", n, n)
	g.pf("    %sReset( *node ); // one member at a time, never `%s{}` over the aggregate (#320)\n", n, n)
	g.pf("    slot.value = (int64_t) ( ( sink.base + at ) - (uint8_t *) &slot );\n")
	g.pf("    return node;\n}\n")
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
		g.emitPackMeasure(st)
		g.emitPack(st)
		g.emitLoadMeasureBody(st)
	}
	for _, st := range members {
		if g.isVar(st.Name) {
			g.emitBuilderAndPublicSurface(st)
		}
	}
}

// emitPackMeasure emits the exact byte count of a value's DESCENDANT nodes in
// the packed form — Lock's sizing half.
func (g *tableGen) emitPackMeasure(st *ir.Struct) {
	g.pf("// %sPackMeasure: the packed region bytes of everything %s POINTS AT.\n", st.Name, st.Name)
	g.pf("// ONE VISIT PER NODE: `seen` carries the first-visit numbering (§3.1), so a\n")
	g.pf("// node two references name is measured ONCE and packed once, and a\n")
	g.pf("// reference to a node whose descent is still open is a data cycle, refused.\n")
	g.pf("template <typename Ctx>\ninline int64_t %sPackMeasure( const Ctx & ctx, TablePackMap & seen, const %s & value, int32_t depth )\n{\n", st.Name, st.Name)
	g.pf("    if ( depth > kTableMaxDepth ) { return -1; } // a chain past the cap\n")
	g.pf("    int64_t bytes = 0;\n")
	if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
		g.pf("    (void) ctx; (void) seen; (void) value; // no pointers below this node\n")
	}
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee = %sAt( ctx, value.%s ); // %s\n", t, t, f.Name, f.Name)
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
		g.pf("                int64_t inner = %sPackMeasure( ctx, seen, *pointee, depth + 1 );\n", t)
		g.pf("                if ( inner < 0 ) { return -1; }\n")
		g.pf("                TablePackMapClose( seen, (const void *) pointee, slot );\n")
		g.pf("                bytes += TableAlignUp64( (int64_t) sizeof( %s ) ) + inner;\n", t)
		g.pf("            }\n")
		g.pf("        }\n    }\n")
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalk(f, func(expr string) {
			g.pf("        int64_t inner = %sPackMeasure( ctx, seen, %s, depth );\n", f.Type.Name, expr)
			g.pf("        if ( inner < 0 ) { return -1; }\n")
			g.pf("        bytes += inner;\n")
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
	g.pf("template <typename Ctx>\ninline bool %sPack( const Ctx & ctx, TablePackMap & seen, const %s & src, %s & dst, uint8_t * base, int64_t capacity, int64_t & used, int32_t depth )\n{\n", st.Name, st.Name, st.Name)
	g.pf("    if ( depth > kTableMaxDepth ) { return false; }\n")
	g.pf("    memcpy( (void *) &dst, (const void *) &src, sizeof( %s ) ); // trivially copyable, by construction\n", st.Name)
	if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
		g.pf("    (void) ctx; (void) seen; (void) base; (void) capacity; (void) used;\n")
	}
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        dst.%s.value = 0; // %s\n", f.Name, f.Name)
		g.pf("        const %s * pointee = %sAt( ctx, src.%s );\n", t, t, f.Name)
		g.pf("        if ( pointee != NULL )\n        {\n")
		g.pf("            int64_t at = TableAlignUp64( used ); // where it WOULD land, if this is its first visit\n")
		g.pf("            bool taken = false;\n")
		g.pf("            int64_t slot = 0;\n")
		g.pf("            const TablePackEntry * entry = TablePackMapReach( seen, (const void *) pointee, at, taken, slot );\n")
		g.pf("            if ( entry == NULL ) { return false; } // the map could not grow\n")
		g.pf("            if ( !taken )\n            {\n")
		g.pf("                if ( entry->open != 0 ) { return false; } // a data cycle\n")
		g.pf("                dst.%s.value = (int64_t) ( ( base + entry->offset ) - (const uint8_t *) &dst.%s ); // the one body it already has\n", f.Name, f.Name)
		g.pf("            }\n            else\n            {\n")
		g.pf("                if ( at + (int64_t) sizeof( %s ) > capacity ) { return false; }\n", t)
		g.pf("                used = at + TableAlignUp64( (int64_t) sizeof( %s ) );\n", t)
		g.pf("                %s * child = new ( base + at ) %s; // lifetime only: the Pack below memcpy's the whole node over it\n", t, t)
		g.pf("                dst.%s.value = (int64_t) ( ( base + at ) - (const uint8_t *) &dst.%s );\n", f.Name, f.Name)
		g.pf("                if ( !%sPack( ctx, seen, *pointee, *child, base, capacity, used, depth + 1 ) ) { return false; }\n", t)
		g.pf("                TablePackMapClose( seen, (const void *) pointee, slot );\n")
		g.pf("            }\n")
		g.pf("        }\n    }\n")
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalkPack(f)
	}
	g.pf("    return true;\n}\n\n")
}

func (g *tableGen) emitVariableByValueWalkPack(f *ir.Field) {
	t := f.Type.Name
	call := func(srcExpr, dstExpr string) {
		g.pf("        if ( !%sPack( ctx, seen, %s, %s, base, capacity, used, depth ) ) { return false; }\n", t, srcExpr, dstExpr)
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

// emitLoadMeasureBody emits the wire sizing pre-pass: how many region bytes
// the nodes BELOW this body will need. Skip-based — it reads framing and
// nothing else, which is what lets a caller own the allocation (SPEC §7).
func (g *tableGen) emitLoadMeasureBody(st *ir.Struct) {
	ptrs := pointerFields(st)
	nested := g.byValueVariableFields(st)
	g.pf("// %sLoadMeasureBody: region bytes for the nodes under this wire body.\n", st.Name)
	g.pf("// Framing only — no field value is decoded, so a caller can size its\n")
	g.pf("// buffer before a single byte is placed.\n")
	g.pf("inline int64_t %sLoadMeasureBody( TableReader & r, int32_t depth )\n{\n", st.Name)
	g.pf("    int64_t bytes = 0;\n")
	if len(ptrs) == 0 && len(nested) == 0 {
		g.pf("    (void) r; (void) depth; // nothing below this body allocates\n")
		g.pf("    return bytes;\n}\n\n")
		return
	}
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        if ( !r.has( 2 ) ) { return bytes; }\n")
	g.pf("        uint16_t field_id = r.get16();\n")
	g.pf("        if ( field_id == 0 ) { return bytes; }\n")
	g.pf("        if ( !r.has( 1 ) ) { return bytes; }\n")
	g.pf("        uint8_t kind = r.get8();\n")
	g.pf("        switch ( field_id )\n        {\n")
	for _, f := range ptrs {
		t := f.Type.Name
		g.pf("            case 0x%04x: // %s (*%s)\n            {\n", ir.TableFieldId(f), f.Name, t)
		g.pf("                if ( kind != %d ) { if ( !r.skip( kind ) ) { return bytes; } break; }\n", tkTable)
		g.pf("                if ( !r.has( 4 ) ) { return bytes; }\n")
		g.pf("                uint32_t body_len = r.get32();\n")
		g.pf("                if ( !r.has( body_len ) ) { return bytes; }\n")
		g.pf("                if ( depth < kTableMaxDepth )\n                {\n")
		g.pf("                    bytes += TableAlignUp64( (int64_t) sizeof( %s ) );\n", t)
		g.pf("                    TableReader sub( r.buffer + r.offset, body_len, r.report );\n")
		g.pf("                    bytes += %sLoadMeasureBody( sub, depth + 1 );\n", t)
		g.pf("                }\n")
		g.pf("                r.offset += body_len;\n")
		g.pf("                break;\n            }\n")
	}
	for _, f := range nested {
		t := f.Type.Name
		wireKind := tkTable
		if f.Array != ir.ArrayNone {
			wireKind = tkArray
		}
		if f.KeyEnum != "" {
			wireKind = tkKeyed // a keyed body is its own kind (docs/SPEC-TABLES.md §3.2)
		}
		g.pf("            case 0x%04x: // %s (%s nested by value)\n            {\n", ir.TableFieldId(f), f.Name, t)
		g.pf("                if ( kind != %d ) { if ( !r.skip( kind ) ) { return bytes; } break; }\n", wireKind)
		g.pf("                if ( !r.has( 4 ) ) { return bytes; }\n")
		g.pf("                uint32_t body_len = r.get32();\n")
		g.pf("                if ( !r.has( body_len ) ) { return bytes; }\n")
		g.pf("                int64_t body_end = r.offset + body_len;\n")
		if f.Array == ir.ArrayNone {
			g.pf("                {\n")
			g.pf("                    TableReader sub( r.buffer + r.offset, body_len, r.report );\n")
			g.pf("                    bytes += %sLoadMeasureBody( sub, depth );\n", t)
			g.pf("                }\n")
		} else {
			g.pf("                if ( body_len >= 5 )\n                {\n")
			g.pf("                    r.get8(); // element kind\n")
			g.pf("                    uint32_t count = r.get32();\n")
			g.pf("                    TableReader elems( r.buffer + r.offset, body_end - r.offset, r.report );\n")
			g.pf("                    for ( uint32_t i = 0; i < count && i < %d; i++ )\n                    {\n", f.ArrayBound)
			if f.KeyEnum != "" {
				// an ENUM-KEYED array puts the slot's variant id before the
				// element's length (docs/SPEC-TABLES.md §3.2). The pre-pass reads
				// framing only, so it skips the key without naming it — but it
				// must skip it, or every element length after the first is read
				// out of a key's bytes
				g.pf("                        if ( !elems.has( 2 ) ) { break; }\n")
				g.pf("                        elems.get16(); // the slot's variant id\n")
			}
			g.pf("                        if ( !elems.has( 4 ) ) { break; }\n")
			g.pf("                        uint32_t elem_len = elems.get32();\n")
			g.pf("                        if ( !elems.has( elem_len ) ) { break; }\n")
			g.pf("                        TableReader elem( elems.buffer + elems.offset, elem_len, r.report );\n")
			g.pf("                        bytes += %sLoadMeasureBody( elem, depth );\n", t)
			g.pf("                        elems.offset += elem_len;\n")
			g.pf("                    }\n")
			g.pf("                }\n")
		}
		g.pf("                r.offset = body_end;\n")
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n            {\n")
	g.pf("                if ( !r.skip( kind ) ) { return bytes; }\n")
	g.pf("                break;\n            }\n")
	g.pf("        }\n    }\n}\n\n")
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
	g.pf("    %sBuilder()\n    {\n", n)
	g.pf("        TableArenaInit( arena );\n")
	g.pf("        main.arena = &arena;\n")
	g.pf("        TableSlot<%s> slot = main.Alloc<%s>();\n", n, n)
	g.pf("        root_ref = slot.ref;\n")
	g.pf("    }\n")
	g.pf("    ~%sBuilder() { TableArenaShutdown( arena ); free( region ); }\n", n)
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
	g.pf("    TablePackMapInit( seen );\n")
	g.pf("    bool root_taken = false;\n")
	g.pf("    int64_t root_slot = 0;\n")
	g.pf("    int64_t below = -1;\n")
	g.pf("    if ( TablePackMapReach( seen, (const void *) &root, 0, root_taken, root_slot ) != NULL )\n    {\n")
	g.pf("        below = %sPackMeasure( ctx, seen, root, 1 );\n", n)
	g.pf("    }\n")
	g.pf("    if ( below < 0 ) { TablePackMapShutdown( seen ); return false; } // a data cycle, or a chain past kTableMaxDepth\n")
	g.pf("    int64_t total = TableAlignUp64( (int64_t) sizeof( %s ) ) + below;\n", n)
	g.pf("    uint8_t * packed = (uint8_t *) malloc( (size_t) total ); // the AUTHORING path may allocate\n")
	g.pf("    if ( packed == NULL ) { TablePackMapShutdown( seen ); return false; }\n")
	g.pf("    memset( packed, 0, (size_t) total );\n")
	g.pf("    int64_t used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    %s * destination = new ( packed ) %s; // lifetime only: the Pack below memcpy's the whole node over it\n", n, n)
	g.pf("    // The pack walk RE-DERIVES the same numbering rather than carrying the\n")
	g.pf("    // measure's — nothing passes between them, which is what makes\n")
	g.pf("    // `used == total` below a real check and not a tautology (§3.1). The\n")
	g.pf("    // map keeps the capacity the measure paid for, so the second walk\n")
	g.pf("    // rehashes nothing.\n")
	g.pf("    TablePackMapReset( seen );\n")
	g.pf("    if ( TablePackMapReach( seen, (const void *) &root, 0, root_taken, root_slot ) == NULL ||\n")
	g.pf("         !%sPack( ctx, seen, root, *destination, packed, total, used, 1 ) || used != total )\n    {\n", n)
	g.pf("        TablePackMapShutdown( seen );\n")
	g.pf("        free( packed );\n        return false;\n    }\n")
	g.pf("    TablePackMapShutdown( seen );\n")
	g.pf("    region = packed;\n")
	g.pf("    region_bytes = total;\n")
	g.pf("    arena.locked = true; // MONOTONIC: there is no unlock\n")
	g.pf("    TableArenaShutdown( arena );\n")
	g.pf("    return true;\n}\n\n")

	// wire out
	g.pf("// ---- %s on the wire: the generic, tolerant form (docs/SPEC-TABLES.md §3) ----\n\n", n)
	g.pf("inline int64_t %sMeasure( const %s * root )\n{\n", n, n)
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return root != NULL ? %sMeasureBody( ctx, *root, 1 ) : -1;\n}\n\n", n)
	g.pf("inline int64_t %sSave( const %s * root, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    if ( root == NULL ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    if ( !%sSaveBody( ctx, w, *root, 1 ) ) { return -1; }\n", n)
	g.pf("    return w.offset; // == %sMeasure( root )\n}\n\n", n)
	g.pf("inline int64_t %sMeasure( const %sBuilder & builder )\n{\n", n, n)
	g.pf("    if ( builder.region != NULL ) { return %sMeasure( builder.AsConst() ); }\n", n)
	g.pf("    if ( builder.root_ref.null() ) { return -1; } // the root allocation failed\n")
	g.pf("    TableArenaCtx ctx = { &builder.arena };\n")
	g.pf("    return %sMeasureBody( ctx, *(const %s *) TableArenaAt( builder.arena, (uint32_t) builder.root_ref.value ), 1 );\n}\n\n", n, n)
	g.pf("inline int64_t %sSave( const %sBuilder & builder, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    if ( builder.region != NULL ) { return %sSave( builder.AsConst(), buffer, capacity ); }\n", n)
	g.pf("    if ( builder.root_ref.null() ) { return -1; } // the root allocation failed\n")
	g.pf("    TableArenaCtx ctx = { &builder.arena };\n")
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    if ( !%sSaveBody( ctx, w, *(const %s *) TableArenaAt( builder.arena, (uint32_t) builder.root_ref.value ), 1 ) ) { return -1; }\n", n, n)
	g.pf("    return w.offset;\n}\n\n")

	// load
	g.pf("// %sLoadMeasure: the exact region bytes a wire buffer will need. The\n", n)
	g.pf("// caller owns the allocation — generated load code allocates nothing.\n")
	g.pf("inline int64_t %sLoadMeasure( const uint8_t * wire, int64_t wire_bytes )\n{\n", n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReader r( wire, wire_bytes, &ignored );\n")
	g.pf("    int64_t below = %sLoadMeasureBody( r, 1 );\n", n)
	g.pf("    if ( below < 0 ) { below = 0; }\n")
	g.pf("    return TableAlignUp64( (int64_t) sizeof( %s ) ) + below;\n}\n\n", n)
	g.pf("// %sLoad: decode the tolerant wire into the caller's exact-sized region and\n", n)
	g.pf("// return the root. Partial results are kept, as everywhere on this wire —\n")
	g.pf("// the report says what happened. NULL means the CALLER's buffer was wrong.\n")
	g.pf("inline const %s * %sLoad( uint8_t * region, int64_t region_bytes, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    if ( region == NULL || region_bytes < (int64_t) sizeof( %s ) ) { out->malformed = true; return NULL; }\n", n)
	g.pf("    if ( ( ( (uintptr_t) region ) & ( kTableAlign - 1 ) ) != 0 ) { out->malformed = true; return NULL; }\n")
	g.pf("    memset( region, 0, (size_t) region_bytes );\n")
	g.pf("    TableRegionSink sink;\n")
	g.pf("    sink.base = region;\n")
	g.pf("    sink.capacity = region_bytes;\n")
	g.pf("    sink.used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    %s * root = new ( region ) %s; // lifetime only: LoadBody's first act is %sReset\n", n, n, n)
	g.pf("    TableReader r( wire, wire_bytes, out );\n")
	g.pf("    %sLoadBody( r, sink, *root, 1 );\n", n)
	g.pf("    return root;\n}\n\n")
	g.pf("// %sLoadBuilder: the TOOL's path — the same tolerant decode into a fresh\n", n)
	g.pf("// builder, so loaded data can be edited and locked again.\n")
	g.pf("inline bool %sLoadBuilder( %sBuilder & builder, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    %s * root = builder.GetRoot();\n", n)
	g.pf("    if ( root == NULL ) { out->malformed = true; return false; }\n")
	g.pf("    TableReader r( wire, wire_bytes, out );\n")
	g.pf("    return %sLoadBody( r, builder.main, *root, 1 );\n}\n\n", n)
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
