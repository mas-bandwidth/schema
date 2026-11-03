// THE FIXED FORM AT A FIXED ROOT (docs/SPEC-TABLES.md "The three wires", §3.4):
// the engine's reader and writer of form 3 for one record of a declared fixed
// table, over the same instance model the variable form uses.
//
// A ROOT THAT CARRIES FORM 3 IS WRITTEN AS FORM 3 AND READ AS FORM 3, and
// nothing else. Glenn, 2026-11-03: "If we expect a fixed table, but it is not,
// refuse." So Encode writes these bytes for every such root, and Decode refuses
// a form-1 file there as previous_form with nothing decoded and no counter
// moved (docs/rulings.json fixed-root-refuses-form1). Which roots carry the
// form is [ir.TableFixedEmitted]'s answer, the one place it is computed; a
// declared fixed table past one of §3.4's bounds carries no form 3 and keeps
// the variable form's bytes.
//
// The bytes are the C++ reference's <T>FixedSave of one record
// (internal/codegen/cpptable/fixedform.go): the sixteen-byte header (form byte,
// seven reserved zeros, the layout hash at 8), the layout behind its u32
// length, then each record as its own eight hash bytes and the body at the
// constant offsets §3.4's table gives every field. The read is the identity
// plan's: the header's hash is this build's own, the layout is this build's
// own byte for byte, and every leaf lands at its own offset.
package tablewire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableFixedForm is form 3's byte (docs/SPEC-TABLES.md §3, THE FIRST BYTE).
const tableFixedForm = 3

// tableFixedHeaderBytes and tableFixedHashAt are the header §3 gives all five
// forms: the form byte, seven reserved zeros, the layout hash at 8, the body at 16.
const (
	tableFixedHeaderBytes = 16
	tableFixedHashAt      = 8
)

// FixedRefusal is a read of a fixed root that decoded nothing, by its §6.5
// name: previous_form for a form-1 file, layout_unknown for a hash this build
// does not hold, and the other names the fixed reader owes
// (docs/SPEC-TABLES.md §3.4, §21.3). Like [FormRefusal] it moves none of the
// report's counters and is returned rather than folded into the report.
type FixedRefusal struct{ Reason string }

func (e *FixedRefusal) Error() string {
	return fmt.Sprintf("the fixed-table read was refused: %s (docs/SPEC-TABLES.md §3.4)", e.Reason)
}

// The fixed reader's refusal names this engine answers, spelled as §6.5's
// reason table spells them.
const (
	FixedReasonPreviousForm      = "previous_form"
	FixedReasonLayoutUnknown     = "layout_unknown"
	FixedReasonLayoutMalformed   = "layout_malformed"
	FixedReasonReservedNotZero   = "reserved_not_zero"
	FixedReasonTruncated         = "truncated"
	FixedReasonBatchTooLarge     = "batch_too_large"
	FixedReasonCountOverBound    = "count_over_bound"
	FixedReasonLengthOverBound   = "length_over_bound"
	FixedReasonTextIllFormed     = "text_ill_formed"
	FixedReasonIntegrityMismatch = "integrity_mismatch"
)

// FixedRoot reports whether a root is read and written as form 3: a declared
// fixed table that carries the form ([ir.TableFixedEmitted]).
func FixedRoot(m *tabletext.Model, st *ir.Struct) bool {
	return st != nil && st.FixedDeclared && ir.TableFixedEmitted(m.Unit, st)
}

// fixedLayout is one root's layout bytes and hash, the two things the header
// and every record carry.
func fixedLayout(st *ir.Struct) ([]byte, uint64) {
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	return layout, ir.TableFixedLayoutHash(layout, st)
}

