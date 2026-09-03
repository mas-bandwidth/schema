// The per-table VARIABLE-LENGTH surface in C (docs/SPEC-TABLES.md §2, §6, §9):
// the allocation accessors, the pack walkers behind Lock, the wire sizing
// pre-pass behind Load, and the builder itself.
//
// Nothing here is emitted for a FIXED-SIZE table. A table whose by-value
// closure holds no pointer gets its struct and its three free functions —
// <Name>Measure, <Name>Save, <Name>Load — and not one byte more, which is the
// point of deriving the mode instead of declaring it.
//
// C HAS NO OVERLOADING and no templates, so the two resolution contexts C++
// distinguishes by type are ONE struct here — TableCtx, whose NULL arena means
// a region — and the two allocation sinks are ONE struct, TableSink, whose two
// nullable members say which. The walks take those by pointer where C++ takes
// a template parameter; nothing about the forms differs.
package ctable

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

// needsWalkers returns true when a member needs the pointer-graph walkers:
// every variable member, plus every table some pointer targets (a pointed-at
// table may itself be pointer-free, and still needs to be allocated, packed,
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
	if vars := g.varMembers(members); len(vars) > 0 {
		g.pf("/* ---- pointer targets: allocation and resolution (docs/SPEC-TABLES.md §2) ----\n")
		g.pf("\n")
		g.pf("   A reference resolves differently in the two forms, and the CONTEXT says\n")
		g.pf("   which: with an arena it is an offset; with none it is a self-relative\n")
		g.pf("   delta, so the deref below is one add and needs no base pointer. */\n\n")
		for _, st := range members {
			if !g.targets[st.Name] {
				continue
			}
			g.emitPointerTargetSurface(st)
		}
	}
	g.pf("/* ---- codecs: measure/save/load per closure member ---- */\n\n")
	for _, st := range members {
		if g.isVar(st.Name) {
			g.pf("static SCHEMA_UNUSED int64_t %sMeasureBody( const TableCtx * ctx, const %s * value, int32_t depth );\n", st.Name, st.Name)
			g.pf("static SCHEMA_UNUSED int %sSaveBody( const TableCtx * ctx, TableWriter * w, const %s * value, int32_t depth );\n", st.Name, st.Name)
			g.pf("static SCHEMA_UNUSED int %sLoadBody( TableReader * r, TableSink * sink, %s * value, int32_t depth );\n", st.Name, st.Name)
			continue
		}
		g.pf("static SCHEMA_UNUSED int64_t %sMeasure( const %s * value );\n", st.Name, st.Name)
		g.pf("static SCHEMA_UNUSED %s int %sSaveBody( TableWriter * w, const %s * value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
		g.pf("static SCHEMA_UNUSED %s int %sLoadBody( TableReader * r, %s * value );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
	}
	g.pf("\n")
	if vars := g.varMembers(members); len(vars) > 0 {
		g.pf("/* ---- pointer-graph walkers: pack (Lock), size (Load) ---- */\n\n")
		for _, st := range vars {
			g.pf("static SCHEMA_UNUSED int64_t %sPackMeasure( const TableCtx * ctx, const %s * value, int32_t depth );\n", st.Name, st.Name)
			g.pf("static SCHEMA_UNUSED int %sPack( const TableCtx * ctx, const %s * src, %s * dst, uint8_t * base, int64_t capacity, int64_t * used, int32_t depth );\n", st.Name, st.Name, st.Name)
			g.pf("static SCHEMA_UNUSED int64_t %sLoadMeasureBody( TableReader * r, int32_t depth );\n", st.Name)
		}
		g.pf("\n")
	}
}

// emitPointerTargetSurface emits one pointed-at table's resolution and
// allocation entries.
func (g *tableGen) emitPointerTargetSurface(st *ir.Struct) {
	n := st.Name
	g.pf("/* %s is a pointer target. %sAt resolves a slot in EITHER form: a NULL ctx,\n", n, n)
	g.pf("   or one whose arena is NULL, is a REGION, where the slot holds a signed\n")
	g.pf("   self-relative delta and the deref is one add. */\n")
	g.pf("static SCHEMA_UNUSED %s * %sAt( const TableCtx * ctx, const TableRef * ref )\n{\n", n, n)
	g.pf("    if ( ref->value == 0 ) { return NULL; }\n")
	g.pf("    if ( ctx != NULL && ctx->arena != NULL )\n")
	g.pf("    {\n        return (%s *) TableArenaAt( ctx->arena, (uint32_t) ref->value );\n    }\n", n)
	g.pf("    return (%s *) (void *) ( (uint8_t *) (void *) ref + ref->value );\n}\n", n)
	g.pf("/* bump one %s into the sink the caller gave: a region sink places it in the\n", n)
	g.pf("   caller's exact region and the slot comes out self-relative; a worker\n")
	g.pf("   allocates it in the arena and the slot holds the arena offset. */\n")
	g.pf("static SCHEMA_UNUSED %s * %sEmplace( TableSink * sink, TableRef * slot )\n{\n", n, n)
	g.pf("    if ( sink == NULL ) { return NULL; }\n")
	g.pf("    if ( sink->region != NULL )\n    {\n")
	g.pf("        int64_t at = TableAlignUp64( sink->region->used );\n")
	g.pf("        %s * node;\n", n)
	g.pf("        if ( at + (int64_t) sizeof( %s ) > sink->region->capacity ) { return NULL; }\n", n)
	g.pf("        sink->region->used = at + TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("        node = (%s *) (void *) ( sink->region->base + at );\n", n)
	g.pf("        %sReset( node );\n", n)
	g.pf("        slot->value = (int64_t) ( ( sink->region->base + at ) - (uint8_t *) (void *) slot );\n")
	g.pf("        return node;\n    }\n")
	g.pf("    if ( sink->worker != NULL )\n    {\n")
	g.pf("        uint32_t at = TableWorkerBump( sink->worker, (uint32_t) sizeof( %s ) );\n", n)
	g.pf("        %s * node;\n", n)
	g.pf("        if ( at == kTableAllocFailed ) { return NULL; }\n")
	g.pf("        node = (%s *) (void *) TableArenaAt( sink->worker->arena, at );\n", n)
	g.pf("        %sReset( node );\n", n)
	g.pf("        slot->value = (int64_t) at;\n")
	g.pf("        return node;\n    }\n")
	g.pf("    return NULL;\n}\n\n")
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
	g.pf("/* %sPackMeasure: the packed region bytes of everything %s POINTS AT.\n", st.Name, st.Name)
	g.pf("   Aliasing is not preserved: two pointers to one node pack as two nodes,\n")
	g.pf("   exactly as they ride the wire as two bodies (docs/SPEC-TABLES.md §3). */\n")
	g.pf("static SCHEMA_UNUSED int64_t %sPackMeasure( const TableCtx * ctx, const %s * value, int32_t depth )\n{\n", st.Name, st.Name)
	g.pf("    int64_t bytes = 0;\n")
	g.pf("    if ( depth > kTableMaxDepth ) { return -1; } /* a data cycle, or a chain past the cap */\n")
	if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
		g.pf("    (void) ctx; (void) value; /* no pointers below this node */\n")
	}
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee = %sAt( ctx, &value->%s ); /* %s */\n", t, t, f.Name, f.Name)
		g.pf("        if ( pointee != NULL )\n        {\n")
		g.pf("            int64_t inner = %sPackMeasure( ctx, pointee, depth + 1 );\n", t)
		g.pf("            if ( inner < 0 ) { return -1; }\n")
		g.pf("            bytes += TableAlignUp64( (int64_t) sizeof( %s ) ) + inner;\n", t)
		g.pf("        }\n    }\n")
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalk(f, func(expr string) {
			g.pf("        int64_t inner = %sPackMeasure( ctx, %s, depth );\n", f.Type.Name, expr)
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
	bound := fmt.Sprintf("%d", f.ArrayBound)
	if f.KeyEnum != "" {
		bound = enumMaxConst(f.KeyEnum)
	}
	switch {
	case f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("    { /* %s (nested by value) */\n", f.Name)
		body("&value->" + f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    { int32_t i; for ( i = 0; i < value->%s_count && i < %d; i++ ) /* %s */\n    {\n", f.Name, f.ArrayBound, f.Name)
		body(fmt.Sprintf("&value->%s[i]", f.Name))
		g.pf("    } }\n")
	default:
		g.pf("    { int32_t i; for ( i = 0; i < (int32_t) ( %s ); i++ ) /* %s */\n    {\n", bound, f.Name)
		body(fmt.Sprintf("&value->%s[i]", f.Name))
		g.pf("    } }\n")
	}
}

// emitPack copies one node into the packed region and lays its pointees out
// depth-first behind it, rewriting every reference into the region's
// self-relative encoding.
func (g *tableGen) emitPack(st *ir.Struct) {
	g.pf("/* %sPack: copy src into dst (already placed), then lay every pointee out\n", st.Name)
	g.pf("   depth-first behind it, in FIELD ORDER, by bump allocation.\n")
	g.pf("\n")
	g.pf("   The pre-order is what makes a region simple to reason about: a child\n")
	g.pf("   always lands after the slot naming it, so region deltas are strictly\n")
	g.pf("   positive and a packed region cannot contain a cycle. */\n")
	g.pf("static SCHEMA_UNUSED int %sPack( const TableCtx * ctx, const %s * src, %s * dst, uint8_t * base, int64_t capacity, int64_t * used, int32_t depth )\n{\n", st.Name, st.Name, st.Name)
	g.pf("    if ( depth > kTableMaxDepth ) { return 0; }\n")
	g.pf("    memcpy( (void *) dst, (const void *) src, sizeof( %s ) ); /* a plain struct, by construction */\n", st.Name)
	if len(pointerFields(st)) == 0 && len(g.byValueVariableFields(st)) == 0 {
		g.pf("    (void) ctx; (void) base; (void) capacity; (void) used;\n")
	}
	for _, f := range pointerFields(st) {
		t := f.Type.Name
		g.pf("    {\n")
		g.pf("        const %s * pointee;\n", t)
		g.pf("        dst->%s.value = 0; /* %s */\n", f.Name, f.Name)
		g.pf("        pointee = %sAt( ctx, &src->%s );\n", t, f.Name)
		g.pf("        if ( pointee != NULL )\n        {\n")
		g.pf("            int64_t at = TableAlignUp64( *used );\n")
		g.pf("            %s * child;\n", t)
		g.pf("            if ( at + (int64_t) sizeof( %s ) > capacity ) { return 0; }\n", t)
		g.pf("            *used = at + TableAlignUp64( (int64_t) sizeof( %s ) );\n", t)
		g.pf("            child = (%s *) (void *) ( base + at );\n", t)
		g.pf("            dst->%s.value = (int64_t) ( ( base + at ) - (const uint8_t *) (const void *) &dst->%s );\n", f.Name, f.Name)
		g.pf("            if ( !%sPack( ctx, pointee, child, base, capacity, used, depth + 1 ) ) { return 0; }\n", t)
		g.pf("        }\n    }\n")
	}
	for _, f := range g.byValueVariableFields(st) {
		g.emitVariableByValueWalkPack(f)
	}
	g.pf("    return 1;\n}\n\n")
}

func (g *tableGen) emitVariableByValueWalkPack(f *ir.Field) {
	t := f.Type.Name
	call := func(srcExpr, dstExpr string) {
		g.pf("        if ( !%sPack( ctx, %s, %s, base, capacity, used, depth ) ) { return 0; }\n", t, srcExpr, dstExpr)
	}
	bound := fmt.Sprintf("%d", f.ArrayBound)
	if f.KeyEnum != "" {
		bound = enumMaxConst(f.KeyEnum)
	}
	switch {
	case f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("    { /* %s (nested by value) */\n", f.Name)
		call("&src->"+f.Name, "&dst->"+f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    { int32_t i; for ( i = 0; i < src->%s_count && i < %d; i++ ) /* %s */\n    {\n", f.Name, f.ArrayBound, f.Name)
		call(fmt.Sprintf("&src->%s[i]", f.Name), fmt.Sprintf("&dst->%s[i]", f.Name))
		g.pf("    } }\n")
	default:
		g.pf("    { int32_t i; for ( i = 0; i < (int32_t) ( %s ); i++ ) /* %s */\n    {\n", bound, f.Name)
		call(fmt.Sprintf("&src->%s[i]", f.Name), fmt.Sprintf("&dst->%s[i]", f.Name))
		g.pf("    } }\n")
	}
}

// emitLoadMeasureBody emits the wire sizing pre-pass: how many region bytes
// the nodes BELOW this body will need. Skip-based — it reads framing and
// nothing else, which is what lets a caller own the allocation (§6.5).
func (g *tableGen) emitLoadMeasureBody(st *ir.Struct) {
	ptrs := pointerFields(st)
	nested := g.byValueVariableFields(st)
	g.pf("/* %sLoadMeasureBody: region bytes for the nodes under this wire body.\n", st.Name)
	g.pf("   Framing only — no field value is decoded, so a caller can size its\n")
	g.pf("   buffer before a single byte is placed. */\n")
	g.pf("static SCHEMA_UNUSED int64_t %sLoadMeasureBody( TableReader * r, int32_t depth )\n{\n", st.Name)
	g.pf("    int64_t bytes = 0;\n")
	if len(ptrs) == 0 && len(nested) == 0 {
		g.pf("    (void) r; (void) depth; /* nothing below this body allocates */\n")
		g.pf("    return bytes;\n}\n\n")
		return
	}
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint16_t field_id;\n        uint8_t kind;\n")
	g.pf("        if ( !TableReaderHas( r, 2 ) ) { return bytes; }\n")
	g.pf("        field_id = TableReaderGet16( r );\n")
	g.pf("        if ( field_id == 0 ) { return bytes; }\n")
	g.pf("        if ( !TableReaderHas( r, 1 ) ) { return bytes; }\n")
	g.pf("        kind = TableReaderGet8( r );\n")
	g.pf("        switch ( field_id )\n        {\n")
	for _, f := range ptrs {
		t := f.Type.Name
		g.pf("            case 0x%04x: /* %s (*%s) */\n            {\n", ir.TableFieldId(f), f.Name, t)
		g.pf("                uint32_t body_len;\n")
		g.pf("                if ( kind != %d ) { if ( !TableReaderSkip( r, kind ) ) { return bytes; } break; }\n", tkTable)
		g.pf("                if ( !TableReaderHas( r, 4 ) ) { return bytes; }\n")
		g.pf("                body_len = TableReaderGet32( r );\n")
		g.pf("                if ( !TableReaderHas( r, body_len ) ) { return bytes; }\n")
		g.pf("                if ( depth < kTableMaxDepth )\n                {\n")
		g.pf("                    TableReader sub = TableReaderMake( r->buffer + r->offset, body_len, r->report );\n")
		g.pf("                    bytes += TableAlignUp64( (int64_t) sizeof( %s ) );\n", t)
		g.pf("                    bytes += %sLoadMeasureBody( &sub, depth + 1 );\n", t)
		g.pf("                }\n")
		g.pf("                r->offset += body_len;\n")
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
		bound := fmt.Sprintf("%d", f.ArrayBound)
		if f.KeyEnum != "" {
			bound = enumMaxConst(f.KeyEnum)
		}
		g.pf("            case 0x%04x: /* %s (%s nested by value) */\n            {\n", ir.TableFieldId(f), f.Name, t)
		g.pf("                uint32_t body_len;\n                int64_t body_end;\n")
		g.pf("                if ( kind != %d ) { if ( !TableReaderSkip( r, kind ) ) { return bytes; } break; }\n", wireKind)
		g.pf("                if ( !TableReaderHas( r, 4 ) ) { return bytes; }\n")
		g.pf("                body_len = TableReaderGet32( r );\n")
		g.pf("                if ( !TableReaderHas( r, body_len ) ) { return bytes; }\n")
		g.pf("                body_end = r->offset + body_len;\n")
		if f.Array == ir.ArrayNone && f.KeyEnum == "" {
			g.pf("                {\n")
			g.pf("                    TableReader sub = TableReaderMake( r->buffer + r->offset, body_len, r->report );\n")
			g.pf("                    bytes += %sLoadMeasureBody( &sub, depth );\n", t)
			g.pf("                }\n")
		} else {
			g.pf("                if ( body_len >= 5 )\n                {\n")
			g.pf("                    uint32_t count;\n                    TableReader elems;\n                    uint32_t i;\n")
			g.pf("                    TableReaderGet8( r ); /* element kind */\n")
			g.pf("                    count = TableReaderGet32( r );\n")
			g.pf("                    elems = TableReaderMake( r->buffer + r->offset, body_end - r->offset, r->report );\n")
			g.pf("                    for ( i = 0; i < count && i < (uint32_t) ( %s ); i++ )\n                    {\n", bound)
			g.pf("                        uint32_t elem_len;\n                        TableReader elem;\n")
			if f.KeyEnum != "" {
				// an ENUM-KEYED array puts the slot's variant id before the
				// element's length (docs/SPEC-TABLES.md §3.2). The pre-pass reads
				// framing only, so it skips the key without naming it — but it
				// must skip it, or every element length after the first is read
				// out of a key's bytes
				g.pf("                        if ( !TableReaderHas( &elems, 2 ) ) { break; }\n")
				g.pf("                        TableReaderGet16( &elems ); /* the slot's variant id */\n")
			}
			g.pf("                        if ( !TableReaderHas( &elems, 4 ) ) { break; }\n")
			g.pf("                        elem_len = TableReaderGet32( &elems );\n")
			g.pf("                        if ( !TableReaderHas( &elems, elem_len ) ) { break; }\n")
			g.pf("                        elem = TableReaderMake( elems.buffer + elems.offset, elem_len, r->report );\n")
			g.pf("                        bytes += %sLoadMeasureBody( &elem, depth );\n", t)
			g.pf("                        elems.offset += elem_len;\n")
			g.pf("                    }\n")
			g.pf("                }\n")
		}
		g.pf("                r->offset = body_end;\n")
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n            {\n")
	g.pf("                if ( !TableReaderSkip( r, kind ) ) { return bytes; }\n")
	g.pf("                break;\n            }\n")
	g.pf("        }\n    }\n}\n\n")
}

// ---- the builder and the public surface ----

func (g *tableGen) emitBuilderAndPublicSurface(st *ir.Struct) {
	n := st.Name
	g.pf("/* ---- %s: the variable-length life (docs/SPEC-TABLES.md §2, §6, §9) ----\n", n)
	g.pf("\n")
	g.pf("   MUTABLE: %sBuilder — allocate nodes, wire them together, then Lock.\n", n)
	g.pf("   CONST:   one packed region, root at its base. Lock produces it and Load\n")
	g.pf("            produces it, so a locked structure and a loaded one are the\n")
	g.pf("            SAME representation with one view API. There is no unlock:\n")
	g.pf("            re-editing means loading the const form into a fresh builder.\n")
	g.pf("   %s is never held by value — a file-format-scale structure is a region\n", n)
	g.pf("   and a root pointer, not a struct you copy.\n")
	g.pf("\n")
	g.pf("   THE MEMBERS ARE PUBLIC, which is C's answer to C++'s accessors: `region`\n")
	g.pf("   is the packed const form and NULL until Lock succeeds, `region_bytes` is\n")
	g.pf("   its length, and `arena.locked` is the one-way flag. Read them; the four\n")
	g.pf("   functions below are the only ones that write them. */\n")
	g.pf("typedef struct %sBuilder\n{\n", n)
	g.pf("    TableArena arena;\n")
	g.pf("    TableWorker main;       /* the calling thread's allocation front */\n")
	g.pf("    TableRef root_ref;\n")
	g.pf("    uint8_t * region;       /* the packed const form, produced by %sBuilderLock */\n", n)
	g.pf("    int64_t region_bytes;\n")
	g.pf("} %sBuilder;\n\n", n)

	g.pf("/* Allocate a node in THIS thread's front: no lock, no atomic per node. One\n")
	g.pf("   worker per thread; allocate on your own, and synchronize your own writes\n")
	g.pf("   to nodes another worker allocated (§6.4). */\n")
	g.pf("static SCHEMA_UNUSED int %sBuilderInit( %sBuilder * builder )\n{\n", n, n)
	g.pf("    uint32_t at;\n")
	g.pf("    TableArenaInit( &builder->arena );\n")
	g.pf("    builder->main = TableWorkerMake( &builder->arena );\n")
	g.pf("    builder->root_ref.value = 0;\n")
	g.pf("    builder->region = NULL;\n")
	g.pf("    builder->region_bytes = 0;\n")
	g.pf("    /* the ROOT is allocated like any node and is not a pointer target, so\n")
	g.pf("       it takes the arena's untyped bump directly rather than an %sEmplace\n", n)
	g.pf("       that only a pointed-at table has */\n")
	g.pf("    at = TableWorkerBump( &builder->main, (uint32_t) sizeof( %s ) );\n", n)
	g.pf("    if ( at == kTableAllocFailed ) { return 0; }\n")
	g.pf("    builder->root_ref.value = (int64_t) at;\n")
	g.pf("    %sReset( (%s *) (void *) TableArenaAt( &builder->arena, at ) );\n", n, n)
	g.pf("    return 1;\n}\n\n")

	g.pf("static SCHEMA_UNUSED void %sBuilderShutdown( %sBuilder * builder )\n{\n", n, n)
	g.pf("    TableArenaShutdown( &builder->arena );\n")
	g.pf("    free( builder->region );\n")
	g.pf("    builder->region = NULL;\n    builder->region_bytes = 0;\n}\n\n")

	g.pf("/* The mutable root, or NULL once the builder is locked. */\n")
	g.pf("static SCHEMA_UNUSED %s * %sBuilderRoot( %sBuilder * builder )\n{\n", n, n, n)
	g.pf("    if ( builder->arena.locked || builder->root_ref.value == 0 ) { return NULL; }\n")
	g.pf("    return (%s *) (void *) TableArenaAt( &builder->arena, (uint32_t) builder->root_ref.value );\n}\n\n", n)

	g.pf("/* Lock is ONE WAY and it is the compaction: the segmented arena becomes one\n")
	g.pf("   exact-packed region with zero slack, references rewritten self-relative,\n")
	g.pf("   and the mutable life released. Single-threaded: call it after the workers\n")
	g.pf("   have joined. The region comes back in builder->region. */\n")
	g.pf("static SCHEMA_UNUSED int %sBuilderLock( %sBuilder * builder )\n{\n", n, n)
	g.pf("    TableCtx ctx;\n    const %s * root;\n    int64_t below, total, used;\n    uint8_t * packed;\n", n)
	g.pf("    if ( builder->arena.locked ) { return builder->region != NULL; }\n")
	g.pf("    if ( builder->root_ref.value == 0 ) { return 0; }\n")
	g.pf("    ctx.arena = &builder->arena;\n")
	g.pf("    root = (const %s *) (void *) TableArenaAt( &builder->arena, (uint32_t) builder->root_ref.value );\n", n)
	g.pf("    below = %sPackMeasure( &ctx, root, 1 );\n", n)
	g.pf("    if ( below < 0 ) { return 0; } /* a data cycle, or a chain past kTableMaxDepth */\n")
	g.pf("    total = TableAlignUp64( (int64_t) sizeof( %s ) ) + below;\n", n)
	g.pf("    packed = (uint8_t *) malloc( (size_t) total ); /* the AUTHORING path may allocate */\n")
	g.pf("    if ( packed == NULL ) { return 0; }\n")
	g.pf("    memset( packed, 0, (size_t) total );\n")
	g.pf("    used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    if ( !%sPack( &ctx, root, (%s *) (void *) packed, packed, total, &used, 1 ) || used != total )\n    {\n", n, n)
	g.pf("        free( packed );\n        return 0;\n    }\n")
	g.pf("    builder->region = packed;\n")
	g.pf("    builder->region_bytes = total;\n")
	g.pf("    builder->arena.locked = 1; /* MONOTONIC: there is no unlock */\n")
	g.pf("    TableArenaShutdown( &builder->arena );\n")
	g.pf("    return 1;\n}\n\n")

	// wire out
	g.pf("/* ---- %s on the wire: the generic, tolerant form (docs/SPEC-TABLES.md §3) ----\n", n)
	g.pf("\n")
	g.pf("   The CONTEXT says which form the root is in: NULL, or a ctx with a NULL\n")
	g.pf("   arena, is a packed REGION — what Lock and Load produce — and a ctx naming\n")
	g.pf("   a builder's arena is the mutable form. C++ spells the two as overloads;\n")
	g.pf("   one parameter says the same thing here. */\n")
	g.pf("static SCHEMA_UNUSED int64_t %sMeasure( const TableCtx * ctx, const %s * root )\n{\n", n, n)
	g.pf("    return root != NULL ? %sMeasureBody( ctx, root, 1 ) : -1;\n}\n\n", n)
	g.pf("static SCHEMA_UNUSED int64_t %sSave( const TableCtx * ctx, const %s * root, uint8_t * buffer, int64_t capacity )\n{\n", n, n)
	g.pf("    TableWriter w;\n")
	g.pf("    if ( root == NULL ) { return -1; }\n")
	g.pf("    w = TableWriterMake( buffer, capacity );\n")
	g.pf("    if ( !%sSaveBody( ctx, &w, root, 1 ) ) { return -1; }\n", n)
	g.pf("    return w.offset; /* == %sMeasure( ctx, root ) */\n}\n\n", n)

	// load
	g.pf("/* %sLoadMeasure: the exact region bytes a wire buffer will need. The\n", n)
	g.pf("   caller owns the allocation — generated load code allocates nothing. */\n")
	g.pf("static SCHEMA_UNUSED int64_t %sLoadMeasure( const uint8_t * wire, int64_t wire_bytes )\n{\n", n)
	g.pf("    TableReport ignored;\n    TableReader r;\n    int64_t below;\n")
	g.pf("    memset( &ignored, 0, sizeof( ignored ) );\n")
	g.pf("    r = TableReaderMake( wire, wire_bytes, &ignored );\n")
	g.pf("    below = %sLoadMeasureBody( &r, 1 );\n", n)
	g.pf("    if ( below < 0 ) { below = 0; }\n")
	g.pf("    return TableAlignUp64( (int64_t) sizeof( %s ) ) + below;\n}\n\n", n)
	g.pf("/* %sLoad: decode the tolerant wire into the caller's exact-sized region and\n", n)
	g.pf("   return the root. Partial results are kept, as everywhere on this wire —\n")
	g.pf("   the report says what happened. NULL means the CALLER's buffer was wrong. */\n")
	g.pf("static SCHEMA_UNUSED const %s * %sLoad( uint8_t * region, int64_t region_bytes, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n    TableReport * out;\n    TableRegionSink region_sink;\n    TableSink sink;\n    %s * root;\n    TableReader r;\n", n)
	g.pf("    memset( &ignored, 0, sizeof( ignored ) );\n")
	g.pf("    out = report != NULL ? report : &ignored;\n")
	g.pf("    if ( region == NULL || region_bytes < (int64_t) sizeof( %s ) ) { out->malformed = 1; return NULL; }\n", n)
	g.pf("    if ( ( ( (uintptr_t) region ) & ( kTableAlign - 1 ) ) != 0 ) { out->malformed = 1; return NULL; }\n")
	g.pf("    memset( region, 0, (size_t) region_bytes );\n")
	g.pf("    region_sink.base = region;\n")
	g.pf("    region_sink.capacity = region_bytes;\n")
	g.pf("    region_sink.used = TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
	g.pf("    sink.region = &region_sink;\n    sink.worker = NULL;\n")
	g.pf("    root = (%s *) (void *) region;\n", n)
	g.pf("    r = TableReaderMake( wire, wire_bytes, out );\n")
	g.pf("    %sLoadBody( &r, &sink, root, 1 );\n", n)
	g.pf("    return root;\n}\n\n")
	g.pf("/* %sLoadBuilder: the TOOL's path — the same tolerant decode into a fresh\n", n)
	g.pf("   builder, so loaded data can be edited and locked again. */\n")
	g.pf("static SCHEMA_UNUSED int %sLoadBuilder( %sBuilder * builder, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n    TableReport * out;\n    %s * root;\n    TableSink sink;\n    TableReader r;\n", n)
	g.pf("    memset( &ignored, 0, sizeof( ignored ) );\n")
	g.pf("    out = report != NULL ? report : &ignored;\n")
	g.pf("    root = %sBuilderRoot( builder );\n", n)
	g.pf("    if ( root == NULL ) { out->malformed = 1; return 0; }\n")
	g.pf("    sink.region = NULL;\n    sink.worker = &builder->main;\n")
	g.pf("    r = TableReaderMake( wire, wire_bytes, out );\n")
	g.pf("    return %sLoadBody( &r, &sink, root, 1 );\n}\n\n", n)
}

// emitRelocatabilityPreamble writes the note above the layout contract, which
// covers both forms.
func (g *tableGen) emitRelocatabilityPreamble() {
	g.pf("/* ---- relocatability: the wire is a pure length-prefixed stream AND the\n")
	g.pf("   decoded storage is pointer-free — every closure type is a plain C struct\n")
	g.pf("   of scalars and arrays, so instances can be memcpy'd, mmap'd, shared\n")
	g.pf("   across processes, and walked through descriptor offsets. C has no way to\n")
	g.pf("   put a pointer, a vtable or a non-trivial member in one of these, which is\n")
	g.pf("   why the property is a fact of the language here rather than an assert.\n")
	if g.anyVariable {
		g.pf("   A pointer FIELD is a TableRef — eight bytes and no address — so the\n")
		g.pf("   property holds in BOTH forms: a fixed-size table is one relocatable\n")
		g.pf("   struct, and a packed region is one relocatable block whose references\n")
		g.pf("   are self-relative and therefore survive a plain memcpy.\n")
	}
	g.pf("   What IS asserted is the layout the build version was taken over. */\n\n")
}

// modeColumn renders the descriptor's derived-mode column, present only in a
// unit that has pointers (the zero-cost gate).
func (g *tableGen) modeColumn(st *ir.Struct) string {
	if !g.anyVariable {
		return ""
	}
	return fmt.Sprintf(", %s", boolC(g.isVar(st.Name)))
}
