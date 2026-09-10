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

// Arg is the guard's arm ordinal: tableFixedRun compares src[Guard] against
// it. Meta is a text entry's flavour (tableFixedTextUtf8/Wide/Bytes). They
// shared one lane until reference-fix 12; compiling a string under a union
// arm overwrote the guard with the flavour and the read skipped the string.
// tableFixedEntryBytes is the FILE layout entry (id+kind+size+children),
// not this in-memory plan row — Meta does not ride the wire.
type TableFixedEntry struct {
	Src, Dst, Size, Aux, Guard uint32
	Op, Arg, DstSize, Sign, Meta uint8
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
	return from == 10 && to == 11
}
func tableFixedSignedKind(kind uint8) bool { return kind >= 2 && kind <= 5 }
func tableFixedKindKnown(kind uint8) bool { return kind <= 33 || kind == 35 }

// tableFixedRuns is whether an op advances src and dst together one byte at a
// time, which is the whole of what lets two adjacent entries become one.
func tableFixedRuns(op uint8) bool { return op == tableFixedCopy || op == tableFixedBool }

func tableFixedBuildPlan(emit func([]TableFixedEntry, uint32, uint32) int, n int) tableFixedPlan {
	raw := make([]TableFixedEntry, n)
	written := emit(raw, 0, 0)
	out := 0
	for i := 0; i < written; i++ {
		if out > 0 && raw[out-1].Op == raw[i].Op && tableFixedRuns(raw[i].Op) &&
			raw[out-1].Guard == raw[i].Guard && raw[out-1].Arg == raw[i].Arg &&
			raw[out-1].Meta == raw[i].Meta &&
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

func tableFixedHashOf(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, c := range layout {
		h ^= uint64(c)
		h *= 0x100000001b3
	}
	return h
}

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
		if p.Guard != tableFixedNoGuard && src[p.Guard] != p.Arg {
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
			var raw uint32
			switch p.Size {
			case 1:
				raw = uint32(src[p.Src])
			case 2:
				raw = uint32(binary.LittleEndian.Uint16(src[p.Src:]))
			default:
				raw = binary.LittleEndian.Uint32(src[p.Src:])
			}
			p16 := (*uint16)(unsafe.Add(unsafe.Pointer(&plan[0]), uintptr(p.Aux)))
			table := unsafe.Slice(p16, int(*p16)+1)
			var v uint32
			if raw != 0 && raw <= uint32(table[0]) {
				v = uint32(table[raw])
			}
			switch p.DstSize {
			case 1:
				dst[p.Dst] = uint8(v)
			case 2:
				binary.LittleEndian.PutUint16(dst[p.Dst:], uint16(v))
			default:
				binary.LittleEndian.PutUint32(dst[p.Dst:], v)
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

func tableFixedEntryAt(b tableFixedLayoutView, i int32) tableFixedLayoutEntry {
	e := b.Bytes[4+int64(i)*int64(tableFixedEntryBytes):]
	return tableFixedLayoutEntry{
		Id: tableFixedGet64(e),
		Kind: e[8],
		Size: tableFixedGet32(e[9:]),
		Children: tableFixedGet32(e[13:]),
	}
}

func tableFixedSubtree(b tableFixedLayoutView, i int32) int32 {
	if i < 0 || i >= b.Count {
		return 1
	}
	e := tableFixedEntryAt(b, i)
	n := int32(1)
	at := i + 1
	for c := uint32(0); c < e.Children; c++ {
		if at >= b.Count {
			break
		}
		sub := tableFixedSubtree(b, at)
		at += sub
		n += sub
	}
	return n
}

func tableFixedUnionArmBytes(b tableFixedLayoutView, i int32) uint32 {
	e := tableFixedEntryAt(b, i)
	var widest uint32
	at := i + 1
	for k := uint32(0); k < e.Children; k++ {
		a := tableFixedEntryAt(b, at)
		if a.Size > widest {
			widest = a.Size
		}
		at += tableFixedSubtree(b, at)
	}
	return widest
}

func tableFixedTagBytes(b tableFixedLayoutView, i int32) uint32 {
	return tableFixedEntryAt(b, i).Size - tableFixedUnionArmBytes(b, i)
}

func tableFixedParseLayout(bytes []byte) (tableFixedLayoutView, bool) {
	var out tableFixedLayoutView
	if len(bytes) < 4 {
		return out, false
	}
	count := tableFixedGet32(bytes)
	if count == 0 || int64(count)*int64(tableFixedEntryBytes)+4 != int64(len(bytes)) {
		return out, false
	}
	out.Bytes = bytes
	out.Count = int32(count)
	if tableFixedSubtree(out, 0) != out.Count {
		return tableFixedLayoutView{}, false
	}
	for i := int32(0); i < out.Count; i++ {
		if !tableFixedKindKnown(tableFixedEntryAt(out, i).Kind) {
			return tableFixedLayoutView{}, false
		}
	}
	return out, true
}

type tableFixedCompiler struct {
	plan []TableFixedEntry
	capacity, count, pool int32
	overflow bool
	report *TableReport
}

func tableFixedPush(c *tableFixedCompiler, e TableFixedEntry) {
	room := c.capacity - (c.pool+int32(unsafe.Sizeof(TableFixedEntry{}))-1)/int32(unsafe.Sizeof(TableFixedEntry{}))
	if c.count >= room {
		c.overflow = true
		return
	}
	c.plan[c.count] = e
	c.count++
}

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

func tableFixedMatchChildren(c *tableFixedCompiler, theirs tableFixedLayoutView, ti int32, theirAt uint32, mine tableFixedLayoutView, mi int32, dst []TableFixedDst, myAt, guard uint32, arg uint8) {
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
				tableFixedCompileEntry(c, theirs, theirChild, theirOff, mine, myChild, dst, myAt, guard, arg)
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
		if !named && c.report != nil {
			c.report.Unknown++
		}
		tcAt += tableFixedSubtree(theirs, tcAt)
	}
}

func tableFixedCompileEntry(c *tableFixedCompiler, theirs tableFixedLayoutView, ti int32, theirAt uint32, mine tableFixedLayoutView, mi int32, dst []TableFixedDst, myAt, guard uint32, arg uint8) {
	te := tableFixedEntryAt(theirs, ti)
	me := tableFixedEntryAt(mine, mi)
	d := dst[mi]
	at := myAt + d.Dst
	auxAt := myAt + d.Aux
	if te.Kind != me.Kind {
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
	switch me.Kind {
	case 35:
		tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: auxAt, Size: 1, Guard: guard, Op: tableFixedBool, Arg: arg})
		tableFixedCompileEntry(c, theirs, ti+1, theirAt+1, mine, mi+1, dst, myAt, guard, arg)
	case 13:
		tableFixedMatchChildren(c, theirs, ti, theirAt, mine, mi, dst, at, guard, arg)
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
		if d.Counted != 0 {
			tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: auxAt, Size: myN, Guard: guard, Op: tableFixedCount, Arg: arg})
		}
		n := theirN
		if myN < n {
			n = myN
		}
		for i := uint32(0); i < n; i++ {
			tableFixedCompileEntry(c, theirs, ti+1, theirBase+i*tel.Size, mine, mi+1, dst, at+i*d.Stride, guard, arg)
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
				tableFixedCompileEntry(c, theirs, tel, theirAt+j*tee.Size, mine, mel, dst, at+k*d.Stride, guard, arg)
				break
			}
		}
	case 15:
		theirTag := tableFixedTagBytes(theirs, ti)
		myTag := tableFixedTagBytes(mine, mi)
		myArm := mi + 1
		for k := uint32(0); k < me.Children; k++ {
			ma := tableFixedEntryAt(mine, myArm)
			theirArm := ti + 1
			for j := uint32(0); j < te.Children; j++ {
				ta := tableFixedEntryAt(theirs, theirArm)
				if ta.Id == ma.Id {
					tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: auxAt, Size: myTag, Aux: k + 1, Guard: theirAt, Op: tableFixedConst, Arg: uint8(j + 1)})
					tableFixedCompileEntry(c, theirs, theirArm, theirAt+theirTag, mine, myArm, dst, at, theirAt, uint8(j+1))
					break
				}
				theirArm += tableFixedSubtree(theirs, theirArm)
			}
			myArm += tableFixedSubtree(mine, myArm)
		}
	case 30:
		var m [256]uint16
		n := te.Children
		if n > 255 {
			n = 255
		}
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
		e.Aux = tableFixedLayTable(c, m[:], int32(n))
		tableFixedPush(c, e)
	case 12, 33:
		units := me.Size - 4
		if te.Size-4 < units {
			units = te.Size - 4
		}
		tableFixedPush(c, TableFixedEntry{Src: theirAt, Dst: at, Size: units, Aux: auxAt, Guard: guard, Op: tableFixedText, Arg: arg, Meta: d.Arg})
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
	mine, ok := tableFixedParseLayout(myLayout)
	if !ok {
		return -1
	}
	c := tableFixedCompiler{plan: plan, capacity: int32(len(plan)), report: report}
	tableFixedMatchChildren(&c, theirs, 0, 0, mine, 0, dst, 0, tableFixedNoGuard, 0)
	if c.overflow {
		return -1
	}
	out := int32(0)
	for i := int32(0); i < c.count; i++ {
		if out > 0 && plan[out-1].Op == plan[i].Op && tableFixedRuns(plan[i].Op) &&
			plan[out-1].Guard == plan[i].Guard && plan[out-1].Arg == plan[i].Arg &&
			plan[out-1].Meta == plan[i].Meta &&
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

func tableFixedRefuse(report *TableReport, reason string) int64 {
	report.Verdict = TableOpenRefused
	report.Reason = reason
	return -1
}

func tableFixedOverlay(ptr unsafe.Pointer, n uintptr) []byte {
	return unsafe.Slice((*byte)(ptr), n)
}
`
