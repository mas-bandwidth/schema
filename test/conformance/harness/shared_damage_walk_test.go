package main

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixtureDamage means the independently walked bytes contain structural damage.
// It is not an unsupported-form result, and an empty shape set alongside this
// error does not establish that the entire damaged wire is canonical.
type fixtureDamage struct {
	Issues    []string
	PrefixEnd int
}

func (e *fixtureDamage) Error() string {
	return "fixture framing damage: " + strings.Join(e.Issues, "; ")
}

type fixtureUnsupported struct{ Form byte }

func (e *fixtureUnsupported) Error() string {
	return fmt.Sprintf("fixture walker does not cover form %d", e.Form)
}

type fixtureWalker struct {
	data      []byte
	frame     *wireFrame
	chain     []int
	issues    []string
	prefixEnd int
}

func (s *fixtureWalker) damage(off int, why string) {
	if len(s.issues) == 0 {
		s.prefixEnd = off
	}
	s.issues = append(s.issues, fmt.Sprintf("byte %d: %s", off, why))
}
func (s *fixtureWalker) leb(off, end int, kind wireSpotKind) (uint64, int, bool) {
	var v uint64
	for i := 0; i < 10 && off+i < end; i++ {
		c := s.data[off+i]
		if i == 9 && c > 1 {
			break
		}
		v |= uint64(c&127) << uint(7*i)
		if c&128 == 0 {
			if i > 0 && c == 0 {
				break
			}
			s.frame.spots = append(s.frame.spots, wireSpot{kind: kind, off: off, width: i + 1, limit: end, value: v, leb: true, enclosing: append([]int(nil), s.chain...)})
			return v, off + i + 1, true
		}
	}
	s.damage(off, "incomplete, overflowing or nonminimal LEB128")
	return 0, off, false
}
func (s *fixtureWalker) kind(off, end int, role kindRole) (int, int, bool) {
	if off >= end {
		s.damage(off, "missing kind")
		return 0, off, false
	}
	k := int(s.data[off])
	s.frame.spots = append(s.frame.spots, wireSpot{kind: spotKind, role: role, off: off, width: 1, limit: end, value: uint64(k), enclosing: append([]int(nil), s.chain...)})
	return k, off + 1, true
}

// envelope bounds an L before invoking its walker. A damaged child's contents
// never erase the known end of its envelope, so its later siblings remain walkable.
func (s *fixtureWalker) envelope(off, end int, arm, body bool, inner func(int, int)) (int, bool) {
	n, a, ok := s.leb(off, end, spotLength)
	if !ok {
		return off, false
	}
	key := len(s.frame.spots) - 1
	s.frame.spots[key].arm = arm
	s.frame.spots[key].body = body
	if n > uint64(end-a) {
		s.damage(off, "length exceeds enclosing boundary")
		return off, false
	}
	stop := a + int(n)
	s.chain = append(s.chain, key)
	if inner != nil {
		inner(a, stop)
	}
	s.chain = s.chain[:len(s.chain)-1]
	return stop, true
}
func (s *fixtureWalker) body(off, end int, root bool) {
	if len(s.chain) > 128 {
		s.damage(off, "structural depth exceeds 128")
		return
	}
	seen := map[uint64]bool{}
	for off < end {
		begin, mark := off, len(s.frame.spots)
		ref, a, ok := s.leb(off, end, spotRef)
		if !ok {
			return
		}
		off = a
		if ref == 0 {
			if off != end {
				s.damage(off, "bytes after body terminator")
			}
			return
		}
		id, named := s.frame.entryOf(ref)
		if !named {
			s.frame.spots = s.frame.spots[:mark]
			s.damage(begin, "field reference outside ID table")
			return
		}
		if seen[id] {
			s.frame.spots = s.frame.spots[:mark]
			s.damage(begin, "duplicate field ID")
			return
		}
		seen[id] = true
		k, a, ok := s.kind(off, end, roleField)
		if !ok {
			s.frame.spots = s.frame.spots[:mark]
			return
		}
		off, ok = s.payload(k, a, end, root && id == ir.TableNodeWireId)
		if !ok {
			return
		}
		s.frame.fields = append(s.frame.fields, wireField{start: begin, end: off, enclosing: append([]int(nil), s.chain...)})
	}
	s.damage(end, "missing body terminator")
}
func (s *fixtureWalker) payload(k, off, end int, nodes bool) (int, bool) {
	switch k {
	case ir.TableKindPointer, ir.TableKindEnum:
		kind := spotIndex
		if k == ir.TableKindEnum {
			kind = spotVariant
		}
		ref, a, ok := s.leb(off, end, kind)
		if ok && k == ir.TableKindEnum && ref > uint64(len(s.frame.entries)) {
			s.damage(off, "enum reference outside ID table")
			return off, false
		}
		return a, ok
	case ir.TableKindString:
		if nodes {
			return s.envelope(off, end, false, false, s.nodes)
		}
		return s.envelope(off, end, false, false, nil)
	case ir.TableKindTable:
		return s.envelope(off, end, false, true, func(a, b int) { s.body(a, b, false) })
	case ir.TableKindArray, ir.TableKindKeyed:
		// Header failure leaves no interpretation of the remainder of this body.
		// A valid child envelope still lets an enclosing collection continue.
		valid := true
		a, ok := s.envelope(off, end, false, false, func(a, b int) { valid = s.collection(a, b, k == ir.TableKindKeyed) })
		return a, ok && valid
	case ir.TableKindUnion:
		return s.arm(off, end)
	case ir.TableKindEscape:
		return s.envelope(off, end, false, false, nil)
	case ir.TableKindNoPayload:
		valid := true
		a, ok := s.envelope(off, end, false, false, func(a, b int) {
			if a != b {
				s.damage(a, "payload-free kind has bytes")
				valid = false
			}
		})
		return a, ok && valid
	default:
		w := kindWidth(k)
		if w == 0 {
			s.damage(off, "kind outside closed set")
			return off, false
		}
		if w > end-off {
			s.damage(off, "scalar crosses enclosing boundary")
			return off, false
		}
		return off + w, true
	}
}

