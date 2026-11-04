// THE CONDITIONS DECIDABLE FROM THE WIRE ALONE (docs/SPEC-TABLES.md §6.5, "TWO
// KINDS OF CONDITION"), applied to EVERY byte of a form-1 file before any value
// is decoded: in fields and records this reader declares and in those it does
// not. Skipped is not unread (§4): an unknown field is walked by the same rules
// as a known one, so damage inside it refuses the load.
//
// The walk takes no declaration. It checks the framing (lengths, counts,
// terminators, headers inside their own L, the id table's references, canonical
// LEB128, a kind no reader can skip, kind 32 with a nonzero L), a bool byte
// other than 0 or 1, ill-formed text, a non-finite float, a repeated field id
// or keyed slot in one body, bytes left inside an arm after a nested union,
// bytes left inside an array's, keyed array's or map's L after its elements,
// the node table's framing, a reserved id out of place, a count past the
// storage cap and a blob past its size cap. It collects every kind 17 payload
// in wire order, and then judges the pointer graph: an index past the node
// table, a cycle, a record no pointer reaches, and one blob record named from
// two slots (§6.5, "THE GRAPH, DEFINED ONCE").
//
// tla/ReadOrder.tla models this split: WireDamage, Unreachable, Cycle and
// BlobShared are SpecRefuses' wire-alone disjuncts, and the graph is walked
// over every slot, known or not (the KnownSlotsOnly witness is the bug this
// walk does not have). tla/Framing.tla's header-inside-its-own-L and
// no-read-on rules are the array, keyed and map checks below.
package tablewire

import (
	"encoding/binary"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wireCheck is the declaration-free walk over one file.
type wireCheck struct {
	ids []uint64
	// announcement admits the two reserved announcement ids in the root body,
	// the one body whose transport they are (§3.3)
	announcement bool
	// edges collects the kind 17 payloads of the body being walked, in wire
	// order; checkFile moves them into the node's child list.
	edges []uint64
	fail  error
	// nonFinite is the first NaN or infinity the walk met. It refuses the
	// read like every other condition, but the walk goes on so the decode can
	// name the field where this reader declares it; detection order is free
	// (§6.5).
	nonFinite error
	// depth is the framed level of the payload being walked: 0 in a root body
	// or a node record, one more inside each table, array, keyed or map body
	// (ir.TableBinaryMaxDepth)
	depth int
}

// fixedWidthKind reports the kinds whose payload is exactly their width.
func fixedWidthKind(kind int) bool {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128,
		ir.TableKindTimestamp:
		return true
	}
	return false
}

func (c *wireCheck) notFinite() {
	if c.nonFinite == nil {
		c.nonFinite = &NonFiniteRefusal{Field: "a float the wire carries"}
	}
}

func (c *wireCheck) damage(why string) bool {
	if c.fail == nil {
		c.fail = &Refusal{Reason: ReasonWireDamaged, Detail: why}
	}
	return false
}

func (c *wireCheck) set(err error) bool {
	if c.fail == nil {
		c.fail = err
	}
	return false
}

func (c *wireCheck) refuse(reason, why string) bool {
	if c.fail == nil {
		c.fail = &Refusal{Reason: reason, Detail: why}
	}
	return false
}

// checkFile runs every wire-alone condition over a form-1 file's root body
// (the bytes between the form byte and the id table). It answers nil, or the
// refusal.
// checkFile answers the first refusal, or nil. A non-finite float is answered
// apart as `nonFinite` when it is the only condition met, so the decode can
// name the field (decodeWalk).
func checkFile(body []byte, ids []uint64) (fail, nonFinite error) {
	c := &wireCheck{ids: ids}
	if err := checkBody(c, body); err != nil {
		return err, nil
	}
	return nil, c.nonFinite
}

// checkAnnouncement is checkFile over an announcement's root body (§3.3).
func checkAnnouncement(body []byte, ids []uint64) error {
	c := &wireCheck{ids: ids, announcement: true}
	if err := checkBody(c, body); err != nil {
		return err
	}
	return c.nonFinite
}

