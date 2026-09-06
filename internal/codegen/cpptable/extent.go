// The NODE EXTENT's shared runtime (docs/SPEC-TABLES.md §2.8, §2.9, §6.3):
// what a map and an unbounded array have in common once the key and the sort
// are taken out of the map. Both put their arrays in the holder's node extent
// after the record's own storage, both are carved from that extent as the
// node's body decodes, and both make LoadMeasure walk the wire's framing for
// every N at every depth. So the cursor, the framing walkers over the by-value
// edges that hold them, and the one test an unreached slot takes live here,
// emitted into a unit that declares either construct and into no other.
package cpptable

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableExtentRuntime is the extent half of the variable-length runtime. It
// follows the arena runtime it is spelled in terms of and precedes the map
// and list runtimes that are spelled in terms of it.
func tableExtentRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_EXTENT"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- the NODE EXTENT: where a map's entries and a list's elements live (§2.8, §2.9) ----

// What a node's storage answers when the FRAMING ITSELF is refused rather than
// merely unnameable: a count its L cannot carry, or one above the int32 cap
// (docs/SPEC-TABLES.md §6.5). An unnameable type id commands no storage and
// keeps its index. This one makes the whole measure answer -1 with its reason.
static const int64_t kTableNodeRefused = -2;

// TableExtentCarve is a node's extent cursor, PRE-ORDER: a container's whole
// array first, then, element by element in the container's own order, the
// arrays of any list or map an element holds by value. The cursor is the node
// map's, because the generated decoder is threaded with that and not with a
// region.
struct TableExtentCarve
{
    uint8_t * at = NULL;         // the region path: the node's extent, unspent
    int64_t left = 0;
    TableWorker * worker = NULL; // the TOOL's path: the arrays come from the arena
};

// AN UNREACHED SLOT MUST HOLD NO LIST OR MAP WITH ELEMENTS IN IT (§2.8, §2.9,
// §7.6). An empty one takes no bytes, so a record whose extent measures ZERO is
// a record whose every by-value list and map is empty. A measure that REFUSED
// answers non-zero here too, and refusing on it is the same answer one level up.
inline bool TableExtentUnreachedEmpty( int64_t extent ) { return extent == 0; }

// ---- LoadMeasure's framing walk (§6.5) ----
//
// The measure reads no field value: it walks each record's field headers,
// skipping every payload by its framing, to reach each N at every depth. A
// false is a REFUSAL, and it carries its reason (§6.5).
typedef bool ( * TableWireExtentFn )( const uint8_t * body, int64_t length, int64_t & at, const TableIdTable * ids, TableRefuseReason & reason );

// the framing walk over an ARRAY OF TABLES held by value: its elements' own
// lists and maps are part of this node's extent too
inline bool TableWireExtentElements( const uint8_t * body, int64_t length, int64_t & at, TableWireExtentFn inner, const TableIdTable * ids, TableRefuseReason & reason )
{
    TableReport scratch;
    TableReader r( body, length, &scratch, ids );
    if ( length < 2 ) { return true; }
    if ( r.get8() != 13 ) { return true; }
    uint64_t n = 0;
    if ( !r.getleb( n ) ) { return true; }
    for ( uint64_t i = 0; i < n; i++ )
    {
        uint64_t elem = 0;
        if ( !r.getleb( elem ) || !r.room( elem ) ) { return true; }
        if ( !inner( r.buffer + r.offset, (int64_t) elem, at, ids, reason ) ) { return false; }
        r.offset += (int64_t) elem;
    }
    return true;
}

// and over an ENUM-KEYED array, whose triples carry a key REFERENCE before each
// length-prefixed element (docs/SPEC-TABLES.md §3.2)
inline bool TableWireExtentKeyed( const uint8_t * body, int64_t length, int64_t & at, TableWireExtentFn inner, const TableIdTable * ids, TableRefuseReason & reason )
{
    TableReport scratch;
    TableReader r( body, length, &scratch, ids );
    if ( length < 2 ) { return true; }
    if ( r.get8() != 13 ) { return true; }
    uint64_t n = 0;
    if ( !r.getleb( n ) ) { return true; }
    for ( uint64_t i = 0; i < n; i++ )
    {
        uint64_t key = 0;
        if ( !r.getleb( key ) ) { return true; }
        uint64_t elem = 0;
        if ( !r.getleb( elem ) || !r.room( elem ) ) { return true; }
        if ( !inner( r.buffer + r.offset, (int64_t) elem, at, ids, reason ) ) { return false; }
        r.offset += (int64_t) elem;
    }
    return true;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// ---- the NODE EXTENT's emitters (docs/SPEC-TABLES.md §2.8, §2.9, §6.3) ----
//
// A map's entries and a list's elements are BY-VALUE RECORDS INSIDE THE
// HOLDER'S NODE EXTENT, laid after the record's own storage: count x sizeof
// at the element's alignment, zero slack, one array per container reachable
// BY VALUE from the record, which includes one inside a nested table and one
// inside an element or an entry, in depth-first field order. The placement
// is PRE-ORDER and it interleaves lists and maps on ONE rule: a container's
// whole array first, then, element by element in the container's own order,
// the arrays of any list or map that element holds by value.
//
// Two emitters walk that layout and they are ONE walk: the measure advances a
// running offset, and the pack advances the same one and copies. Nothing
// passes between them, which is what makes `used == total` a real check.

// hasExtent reports a member with any list or map reachable by value: the
// members that carry an extent, and the ones whose extent walks are emitted.
func (g *tableGen) hasExtent(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.IsMap() || f.IsList() {
			return true
		}
		// A CONTAINER REACHABLE BY VALUE IS THIS RECORD'S EXTENT, whichever
		// by-value edge reaches it: a nested table, an array of them, an
		// enum-keyed array of them, or a union arm.
		switch g.edgeOf(f) {
		case edgeNested:
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && g.hasExtent(ref) {
				return true
			}
		case edgeArm:
			if g.unionHasExtent(f.Type.Ref.(*ir.Union), map[*ir.Union]bool{}) {
				return true
			}
		}
		// a nested table this walk does not call an edge can still hold a
		// container, because a container makes its holder VARIABLE and every
		// variable nesting is an edge, so there is nothing else to look at
	}
	return false
}

