package tablewire

import (
	"bytes"
	"unicode/utf8"
)

// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
// well-formed UTF-8 with no zero byte among its bytes, checked as it ARRIVES
// and before the reader's own bound, because a payload that is not text is not
// text at whatever length the reader would have kept. It is SPEC.md §4.7's
// rule in this wire's idiom: the field reads its declared default, one
// `malformed` counts, and the parent reads on past L.
func textValid(b []byte) bool {
	return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0
}

// textBoundary is where a clamp cuts a well-formed payload longer than the
// reader's bound: the last whole code point that fits within `bound` bytes
// (§3, §16.2), so the clamp can never invent ill-formed storage.
func textBoundary(b []byte, bound int) int {
	if len(b) <= bound {
		return len(b)
	}
	cut := bound
	for cut > 0 && b[cut]&0xC0 == 0x80 {
		cut--
	}
	return cut
}

// wideUnits is a kind 33 payload's code units: `L / 2` of them, two bytes each
// LITTLE-ENDIAN, which is this wire's order for every fixed-width number
// (docs/SPEC-TABLES.md §3). An ODD L is framing damage and the caller rejects
// it ahead of this, because the value is L / 2 units.
func wideUnits(b []byte) []uint16 {
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
	}
	return out
}

// wideBytes is the inverse: the units back onto the wire, two bytes each
// little-endian, which is what the field's `L` frames.
func wideBytes(units []uint16) []byte {
	out := make([]byte, len(units)*2)
	for i, u := range units {
		out[i*2] = byte(u)
		out[i*2+1] = byte(u >> 8)
	}
	return out
}

// wideValid is kind 33's content rule (docs/SPEC-TABLES.md §3, §4): SURROGATES
// PAIRED and no zero code unit among the units, checked as the payload ARRIVES
// and before the reader's own bound, on the rule kind 12 takes for UTF-8. It
// is SPEC.md §4.12's read-side refusal in this wire's idiom, the one
// difference being the recovery: a packet read is terminal and a table read
// defaults-and-counts.
func wideValid(units []uint16) bool {
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u == 0:
			return false
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 >= len(units) {
				return false // a high surrogate with no low half
			}
			if low := units[i+1]; low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return false // a low surrogate first
		}
	}
	return true
}

// wideBoundary is where a clamp cuts a payload longer than the reader's bound:
// the first `bound` code units, and where the last kept unit is a HIGH
// SURROGATE whose low half did not fit, that unit is dropped with it (§3). So
// a clamp can never invent an unpaired surrogate, exactly as kind 12's clamp
// can never invent a broken sequence.
func wideBoundary(units []uint16, bound int) int {
	if len(units) <= bound {
		return len(units)
	}
	cut := bound
	if cut > 0 {
		if last := units[cut-1]; last >= 0xD800 && last <= 0xDBFF {
			cut--
		}
	}
	return cut
}
