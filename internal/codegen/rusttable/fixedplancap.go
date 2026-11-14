package rusttable

// THE PLAN CAPACITY IS THE PLAN'S, NOT THE RECORD'S.
//
// The generated reader lays one plan per locked layout down at build time,
// through the same walk the runtime's plan compiler runs (fixedruntime.go,
// table_fixed_compile). That walk writes entries into the plan's storage as it
// goes, BEFORE it coalesces, so the storage has to hold the walk's peak entry
// count. The count is a fact about the OLD entry's layout and this reader's
// layout, and it can be far larger than either record's byte size: a union
// under an array contributes a constant per arm and one entry per arm slot, so
// the array bound multiplies the arm count while the record stays one tag byte
// an element.
//
// This is that walk at codegen time, counting only what the runtime counts — a
// `push` per entry — so the emitter can bake the exact maximum across the
// lineage instead of guessing from the record's size. It mirrors
// `match_children`/`compile_entry` in fixedruntime.go; a change to that walk
// changes this counter with it, and the lineage test drives the generated
// reader over an older file to prove the number covers the real plan.

import (
	"encoding/binary"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type fixedCapEntry struct {
	id       uint64
	kind     uint8
	size     uint32
	children uint32
}

type fixedCapLayout struct {
	entries []fixedCapEntry
}

func fixedCapParse(b []byte) (*fixedCapLayout, bool) {
	if len(b) < ir.TableFixedLayoutHeaderBytes {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	if n <= 0 || ir.TableFixedLayoutHeaderBytes+ir.TableFixedEntryBytes*n != len(b) {
		return nil, false
	}
	entries := make([]fixedCapEntry, n)
	for i := range entries {
		at := ir.TableFixedLayoutHeaderBytes + i*ir.TableFixedEntryBytes
		entries[i] = fixedCapEntry{
			id:       binary.LittleEndian.Uint64(b[at:]),
			kind:     b[at+8],
			size:     binary.LittleEndian.Uint32(b[at+9:]),
			children: binary.LittleEndian.Uint32(b[at+13:]),
		}
	}
	if entries[0].kind != 13 { // the root is a TABLE or the layout is not one this reader compiles
		return nil, false
	}
	return &fixedCapLayout{entries: entries}, true
}

func (l *fixedCapLayout) entry(i int) fixedCapEntry {
	if i < 0 || i >= len(l.entries) {
		return fixedCapEntry{}
	}
	return l.entries[i]
}

func (l *fixedCapLayout) subtree(i int) int {
	if n, ok := l.subtreeAt(i, 0); ok {
		return n
	}
	return 1
}

func (l *fixedCapLayout) subtreeAt(i int, depth uint32) (int, bool) {
	if depth > ir.TableFixedMaxDepth || i < 0 || i >= len(l.entries) {
		return 0, false
	}
	e := l.entries[i]
	n, at := 1, i+1
	for k := 0; k < int(e.children); k++ {
		if at >= len(l.entries) {
			return 0, false
		}
		sub, ok := l.subtreeAt(at, depth+1)
		if !ok {
			return 0, false
		}
		at += sub
		n += sub
	}
	return n, true
}

func (l *fixedCapLayout) tagBytes(i int) uint32 {
	e := l.entry(i)
	var widest uint32
	at := i + 1
	for k := 0; k < int(e.children); k++ {
		if at >= len(l.entries) {
			break
		}
		if a := l.entry(at); a.size > widest {
			widest = a.size
		}
		at += l.subtree(at)
	}
	if e.size > widest {
		return e.size - widest
	}
	return 0
}

func fixedCapAdd(a, b uint32) uint32 {
	if sum := a + b; sum >= a {
		return sum
	}
	return ^uint32(0)
}

type fixedCapCounter struct {
	count int
}

func (c *fixedCapCounter) push() { c.count++ }

func fixedCapPlanCount(theirs, mine *fixedCapLayout, counted []byte) int {
	c := &fixedCapCounter{}
	c.matchChildren(theirs, 0, 0, mine, 0, 0, counted, 0)
	return c.count
}

// matchChildren mirrors the runtime's walk of a TABLE's children, matching by
// id. The compile census is not counted here: it moves a report counter, never
// a plan entry.
func (c *fixedCapCounter) matchChildren(theirs *fixedCapLayout, ti int, theirAt uint32, mine *fixedCapLayout, mi int, myAt uint32, counted []byte, depth uint32) {
	if depth > ir.TableFixedMaxDepth {
		return
	}
	te, me := theirs.entry(ti), mine.entry(mi)
	myChild, myOff := mi+1, myAt
	for k := 0; k < int(me.children); k++ {
		mc := mine.entry(myChild)
		theirChild, theirOff := ti+1, theirAt
		for j := 0; j < int(te.children); j++ {
			tc := theirs.entry(theirChild)
			if tc.id == mc.id {
				c.compileEntry(theirs, theirChild, theirOff, mine, myChild, myOff, counted, depth+1)
				break
			}
			theirOff = fixedCapAdd(theirOff, tc.size)
			theirChild += theirs.subtree(theirChild)
		}
		myOff = fixedCapAdd(myOff, mc.size)
		myChild += mine.subtree(myChild)
	}
}

// compileEntry mirrors the runtime's `compile_entry`, counting each entry the
// walk would push. The guard chain is not modelled: it bounds neither the
// plan's entry count nor whether one is pushed, only where its links live.
func (c *fixedCapCounter) compileEntry(theirs *fixedCapLayout, ti int, theirAt uint32, mine *fixedCapLayout, mi int, myAt uint32, counted []byte, depth uint32) {
	if depth > ir.TableFixedMaxDepth {
		return
	}
	te, me := theirs.entry(ti), mine.entry(mi)
	if me.kind == 35 && te.kind != 35 {
		// T INTO ?T: the present constant, then the payload against the wrapper's child.
		c.push()
		c.compileEntry(theirs, ti, theirAt, mine, mi+1, myAt+1, counted, depth+1)
		return
	}
	if te.kind != me.kind {
		if fixedCapWidens(te.kind, me.kind) {
			c.push()
		}
		return // a kind that moved is reported, never reinterpreted, and lands no entry
	}
	switch me.kind {
	case 35: // the optional wrapper: present byte, then payload
		c.push()
		c.compileEntry(theirs, ti+1, fixedCapAdd(theirAt, 1), mine, mi+1, fixedCapAdd(myAt, 1), counted, depth+1)
	case 13: // a table
		c.matchChildren(theirs, ti, theirAt, mine, mi, myAt, counted, depth+1)
	case 14: // an array: the count, then min(their bound, my bound) elements
		tel, mel := theirs.entry(ti+1), mine.entry(mi+1)
		head := uint32(0)
		if mi < len(counted) && counted[mi] != 0 {
			head = 4
		}
		theirN, myN := uint32(0), uint32(0)
		if tel.size != 0 && te.size > head {
			theirN = (te.size - head) / tel.size
		}
		if mel.size != 0 && me.size > head {
			myN = (me.size - head) / mel.size
		}
		if head != 0 {
			c.push()
		}
		n := min(theirN, myN)
		for i := uint32(0); i < n; i++ {
			c.compileEntry(
				theirs, ti+1, fixedCapAdd(theirAt, head+i*tel.size),
				mine, mi+1, fixedCapAdd(myAt, head+i*mel.size),
				counted, depth+1,
			)
		}
	case 16: // an enum-keyed array: every slot, matched by the key's id
		tkey, mkey := theirs.entry(ti+1), mine.entry(mi+1)
		tel := ti + 1 + theirs.subtree(ti+1)
		mel := mi + 1 + mine.subtree(mi+1)
		tee, mee := theirs.entry(tel), mine.entry(mel)
		for k := 0; k < int(mkey.children); k++ {
			keyID := mine.entry(mi + 2 + k).id
			for j := 0; j < int(tkey.children); j++ {
				if theirs.entry(ti+2+j).id != keyID {
					continue
				}
				c.compileEntry(
					theirs, tel, fixedCapAdd(theirAt, uint32(j)*tee.size),
					mine, mel, fixedCapAdd(myAt, uint32(k)*mee.size),
					counted, depth+1,
				)
				break
			}
		}
	case 15: // a union: the None constant, then each arm's constant and payload
		theirTag, myTag := theirs.tagBytes(ti), mine.tagBytes(mi)
		c.push()
		myArm := mi + 1
		for k := 0; k < int(me.children); k++ {
			ma := mine.entry(myArm)
			theirArm := ti + 1
			for j := 0; j < int(te.children); j++ {
				ta := theirs.entry(theirArm)
				if ta.id == ma.id {
					c.push()
					c.compileEntry(
						theirs, theirArm, fixedCapAdd(theirAt, theirTag),
						mine, myArm, fixedCapAdd(myAt, myTag),
						counted, depth+1,
					)
					break
				}
				theirArm += theirs.subtree(theirArm)
			}
			myArm += mine.subtree(myArm)
		}
	case 30: // an enum: an ordinal remap, or a widen when the writer's width grew
		c.push()
	case 12: // text: the length and the content
		c.push()
	default:
		if te.size == me.size || (te.size < me.size && me.size <= 8) {
			c.push()
		}
	}
}

// fixedCapWidens is the runtime's widening ladder (§4): the same family at a
// wider rung, or f32 into f64.
func fixedCapWidens(from, to uint8) bool {
	switch {
	case from >= 6 && from <= 9 && to >= 6 && to <= 9, // u8 .. u64
		from >= 2 && from <= 5 && to >= 2 && to <= 5,     // i8 .. i64
		from >= 20 && from <= 24 && to >= 20 && to <= 24, // signed fixed-point
		from >= 25 && from <= 29 && to >= 25 && to <= 29: // unsigned fixed-point
		return to > from
	case from == 10 && to == 11: // f32 -> f64
		return true
	}
	return false
}

// fixedLineagePlanCap is the exact maximum plan entry count over every lineage
// entry this build compiles, with the identity plan's one entry as the floor.
// A lineage entry that is not a parseable layout is skipped: the runtime records
// layout_malformed for it and compiles no plan, so it needs no storage.
func fixedLineagePlanCap(own []byte, ownEntries []ir.TableFixedLayoutEntry, lineage []FixedLineageEntry, ownIndex int) int {
	cap := 1 // the identity plan is one Copy of the whole body
	mine, ok := fixedCapParse(own)
	if !ok {
		return cap
	}
	counted := make([]byte, len(ownEntries))
	for i, e := range ownEntries {
		if fixedCounted(e) {
			counted[i] = 1
		}
	}
	for i, e := range lineage {
		if i == ownIndex {
			continue
		}
		theirs, ok := fixedCapParse(e.Layout)
		if !ok {
			continue
		}
		if n := fixedCapPlanCount(theirs, mine, counted); n > cap {
			cap = n
		}
	}
	return cap
}