// unionHasExtent reports a union with a container below an arm (§2.6): a
// TABLE ARM held by value that holds a list or a map, or an arm that is
// another union with one, asked one level in. A pointer arm's pointee is its
// own node with its own extent, so it is not this holder's.
func (g *tableGen) unionHasExtent(un *ir.Union, seen map[*ir.Union]bool) bool {
	if seen[un] {
		return false
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if v.Body() && g.hasExtent(v.Ref) {
			return true
		}
		if inner, ok := v.F.Type.Ref.(*ir.Union); ok && g.unionHasExtent(inner, seen) {
			return true
		}
	}
	return false
}

// memberOf resolves one closure member by name.
func memberOf(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// alignOfEntry is the C ABI alignment of one generated entry record, the
// alignment its array is laid at, and the same model §20.3 commits the
// compiler to for every record in the closure.
func alignOfEntry(u *ir.Unit, entry *ir.Struct) int64 {
	if ml := ir.RecordLayout(u, entry); ml != nil && ml.Align > 0 {
		return ml.Align
	}
	return 8
}

// extentVisitor is what one extent emitter does at the two containers and at
// a by-value nesting that holds one. Each takes the storage as the walk
// spells it under the emitter's subjects: the value's path, the pack's twin,
// and the cook's byte address.
type extentVisitor struct {
	mapField  func(f *ir.Field, expr edgeExpr, ind string)
	listField func(f *ir.Field, expr edgeExpr, ind string)
	descend   func(table string, expr edgeExpr, ind string)
}

// emitExtentWalk is the ONE walk every extent emitter takes: the record's
// fields in declaration order, every container at its own position,
// descending each by-value edge in place, a nested table, an array element, a
// union's set arm, and an arm of an arm. A pointer is NOT an edge here, a
// pointee is its own node with its own extent, and neither is a union arm
// that is one, for the same reason. `base` names the subjects the storage is
// spelled under.
func (g *tableGen) emitExtentWalk(st *ir.Struct, base edgeVisitor, v extentVisitor) {
	g.owner = st
	ev := base
	ev.owner = st
	for _, f := range st.Fields {
		if f.IsMap() {
			v.mapField(f, g.fieldExpr(ev, f), "    ")
			continue
		}
		if f.IsList() {
			v.listField(f, g.fieldExpr(ev, f), "    ")
			continue
		}
		switch g.edgeOf(f) {
		case edgeNested:
			ref, _ := f.Type.Ref.(*ir.Struct)
			if ref == nil || !g.hasExtent(ref) {
				continue
			}
			g.emitVariableByValueWalk(f, ev, func(expr edgeExpr) { v.descend(f.Type.Name, expr, "        ") })
			g.emitUnreachedExtentRefusal(f, ref, ev.read)
		case edgeArm:
			if !g.unionHasExtent(f.Type.Ref.(*ir.Union), map[*ir.Union]bool{}) {
				continue
			}
			g.emitVariableUnionWalk(f, g.extentArmVisitor(ev, v))
		}
	}
}

// extentArmVisitor is the edge visitor the extent walk takes through a union's
// arms: only the ARMS that hold a container are descended, because a pointer
// arm and a byte buffer arm reach nodes, not this node's extent.
func (g *tableGen) extentArmVisitor(ev edgeVisitor, v extentVisitor) edgeVisitor {
	armed := ev
	armed.pointer = func(*ir.Field, edgeExpr) {}
	armed.blob = func(*ir.Field, edgeExpr) {}
	armed.descend = func(table string, expr edgeExpr, indent string) {
		if ref := memberOf(g.unit, table); ref != nil && g.hasExtent(ref) {
			v.descend(table, expr, indent)
		}
	}
	return armed
}

// emitListElementExtents emits the walk over a list's elements for the
// containers each holds by value, element by element in index order after the
// whole array (§2.9): a table element through its own extent walk, and a union
// element through its set arm (§2.6). `element` spells element i under the
// emitter's subjects, and `note` is the loop's comment.
func (g *tableGen) emitListElementExtents(f *ir.Field, element edgeExpr, ev edgeVisitor, v extentVisitor, ind, note string) {
	ref, un := listElementStruct(f), listElementUnion(f)
	if (ref == nil || !g.hasExtent(ref)) && (un == nil || !g.unionHasExtent(un, map[*ir.Union]bool{})) {
		return
	}
	g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ )%s\n%s    {\n", ind, note, ind)
	if ref != nil {
		v.descend(ref.Name, element, ind+"        ")
	} else {
		g.emitUnionArmWalk(un, element, g.extentArmVisitor(ev, v), f.Name, ind+"        ")
	}
	g.pf("%s    }\n", ind)
}

// emitExtent emits `<T>ExtentAt`: the running offset every array reachable by
// value from one record takes, in the order the pack lays them, and
// `<T>Extent`, the whole extent from a fresh offset.
func (g *tableGen) emitExtent(st *ir.Struct) {
	g.pf("// %sExtentAt: the node extent %s's lists and maps take, PRE-ORDER, advancing\n", st.Name, st.Name)
	g.pf("// the running offset exactly as %sExtentPack advances it (§2.8, §2.9).\n", st.Name)
	g.pf("template <typename Ctx>\ninline bool %sExtentAt( const Ctx & ctx, const %s & value, int64_t & at )\n{\n", st.Name, st.Name)
	if !g.hasExtent(st) {
		g.pf("    (void) ctx; (void) value; (void) at; // no list or map below this record\n")
		g.pf("    return true;\n}\n\n")
		// a variable member with no extent of its own is still a root Lock and
		// a cook size from, so its whole extent is spelled, and it is zero
		g.pf("template <typename Ctx>\ninline int64_t %sExtent( const Ctx & ctx, const %s & value )\n{\n", st.Name, st.Name)
		g.pf("    (void) ctx; (void) value; // no list or map below this record\n")
		g.pf("    return 0;\n}\n\n")
		return
	}
	ev := edgeVisitor{read: "value"}
	var v extentVisitor
	v = extentVisitor{
		mapField: func(f *ir.Field, expr edgeExpr, ind string) {
			entry := mapEntryOf(f)
			g.pf("%s{\n", ind)
			g.pf("%s    TableMapCursor<%s> cursor = TableMapOrder( ctx, %s );\n", ind, entry.Name, expr.Src)
			g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
			g.pf("%s    at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", ind, alignOfEntry(g.unit, entry)-1, alignOfEntry(g.unit, entry)-1, entry.Name)
			g.pf("%s    at += (int64_t) cursor.count * (int64_t) sizeof( %s ); // the whole array FIRST\n", ind, entry.Name)
			if g.isVar(entry.Name) {
				g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ ) // then, entry by entry in key order\n%s    {\n", ind, ind)
				g.pf("%s        if ( !%sExtentAt( ctx, *cursor[i], at ) ) { TableMapRelease( cursor ); return false; }\n", ind, entry.Name)
				g.pf("%s    }\n", ind)
			}
			g.pf("%s    TableMapRelease( cursor );\n", ind)
			g.pf("%s}\n", ind)
		},
		listField: func(f *ir.Field, expr edgeExpr, ind string) {
			elem := g.listElementType(f)
			g.pf("%s{\n", ind)
			g.pf("%s    TableListCursor<%s> cursor = TableListElements( ctx, %s );\n", ind, elem, expr.Src)
			g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
			g.pf("%s    at = ( at + (int64_t) alignof( %s ) - 1 ) & ~( (int64_t) alignof( %s ) - 1 );\n", ind, elem, elem)
			g.pf("%s    at += (int64_t) cursor.count * (int64_t) sizeof( %s ); // the whole array FIRST\n", ind, elem)
			g.emitListElementExtents(f, edgeExpr{Src: "cursor[i]"}, ev, v, ind, " // then, element by element in index order")
			g.pf("%s}\n", ind)
		},
		descend: func(table string, expr edgeExpr, ind string) {
			g.pf("%sif ( !%sExtentAt( ctx, %s, at ) ) { return false; }\n", ind, table, expr.Src)
		},
	}
	g.emitExtentWalk(st, ev, v)
	g.pf("    return true;\n}\n\n")
	g.pf("// the whole extent of one node, from a fresh offset: what a pack reserves\n")
	g.pf("// for it beside the record's own storage.\n")
	g.pf("template <typename Ctx>\ninline int64_t %sExtent( const Ctx & ctx, const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    int64_t at = 0;\n")
	g.pf("    if ( !%sExtentAt( ctx, value, at ) ) { return -1; }\n", st.Name)
	g.pf("    return at;\n}\n\n")
}