// raw walks an arm/triple payload whose L already supplied the framing.
func (s *fixtureWalker) raw(k, a, b int) {
	switch k {
	case ir.TableKindTable:
		s.body(a, b, false)
	case ir.TableKindArray, ir.TableKindKeyed:
		s.collection(a, b, k == ir.TableKindKeyed)
	case ir.TableKindString, ir.TableKindEscape:
	case ir.TableKindNoPayload:
		if a != b {
			s.damage(a, "payload-free kind has bytes")
		}
	default:
		n, ok := s.payload(k, a, b, false)
		if ok && n != b {
			s.damage(n, "bytes after framed payload")
		}
	}
}
func (s *fixtureWalker) arm(off, end int) (int, bool) {
	if len(s.chain) > 128 {
		s.damage(off, "structural depth exceeds 128")
		return off, false
	}
	ref, a, ok := s.leb(off, end, spotArm)
	if !ok {
		return off, false
	}
	if ref == 0 {
		return a, true
	}
	if _, ok := s.frame.entryOf(ref); !ok {
		s.damage(off, "arm reference outside ID table")
		return off, false
	}
	k, a, ok := s.kind(a, end, roleArm)
	if !ok {
		return off, false
	}
	return s.envelope(a, end, true, k == ir.TableKindTable, func(a, b int) { s.raw(k, a, b) })
}
func (s *fixtureWalker) collection(off, end int, keyed bool) bool {
	if len(s.chain) > 128 {
		s.damage(off, "structural depth exceeds 128")
		return false
	}
	if end-off < 2 {
		s.damage(off, "array header does not fit its length")
		return false
	}
	role := roleElement
	if keyed {
		role = roleKeyedElement
	}
	k, a, ok := s.kind(off, end, role)
	if !ok {
		return false
	}
	// Nested positional arrays have no legal element framing in this major.
	if k == ir.TableKindArray || k == ir.TableKindKeyed || k == ir.TableKindEscape || k == ir.TableKindNoPayload || k < 1 || k > ir.TableKindEnum || (keyed && (k == ir.TableKindString || k == ir.TableKindUnion || k == ir.TableKindPointer)) {
		s.damage(off, "element kind outside closed set")
		return false
	}
	n, a, ok := s.leb(a, end, spotCount)
	if !ok {
		return false
	}
	// Every element needs at least one byte, preventing count-driven work on an
	// adversarial count without allocating anything proportional to that count.
	if n > uint64(end-a) {
		s.damage(a, "count exceeds available element bytes")
		return false
	}
	for i := uint64(0); i < n; i++ {
		if keyed {
			ref, b, ok := s.leb(a, end, spotKey)
			if !ok {
				return false
			}
			if ref == 0 || ref > uint64(len(s.frame.entries)) {
				s.damage(a, "keyed reference outside ID table")
				return false
			}
			a, ok = s.envelope(b, end, false, k == ir.TableKindTable, func(x, y int) { s.raw(k, x, y) })
			if !ok {
				return false
			}
		} else {
			a, ok = s.payload(k, a, end, false)
			if !ok {
				return false
			}
		}
	}
	if a != end {
		s.damage(a, "bytes after collection elements")
		return false
	}
	return true
}
func (s *fixtureWalker) nodes(off, end int) {
	n, a, ok := s.leb(off, end, spotNodeCount)
	if !ok {
		return
	}
	if n > uint64((end-a)/2) {
		s.damage(a, "node count exceeds available record headers")
		return
	}
	for i := uint64(0); i < n; i++ {
		ref, b, ok := s.leb(a, end, spotRecordType)
		if !ok {
			return
		}
		id, named := s.frame.entryOf(ref)
		if !named {
			s.damage(a, "node type reference outside ID table")
			return
		}
		var inner func(int, int)
		if id != ir.BytesWireTypeId && id != ir.StringWireTypeId {
			inner = func(x, y int) { s.body(x, y, false) }
		}
		a, ok = s.envelope(b, end, false, false, inner)
		if !ok {
			return
		}
		s.frame.records++
	}
	if a != end {
		s.damage(a, "bytes after node records")
	}
}