func checkBody(c *wireCheck, body []byte) error {
	var table []byte
	present := false
	if !c.body(body, true, func(payload []byte) bool {
		table, present = payload, true
		return true
	}) {
		return c.fail
	}
	// the root body's terminator must be its last byte: bodyExtent checked it
	// before this walk, and body() checks it again for every nested body
	children := [][]uint64{c.edges}
	var blobs []bool
	if present {
		records, ok := scanNodeRecords(table, c.ids)
		if !ok {
			c.damage("the node table cannot be read whole")
			return c.fail
		}
		for _, rec := range records {
			c.edges = nil
			switch rec.TypeId {
			case ir.BytesWireTypeId, ir.StringWireTypeId:
				// A BLOB RECORD IS ITS BYTES (§2.5, §3.1): the reserved type ids
				// are known to every reader, so the content rule of a text blob
				// and the size cap are wire-alone conditions
				if uint64(len(rec.Body)) > math.MaxUint32 {
					c.refuse(ReasonBlobOverSizeCap, "a blob record past the size cap")
					return c.fail
				}
				if rec.TypeId == ir.StringWireTypeId && !textValid(rec.Body) {
					c.damage("a string blob record that is not well-formed text")
					return c.fail
				}
				blobs = append(blobs, true)
			default:
				if !c.body(rec.Body, false, nil) {
					return c.fail
				}
				blobs = append(blobs, false)
			}
			children = append(children, c.edges)
		}
	}
	if !c.graph(children, blobs) {
		return c.fail
	}
	return nil
}

// graph judges the pointer graph: node 0 is the root, node k is record k (node
// index k + 1 on the wire). Every pointer names null, the root or a record; a
// walk from the root that meets a node whose descent is still open is a cycle;
// a record the walk never enters is unreachable; and a blob record named from
// two slots, anywhere in the input, is blob_shared.
func (c *wireCheck) graph(children [][]uint64, blobs []bool) bool {
	n := len(children)
	names := make([]int, n)
	for _, out := range children {
		for _, index := range out {
			if index == ir.NodeWireIndexNull {
				continue
			}
			if index-1 >= uint64(n) {
				return c.damage("a pointer index past the node table")
			}
			names[index-1]++
		}
	}
	for k, blob := range blobs {
		if blob && names[k+1] > 1 {
			return c.refuse(ReasonBlobShared, "one blob record named from two slots")
		}
	}
	mark := make([]uint8, n)
	type frame struct {
		node int
		next int
	}
	stack := []frame{{node: 0}}
	mark[0] = markOpen
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		out := children[top.node]
		if top.next == len(out) {
			mark[top.node] = markClosed
			stack = stack[:len(stack)-1]
			continue
		}
		index := out[top.next]
		top.next++
		if index == ir.NodeWireIndexNull {
			continue
		}
		t := int(index - 1)
		switch mark[t] {
		case markOpen:
			return c.damage("a pointer to a node whose descent is still open: a cycle")
		case markClosed:
			continue
		}
		mark[t] = markOpen
		stack = append(stack, frame{node: t})
	}
	for k := 1; k < n; k++ {
		if mark[k] == markUnvisited {
			return c.damage("a node record no pointer reaches")
		}
	}
	return true
}

// body walks one table body that must end with its terminator as its LAST
// byte. `root` admits the node table's reserved id, whose kind 12 payload is
// handed to `table` instead of being read as text.
func (c *wireCheck) body(buf []byte, root bool, table func([]byte) bool) bool {
	off := 0
	var seen map[uint64]bool
	for {
		ref, next, ok := readLeb(buf, off)
		if !ok {
			return c.damage("a field reference that is truncated or not canonical")
		}
		off = next
		if ref == 0 {
			if off != len(buf) {
				return c.damage("bytes after a body's terminator inside its L")
			}
			return true
		}
		if ref > uint64(len(c.ids)) {
			return c.damage("a field reference past the id table")
		}
		id := c.ids[ref-1]
		if seen == nil {
			seen = map[uint64]bool{}
		}
		if seen[id] {
			return c.damage("the same field id twice in one body")
		}
		seen[id] = true
		if off >= len(buf) {
			return c.damage("a field header cut short")
		}
		kind := int(buf[off])
		off++
		switch id {
		case ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:
			if !root || !c.announcement {
				return c.damage("a reserved announcement id out of place")
			}
		case ir.TableNodeWireId:
			if !root {
				return c.damage("the node table's reserved id in a nested body")
			}
			if kind != ir.TableKindString {
				return c.damage("the node table under a kind other than 12")
			}
			n, after, good := readLeb(buf, off)
			if !good || n > uint64(len(buf)-after) {
				return c.damage("the node table's length runs past the root body")
			}
			off = after + int(n)
			if !table(buf[after:off]) {
				return false
			}
			continue
		}
		if off, ok = c.payload(buf, off, kind); !ok {
			return false
		}
	}
}