// emitExtentPack emits `<T>ExtentPack`: the same walk, copying each map's
// entries in key order and each list's elements in index order into the
// node's extent and pointing the record's slot at them.
func (g *tableGen) emitExtentPack(st *ir.Struct) {
	g.pf("// %sExtentPack: carve %s's arrays out of the node's extent and copy the\n", st.Name, st.Name)
	g.pf("// entries in ASCENDING key order and the elements in INDEX order, PRE-ORDER,\n")
	g.pf("// advancing the same running offset %sExtentAt advances (§2.8, §2.9).\n", st.Name)
	g.pf("template <typename Ctx>\ninline bool %sExtentPack( const Ctx & ctx, const %s & src, %s & dst, uint8_t * extent, int64_t & at, int64_t capacity )\n{\n", st.Name, st.Name, st.Name)
	if !g.hasExtent(st) {
		g.pf("    (void) ctx; (void) src; (void) dst; (void) extent; (void) at; (void) capacity; // no list or map below this record\n")
		g.pf("    return true;\n}\n\n")
		return
	}
	ev := edgeVisitor{read: "src", write: "dst"}
	var v extentVisitor
	v = extentVisitor{
		mapField: func(f *ir.Field, expr edgeExpr, ind string) {
			entry := mapEntryOf(f)
			slot := expr.Dst
			g.pf("%s{\n", ind)
			g.pf("%s    TableMapCursor<%s> cursor = TableMapOrder( ctx, %s );\n", ind, entry.Name, expr.Src)
			g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
			g.pf("%s    at = ( at + %d ) & ~(int64_t) %d;\n", ind, alignOfEntry(g.unit, entry)-1, alignOfEntry(g.unit, entry)-1)
			g.pf("%s    const int64_t bytes = (int64_t) cursor.count * (int64_t) sizeof( %s );\n", ind, entry.Name)
			g.pf("%s    if ( at + bytes > capacity ) { TableMapRelease( cursor ); return false; }\n", ind)
			g.pf("%s    %s * placed = (%s *) ( extent + at );\n", ind, entry.Name, entry.Name)
			g.pf("%s    at += bytes;\n", ind)
			g.pf("%s    %s.count = cursor.count;\n", ind, slot)
			g.pf("%s    %s.padding = 0;\n", ind, slot)
			g.pf("%s    %s.entries.value = cursor.count > 0 ? (int64_t) ( (uint8_t *) placed - (const uint8_t *) &%s.entries ) : 0;\n", ind, slot, slot)
			g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ )\n%s    {\n", ind, ind)
			g.pf("%s        memcpy( (void *) ( placed + i ), (const void *) cursor[i], sizeof( %s ) ); // trivially copyable, by construction\n", ind, entry.Name)
			g.pf("%s    }\n", ind)
			if g.isVar(entry.Name) {
				g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ )\n%s    {\n", ind, ind)
				g.pf("%s        if ( !%sExtentPack( ctx, *cursor[i], placed[i], extent, at, capacity ) ) { TableMapRelease( cursor ); return false; }\n", ind, entry.Name)
				g.pf("%s    }\n", ind)
			}
			g.pf("%s    TableMapRelease( cursor );\n", ind)
			g.pf("%s}\n", ind)
		},
		listField: func(f *ir.Field, expr edgeExpr, ind string) {
			elem := g.listElementType(f)
			slot := expr.Dst
			g.pf("%s{\n", ind)
			g.pf("%s    TableListCursor<%s> cursor = TableListElements( ctx, %s );\n", ind, elem, expr.Src)
			g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
			g.pf("%s    at = ( at + (int64_t) alignof( %s ) - 1 ) & ~( (int64_t) alignof( %s ) - 1 );\n", ind, elem, elem)
			g.pf("%s    const int64_t bytes = (int64_t) cursor.count * (int64_t) sizeof( %s );\n", ind, elem)
			g.pf("%s    if ( at + bytes > capacity ) { return false; }\n", ind)
			g.pf("%s    %s * placed = (%s *) ( extent + at );\n", ind, elem, elem)
			g.pf("%s    at += bytes;\n", ind)
			g.pf("%s    %s.count = cursor.count;\n", ind, slot)
			g.pf("%s    %s.padding = 0;\n", ind, slot)
			g.pf("%s    %s.elements.value = cursor.count > 0 ? (int64_t) ( (uint8_t *) placed - (const uint8_t *) &%s.elements ) : 0;\n", ind, slot, slot)
			g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ ) // INDEX order, live elements only\n%s    {\n", ind, ind)
			g.pf("%s        memcpy( (void *) ( placed + i ), (const void *) &cursor[i], sizeof( %s ) ); // trivially copyable, by construction\n", ind, elem)
			g.pf("%s    }\n", ind)
			g.emitListElementExtents(f, edgeExpr{Src: "cursor[i]", Dst: "placed[i]"}, ev, v, ind, "")
			g.pf("%s}\n", ind)
		},
		descend: func(table string, expr edgeExpr, ind string) {
			g.pf("%sif ( !%sExtentPack( ctx, %s, %s, extent, at, capacity ) ) { return false; }\n", ind, table, expr.Src, expr.Dst)
		},
	}
	g.emitExtentWalk(st, ev, v)
	g.pf("    return true;\n}\n\n")
}