// damageAwareFrame is independent of frameWire, frameScanner and the production
// reader. It covers variable file framing, not value legality (UTF-8, finite
// floats, declaration bounds or graph reachability). Only completed fields are
// exposed to the six-shape classifier. Damage is retained as an explicit result.
func damageAwareFrame(data []byte) (*wireFrame, error) {
	f := &wireFrame{}
	if len(data) == 0 {
		return f, &fixtureDamage{Issues: []string{"missing form"}}
	}
	if data[0] != 1 {
		return f, &fixtureUnsupported{Form: data[0]}
	}
	if len(data) < 9 {
		return f, &fixtureDamage{Issues: []string{"missing ID-table count"}}
	}
	count := binary.LittleEndian.Uint64(data[len(data)-8:])
	if count > uint64((len(data)-9)/8) {
		return f, &fixtureDamage{Issues: []string{"ID-table count exceeds file"}}
	}
	f.countAt = len(data) - 8
	f.entriesAt = f.countAt - int(count)*8
	f.bodyStart = 1
	f.bodyEnd = f.entriesAt
	f.entries = make([]uint64, int(count))
	for i := range f.entries {
		f.entries[i] = binary.LittleEndian.Uint64(data[f.entriesAt+i*8:])
	}
	s := &fixtureWalker{data: data, frame: f}
	s.body(1, f.bodyEnd, true)
	if len(s.issues) > 0 {
		return f, &fixtureDamage{Issues: s.issues, PrefixEnd: s.prefixEnd}
	}
	return f, nil
}

// damageAwareFileSpellings retains observed shapes plus distinguishable framing
// damage. A damaged suffix is unclassified, never silently declared canonical.
func damageAwareFileSpellings(u *ir.Unit, root *ir.Struct, data []byte) (map[string]bool, error) {
	f, err := damageAwareFrame(data)
	out := map[string]bool{}
	scopes := damageAwareScopes(u, root, f)
	complete := map[int]bool{}
	for _, field := range f.fields {
		complete[field.start] = true
	}
	nodePos, fields := -1, 0
	for i, sp := range f.spots {
		if sp.kind == spotNodeCount && sp.value == 0 {
			out["empty-node-table"] = true
		}
		if sp.kind != spotRef || sp.value == 0 || i+1 >= len(f.spots) || !complete[sp.off] {
			continue
		}
		key := -1
		if len(sp.enclosing) > 0 {
			key = sp.enclosing[len(sp.enclosing)-1]
		}
		st := scopes[key].owner
		if st == nil {
			continue
		}
		id, ok := f.entryOf(sp.value)
		if !ok {
			continue
		}
		if key == -1 {
			if id == ir.TableNodeWireId {
				nodePos = fields
			}
			fields++
		}
		fd := scopeField(st, id)
		if fd == nil {
			continue
		}
		if fd.Required || fd.Deprecated || fd.Type.Optional || (st.MapEntryOf != "" && (fd.Name == "key" || fd.Name == "value")) {
			continue
		}
		k := f.spots[i+1]
		if k.kind != spotKind || k.role != roleField || int(k.value) != ir.TableWireFieldKind(fd) || i+2 >= len(f.spots) {
			continue
		}
		next := f.spots[i+2]
		if k.value == ir.TableKindPointer && next.kind == spotIndex && next.value == 0 {
			out["explicit-null-pointer"] = true
		}
		if k.value == ir.TableKindUnion && next.kind == spotArm && next.value == 0 {
			out["union-arm-zero"] = true
		}
	}
	if nodePos >= 0 && nodePos != fields-1 {
		out["node-table-out-of-place"] = true
	}
	clean := *f
	clean.spots = append([]wireSpot(nil), f.spots...)
	for i := range clean.spots {
		if clean.spots[i].kind == spotRef && !complete[clean.spots[i].off] {
			clean.spots[i].value = 0
		}
	}
	for key, scope := range scopes {
		if scope.owner != nil && len(bodyStoredDefaults(scope.owner, data, &clean, key)) > 0 {
			out["stored-default"] = true
		}
	}
	return out, err
}

