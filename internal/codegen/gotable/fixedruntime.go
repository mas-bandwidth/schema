package gotable

// tableFixedRuntime is the FIXED FORM's package-scoped runtime
// (docs/SPEC-TABLES.md §3.4): the plan entry, the plan builder, the ONE read
// loop, the layout reader and the plan compiler. Emitted into the unit's home
// Table.go, once.
//
// THE WORD IS LAYOUT, not block (Glenn 2026-09-09 20:17Z). The form byte
// versions the file; there is no version byte inside the layout. A layout
// whose bytes are not a layout is layout_malformed. An unknown kind is refused
// (Glenn, same sitting).
//
// FILE HEADER pinned to the C++ reference (docs/SPEC-TABLES.md §3 THE FIRST BYTE):
// form byte 3 at 0, seven reserved zeros, layout hash at 8, body at 16, then
// the layout behind its u32 length, then the records. The child's 5-byte
// header was the older tree; cc6e5953 writes this one.
const tableFixedRuntime = `
const TableFixedForm uint8 = 3
const TableFixedHeaderBytes = 16
const TableFixedHashAt = 8

const (
	tableFixedCopy uint8 = iota
	tableFixedCount
	tableFixedText
	tableFixedOrdinal
	tableFixedWiden
	tableFixedConst
	tableFixedWidenF
	tableFixedBool
	tableFixedTag
)

const (
	tableFixedTextUtf8 uint8 = 1
	tableFixedTextWide uint8 = 2
	tableFixedTextBytes uint8 = 3
)

const tableFixedNoGuard uint32 = 0xFFFFFFFF
const tableFixedEntryBytes int32 = 17

// THE LAYOUT'S OWN BOUNDS (docs/FIXED-FORM-ALGORITHM.md §1.1). The header is
// the u32 entry count and nothing else. 65536 is the form's record body bound,
// which a DECLARED fixed table is held to at compile time and an untrusted
// peer's layout is held to here, so the two sides agree by construction. The
// depth is a bound on THIS READER'S WALK and not on the wire: the reference
// states 64 and every leg states the same number.
const tableFixedLayoutHeaderBytes int32 = 4
const tableFixedRecordMaxBytes uint32 = 65536
const tableFixedMaxDepth int32 = 64

// Arg is the guard's arm ordinal: tableFixedRun compares src[Guard] against
// it. Meta is a text entry's flavour (tableFixedTextUtf8/Wide/Bytes). They
// shared one lane until reference-fix 12; compiling a string under a union
// arm overwrote the guard with the flavour and the read skipped the string.
// tableFixedEntryBytes is the FILE layout entry (id+kind+size+children),
// not this in-memory plan row — Meta does not ride the wire.
// ArgW IS THE GUARD'S WIDTH IN BYTES (§5.2 THE GUARD'S WIDTH): a union tag is
// one, two, four or eight bytes, clamped 1..8, and 0 reads as 1. The guard is
// tested at that FULL width and never at its first byte — a foreign two-byte
// tag 0x0101 is not arm 1. Arg is the arm ordinal and carries NO BYTE LANE
// (§5.8 row 5): a 256-arm union's ordinal does not fit a byte.
type TableFixedEntry struct {
	Src, Dst, Size, Aux, Guard, Arg uint32
	Op, ArgW, DstSize, Sign, Meta uint8
}

type TableFixedDst struct {
	Dst, Stride, Aux uint32
	Counted, Arg, Meta uint8
}

type tableFixedPlan struct {
	Entries []TableFixedEntry
	Count int32
}

func tableFixedWidens(from, to uint8) bool {
	if from >= 6 && from <= 9 && to >= 6 && to <= 9 {
		return to > from
	}
	if from >= 2 && from <= 5 && to >= 2 && to <= 5 {
		return to > from
	}
	// THE FIXED-POINT RUNGS, signed 20..24 and unsigned 25..29: a fixed(I,F)
	// into a wider I at equal F is a LADDER widen and not a kind that moved
	// (docs/FIXED-FORM-ALGORITHM.md §1, §5.1).
	if from >= 20 && from <= 24 && to >= 20 && to <= 24 {
		return to > from
	}
	if from >= 25 && from <= 29 && to >= 25 && to <= 29 {
		return to > from
	}
	return from == 10 && to == 11
}

// A WIDEN'S SIGN IS THE LADDER'S: the source sign-extends when the WRITER's
// kind is signed — i8..i64 and a SIGNED fixed(I,F), 20..24 — and zero-extends
// otherwise (§5.2).
func tableFixedSignedKind(kind uint8) bool {
	return (kind >= 2 && kind <= 5) || (kind >= 20 && kind <= 24)
}

// THE CLOSED KIND SET (docs/FIXED-FORM-ALGORITHM.md §1): §3's own kinds 1..30,
// the no-payload variant 32, wstring 33, and the ONE kind this form's layout
// adds, the optional wrapper 35. 0 is no kind at all, 31 is §3's BODY framing
// escape and 34 is reserved: none of the three is a kind a declaration spells,
// so none is ever an entry's kind. A KIND OUTSIDE THIS SET IS A REFUSAL, not a
// leaf to step over — the form byte versions the kinds behind it, so a kind
// this build does not know names a FORM this reader never saw.
func tableFixedKindKnown(kind uint8) bool {
	if kind >= 1 && kind <= 30 {
		return true
	}
	return kind == 32 || kind == 33 || kind == 35
}

// tableFixedOrdinalWidth is the widths an ordinal — an enum's, a union tag's —
// is stored at.
func tableFixedOrdinalWidth(n uint32) bool { return n == 1 || n == 2 || n == 4 || n == 8 }

// tableFixedLeafSize answers whether a kind is a LEAF and, if it is, whether
// the size it states is one its kind admits (§1.2). Kind and size fix each
// other on this wire with ONE exception, stated here rather than left to be
// found: a bits(N) field rides at its DECLARED STORAGE WIDTH — four bytes for
// N <= 32 and eight above — under the unsigned integer kind its BIT COUNT
// picks, so kinds 6 and 7 admit 4 as well as their own width.
func tableFixedLeafSize(kind uint8, size uint32) (admitted, leaf bool) {
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
	case 17: // a pointer index, which no fixed table carries
		return size == 4, leaf
	case 18, 19: // i128, u128
		return size == 16, leaf
	case 20, 25: // fixed8, ufixed8
		return size == 1, leaf
	case 21, 26:
		return size == 2, leaf
	case 22, 27:
		return size == 4, leaf
	case 23, 28:
		return size == 8, leaf
	case 24, 29:
		return size == 16, leaf
	case 32: // a variant, and an arm that holds nothing
		return size == 0, leaf
	}
	return true, false
}

// tableFixedRuns is whether an op advances src and dst together one byte at a
// time, which is the whole of what lets two adjacent entries become one.
func tableFixedRuns(op uint8) bool { return op == tableFixedCopy || op == tableFixedBool }

// THE GUARD'S WIDTH, clamped (§5.2): 0 reads as 1, and nothing above 8 is a
// width an ordinal is stored at.
func tableFixedArgWidth(w uint8) uint32 {
	if w == 0 {
		return 1
	}
	if w > 8 {
		return 8
	}
	return uint32(w)
}

// tableFixedTagAt loads the guard WHOLE, little-endian, at its own width. A
// one-byte compare fires arm 1 on a foreign 0x0101; this is the compare.
func tableFixedTagAt(src []byte, at uint32, w uint8) uint64 {
	n := tableFixedArgWidth(w)
	var v uint64
	for k := uint32(0); k < n; k++ {
		v |= uint64(src[at+k]) << (8 * k)
	}
	return v
}

func tableFixedBuildPlan(emit func([]TableFixedEntry, uint32, uint32) int, n int) tableFixedPlan {
	raw := make([]TableFixedEntry, n)
	written := emit(raw, 0, 0)
	out := 0
	for i := 0; i < written; i++ {
		if out > 0 && raw[out-1].Op == raw[i].Op && tableFixedRuns(raw[i].Op) &&
			raw[out-1].Guard == raw[i].Guard && raw[out-1].Arg == raw[i].Arg &&
			raw[out-1].ArgW == raw[i].ArgW && raw[out-1].Meta == raw[i].Meta &&
			raw[out-1].Src+raw[out-1].Size == raw[i].Src &&
			raw[out-1].Dst+raw[out-1].Size == raw[i].Dst {
			raw[out-1].Size += raw[i].Size
			continue
		}
		raw[out] = raw[i]
		out++
	}
	return tableFixedPlan{Entries: raw[:out], Count: int32(out)}
}

func tableFixedPut8(b []byte, v uint8) { b[0] = v }
func tableFixedPut16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func tableFixedPut32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func tableFixedPut64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }
func tableFixedPutF32(b []byte, v float32) { tableFixedPut32(b, math.Float32bits(v)) }
func tableFixedPutF64(b []byte, v float64) { tableFixedPut64(b, math.Float64bits(v)) }
func tableFixedPut128(b []byte, lo, hi uint64) { tableFixedPut64(b, lo); tableFixedPut64(b[8:], hi) }
func tableFixedGet32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func tableFixedGet64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

func tableFixedCopyRun(d, s []byte, n uint32) {
	if n == 0 {
		return
	}
	if n <= 16 {
		if n >= 8 {
			a := binary.LittleEndian.Uint64(s)
			b := binary.LittleEndian.Uint64(s[n-8:])
			binary.LittleEndian.PutUint64(d, a)
			binary.LittleEndian.PutUint64(d[n-8:], b)
		} else if n >= 4 {
			a := binary.LittleEndian.Uint32(s)
			b := binary.LittleEndian.Uint32(s[n-4:])
			binary.LittleEndian.PutUint32(d, a)
			binary.LittleEndian.PutUint32(d[n-4:], b)
		} else {
			d[0] = s[0]
			d[n>>1] = s[n>>1]
			d[n-1] = s[n-1]
		}
		return
	}
	copy(d[:n], s[:n])
}

func tableFixedRun(plan []TableFixedEntry, count int32, src, dst []byte, report *TableReport) {
	for i := int32(0); i < count; i++ {
		p := plan[i]
		// THE GUARD AT ITS FULL WIDTH, never its first byte (§5.2).
		if p.Guard != tableFixedNoGuard && tableFixedTagAt(src, p.Guard, p.ArgW) != uint64(p.Arg) {
			continue
		}
		switch p.Op {
		case tableFixedCopy:
			tableFixedCopyRun(dst[p.Dst:], src[p.Src:], p.Size)
		case tableFixedTag:
			// A UNION TAG NAMING NO ARM LANDS AS None AND COUNTS UNKNOWN,
			// which is what form 1 does with an arm id it does not know
			// (docs/SPEC-TABLES.md §4). Copied raw it would land as a
			// discriminant no variant spells, and every arm's guard would
			// then decline to fill it — a value that is neither None nor an
			// arm. Aux is the reader's own arm count.
			var tag uint64
			for k := uint32(0); k < p.Size; k++ {
				tag |= uint64(src[p.Src+k]) << (8 * k)
			}
			if tag > uint64(p.Aux) {
				tag = 0
				report.Unknown++
			}
			for k := uint32(0); k < p.Size; k++ {
				dst[p.Dst+k] = byte(tag >> (8 * k))
			}
		case tableFixedBool:
			// A GO bool IS NOT A BYTE. A Go true is the byte 1 (v == true,
			// array equality). if v is TESTB (nonzero) on amd64 and TBZ bit
			// 0 on arm64, so a hostile 2 reads true on one and false on the
			// other. The wire says nonzero is true (docs/SPEC-TABLES.md §3);
			// normalise here (byte != 0 -> 1) and never copy.
			for k := uint32(0); k < p.Size; k++ {
				v := byte(0)
				if src[p.Src+k] != 0 {
					v = 1
				}
				dst[p.Dst+k] = v
			}
		case tableFixedCount:
			v := int32(tableFixedGet32(src[p.Src:]))
			if v < 0 {
				v = 0
				report.Clamped++
			} else if v > int32(p.Size) {
				v = int32(p.Size)
				report.Clamped++
			}
			binary.LittleEndian.PutUint32(dst[p.Dst:], uint32(v))
		case tableFixedText:
			unit := uint32(1)
			if p.Meta == tableFixedTextWide {
				unit = 2
			}
			capn := p.Size / unit
			v := int32(tableFixedGet32(src[p.Src:]))
			if v < 0 {
				v = 0
				report.Clamped++
			} else if uint32(v) > capn {
				v = int32(capn)
				report.Clamped++
			}
			binary.LittleEndian.PutUint32(dst[p.Dst:], uint32(v))
			tableFixedCopyRun(dst[p.Aux:], src[p.Src+4:], p.Size)
			if p.Meta != tableFixedTextBytes && uint32(v) < capn {
				off := p.Aux + uint32(v)*unit
				dst[off] = 0
				if unit == 2 {
					dst[off+1] = 0
				}
			}
		case tableFixedOrdinal:
			// SIXTY-FOUR BITS. An ordinal rides at 1, 2, 4 or 8 bytes (§1.2);
			// read through a 32-bit temporary an eight-byte ordinal loses its
			// top four bytes, so a forged 0x1_0000_0001 would land as 1.
			raw := tableFixedTagAt(src, p.Src, uint8(p.Size))
			p16 := (*uint16)(unsafe.Add(unsafe.Pointer(&plan[0]), uintptr(p.Aux)))
			table := unsafe.Slice(p16, int(*p16)+1)
			// THE OP LANDS THE RAW VALUE AND COUNTS NOTHING (§5.9 #27). A
			// raw INSIDE the writer's table is remapped to the reader's
			// ordinal; a raw PAST the writer's variant count lands as
			// ITSELF, UNREMAPPED. The BOUNDS PASS over storage is what
			// clamps an ordinal past the READER's extent to None and counts
			// it clamped — the same pass for the identity plan and for this
			// one (§3.4). A pass over storage cannot tell a forged None from
			// a real one, so the raw must survive the op to reach it. The
			// lock guarantees the raw fits the reader's storage, because
			// widths only grow.
			v := raw
			if raw != 0 && raw <= uint64(table[0]) {
				v = uint64(table[raw])
			}
			switch p.DstSize {
			case 1:
				dst[p.Dst] = uint8(v)
			case 2:
				binary.LittleEndian.PutUint16(dst[p.Dst:], uint16(v))
			case 8:
				binary.LittleEndian.PutUint64(dst[p.Dst:], v)
			default:
				binary.LittleEndian.PutUint32(dst[p.Dst:], uint32(v))
			}
		case tableFixedWiden:
			var raw uint64
			for i := uint32(0); i < p.Size; i++ {
				raw |= uint64(src[p.Src+i]) << (8 * i)
			}
			if p.Sign != 0 {
				bits := p.Size * 8
				top := uint64(1) << (bits - 1)
				if raw&top != 0 {
					raw |= ^((top << 1) - 1)
				}
			}
			for i := uint8(0); i < p.DstSize; i++ {
				dst[p.Dst+uint32(i)] = byte(raw >> (8 * i))
			}
			report.Widened++
		case tableFixedWidenF:
			f := math.Float32frombits(tableFixedGet32(src[p.Src:]))
			tableFixedPutF64(dst[p.Dst:], float64(f))
			report.Widened++
		case tableFixedConst:
			aux := p.Aux
			for i := uint32(0); i < p.Size; i++ {
				dst[p.Dst+i] = byte(aux >> (8 * i))
			}
		}
	}
}

type tableFixedLayoutEntry struct {
	Id uint64
	Size, Children uint32
	Kind uint8
}
type tableFixedLayoutView struct {
	Bytes []byte
	Count int32
}

// AN INDEX PAST THE ENTRIES IS ANSWERED, NOT READ. Every index into a layout is
// arithmetic over child counts a STRANGER wrote, so the one place that can hold
// the whole walk inside the buffer is the one place that touches it. An
// out-of-range index answers kind 0xFF, which is a kind no declaration has and
// nothing matches, so the walk that asked for it finds nothing and moves on.
func tableFixedEntryAt(b tableFixedLayoutView, i int32) tableFixedLayoutEntry {
	if b.Bytes == nil || i < 0 || i >= b.Count {
		return tableFixedLayoutEntry{Kind: 0xFF}
	}
	e := b.Bytes[int64(tableFixedLayoutHeaderBytes)+int64(i)*int64(tableFixedEntryBytes):]
	return tableFixedLayoutEntry{
		Id: tableFixedGet64(e),
		Kind: e[8],
		Size: tableFixedGet32(e[9:]),
		Children: tableFixedGet32(e[13:]),
	}
}

// tableFixedSubtree is how many entries the subtree rooted at i occupies, so a
// walk steps over a child it does not want without knowing what is in it.
//
// ITERATIVE ON PURPOSE (docs/FIXED-FORM-ALGORITHM.md §1.1). A layout is a
// stranger's bytes, so a chain of single-child entries is a stack depth the WIRE
// gets to choose; this walk gives it none. The pending counter is the pre-order
// walk's own arithmetic: one entry is owed at the start, each entry owes its
// children, and the subtree ends when nothing is owed.
func tableFixedSubtree(b tableFixedLayoutView, i int32) int32 {
	if i < 0 || i >= b.Count {
		return 1
	}
	pending := int64(1)
	n := int32(0)
	at := i
	for pending > 0 && at < b.Count {
		pending--
		n++
		pending += int64(tableFixedEntryAt(b, at).Children)
		at++
		if pending > int64(b.Count) {
			break
		}
	}
	return n
}

func tableFixedUnionArmBytes(b tableFixedLayoutView, i int32) uint32 {
	e := tableFixedEntryAt(b, i)
	var widest uint32
	at := i + 1
	for k := uint32(0); k < e.Children && at < b.Count; k++ {
		a := tableFixedEntryAt(b, at)
		if a.Size > widest {
			widest = a.Size
		}
		at += tableFixedSubtree(b, at)
	}
	return widest
}

// ---- THE LAYOUT'S OWN VALIDATION, RULE BY NAMED RULE -----------------------
//
// A LAYOUT ARRIVES FROM AN UNTRUSTED PEER and it is the one structure a reader
// must parse before it knows anything at all, so every rule below runs BEFORE a
// single record byte is touched and each refuses under ITS OWN NAME rather than
// under one word for all of them (docs/FIXED-FORM-ALGORITHM.md §1.1).
//
// NOR IS THERE A CYCLE RULE, because a pre-order walk cannot express a cycle: an
// entry's children ARE THE ENTRIES THAT FOLLOW IT, so a child's index is always
// higher than its parent's, the layout is finite, and there is no back reference
// for a cycle to be made of. The tree-closure rule is what bounds the walk, and
// the depth rule is what bounds the reader's stack.
type tableFixedCheck struct {
	layout tableFixedLayoutView
	why string
}

// Keep the FIRST reason and stop: the order of the rules is load-bearing, so the
// reason a reader reports is the first one the layout broke.
func (c *tableFixedCheck) fail(why string) {
	if c.why == "" {
		c.why = why
	}
}

// tableFixedCheckEntry validates the subtree rooted at i and answers how many
// entries it occupies, which is the same arithmetic tableFixedSubtree does and
// is why that walk is safe to run afterwards and only afterwards.
func tableFixedCheckEntry(c *tableFixedCheck, i, depth int32) int32 {
	if c.why != "" {
		return 0
	}
	if i < 0 || i >= c.layout.Count {
		c.fail("layout_tree_unclosed")
		return 0
	}
	if depth > tableFixedMaxDepth {
		c.fail("layout_too_deep")
		return 0
	}
	e := tableFixedEntryAt(c.layout, i)
	// THE CLOSED KIND SET, FIRST: a kind outside it is a layout of another form
	// and is refused whole, never walked and never stepped over.
	if !tableFixedKindKnown(e.Kind) {
		c.fail("layout_kind_unknown")
		return 0
	}
	if e.Size > tableFixedRecordMaxBytes {
		c.fail("layout_record_too_large")
		return 0
	}

	// THE CHILDREN FIRST, in the pre-order the layout is written in.
	at := i + 1
	var sum uint64
	var widest, firstSize, secondSize, firstChildren uint32
	var firstKind uint8
	kidsAreVariants := true
	for k := uint32(0); k < e.Children; k++ {
		child := at
		used := tableFixedCheckEntry(c, child, depth+1)
		if c.why != "" {
			return 0
		}
		ce := tableFixedEntryAt(c.layout, child)
		if k == 0 {
			firstSize, firstKind, firstChildren = ce.Size, ce.Kind, ce.Children
		}
		if k == 1 {
			secondSize = ce.Size
		}
		if ce.Kind != 32 {
			kidsAreVariants = false
		}
		// THE SUM IS 64 BITS precisely so a u32 that overflows is CAUGHT rather
		// than wrapped into a small number that then agrees with its parent.
		sum += uint64(ce.Size)
		if ce.Size > widest {
			widest = ce.Size
		}
		if sum > uint64(tableFixedRecordMaxBytes) {
			c.fail("layout_record_too_large")
			return 0
		}
		at += used
	}

	admitted, leaf := tableFixedLeafSize(e.Kind, e.Size)
	if !admitted {
		c.fail("layout_size_mismatch")
		return 0
	}
	if leaf {
		if e.Children != 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		return at - i
	}
	switch e.Kind {
	case 13: // a TABLE: its size is the SUM of its fields'
		if uint64(e.Size) != sum {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 35: // the OPTIONAL WRAPPER: one child, one present byte in front of it
		if e.Children != 1 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if uint64(e.Size) != sum+1 {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 14: // an ARRAY: a whole number of elements, behind a count or not
		if e.Children != 1 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if firstSize == 0 {
			c.fail("layout_size_mismatch")
			return 0
		}
		bare := e.Size%firstSize == 0
		counted := e.Size >= 4 && (e.Size-4)%firstSize == 0
		if !bare && !counted {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 16: // an ENUM-KEYED array: the KEY ENUM then the ELEMENT, every slot written
		if e.Children != 2 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if firstKind != 30 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if secondSize == 0 || e.Size%secondSize != 0 {
			c.fail("layout_size_mismatch")
			return 0
		}
		// AT LEAST one slot per variant. Not exactly one: an enum widened by an
		// explicit max has more slots than it has names, and the layout carries
		// only the names.
		if e.Size/secondSize < firstChildren {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 15: // a UNION: the TAG at its own width, then the WIDEST ARM
		if e.Children == 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if e.Size <= widest {
			c.fail("layout_size_mismatch")
			return 0
		}
		if !tableFixedOrdinalWidth(e.Size - widest) {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 30: // an ENUM: the ordinal's storage width, and its children are VARIANTS
		if !tableFixedOrdinalWidth(e.Size) {
			c.fail("layout_size_mismatch")
			return 0
		}
		if e.Children != 0 && !kidsAreVariants {
			c.fail("layout_kind_invalid")
			return 0
		}
	case 12: // string(N): the length, then N bytes
		if e.Children != 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if e.Size < 4 {
			c.fail("layout_size_mismatch")
			return 0
		}
	case 33: // wstring(N): the length in CODE UNITS, then 2N bytes
		if e.Children != 0 {
			c.fail("layout_kind_invalid")
			return 0
		}
		if e.Size < 4 || (e.Size-4)%2 != 0 {
			c.fail("layout_size_mismatch")
			return 0
		}
	default:
		// Every kind of the closed set is either a leaf above or a case here, so
		// this is unreachable — and it refuses rather than admits, because a
		// kind that reached it is a kind the two lists disagree about.
		c.fail("layout_kind_unknown")
		return 0
	}
	return at - i
}

func tableFixedTagBytes(b tableFixedLayoutView, i int32) uint32 {
	return tableFixedEntryAt(b, i).Size - tableFixedUnionArmBytes(b, i)
}

// tableFixedParseLayout is the WHOLE of what a reader trusts a layout on. It
// answers the view and a REASON BY NAME — empty when the layout passed every
// rule — and the caller reports that reason. A layout that fails any rule SETS
// NOTHING: no view, no plan, and no counter moved.
//
// THE ORDER IS LOAD-BEARING (docs/FIXED-FORM-ALGORITHM.md §1.1): rule 1; the
// root's kind and size; then the walk — index in range (5), depth (7), kind
// known (2), own size (6), the children, then the size and shape rules (3, 4);
// and last that the walk consumed exactly the entries (5).
func tableFixedParseLayout(bytes []byte) (tableFixedLayoutView, string) {
	var out tableFixedLayoutView
	// THE RESIDUE: bytes that are not a layout at all — fewer than a header's
	// worth of them — are layout_malformed and not one of the seven.
	if int32(len(bytes)) < tableFixedLayoutHeaderBytes {
		return out, "layout_malformed"
	}
	// 1. THE ENTRY COUNT FITS THE LAYOUT LENGTH EXACTLY
	count := tableFixedGet32(bytes)
	if count == 0 || int64(count)*int64(tableFixedEntryBytes)+int64(tableFixedLayoutHeaderBytes) != int64(len(bytes)) {
		return out, "layout_count_mismatch"
	}
	view := tableFixedLayoutView{Bytes: bytes, Count: int32(count)}
	// 2. THE ROOT IS A TABLE, and its size is the record's body
	root := tableFixedEntryAt(view, 0)
	if root.Kind != 13 {
		return out, "layout_kind_invalid"
	}
	if root.Size == 0 || root.Size > tableFixedRecordMaxBytes {
		return out, "layout_record_too_large"
	}
	// 3. EVERY KIND IN THE CLOSED SET AND USED AS ITS DEFINITION ALLOWS, EVERY
	//    SIZE THE ONE ITS CHILDREN ACCOUNT FOR, NOTHING PAST THE WALK'S BOUND
	c := tableFixedCheck{layout: view}
	used := tableFixedCheckEntry(&c, 0, 0)
	if c.why != "" {
		return out, c.why
	}
	// 4. THE PRE-ORDER WALK CONSUMES EXACTLY THE ENTRIES: the tree closes and
	//    the layout has nothing left over
	if used != view.Count {
		return out, "layout_tree_unclosed"
	}
	return view, ""
}

type tableFixedCompiler struct {
	plan []TableFixedEntry
	capacity, count, pool int32
	overflow bool
	// tooLarge is ONE ENTRY PAST THE WRITER'S RECORD, which refuses the plan
	// WHOLE under layout_record_too_large (§5.2) and never lands a short read.
	tooLarge bool
	// record is the WRITER's declared root size, the bound every entry's
	// source extent is held to.
	record uint32
	// argW is the CURRENT union's tag width, stamped onto every guarded push.
	argW uint8
	report *TableReport
}

// tableFixedSrcEnd is the last source byte, exclusive, an entry reads — the
// guard included, because a guarded entry loads the tag before anything else.
func tableFixedSrcEnd(e TableFixedEntry) uint64 {
	var end uint64
	switch e.Op {
	case tableFixedConst:
		end = 0 // a constant reads no source at all
	case tableFixedCount:
		end = uint64(e.Src) + 4 // Size is the BOUND, not a byte count
	case tableFixedText:
		end = uint64(e.Src) + 4 + uint64(e.Size)
	default:
		end = uint64(e.Src) + uint64(e.Size)
	}
	if e.Guard != tableFixedNoGuard {
		if g := uint64(e.Guard) + uint64(tableFixedArgWidth(e.ArgW)); g > end {
			end = g
		}
	}
	return end
}

func tableFixedPush(c *tableFixedCompiler, e TableFixedEntry) {
	if e.Guard != tableFixedNoGuard {
		e.ArgW = uint8(tableFixedArgWidth(c.argW))
	}
	// ONE ENTRY PAST THE WRITER'S RECORD REFUSES THE PLAN WHOLE (§5.2). The
	// bound is the writer's declared root size and not this reader's: an entry
	// that reached past it would read the NEXT record's bytes.
	if c.record != 0 && tableFixedSrcEnd(e) > uint64(c.record) {
		c.tooLarge = true
		return
	}
	room := c.capacity - (c.pool+int32(unsafe.Sizeof(TableFixedEntry{}))-1)/int32(unsafe.Sizeof(TableFixedEntry{}))
	if c.count >= room {
		c.overflow = true
		return
	}
	c.plan[c.count] = e
	c.count++
}

// tableFixedRemapMax is the longest remap table the pool's u16 length word can
// name. A writer's variant count above it is a layout this reader cannot plan.
const tableFixedRemapMax uint32 = 0xFFFF

func tableFixedLayTable(c *tableFixedCompiler, values []uint16, n int32) uint32 {
	bytes := (n + 1) * 2
	total := int32(unsafe.Sizeof(TableFixedEntry{})) * c.capacity
	c.pool += bytes
	if total-c.pool < c.count*int32(unsafe.Sizeof(TableFixedEntry{})) {
		c.overflow = true
		return 0
	}
	at := uint32(total - c.pool)
	dst := unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(&c.plan[0]), uintptr(at))), n+1)
	dst[0] = uint16(n)
	copy(dst[1:], values[:n])
	return at
}

// census is whether THIS entry is the first time the peer's field is seen:
// an array's element subtree is compiled once per element and the unknown
// count is ONCE PER FIELD PER PEER (§5.4), so only element 0 censuses.
func tableFixedMatchChildren(c *tableFixedCompiler, theirs tableFixedLayoutView, ti int32, theirAt uint32, mine tableFixedLayoutView, mi int32, dst []TableFixedDst, myAt, guard uint32, arg uint32, census bool) {
	te := tableFixedEntryAt(theirs, ti)
	me := tableFixedEntryAt(mine, mi)
	myChild := mi + 1
	for k := uint32(0); k < me.Children; k++ {
		mc := tableFixedEntryAt(mine, myChild)
		theirChild := ti + 1
		theirOff := theirAt
		for j := uint32(0); j < te.Children; j++ {
			tc := tableFixedEntryAt(theirs, theirChild)
			if tc.Id == mc.Id {
				tableFixedCompileEntry(c, theirs, theirChild, theirOff, mine, myChild, dst, myAt, guard, arg, census)
				break
			}
			theirOff += tc.Size
			theirChild += tableFixedSubtree(theirs, theirChild)
		}
		myChild += tableFixedSubtree(mine, myChild)
	}
	tcAt := ti + 1
	for j := uint32(0); j < te.Children; j++ {
		tc := tableFixedEntryAt(theirs, tcAt)
		named := false
		mcAt := mi + 1
		for k := uint32(0); k < me.Children; k++ {
			if tableFixedEntryAt(mine, mcAt).Id == tc.Id {
				named = true
				break
			}
			mcAt += tableFixedSubtree(mine, mcAt)
		}
		if !named && census && c.report != nil {
			c.report.Unknown++
		}
		tcAt += tableFixedSubtree(theirs, tcAt)
	}
}

func tableFixedCompileEntry(c *tableFixedCompiler, theirs tableFixedLayoutView, ti int32, theirAt uint32, mine tableFixedLayoutView, mi int32, dst []TableFixedDst, myAt, guard uint32, arg uint32, census bool) {
	te := tableFixedEntryAt(theirs, ti)
	me := tableFixedEntryAt(mine, mi)
	d := dst[mi]
	at := myAt + d.Dst
	auxAt := myAt + d.Aux
	if te.Kind != me.Kind && !(me.Kind == 35) {
		if tableFixedWidens(te.Kind, me.Kind) {
			e := TableFixedEntry{Src: theirAt, Dst: at, Size: te.Size, DstSize: uint8(me.Size), Guard: guard, Arg: arg}
			if te.Kind == 10 {
				e.Op = tableFixedWidenF
			} else {
				e.Op = tableFixedWiden
			}
			if tableFixedSignedKind(te.Kind) {
				e.Sign = 1
			}
			tableFixedPush(c, e)
			return
		}
		if c.report != nil {
			c.report.KindMismatch++
		}
		return
	}
	// T INTO ?T (§5.2 EMIT, bill §12.8): the reader wrapped a value the writer
	// carried bare. The PRESENT byte is an UNGUARDED constant 1 at the
	// wrapper's aux lane, and the payload is compiled against the writer's own
	// entry under the same guard and ordinal. It is not a kind that moved.
	if me.Kind == 35 && te.Kind != 35 {
		tableFixedPush(c, TableFixedEntry{Dst: auxAt, Size: 1, Aux: 1, Guard: guard, Op: tableFixedConst, Arg: arg})
		tableFixedCompileEntry(c, theirs, ti, theirAt, mine, mi+1, dst, myAt, guard, arg, census)
		return
	}
	switch me.Kind {
	case 35:
		tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: auxAt, Size: 1, Guard: guard, Op: tableFixedBool, Arg: arg})
		tableFixedCompileEntry(c, theirs, ti+1, theirAt+1, mine, mi+1, dst, myAt, guard, arg, census)
	case 13:
		tableFixedMatchChildren(c, theirs, ti, theirAt, mine, mi, dst, at, guard, arg, census)
	case 14:
		tel := tableFixedEntryAt(theirs, ti+1)
		mel := tableFixedEntryAt(mine, mi+1)
		head := uint32(0)
		if d.Counted != 0 {
			head = 4
		}
		var theirN, myN uint32
		if tel.Size != 0 {
			theirN = (te.Size - head) / tel.Size
		}
		if mel.Size != 0 {
			myN = (me.Size - head) / mel.Size
		}
		theirBase := theirAt + head
		n := theirN
		if myN < n {
			n = myN
		}
		if d.Counted != 0 {
			// THE COUNT'S BOUND IS THE WRITER'S their_n (§5.2 EMIT kind 14,
			// §5.3 step 11: BOUNDS runs against the PLAN's bounds). The
			// reader's own bound is the wrong number: a forged 7 from a writer
			// bounded at 4 must land 4 and COUNT, not be admitted because this
			// reader grew to 8. n is their_n held to this reader's storage,
			// which is their_n itself on every legal widening.
			tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: auxAt, Size: n, Guard: guard, Op: tableFixedCount, Arg: arg})
		}
		for i := uint32(0); i < n; i++ {
			tableFixedCompileEntry(c, theirs, ti+1, theirBase+i*tel.Size, mine, mi+1, dst, at+i*d.Stride, guard, arg, census && i == 0)
		}
	case 16:
		tkey := tableFixedEntryAt(theirs, ti+1)
		mkey := tableFixedEntryAt(mine, mi+1)
		tel := ti + 1 + tableFixedSubtree(theirs, ti+1)
		mel := mi + 1 + tableFixedSubtree(mine, mi+1)
		tee := tableFixedEntryAt(theirs, tel)
		for k := uint32(0); k < mkey.Children; k++ {
			keyId := tableFixedEntryAt(mine, mi+2+int32(k)).Id
			for j := uint32(0); j < tkey.Children; j++ {
				if tableFixedEntryAt(theirs, ti+2+int32(j)).Id != keyId {
					continue
				}
				tableFixedCompileEntry(c, theirs, tel, theirAt+j*tee.Size, mine, mel, dst, at+k*d.Stride, guard, arg, census && k == 0)
				break
			}
		}
	case 15:
		theirTag := tableFixedTagBytes(theirs, ti)
		myTag := tableFixedTagBytes(mine, mi)
		// THE WRITER'S TAG WIDTH IS THE GUARD'S (§5.2 THE GUARD'S WIDTH): every
		// entry pushed beneath this union is compared at it, whole.
		savedW := c.argW
		c.argW = 1
		if theirTag >= 1 && theirTag <= 8 {
			c.argW = uint8(theirTag)
		}
		myArm := mi + 1
		for k := uint32(0); k < me.Children; k++ {
			ma := tableFixedEntryAt(mine, myArm)
			theirArm := ti + 1
			for j := uint32(0); j < te.Children; j++ {
				ta := tableFixedEntryAt(theirs, theirArm)
				if ta.Id == ma.Id {
					tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: auxAt, Size: myTag, Aux: k + 1, Guard: theirAt, Op: tableFixedConst, Arg: j + 1})
					tableFixedCompileEntry(c, theirs, theirArm, theirAt+theirTag, mine, myArm, dst, at, theirAt, j+1, census)
					break
				}
				theirArm += tableFixedSubtree(theirs, theirArm)
			}
			myArm += tableFixedSubtree(mine, myArm)
		}
		c.argW = savedW
	case 30:
		// A GROWN ORDINAL WIDTH IS A WIDEN, unsigned, and it counts widened
		// (§5.2 EMIT kind 30, §5.4). Only a SAME-WIDTH ordinal takes the remap
		// table.
		if te.Size < me.Size {
			tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: at, Size: te.Size, Guard: guard,
				Op: tableFixedWiden, Arg: arg, DstSize: uint8(me.Size)})
			return
		}
		// THE TABLE IS AS LONG AS THE WRITER'S VARIANT COUNT (§5.2), not 255:
		// capped at 255 a 300-variant writer's variant 299 read as out of the
		// set and landed None.
		n := te.Children
		if n > tableFixedRemapMax {
			c.tooLarge = true
			return
		}
		m := make([]uint16, n)
		for j := uint32(0); j < n; j++ {
			vid := tableFixedEntryAt(theirs, ti+1+int32(j)).Id
			var landed uint16
			for k := uint32(0); k < me.Children; k++ {
				if tableFixedEntryAt(mine, mi+1+int32(k)).Id == vid {
					landed = uint16(k + 1)
					break
				}
			}
			m[j] = landed
		}
		e := TableFixedEntry{Src: theirAt, Dst: at, Size: te.Size, Guard: guard, Op: tableFixedOrdinal, Arg: arg, DstSize: uint8(me.Size)}
		e.Aux = tableFixedLayTable(c, m, int32(n))
		tableFixedPush(c, e)
	case 12, 33:
		units := me.Size - 4
		if te.Size-4 < units {
			units = te.Size - 4
		}
		tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: at, Size: units, Aux: auxAt, Guard: guard, Op: tableFixedText, Arg: arg, Meta: d.Meta})
	default:
		e := TableFixedEntry{Src: theirAt, Dst: at, Guard: guard, Arg: arg}
		if te.Size == me.Size {
			e.Size = me.Size
			e.Op = tableFixedCopy
			if me.Kind == 1 {
				e.Op = tableFixedBool
			}
			tableFixedPush(c, e)
		} else if te.Size < me.Size && me.Size <= 8 {
			e.Size = te.Size
			e.DstSize = uint8(me.Size)
			e.Op = tableFixedWiden
			tableFixedPush(c, e)
		} else if c.report != nil {
			c.report.KindMismatch++
		}
	}
}

func tableFixedCompile(theirs tableFixedLayoutView, myLayout []byte, dst []TableFixedDst, plan []TableFixedEntry, report *TableReport) int32 {
	if len(plan) == 0 {
		return -1
	}
	// THE READER'S OWN LAYOUT, and a failure to parse it owes its OWN name and
	// not plan_too_large: -2 is that, -1 is the plan storage (reference fix 3).
	mine, why := tableFixedParseLayout(myLayout)
	if why != "" {
		return -2
	}
	// THE WRITER'S RECORD IS THE BOUND ON EVERY ENTRY (§5.2): the root's
	// declared size, taken from the layout this plan is compiled against.
	c := tableFixedCompiler{plan: plan, capacity: int32(len(plan)), report: report,
		record: tableFixedEntryAt(theirs, 0).Size}
	tableFixedMatchChildren(&c, theirs, 0, 0, mine, 0, dst, 0, tableFixedNoGuard, 0, true)
	// ONE ENTRY PAST THE WRITER'S RECORD REFUSES THE PLAN WHOLE, by its own
	// name: -3 is layout_record_too_large, -2 the reader's own layout, -1 the
	// plan storage.
	if c.tooLarge {
		return -3
	}
	if c.overflow {
		return -1
	}
	out := int32(0)
	for i := int32(0); i < c.count; i++ {
		if out > 0 && plan[out-1].Op == plan[i].Op && tableFixedRuns(plan[i].Op) &&
			plan[out-1].Guard == plan[i].Guard && plan[out-1].Arg == plan[i].Arg &&
			plan[out-1].ArgW == plan[i].ArgW && plan[out-1].Meta == plan[i].Meta &&
			plan[out-1].Src+plan[out-1].Size == plan[i].Src &&
			plan[out-1].Dst+plan[out-1].Size == plan[i].Dst {
			plan[out-1].Size += plan[i].Size
			continue
		}
		plan[out] = plan[i]
		out++
	}
	return out
}

// tableFixedHole is one dst range the winning plan does not land. Load copies
// declared defaults into exactly these, from one Reset image. Identity's list
// on Go is ABI padding and unselected union arms.
type tableFixedHole struct {
	Off, Size uint32
}

func tableFixedLand(cover []byte, off, n uint32) {
	if n == 0 {
		return
	}
	end := uint32(len(cover))
	if off >= end {
		return
	}
	last := off + n
	if last > end {
		last = end
	}
	for i := off; i < last; i++ {
		cover[i] = 1
	}
}

// tableFixedHoles is the complement of the unguarded dest writes. Guarded
// entries (union arms, compiled tag consts) are not always written, so they
// stay holes and keep the defaults. The write sizes match tableFixedRun.
func tableFixedHoles(plan []TableFixedEntry, count int32, dstSize uint32) []tableFixedHole {
	if dstSize == 0 {
		return nil
	}
	cover := make([]byte, dstSize)
	for i := int32(0); i < count; i++ {
		p := plan[i]
		if p.Guard != tableFixedNoGuard {
			continue
		}
		switch p.Op {
		case tableFixedCopy, tableFixedBool, tableFixedTag, tableFixedConst:
			tableFixedLand(cover, p.Dst, p.Size)
		case tableFixedCount:
			tableFixedLand(cover, p.Dst, 4)
		case tableFixedText:
			tableFixedLand(cover, p.Dst, 4)
			tableFixedLand(cover, p.Aux, p.Size)
		case tableFixedWiden:
			tableFixedLand(cover, p.Dst, uint32(p.DstSize))
		case tableFixedWidenF:
			tableFixedLand(cover, p.Dst, 8)
		case tableFixedOrdinal:
			n := uint32(4)
			if p.DstSize == 1 || p.DstSize == 2 || p.DstSize == 8 {
				n = uint32(p.DstSize)
			}
			tableFixedLand(cover, p.Dst, n)
		}
	}
	var holes []tableFixedHole
	i := uint32(0)
	for i < dstSize {
		if cover[i] != 0 {
			i++
			continue
		}
		j := i + 1
		for j < dstSize && cover[j] == 0 {
			j++
		}
		holes = append(holes, tableFixedHole{Off: i, Size: j - i})
		i = j
	}
	return holes
}

// ---- THE LINEAGE, AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5) ----------
//
// A fixed table reads BACKWARD and never forward. A file is matched on the
// eight bytes of its header's hash against the lineage the BUILD laid down, and
// the layout it carries is held to a BYTE COMPARISON against the bytes the lock
// recorded — never walked. A hash the lineage does not hold is layout_newer
// (ship the reader); a hash it holds below the floor is layout_unsupported
// (upgrade the client). Those are the operator's two distinct answers.

// TableFixedKnownLayout is one locked layout: the wire hash a file is matched
// on, the layout bytes verbatim, and the RECORD SIZE taken from the lock and
// never from the file.
type TableFixedKnownLayout struct {
	Hash   uint64
	Layout []byte
	Record int64
}

// tableFixedLineagePlan is one older entry's plan, laid down once from the
// LOCK'S bytes. Unknown and KindMismatch are the COMPILE census: a writer field
// no reader field names is counted ONCE PER PEER and never per record (§5.4).
type tableFixedLineagePlan struct {
	Entries      []TableFixedEntry
	Count        int32
	Unknown      int32
	KindMismatch int32
	Why          string
}

func tableFixedSelect(known []TableFixedKnownLayout, hash uint64) int32 {
	for i := range known {
		if known[i].Hash == hash {
			return int32(i)
		}
	}
	return -1
}

// tableFixedRefuseHash is the two refusals that report the FILE'S hash and
// nothing else (§5.3).
func tableFixedRefuseHash(report *TableReport, reason string, hash uint64) int64 {
	report.LayoutHash = hash
	return tableFixedRefuse(report, reason)
}

// tableFixedLineagePlans compiles one plan per lineage entry from THE LOCK'S
// layout bytes, at package initialization — nothing compiles on the load path,
// and there is no cache to miss (§5.2, §5.8 row 3). The identity entry keeps a
// nil plan: the baked one answers it.
func tableFixedLineagePlans(known []TableFixedKnownLayout, myLayout []byte, dst []TableFixedDst, own uint64) []tableFixedLineagePlan {
	out := make([]tableFixedLineagePlan, len(known))
	for i := range known {
		if known[i].Hash == own {
			continue
		}
		parsed, why := tableFixedParseLayout(known[i].Layout)
		if why != "" {
			// A lineage entry that is not a layout is a bug in the LOCK, not a
			// wire event; the reader still owes a name rather than a panic.
			out[i].Why = "layout_malformed"
			continue
		}
		for room := 256; ; room *= 4 {
			plan := make([]TableFixedEntry, room)
			var census TableReport
			made := tableFixedCompile(parsed, myLayout, dst, plan, &census)
			if made == -2 {
				out[i].Why = "layout_malformed"
				break
			}
			if made == -3 {
				out[i].Why = "layout_record_too_large"
				break
			}
			if made < 0 {
				if room >= 1<<18 {
					out[i].Why = "plan_too_large"
					break
				}
				continue
			}
			out[i] = tableFixedLineagePlan{
				Entries:      plan[:made],
				Count:        made,
				Unknown:      census.Unknown,
				KindMismatch: census.KindMismatch,
			}
			break
		}
	}
	return out
}

// A REFUSAL BY NAME HAS COUNTERS ALL ZERO (§5.3's joint table). A no_layout
// found mid-batch must not carry record 0's counters out with it, and a report
// a caller reuses must not keep a refusal that is no longer true. The FILE'S
// hash survives, because the two refusals that report it set it first.
func tableFixedRefuse(report *TableReport, reason string) int64 {
	hash := report.LayoutHash
	*report = TableReport{}
	report.LayoutHash = hash
	report.Verdict = TableOpenRefused
	report.Reason = reason
	return -1
}

func tableFixedOverlay(ptr unsafe.Pointer, n uintptr) []byte {
	return unsafe.Slice((*byte)(ptr), n)
}
`
