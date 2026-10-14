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

type layoutFrame struct {
	entry           layoutEntry
	remainingKids   uint32
	sum             uint64
	widest          uint32
	firstSize       uint32
	secondSize      uint32
	firstChildren   uint32
	firstKind       uint8
	kidsAreVariants bool
}

type layoutCheck struct {
	bytes []byte
	count int32
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
	// Root requirements: kind 13, 0 < size <= 65536
	root := c.entryAt(0)
	if root.kind != 13 {
		return "layout_kind_invalid"
	}
	if root.size == 0 || root.size > layoutRecordMaxBytes {
		return "layout_record_too_large"
	}

	var stack []layoutFrame

	for cursor := int32(0); cursor < c.count; cursor++ {
		depth := int32(len(stack))
		if depth > layoutMaxDepth {
			return "layout_too_deep"
		}
		e := c.entryAt(cursor)
		if !layoutKindKnown(e.kind) {
			return "layout_kind_unknown"
		}
		if e.size > layoutRecordMaxBytes {
			return "layout_record_too_large"
		}

		if e.children > 0 {
			stack = append(stack, layoutFrame{
				entry:           e,
				remainingKids:   e.children,
				kidsAreVariants: true,
			})
			continue
		}

		// e.children == 0: check post-children rules
		if reason := checkPostChildren(e, 0, 0, 0, 0, 0, 0, true); reason != "" {
			return reason
		}

		completed := e
		for len(stack) > 0 {
			parent := &stack[len(stack)-1]
			k := parent.entry.children - parent.remainingKids
			if k == 0 {
				parent.firstSize = completed.size
				parent.firstKind = completed.kind
				parent.firstChildren = completed.children
			}
			if k == 1 {
				parent.secondSize = completed.size
			}
			if completed.kind != 32 {
				parent.kidsAreVariants = false
			}
			parent.sum += uint64(completed.size)
			if completed.size > parent.widest {
				parent.widest = completed.size
			}
			if parent.sum > uint64(layoutRecordMaxBytes) {
				return "layout_record_too_large"
			}

			parent.remainingKids--
			if parent.remainingKids > 0 {
				break
			}

			if reason := checkPostChildren(parent.entry, parent.sum, parent.widest, parent.firstSize, parent.secondSize, parent.firstChildren, parent.firstKind, parent.kidsAreVariants); reason != "" {
				return reason
			}
			completed = parent.entry
			stack = stack[:len(stack)-1]
		}

		if len(stack) == 0 && cursor+1 < c.count {
			return "layout_tree_unclosed"
		}
	}

	if len(stack) > 0 {
		return "layout_tree_unclosed"
	}

	return ""
}

func checkPostChildren(e layoutEntry, sum uint64, widest, firstSize, secondSize, firstChildren uint32, firstKind uint8, kidsAreVariants bool) string {
	admitted, leaf := layoutLeafSize(e.kind, e.size)
	if !admitted {
		return "layout_size_mismatch"
	}
	if leaf {
		if e.children != 0 {
			return "layout_kind_invalid"
		}
		return ""
	}

	switch e.kind {
	case 13: // table: size is sum of fields
		if uint64(e.size) != sum {
			return "layout_size_mismatch"
		}
	case 35: // optional wrapper: 1 child, size == sum + 1
		if e.children != 1 {
			return "layout_kind_invalid"
		}
		if uint64(e.size) != sum+1 {
			return "layout_size_mismatch"
		}
	case 14: // array: 1 child, element size != 0, bare or counted
		if e.children != 1 {
			return "layout_kind_invalid"
		}
		if firstSize == 0 {
			return "layout_size_mismatch"
		}
		bare := e.size%firstSize == 0
		counted := e.size >= 4 && (e.size-4)%firstSize == 0
		if !bare && !counted {
			return "layout_size_mismatch"
		}
	case 16: // enum-keyed array: 2 children, first kind 30, element size != 0, slots >= variants
		if e.children != 2 {
			return "layout_kind_invalid"
		}
		if firstKind != 30 {
			return "layout_kind_invalid"
		}
		if secondSize == 0 || e.size%secondSize != 0 {
			return "layout_size_mismatch"
		}
		if e.size/secondSize < firstChildren {
			return "layout_size_mismatch"
		}
	case 15: // union: >= 1 child, size > widest, size - widest is ordinal width
		if e.children == 0 {
			return "layout_kind_invalid"
		}
		if e.size <= widest {
			return "layout_size_mismatch"
		}
		if !layoutOrdinalWidth(e.size - widest) {
			return "layout_size_mismatch"
		}
	case 30: // enum: size is ordinal width, children are variants (kind 32)
		if !layoutOrdinalWidth(e.size) {
			return "layout_size_mismatch"
		}
		if e.children != 0 && !kidsAreVariants {
			return "layout_kind_invalid"
		}
	case 12: // string(N): length + N bytes, size >= 4, no children
		if e.children != 0 {
			return "layout_kind_invalid"
		}
		if e.size < 4 {
			return "layout_size_mismatch"
		}
	case 33: // wstring(N): length + 2N bytes, size >= 4 and size-4 even, no children
		if e.children != 0 {
			return "layout_kind_invalid"
		}
		if e.size < 4 || (e.size-4)%2 != 0 {
			return "layout_size_mismatch"
		}
	default:
		return "layout_kind_unknown"
	}
	return ""
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
