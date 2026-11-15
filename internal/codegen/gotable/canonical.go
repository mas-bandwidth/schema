package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

// takeElides returns whether the field read being emitted is one its writer
// elides at its default (g.elides), and clears it, so the first emitter that
// reads the field's wire takes it and no element or nested read sees it.
func (g *tableGen) takeElides() bool {
	elide := g.elides
	g.elides = false
	return elide
}

// emitElidedRefusal refuses a field read whose value is the one its writer
// elides (docs/SPEC-TABLES.md §3, §4), for every field shape whose stored value
// says so exactly: the writer's own ride test (emitFieldRides) on the value
// read. The shapes whose stored value cannot say it — an enum (an unknown or a
// deprecated variant lands None or the default), a pointer (a record of a type
// this reader cannot name lands null), a union (an unknown arm lands None), a
// nested table, a keyed array, a list, a map and unbounded text — are refused
// where their emitter reads the wire (g.elides), and an array of enums, unions
// or pointers is not held to it, for the same reason.
func (g *tableGen) emitElidedRefusal(f *ir.Field, expr, ind string) {
	g.emitElidedRefusalWhen(f, expr, ind, "")
}

// emitElidedRefusalWhen is emitElidedRefusal under a condition the read
// decides at run time (the message form's same build version), "" for none.
func (g *tableGen) emitElidedRefusalWhen(f *ir.Field, expr, ind, when string) {
	switch {
	case f.Required || f.Type.Optional, f.IsList(), f.IsMap(), f.KeyEnum != "", f.Type.TextUnbounded():
		return
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
	case f.Array == ir.ArrayFixed:
		if enumRef(f) != nil || isUnionRef(f.Type) || isStructRef(f.Type) || f.Type.Pointer || f.Type.Kind == ir.TString {
			return
		}
	case f.Array == ir.ArrayCounted:
	case enumRef(f) != nil || isUnionRef(f.Type) || isStructRef(f.Type):
		return
	}
	rides := g.emitFieldRides(f, expr, ind)
	g.pf("%sif %s!(%s) { %s; return false }\n", ind, when, rides, damaged)
}

// tableFirstUseSource is the open's first-use walk, emitted beside the wire
// runtime (tableWireSource) into every unit that reads form 1.
const tableFirstUseSource = `
// THE ID TABLE IS IN FIRST-USE ORDER, AND THE NODE TABLE IN ITS PLACE
// (docs/SPEC-TABLES.md §3, §3.1): a table holding an entry no reference names,
// or one out of its place, and a node table that numbers no record or is not
// the root body's last field, are spellings no conforming writer produces, so
// the open refuses them. Every reference is met in byte order, so the walk
// keeps one number, the next entry a new reference may name, and work linear
// in the file. Framing it cannot walk is the body read's to refuse: the walk
// answers only what it saw whole.
type tableFirstUse struct {
	next uint64
	depth int
	bad bool
}

func tableFirstUseHolds(r TableReader, n uint64) bool {
	u := tableFirstUse{next: 1}
	if !u.body(&r, true) { return !u.bad }
	return u.next == n+1
}

// ref meets one reference: an entry already named, or the next one.
func (u *tableFirstUse) ref(k uint64) bool {
	if k > u.next { u.bad = true; return false }
	if k == u.next { u.next++ }
	return true
}

// body walks one body to its terminator. Past four times the nesting bound
// the file is damage the body read refuses, so the walk stops there.
func (u *tableFirstUse) body(r *TableReader, root bool) bool {
	if u.depth > 4*tableBinaryMaxDepth { return false }
	u.depth++
	ok := u.fields(r, root)
	u.depth--
	return ok
}

func (u *tableFirstUse) fields(r *TableReader, root bool) bool {
	nodes := false
	for {
		k, ok := r.Leb()
		if !ok { return false }
		if k == 0 { return true }
		if nodes { u.bad = true; return false }
		if !u.ref(k) { return false }
		id, ok := r.Resolve(k)
		if !ok || !r.Has(1) { return false }
		kind := r.Get8()
		if root && id == 0xffffffffffffffff && kind == 12 {
			sub, ok := r.Body()
			if !ok || !u.records(&sub) { return false }
			nodes = true
			continue
		}
		if !u.value(r, kind) { return false }
	}
}

// records walks the node table's payload: its count, then each record's type
// reference and, for a table body, the body.
func (u *tableFirstUse) records(r *TableReader) bool {
	count, ok := r.Leb()
	if !ok { return false }
	if count == 0 { u.bad = true; return false }
	for r.Offset < int64(len(r.Buffer)) {
		k, ok := r.Leb()
		if !ok || !u.ref(k) { return false }
		id, ok := r.Resolve(k)
		if !ok { return false }
		body, ok := r.Body()
		if !ok { return false }
		if id != tableWireBytesID && id != tableWireStringID && !u.body(&body, false) { return false }
	}
	return true
}

// value walks one payload of a kind, a field's or an element's.
func (u *tableFirstUse) value(r *TableReader, kind uint8) bool {
	switch kind {
	case 30:
		k, ok := r.Leb()
		return ok && (k == 0 || u.ref(k))
	case 15:
		return u.union(r)
	case 13, 14, 16:
		sub, ok := r.Body()
		return ok && u.framed(&sub, kind)
	}
	return r.Skip(kind)
}

// framed walks the content of an L-framed payload of a kind: a nested body,
// an array or keyed body, a keyed slot's element or an arm's payload.
func (u *tableFirstUse) framed(r *TableReader, kind uint8) bool {
	switch kind {
	case 13:
		return u.body(r, false)
	case 14, 16:
		if !r.Has(2) || u.depth > 4*tableBinaryMaxDepth { return false }
		u.depth++
		defer func() { u.depth-- }()
		elem := r.Get8()
		count, ok := r.Leb()
		if !ok || count > uint64(len(r.Buffer)) { return false }
		for range count {
			if kind == 16 {
				k, ok := r.Leb()
				if !ok || k == 0 || !u.ref(k) { return false }
				sub, ok := r.Body()
				if !ok || !u.framed(&sub, elem) { return false }
			} else if !u.value(r, elem) {
				return false
			}
		}
		return true
	case 15:
		return u.union(r)
	case 30:
		return u.value(r, kind)
	}
	return true
}

func (u *tableFirstUse) union(r *TableReader) bool {
	k, ok := r.Leb()
	if !ok { return false }
	if k == 0 { return true }
	if !u.ref(k) || !r.Has(1) || u.depth > 4*tableBinaryMaxDepth { return false }
	kind := r.Get8()
	arm, ok := r.Body()
	if !ok { return false }
	u.depth++
	ok = u.framed(&arm, kind)
	u.depth--
	return ok
}
`
