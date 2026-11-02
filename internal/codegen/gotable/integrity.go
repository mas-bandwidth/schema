package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

// tableIntegritySource is the integrity trailer's runtime (docs/SPEC-TABLES.md
// §2.14), emitted into the home file only when a table of the unit declares
// `integrity`, so a unit without one carries none of it.
const tableIntegritySource = `
// tableIntegrityTable is CRC-32C, the Castagnoli polynomial (docs/SPEC-TABLES.md §2.14).
var tableIntegrityTable = crc32.MakeTable(crc32.Castagnoli)

// tableIntegrityOpen is §2.14's read order for a declaring root's file: the
// form byte first, so an empty file or another form passes through to the
// open that names it; then the trailer, checked before any field is decoded.
// It answers the file without its trailer, or false with the report holding
// the whole-file damage named integrity_mismatch.
func tableIntegrityOpen(wire []byte, report *TableReport) ([]byte, bool) {
 if len(wire) == 0 || wire[0] != 1 { return wire, true }
 if tableIntegrityHolds(wire) { return wire[:len(wire)-4], true }
 if report != nil { report.Verdict = TableOpenDamaged; report.Malformed = true; report.Reason = "integrity_mismatch" }
 return nil, false
}

// tableIntegrityHolds is §2.14's check, the one both forms read through (the
// fixed form's at §3.4, THE FRAMING): a file of at least five bytes whose last
// four are the CRC-32C of every byte before them.
func tableIntegrityHolds(wire []byte) bool {
 n := len(wire) - 4
 return n >= 1 && binary.LittleEndian.Uint32(wire[n:]) == crc32.Checksum(wire[:n], tableIntegrityTable)
}

// tableIntegritySeal is §2.14's write: the trailer after a file of n bytes,
// the CRC-32C of those bytes, four bytes little-endian.
func tableIntegritySeal(buffer []byte, n int64) int64 {
 if n < 0 || n+4 > int64(len(buffer)) { return -1 }
 binary.LittleEndian.PutUint32(buffer[n:], crc32.Checksum(buffer[:n], tableIntegrityTable))
 return n + 4
}

`

// emitIntegrityMember is the variable root's file surface for a table that
// declares `integrity` (docs/SPEC-TABLES.md §2.14): the same calls as
// emitRegionMember's, the measure counting the four trailer bytes, the save
// sealing them, and every read checking them before the open.
func (g *tableGen) emitIntegrityMember(st *ir.Struct, blobmask int) {
	n, typ := st.Name, g.storageName(st.Name)
	g.pf("func %sLoadMeasure(wire []byte) int64 { wire,ok:=tableIntegrityOpen(wire,nil);if !ok {return -1};n,_,_:=tableRegionMeasure(wire,%sTableType(),%sTableType().NodeType,%d);return n }\n", n, n, n, blobmask)
	g.pf("func %sLoadMeasureReason(wire []byte)(int64,error){wire,ok:=tableIntegrityOpen(wire,nil);if !ok {return -1,TableRefuseIntegrityMismatch};size,_,_,reason:=tableRegionMeasureReason(wire,%sTableType(),%sTableType().NodeType,%d);return size,tableRefuseError(reason)}\n", n, n, n, blobmask)
	// The plain %sLoad and %sLoadBuilder are emitted by emitRegionMember as the STRICT reads over these
	// tolerant twins (docs/SPEC-TABLES.md §6.5); the trailer check runs inside each twin.
	g.pf("// %sLoadTolerant is the TOLERANT region read (docs/SPEC-TABLES.md §6.5, §2.14): the trailer is checked first,\n// then every wire the framing walk accepts returns its root, with the counters counted onto the report; damage\n// returns nil and hands back nothing (docs/SPEC-TABLES.md §4).\n", n)
	g.pf("func %sLoadTolerant(region,wire []byte,report *TableReport) *%s { wire,ok:=tableIntegrityOpen(wire,report);if !ok {return nil};return (*%s)(tableRegionLoad(region,wire,%sTableType(),%sTableType().NodeType,%d,report)) }\n", n, typ, typ, n, n, blobmask)
	g.pf("func %sMeasure(value *%s,context ...TableWriteContext) int64 { arena,allocator:=tableWriteOptions(context);size:=tableRegionSave(unsafe.Pointer(value),%sTableType(),nil,true,arena,allocator);if size<0 {return size};return size+4 }\n", n, typ, n)
	g.pf("func %sMeasureReason(value *%s,context ...TableWriteContext)(int64,error){arena,allocator:=tableWriteOptions(context);size,reason:=tableRegionSaveReason(unsafe.Pointer(value),%sTableType(),nil,true,arena,allocator);if size>=0 {size+=4};return size,tableRefuseError(reason)}\n", n, typ, n)
	g.pf("func %sSave(value *%s,buffer []byte,context ...TableWriteContext) int64 { arena,allocator:=tableWriteOptions(context);if len(buffer)<4 {return -1};return tableIntegritySeal(buffer,tableRegionSave(unsafe.Pointer(value),%sTableType(),buffer[:len(buffer)-4],false,arena,allocator)) }\n", n, typ, n)
	g.pf("// %sLoadBuilderTolerant is the TOLERANT builder read (docs/SPEC-TABLES.md §6.5, §2.14): the trailer is\n// checked first, then the value is read into the builder, with the counters counted onto the report; damage\n// answers false with the builder's root at its defaults (docs/SPEC-TABLES.md §4).\n", n)
	g.pf("func %sLoadBuilderTolerant(b *%sBuilder,wire []byte,report *TableReport) bool {wire,ok:=tableIntegrityOpen(wire,report);if !ok {return false};return tableBuilderLoad(unsafe.Pointer(b.GetRoot()),b.root,&b.Main,wire,%sTableType(),%sTableType().NodeType,%d,report)}\n", n, n, n, n, blobmask)
}

// emitIntegrityRetain is the retaining round trip's file surface for a table
// that declares `integrity` (docs/SPEC-TABLES.md §2.14, §6.6): the same three
// calls as the plain member's, with the trailer checked, counted and sealed.
func (g *tableGen) emitIntegrityRetain(st *ir.Struct, blobs int) {
	n, typ := st.Name, g.storageName(st.Name)
	g.pf("func %sLoadRetain(region,wire []byte,retain *TableRetain,report *TableReport)*%s{wire,ok:=tableIntegrityOpen(wire,report);if !ok {return nil};return (*%s)(tableRegionLoadRetain(region,wire,%sTableType(),%sTableType().NodeType,%d,report,retain))}\n", n, typ, typ, n, n, blobs)
	g.pf("func %sMeasureRetain(value *%s,retain *TableRetain,allocator ...TableAllocator)int64{size:=tableRegionSaveRetain(unsafe.Pointer(value),%sTableType(),nil,true,retain,nil,allocator...);if size<0 {return size};return size+4}\n", n, typ, n)
	g.pf("func %sSaveRetain(value *%s,retain *TableRetain,buffer []byte,report *TableReport,allocator ...TableAllocator)int64{if report==nil||len(buffer)<4{return -1};return tableIntegritySeal(buffer,tableRegionSaveRetain(unsafe.Pointer(value),%sTableType(),buffer[:len(buffer)-4],false,retain,report,allocator...))}\n", n, typ, n)
}