// emitExtentWalkSurface emits the framing walk and the two extent walks for
// every variable member of a unit that declares a list or a map. They are
// emitted for EVERY such member, because a walk that descends a by-value
// nesting has to be able to name the nested one's.
func (g *tableGen) emitExtentWalkSurface(members []*ir.Struct) {
	if !g.anyExtent {
		return
	}
	// A UNION WITH A CONTAINER BELOW AN ARM has a framing walk of its own
	// (§2.6): one arm header read where a union value sits, on a field, an
	// element of a bounded array and an element of a list alike. It is
	// declared before the members' walks, which call it, and defined after
	// them, because it calls the arm tables' walks, which this file's order
	// puts before their holders.
	unions := g.extentUnionsOf()
	for _, un := range unions {
		g.pf("inline bool %sWireArmExtent( TableReader & r, int64_t & at, TableRefuseReason & reason );\n", un.Name)
		g.pf("inline bool %sWireArmsExtent( const uint8_t * body, int64_t length, int64_t & at, const TableIdTable * ids, TableRefuseReason & reason );\n", un.Name)
	}
	if len(unions) > 0 {
		g.pf("\n")
	}
	for _, st := range g.varMembers(members) {
		g.emitWireExtent(st)
		g.emitExtent(st)
		g.emitExtentPack(st)
	}
	for _, un := range unions {
		g.emitUnionWireExtent(un)
	}
}

// extentUnionsOf lists the unions this file declares that hold a container
// below an arm, in declaration order.
func (g *tableGen) extentUnionsOf() []*ir.Union {
	var out []*ir.Union
	for _, un := range tableUnionsOf(g.file) {
		if g.unionHasExtent(un, map[*ir.Union]bool{}) {
			out = append(out, un)
		}
	}
	return out
}

// emitUnionWireExtent emits `<U>WireArmExtent`, the framing walk over ONE ARM
// HEADER of a union: the arm id, its kind byte and its L, then the set arm's
// own walk over the payload, a table arm's or a nested union's
// (docs/SPEC-TABLES.md §2.6, §6.5). Framing damage EXHAUSTS THE READER, so
// the scan holding it ends there and the load reports the damage. Beside it,
// `<U>WireArmsExtent` is that walk over an ARRAY of the union's values, whose
// body is the element kind, the count and one arm header per element in its
// place, a None element the zero reference alone (§3).
func (g *tableGen) emitUnionWireExtent(un *ir.Union) {
	g.pf("// %sWireArmExtent: the extent one %s value's set arm commands, from the FRAMING alone (§2.6, §6.5).\n", un.Name, un.Name)
	g.pf("inline bool %sWireArmExtent( TableReader & r, int64_t & at, TableRefuseReason & reason )\n{\n", un.Name)
	g.pf("    uint64_t arm_ref = 0;\n")
	g.pf("    if ( !r.getleb( arm_ref ) ) { r.offset = r.size; return true; }\n")
	g.pf("    if ( arm_ref == 0 ) { return true; } // None: the reference is the whole payload\n")
	g.pf("    if ( r.ids == NULL || arm_ref > (uint64_t) r.ids->count ) { r.offset = r.size; return true; }\n")
	g.pf("    const uint64_t arm_id = r.ids->at( arm_ref );\n")
	g.pf("    if ( !r.has( 1 ) ) { r.offset = r.size; return true; }\n")
	g.pf("    r.offset += 1; // the arm's kind byte\n")
	g.pf("    uint64_t arm_len = 0;\n")
	g.pf("    if ( !r.getleb( arm_len ) || !r.room( arm_len ) ) { r.offset = r.size; return true; }\n")
	g.pf("    const uint8_t * arm_body = r.buffer + r.offset;\n")
	g.pf("    r.offset += (int64_t) arm_len;\n")
	g.pf("    switch ( arm_id )\n    {\n")
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		switch {
		case v.Body() && g.hasExtent(v.Ref):
			g.pf("        case 0x%016xull: if ( !%sWireExtent( arm_body, (int64_t) arm_len, at, r.ids, reason ) ) { return false; } break; // %s\n",
				ir.TableWireId(v.WireName()), v.Type, v.Name)
		default:
			inner, isUnion := v.F.Type.Ref.(*ir.Union)
			if !isUnion || !g.unionHasExtent(inner, map[*ir.Union]bool{}) {
				continue
			}
			g.pf("        case 0x%016xull: // %s: an arm that is another union\n        {\n", ir.TableWireId(v.WireName()), v.Name)
			g.pf("            TableReader arm( arm_body, (int64_t) arm_len, r.report, r.ids );\n")
			g.pf("            if ( !%sWireArmExtent( arm, at, reason ) ) { return false; }\n", inner.Name)
			g.pf("            break;\n        }\n")
		}
	}
	g.pf("        default: break; // an arm this reader cannot name reads None\n")
	g.pf("    }\n")
	g.pf("    return true;\n}\n\n")

	g.pf("// %sWireArmsExtent: that walk over an ARRAY of %s values, one arm header per element in its place (§2.6, §3).\n", un.Name, un.Name)
	g.pf("inline bool %sWireArmsExtent( const uint8_t * body, int64_t length, int64_t & at, const TableIdTable * ids, TableRefuseReason & reason )\n{\n", un.Name)
	g.pf("    TableReport scratch;\n")
	g.pf("    TableReader r( body, length, &scratch, ids );\n")
	g.pf("    if ( length < 2 ) { return true; }\n")
	g.pf("    if ( r.get8() != %d ) { return true; } // another element kind: the field reads empty\n", tkUnion)
	g.pf("    uint64_t n = 0;\n")
	g.pf("    if ( !r.getleb( n ) ) { return true; }\n")
	g.pf("    for ( uint64_t i = 0; i < n && r.offset < r.size; i++ )\n    {\n")
	g.pf("        if ( !%sWireArmExtent( r, at, reason ) ) { return false; }\n", un.Name)
	g.pf("    }\n")
	g.pf("    return true;\n}\n\n")
}

