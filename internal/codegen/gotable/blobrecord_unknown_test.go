package gotable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A blob record is a blob in every reader's record walk (docs/SPEC-TABLES.md
// §3.1, §4): a unit that declares no *bytes or *string still reads a lawful
// file from a newer writer that carries one under a field it does not know,
// counts it unknown, and agrees with its measure. A damaged blob record is
// still refused.
const blobUnknownSchema = `package probe
fixed table Leaf { v int32 = 0 }
table Node {
 n int32 = 0 | min = 0, max = 100
 next *Node
 leaf *Leaf
}
`

const blobBytesOnlySchema = `package probe
table BlobNode {
 n int32 = 0
 next *BlobNode
 data *bytes
}
`

const blobUnknownPrelude = `package probe
import ("testing"; "unsafe")

func region(n int64) []byte {
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

func body(ids *TableIds, f func(w *TableWriter)) []byte {
	w := TableWriter{Buffer: make([]byte, 4096), Ids: ids}
	f(&w)
	if w.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

type record struct { typ uint64; body []byte }

func framed(w *TableWriter, id uint64, kind uint8, payload []byte) { w.ID(id); w.Put8(kind); w.PutLeb(uint64(len(payload))); w.Raw(payload) }

func ptr(w *TableWriter, id uint64, index uint64) { w.ID(id); w.Put8(17); w.PutLeb(index) }

func file(ids *TableIds, root func(w *TableWriter), records ...record) []byte {
	w := TableWriter{Buffer: make([]byte, 8192), Ids: ids}
	w.Put8(1)
	root(&w)
	if len(records) > 0 {
		payload := body(ids, func(p *TableWriter) {
			p.PutLeb(uint64(len(records)))
			for _, r := range records { p.ID(r.typ); p.PutLeb(uint64(len(r.body))); p.Raw(r.body) }
		})
		framed(&w, 0xffffffffffffffff, 12, payload)
	}
	w.Put8(0)
	w.Trailer()
	if w.Overflow || ids.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

// rawFile writes the node table's payload from its own builder, so a record's
// declared length can be made to run past its framing.
func rawFile(ids *TableIds, root func(w *TableWriter), payload func(p *TableWriter)) []byte {
	w := TableWriter{Buffer: make([]byte, 8192), Ids: ids}
	w.Put8(1)
	root(&w)
	framed(&w, 0xffffffffffffffff, 12, body(ids, payload))
	w.Put8(0)
	w.Trailer()
	if w.Overflow || ids.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}
`

const blobUnknownConditions = `
func field(i int) uint64 { return NodeTableFields[i].ID }

const (fN, fNext = 0, 1)

func TestUnknownBlobSizeCapIsIndependentOfMask(t *testing.T) {
	t.Parallel()
	for _, id := range []uint64{tableBytesTypeID, tableStringTypeID} {
		const maxBlobLength = int64((uint64(1) << 32) - 1)
		n, reason, blob := tableBlobStorageReason(id, maxBlobLength, 0)
		if !blob || n != -1 || reason != TableRefuseOk {
			t.Fatalf("type id %016x at cap: storage=%d reason=%v blob=%v, want -1/ok/true", id, n, reason, blob)
		}
		n, reason, blob = tableBlobStorageReason(id, maxBlobLength+1, 0)
		if !blob || n != -2 || reason != TableRefuseBlobOverSizeCap {
			t.Fatalf("type id %016x over cap: storage=%d reason=%v blob=%v, want -2/blob_over_size_cap/true", id, n, reason, blob)
		}
	}
}

func TestBlobRecordUnderUnknownField(t *testing.T) {
	var ids TableIds
	// two unknown fields and two records this reader cannot name: four events
	wire := file(&ids, func(w *TableWriter) {
		w.ID(field(fN)); w.Put8(4); w.Put32(7)
		ptr(w, dataField, 2)
		ptr(w, noteField, 3)
	}, record{blobBytesID, []byte("abc")}, record{blobStringID, []byte("hello")})
	size := NodeLoadMeasure(wire)
	if size < 0 { t.Fatalf("measure refused a lawful newer file: %d", size) }
	var r TableReport
	root := NodeLoad(region(size), wire, &r)
	if root == nil { t.Fatalf("Load refused a bytes and a string record under unknown fields: %+v", r) }
	if r.Err() != nil || r.Unknown != 4 { t.Fatalf("Load unknown=%d err=%v, want accepted unknown 4", r.Unknown, r.Err()) }
	var rr TableReport
	if NodeLoadRegion(region(size), wire, &rr) == nil { t.Fatalf("LoadRegion refused the same file: %+v", rr) }
	if rr.Err() != nil || rr.Unknown != 4 { t.Fatalf("LoadRegion unknown=%d err=%v, want accepted unknown 4", rr.Unknown, rr.Err()) }
	v, err := NodeReadStrict(wire)
	if err != nil || v == nil { t.Fatalf("ReadStrict refused the same file: %v", err) }
	// one unknown field naming one blob record is two events, for both ids and
	// for an empty byte string
	for _, tc := range []struct { name string; rec record }{
		{"bytes", record{blobBytesID, []byte("abc")}},
		{"empty bytes", record{blobBytesID, nil}},
		{"string", record{blobStringID, []byte("hello")}},
	} {
		var one TableIds
		single := file(&one, func(w *TableWriter) { ptr(w, dataField, 2) }, tc.rec)
		n := NodeLoadMeasure(single)
		if n < 0 { t.Fatalf("%s: measure refused a lawful newer file: %d", tc.name, n) }
		var sr TableReport
		if NodeLoad(region(n), single, &sr) == nil { t.Fatalf("%s: Load refused: %+v", tc.name, sr) }
		if sr.Err() != nil || sr.Unknown != 2 { t.Fatalf("%s: Load unknown=%d err=%v, want accepted unknown 2", tc.name, sr.Unknown, sr.Err()) }
		if _, err := NodeReadStrict(single); err != nil { t.Fatalf("%s: ReadStrict refused: %v", tc.name, err) }
	}
}

func TestDamagedBlobRecordRefused(t *testing.T) {
	var ids TableIds
	// ill-formed text in a string record is damage, declared or not
	badText := file(&ids, func(w *TableWriter) { ptr(w, noteField, 2) }, record{blobStringID, []byte{0xff}})
	if n := NodeLoadMeasure(badText); n >= 0 { t.Fatalf("measure accepted an ill-formed string record: %d", n) }
	var r TableReport
	if NodeLoad(region(1<<16), badText, &r) != nil { t.Fatalf("Load accepted an ill-formed string record: %+v", r) }
	if _, err := NodeReadStrict(badText); err == nil { t.Fatal("ReadStrict accepted an ill-formed string record") }
	// a record length past its framing is damage
	badLen := rawFile(&ids, func(w *TableWriter) { ptr(w, dataField, 2) }, func(p *TableWriter) {
		p.PutLeb(1)
		p.ID(blobBytesID)
		p.PutLeb(100)
		p.Raw([]byte("abc"))
	})
	if n := NodeLoadMeasure(badLen); n >= 0 { t.Fatalf("measure accepted a record length past its framing: %d", n) }
	var rl TableReport
	if NodeLoad(region(1<<16), badLen, &rl) != nil { t.Fatalf("Load accepted a record length past its framing: %+v", rl) }
	if _, err := NodeReadStrict(badLen); err == nil { t.Fatal("ReadStrict accepted a record length past its framing") }
}
`

