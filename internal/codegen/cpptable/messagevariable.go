// THE MESSAGE FORM over a POINTERED root (docs/SPEC-TABLES.md §3.1, §3.3): the
// batch's three verbs over a REGION, and the node dispatch a bit-framed node
// table needs.
//
// A body's NODE TABLE is its FIRST FIELD, because a pointer index is
// `bits_required(0, node count)` wide and the node count rides in the node
// table, so the table has to be read before an index can be. Its framing is
// this wire's own: the reserved id as a reference, the count at thirty-two raw
// bits, then the records back to back — a type id reference and a body that
// ends at its own zero reference for a table, or a thirty-two bit length, an
// align and the bytes for a blob. §3.1's numbering, its depth-first order and
// every malformed rule of it are untouched.
//
// A BATCH OF POINTERED ROOTS TAKES ONE REGION FOR THE BATCH, not one a body:
// LoadMeasure reads the batch and returns the region bytes for the whole of
// it, the caller allocates once, and LoadMessages fills it and writes each
// body's root into the caller's array of root pointers. Each body keeps its
// OWN numbering inside that one region.
package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitVariableMessageSurface emits one VARIABLE root's message surface.
func (g *tableGen) emitVariableMessageSurface(st *ir.Struct) {
	n := st.Name
	g.pf("// ---- %s on the MESSAGE wire: the batch over a region (docs/SPEC-TABLES.md §3.3) ----\n\n", n)

	// ONE BODY's bits and bytes: the numbering derived, the node table FIRST,
	// then the root's own fields and its terminator.
	g.pf("// %sMessageBodyBits: one root body's bits at bit position `at` of the batch —\n", n)
	g.pf("// the numbering derived from the graph, the node table FIRST, then the\n")
	g.pf("// fields, then the zero reference. Measure derives the numbering and save\n")
	g.pf("// derives the same one, and nothing passes between them (§3.1).\n")
	g.pf("template <typename Ctx>\ninline int64_t %sMessageBodyBits( const Ctx & ctx, const %s & root, int64_t at, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    int64_t bits = -1;\n")
	g.pf("    if ( %sNumberFrom( ctx, numbering, root ) )\n    {\n", n)
	g.pf("        const int64_t index_bits = TableBitsRequired( 0, numbering.count + 1 );\n")
	g.pf("        const int64_t table = TableMessageNodeTableMeasure( ctx, numbering, index_bits, at );\n")
	g.pf("        if ( table >= 0 )\n        {\n")
	g.pf("            const int64_t body = %sMeasureMessageBody( ctx, numbering, index_bits, at + table, root );\n", n)
	g.pf("            bits = body < 0 ? -1 : table + body;\n")
	g.pf("        }\n    }\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return bits;\n}\n\n")

	g.pf("template <typename Ctx>\ninline bool %sMessageBodySave( const Ctx & ctx, const %s & root, TableBitWriter & w, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    bool ok = false;\n")
	g.pf("    if ( %sNumberFrom( ctx, numbering, root ) )\n    {\n", n)
	g.pf("        const int64_t index_bits = TableBitsRequired( 0, numbering.count + 1 );\n")
	g.pf("        ok = TableMessageNodeTableSave( ctx, numbering, index_bits, w ) && %sSaveMessageBody( ctx, numbering, index_bits, w, root );\n", n)
	g.pf("    }\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return ok && !w.overflow;\n}\n\n")

	// the three verbs, over an array of root pointers
	g.pf("// THE PRIMITIVE IS A BATCH (§3.3): a number of ROOTS in one buffer, one count\n")
	g.pf("// and one continuous bit stream, each body carrying its own numbering. A\n")
	g.pf("// root is a locked region's, `builder.AsConst()`, or a loaded one's. M above\n")
	g.pf("// 256 is a refusal by name, batch_too_large, with nothing written.\n")
	g.pf("inline int64_t %sMeasureMessages( const %s * const * roots, int64_t count, TableReport * report, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( roots == NULL || count < 1 ) { return -1; }\n")
	g.pf("    if ( count > kTableMessageBatchMax ) { TableMessageRefuseBatch( report ); return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    int64_t bits = 8; // the body count\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( roots[i] == NULL ) { return -1; }\n")
	g.pf("        const int64_t body = %sMessageBodyBits( ctx, *roots[i], bits, allocator );\n", n)
	g.pf("        if ( body < 0 ) { return -1; }\n")
	g.pf("        bits += body;\n    }\n")
	g.pf("    return 1 + ( bits + 7 ) / 8;\n}\n\n")

	g.pf("inline int64_t %sSaveMessages( const %s * const * roots, int64_t count, uint8_t * buffer, int64_t capacity, TableReport * report, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( roots == NULL || count < 1 ) { return -1; }\n")
	g.pf("    if ( count > kTableMessageBatchMax ) { TableMessageRefuseBatch( report ); return -1; }\n")
	g.pf("    TableMessageBatch batch;\n")
	g.pf("    if ( !TableMessageBatchBegin( batch, buffer, capacity, count ) ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    for ( int64_t i = 0; i < count; i++ )\n    {\n")
	g.pf("        if ( roots[i] == NULL || !%sMessageBodySave( ctx, *roots[i], batch.w, allocator ) ) { return -1; }\n", n)
	g.pf("        batch.written++;\n    }\n")
	g.pf("    return TableMessageBatchEnd( batch ); // == %sMeasureMessages( roots, count, report, allocator )\n}\n\n", n)

	// one record's scan: the type reference, then its body stepped over
	g.pf("// %sMessageRecordScan: one node record's type id and the extent its maps\n", n)
	g.pf("// take, or a blob's length, the reader left after the record. A type id\n")
	g.pf("// reference of 0, one past E, or one naming anything but a kind-0 entry is\n")
	g.pf("// damage, as §3.1 and §3.3 say.\n")
	g.pf("inline bool %sMessageRecordScan( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, uint64_t & type_id, int64_t & extent, int64_t & length )\n{\n", n)
	g.pf("    uint64_t type_ref = 0;\n")
	g.pf("    if ( !r.get( type_ref, vocabulary.ref_bits ) ) { return false; }\n")
	g.pf("    TableMessageEntry type_entry;\n")
	g.pf("    if ( !TableMessageNameEntry( vocabulary, type_ref, type_entry ) ) { return false; }\n")
	g.pf("    type_id = type_entry.id;\n")
	g.pf("    extent = 0;\n")
	g.pf("    length = 0;\n")
	g.pf("    if ( type_id == kTableBytesTypeId || type_id == kTableStringTypeId )\n    {\n")
	g.pf("        // A BLOB RECORD CARRIES A LENGTH AT THIRTY-TWO RAW BITS, then ALIGNS,\n")
	g.pf("        // then the bytes verbatim (§3.3)\n")
	g.pf("        uint64_t n = 0;\n")
	g.pf("        if ( !r.get( n, 32 ) || !r.align() || !r.skip( (int64_t) n * 8 ) ) { return false; }\n")
	g.pf("        length = (int64_t) n;\n")
	g.pf("        return true;\n    }\n")
	g.pf("    return %sNodeMessageExtent( type_id, r, vocabulary, index_bits, extent );\n}\n\n", n)

	// the per-body storage scan, shared by LoadMeasure and LoadMessages
	g.pf("// %sMessageBodyStorage: one body's node count and data bytes from the FRAMING\n", n)
	g.pf("// alone, the reader left at the next body. The node table is walked record\n")
	g.pf("// by record — a table record's body stepped over by its announced shapes, a\n")
	g.pf("// blob's by its length — and the root's own fields after it. False is a numbering\n")
	g.pf("// that could not be sized; `complete` false is a ROOT body whose own framing\n")
	g.pf("// gave out, which the load meets as damage inside this body after the\n")
	g.pf("// bodies before it were delivered, so the batch is sized through this body\n")
	g.pf("// and no further (§3.3).\n")
	g.pf("inline bool %sMessageBodyStorage( TableBitReader & r, const TableVocabulary & vocabulary, int64_t & records, int64_t & data, bool & complete )\n{\n", n)
	g.pf("    complete = true;\n")
	g.pf("    records = 0;\n")
	g.pf("    data = 0;\n")
	g.pf("    int64_t count = 0;\n")
	g.pf("    if ( !TableMessageNodeTableOpen( r, vocabulary, count ) ) { return false; }\n")
	g.pf("    const int64_t index_bits = TableBitsRequired( 0, count + 1 );\n")
	g.pf("    for ( int64_t k = 0; k < count; k++ )\n    {\n")
	g.pf("        uint64_t type_id = 0;\n")
	g.pf("        int64_t extent = 0, length = 0;\n")
	g.pf("        if ( !%sMessageRecordScan( r, vocabulary, index_bits, type_id, extent, length ) ) { return false; }\n", n)
	g.pf("        const int64_t storage = %sNodeMessageStorage( type_id, extent, length );\n", n)
	g.pf("        if ( storage > 0 ) { data += storage; } // a type id this build cannot name commands none\n")
	g.pf("        records++;\n")
	g.pf("    }\n")
	g.pf("    int64_t root_extent = 0;\n")
	if g.anyExtent && g.hasExtent(st) {
		g.pf("    if ( !%sMessageExtent( r, vocabulary, index_bits, root_extent ) ) { complete = false; }\n", n)
	} else {
		g.pf("    if ( !TableMessageSkipBody( r, vocabulary, index_bits ) ) { complete = false; }\n")
	}
	g.pf("    data += TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent );\n", n)
	g.pf("    return true;\n}\n\n")

	// LoadMeasure over the batch: ONE region
	g.pf("// %sLoadMeasure's MESSAGE overload: the exact region bytes ONE BATCH needs,\n", n)
	g.pf("// which is one measurement, one allocation and one bounds check for however\n")
	g.pf("// many bodies ride (§3.3, §6.5). It is a scan by the announced shapes and\n")
	g.pf("// reads no field value. The answer is the data bytes plus the attribution,\n")
	g.pf("// one node directory a body, and -1 for a wire it cannot size: no vocabulary,\n")
	g.pf("// another form, or framing that gives out.\n")
	g.pf("inline int64_t %sLoadMeasure( const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, int64_t * attribution_bytes = NULL )\n{\n", n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableMessageBatchReader br;\n")
	g.pf("    const int64_t bodies = TableMessageBatchOpen( br, vocabulary, buffer, bytes, &ignored );\n")
	g.pf("    if ( bodies < 0 ) { return -1; }\n")
	g.pf("    int64_t data = 0, attribution = 0;\n")
	g.pf("    for ( int64_t b = 0; b < bodies; b++ )\n    {\n")
	g.pf("        int64_t records = 0, body_data = 0;\n        bool complete = true;\n")
	g.pf("        if ( !%sMessageBodyStorage( br.r, vocabulary, records, body_data, complete ) ) { return -1; }\n", n)
	g.pf("        data += body_data;\n")
	g.pf("        attribution += ( records + 1 ) * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("        if ( !complete ) { break; } // damage inside this body: the load delivers the ones before it\n")
	g.pf("    }\n")
	g.pf("    if ( attribution_bytes != NULL ) { *attribution_bytes = attribution; }\n")
	g.pf("    return data + attribution;\n}\n\n")

	// ONE BODY into the region: the directory, the records, the root
	g.pf("// %sLoadMessageBodyInto: one body of a batch into the region at `used`. Its\n", n)
	g.pf("// chunk is the node DIRECTORY, then the records in wire order, then the root\n")
	g.pf("// and the extent its maps take, so every offset a pass needs is known when\n")
	g.pf("// the pass reaches it. PASS ONE fills the numbering from the framing and\n")
	g.pf("// places every node; PASS TWO decodes each record's body into the storage it\n")
	g.pf("// owns; the ROOT's own body decodes last, so every index it carries resolves\n")
	g.pf("// against a numbering already known whole.\n")
	g.pf("inline bool %sLoadMessageBodyInto( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * out, uint8_t * region, int64_t region_bytes, int64_t & used, const %s * & root_out )\n{\n", n, n)
	g.pf("    // the node table opens the body, or the body has none\n")
	g.pf("    int64_t count = 0;\n")
	g.pf("    if ( !TableMessageNodeTableOpen( r, vocabulary, count ) ) { out->malformed = true; return false; }\n")
	g.pf("    const int64_t directory_bytes = ( count + 1 ) * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("    if ( used + directory_bytes > region_bytes ) { out->malformed = true; return false; }\n")
	g.pf("    TableNodeDirEntry * directory = (TableNodeDirEntry *) ( region + used );\n")
	g.pf("    used += directory_bytes;\n")
	g.pf("    const int64_t index_bits = TableBitsRequired( 0, count + 1 );\n")
	g.pf("    TableNodeMap nodes;\n")
	g.pf("    nodes.base = region;\n")
	g.pf("    nodes.entries = directory;\n")
	g.pf("    nodes.count = count + 1;\n")
	g.pf("    nodes.good = false;\n\n")
	g.pf("    // PASS ONE: the numbering from the framing, every node placed, no body read\n")
	g.pf("    const int64_t records_start = r.offset;\n")
	g.pf("    int32_t unknown_records = 0;\n")
	g.pf("    for ( int64_t k = 0; k < count; k++ )\n    {\n")
	g.pf("        uint64_t type_id = 0;\n")
	g.pf("        int64_t extent = 0, length = 0;\n")
	g.pf("        if ( !%sMessageRecordScan( r, vocabulary, index_bits, type_id, extent, length ) ) { out->malformed = true; return false; }\n", n)
	g.pf("        const int64_t storage = %sNodeMessageStorage( type_id, extent, length );\n", n)
	g.pf("        directory[k + 1].type_id = type_id;\n")
	g.pf("        if ( storage <= 0 )\n        {\n")
	g.pf("            // a record whose type id this build cannot name KEEPS ITS INDEX, is\n")
	g.pf("            // counted once here and not once per pointer, and every reference\n")
	g.pf("            // to it reads null (§3.1)\n")
	g.pf("            unknown_records++;\n")
	g.pf("            directory[k + 1].offset = kTableNodeAbsent;\n")
	g.pf("            continue;\n        }\n")
	g.pf("        if ( used + storage > region_bytes ) { out->malformed = true; return false; }\n")
	g.pf("        directory[k + 1].offset = (uint64_t) used;\n")
	g.pf("        %sNodePlace( type_id, region + used, length );\n", n)
	g.pf("        used += storage;\n")
	g.pf("    }\n")
	g.pf("    const int64_t fields_start = r.offset;\n")
	g.pf("    int64_t root_extent = 0;\n")
	if g.anyExtent && g.hasExtent(st) {
		g.pf("    {\n        TableBitReader walk = r;\n")
		g.pf("        if ( !%sMessageExtent( walk, vocabulary, index_bits, root_extent ) ) { out->malformed = true; return false; }\n    }\n", n)
	}
	g.pf("    const int64_t root_bytes = TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent );\n", n)
	g.pf("    if ( used + root_bytes > region_bytes ) { out->malformed = true; return false; }\n")
	g.pf("    directory[0].offset = (uint64_t) used;\n")
	g.pf("    directory[0].type_id = 0x%016xull;\n", ir.TableWireId(st.Name))
	g.pf("    %s * root = new ( region + used ) %s; // lifetime only: LoadMessageBody's first act is %sReset\n", n, n, n)
	g.pf("    %sReset( *root );\n", n)
	// THE ROOT IS HANDED BACK AS SOON AS IT IS PLACED, so a body damaged inside
	// its own fields still answers where the decode put what it decoded — the
	// COUNT is what says it is not a body (§3.3)
	g.pf("    root_out = root;\n")
	if g.anyExtent {
		g.pf("    TableExtentCarve root_carve;\n")
		g.pf("    root_carve.at = region + used + TableAlignUp64( (int64_t) sizeof( %s ) );\n", n)
		g.pf("    root_carve.left = root_extent;\n")
	}
	g.pf("    used += root_bytes;\n")
	g.pf("    nodes.good = true;\n")
	g.pf("    out->unknown += unknown_records;\n\n")
	g.pf("    // PASS TWO: each record's body into its own storage, in wire order\n")
	g.pf("    r.offset = records_start;\n")
	g.pf("    for ( int64_t k = 0; k < count; k++ )\n    {\n")
	g.pf("        uint64_t type_ref = 0;\n")
	g.pf("        if ( !r.get( type_ref, vocabulary.ref_bits ) ) { out->malformed = true; return false; }\n")
	g.pf("        const uint64_t type_id = directory[k + 1].type_id;\n")
	g.pf("        if ( type_id == kTableBytesTypeId || type_id == kTableStringTypeId )\n        {\n")
	g.pf("            uint64_t length = 0;\n")
	g.pf("            if ( !r.get( length, 32 ) || !r.align() || !r.has( (int64_t) length * 8 ) ) { out->malformed = true; return false; }\n")
	g.pf("            if ( directory[k + 1].offset != kTableNodeAbsent && length > 0 ) { memcpy( region + directory[k + 1].offset + kTableBlobHeader, r.buffer + r.offset / 8, (size_t) length ); }\n")
	g.pf("            r.offset += (int64_t) length * 8;\n")
	g.pf("            continue;\n        }\n")
	g.pf("        if ( directory[k + 1].offset == kTableNodeAbsent )\n        {\n")
	g.pf("            if ( !TableMessageSkipBody( r, vocabulary, index_bits ) ) { out->malformed = true; return false; }\n")
	g.pf("            continue;\n        }\n")
	g.pf("        if ( !%sNodeMessageBody( type_id, r, vocabulary, out, nodes, index_bits, region + directory[k + 1].offset ) ) { return false; }\n", n)
	g.pf("    }\n")
	g.pf("    if ( r.offset != fields_start ) { out->malformed = true; return false; } // the two passes disagree about the table's extent\n\n")
	g.pf("    // and the ROOT's own body last\n")
	if g.anyExtent {
		g.pf("    nodes.carve = &root_carve; // the ROOT's extent is its own, like every node's\n")
	}
	g.pf("    return %sLoadMessageBody( r, vocabulary, out, nodes, index_bits, *root );\n}\n\n", n)
	// LoadMessages: the batch into ONE region
	g.pf("// %sLoadMessages: decode a BATCH into the caller's exact-sized region and\n", n)
	g.pf("// write each body's root into `roots`. `count` is IN and OUT: the storage the\n")
	g.pf("// caller has room for, then what it got. M above the capacity is a refusal\n")
	g.pf("// by name with count holding the wire's M; damage inside body k delivers\n")
	g.pf("// bodies 1 to k - 1 and count says k - 1 (§3.3). LOAD IS A SCAN: it follows\n")
	g.pf("// no reference, so there is no depth cap and no visited set. NULL roots\n")
	g.pf("// beyond count are not bodies.\n")
	g.pf("inline bool %sLoadMessages( const %s ** roots, int64_t * count, uint8_t * region, int64_t region_bytes, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )\n{\n", n, n)
	g.pf("    TableReport ignored;\n")
	g.pf("    TableReport * out = report != NULL ? report : &ignored;\n")
	g.pf("    if ( roots == NULL || count == NULL ) { out->malformed = true; return false; }\n")
	g.pf("    const int64_t capacity = *count;\n")
	g.pf("    *count = 0;\n")
	g.pf("    TableMessageBatchReader br;\n")
	g.pf("    const int64_t bodies = TableMessageBatchOpen( br, vocabulary, buffer, bytes, out );\n")
	g.pf("    if ( bodies < 0 ) { return false; }\n")
	g.pf("    if ( bodies > capacity ) { *count = bodies; TableMessageRefuseBatch( out ); return false; }\n")
	g.pf("    if ( region == NULL || region_bytes < 0 || ( ( (uintptr_t) region ) & ( kTableAlign - 1 ) ) != 0 ) { out->malformed = true; return false; }\n")
	g.pf("    memset( region, 0, (size_t) region_bytes );\n")
	g.pf("    int64_t used = 0;\n")
	g.pf("    for ( int64_t b = 0; b < bodies; b++ )\n    {\n")
	g.pf("        roots[b] = NULL;\n")
	g.pf("        if ( !%sLoadMessageBodyInto( br.r, vocabulary, out, region, region_bytes, used, roots[b] ) ) { *count = b; return false; }\n", n)
	g.pf("        br.remaining--;\n    }\n")
	g.pf("    *count = bodies;\n")
	g.pf("    return TableMessageBatchClose( br );\n}\n\n")

}

// emitRootNodeMessageDispatch emits the three answers a MESSAGE load needs
// about a wire type id, over the members this root's numbering can name
// (docs/SPEC-TABLES.md §3.1, §3.3): the storage a record commands, the extent
// its maps take read off the bit stream, and the decode of its body.
func (g *tableGen) emitRootNodeMessageDispatch(st *ir.Struct) {
	n := st.Name
	reachable := ir.PointerReachable(st)
	blobs := reachableBlobs(st)

	g.pf("// %sNodeMessageStorage: the region bytes one record commands on the message\n", n)
	g.pf("// wire, or -1 for a type id this build cannot name. A table's is its own\n")
	g.pf("// storage plus the extent its maps take; a byte buffer's is its header and\n")
	g.pf("// its bytes, which is the one answer the record's LENGTH decides.\n")
	g.pf("inline int64_t %sNodeMessageStorage( uint64_t type_id, int64_t extent, int64_t length )\n{\n", n)
	if len(blobs) == 0 {
		g.pf("    (void) length;\n")
	}
	if len(reachable) == 0 {
		g.pf("    (void) extent;\n")
	}
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: return TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + extent ); // %s\n", ir.TableWireId(t.Name), t.Name, t.Name)
	}
	for _, b := range blobs {
		g.pf("        case %s: return TableBlobStorage( length, %v ); // *%s\n", b.constant, b.terminated, b.word)
	}
	g.pf("        default: break;\n    }\n")
	g.pf("    return -1;\n}\n\n")

	g.pf("// %sNodeMessageExtent: step over one TABLE record's body, tallying the extent\n", n)
	g.pf("// its maps take where its type has any (§2.8). A type this build cannot\n")
	g.pf("// name is stepped over by its announced shapes and takes no extent.\n")
	g.pf("inline bool %sNodeMessageExtent( uint64_t type_id, TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, int64_t & extent )\n{\n", n)
	g.pf("    extent = 0;\n")
	anyExtent := false
	for _, t := range reachable {
		if g.anyExtent && g.hasExtent(t) {
			anyExtent = true
		}
	}
	if anyExtent {
		g.pf("    switch ( type_id )\n    {\n")
		for _, t := range reachable {
			if g.anyExtent && g.hasExtent(t) {
				g.pf("        case 0x%016xull: return %sMessageExtent( r, vocabulary, index_bits, extent ); // %s\n", ir.TableWireId(t.Name), t.Name, t.Name)
			}
		}
		g.pf("        default: break;\n    }\n")
	} else {
		g.pf("    (void) type_id; // no map below any node this root can name\n")
	}
	g.pf("    return TableMessageSkipBody( r, vocabulary, index_bits );\n}\n\n")

	g.pf("// %sNodeMessageBody: PASS TWO's half — decode one record's body into the\n", n)
	g.pf("// storage it already owns, its map entries carved from its own extent.\n")
	g.pf("inline bool %sNodeMessageBody( uint64_t type_id, TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, const TableNodeMap & nodes, int64_t index_bits, uint8_t * at )\n{\n", n)
	// A LIST CARVES FROM THE NODE'S EXTENT EXACTLY AS A MAP DOES
	// (docs/SPEC-TABLES.md §2.8, §2.9), so the cursor rides wherever the unit
	// has an extent at all and not only where it has a map.
	if g.anyExtent {
		g.pf("    TableExtentCarve carve;\n")
		g.pf("    carve.at = at + %sNodeRecordBytes( type_id );\n", n)
		g.pf("    carve.left = 0;\n")
		g.pf("    {\n")
		g.pf("        // the extent this record was placed with, re-read from the framing\n")
		g.pf("        TableBitReader walk = r;\n")
		g.pf("        int64_t extent = 0;\n")
		g.pf("        if ( !%sNodeMessageExtent( type_id, walk, vocabulary, index_bits, extent ) ) { report->malformed = true; return false; }\n", n)
		g.pf("        carve.left = extent;\n")
		g.pf("    }\n")
		// THE CARVE IS THIS FRAME'S AND NEVER OUTLIVES IT: the node map's
		// cursor is restored on the way out, so the address of a local is
		// never left behind in a map the caller reads on with.
		g.pf("    TableExtentCarve * const outer = nodes.carve;\n")
		g.pf("    nodes.carve = &carve;\n")
	}
	anyVar := false
	for _, t := range reachable {
		if g.isVar(t.Name) {
			anyVar = true
		}
	}
	if !anyVar {
		g.pf("    (void) nodes; (void) index_bits; // every node this root can name is a FIXED table\n")
	}
	g.pf("    bool ok = false;\n")
	g.pf("    switch ( type_id )\n    {\n")
	for _, t := range reachable {
		g.pf("        case 0x%016xull: ok = %s; break; // %s\n", ir.TableWireId(t.Name), g.msgLoadCall(t.Name, "r", fmt.Sprintf("*(%s *) at", t.Name)), t.Name)
	}
	g.pf("        // a record this dispatch cannot name never reaches here: pass one left it absent\n")
	g.pf("        default: report->malformed = true; break;\n    }\n")
	if g.anyExtent {
		g.pf("    nodes.carve = outer;\n")
	}
	g.pf("    return ok;\n}\n\n")
}
