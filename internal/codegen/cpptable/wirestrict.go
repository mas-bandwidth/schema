package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableWireStrictRuntime is THE STRICT FORM-1 WALK (docs/SPEC-TABLES.md §4,
// §6.5, "TWO KINDS OF CONDITION"; docs/LAYERS/1-framing.md): every condition
// decidable from the wire alone, applied to every byte of a form-1 file before
// any value is decoded, in fields and records this reader declares and in
// those it does not. It is emitted into every unit after TableBodyEndsEarly,
// and every form-1 read (<T>Load and its verdict, <T>LoadMeasure, the region
// <T>Load, ReadStrict and the builder load) runs it before decode, right
// after TableBodyEndsEarly's root-terminator check. The Go engine's walk is the oracle
// (internal/tablewire/wirecheck.go) and this is its port, rule for rule.
var tableWireStrictRuntime = `
// THE STRICT FORM-1 WALK (docs/SPEC-TABLES.md §4, §6.5): the conditions
// decidable from the wire alone, over EVERY byte of a form-1 file before any
// value is decoded. SKIPPED IS NOT UNREAD: an unknown field, and a node record
// whose type this build cannot name, are walked by the same rules as a known
// one, so damage inside them refuses the whole load. The walk takes no
// declaration. It checks the framing (lengths, counts, terminators, an array,
// keyed or map header inside its own L, the id table's references, canonical
// LEB128, a kind no reader can skip, kind 32 with a nonzero L), a bool byte
// other than 0 or 1, ill-formed text, a repeated field id or keyed slot in one
// body, bytes left inside an L after its elements or after an arm, the node
// table's framing, a reserved id out of place, a count past the storage cap
// and a blob past its size cap, and then the pointer graph: an index past the
// node table, a cycle, a record no pointer reaches, and one blob record named
// from two slots. A non-finite float is counted and not refused here, so the
// decode can name the field where this reader declares it (§3.3).
//
// THE WORK IS BOUNDED BY THE WIRE. Nothing is sized from a count the input
// claims: a body's seen-set is a bitset over the id table TableOpen already
// bounded, the pointers are collected as they are met, and the graph's marks
// are sized by the node records the scan actually found. A file with no node
// table allocates nothing.
struct TableStrictWalk
{
    const TableIdTable * ids = NULL;
    // every kind 17 payload, in wire order: the root's, then each record's
    uint64_t * edges = NULL;
    int64_t edge_count = 0;
    int64_t edge_capacity = 0;
    // the graph's scratch, sized by the records the scan found
    int64_t * starts = NULL;
    uint8_t * marks = NULL;
    int64_t * stack = NULL;
    int64_t non_finite = 0;                // the NaNs and infinities the wire carries (§3.3)
    int32_t depth = 0;                     // the framed level being walked (§4)
    TableRefuseReason refusal = wire_damaged; // what a false answer refuses with

    TableStrictWalk() {}
    TableStrictWalk( const TableStrictWalk & ) = delete;
    TableStrictWalk & operator=( const TableStrictWalk & ) = delete;
    ~TableStrictWalk()
    {
        delete[] edges;
        delete[] starts;
        delete[] marks;
        delete[] stack;
    }

    // a canonical LEB128: the minimal spelling, at most ten bytes, the tenth
    // carrying no bit above the 64th (§3)
    static bool leb( const uint8_t * b, int64_t len, int64_t off, uint64_t & value, int64_t & next )
    {
        uint64_t v = 0;
        uint32_t shift = 0;
        for ( int32_t i = 0;; i++ )
        {
            if ( off >= len || i >= 10 ) { return false; }
            const uint8_t c = b[off++];
            if ( i == 9 && c > 1 ) { return false; }
            v |= uint64_t( c & 0x7F ) << shift;
            if ( ( c & 0x80 ) == 0 )
            {
                if ( i > 0 && c == 0 ) { return false; } // 0x80 0x00 is not zero's spelling
                value = v;
                next = off;
                return true;
            }
            shift += 7;
        }
    }

    // the width of a fixed-width kind, and 0 for every other kind
    static int64_t width( int32_t kind )
    {
        switch ( kind )
        {
            case 1:
            case 2:
            case 6:
            case 20:
            case 25: return 1;
            case 3:
            case 7:
            case 21:
            case 26: return 2;
            case 4:
            case 8:
            case 10:
            case 22:
            case 27: return 4;
            case 5:
            case 9:
            case 11:
            case 23:
            case 28:
            case 36: return 8;
            case 18:
            case 19:
            case 24:
            case 29: return 16;
            default: return 0;
        }
    }

    // the three ids the language holds back (§3.1, §3.3)
    static bool reserved( uint64_t id ) { return id == kTableNodeTableFieldId || id == 0xFFFFFFFFFFFFFFFEull || id == 0xFFFFFFFFFFFFFFFDull; }

    // a bool byte is 0 or 1; a float's finiteness is counted for the decode
    bool scalar( const uint8_t * b, int32_t kind )
    {
        if ( kind == 1 ) { return b[0] <= 1; }
        if ( kind == 10 )
        {
            const uint32_t bits = uint32_t( b[0] ) | uint32_t( b[1] ) << 8 | uint32_t( b[2] ) << 16 | uint32_t( b[3] ) << 24;
            if ( !TableBitsFinite32( bits ) ) { non_finite++; }
        }
        if ( kind == 11 )
        {
            uint64_t bits = 0;
            for ( int32_t i = 0; i < 8; i++ ) { bits |= uint64_t( b[i] ) << ( 8 * i ); }
            if ( !TableBitsFinite64( bits ) ) { non_finite++; }
        }
        return true;
    }

    // one pointer collected: the store grows with the pointers the wire
    // actually carries, each at least two of its bytes
    void edge( uint64_t index )
    {
        if ( edge_count == edge_capacity )
        {
            const int64_t capacity = edge_capacity == 0 ? 16 : edge_capacity * 2;
            uint64_t * grown = new uint64_t[size_t( capacity )];
            for ( int64_t i = 0; i < edge_count; i++ ) { grown[i] = edges[i]; }
            delete[] edges;
            edges = grown;
            edge_capacity = capacity;
        }
        edges[edge_count++] = index;
    }

    // one framed level deeper, refused past the binary bound (§4)
    bool deeper()
    {
        if ( depth >= ` + fmt.Sprintf("%d", ir.TableBinaryMaxDepth) + ` ) { return false; }
        depth++;
        return true;
    }

    // ONE TABLE BODY, whose terminator must be its LAST byte. Only the root
    // admits the node table's reserved id, as kind 12, and hands its payload
    // back; the same field id twice in one body is damage (§4).
    bool body( const uint8_t * b, int64_t len, bool root, const uint8_t ** table, int64_t * node_table_bytes )
    {
        uint64_t seen[kTableProbeBound / 64];
        for ( int64_t w = 0; w < ( ids->count + 63 ) / 64; w++ ) { seen[w] = 0; }
        int64_t off = 0;
        for ( ;; )
        {
            uint64_t ref = 0;
            if ( !leb( b, len, off, ref, off ) ) { return false; }
            if ( ref == 0 ) { return off == len; } // bytes after a body's terminator inside its L
            if ( ref > uint64_t( ids->count ) ) { return false; }
            const uint64_t bit = 1ull << ( ( ref - 1 ) & 63 );
            if ( ( seen[( ref - 1 ) >> 6] & bit ) != 0 ) { return false; } // the same field id twice in one body
            seen[( ref - 1 ) >> 6] |= bit;
            if ( off >= len ) { return false; }
            const int32_t kind = b[off++];
            const uint64_t id = ids->at( ref );
            if ( id == kTableNodeTableFieldId )
            {
                if ( !root || kind != 12 ) { return false; } // the node table rides at the root, as kind 12 (§3.1)
                uint64_t n = 0;
                int64_t after = 0;
                if ( !leb( b, len, off, n, after ) || n > uint64_t( len - after ) ) { return false; }
                *table = b + after;
                *node_table_bytes = int64_t( n );
                off = after + int64_t( n );
                continue;
            }
            if ( reserved( id ) ) { return false; } // an announcement id in a file (§3.3)
            if ( !payload( b, len, off, kind, off ) ) { return false; }
        }
    }

    // one SELF-FRAMED payload of kind at off: what follows a field's kind
    // byte, and an array element
    bool payload( const uint8_t * b, int64_t len, int64_t off, int32_t kind, int64_t & next )
    {
        const int64_t w = width( kind );
        if ( w > 0 )
        {
            if ( w > len - off ) { return false; }
            next = off + w;
            return scalar( b + off, kind );
        }
        uint64_t n = 0;
        int64_t after = 0;
        switch ( kind )
        {
            case 17: // a node index: bounds-checked with the graph, never followed here (§3.1)
                if ( !leb( b, len, off, n, next ) ) { return false; }
                edge( n );
                return true;
            case 30: // an enum's variant reference into the id table
                if ( !leb( b, len, off, n, next ) ) { return false; }
                return n <= uint64_t( ids->count );
            case 15: return arm( b, len, off, next );
            case 12:
            case 13:
            case 14:
            case 16:
            case 31:
            case 32:
                if ( !leb( b, len, off, n, after ) || n > uint64_t( len - after ) ) { return false; } // a length past the body carrying it
                next = after + int64_t( n );
                return content( b + after, int64_t( n ), kind );
            default: return false; // a kind no reader can skip
        }
    }

    // a payload already framed by a length, consumed exactly
    bool content( const uint8_t * b, int64_t len, int32_t kind )
    {
        const int64_t w = width( kind );
        if ( w > 0 )
        {
            if ( len != w ) { return false; } // an L that is not its kind's width
            return scalar( b, kind );
        }
        int64_t end = 0;
        bool whole = false;
        switch ( kind )
        {
            case 12: return TableUtf8Valid( b, uint64_t( len ) );
            case 31: return true; // the escape: opaque by rule (§3)
            case 32: return len == 0;
            case 13:
            case 14:
            case 16:
                if ( !deeper() ) { return false; }
                if ( kind == 13 ) { whole = body( b, len, false, NULL, NULL ); }
                else if ( kind == 14 ) { whole = array( b, len ); }
                else
                {
                    whole = keyed( b, len );
                }
                depth--;
                return whole;
            case 15:
            case 17:
            case 30:
                if ( !payload( b, len, 0, kind, end ) ) { return false; }
                return end == len; // bytes left inside an arm after its payload
            default: return false;
        }
    }

    // THE HEADER LIES INSIDE ITS OWN L (§4): an array, keyed or map body too
    // short for its element kind and count is damage, and a count past the
    // storage cap is refused by name
    bool header( const uint8_t * b, int64_t len, int32_t & element, uint64_t & count, int64_t & off )
    {
        if ( len < 2 || !leb( b, len, 1, count, off ) ) { return false; }
        if ( count > 0x7FFFFFFFull )
        {
            refusal = count_over_extent_cap;
            return false;
        }
        element = b[0];
        return true;
    }

    bool array( const uint8_t * b, int64_t len )
    {
        int32_t element = 0;
        uint64_t count = 0;
        int64_t off = 0;
        if ( !header( b, len, element, count, off ) ) { return false; }
        for ( uint64_t i = 0; i < count; i++ )
        {
            if ( !payload( b, len, off, element, off ) ) { return false; }
        }
        return off == len; // bytes left inside an array's L after its elements
    }

    bool keyed( const uint8_t * b, int64_t len )
    {
        int32_t element = 0;
        uint64_t count = 0;
        int64_t off = 0;
        if ( !header( b, len, element, count, off ) ) { return false; }
        uint64_t seen[kTableProbeBound / 64];
        for ( int64_t w = 0; w < ( ids->count + 63 ) / 64; w++ ) { seen[w] = 0; }
        for ( uint64_t i = 0; i < count; i++ )
        {
            uint64_t key = 0;
            uint64_t n = 0;
            int64_t after = 0;
            if ( !leb( b, len, off, key, off ) ) { return false; }
            if ( key == 0 || key > uint64_t( ids->count ) ) { return false; } // a slot key that names no id
            const uint64_t bit = 1ull << ( ( key - 1 ) & 63 );
            if ( ( seen[( key - 1 ) >> 6] & bit ) != 0 ) { return false; } // the same slot twice in one body
            seen[( key - 1 ) >> 6] |= bit;
            if ( !leb( b, len, off, n, after ) || n > uint64_t( len - after ) ) { return false; }
            off = after + int64_t( n );
            if ( !content( b + after, int64_t( n ), element ) ) { return false; }
        }
        return off == len; // bytes left inside a keyed or map body's L after its slots
    }

    // a union payload in place: the arm reference, and for a set arm its kind
    // byte, its L and the content its L frames. A structural arm is a level.
    bool arm( const uint8_t * b, int64_t len, int64_t off, int64_t & next )
    {
        uint64_t ref = 0;
        int64_t after_ref = 0;
        if ( !leb( b, len, off, ref, after_ref ) ) { return false; }
        if ( ref == 0 )
        {
            next = after_ref;
            return true;
        }
        if ( ref > uint64_t( ids->count ) || after_ref >= len ) { return false; }
        const int32_t kind = b[after_ref];
        uint64_t n = 0;
        int64_t after = 0;
        if ( !leb( b, len, after_ref + 1, n, after ) || n > uint64_t( len - after ) ) { return false; }
        next = after + int64_t( n );
        const bool structural = kind == 13 || kind == 14 || kind == 16 || kind == 15;
        if ( structural && !deeper() ) { return false; }
        const bool whole = content( b + after, int64_t( n ), kind );
        if ( structural ) { depth--; }
        return whole;
    }

    // THE GRAPH, DEFINED ONCE (§6.5): node 0 is the root and node k is record
    // k, named by index k + 1 on the wire. Every index names null or a node; a
    // blob record is named from one slot at most; a walk from the root that
    // meets a node whose descent is still open is a cycle; and a record the
    // walk never enters is unreachable.
    TableRefuseReason graph( int64_t nodes, const uint8_t * blob )
    {
        marks = new uint8_t[size_t( nodes )];
        for ( int64_t k = 0; k < nodes; k++ ) { marks[k] = 0; }
        for ( int64_t e = 0; e < edge_count; e++ )
        {
            if ( edges[e] == 0 ) { continue; }
            if ( edges[e] - 1 >= uint64_t( nodes ) ) { return wire_damaged; } // an index past the node table
            const int64_t target = int64_t( edges[e] - 1 );
            if ( target > 0 && blob[target - 1] != 0 )
            {
                if ( marks[target] != 0 ) { return blob_shared; } // one blob record named from two slots (§2.5)
                marks[target] = 1;
            }
        }
        for ( int64_t k = 0; k < nodes; k++ ) { marks[k] = 0; } // 0 unvisited, 1 open, 2 closed
        stack = new int64_t[size_t( nodes ) * 2];                 // each node's index and its next edge
        stack[0] = 0;
        stack[1] = starts[0];
        marks[0] = 1;
        int64_t top = 1;
        while ( top > 0 )
        {
            const int64_t node = stack[( top - 1 ) * 2];
            const int64_t next = stack[( top - 1 ) * 2 + 1];
            if ( next == starts[node + 1] )
            {
                marks[node] = 2;
                top--;
                continue;
            }
            stack[( top - 1 ) * 2 + 1] = next + 1;
            if ( edges[next] == 0 ) { continue; }
            const int64_t target = int64_t( edges[next] - 1 );
            if ( marks[target] == 1 ) { return wire_damaged; } // a cycle, over every slot
            if ( marks[target] == 2 ) { continue; }            // a second pointer to a closed node is a share
            marks[target] = 1;
            stack[top * 2] = target;
            stack[top * 2 + 1] = starts[target];
            top++;
        }
        for ( int64_t k = 1; k < nodes; k++ )
        {
            if ( marks[k] == 0 ) { return wire_damaged; } // a node record no pointer reaches
        }
        return ok;
    }

    // what a false answer from a body refuses with. TWO REFUSALS IN ONE READ:
    // the first in read order names it (§2.9), and the walk stops at an
    // over-cap count, so a NaN it counted came before the count
    TableRefuseReason refused() const { return refusal == count_over_extent_cap && non_finite > 0 ? non_finite_float : refusal; }

    // THE REFUSAL ON THE REPORT: damage for every framing condition, and the
    // ones the strict read refuses by name: blob_shared, the storage cap and
    // a NaN met before it.
    static void stamp( TableReport * report, TableRefuseReason why )
    {
        if ( why == wire_damaged )
        {
            report->damaged = true;
            report->refuse_reason = wire_damaged;
            return;
        }
        report->refused = true;
        report->reason = why;
        report->refuse_reason = why;
    }
};

// TableWireStrict runs the walk over a form-1 file's ROOT BODY (the bytes
// between the form byte and the id table TableOpen read) and over every node
// record the root's node table carries. It answers ok for a wire the decode
// may read, and otherwise the refusal: wire_damaged, blob_shared,
// count_over_extent_cap, or non_finite_float for a NaN met before an
// over-cap count. non_finite says a float the wire carries is a NaN or
// an infinity, in a field this reader declares or not: the read refuses it
// once the decode has named the field (§3.3).
inline TableRefuseReason TableWireStrict( const uint8_t * body, int64_t bytes, const TableIdTable & ids, bool & non_finite )
{
    non_finite = false;
    TableStrictWalk walk;
    walk.ids = &ids;
    const uint8_t * table = NULL;
    int64_t node_table_bytes = -1;
    if ( !walk.body( body, bytes, true, &table, &node_table_bytes ) ) { return walk.refused(); }
    if ( node_table_bytes < 0 )
    {
        // NO NODE TABLE: the root is the only node, so every index is null;
        // index 1 names the root, whose descent is open, and is a cycle
        for ( int64_t e = 0; e < walk.edge_count; e++ )
        {
            if ( walk.edges[e] != 0 ) { return wire_damaged; }
        }
        non_finite = walk.non_finite > 0;
        return ok;
    }
    // THE RECORD SCAN IS AUTHORITATIVE (§3.1): a node_count that disagrees
    // with it is malformed, and nothing is sized from node_count
    uint64_t declared = 0;
    int64_t first = 0;
    if ( !TableStrictWalk::leb( table, node_table_bytes, 0, declared, first ) ) { return wire_damaged; }
    int64_t records = 0;
    for ( int64_t off = first; off < node_table_bytes; records++ )
    {
        uint64_t ref = 0;
        uint64_t n = 0;
        int64_t after = 0;
        if ( !TableStrictWalk::leb( table, node_table_bytes, off, ref, off ) ) { return wire_damaged; }
        if ( ref == 0 || ref > uint64_t( ids.count ) ) { return wire_damaged; }      // a type reference that names no id
        if ( TableStrictWalk::reserved( ids.at( ref ) ) ) { return wire_damaged; } // a reserved id in a record's type position
        if ( !TableStrictWalk::leb( table, node_table_bytes, off, n, after ) || n > uint64_t( node_table_bytes - after ) ) { return wire_damaged; }
        off = after + int64_t( n );
    }
    if ( declared != uint64_t( records ) ) { return wire_damaged; }
    const int64_t nodes = records + 1;
    walk.starts = new int64_t[size_t( nodes ) + 1];
    uint8_t * blob = new uint8_t[size_t( nodes )];
    walk.starts[0] = 0;
    walk.starts[1] = walk.edge_count;
    TableRefuseReason verdict = ok;
    int64_t off = first;
    for ( int64_t k = 0; k < records && verdict == ok; k++ )
    {
        uint64_t ref = 0;
        uint64_t n = 0;
        int64_t after = 0;
        if ( !TableStrictWalk::leb( table, node_table_bytes, off, ref, off ) || !TableStrictWalk::leb( table, node_table_bytes, off, n, after ) )
        {
            verdict = wire_damaged;
            break;
        }
        const uint8_t * record = table + after;
        const uint64_t type_id = ids.at( ref );
        off = after + int64_t( n );
        blob[k] = 0;
        if ( type_id == 0x` + fmt.Sprintf("%016x", ir.BytesWireTypeId) + `ull || type_id == 0x` + fmt.Sprintf("%016x", ir.StringWireTypeId) + `ull )
        {
            // A BLOB RECORD IS ITS BYTES (§2.5, §3.1): no body to walk; a text
            // blob is well-formed text, and a blob is within its size cap
            blob[k] = 1;
            if ( n > 0xFFFFFFFFull ) { verdict = wire_damaged; }
            else if ( type_id == 0x` + fmt.Sprintf("%016x", ir.StringWireTypeId) + `ull && !TableUtf8Valid( record, n ) ) { verdict = wire_damaged; }
        }
        else if ( !walk.body( record, int64_t( n ), false, NULL, NULL ) ) { verdict = walk.refused(); }
        walk.starts[k + 2] = walk.edge_count;
    }
    if ( verdict == ok ) { verdict = walk.graph( nodes, blob ); }
    delete[] blob;
    non_finite = verdict == ok && walk.non_finite > 0;
    return verdict;
}
`