// EncodeFixed is one record of a fixed root as a form-3 file: the bytes the
// reference's <T>FixedSave writes for a batch of one.
func EncodeFixed(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	st := inst.Def
	layout, hash := fixedLayout(st)
	body := ir.TableFixedTypeBytes(st)
	out := make([]byte, tableFixedHeaderBytes+4+len(layout)+8+int(body))
	out[0] = tableFixedForm
	binary.LittleEndian.PutUint64(out[tableFixedHashAt:], hash)
	binary.LittleEndian.PutUint32(out[tableFixedHeaderBytes:], uint32(len(layout)))
	copy(out[tableFixedHeaderBytes+4:], layout)
	rec := out[tableFixedHeaderBytes+4+len(layout):]
	binary.LittleEndian.PutUint64(rec, hash)
	if err := encodeFixedBody(m, rec[8:], inst); err != nil {
		return nil, err
	}
	switch st.Integrity {
	case "":
	case "crc32c":
		out = binary.LittleEndian.AppendUint32(out, crc32.Checksum(out, fileCRC32C))
	default:
		return nil, fmt.Errorf("unsupported integrity %q", st.Integrity)
	}
	return out, nil
}

// encodeFixedBody writes one body at its constant offsets. A DEPRECATED
// FIELD'S SLOT CARRIES ITS DECLARED DEFAULT whatever the value holds (§3.4,
// DEPRECATED SLOT), so its bytes come from a freshly reset field.
func encodeFixedBody(m *tabletext.Model, b []byte, inst *tabletext.Instance) error {
	var fresh *tabletext.Instance
	off := int64(0)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		if f.Deprecated {
			if fresh == nil {
				fresh = m.New(inst.Def)
			}
			fv = &fresh.Fields[i]
		}
		if err := encodeFixedField(m, b[off:], fv); err != nil {
			return err
		}
		off += ir.TableFixedFieldBytes(f)
	}
	return nil
}

func encodeFixedField(m *tabletext.Model, b []byte, fv *tabletext.Field) error {
	if fv.Def.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS ZEROS (docs/FIXED-FORM-ALGORITHM.md
		// §3.1): the payload rides whole, and absent it is the template's zeros
		if !fv.Present {
			return nil
		}
		b[0] = 1
		return encodeFixedPayload(m, b[ir.TableFixedPresentBytes:], fv)
	}
	return encodeFixedPayload(m, b, fv)
}