// damageAwareIDTableFault judges duplicates even on damaged framing. First-use
// order is judged only on the trustworthy prefix; unused IDs require a complete
// walk. A suffix the walker cannot frame may use any previously unused ID.
func damageAwareIDTableFault(data []byte) (string, error) {
	f, err := damageAwareFrame(data)
	if f.countAt == 0 {
		return "", err
	}
	seenIDs := map[uint64]bool{}
	for _, id := range f.entries {
		if seenIDs[id] {
			return "duplicate ID-table entry", err
		}
		seenIDs[id] = true
	}
	limit := len(data)
	if damage, ok := err.(*fixtureDamage); ok {
		limit = damage.PrefixEnd
	}
	used := map[uint64]bool{}
	next := uint64(1)
	for _, sp := range f.spots {
		if sp.off >= limit {
			continue
		}
		switch sp.kind {
		case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
		default:
			continue
		}
		if sp.value == 0 || sp.value > uint64(len(f.entries)) || used[sp.value] {
			continue
		}
		used[sp.value] = true
		if sp.value != next {
			return fmt.Sprintf("first use of ID slot %d where slot %d is next", sp.value, next), err
		}
		next++
	}
	if err == nil && len(used) != len(f.entries) {
		return "unused ID-table entries", nil
	}
	return "", err
}

// damageAwareScopes requires a header and its length to have the same actual
// enclosing chain. A truncated child cannot lend its last scalar kind to the
// next sibling's length. No adjacency across a boundary establishes ownership.
func damageAwareScopes(u *ir.Unit, root *ir.Struct, f *wireFrame) map[int]nestedScope {
	scopes := map[int]nestedScope{-1: {owner: root}}
	nodeTypes := map[uint64]*ir.Struct{}
	for _, st := range u.Tables {
		nodeTypes[ir.TableTypeId(st.WireName())] = st
	}
	for i, sp := range f.spots {
		if sp.kind != spotLength {
			continue
		}
		parentKey := -1
		if len(sp.enclosing) > 0 {
			parentKey = sp.enclosing[len(sp.enclosing)-1]
		}
		parent := scopes[parentKey]
		scope := nestedScope{}
		if i > 0 && f.spots[i-1].kind == spotRecordType && slices.Equal(f.spots[i-1].enclosing, sp.enclosing) {
			if id, ok := f.entryOf(f.spots[i-1].value); ok {
				scope.owner = nodeTypes[id]
			}
		} else if i >= 2 && f.spots[i-1].kind == spotKind && f.spots[i-1].role == roleField && f.spots[i-2].kind == spotRef && slices.Equal(f.spots[i-1].enclosing, sp.enclosing) && slices.Equal(f.spots[i-2].enclosing, sp.enclosing) {
			id, _ := f.entryOf(f.spots[i-2].value)
			fd := scopeField(parent.owner, id)
			if fd != nil && !fd.Deprecated && int(f.spots[i-1].value) == ir.TableWireFieldKind(fd) {
				switch f.spots[i-1].value {
				case ir.TableKindTable:
					scope.owner = scopeTarget(fd)
				case ir.TableKindArray, ir.TableKindKeyed:
					scope.element = scopeTarget(fd)
					scope.union, _ = fd.Type.Ref.(*ir.Union)
				}
			}
		} else if i > 0 && f.spots[i-1].kind == spotKey && slices.Equal(f.spots[i-1].enclosing, sp.enclosing) {
			scope.owner = parent.element
		} else if sp.body && parent.element != nil {
			scope.owner = parent.element
		}
		// A table-valued union arm carries the arm's declaration, not the union's.
		if sp.arm && i >= 2 && f.spots[i-1].kind == spotKind && f.spots[i-2].kind == spotArm && slices.Equal(f.spots[i-1].enclosing, sp.enclosing) && slices.Equal(f.spots[i-2].enclosing, sp.enclosing) {
			un := parent.union
			if un == nil && parent.owner != nil {
				// The arm belongs to the closest enclosing declared union field.
				for j := i - 3; j >= 0; j-- {
					q := f.spots[j]
					if !slices.Equal(q.enclosing, sp.enclosing) {
						continue
					}
					if q.kind == spotRef {
						id, _ := f.entryOf(q.value)
						fd := scopeField(parent.owner, id)
						if fd != nil {
							un, _ = fd.Type.Ref.(*ir.Union)
						}
						break
					}
				}
			}
			if un != nil {
				if id, ok := f.entryOf(f.spots[i-2].value); ok {
					for _, arm := range un.Variants {
						if arm.F != nil && ir.TableFieldWireId(arm.F) == id {
							scope.owner = scopeTarget(arm.F)
							scope.union, _ = arm.F.Type.Ref.(*ir.Union)
							break
						}
					}
				}
			}
		}
		scopes[i] = scope
	}
	return scopes
}
