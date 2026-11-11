package ctable

// THE STATIC PLAN STORAGE OF A LINEAGE ENTRY.
//
// Every older lineage entry's plan is compiled once, off the load path, into
// static storage (fixedform.go's lazy build). That storage is a capacity: the
// compile lays entries down from the bottom and its remap and prefill tables
// down from the top, and an entry whose plan does not fit records
// plan_too_large for the life of the process (§5.9 #4). The capacity therefore
// has to be the WORST one any locked entry can need, not a fold of this build's
// own shape: a flat array is one identity entry, but an evolved element is
// walked once per element and can need an entry per element.
//
// This file answers that worst case from the SAME WALK the runtime runs
// (table_fixed_compile in fixedruntime.go), over each locked layout. It walks
// the writer's layout bytes and this reader's own layout together, counts the
// plan entries the compile pushes in both passes, coalesces them by the
// runtime's rule, and runs the prefill subtraction to size the pool laid above
// them. It builds no values; only the capacity is needed at generate time.

import (
	"encoding/binary"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedLineageLayoutEntry is one entry of a writer's layout as it rides: bytes,
// not the Go walk's destination rows. The zero value is unused; the accessor
// answers kind 0xFF for an index past the entries, exactly as the runtime's
// table_fixed_entry_at does.
type fixedLineageLayoutEntry struct {
	id       uint64
	kind     uint8
	size     uint32
	children uint32
}

func fixedParseLineageLayout(b []byte) ([]fixedLineageLayoutEntry, bool) {
	if len(b) < ir.TableFixedLayoutHeaderBytes {
		return nil, false
	}
	count := binary.LittleEndian.Uint32(b)
	if count == 0 || int64(count)*ir.TableFixedEntryBytes+ir.TableFixedLayoutHeaderBytes != int64(len(b)) {
		return nil, false
	}
	out := make([]fixedLineageLayoutEntry, count)
	for i := range out {
		at := ir.TableFixedLayoutHeaderBytes + int64(i)*ir.TableFixedEntryBytes
		out[i] = fixedLineageLayoutEntry{
			id:       binary.LittleEndian.Uint64(b[at:]),
			kind:     b[at+8],
			size:     binary.LittleEndian.Uint32(b[at+9:]),
			children: binary.LittleEndian.Uint32(b[at+13:]),
		}
	}
	return out, true
}

// fixedPlanDstRow is the numeric form of a TableFixedDstSpec: the columns the
// runtime's plan compiler reads.
type fixedPlanDstRow struct {
	dst     uint32
	stride  uint32
	aux     uint32
	counted uint8
	meta    uint8
}

func (g *tableGen) fixedPlanDstRows(st *ir.Struct) []fixedPlanDstRow {
	entries := ir.TableFixedWalkRoot(st)
	out := make([]fixedPlanDstRow, len(entries))
	for i, e := range entries {
		d := e.Dst
		stride := uint32(0)
		switch {
		case d.Stride1:
			stride = 1
		case d.Stride != nil:
			stride = uint32(ir.TableFixedStrideBytes(g.unit, d.Stride))
		}
		dst := uint32(g.fixedTerms(d.Dst))
		aux := uint32(g.fixedTerms(d.Aux))
		if d.AuxExtendsDst {
			aux = dst + uint32(g.fixedTerms(d.Aux))
		}
		out[i] = fixedPlanDstRow{dst: dst, stride: stride, aux: aux, counted: uint8(d.Counted), meta: uint8(d.Meta)}
	}
	return out
}

// fixedSizedEntry is the part of a compiled TableFixedEntry the sizing walk
// needs: enough to coalesce by the runtime's rule and to say which storage
// bytes the entry lands.
type fixedSizedEntry struct {
	src, dst, size, aux uint32
	guard, guard2       uint32
	arg, arg2           uint64
	op                  uint8
	meta, dstsize       uint8
	argw, argw2         uint8
	elem                uint8
}

const (
	fixedNoGuard = ^uint32(0)

	// The runtime's plan ops (fixedruntime.go's kTableFixed* enum), in Go.
	kTableFixedCopy    uint8 = 0
	kTableFixedCount   uint8 = 1
	kTableFixedText    uint8 = 2
	kTableFixedOrdinal uint8 = 3
	kTableFixedWiden   uint8 = 4
	kTableFixedConst   uint8 = 5
	kTableFixedWidenF  uint8 = 6
	kTableFixedPresent uint8 = 7
	kTableFixedBool    uint8 = 8

	kTableFixedTextBytes uint8 = 3
)

func fixedWidens(from, to uint8) bool {
	if from >= 6 && from <= 9 && to >= 6 && to <= 9 {
		return to > from
	}
	if from >= 2 && from <= 5 && to >= 2 && to <= 5 {
		return to > from
	}
	if from >= 20 && from <= 24 && to >= 20 && to <= 24 {
		return to > from
	}
	if from >= 25 && from <= 29 && to >= 25 && to <= 29 {
		return to > from
	}
	return from == 10 && to == 11
}

type fixedStrideCompiler struct {
	writer []fixedLineageLayoutEntry
	mine   []ir.TableFixedLayoutEntry
	dst    []fixedPlanDstRow

	plan        []fixedSizedEntry
	pool        int32
	record      uint32
	depth       int
	wantGuarded bool
	argw        uint8
	hostile     bool
	overflow    bool
}

func (c *fixedStrideCompiler) their(i int32) fixedLineageLayoutEntry {
	if i < 0 || int(i) >= len(c.writer) {
		return fixedLineageLayoutEntry{kind: 0xFF}
	}
	return c.writer[i]
}

func (c *fixedStrideCompiler) mineEntry(i int32) ir.TableFixedLayoutEntry {
	if i < 0 || int(i) >= len(c.mine) {
		return ir.TableFixedLayoutEntry{Kind: -1}
	}
	return c.mine[i]
}

func (c *fixedStrideCompiler) subtree(i int32) int32 {
	if i < 0 || int(i) >= len(c.writer) {
		return 1
	}
	pending := int64(1)
	n := int32(0)
	at := i
	for pending > 0 && int(at) < len(c.writer) {
		pending--
		n++
		pending += int64(c.their(at).children)
		at++
		if pending > int64(len(c.writer)) {
			break
		}
	}
	return n
}

func (c *fixedStrideCompiler) unionArmBytes(i int32) uint32 {
	e := c.their(i)
	widest := uint32(0)
	at := i + 1
	for k := uint32(0); k < e.children && int(at) < len(c.writer); k++ {
		a := c.their(at)
		if a.size > widest {
			widest = a.size
		}
		at += c.subtree(at)
	}
	return widest
}

func (c *fixedStrideCompiler) tagBytes(i int32) uint32 {
	return c.their(i).size - c.unionArmBytes(i)
}

func (c *fixedStrideCompiler) push(e fixedSizedEntry) {
	// THE RUNTIME'S entry_zero() STARTS WITH NO SECOND GUARD and a width of
	// one. A pushed entry is never nested yet; the union walk stamps guard2 on
	// entries already in the plan.
	if e.guard2 == 0 {
		e.guard2 = fixedNoGuard
	}
	if e.argw == 0 {
		e.argw = 1
	}
	if e.argw2 == 0 {
		e.argw2 = 1
	}
	if (e.guard != fixedNoGuard) != c.wantGuarded {
		return
	}
	if e.guard != fixedNoGuard {
		e.argw = c.argwOr1()
	}
	gw := e.argw
	if gw == 0 {
		gw = 1
	}
	gw2 := e.argw2
	if gw2 == 0 {
		gw2 = 1
	}
	guardOut := (e.guard != fixedNoGuard && (e.guard >= c.record || uint64(e.guard)+uint64(gw) > uint64(c.record))) ||
		(e.guard2 != fixedNoGuard && (e.guard2 >= c.record || uint64(e.guard2)+uint64(gw2) > uint64(c.record)))
	reach := uint64(e.src) + uint64(e.size)
	if e.op == kTableFixedText || e.op == kTableFixedCount {
		reach += 4
	}
	if reach > uint64(c.record) || guardOut {
		c.hostile = true
		return
	}
	c.plan = append(c.plan, e)
}

func (c *fixedStrideCompiler) argwOr1() uint8 {
	if c.argw == 0 {
		return 1
	}
	return c.argw
}

func (c *fixedStrideCompiler) layTable(n int32) {
	if n < 0 || n > 65535 {
		c.overflow = true
		return
	}
	c.pool += (n + 1) * 2
}

func (c *fixedStrideCompiler) matchChildren(ti int32, theirAt uint32, mi int32, myAt uint32, guard uint32, arg uint64) {
	te := c.their(ti)
	me := c.mineEntry(mi)
	myChild := mi + 1
	for k := uint32(0); k < uint32(me.Children) && int(myChild) < len(c.mine); k++ {
		mc := c.mineEntry(myChild)
		theirChild := ti + 1
		theirOff := theirAt
		for j := uint32(0); j < te.children && int(theirChild) < len(c.writer); j++ {
			tc := c.their(theirChild)
			if tc.id == mc.ID {
				c.compileEntry(theirChild, theirOff, myChild, myAt, guard, arg)
				break
			}
			theirOff += tc.size
			theirChild += c.subtree(theirChild)
		}
		myChild += c.subtreeMine(myChild)
	}
}

func (c *fixedStrideCompiler) subtreeMine(i int32) int32 {
	if i < 0 || int(i) >= len(c.mine) {
		return 1
	}
	pending := int64(1)
	n := int32(0)
	at := i
	for pending > 0 && int(at) < len(c.mine) {
		pending--
		n++
		pending += int64(c.mine[at].Children)
		at++
		if pending > int64(len(c.mine)) {
			break
		}
	}
	return n
}

func (c *fixedStrideCompiler) compileEntry(ti int32, theirAt uint32, mi int32, myAt uint32, guard uint32, arg uint64) {
	te := c.their(ti)
	me := c.mineEntry(mi)
	if int(mi) >= len(c.dst) {
		return
	}
	d := c.dst[mi]
	at := myAt + d.dst
	auxAt := myAt + d.aux
	if c.depth > ir.TableFixedMaxDepth {
		c.hostile = true
		return
	}
	c.depth++
	defer func() { c.depth-- }()

	if int(me.Kind) == ir.TableKindOptional && int(te.kind) != ir.TableKindOptional {
		c.push(fixedSizedEntry{dst: auxAt, size: 1, guard: guard, arg: arg, op: kTableFixedPresent})
		c.compileEntry(ti, theirAt, mi+1, myAt, guard, arg)
		return
	}
	if int(te.kind) != me.Kind {
		if fixedWidens(te.kind, uint8(me.Kind)) {
			op := uint8(kTableFixedWiden)
			if te.kind == 10 {
				op = kTableFixedWidenF
			}
			c.push(fixedSizedEntry{src: theirAt, dst: at, size: te.size, dstsize: uint8(me.Size), guard: guard, arg: arg, op: op})
		}
		return
	}
	switch me.Kind {
	case ir.TableKindOptional:
		c.push(fixedSizedEntry{src: theirAt, dst: auxAt, size: 1, guard: guard, arg: arg, op: kTableFixedBool})
		c.compileEntry(ti+1, theirAt+1, mi+1, myAt, guard, arg)
	case ir.TableKindTable:
		c.matchChildren(ti, theirAt, mi, myAt, guard, arg)
	case ir.TableKindArray:
		if d.counted != 0 && te.size < 4 {
			c.hostile = true
			return
		}
		tel := c.their(ti + 1)
		mel := c.mineEntry(mi + 1)
		head := uint32(0)
		if d.counted != 0 {
			head = 4
		}
		theirN := uint32(0)
		if tel.size > 0 {
			theirN = (te.size - head) / tel.size
		}
		myN := uint32(0)
		if mel.Size > 0 {
			myN = (uint32(me.Size) - head) / uint32(mel.Size)
		}
		n := theirN
		if myN < n {
			n = myN
		}
		if n > 1<<20 {
			c.hostile = true
			return
		}
		theirBase := theirAt + head
		if d.counted != 0 {
			c.push(fixedSizedEntry{src: theirAt, dst: auxAt, size: n, guard: guard, arg: arg, op: kTableFixedCount})
		}
		for i := uint32(0); i < n; i++ {
			c.compileEntry(ti+1, theirBase+i*tel.size, mi+1, at+i*d.stride, guard, arg)
		}
	case ir.TableKindKeyed:
		tkey := c.their(ti + 1)
		mkey := c.mineEntry(mi + 1)
		tel := ti + 1 + c.subtree(ti+1)
		mel := mi + 1 + c.subtreeMine(mi+1)
		tee := c.their(tel)
		for k := uint32(0); k < uint32(mkey.Children) && int(mi+2+int32(k)) < len(c.mine); k++ {
			keyID := c.mineEntry(mi + 2 + int32(k)).ID
			for j := uint32(0); j < tkey.children && int(ti+2+int32(j)) < len(c.writer); j++ {
				if c.their(ti+2+int32(j)).id != keyID {
					continue
				}
				c.compileEntry(tel, theirAt+j*tee.size, mel, at+k*d.stride, guard, arg)
				break
			}
		}
	case ir.TableKindUnion:
		theirTag := c.tagBytes(ti)
		myTag := c.tagBytes(mi)
		savedArgw := c.argw
		c.argw = 1
		if theirTag >= 1 && theirTag <= 8 {
			c.argw = uint8(theirTag)
		}
		myArm := mi + 1
		innerArgw := c.argw
		if guard != fixedNoGuard {
			c.argw = c.argwOr1()
		}
		none := fixedSizedEntry{src: theirAt, dst: auxAt, size: myTag, aux: 0, guard: guard, op: kTableFixedConst, arg: arg}
		if te.children > 0 && te.children <= 255 {
			none.dstsize = uint8(te.children)
		}
		c.push(none)
		c.argw = innerArgw
		restampFrom := len(c.plan)
		for k := uint32(0); k < uint32(me.Children) && int(myArm) < len(c.mine); k++ {
			ma := c.mineEntry(myArm)
			theirArm := ti + 1
			for j := uint32(0); j < te.children && int(theirArm) < len(c.writer); j++ {
				ta := c.their(theirArm)
				if ta.id == ma.ID {
					tag := fixedSizedEntry{src: theirAt, dst: auxAt, size: myTag, aux: k + 1, guard: theirAt, op: kTableFixedConst, arg: uint64(j) + 1, argw: c.argw}
					c.push(tag)
					c.compileEntry(theirArm, theirAt+theirTag, myArm, at, theirAt, uint64(j)+1)
					break
				}
				theirArm += c.subtree(theirArm)
			}
			myArm += c.subtreeMine(myArm)
		}
		if guard != fixedNoGuard {
			outerW := savedArgw
			if outerW == 0 {
				outerW = 1
			}
			for q := restampFrom; q < len(c.plan); q++ {
				if c.plan[q].guard2 != fixedNoGuard {
					c.hostile = true
					break
				}
				c.plan[q].guard2 = guard
				c.plan[q].arg2 = arg
				c.plan[q].argw2 = outerW
			}
		}
		c.argw = savedArgw
	case ir.TableKindEnum:
		if (guard != fixedNoGuard) != c.wantGuarded {
			return
		}
		if te.size < uint32(me.Size) {
			c.push(fixedSizedEntry{src: theirAt, dst: at, size: te.size, dstsize: uint8(me.Size), guard: guard, arg: arg, op: kTableFixedWiden})
			return
		}
		n := te.children
		c.layTable(int32(n))
		if c.overflow {
			return
		}
		c.push(fixedSizedEntry{src: theirAt, dst: at, size: te.size, guard: guard, arg: arg, op: kTableFixedOrdinal, dstsize: uint8(me.Size)})
	case ir.TableKindString:
		units := uint32(me.Size) - 4
		if te.size-4 < units {
			units = te.size - 4
		}
		c.push(fixedSizedEntry{src: theirAt, dst: at, size: units, aux: auxAt, guard: guard, arg: arg, op: kTableFixedText, meta: d.meta})
	default:
		e := fixedSizedEntry{src: theirAt, dst: at, guard: guard, arg: arg}
		if te.size == uint32(me.Size) {
			e.size = uint32(me.Size)
			e.op = kTableFixedCopy
			if me.Kind == ir.TableKindBool {
				e.op = kTableFixedBool
			}
			if me.Size == 16 {
				e.elem = 8
			} else {
				e.elem = uint8(me.Size)
			}
			c.push(e)
		} else if te.size < uint32(me.Size) && me.Size <= 8 {
			e.size = te.size
			e.dstsize = uint8(me.Size)
			e.op = kTableFixedWiden
			c.push(e)
		}
	}
}

func fixedCoalescePlan(plan []fixedSizedEntry, plain int) []fixedSizedEntry {
	out := make([]fixedSizedEntry, 0, len(plan))
	split := 0
	for i := range plan {
		if i == plain {
			split = len(out)
		}
		if len(out) > split && out[len(out)-1].op == plan[i].op &&
			(plan[i].op == kTableFixedCopy || plan[i].op == kTableFixedBool) &&
			out[len(out)-1].guard == plan[i].guard && out[len(out)-1].arg == plan[i].arg &&
			out[len(out)-1].argw == plan[i].argw &&
			out[len(out)-1].guard2 == plan[i].guard2 && out[len(out)-1].arg2 == plan[i].arg2 &&
			out[len(out)-1].argw2 == plan[i].argw2 && out[len(out)-1].elem == plan[i].elem &&
			out[len(out)-1].src+out[len(out)-1].size == plan[i].src &&
			out[len(out)-1].dst+out[len(out)-1].size == plan[i].dst {
			out[len(out)-1].size += plan[i].size
			continue
		}
		out = append(out, plan[i])
	}
	return out
}

func fixedEntryLands(e fixedSizedEntry) [4]uint32 {
	lands := [4]uint32{e.dst, e.dst, 0, 0}
	switch e.op {
	case kTableFixedCount:
		lands[1] = e.dst + 4
	case kTableFixedText:
		lands[1] = e.dst + 4
		lands[2] = e.aux
		term := uint32(0)
		if e.meta != kTableFixedTextBytes {
			term = 1
		}
		lands[3] = e.aux + e.size + term
	case kTableFixedWidenF:
		lands[1] = e.dst + 8
	case kTableFixedWiden, kTableFixedOrdinal:
		lands[1] = e.dst + uint32(e.dstsize)
	default:
		lands[1] = e.dst + e.size
	}
	return lands
}

func fixedFillCount(plan []fixedSizedEntry, cover []ir.TableFixedRange) int32 {
	n := int32(0)
	for _, r := range cover {
		hi := uint32(r.Dst + r.Size)
		pos := uint32(r.Dst)
		for pos < hi {
			end := pos
			next := hi
			for i := range plan {
				lands := fixedEntryLands(plan[i])
				for q := 0; q < 4; q += 2 {
					lo, top := lands[q], lands[q+1]
					if top <= lo {
						continue
					}
					if lo <= pos && pos < top {
						if top > end {
							end = top
						}
					} else if lo > pos && lo < next {
						next = lo
					}
				}
			}
			if end > pos {
				pos = end
				continue
			}
			if next > hi {
				next = hi
			}
			n++
			pos = next
		}
	}
	return n
}

// fixedLineagePlanStorage answers the two numbers the emitted stride is built
// from: the greatest entry count any older lineage entry's compile pushes
// (after the runtime's coalescing), and the greatest pool the compile lays
// above the entries (enum remap tables and the prefill's ranges). ok is false
// when a layout does not parse, or asks for a remap past the uint16 length word;
// such an entry can never produce a plan and the runtime refuses it by name.
func (g *tableGen) fixedLineagePlanStorage(st *ir.Struct, lineage []FixedLineageEntry, own uint64) (maxEntries int, maxPool int) {
	mine := ir.TableFixedWalkRoot(st)
	dst := g.fixedPlanDstRows(st)
	cover := ir.TableFixedIdentityCoverage(g.unit, st)
	for _, le := range lineage {
		if le.Wire == own {
			continue
		}
		writer, ok := fixedParseLineageLayout(le.Layout)
		if !ok {
			continue
		}
		if len(mine) == 0 || len(writer) == 0 {
			continue
		}
		c := &fixedStrideCompiler{writer: writer, mine: mine, dst: dst, record: writer[0].size}
		c.wantGuarded = false
		c.matchChildren(0, 0, 0, 0, fixedNoGuard, 0)
		plain := len(c.plan)
		c.wantGuarded = true
		c.matchChildren(0, 0, 0, 0, fixedNoGuard, 0)
		if c.hostile || c.overflow {
			continue
		}
		// THE ENTRIES ARE PUSHED BEFORE THEY ARE COALESCED, so the array the
		// runtime writes through must hold the RAW count as well as the final
		// one; and the prefill's pool is laid after coalescing, against the
		// final count. Both requirements are covered by the separate maxima.
		rawEntries := len(c.plan)
		rawPool := c.pool
		plan := fixedCoalescePlan(c.plan, plain)
		pool := rawPool
		if fills := fixedFillCount(plan, cover); fills > 0 {
			pool = (pool + fills*8 + 3) &^ 3
		}
		if rawEntries > maxEntries {
			maxEntries = rawEntries
		}
		if len(plan) > maxEntries {
			maxEntries = len(plan)
		}
		if int(rawPool) > maxPool {
			maxPool = int(rawPool)
		}
		if int(pool) > maxPool {
			maxPool = int(pool)
		}
	}
	return maxEntries, maxPool
}
