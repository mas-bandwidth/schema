package lockfile

import (
	"encoding/binary"
)

// The layout's wire format and bounds (docs/FIXED-FORM-ALGORITHM.md §1.1).
const (
	layoutHeaderBytes    = 4
	layoutEntryBytes     = 17
	layoutRecordMaxBytes = 65536
	layoutMaxDepth       = 64
)

type layoutEntry struct {
	id       uint64
	kind     uint8
	size     uint32
	children uint32
}

type layoutCheck struct {
	bytes []byte
	count int32
	why   string
}

func (c *layoutCheck) fail(why string) {
	if c.why == "" {
		c.why = why
	}
}

func (c *layoutCheck) entryAt(i int32) layoutEntry {
	if i < 0 || i >= c.count {
		return layoutEntry{kind: 0xFF}
	}
	off := layoutHeaderBytes + int(i)*layoutEntryBytes
	e := c.bytes[off : off+layoutEntryBytes]
	return layoutEntry{
		id:       binary.LittleEndian.Uint64(e[0:8]),
		kind:     e[8],
		size:     binary.LittleEndian.Uint32(e[9:13]),
		children: binary.LittleEndian.Uint32(e[13:17]),
	}
}

// ValidateLayout validates a layout's bytes against the seven §1.1 rules
// of docs/FIXED-FORM-ALGORITHM.md. It returns the refusal reason by name,
// or empty string if the layout is valid.
func ValidateLayout(bytes []byte) string {
	// Residue: fewer than 4-byte header or absent
	if len(bytes) < layoutHeaderBytes {
		return "layout_malformed"
	}
	// 1. Rule 1: count fits length exactly and count != 0
	count := binary.LittleEndian.Uint32(bytes[0:4])
	if count == 0 || int64(count)*int64(layoutEntryBytes)+int64(layoutHeaderBytes) != int64(len(bytes)) {
		return "layout_count_mismatch"
	}
	c := layoutCheck{bytes: bytes, count: int32(count)}
	// Root requirements: kind 13, size <= 65536
	root := c.entryAt(0)
	if root.kind != 13 {
		return "layout_kind_invalid"
	}
	if root.size > layoutRecordMaxBytes {
		return "layout_record_too_large"
	}
	// Walk the pre-order tree
	used := c.checkEntry(0, 0)
	if c.why != "" {
		return c.why
	}
	// Rule 5: walk consumes exactly count entries
	if used != c.count {
		return "layout_tree_unclosed"
	}
	return ""
}

func (c *layoutCheck) checkEntry(i, depth int32) int32 {
	if c.why != "" {
		return 0
	}
	if i < 0 || i >= c.count {
		c.fail("layout_tree_unclosed")
		return 0
	}
	if depth > layoutMaxDepth {
		c.fail("layout_too_deep")
		return 0
	}
	e := c.entryAt(i)
	if !layoutKindKnown(e.kind) {
		c.fail("layout_kind_unknown")
		return 0
	}
	if e.size > layoutRecordMaxBytes {
		c.fail("layout_record_too_large")
		return 0
	}

	at := i + 1
	var sum uint64
	var widest, firstSize, secondSize, firstChildren uint32
	var firstKind uint8
	kidsAreVariants := true
	for k := uint32(0); k < e.children; k++ {
		child := at
		used := c.checkEntry(child, depth+1)
		if c.why != "" {
			return 0
		}
		ce := c.entryAt(child)
		if k == 0 {
			firstSize, firstKind, firstChildren = ce.size, ce.kind, ce.children
		}
		if k == 1 {
			secondSize = ce.size
		}
		if ce.kind != 32 {
			kidsAreVariants = false
		}
		sum += uint64(ce.size)
		if ce.size > widest {
			widest = ce.size
		}
		if sum > uint64(layoutRecordMaxBytes) {
			c.fail("layout_record_too_large")
			return 0
		}
		at += used
	}

	admitted, leaf := layoutLeafSize(e.kind, e.size)
	if !admitted {
		c.fail("layout_size_mismatch")
		return 0
	}
	if leaf {
		if e.children != 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		return at - i
	}

	switch e.kind {
	case 13: // table: size is sum of fields
		if uint64(e.size) != sum {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 35: // optional wrapper: 1 child, size == sum + 1
		if e.children != 1 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if uint64(e.size) != sum+1 {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 14: // array: 1 child, element size != 0, bare or counted
		if e.children != 1 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if firstSize == 0 {
			c.fail("layout_size_mismatch")
			return 0
		}
		bare := e.size%firstSize == 0
		counted := e.size >= 4 && (e.size-4)%firstSize == 0
		if !bare && !counted {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 16: // enum-keyed array: 2 children, first kind 30, element size != 0, slots >= variants
		if e.children != 2 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if firstKind != 30 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if secondSize == 0 || e.size%secondSize != 0 {
			c.fail("layout_size_mismatch")
			return 0
		}
		if e.size/secondSize < firstChildren {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 15: // union: >= 1 child, size > widest, size - widest is ordinal width
		if e.children == 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if e.size <= widest {
			c.fail("layout_size_mismatch")
			return 0
		}
		if !layoutOrdinalWidth(e.size - widest) {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 30: // enum: size is ordinal width, children are variants (kind 32)
		if !layoutOrdinalWidth(e.size) {
			c.fail("layout_size_mismatch")
			return 0
		}
		if e.children != 0 && !kidsAreVariants {
			c.fail("layout_kind_invalid")
			return 0
		}
	case 12: // string(N): length + N bytes, size >= 4, no children
		if e.children != 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if e.size < 4 {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 33: // wstring(N): length + 2N bytes, size >= 4 and size-4 even, no children
		if e.children != 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if e.size < 4 || (e.size-4)%2 != 0 {
			c.fail("layout_size_mismatch")
			return 0
		}
	default:
		c.fail("layout_kind_unknown")
		return 0
	}
	return at - i
}

func layoutKindKnown(kind uint8) bool {
	if kind >= 1 && kind <= 30 {
		return true
	}
	return kind == 32 || kind == 33 || kind == 35
}

func layoutOrdinalWidth(n uint32) bool {
	return n == 1 || n == 2 || n == 4 || n == 8
}

func layoutLeafSize(kind uint8, size uint32) (admitted, leaf bool) {
	leaf = true
	switch kind {
	case 1, 2: // bool, i8
		return size == 1, leaf
	case 3: // i16
		return size == 2, leaf
	case 4: // i32
		return size == 4, leaf
	case 5: // i64
		return size == 8, leaf
	case 6: // u8, and bits( 1 .. 8 )
		return size == 1 || size == 4, leaf
	case 7: // u16, and bits( 9 .. 16 )
		return size == 2 || size == 4, leaf
	case 8: // u32, and bits( 17 .. 32 )
		return size == 4, leaf
	case 9: // u64, flags, and bits( 33 .. 64 )
		return size == 8, leaf
	case 10: // f32
		return size == 4, leaf
	case 11: // f64
		return size == 8, leaf
	case 17: // pointer index
		return size == 4, leaf
	case 18, 19: // i128, u128
		return size == 16, leaf
	case 20, 25: // fixed8, ufixed8
		return size == 1, leaf
	case 21, 26: // fixed16, ufixed16
		return size == 2, leaf
	case 22, 27: // fixed32, ufixed32
		return size == 4, leaf
	case 23, 28: // fixed64, ufixed64
		return size == 8, leaf
	case 24, 29: // fixed128, ufixed128
		return size == 16, leaf
	case 32: // variant, and payload-free arm
		return size == 0, leaf
	}
	return true, false
}
