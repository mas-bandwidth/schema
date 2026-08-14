// Package pack is the data compiler: it walks the unit IR against JSON
// instance files and encodes schema wire bytes directly — no generated code
// in the loop, one static binary (SPEC §1's compiler-linker-for-data frame).
//
// The bit-level contract is SPEC §4.3's: bit i of the stream lives in byte
// i/8, at bit position i%8 (LSB-first within each byte) — the same layout the
// serialize family's 32-bit little-endian word packer produces. The golden
// test pins this against the corpus wire goldens the five generated runtimes
// already byte-match, so a divergence here cannot survive `go test`.
package pack

import "math/big"

// bitWriter accumulates a wire stream. Bit-at-a-time on purpose: data
// compilation is small and offline, and the simple form is the auditable one.
type bitWriter struct {
	buf  []byte
	bits int64
}

func (w *bitWriter) writeBits(value uint64, n int64) {
	for i := int64(0); i < n; i++ {
		p := w.bits + i
		for int64(len(w.buf)) <= p/8 {
			w.buf = append(w.buf, 0)
		}
		if (value>>uint(i))&1 == 1 {
			w.buf[p/8] |= 1 << uint(p%8)
		}
	}
	w.bits += n
}

// writeBigBits writes the low n bits of a NON-NEGATIVE value, least
// significant bit first — writeBits past 64 bits, for the 128-bit family and
// wide fixed point. STANDARD.md splits those into "full 32-bit groups from the
// least significant upward, the final group carrying the remainder"; a group
// is itself written LSB-first at increasing stream offsets, so the whole
// stream is exactly the value's bits in LSB-first order and the split needs no
// separate expression here (SPEC §4.3's bit-order rule states the same thing).
func (w *bitWriter) writeBigBits(value *big.Int, n int64) {
	for i := int64(0); i < n; i++ {
		p := w.bits + i
		for int64(len(w.buf)) <= p/8 {
			w.buf = append(w.buf, 0)
		}
		if value.Bit(int(i)) == 1 {
			w.buf[p/8] |= 1 << uint(p%8)
		}
	}
	w.bits += n
}

// align zero-pads to the next byte boundary (a no-op when already aligned).
func (w *bitWriter) align() {
	if pad := (8 - w.bits%8) % 8; pad > 0 {
		w.writeBits(0, pad)
	}
}

// bytes returns the stream's bytes: ceil(bits/8), zero-padded in the final
// partial byte — GetBytesProcessed's shape.
func (w *bitWriter) bytes() []byte {
	n := (w.bits + 7) / 8
	for int64(len(w.buf)) < n {
		w.buf = append(w.buf, 0)
	}
	return w.buf[:n]
}
