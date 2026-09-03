// The wire fuzzer's MUTATORS (docs/SPEC-TABLES.md §4.2): the framing of a pinned
// wire, located so that a mutation can aim at a number rather than at a byte,
// and the passes that turn one seed into its mutants.
//
// The scanner here is a LOCATOR, not a reader. It walks §3's framing to find
// where every id, kind, length, count, key, arm id and node index sits, and
// which lengths enclose each field. It decodes no value and mirrors no
// reader's decisions — the oracle is internal/tablewire, and this file never
// says what a mutant means.
package main

import (
	"encoding/binary"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type wireSpotKind int

const (
	spotID         wireSpotKind = iota // a field's u16 id
	spotKind                           // a field's kind byte, or an array's element kind
	spotLength                         // a u32 L; limit is the end of the enclosing body
	spotCount                          // a u32 N
	spotIndex                          // a u32 node index under kind 17
	spotKey                            // an enum-keyed pair's u16 variant id
	spotArm                            // a union's u16 arm id
	spotNodeCount                      // the node table's u64 node_count
	spotRecordType                     // a node record's u64 type id
)

var spotNames = [...]string{"id", "kind", "length", "count", "index", "key", "arm", "node-count", "record-type"}

type wireSpot struct {
	kind  wireSpotKind
	off   int
	width int
	limit int // the end of the body the number is read inside
	value uint64
}

// remaining is how many bytes a length spot may claim without leaving its
// body: the number "past the body" is one more than this.
func (s wireSpot) remaining() int { return s.limit - (s.off + s.width) }

type wireField struct {
	start, end int   // id, kind and payload
	enclosing  []int // every length spot framing this field, outermost first
}

type wireFrame struct {
	spots   []wireSpot
	fields  []wireField
	records int // node records the root body carries
}

type frameScanner struct {
	data  []byte
	f     *wireFrame
	chain []int
	first bool // the next node-table field opens with node_count
}

// frameWire locates every number in one root body. It stops at the first byte
// it cannot frame, because it is only ever pointed at a pinned, valid wire.
func frameWire(data []byte) *wireFrame {
	s := &frameScanner{data: data, f: &wireFrame{}, first: true}
	s.body(0, len(data), true)
	return s.f
}

func (s *frameScanner) read(off, width int) uint64 {
	switch width {
	case 1:
		return uint64(s.data[off])
	case 2:
		return uint64(binary.LittleEndian.Uint16(s.data[off:]))
	case 4:
		return uint64(binary.LittleEndian.Uint32(s.data[off:]))
	}
	return binary.LittleEndian.Uint64(s.data[off:])
}

func (s *frameScanner) spot(kind wireSpotKind, off, width, limit int) int {
	s.f.spots = append(s.f.spots, wireSpot{kind: kind, off: off, width: width, limit: limit, value: s.read(off, width)})
	return len(s.f.spots) - 1
}

// framed opens one L-framed payload: the length spot is recorded and pushed
// onto the enclosing chain, `inner` walks the body, and the chain is popped.
// It answers the offset after the body, or -1 when the length runs past `end`.
func (s *frameScanner) framed(off, end int, inner func(start, stop int)) int {
	if off+4 > end {
		return -1
	}
	n := int(s.read(off, 4))
	ls := s.spot(spotLength, off, 4, end)
	off += 4
	if n < 0 || off+n > end {
		return -1
	}
	s.chain = append(s.chain, ls)
	if inner != nil {
		inner(off, off+n)
	}
	s.chain = s.chain[:len(s.chain)-1]
	return off + n
}

func (s *frameScanner) body(start, end int, root bool) {
	off := start
	for off+2 <= end {
		fieldStart := off
		id := uint16(s.read(off, 2))
		s.spot(spotID, off, 2, end)
		off += 2
		if id == 0 {
			return
		}
		if off+1 > end {
			return
		}
		kind := s.data[off]
		s.spot(spotKind, off, 1, end)
		off++
		switch kind {
		case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8, ir.TableKindI16, ir.TableKindU16,
			ir.TableKindI32, ir.TableKindU32, ir.TableKindF32, ir.TableKindI64, ir.TableKindU64, ir.TableKindF64:
			w := kindWidth(int(kind))
			if off+w > end {
				return
			}
			off += w
		case ir.TableKindPointer:
			if off+4 > end {
				return
			}
			s.spot(spotIndex, off, 4, end)
			off += 4
		case ir.TableKindString:
			var inner func(int, int)
			if root && id == ir.NodeTableFieldId {
				inner = s.nodeTable
			}
			off = s.framed(off, end, inner)
		case ir.TableKindTable:
			off = s.framed(off, end, func(a, b int) { s.body(a, b, false) })
		case ir.TableKindArray:
			off = s.framed(off, end, s.array)
		case ir.TableKindKeyed:
			off = s.framed(off, end, s.keyed)
		case ir.TableKindUnion:
			if off+2 > end {
				return
			}
			arm := s.read(off, 2)
			s.spot(spotArm, off, 2, end)
			off += 2
			if arm != 0 {
				off = s.framed(off, end, func(a, b int) { s.body(a, b, false) })
			}
		default:
			return
		}
		if off < 0 {
			return
		}
		s.f.fields = append(s.f.fields, wireField{start: fieldStart, end: off, enclosing: append([]int(nil), s.chain...)})
	}
}

// nodeTable walks one node-table field's payload (§3.1): node_count in the
// first, then whole records — type id, length, body — in every one.
func (s *frameScanner) nodeTable(off, end int) {
	if s.first {
		if off+8 > end {
			return
		}
		s.spot(spotNodeCount, off, 8, end)
		off += 8
		s.first = false
	}
	for off+12 <= end {
		s.spot(spotRecordType, off, 8, end)
		off += 8
		s.f.records++
		off = s.framed(off, end, func(a, b int) { s.body(a, b, false) })
		if off < 0 {
			return
		}
	}
}

// array walks an array body: element kind, N, then the elements at the
// element kind's framing.
func (s *frameScanner) array(off, end int) {
	if off+5 > end {
		return
	}
	ek := int(s.data[off])
	s.spot(spotKind, off, 1, end)
	count := int(s.read(off+1, 4))
	s.spot(spotCount, off+1, 4, end)
	off += 5
	for i := 0; i < count && off < end; i++ {
		switch ek {
		case ir.TableKindPointer:
			if off+4 > end {
				return
			}
			s.spot(spotIndex, off, 4, end)
			off += 4
		case ir.TableKindTable:
			off = s.framed(off, end, func(a, b int) { s.body(a, b, false) })
		case ir.TableKindString:
			off = s.framed(off, end, nil)
		default:
			w := kindWidth(ek)
			if w == 0 || off+w > end {
				return
			}
			off += w
		}
		if off < 0 {
			return
		}
	}
}

// keyed walks an enum-keyed body (§3.2): element kind, N, then N pairs of
// variant id, L, element.
func (s *frameScanner) keyed(off, end int) {
	if off+5 > end {
		return
	}
	ek := int(s.data[off])
	s.spot(spotKind, off, 1, end)
	count := int(s.read(off+1, 4))
	s.spot(spotCount, off+1, 4, end)
	off += 5
	for i := 0; i < count && off+2 <= end; i++ {
		s.spot(spotKey, off, 2, end)
		off += 2
		var inner func(int, int)
		if ek == ir.TableKindTable {
			inner = func(a, b int) { s.body(a, b, false) }
		}
		off = s.framed(off, end, inner)
		if off < 0 {
			return
		}
	}
}

// kindWidth is a fixed-width kind's payload width, and 0 for every other kind.
func kindWidth(kind int) int {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8:
		return 1
	case ir.TableKindI16, ir.TableKindU16:
		return 2
	case ir.TableKindI32, ir.TableKindU32, ir.TableKindF32, ir.TableKindPointer:
		return 4
	case ir.TableKindI64, ir.TableKindU64, ir.TableKindF64:
		return 8
	}
	return 0
}

