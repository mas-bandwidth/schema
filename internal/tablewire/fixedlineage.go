// THE FIXED FORM'S OLDER WRITERS, READ THROUGH THE LOCK (docs/SPEC-TABLES.md
// §3.4, §21; docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3). A form-3 file whose
// header hash is not this build's own is matched against the unit's
// schema.lock lineage and nowhere else: outside it is layout_unknown, below the
// floor is layout_unsupported, and a file whose layout bytes differ from the
// lock's for that hash is layout_malformed. The file's own layout is compared
// and never parsed; the walk below reads the LOCK's bytes.
//
// THE READ IS STRICT: it produces the value or ONE refusal. A writer's field
// the reader declares lands, widened where the reader's kind is wider; a field
// the reader no longer declares, an enum variant or a union arm it cannot name
// is unknown and lands nothing (None for the variant and the arm); a field the
// writer never had keeps the reader's declared default. A value outside the
// reader's declared range, a count or a used length past the reader's bound, a
// kind the reader does not widen to and a forged ordinal or tag each refuse the
// read by name. Nothing is clamped.
package tablewire

import (
	"encoding/binary"
	"slices"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The lineage read's refusal names, beside the identity read's.
const (
	FixedReasonLayoutUnsupported = "layout_unsupported"
	FixedReasonKindMismatch      = "kind_mismatch"
	FixedReasonOutOfRange        = "out_of_range"
)

// unitLocks caches each unit's parsed lock: one Open per unit, and a nil entry
// for a unit with no lock, which promises nothing (§5.9 #2).
var unitLocks sync.Map // *ir.Unit -> *lockfile.Unit

func unitLock(u *ir.Unit) (*lockfile.Unit, error) {
	if v, ok := unitLocks.Load(u); ok {
		return v.(*lockfile.Unit), nil
	}
	var paths []string
	for _, f := range u.Files {
		if f != nil && f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	var lock *lockfile.Unit
	if len(paths) > 0 {
		l, ok, err := lockfile.Open(paths)
		if err != nil {
			return nil, err
		}
		if ok {
			lock = l
		}
	}
	unitLocks.Store(u, lock)
	return lock, nil
}

// fixedNode is one entry of a locked layout with its subtree: the seventeen
// bytes §3.4 gives an entry, read from the LOCK.
type fixedNode struct {
	id       uint64
	kind     int
	size     int64
	children []*fixedNode
}

// parseFixedLayout builds the tree of a layout the lock holds, or nil for bytes
// that are not one.
func parseFixedLayout(b []byte) *fixedNode {
	if len(b) < 4 {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(b))
	const entryBytes = 17
	if len(b) != 4+count*entryBytes || count == 0 {
		return nil
	}
	at := 0
	var walk func(depth int) *fixedNode
	walk = func(depth int) *fixedNode {
		if at >= count || depth > ir.TableFixedMaxDepth {
			return nil
		}
		e := b[4+at*entryBytes:]
		at++
		n := &fixedNode{
			id:   binary.LittleEndian.Uint64(e),
			kind: int(e[8]),
			size: int64(binary.LittleEndian.Uint32(e[9:])),
		}
		kids := int(binary.LittleEndian.Uint32(e[13:]))
		for range kids {
			c := walk(depth + 1)
			if c == nil {
				return nil
			}
			n.children = append(n.children, c)
		}
		return n
	}
	root := walk(0)
	if root == nil || at != count {
		return nil
	}
	return root
}

// decodeFixedLineage reads one record an OLDER writer of this table wrote,
// under the lock's layout for its hash. It answers a refusal's name, or "".
func decodeFixedLineage(m *tabletext.Model, inst *tabletext.Instance, hash uint64, layout, rest []byte, report *tabletext.Report) string {
	lock, err := unitLock(m.Unit)
	if err != nil || lock == nil {
		return FixedReasonLayoutUnknown
	}
	entries := lockfile.Lineage(lock, inst.Def.WireName())
	at := -1
	for i, e := range entries {
		if e.Wire == hash {
			at = i
		}
	}
	if at < 0 {
		return FixedReasonLayoutUnknown
	}
	if at < lockfile.Floor(lock, inst.Def.WireName()) {
		return FixedReasonLayoutUnsupported
	}
	known := entries[at]
	if string(layout) != string(known.Layout) {
		return FixedReasonLayoutMalformed
	}
	root := parseFixedLayout(known.Layout)
	if root == nil || root.kind != ir.TableKindTable {
		return FixedReasonLayoutMalformed
	}
	record := 8 + int(known.Record)
	if len(rest)%record != 0 || len(rest) == 0 {
		return FixedReasonTruncated
	}
	if len(rest) != record {
		return FixedReasonBatchTooLarge
	}
	if binary.LittleEndian.Uint64(rest) != hash {
		return FixedReasonLayoutMalformed
	}
	d := &lineageDecoder{m: m, report: report}
	return d.table(root, rest[8:], inst)
}

type lineageDecoder struct {
	m      *tabletext.Model
	report *tabletext.Report
}

// table lands a writer's body onto a reader's instance, field by field, by id.
func (d *lineageDecoder) table(w *fixedNode, b []byte, inst *tabletext.Instance) string {
	if int64(len(b)) < w.size {
		return FixedReasonTruncated
	}
	byID := map[uint64]int{}
	for i := range inst.Fields {
		byID[ir.TableFieldWireId(inst.Fields[i].Def)] = i
	}
	off := int64(0)
	for _, c := range w.children {
		i, named := byID[c.id]
		switch {
		case !named:
			d.report.Unknown++ // a field the reader no longer declares (§4)
		case inst.Fields[i].Def.Deprecated:
			// the reader yields its declared default out of a deprecated slot
		default:
			if reason := d.field(c, b[off:off+c.size], &inst.Fields[i]); reason != "" {
				return reason
			}
		}
		off += c.size
	}
	return ""
}

func (d *lineageDecoder) field(w *fixedNode, b []byte, fv *tabletext.Field) string {
	if w.kind == ir.TableKindOptional {
		if b[0] > 1 {
			return ReasonWireDamaged // a present byte other than 0 or 1 (§3.4)
		}
		present := b[0] != 0
		if !present {
			return "" // absent: the reader's declared default stands
		}
		fv.Present = true
		return d.payload(w.children[0], b[ir.TableFixedPresentBytes:], fv)
	}
	if fv.Def.Type.Optional {
		fv.Present = true
	}
	return d.payload(w, b, fv)
}

func (d *lineageDecoder) payload(w *fixedNode, b []byte, fv *tabletext.Field) string {
	f := fv.Def
	switch {
	case f.KeyEnum != "":
		if w.kind != ir.TableKindKeyed || len(w.children) != 2 {
			return FixedReasonKindMismatch
		}
		key, elem := w.children[0], w.children[1]
		for k, variant := range key.children {
			slot := int(enumValueForId(f.KeyEnumRef, variant.id)) - 1
			if slot < 0 || slot >= len(fv.Elems) {
				d.report.Unknown++ // a key the reader cannot name (§3.2)
				continue
			}
			if reason := d.element(elem, b[int64(k)*elem.size:], f, &fv.Elems[slot]); reason != "" {
				return reason
			}
		}
		return ""
	case f.Array != ir.ArrayNone:
		if w.kind != ir.TableKindArray || len(w.children) != 1 {
			return FixedReasonKindMismatch
		}
		elem := w.children[0]
		head := int64(0)
		if f.Array == ir.ArrayCounted {
			head = ir.TableFixedCountBytes
		}
		if elem.size <= 0 || w.size < head {
			return FixedReasonKindMismatch
		}
		theirs := (w.size - head) / elem.size
		n := theirs
		if f.Array == ir.ArrayCounted {
			count := int32(binary.LittleEndian.Uint32(b))
			if count < 0 || int64(count) > theirs || int64(count) > f.ArrayBound {
				return FixedReasonCountOverBound
			}
			n = int64(count)
			fv.Count = int(count)
		} else if n > f.ArrayBound {
			// a fixed array that SHRANK: more elements than the reader has
			// slots for refuses, as a count past the bound does (§4)
			return FixedReasonCountOverBound
		}
		for i := int64(0); i < n; i++ {
			if reason := d.element(elem, b[head+i*elem.size:], f, &fv.Elems[i]); reason != "" {
				return reason
			}
		}
		return ""
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		if f.Type.Kind == ir.TString && w.kind != ir.TableKindString {
			return FixedReasonKindMismatch
		}
		if f.Type.Kind == ir.TBytes && w.kind != ir.TableKindArray {
			return FixedReasonKindMismatch
		}
		used := int32(binary.LittleEndian.Uint32(b))
		if used < 0 || int64(used) > w.size-4 || int64(used) > f.Type.Size {
			return FixedReasonLengthOverBound
		}
		text := b[4 : 4+used]
		if f.Type.Kind == ir.TString && (!textValid(text) || containsZero(text)) {
			return FixedReasonTextIllFormed
		}
		fv.Cell.Str = append([]byte(nil), text...)
		fv.Count = int(used)
		return ""
	}
	return d.element(w, b, f, &fv.Cell)
}

func containsZero(b []byte) bool {
	return slices.Contains(b, 0)
}

func (d *lineageDecoder) element(w *fixedNode, b []byte, f *ir.Field, cell *tabletext.Cell) string {
	if st := tabletext.StructOf(f); st != nil {
		if w.kind != ir.TableKindTable {
			return FixedReasonKindMismatch
		}
		sub := d.m.New(st)
		cell.Tab = sub
		return d.table(w, b, sub)
	}
	if un := tabletext.UnionOf(f); un != nil {
		return d.union(w, b, un, cell)
	}
	if en := tabletext.EnumOf(f); en != nil {
		if w.kind != ir.TableKindEnum {
			return FixedReasonKindMismatch
		}
		raw := getFixedUint(b, w.size)
		switch {
		case raw == 0:
			cell.U = 0
		case raw > uint64(len(w.children)):
			return FixedReasonOutOfRange // a forged ordinal (docs/FIXED-FORM-ALGORITHM.md §5.4)
		default:
			v := enumValueForId(en, w.children[raw-1].id)
			switch {
			case v < 0:
				cell.U = 0 // a variant the reader cannot name reads None (§4)
				d.report.Unknown++
			case tabletext.VariantDeprecated(en, v):
				*cell = d.m.FieldDefaultCell(f)
			default:
				cell.U = uint64(v)
			}
		}
		return ""
	}
	if tabletext.FlagsOf(f) != nil {
		if w.size != 8 || w.kind == ir.TableKindTable || len(w.children) != 0 {
			return FixedReasonKindMismatch
		}
		cell.U = binary.LittleEndian.Uint64(b)
		return ""
	}
	if f.Type.Kind == ir.TBool {
		if w.kind != ir.TableKindBool {
			return FixedReasonKindMismatch
		}
		if b[0] > 1 {
			return ReasonWireDamaged // a bool byte other than 0 or 1 (§3.4)
		}
		cell.B = b[0] != 0
		return ""
	}
	declared := ir.TableFixedLeafKind(f)
	if w.kind != declared && !ir.TableKindWidens(w.kind, declared) {
		return FixedReasonKindMismatch
	}
	// the writer's value at the writer's KIND width, then the variable read's
	// own scalar rule: the finite test, the widening, and the reader's range,
	// which on this read REFUSES rather than clamps
	width := int64(ir.TableKindWidth(w.kind))
	if width == 0 || width > w.size {
		return FixedReasonKindMismatch
	}
	tmp := make([]byte, width)
	copy(tmp, b[:width])
	if w.size > width && !ir.TableKindWide(w.kind) {
		// a storage wider than its kind (bits(N) in four bytes): the low bytes
		// are the value, and any bit above the kind's width is out of range
		raw := getFixedUint(b, w.size)
		if width < 8 && raw>>(8*uint(width)) != 0 {
			return FixedReasonOutOfRange
		}
	}
	var scratch tabletext.Report
	r := &wireReader{buf: tmp, report: &scratch, m: d.m, fail: &readFail{}}
	if !r.scalarAt(cell, f, w.kind) {
		return RefusalReason(r.err())
	}
	if w.kind != declared {
		d.report.Widened++
	}
	return ""
}

func (d *lineageDecoder) union(w *fixedNode, b []byte, un *ir.Union, cell *tabletext.Cell) string {
	if w.kind != ir.TableKindUnion {
		return FixedReasonKindMismatch
	}
	widest := int64(0)
	for _, c := range w.children {
		if c.kind != ir.TableKindNoPayload && c.size > widest {
			widest = c.size
		}
	}
	tagBytes := w.size - widest
	if tagBytes < 1 || tagBytes > 8 {
		return FixedReasonKindMismatch
	}
	*cell = tabletext.Cell{}
	tag := getFixedUint(b, tagBytes)
	if tag == 0 {
		return ""
	}
	if tag > uint64(len(w.children)) {
		return FixedReasonOutOfRange // a forged tag (docs/FIXED-FORM-ALGORITHM.md §5.4)
	}
	arm := w.children[tag-1]
	for i, v := range un.Variants {
		if ir.TableWireId(v.WireName()) != arm.id {
			continue
		}
		cell.U = uint64(i + 1)
		switch {
		case v.Void():
			if arm.kind != ir.TableKindNoPayload {
				return FixedReasonKindMismatch
			}
			return ""
		case v.Body():
			sub := d.m.New(v.Ref)
			cell.Tab = sub
			if arm.kind != ir.TableKindTable {
				return FixedReasonKindMismatch
			}
			return d.table(arm, b[tagBytes:], sub)
		}
		fv := d.m.NewArm(v)
		cell.Arm = fv
		return d.payload(arm, b[tagBytes:], fv)
	}
	d.report.Unknown++ // an arm the reader cannot name reads None (§4)
	return ""
}