// emitNodeBytes emits the bytes ONE NODE takes in a packed region: the
// record's own storage rounded to the arena's alignment, plus the extent its
// lists and maps take (docs/SPEC-TABLES.md §2.8, §2.9, §6.3), the sum rounded
// again so the next node starts aligned. A unit with neither construct emits
// exactly the term it always emitted.
func (g *tableGen) emitNodeBytes(table, expr, ind, onBad string, plain func(term string), extent func(term string)) {
	target := memberOf(g.unit, table)
	if !g.anyExtent || target == nil || !g.hasExtent(target) {
		// a node with no container below it takes exactly the term it always
		// took, so a unit without one emits what it emitted before either
		// construct existed
		plain(fmt.Sprintf("TableAlignUp64( (int64_t) sizeof( %s ) )", table))
		return
	}
	g.pf("%sint64_t node_extent = %sExtent( ctx, %s );\n", ind, table, expr)
	g.pf("%sif ( node_extent < 0 ) { %s }\n", ind, onBad)
	extent(fmt.Sprintf("TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + node_extent )", table))
}

// emitWireExtent emits `<T>WireExtent`: the region bytes one record's lists
// and maps command, read from the wire FRAMING alone at every depth
// (docs/SPEC-TABLES.md §2.8, §2.9, §6.5). False is the refusal, carrying its
// reason, and it is what makes LoadMeasure answer -1.
func (g *tableGen) emitWireExtent(st *ir.Struct) {
	g.pf("// %sWireExtent: the extent %s's lists and maps command, from the FRAMING alone.\n", st.Name, st.Name)
	g.pf("// It reads no field value, so a caller can refuse a number it did not\n")
	g.pf("// expect before one byte is allocated (docs/SPEC-TABLES.md §6.5).\n")
	g.pf("inline bool %sWireExtent( const uint8_t * body, int64_t length, int64_t & at, const TableIdTable * ids, TableRefuseReason & reason )\n{\n", st.Name)
	if !g.hasExtent(st) {
		g.pf("    (void) body; (void) length; (void) at; (void) ids; (void) reason; // no list or map below this record\n")
		g.pf("    return true;\n}\n\n")
		return
	}
	g.pf("    TableReport scratch; // the scan's framing damage is the LOAD's to report\n")
	g.pf("    TableReader r( body, length, &scratch, ids );\n")
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t field_ref = 0;\n")
	g.pf("        if ( !r.getleb( field_ref ) ) { return true; }\n")
	g.pf("        if ( field_ref == 0 ) { return true; }\n")
	g.pf("        if ( ids == NULL || field_ref > (uint64_t) ids->count ) { return true; }\n")
	g.pf("        const uint64_t field_id = ids->at( field_ref );\n")
	g.pf("        if ( !r.has( 1 ) ) { return true; }\n")
	g.pf("        uint8_t field_kind = r.get8();\n")
	g.emitWireExtentCases(st)
	g.pf("        if ( !r.skip( field_kind ) ) { return true; }\n")
	g.pf("    }\n}\n\n")
}

