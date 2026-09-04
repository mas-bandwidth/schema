// THE ONE NUMBER SHAPE (docs/SPEC-TABLES.md §3): every length, count, index
// and id reference on this wire is an unsigned LEB128 — seven value bits a
// byte, the lowest group first, with the high bit set on every byte but the
// last — and it is 64 bits in capability, so no body, count or index of any
// kind has a ceiling below 2^64 − 1.
//
// Every one of them is CANONICAL, and a non-minimal encoding is MALFORMED.
// `0x80 0x00` and `0x00` both spell zero, and only the second is legal input:
// one value has one spelling, so two conforming writers agree byte for byte
// and a reader has one thing to check rather than a range of paddings to
// tolerate.
package tablewire

// lebSize is the byte count one value takes, which a measure needs before the
// bytes exist.
func lebSize(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

// appendLeb writes one value in its one legal spelling.
func appendLeb(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, uint8(v)|0x80)
		v >>= 7
	}
	return append(b, uint8(v))
}

// readLeb decodes one value at `off`. ok is false where the encoding runs off
// the end of the buffer, past ten bytes, or is NON-MINIMAL — a tenth byte with
// a bit above the 64th value bit is malformed on the same rule.
func readLeb(buf []byte, off int) (v uint64, next int, ok bool) {
	shift := uint(0)
	for i := 0; ; i++ {
		if off >= len(buf) || i >= 10 {
			return 0, off, false
		}
		b := buf[off]
		off++
		if i == 9 && b > 1 {
			return 0, off, false // a tenth byte above the 64th value bit
		}
		v |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			if i > 0 && b == 0 {
				return 0, off, false // a redundant continuation: 0x80 0x00 is not zero's spelling
			}
			return v, off, true
		}
		shift += 7
	}
}