const blobBytesOnlyConditions = `
func fieldB(i int) uint64 { return BlobNodeTableFields[i].ID }

const (bN = 0)

func TestStringRecordUnderUnknownField(t *testing.T) {
	var ids TableIds
	wire := file(&ids, func(w *TableWriter) {
		w.ID(fieldB(bN)); w.Put8(4); w.Put32(1)
		ptr(w, noteField, 2)
	}, record{blobStringID, []byte("hello")})
	size := BlobNodeLoadMeasure(wire)
	if size < 0 { t.Fatalf("measure refused a string record under an unknown field: %d", size) }
	var r TableReport
	root := BlobNodeLoad(region(size), wire, &r)
	if root == nil { t.Fatalf("Load refused a string record under an unknown field: %+v", r) }
	if r.Err() != nil || r.Unknown != 2 { t.Fatalf("Load unknown=%d err=%v, want accepted unknown 2", r.Unknown, r.Err()) }
	var rr TableReport
	if BlobNodeLoadRegion(region(size), wire, &rr) == nil { t.Fatalf("LoadRegion refused the same file: %+v", rr) }
	if rr.Err() != nil || rr.Unknown != 2 { t.Fatalf("LoadRegion unknown=%d err=%v, want accepted unknown 2", rr.Unknown, rr.Err()) }
	v, err := BlobNodeReadStrict(wire)
	if err != nil || v == nil { t.Fatalf("ReadStrict refused the same file: %v", err) }
	// the blob this unit does declare, under a field it does not know, is a
	// blob it can place; the field is the one unknown event
	known := file(&ids, func(w *TableWriter) {
		w.ID(fieldB(bN)); w.Put8(4); w.Put32(1)
		ptr(w, noteField, 2)
	}, record{blobBytesID, []byte("abc")})
	var kr TableReport
	if BlobNodeLoad(region(max(BlobNodeLoadMeasure(known), 0)), known, &kr) == nil { t.Fatalf("Load refused a declared blob under an unknown field: %+v", kr) }
	if kr.Err() != nil || kr.Unknown != 1 { t.Fatalf("declared blob unknown=%d err=%v, want accepted unknown 1", kr.Unknown, kr.Err()) }
}
`

func blobWireIds() string {
	return fmt.Sprintf("const blobBytesID uint64 = 0x%016x\nconst blobStringID uint64 = 0x%016x\nconst dataField uint64 = 0x%016x\nconst noteField uint64 = 0x%016x\n",
		ir.TableWireId("bytes"), ir.TableWireId("string"), ir.TableWireId("data"), ir.TableWireId("note"))
}

func TestGoBlobRecordUnderUnknownFieldReads(t *testing.T) {
	t.Parallel()
	ids := blobWireIds()
	t.Run("blob-free reader", func(t *testing.T) {
		t.Parallel()
		runGeneratedTables(t, blobUnknownSchema, blobUnknownPrelude+ids+blobUnknownConditions)
	})
	t.Run("bytes reader with no string", func(t *testing.T) {
		t.Parallel()
		runGeneratedTables(t, blobBytesOnlySchema, blobUnknownPrelude+ids+blobBytesOnlyConditions)
	})
}