// emitWireExtentCases emits one arm per list field, one per map field and one
// per by-value nesting that holds either, in DECLARATION ORDER, so the framing
// scan advances the running offset in the same order the pack and the load
// carve it.
func (g *tableGen) emitWireExtentCases(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.IsMap() {
			entry := mapEntryOf(f)
			inner := "NULL"
			if g.hasExtent(entry) {
				inner = "&" + entry.Name + "WireExtent"
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s\n        {\n", ir.TableFieldWireId(f), tkArray, f.Name)
			g.pf("            uint64_t map_len = 0;\n")
			g.pf("            if ( !r.getleb( map_len ) || !r.room( map_len ) ) { return true; }\n")
			g.pf("            const uint8_t * map_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) map_len;\n")
			g.pf("            if ( !TableMapWireExtent( map_body, (int64_t) map_len, at, (int64_t) sizeof( %s ), (int64_t) alignof( %s ), %s, ids, reason ) ) { return false; }\n",
				entry.Name, entry.Name, inner)
			g.pf("            continue;\n        }\n")
			continue
		}
		if f.IsList() {
			elem := g.listElementType(f)
			inner := "NULL"
			if ref := listElementStruct(f); ref != nil && g.hasExtent(ref) {
				inner = "&" + ref.Name + "WireExtent"
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: an unbounded array\n        {\n", ir.TableFieldWireId(f), tkArray, f.Name)
			g.pf("            uint64_t list_len = 0;\n")
			g.pf("            if ( !r.getleb( list_len ) || !r.room( list_len ) ) { return true; }\n")
			g.pf("            const uint8_t * list_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) list_len;\n")
			g.pf("            if ( !TableListWireExtent( list_body, (int64_t) list_len, at, (int64_t) sizeof( %s ), (int64_t) alignof( %s ), %d, %d, %s, ids, reason ) ) { return false; }\n",
				elem, elem, listElementWireKind(f), listElementFloor(f), inner)
			if un := listElementUnion(f); un != nil && g.unionHasExtent(un, map[*ir.Union]bool{}) {
				// the elements' arms, after the whole array: one arm header
				// per element, read over the same body (§2.6, §2.9)
				g.pf("            if ( !%sWireArmsExtent( list_body, (int64_t) list_len, at, ids, reason ) ) { return false; }\n", un.Name)
			}
			g.pf("            continue;\n        }\n")
			continue
		}
		switch g.edgeOf(f) {
		case edgeNested:
			ref, _ := f.Type.Ref.(*ir.Struct)
			if ref == nil || !g.hasExtent(ref) {
				continue
			}
			// a nested table's arrays are part of THIS node's extent, so its
			// own scan runs over the nested body at the running offset
			kind, walk := tkTable, ""
			switch {
			case f.KeyEnum != "":
				kind, walk = tkKeyed, "TableWireExtentKeyed"
			case f.Array != ir.ArrayNone:
				kind, walk = tkArray, "TableWireExtentElements"
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: a nesting that holds a list or a map\n        {\n", ir.TableFieldWireId(f), kind, f.Name)
			g.pf("            uint64_t nested_len = 0;\n")
			g.pf("            if ( !r.getleb( nested_len ) || !r.room( nested_len ) ) { return true; }\n")
			g.pf("            const uint8_t * nested_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) nested_len;\n")
			if walk == "" {
				g.pf("            if ( !%sWireExtent( nested_body, (int64_t) nested_len, at, ids, reason ) ) { return false; }\n", f.Type.Name)
			} else {
				g.pf("            if ( !%s( nested_body, (int64_t) nested_len, at, &%sWireExtent, ids, reason ) ) { return false; }\n", walk, f.Type.Name)
			}
			g.pf("            continue;\n        }\n")
		case edgeArm:
			un := f.Type.Ref.(*ir.Union)
			if !g.unionHasExtent(un, map[*ir.Union]bool{}) {
				continue
			}
			// THE FIELD'S ACTUAL SHAPE decides the framing: a union field is one
			// arm header, and an array of unions is a kind 14 body of them
			if f.Array == ir.ArrayNone {
				g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: a union arm that holds a list or a map\n        {\n", ir.TableFieldWireId(f), tkUnion, f.Name)
				g.pf("            if ( !%sWireArmExtent( r, at, reason ) ) { return false; }\n", un.Name)
				g.pf("            continue;\n        }\n")
				continue
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: an array of unions whose arms hold a list or a map\n        {\n", ir.TableFieldWireId(f), tkArray, f.Name)
			g.pf("            uint64_t arms_len = 0;\n")
			g.pf("            if ( !r.getleb( arms_len ) || !r.room( arms_len ) ) { return true; }\n")
			g.pf("            const uint8_t * arms_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) arms_len;\n")
			g.pf("            if ( !%sWireArmsExtent( arms_body, (int64_t) arms_len, at, ids, reason ) ) { return false; }\n", un.Name)
			g.pf("            continue;\n        }\n")
		}
	}
}

// emitRootDataBytes emits a load's DATA term for the root itself: its record,
// plus the extent its own lists and maps take, read from the wire framing
// (§2.8, §2.9, §6.5). `reason` is the refusal's carrier, declared by the
// caller where a unit has an extent.
func (g *tableGen) emitRootDataBytes(st *ir.Struct, ind, onBad string) {
	if !g.anyExtent {
		g.pf("%sint64_t data = TableAlignUp64( (int64_t) sizeof( %s ) );\n", ind, st.Name)
		return
	}
	g.pf("%sint64_t root_extent = 0;\n", ind)
	g.pf("%sif ( !%sWireExtent( wire, wire_bytes, root_extent, &ids_table, reason ) ) { %s }\n", ind, st.Name, onBad)
	g.pf("%sint64_t data = TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent );\n", ind, st.Name)
}

// ---- the COOK's write side at the extent (docs/SPEC-TABLES.md §2.8, §2.9, §7.6) ----
//
// A cook is a region written verbatim, so a cooked map is its SORTED entry
// array and a cooked list its element array in INDEX order, where the cook
// put them: the node's extent, laid after the record's own storage by the
// same PRE-ORDER rule the pack lays it by. A map's Find is then a binary
// search over the mapped bytes and a list's indexing one multiply, in place,
// with nothing to parse.

// cookExtentSignature is one record's extent writer.
func (g *tableGen) cookExtentSignature(st *ir.Struct) string {
	return fmt.Sprintf("template <typename Ctx> inline bool %sCookExtent( const Ctx & ctx, const TableCookRegion & region, uint8_t * extent, int64_t & at, uint8_t * record, const %s & value, TableByteOrder order )", st.Name, st.Name)
}

// emitCookExtent emits one record's extent writer: every list and map
// reachable by value, PRE-ORDER, each element or entry through its own cook
// writer.
func (g *tableGen) emitCookExtent(st *ir.Struct) {
	g.pf("// %sCookExtent: %s's arrays into the node's extent, PRE-ORDER, a map's entries\n", st.Name, st.Name)
	g.pf("// in ASCENDING key order and a list's elements in INDEX order, each through its\n")
	g.pf("// own cook writer (§2.8, §2.9, §7.6).\n")
	g.pf("%s\n{\n", g.cookExtentSignature(st))
	if !g.hasExtent(st) {
		g.pf("    (void) ctx; (void) region; (void) extent; (void) at; (void) record; (void) value; (void) order;\n")
		g.pf("    return true; // no list or map below this record\n}\n\n")
		return
	}
	usesRegion := false
	for _, f := range st.Fields {
		if f.IsList() && listElementIsPointer(f) {
			usesRegion = true
		}
	}
	if !usesRegion {
		g.pf("    (void) region; // a table element's and an entry's references resolve through their own bodies\n")
	}
	// THE SAME WALK THE PACK TAKES, with the record's byte address beside the
	// value: a container's slot is written at the field's offset, and a
	// nested record, an element or a set arm is written at its own bytes
	ev := edgeVisitor{read: "value", bytes: "record"}
	var v extentVisitor
	v = extentVisitor{
		mapField: func(f *ir.Field, expr edgeExpr, ind string) {
			entry := mapEntryOf(f)
			el := ir.RecordLayout(g.unit, entry)
			g.pf("%s{ // %s\n", ind, f.Name)
			g.pf("%s    TableMapCursor<%s> cursor = TableMapOrder( ctx, %s );\n", ind, entry.Name, expr.Src)
			g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
			g.pf("%s    at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", ind, el.Align-1, el.Align-1, entry.Name)
			g.pf("%s    uint8_t * array = extent + at;\n", ind)
			g.pf("%s    at += (int64_t) cursor.count * %d; // the whole array FIRST\n", ind, el.Size)
			g.pf("%s    // the SIXTEEN BYTES of the slot: the self-relative delta, then the count\n", ind)
			g.pf("%s    table_cook_put( %s, cursor.count > 0 ? (uint64_t) (int64_t) ( array - ( %s ) ) : 0, 8, order );\n", ind, expr.addr(0), expr.addr(0))
			g.pf("%s    table_cook_put( %s, (uint64_t) (uint32_t) cursor.count, 4, order );\n", ind, expr.addr(8))
			g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ )\n%s    {\n", ind, ind)
			g.pf("%s        %s\n", ind, g.cookBodyCall(entry, fmt.Sprintf("array + i * %d", el.Size), "*cursor[i]"))
			g.pf("%s    }\n", ind)
			if g.hasExtent(entry) {
				g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ ) // then, entry by entry in key order\n%s    {\n", ind, ind)
				g.pf("%s        if ( !%sCookExtent( ctx, region, extent, at, array + i * %d, *cursor[i], order ) ) { TableMapRelease( cursor ); return false; }\n", ind, entry.Name, el.Size)
				g.pf("%s    }\n", ind)
			}
			g.pf("%s    TableMapRelease( cursor );\n%s}\n", ind, ind)
		},
		listField: func(f *ir.Field, expr edgeExpr, ind string) {
			elem := g.listElementType(f)
			size, align := ir.ListElementLayout(g.unit, f)
			g.pf("%s{ // %s: an unbounded array\n", ind, f.Name)
			g.pf("%s    TableListCursor<%s> cursor = TableListElements( ctx, %s );\n", ind, elem, expr.Src)
			g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
			g.pf("%s    at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", ind, align-1, align-1, elem)
			g.pf("%s    uint8_t * array = extent + at;\n", ind)
			g.pf("%s    at += (int64_t) cursor.count * %d; // the whole array FIRST\n", ind, size)
			g.pf("%s    // the SIXTEEN BYTES of the slot: the self-relative delta, then the count\n", ind)
			g.pf("%s    table_cook_put( %s, cursor.count > 0 ? (uint64_t) (int64_t) ( array - ( %s ) ) : 0, 8, order );\n", ind, expr.addr(0), expr.addr(0))
			g.pf("%s    table_cook_put( %s, (uint64_t) (uint32_t) cursor.count, 4, order );\n", ind, expr.addr(8))
			g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ ) // INDEX order, live elements only\n%s    {\n", ind, ind)
			if listElementIsPointer(f) {
				// the self-relative delta of §6.3 to the node the numbering reached,
				// or a refusal for one it did not, exactly as a pointer field's slot
				g.pf("%s        if ( !table_cook_ref( region, array + i * %d, (const void *) %sAt( ctx, cursor[i] ), order ) ) { return false; }\n", ind, size, f.Type.Name)
			} else {
				g.emitCookWriteElement(f, fmt.Sprintf("array + i * %d", size), "cursor[i]", ind+"        ", "_"+f.Name)
			}
			g.pf("%s    }\n", ind)
			element := edgeExpr{Src: "cursor[i]", At: fmt.Sprintf("array + i * %d", size)}
			g.emitListElementExtents(f, element, ev, v, ind, " // then, element by element in index order")
			g.pf("%s}\n", ind)
		},
		descend: func(table string, expr edgeExpr, ind string) {
			g.pf("%sif ( !%sCookExtent( ctx, region, extent, at, %s, %s, order ) ) { return false; }\n", ind, table, expr.addr(0), expr.Src)
		},
	}
	g.emitExtentWalk(st, ev, v)
	g.pf("    return true;\n}\n\n")
}

// emitCookNode emits `<T>CookNode`: one NODE's record and then its own extent.
// A nested record's writer is the body alone, because a nesting's arrays are
// part of the HOLDER's extent and this walk already reached them.
//
// THE EXTENT WRITTEN IS THE EXTENT MEASURED: the layout sized the node from
// <T>Extent, and the writer's running offset is held to that same number
// before any header is written, so a walk that laid one array short refuses
// rather than reporting a cook (§7.6).
func (g *tableGen) emitCookNode(st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	record := cookAlignUp(ml.Size, ir.RegionAlignFloor)
	g.pf("// %sCookNode: one node, the record, then the extent its lists and maps take (§2.8, §2.9).\n", st.Name)
	g.pf("template <typename Ctx> inline bool %sCookNode( const Ctx & ctx, const TableCookRegion & region, uint8_t * at, const %s & value, TableByteOrder order )\n{\n", st.Name, st.Name)
	if g.isVar(st.Name) {
		g.pf("    if ( !%sCookBody( ctx, region, at, value, order ) ) { return false; }\n", st.Name)
	} else {
		g.pf("    %sCookBody( at, value, order );\n", st.Name)
	}
	g.pf("    int64_t extent_at = 0;\n")
	if !g.hasExtent(st) {
		g.pf("    return %sCookExtent( ctx, region, at + %d, extent_at, at, value, order );\n}\n\n", st.Name, record)
		return
	}
	g.pf("    if ( !%sCookExtent( ctx, region, at + %d, extent_at, at, value, order ) ) { return false; }\n", st.Name, record)
	g.pf("    return extent_at == %sExtent( ctx, value ); // the extent written is the extent measured, or no header is written\n}\n\n", st.Name)
}

// emitCookExtentSurface emits the extent writer and the node writer for every
// closure member of a unit that declares a list or a map.
func (g *tableGen) emitCookExtentSurface(members []*ir.Struct) {
	if !g.anyExtent {
		return
	}
	var bodies []*ir.Struct
	for _, st := range members {
		if ir.RecordLayout(g.unit, st) != nil {
			bodies = append(bodies, st)
		}
	}
	for _, st := range bodies {
		g.pf("%s;\n", g.cookExtentSignature(st))
	}
	g.pf("\n")
	for _, st := range bodies {
		g.emitCookExtent(st)
	}
	for _, st := range bodies {
		g.emitCookNode(st)
	}
}

// emitCookNodeBytes emits one node's whole span in a cooked region: its
// record at the region's alignment floor, plus the extent its lists and maps
// take (docs/SPEC-TABLES.md §2.8, §2.9, §7.2).
func (g *tableGen) emitCookNodeBytes(st *ir.Struct, ind, expr, onBad string) {
	ml := ir.RecordLayout(g.unit, st)
	if !g.anyExtent || !g.hasExtent(st) {
		g.pf("%ssize = %d; node_align = %d;\n", ind, ml.Size, ml.Align)
		return
	}
	g.pf("%s{\n", ind)
	g.pf("%s    const int64_t extent = %sExtent( ctx, %s );\n", ind, st.Name, expr)
	g.pf("%s    if ( extent < 0 ) { %s }\n", ind, onBad)
	g.pf("%s    size = %d + extent; node_align = %d;\n", ind, cookAlignUp(ml.Size, ir.RegionAlignFloor), ml.Align)
	g.pf("%s}\n", ind)
}

// onlyExtentFields reports a record whose every field is a list or a map, a
// cook body that writes the empty slots and reads nothing off the value,
// because the extent writer fills them.
func onlyExtentFields(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !f.IsMap() && !f.IsList() {
			return false
		}
	}
	return len(st.Fields) > 0
}

// placeColumn is the ONE function column an out-of-line array carries
// (docs/SPEC-TABLES.md §8.1, §16): the resolver the ONE text walk cannot spell
// for itself, because TableMap<Entry> and TableList<T> are types it has no
// name for. A map places one entry BY KEY and hands it back at its defaults, and a
// list ignores the key and appends. Empty in a unit that declares neither, so
// such a unit's descriptors are what they always were.
func (g *tableGen) placeColumn(f *ir.Field) string {
	if !g.anyExtent {
		return ""
	}
	if f.IsList() {
		return g.listPlaceThunk(f) + ", "
	}
	if !f.IsMap() {
		return "NULL, "
	}
	entry := mapEntryOf(f)
	n := entry.Name
	hold := fmt.Sprintf("TableMap<%s>", n)
	var insert string
	if mapKeyIsString(f) {
		insert = fmt.Sprintf("[]( TableWorker & worker, void * slot, const char * key, int32_t key_length, int64_t ) -> void * "+
			"{ if ( key == NULL || key_length > k%sKeyBound ) { return NULL; } "+ // KEYS NEVER CLAMP
			"%s * placed = TableMapPlace( worker, *(%s *) slot, key ); "+
			"if ( placed != NULL ) { TableEntrySetKey( *placed, key, key_length ); } return (void *) placed; }", n, n, hold)
	} else {
		typ, _ := g.cppFieldType(ir.MapKeyField(f).Type)
		insert = fmt.Sprintf("[]( TableWorker & worker, void * slot, const char *, int32_t, int64_t key_value ) -> void * "+
			"{ %s * placed = TableMapPlace( worker, *(%s *) slot, (%s) key_value ); "+
			"if ( placed != NULL ) { TableEntrySetKey( *placed, (%s) key_value ); } return (void *) placed; }", n, hold, typ, typ)
	}
	return insert + ", "
}

// nodeStorageBody, nodeStorageArg and nodeStorageReader are the EXTRA
// parameters a node's storage takes where a list or a map rides in an extent
// (docs/SPEC-TABLES.md §2.8, §2.9): the record's body and the id table, from
// which the framing scan sums the arrays, and the refusal's reason. A root
// that can name no such record does not take them, so a unit without either
// construct emits the dispatch it always emitted.
func (g *tableGen) nodeStorageBody(anyExtent bool) string {
	if anyExtent {
		return "const uint8_t * body, "
	}
	return ""
}

func (g *tableGen) nodeStorageTail(anyExtent bool) string {
	if anyExtent {
		return ", const TableIdTable * ids, TableRefuseReason & reason"
	}
	return ""
}

func (g *tableGen) nodeStorageArg(root *ir.Struct) string {
	if g.rootHasExtent(root) {
		return "body, "
	}
	return ""
}

func (g *tableGen) nodeStorageArgTail(root *ir.Struct) string {
	if g.rootHasExtent(root) {
		return ", &ids_table, reason"
	}
	return ""
}

func (g *tableGen) nodeStorageReader(root *ir.Struct) string {
	if g.rootHasExtent(root) {
		return "r.buffer, "
	}
	return ""
}

func (g *tableGen) nodeStorageReaderTail(root *ir.Struct) string {
	if g.rootHasExtent(root) {
		return ", r.ids, reason"
	}
	return ""
}

// rootHasExtent reports whether any record one root's numbering can name holds
// a list or a map by value, which is what decides the signature above.
func (g *tableGen) rootHasExtent(root *ir.Struct) bool {
	if !g.anyExtent {
		return false
	}
	return slices.ContainsFunc(ir.PointerReachable(root), g.hasExtent)
}

// emitUnreachedExtentRefusal refuses an UNREACHED NON-EMPTY SLOT, the same
// refusal §7.6 gives a pointer in that position (docs/SPEC-TABLES.md §2.8,
// §2.9): a COUNTED array's slots past its live count are storage the walk does
// not reach, so a non-empty list or map in one names elements the region will
// not hold, and the write answers false with nothing partial written.
//
// The test is the extent itself: an empty container takes no bytes and
// advances the running offset by none, so a record whose extent measures ZERO
// is a record whose every by-value list and map is empty. A measure that
// refuses answers non-zero here too, and refusing on it is the same answer one
// level up.
func (g *tableGen) emitUnreachedExtentRefusal(f *ir.Field, ref *ir.Struct, subject string) {
	if f.Array != ir.ArrayCounted {
		return // every other array shape is reached whole
	}
	g.pf("    for ( int32_t i = %s.%s_count; i < %d; i++ ) // %s: the slots the walk does not reach (§7.6)\n    {\n",
		subject, f.Name, f.ArrayBound, f.Name)
	g.pf("        if ( !TableExtentUnreachedEmpty( %sExtent( ctx, %s.%s[i] ) ) ) { return false; }\n", ref.Name, subject, f.Name)
	g.pf("    }\n")
}
