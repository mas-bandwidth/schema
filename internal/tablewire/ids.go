// THE ID TABLE (docs/SPEC-TABLES.md §3): the trailer that holds every id the
// body used, once each, in FIRST-USE order over the whole wire, which the body
// names by position.
//
// A reference is 1-based, so reference `k` is the table's `k`th entry and
// reference `0` names NO ID — the body terminator, the enum's `None` and the
// union's empty arm, the three places on this wire where "no id" is a value.
package tablewire

// idTable is a writer's half: the ids seen so far, in first-use order, and the
// reference each took. First-use order follows from the WRITE order, so the
// table is built by the walk that writes the body and is emitted where the
// walk ends — a writer never patches.
type idTable struct {
	ids  []uint64
	slot map[uint64]uint64
}

func newIdTable() *idTable { return &idTable{slot: map[uint64]uint64{}} }

// ref is the reference an id takes, appending the id when this is its first
// use. An id already in the table is referenced again and never appended
// twice.
func (t *idTable) ref(id uint64) uint64 {
	if r, seen := t.slot[id]; seen {
		return r
	}
	t.ids = append(t.ids, id)
	r := uint64(len(t.ids))
	t.slot[id] = r
	return r
}

// mark and rollback are how a writer decides ELISION without spending an
// entry: a field that turns out to elide costs nothing in the id table either
// (docs/SPEC-TABLES.md §3), so the walk interns the field's id, encodes the
// payload that decides, and undoes both when nothing rides.
func (t *idTable) mark() int { return len(t.ids) }

func (t *idTable) rollback(mark int) {
	for _, id := range t.ids[mark:] {
		delete(t.slot, id)
	}
	t.ids = t.ids[:mark]
}