func encodeFixedPayload(m *tabletext.Model, b []byte, fv *tabletext.Field) error {
	f := fv.Def
	elem := ir.TableFixedElementBytes(f)
	switch {
	case f.KeyEnum != "":
		for i := int64(0); i < f.KeyEnumRef.Max && int(i) < len(fv.Elems); i++ {
			if err := encodeFixedElement(m, b[i*elem:], f, &fv.Elems[i]); err != nil {
				return err
			}
		}
	case f.Array == ir.ArrayFixed:
		for i := int64(0); i < f.ArrayBound; i++ {
			if err := encodeFixedElement(m, b[i*elem:], f, &fv.Elems[i]); err != nil {
				return err
			}
		}
	case f.Array == ir.ArrayCounted:
		if fv.Count < 0 || int64(fv.Count) > f.ArrayBound {
			return fmt.Errorf("field %s: count %d is outside 0..%d, and save refuses it (docs/SPEC-TABLES.md §3.4)", f.Name, fv.Count, f.ArrayBound)
		}
		binary.LittleEndian.PutUint32(b, uint32(fv.Count))
		for i := 0; i < fv.Count; i++ {
			if err := encodeFixedElement(m, b[ir.TableFixedCountBytes+int64(i)*elem:], f, &fv.Elems[i]); err != nil {
				return err
			}
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		if int64(len(fv.Cell.Str)) > f.Type.Size {
			return fmt.Errorf("field %s: %d bytes past its capacity of %d, and save refuses it (docs/SPEC-TABLES.md §3.4)", f.Name, len(fv.Cell.Str), f.Type.Size)
		}
		binary.LittleEndian.PutUint32(b, uint32(len(fv.Cell.Str)))
		copy(b[4:], fv.Cell.Str)
	case f.Type.Kind == ir.TWString:
		if int64(len(fv.Cell.Units)) > f.Type.Size {
			return fmt.Errorf("field %s: %d units past its capacity of %d, and save refuses it (docs/SPEC-TABLES.md §3.4)", f.Name, len(fv.Cell.Units), f.Type.Size)
		}
		binary.LittleEndian.PutUint32(b, uint32(len(fv.Cell.Units)))
		for i, u := range fv.Cell.Units {
			binary.LittleEndian.PutUint16(b[4+2*i:], u)
		}
	default:
		return encodeFixedElement(m, b, f, &fv.Cell)
	}
	return nil
}

func encodeFixedElement(m *tabletext.Model, b []byte, f *ir.Field, cell *tabletext.Cell) error {
	if st := tabletext.StructOf(f); st != nil {
		return encodeFixedBody(m, b, subInstanceOf(m, f, cell))
	}
	if un := tabletext.UnionOf(f); un != nil {
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm, and save refuses it (docs/SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		tag := ir.TableFixedUnionTagBytes(un)
		putFixedUint(b, tag, cell.U)
		if cell.U == 0 {
			return nil
		}
		return encodeFixedArm(m, b[tag:], un.Variants[cell.U-1], cell)
	}
	if en := tabletext.EnumOf(f); en != nil {
		putFixedUint(b, int64(en.StorageBits/8), cell.U)
		return nil
	}
	if tabletext.FlagsOf(f) != nil {
		binary.LittleEndian.PutUint64(b, cell.U)
		return nil
	}
	switch f.Type.Kind {
	case ir.TBool:
		if cell.B {
			b[0] = 1
		}
		return nil
	case ir.TFloat32:
		if err := RefuseNonFinite(m, f, cell.F); err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(b, math.Float32bits(float32(cell.F)))
		return nil
	case ir.TFloat64:
		if err := RefuseNonFinite(m, f, cell.F); err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(b, math.Float64bits(cell.F))
		return nil
	}
	width := ir.TableFixedStorageBytes(f.Type)
	if kind := ir.TableScalarKind(f); ir.TableKindWide(kind) {
		copy(b, tabletext.WideBytes(tabletext.WideValue(cell), kind))
		return nil
	}
	putFixedUint(b, width, cell.U)
	return nil
}

// encodeFixedArm is one SET arm under its tag: a body arm's body, or the
// payload a field of the arm's type would carry. A payload-free arm writes
// nothing (§2.6).
func encodeFixedArm(m *tabletext.Model, b []byte, arm ir.UnionVariant, cell *tabletext.Cell) error {
	switch {
	case arm.Void():
		return nil
	case arm.Body():
		payload := cell.Tab
		if payload == nil {
			payload = m.New(arm.Ref)
		}
		return encodeFixedBody(m, b, payload)
	}
	fv := cell.Arm
	if fv == nil {
		fv = m.NewArm(arm)
	}
	return encodeFixedPayload(m, b, fv)
}

func putFixedUint(b []byte, width int64, v uint64) {
	for i := int64(0); i < width; i++ {
		b[i] = byte(v >> (8 * i))
	}
}

func getFixedUint(b []byte, width int64) uint64 {
	var v uint64
	for i := int64(0); i < width; i++ {
		v |= uint64(b[i]) << (8 * i)
	}
	return v
}

// decodeFixed reads a fixed root's file. THE FORM BYTE IS READ FIRST, so a file
// of another form is refused by name before anything about its length is
// asked (docs/FIXED-FORM-ALGORITHM.md §5.3): form 1 is previous_form, form 2 is
// message_form_as_file, and any other byte is newer_form.
func decodeFixed(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	st := inst.Def
	refuse := func(reason string) (bool, error) {
		report.Refused = true
		report.Reason = reason
		return false, &FixedRefusal{Reason: reason}
	}
	damage := func(reason string) (bool, error) {
		report.Malformed = true
		report.Reason = reason
		return false, nil
	}
	if len(data) < 1 {
		return damage(FixedReasonTruncated)
	}
	switch data[0] {
	case tableFixedForm:
	case ir.TableWireForm:
		return refuse(FixedReasonPreviousForm)
	case ir.TableWireMessageForm:
		report.Refused = true
		return false, &MessageRefusal{Reason: ReasonMessageFormAsFile}
	default:
		report.Refused = true
		return false, &FormRefusal{Form: data[0]}
	}
	data, valid := integrityBody(st, data, 5)
	if !valid {
		return damage(FixedReasonIntegrityMismatch)
	}
	if len(data) < tableFixedHeaderBytes+4 {
		return damage(FixedReasonTruncated)
	}
	for _, reserved := range data[1:tableFixedHashAt] {
		if reserved != 0 {
			return damage(FixedReasonReservedNotZero)
		}
	}
	layoutBytes := int(binary.LittleEndian.Uint32(data[tableFixedHeaderBytes:]))
	if layoutBytes > len(data)-tableFixedHeaderBytes-4 {
		return refuse(FixedReasonLayoutMalformed)
	}
	layout := data[tableFixedHeaderBytes+4 : tableFixedHeaderBytes+4+layoutBytes]
	mine, hash := fixedLayout(st)
	if binary.LittleEndian.Uint64(data[tableFixedHashAt:]) != hash {
		// OUTSIDE THIS BUILD'S OWN LAYOUT IS layout_unknown (§21.3): the
		// engine holds the identity plan and no lineage, so a file another
		// layout wrote is one it does not read
		return refuse(FixedReasonLayoutUnknown)
	}
	if !bytes.Equal(layout, mine) {
		return refuse(FixedReasonLayoutMalformed)
	}
	record := 8 + int(ir.TableFixedTypeBytes(st))
	rest := data[tableFixedHeaderBytes+4+layoutBytes:]
	if len(rest)%record != 0 {
		return damage(FixedReasonTruncated)
	}
	// A ROOT IS ONE RECORD: the engine reads a file into one instance, so a
	// batch of more than one is the caller's capacity exceeded, and a batch of
	// none holds no value to read
	if len(rest) != record {
		if len(rest) == 0 {
			return damage(FixedReasonTruncated)
		}
		return refuse(FixedReasonBatchTooLarge)
	}
	if binary.LittleEndian.Uint64(rest) != hash {
		return refuse(FixedReasonLayoutMalformed)
	}
	d := &fixedDecoder{m: m, report: report}
	if reason := d.body(rest[8:], inst); reason != "" {
		m.Clear(inst)
		return refuse(reason)
	}
	return true, nil
}

// fixedDecoder is the identity read of one record body.
type fixedDecoder struct {
	m      *tabletext.Model
	report *tabletext.Report
}

// body lands every field at its own offset, and answers a refusal's name when
// a count or a text's used length is past its bound or a text is ill-formed
// (docs/FIXED-FORM-ALGORITHM.md §4.5), the read's refusals that name a value.
func (d *fixedDecoder) body(b []byte, inst *tabletext.Instance) string {
	off := int64(0)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		n := ir.TableFixedFieldBytes(f)
		// THE READER YIELDS THE DECLARED DEFAULT OUT OF A DEPRECATED SLOT, and
		// nothing counts (§3.4, DEPRECATED SLOT): the field keeps its reset value
		if !f.Deprecated {
			if reason := d.field(b[off:off+n], fv); reason != "" {
				return reason
			}
		}
		off += n
	}
	return ""
}

func (d *fixedDecoder) field(b []byte, fv *tabletext.Field) string {
	if fv.Def.Type.Optional {
		fv.Present = b[0] != 0
		if !fv.Present {
			return ""
		}
		return d.payload(b[ir.TableFixedPresentBytes:], fv)
	}
	return d.payload(b, fv)
}

func (d *fixedDecoder) payload(b []byte, fv *tabletext.Field) string {
	f := fv.Def
	elem := ir.TableFixedElementBytes(f)
	switch {
	case f.KeyEnum != "":
		for i := int64(0); i < f.KeyEnumRef.Max && int(i) < len(fv.Elems); i++ {
			if reason := d.element(b[i*elem:], f, &fv.Elems[i], true); reason != "" {
				return reason
			}
		}
	case f.Array == ir.ArrayFixed:
		for i := int64(0); i < f.ArrayBound; i++ {
			if reason := d.element(b[i*elem:], f, &fv.Elems[i], true); reason != "" {
				return reason
			}
		}
	case f.Array == ir.ArrayCounted:
		count := int32(binary.LittleEndian.Uint32(b))
		if count < 0 || int64(count) > f.ArrayBound {
			return FixedReasonCountOverBound
		}
		fv.Count = int(count)
		for i := 0; i < fv.Count; i++ {
			if reason := d.element(b[ir.TableFixedCountBytes+int64(i)*elem:], f, &fv.Elems[i], true); reason != "" {
				return reason
			}
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		used := int32(binary.LittleEndian.Uint32(b))
		if used < 0 || int64(used) > f.Type.Size {
			return FixedReasonLengthOverBound
		}
		text := b[4 : 4+used]
		if f.Type.Kind == ir.TString && (!textValid(text) || bytes.IndexByte(text, 0) >= 0) {
			return FixedReasonTextIllFormed
		}
		fv.Cell.Str = append([]byte(nil), text...)
		fv.Count = int(used)
	case f.Type.Kind == ir.TWString:
		used := int32(binary.LittleEndian.Uint32(b))
		if used < 0 || int64(used) > f.Type.Size {
			return FixedReasonLengthOverBound
		}
		units := wideUnits(b[4 : 4+2*used])
		if !wideValid(units) {
			return FixedReasonTextIllFormed
		}
		fv.Cell.Units = units
		fv.Count = int(used)
	default:
		return d.element(b, f, &fv.Cell, false)
	}
	return ""
}

func (d *fixedDecoder) element(b []byte, f *ir.Field, cell *tabletext.Cell, isElement bool) string {
	if st := tabletext.StructOf(f); st != nil {
		sub := d.m.New(st)
		cell.Tab = sub
		return d.body(b, sub)
	}
	if un := tabletext.UnionOf(f); un != nil {
		tagBytes := ir.TableFixedUnionTagBytes(un)
		tag := getFixedUint(b, tagBytes)
		*cell = tabletext.Cell{}
		if tag == 0 {
			return ""
		}
		if tag > uint64(len(un.Variants)) {
			// A UNION TAG NAMING NO ARM LANDS AS None AND COUNTS UNKNOWN, which
			// is what form 1 does with an arm id it does not know (§4)
			d.report.Unknown++
			return ""
		}
		arm := un.Variants[tag-1]
		cell.U = tag
		switch {
		case arm.Void():
			return ""
		case arm.Body():
			sub := d.m.New(arm.Ref)
			cell.Tab = sub
			return d.body(b[tagBytes:], sub)
		}
		fv := d.m.NewArm(arm)
		cell.Arm = fv
		return d.payload(b[tagBytes:], fv)
	}
	if en := tabletext.EnumOf(f); en != nil {
		raw := getFixedUint(b, int64(en.StorageBits/8))
		switch {
		case raw > uint64(len(en.Variants)):
			// A FORGED ORDINAL LANDS None AND COUNTS CLAMPED, as the storage
			// clamp counts it (docs/FIXED-FORM-ALGORITHM.md §5.4)
			cell.U = 0
			d.report.Clamped++
		case raw != 0 && tabletext.VariantDeprecated(en, int64(raw)):
			if isElement {
				*cell = tabletext.Cell{}
			} else {
				*cell = d.m.FieldDefaultCell(f)
			}
		default:
			cell.U = raw
		}
		return ""
	}
	if tabletext.FlagsOf(f) != nil {
		cell.U = binary.LittleEndian.Uint64(b)
		return ""
	}
	if f.Type.Kind == ir.TBool {
		// the wire says nonzero is true (§3): a forged 2 is true, never damage
		cell.B = b[0] != 0
		return ""
	}
	// every other leaf takes the variable read's own scalar rule over its
	// storage bytes: the finite test before the value, then the declared range
	kind := ir.TableScalarKind(f)
	if f.Type.Kind == ir.TBits {
		kind = ir.TableKindU32
		if ir.TableFixedStorageBytes(f.Type) == 8 {
			kind = ir.TableKindU64
		}
	}
	r := &wireReader{buf: b, report: d.report, m: d.m}
	r.scalarAt(cell, f, kind)
	return ""
}
