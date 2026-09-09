package gotable

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The write vocabulary is bounded by the schema, while a reader's vocabulary
// belongs to the input. Keep the former on the stack and view the latter in
// the caller's buffer. Neither path needs a map or a heap allocation.
func tableWireRuntime(u *ir.Unit) string {
	n := ir.TableWireIdCapacity(u)
	buckets := 1 << bits.Len(uint(n*2-1))
	source := tableWireSource
	if len(variableTableNames(u)) > 0 {
		source = strings.Replace(source, "type TableIds struct {", "type TableIds struct {\nNumbering *TableNumbering", 1)
		source = strings.Replace(source, "type TableReader struct {", "type TableReader struct {\nNodes TableNodeMap", 1)
		source = strings.ReplaceAll(source, "Ids:r.Ids, Nested:true", "Ids:r.Ids, Nested:true, Nodes:r.Nodes")
	}
	res := fmt.Sprintf(`
const tableIdCapacity = %d
const tableIdBuckets = %d
`, n, buckets) + source
	if len(ir.WideTextFields(u)) > 0 {
		res += tableWStringSource
	}
	return res
}

const tableWireSource = `
// TableIds interns the writer's ids in first-use order. Bucket links are
// one-based so its zero value is ready to use, including for hash zero.
type TableIds struct {
	Values [tableIdCapacity]uint64
	chain [tableIdCapacity]int
	head [tableIdBuckets]int
	slot [tableIdCapacity]uint32
	ordinalOf [tableIdCapacity]int32
	Count int
	Overflow bool
}

func (ids *TableIds) Ref(id uint64) uint64 {
	b := (id ^ id >> 32) & (tableIdBuckets - 1)
	for i := ids.head[b]; i != 0; i = ids.chain[i-1] {
		if ids.Values[i-1] == id { return uint64(i) }
	}
	if ids.Count == len(ids.Values) { ids.Overflow = true; return 0 }
	i := ids.Count
	ids.Values[i], ids.chain[i] = id, ids.head[b]
	ids.ordinalOf[i] = -1
	ids.Count++
	ids.head[b] = ids.Count
	return uint64(ids.Count)
}

// RefAt is Ref for a compile-time-constant id. The ordinal is that id's index
// in TableWireIds. A hit is one load and one compare; first-use order is
// still append order. Truncate clears the ordinal in O(popped) so a
// speculative intern does not leak into the next walk.
func (ids *TableIds) RefAt(ordinal int, id uint64) uint64 {
	if uint(ordinal) < uint(len(ids.slot)) {
		if s := ids.slot[ordinal]; s != 0 {
			return uint64(s)
		}
	}
	r := ids.Ref(id)
	if r != 0 && uint(ordinal) < uint(len(ids.slot)) {
		ids.slot[ordinal] = uint32(r)
		ids.ordinalOf[r-1] = int32(ordinal)
	}
	return r
}

// Truncate removes speculative ids when a measured field elides, or before
// writing the payload whose size was just measured under the same vocabulary.
func (ids *TableIds) Truncate(mark int) {
	for ids.Count > mark {
		ids.Count--
		id := ids.Values[ids.Count]
		ids.head[(id ^ id >> 32) & (tableIdBuckets - 1)] = ids.chain[ids.Count]
		if o := ids.ordinalOf[ids.Count]; o >= 0 {
			ids.slot[o] = 0
		}
	}
}

func tableLebBytes(v uint64) int64 {
	n := int64(1)
	for v >= 128 { n++; v >>= 7 }
	return n
}

// TableWriter writes into the caller's buffer, or counts exactly the same
// bytes in Measuring mode. Offset and every length are checked before use.
type TableWriter struct {
	Buffer []byte
	Offset int64
	Overflow bool
	Measuring bool
	Ids *TableIds
}

func (w *TableWriter) Advance(n int64) bool {
	if n < 0 || w.Offset < 0 || n > 0x7fffffffffffffff-w.Offset {
		w.Overflow = true
		return false
	}
	if !w.Measuring && (w.Offset > int64(len(w.Buffer)) || n > int64(len(w.Buffer))-w.Offset) {
		w.Overflow = true
		return false
	}
	w.Offset += n
	return true
}

func (w *TableWriter) Raw(data []byte) {
	at := w.Offset
	if w.Advance(int64(len(data))) && !w.Measuring { copy(w.Buffer[at:], data) }
}

func (w *TableWriter) Put8(v uint8) {
	at := w.Offset
	if w.Advance(1) && !w.Measuring { w.Buffer[at] = v }
}

func (w *TableWriter) Put16(v uint16) {
	at := w.Offset
	if w.Advance(2) && !w.Measuring { binary.LittleEndian.PutUint16(w.Buffer[at:], v) }
}

func (w *TableWriter) Put32(v uint32) {
	at := w.Offset
	if w.Advance(4) && !w.Measuring { binary.LittleEndian.PutUint32(w.Buffer[at:], v) }
}

func (w *TableWriter) Put64(v uint64) {
	at := w.Offset
	if w.Advance(8) && !w.Measuring { binary.LittleEndian.PutUint64(w.Buffer[at:], v) }
}

// Put128 writes two little-endian lanes, low first, under one Advance.
func (w *TableWriter) Put128(lo, hi uint64) {
	at := w.Offset
	if w.Advance(16) && !w.Measuring {
		binary.LittleEndian.PutUint64(w.Buffer[at:], lo)
		binary.LittleEndian.PutUint64(w.Buffer[at+8:], hi)
	}
}

func (w *TableWriter) PutLeb(v uint64) {
	if v < 128 {
		w.Put8(uint8(v))
		return
	}
	// Groups assemble in a ten-byte stack buffer and go to Raw once. Spelling
	// is the old loop's, group for group. One Advance covers the value; raw's
	// capacity test is untouched. A value that does not fit now leaves the
	// buffer alone rather than filling it first; Save answers -1.
	var b [10]byte
	n := 0
	for v >= 128 { b[n] = uint8(v) | 128; v >>= 7; n++ }
	b[n] = uint8(v)
	n++
	w.Raw(b[:n])
}

func (w *TableWriter) Id(id uint64) { w.PutLeb(w.Ids.Ref(id)) }
func (w *TableWriter) IdAt(ordinal int, id uint64) { w.PutLeb(w.Ids.RefAt(ordinal, id)) }

// Header writes a field's reference and kind. A reference below 128 is one
// LEB byte, so the pair is one two-byte store. Larger references still go
// through PutLeb then Put8. Bytes match that two-call spelling; a pair that
// does not fit now leaves the buffer alone, and Save answers -1.
func (w *TableWriter) Header(ref uint64, kind uint8) {
	if ref < 128 {
		w.Put16(uint16(ref) | uint16(kind)<<8)
		return
	}
	w.PutLeb(ref)
	w.Put8(kind)
}

func (w *TableWriter) Trailer() {
	for i := 0; i < w.Ids.Count; i++ { w.Put64(w.Ids.Values[i]) }
	w.Put64(uint64(w.Ids.Count))
}

// TableOpenVerdict distinguishes an unsupported form from framing damage.
type TableOpenVerdict uint8
const (
	TableOpenOk TableOpenVerdict = iota
	TableOpenRefused
	TableOpenDamaged
	TableOpenBodyStopped
)

// TableReader holds one bounded body and a view of the file's id trailer.
// Sub-readers share the trailer and report, but cannot consume sibling bytes.
type TableReader struct {
	Buffer []byte
	Offset int64
	Report *TableReport
	Ids []byte
	Nested bool
}

func (r *TableReader) Has(n int64) bool {
	return n >= 0 && r.Offset >= 0 && r.Offset <= int64(len(r.Buffer)) && n <= int64(len(r.Buffer))-r.Offset
}

func (r *TableReader) Room(n uint64) bool {
	return r.Has(0) && n <= uint64(int64(len(r.Buffer))-r.Offset)
}

func (r *TableReader) Get8() uint8 { v := r.Buffer[r.Offset]; r.Offset++; return v }
func (r *TableReader) Get16() uint16 { v := binary.LittleEndian.Uint16(r.Buffer[r.Offset:]); r.Offset += 2; return v }
func (r *TableReader) Get32() uint32 { v := binary.LittleEndian.Uint32(r.Buffer[r.Offset:]); r.Offset += 4; return v }
func (r *TableReader) Get64() uint64 { v := binary.LittleEndian.Uint64(r.Buffer[r.Offset:]); r.Offset += 8; return v }

// A rejected integer leaves the cursor unchanged. Its spelling must be
// minimal and fit in 64 bits, including the tenth byte's single value bit.
func (r *TableReader) Leb() (uint64, bool) {
	// One byte below 128 is a complete canonical value: the continuation bit
	// is clear, it is the first byte so the redundant-continuation rule has
	// nothing to say, and one byte is neither overlong nor past ten. No rule
	// is relaxed; every other number goes to the loop with all of its checks.
	if uint64(r.Offset) < uint64(len(r.Buffer)) {
		b := r.Buffer[r.Offset]
		if b < 128 {
			r.Offset++
			return uint64(b), true
		}
	}
	at := r.Offset
	var v uint64
	for i := 0; i < 10; i++ {
		if !r.Has(1) { break }
		b := r.Get8()
		if i == 9 && b > 1 { break }
		v |= uint64(b & 127) << (7*i)
		if b < 128 {
			if i > 0 && b == 0 { break }
			return v, true
		}
	}
	r.Offset = at
	return 0, false
}

func (r *TableReader) Resolve(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(r.Ids)/8) { return 0, false }
	return binary.LittleEndian.Uint64(r.Ids[(ref-1)*8:]), true
}

func (r *TableReader) Sub(n int64) TableReader {
	return TableReader{Buffer:r.Buffer[r.Offset:r.Offset+n], Report:r.Report, Ids:r.Ids, Nested:true}
}

func (r *TableReader) Body() (TableReader, bool) {
	n, ok := r.Leb()
	if !ok || !r.Room(n) { return TableReader{}, false }
	sub := r.Sub(int64(n))
	r.Offset += int64(n)
	return sub, true
}

func tableKindBytes(kind uint8) int64 {
	switch kind {
	case 1,2,6,20,25: return 1
	case 3,7,21,26: return 2
	case 4,8,10,22,27: return 4
	case 5,9,11,23,28: return 8
	case 18,19,24,29: return 16
	}
	return 0
}

func (r *TableReader) Skip(kind uint8) bool {
	if n := tableKindBytes(kind); n != 0 {
		if !r.Has(n) { return false }; r.Offset += n; return true
	}
	switch kind {
	case 17,30:
		_, ok := r.Leb(); return ok
	case 12,13,14,16,31,32,33:
		_, ok := r.Body(); return ok
	case 15:
		ref, ok := r.Leb()
		if !ok { return false }
		if ref == 0 { return true }
		if !r.Has(1) { return false }
		r.Offset++
		_, ok = r.Body(); return ok
	}
	return false
}

// EndsEarly only recognizes an otherwise walkable body's early terminator.
// Other damage is decoded normally, preserving the prefix already read.
func (r TableReader) EndsEarly() bool {
	for {
		ref, ok := r.Leb()
		if !ok { return false }
		if ref == 0 { return r.Offset != int64(len(r.Buffer)) }
		if _, ok = r.Resolve(ref); !ok || !r.Has(1) { return false }
		if !r.Skip(r.Get8()) { return false }
	}
}

func tableOpen(data []byte, report *TableReport) (TableReader, TableOpenVerdict) {
	r := TableReader{Report:report}
	if len(data) == 0 { return r, TableOpenDamaged }
	if data[0] != 1 { return r, TableOpenRefused }
	if len(data) < 9 { return r, TableOpenDamaged }
	n := binary.LittleEndian.Uint64(data[len(data)-8:])
	if n > uint64((len(data)-9)/8) { return r, TableOpenDamaged }
	start := len(data)-8-int(n)*8
	r.Buffer, r.Ids = data[1:start], data[start:len(data)-8]
	// Count is attacker-capped by (bytes-9)/8. The open-addressed path runs
	// only at 8<=n<=256 with 512 slots, compares the stored id not the hash,
	// and caps probes at the slot count. n<8 stays pairwise: 21 comparisons
	// at 7 versus 4.6 KiB of slot zeroing on every open, including a two-entry
	// trailer. Exhausted probes and n>256 keep the pairwise walk, which is
	// the same verdict. The mix is C's table_writer_id.
	if n > 1 {
		pairwise := n < 8 || n > 256
		if !pairwise {
			var seen [512]uint64
			var used [512]uint8
			for i := uint64(0); i < n; i++ {
				id := binary.LittleEndian.Uint64(r.Ids[i*8:])
				hash := id
				hash ^= hash >> 33
				hash *= 0xff51afd7ed558ccd
				hash ^= hash >> 33
				slot := uint32(hash) & 511
				probes := uint32(0)
				for ; probes < 512; probes++ {
					if used[slot] == 0 { used[slot] = 1; seen[slot] = id; break }
					if seen[slot] == id { return r, TableOpenDamaged }
					slot = (slot + 1) & 511
				}
				if probes == 512 { pairwise = true; break }
			}
		}
		if pairwise {
			for i := 0; i < len(r.Ids); i += 8 {
				id := binary.LittleEndian.Uint64(r.Ids[i:])
				for j := 0; j < i; j += 8 {
					if id == binary.LittleEndian.Uint64(r.Ids[j:]) { return r, TableOpenDamaged }
				}
			}
		}
	}
	return r, TableOpenOk
}

// tableOpenFramed is tableOpen plus the framing pre-walk. Variable-class Load,
// LoadMeasure, LoadBuilder and the announcement still answer an early end this
// way; their real walk is a node scan or the announcement, a different question.
// schema #773 left those sites with the pre-walk. The fixed root Load does not
// use this: it answers the early end from its own cursor after LoadBody.
func tableOpenFramed(data []byte, report *TableReport) (TableReader, TableOpenVerdict) {
	r, verdict := tableOpen(data, report)
	if verdict == TableOpenOk && r.EndsEarly() { return r, TableOpenDamaged }
	return r, verdict
}

func tableKindWidens(from, to uint8) bool {
	return (from >= 2 && from <= 5 && (to > from && to <= 5 || to == 18)) ||
		(from >= 6 && from <= 9 && (to > from && to <= 9 || to == 19)) || from == 10 && to == 11
}

func (r *TableReader) Unsigned(kind uint8) uint64 {
 switch tableKindBytes(kind) { case 1: return uint64(r.Get8()); case 2: return uint64(r.Get16()); case 4: return uint64(r.Get32()); default: return r.Get64() }
}

func (r *TableReader) Signed(kind uint8) int64 {
 switch tableKindBytes(kind) { case 1: return int64(int8(r.Get8())); case 2: return int64(int16(r.Get16())); case 4: return int64(int32(r.Get32())); default: return int64(r.Get64()) }
}

func tableWidenFloat(b uint32) uint64 {
 if b & 0x7f800000 == 0x7f800000 { return uint64(b >> 31) << 63 | 0x7ff0000000000000 | uint64(b & 0x7fffff) << 29 }
 return math.Float64bits(float64(math.Float32frombits(b)))
}

func tableUtf8Valid(data []byte) bool {
	if !utf8.Valid(data) { return false }
	for _, b := range data { if b == 0 { return false } }
	return true
}

func tableUtf8Clamp(data []byte, n int64) int64 {
	for n > 0 && n < int64(len(data)) && data[n]&0xc0 == 0x80 { n-- }
	return n
}
`
