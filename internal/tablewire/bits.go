// THE BIT STREAM the message form's bodies ride on (docs/SPEC-TABLES.md
// §3.3). It is the packet wire's own layout — bit `i` of the stream lives in
// byte `i/8` at bit position `i%8`, low bit first — so a value written here
// and a value written by a generated packet writer are the same bits in the
// same places, and the two wires can be read beside each other.
package tablewire

// bitWriter accumulates a body's bits. Nothing here allocates per field: a
// nested body is built in a writer of its own and spliced, exactly as the file
// form's encoder builds a nested body in a byte buffer and copies it, and the
// splice is what a length would otherwise have framed.
type bitWriter struct {
	b []byte
	n int // bits written
}

func (w *bitWriter) bits() int { return w.n }

// put writes the low `n` bits of v, low bit first.
func (w *bitWriter) put(v uint64, n int) {
	for i := 0; i < n; i++ {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.b[w.n/8] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

// putWide writes a 128-bit raw integer as its low half then its high half,
// which is the order every 128-bit value takes on both of this project's
// wires (docs/SPEC-TABLES.md §3, SPEC.md §4.3).
func (w *bitWriter) putWide(lo, hi uint64) {
	w.put(lo, 64)
	w.put(hi, 64)
}

// leb writes one unbounded number as a BIT LEB128: seven-bit groups, low group
// first, each preceded by a continuation bit, in its ONE canonical spelling.
// It is the file form's LEB128 at bit granularity, and it is what carries a
// number whose bound the declaration does not state — a message count, an
// unbounded array's length, the node table's payload size.
func (w *bitWriter) leb(v uint64) {
	for {
		group := v & 0x7F
		v >>= 7
		if v != 0 {
			w.put(1, 1)
			w.put(group, 7)
			continue
		}
		w.put(0, 1)
		w.put(group, 7)
		return
	}
}

// bytes writes raw payload bytes, eight bits each, low bit first — which is
// byte for byte the file form's bytes when the stream happens to be aligned,
// and the same bytes shifted when it is not.
func (w *bitWriter) bytes(p []byte) {
	for _, by := range p {
		w.put(uint64(by), 8)
	}
}

// splice appends another writer's bits, whole. A nested body has no length to
// frame it on this wire, so this is the whole of how one body joins another.
func (w *bitWriter) splice(other *bitWriter) {
	for i := 0; i < other.n; i++ {
		w.put(uint64(other.b[i/8]>>uint(i%8))&1, 1)
	}
}

// align pads to the next byte boundary with zero bits. A batch pays this ONCE,
// at its end, which is the whole of the alignment the message form spends.
func (w *bitWriter) align() {
	for w.n%8 != 0 {
		w.put(0, 1)
	}
}

// bitReader is the reading half. It carries the stream's BIT extent rather
// than its byte length: a batch's last body ends somewhere inside its last
// byte, and the padding after it is not a body.
type bitReader struct {
	b   []byte
	n   int // bits available
	off int // bits consumed
}

func newBitReader(b []byte) *bitReader { return &bitReader{b: b, n: len(b) * 8} }

func (r *bitReader) has(n int) bool { return n >= 0 && r.off+n <= r.n }

func (r *bitReader) get(n int) (uint64, bool) {
	if !r.has(n) {
		return 0, false
	}
	var v uint64
	for i := 0; i < n; i++ {
		if r.b[r.off/8]>>uint(r.off%8)&1 == 1 {
			v |= 1 << uint(i)
		}
		r.off++
	}
	return v, true
}

// leb reads a BIT LEB128 and refuses a NON-CANONICAL spelling, which is what
// §3 already says of every length, count and index on the file form: one
// number, one encoding, and a longer one is framing damage rather than a
// larger number.
func (r *bitReader) leb() (uint64, bool) {
	var v uint64
	for shift := 0; ; shift += 7 {
		more, ok := r.get(1)
		if !ok {
			return 0, false
		}
		group, ok := r.get(7)
		if !ok {
			return 0, false
		}
		if shift >= 64 || (shift == 63 && group > 1) {
			return 0, false
		}
		v |= group << uint(shift)
		if more == 0 {
			if shift > 0 && group == 0 {
				return 0, false // a trailing zero group is a longer spelling of the same number
			}
			return v, true
		}
	}
}

// skip advances past n bits without reading them.
func (r *bitReader) skip(n int) bool {
	if !r.has(n) {
		return false
	}
	r.off += n
	return true
}

// bytes reads n payload bytes back out of the stream.
func (r *bitReader) bytes(n int) ([]byte, bool) {
	if !r.has(n * 8) {
		return nil, false
	}
	out := make([]byte, n)
	for i := range out {
		v, _ := r.get(8)
		out[i] = uint8(v)
	}
	return out, true
}

// alignedTo reports the stream position rounded up to the next byte, which is
// where a batch ends.
func (r *bitReader) alignedTo() int { return (r.off + 7) / 8 * 8 }
