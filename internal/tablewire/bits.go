// THE BIT STREAM the message form's bodies ride on (docs/SPEC-TABLES.md
// §3.3). It is the packet wire's own layout, bit `i` of the stream living in
// byte `i/8` at bit position `i%8` with the low bit first, so a value written
// here and a value written by a generated packet writer are the same bits in
// the same places, and the two wires can be read beside each other.
package tablewire

import "math/big"

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
	for i := range n {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.b[w.n/8] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

// putBig writes the low `n` bits of a wide value, which is what a 128-bit
// kind's ranged offset takes.
func (w *bitWriter) putBig(v *big.Int, n int) {
	for i := range n {
		w.put(uint64(v.Bit(i)), 1)
	}
}

// bytes writes raw payload bytes, eight bits each, low bit first, which is
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

// align pads to the next byte boundary with zero bits. A batch pays this at
// its end, and a `string(N)` or a `bytes(N)` payload pays it before its bytes,
// which buys a memcpy on the largest payload on the wire.
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

// left is the bits still unread. It bounds what a COUNT off the wire can
// honestly claim, so a reader sizes its storage against the stream it holds
// rather than against a number hostile bytes chose.
func (r *bitReader) left() int64 { return int64(r.n - r.off) }

func (r *bitReader) get(n int) (uint64, bool) {
	if !r.has(n) {
		return 0, false
	}
	var v uint64
	for i := range n {
		if r.b[r.off/8]>>uint(r.off%8)&1 == 1 {
			v |= 1 << uint(i)
		}
		r.off++
	}
	return v, true
}

// getBig reads a wide value's low `n` bits.
func (r *bitReader) getBig(n int) (*big.Int, bool) {
	out := new(big.Int)
	for i := range n {
		bit, ok := r.get(1)
		if !ok {
			return nil, false
		}
		out.SetBit(out, i, uint(bit))
	}
	return out, true
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

// align steps past the pad to the next byte boundary and VERIFIES it is zero,
// which is the packet wire's rule for the same reason (SPEC.md §4.3).
func (r *bitReader) align() bool {
	for r.off%8 != 0 {
		bit, ok := r.get(1)
		if !ok || bit != 0 {
			return false
		}
	}
	return true
}