// ---------------------------------------------------------------------------
// the passes
// ---------------------------------------------------------------------------

// wireSeed is one pinned wire the mutators start from: a manifest instance or
// report case, framed once.
type wireSeed struct {
	name  string
	unit  string
	root  string
	wire  []byte
	frame *wireFrame
}

// wireMutant is one input the leg and the oracle both read.
type wireMutant struct {
	seed  *wireSeed
	pass  string
	index int // the mutant's number within its pass
	data  []byte
}

func put(data []byte, off, width int, value uint64) {
	switch width {
	case 1:
		data[off] = uint8(value)
	case 2:
		binary.LittleEndian.PutUint16(data[off:], uint16(value))
	case 4:
		binary.LittleEndian.PutUint32(data[off:], uint32(value))
	case 8:
		binary.LittleEndian.PutUint64(data[off:], value)
	}
}

func patched(seed *wireSeed, sp wireSpot, value uint64) []byte {
	out := append([]byte(nil), seed.wire...)
	put(out, sp.off, sp.width, value)
	return out
}

// duplicateField copies one field's bytes in right after itself. With `fix`
// every length framing the field grows by the copy, so the framing stays
// valid and only the repeated id is the event; without it the enclosing
// lengths are one field short, which is framing damage.
func duplicateField(seed *wireSeed, fi int, fix bool) []byte {
	fld := seed.frame.fields[fi]
	n := fld.end - fld.start
	out := make([]byte, 0, len(seed.wire)+n)
	out = append(out, seed.wire[:fld.end]...)
	out = append(out, seed.wire[fld.start:fld.end]...)
	out = append(out, seed.wire[fld.end:]...)
	if fix {
		for _, si := range fld.enclosing {
			sp := seed.frame.spots[si]
			put(out, sp.off, sp.width, sp.value+uint64(n))
		}
	}
	return out
}