// payload walks one SELF-FRAMED payload of `kind` at `off`: what follows a
// field's kind byte, and an array element.
func (c *wireCheck) payload(buf []byte, off int, kind int) (int, bool) {
	switch {
	case fixedWidthKind(kind):
		w := ir.TableKindWidth(kind)
		if w > len(buf)-off {
			return off, c.damage("a fixed-width payload cut short")
		}
		return off + w, c.scalar(buf[off:off+w], kind)
	case kind == ir.TableKindPointer:
		index, next, ok := readLeb(buf, off)
		if !ok {
			return off, c.damage("a node index that is truncated or not canonical")
		}
		c.edges = append(c.edges, index)
		return next, true
	case kind == ir.TableKindEnum:
		ref, next, ok := readLeb(buf, off)
		if !ok {
			return off, c.damage("a variant reference that is truncated or not canonical")
		}
		if ref > uint64(len(c.ids)) {
			return off, c.damage("a variant reference past the id table")
		}
		return next, true
	case kind == ir.TableKindUnion:
		return c.union(buf, off)
	case kind == ir.TableKindString, kind == ir.TableKindWstring, kind == ir.TableKindEscape,
		kind == ir.TableKindNoPayload, kind == ir.TableKindTable, kind == ir.TableKindArray,
		kind == ir.TableKindKeyed:
		n, next, ok := readLeb(buf, off)
		if !ok || n > uint64(len(buf)-next) {
			return off, c.damage("a length past the body carrying it")
		}
		end := next + int(n)
		return end, c.content(buf[next:end], kind)
	}
	return off, c.damage("a kind no reader can skip")
}

// content walks a payload already framed by a length, and requires it to be
// consumed exactly: a field's L, an arm's L, a keyed slot's L.
func (c *wireCheck) content(b []byte, kind int) bool {
	switch {
	case fixedWidthKind(kind):
		if len(b) != ir.TableKindWidth(kind) {
			return c.damage("an L that is not its kind's width")
		}
		return c.scalar(b, kind)
	case kind == ir.TableKindString:
		if !textValid(b) {
			return c.damage("ill-formed text")
		}
		return true
	case kind == ir.TableKindWstring:
		if len(b)&1 != 0 || !wideValid(wideUnits(b)) {
			return c.damage("ill-formed wide text")
		}
		return true
	case kind == ir.TableKindEscape:
		return true // opaque: the one kind whose meaning a reader may not know (§3)
	case kind == ir.TableKindNoPayload:
		if len(b) != 0 {
			return c.damage("kind 32 with a nonzero L")
		}
		return true
	case kind == ir.TableKindTable, kind == ir.TableKindArray, kind == ir.TableKindKeyed:
		// THE BINARY NESTING BOUND (docs/SPEC-TABLES.md §4): a framed body
		// is one level deeper than the payload holding it
		if c.depth >= ir.TableBinaryMaxDepth {
			return c.damage("a framed payload nested past the binary bound")
		}
		c.depth++
		defer func() { c.depth-- }()
		switch kind {
		case ir.TableKindTable:
			return c.body(b, false, nil)
		case ir.TableKindArray:
			return c.array(b)
		}
		return c.keyed(b)
	case kind == ir.TableKindPointer, kind == ir.TableKindEnum, kind == ir.TableKindUnion:
		end, ok := c.payload(b, 0, kind)
		if !ok {
			return false
		}
		if end != len(b) {
			return c.damage("bytes left inside an arm after its payload")
		}
		return true
	}
	return c.damage("a kind no reader can skip")
}