// enumerated yields every deterministic mutant of one seed, in a fixed order,
// whatever N is: these are the mutants that aim at the checks by name, and
// they cost nothing to run.
func enumerated(seed *wireSeed, emit func(pass string, data []byte)) {
	f := seed.frame
	wire := seed.wire

	// every truncation length: the reader must stop at each one
	for n := 0; n < len(wire); n++ {
		emit("truncate", append([]byte(nil), wire[:n]...))
	}

	records := uint64(f.records)
	// the ids the seed carries, in wire order, so a rename-to-neighbor mutant
	// is the same one every run
	var ids []uint64
	for _, sp := range f.spots {
		if sp.kind == spotID && sp.value != 0 {
			ids = append(ids, sp.value)
		}
	}
	for _, sp := range f.spots {
		switch sp.kind {
		case spotKind:
			// every kind byte swapped to every other value, the two outside
			// the closed set included: 0 and 18 are not skippable
			for k := uint64(0); k <= uint64(ir.TableKindPointer)+1; k++ {
				if k != sp.value {
					emit("kind", patched(seed, sp, k))
				}
			}
		case spotLength:
			rem := uint64(sp.remaining())
			for _, v := range []uint64{0, 1, sp.value - 1, sp.value + 1, rem, rem + 1, rem + 2, 0x7FFFFFFF, 0xFFFFFFFF} {
				if v != sp.value && v <= 0xFFFFFFFF {
					emit("length", patched(seed, sp, v))
				}
			}
		case spotCount:
			rem := uint64(sp.remaining())
			for _, v := range []uint64{0, sp.value + 1, sp.value*2 + 1, rem, rem + 1, 0x80000000, 0xFFFFFFFF} {
				if v != sp.value {
					emit("count", patched(seed, sp, v))
				}
			}
		case spotIndex:
			// null, the root, the first record, the last, the two past the
			// last, and the two the sign bit and the width can spell
			for _, v := range []uint64{0, 1, 2, records, records + 1, records + 2, records + 3, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF} {
				if v != sp.value {
					emit("index", patched(seed, sp, v))
				}
			}
		case spotID:
			if sp.value == 0 {
				continue
			}
			candidates := []uint64{0, uint64(ir.NodeTableFieldId), sp.value ^ 1}
			for _, other := range ids {
				if other != sp.value {
					candidates = append(candidates, other)
					break
				}
			}
			for _, v := range candidates {
				if v != sp.value {
					emit("id", patched(seed, sp, v))
				}
			}
		case spotKey:
			for _, v := range []uint64{0, sp.value ^ 1, 0xFFFF} {
				if v != sp.value {
					emit("key", patched(seed, sp, v))
				}
			}
		case spotArm:
			for _, v := range []uint64{0, sp.value ^ 1, 0xFFFF} {
				if v != sp.value {
					emit("arm", patched(seed, sp, v))
				}
			}
		case spotNodeCount:
			for _, v := range []uint64{0, sp.value + 1, sp.value - 1, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF} {
				if v != sp.value {
					emit("node-count", patched(seed, sp, v))
				}
			}
		case spotRecordType:
			for _, v := range []uint64{0, sp.value ^ 1, 0xFFFFFFFFFFFFFFFF} {
				if v != sp.value {
					emit("record-type", patched(seed, sp, v))
				}
			}
		}
	}

	for fi := range f.fields {
		emit("duplicate", duplicateField(seed, fi, true))
		emit("duplicate-unframed", duplicateField(seed, fi, false))
	}
}

// ---------------------------------------------------------------------------
// the random pass
// ---------------------------------------------------------------------------

// splitmix64 is the generator, written out so that a mutant is the same bytes
// under every Go release: every mutant is a pure function of (seed, index).
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *splitmix64) below(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// randomMutant is mutant `index` of the random pass: a seed chosen by the
// generator, then one to three strategies stacked on it.
func randomMutant(seeds []*wireSeed, seed uint64, index int) *wireMutant {
	r := &splitmix64{state: seed ^ (uint64(index) * 0xD1B54A32D192ED03)}
	s := seeds[r.below(len(seeds))]
	data := append([]byte(nil), s.wire...)
	steps := 1 + r.below(3)
	for range steps {
		data = randomStep(r, s, seeds, data)
	}
	return &wireMutant{seed: s, pass: "random", index: index, data: data}
}

func randomStep(r *splitmix64, s *wireSeed, seeds []*wireSeed, data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	switch r.below(9) {
	case 0: // bit flips
		for range 1 + r.below(8) {
			i := r.below(len(data))
			data[i] ^= 1 << r.below(8)
		}
	case 1: // byte overwrites
		for range 1 + r.below(4) {
			data[r.below(len(data))] = uint8(r.next())
		}
	case 2: // truncation
		data = data[:r.below(len(data))]
	case 3: // an inserted run
		at := r.below(len(data) + 1)
		run := make([]byte, 1+r.below(8))
		for i := range run {
			run[i] = uint8(r.next())
		}
		data = append(data[:at], append(run, data[at:]...)...)
	case 4: // a deleted run
		at := r.below(len(data))
		n := 1 + r.below(8)
		if at+n > len(data) {
			n = len(data) - at
		}
		data = append(data[:at], data[at+n:]...)
	case 5, 6: // a framing number set to a value the strategy picks
		if len(s.frame.spots) == 0 || len(data) != len(s.wire) {
			data[r.below(len(data))] = uint8(r.next())
			break
		}
		sp := s.frame.spots[r.below(len(s.frame.spots))]
		var v uint64
		switch r.below(4) {
		case 0:
			v = r.next()
		case 1:
			v = sp.value + 1 + uint64(r.below(4))
		case 2:
			v = uint64(sp.remaining()) + 1 + uint64(r.below(4))
		case 3:
			v = uint64(r.below(20))
		}
		put(data, sp.off, sp.width, v)
	case 7: // a window spliced in from another seed of the same unit
		var pool []*wireSeed
		for _, o := range seeds {
			if o.unit == s.unit && len(o.wire) > 0 {
				pool = append(pool, o)
			}
		}
		if len(pool) == 0 {
			break
		}
		o := pool[r.below(len(pool))]
		from := r.below(len(o.wire))
		n := 1 + r.below(16)
		if from+n > len(o.wire) {
			n = len(o.wire) - from
		}
		at := r.below(len(data) + 1)
		window := append([]byte(nil), o.wire[from:from+n]...)
		data = append(data[:at], append(window, data[at:]...)...)
	case 8: // a field duplicated, framing fixed or not
		if len(s.frame.fields) == 0 || len(data) != len(s.wire) {
			break
		}
		fixed := duplicateField(s, r.below(len(s.frame.fields)), r.below(2) == 0)
		data = fixed
	}
	return data
}