// scalar checks a fixed-width value's bytes: a bool is 0 or 1, and a float is
// finite (§3.3).
func (c *wireCheck) scalar(b []byte, kind int) bool {
	switch kind {
	case ir.TableKindBool:
		if b[0] > 1 {
			return c.damage("a bool byte other than 0 or 1")
		}
	case ir.TableKindF32:
		if !tabletext.FiniteF32(binary.LittleEndian.Uint32(b)) {
			c.notFinite()
		}
	case ir.TableKindF64:
		if !tabletext.FiniteF64(binary.LittleEndian.Uint64(b)) {
			c.notFinite()
		}
	}
	return true
}

// arrayHeader reads the element kind and the count of an array, keyed or map
// content. THE HEADER LIES INSIDE ITS OWN L (§4): content shorter than the
// two bytes a header needs, or a count that runs past it, is damage.
func (c *wireCheck) arrayHeader(b []byte) (ek int, count uint64, off int, ok bool) {
	if len(b) < 2 {
		return 0, 0, 0, c.damage("an array, keyed or map body too short for its header")
	}
	count, off, ok = readLeb(b, 1)
	if !ok {
		return 0, 0, 0, c.damage("an array count that crosses its L or is not canonical")
	}
	if count > math.MaxInt32 {
		return 0, 0, 0, c.set(&CountRefusal{})
	}
	return int(b[0]), count, off, true
}

func (c *wireCheck) array(b []byte) bool {
	ek, count, off, ok := c.arrayHeader(b)
	if !ok {
		return false
	}
	for range count {
		if off, ok = c.payload(b, off, ek); !ok {
			return false
		}
	}
	if off != len(b) {
		return c.damage("bytes left inside an array's L after its elements")
	}
	return true
}

func (c *wireCheck) keyed(b []byte) bool {
	ek, count, off, ok := c.arrayHeader(b)
	if !ok {
		return false
	}
	var seen map[uint64]bool
	for range count {
		key, next, good := readLeb(b, off)
		if !good {
			return c.damage("a keyed slot's key that is truncated or not canonical")
		}
		if key == 0 || key > uint64(len(c.ids)) {
			return c.damage("a keyed slot whose key reference names no id")
		}
		if seen == nil {
			seen = map[uint64]bool{}
		}
		if seen[key] {
			return c.damage("the same keyed slot twice in one keyed body")
		}
		seen[key] = true
		n, after, good := readLeb(b, next)
		if !good || n > uint64(len(b)-after) {
			return c.damage("a keyed slot's length past its body")
		}
		off = after + int(n)
		if !c.content(b[after:off], ek) {
			return false
		}
	}
	if off != len(b) {
		return c.damage("bytes left inside a keyed array's L after its slots")
	}
	return true
}

// union walks a union payload in place: the arm id reference, and for a set
// arm the arm's kind byte, its L and the content L frames.
func (c *wireCheck) union(buf []byte, off int) (int, bool) {
	ref, next, ok := readLeb(buf, off)
	if !ok {
		return off, c.damage("an arm reference that is truncated or not canonical")
	}
	if ref == 0 {
		return next, true
	}
	if ref > uint64(len(c.ids)) {
		return off, c.damage("an arm reference past the id table")
	}
	if next >= len(buf) {
		return off, c.damage("an arm header cut short")
	}
	kind := int(buf[next])
	n, after, good := readLeb(buf, next+1)
	if !good || n > uint64(len(buf)-after) {
		return off, c.damage("an arm's L past the body carrying it")
	}
	end := after + int(n)
	// A UNION IS A LEVEL WHEN ITS ARM IS STRUCTURAL (docs/SPEC-TABLES.md §4):
	// an arm whose payload is a table, an array, a keyed or map body, or
	// another union nests one level deeper; a scalar arm adds none
	if structuralKind(kind) {
		if c.depth >= ir.TableBinaryMaxDepth {
			return off, c.damage("a union arm nested past the binary bound")
		}
		c.depth++
		defer func() { c.depth-- }()
	}
	return end, c.content(buf[after:end], kind)
}

// structuralKind reports the kinds whose payload is a framed container: a
// table body, an array, a keyed or map body, and a union.
func structuralKind(kind int) bool {
	return kind == ir.TableKindTable || kind == ir.TableKindArray || kind == ir.TableKindKeyed || kind == ir.TableKindUnion
}
