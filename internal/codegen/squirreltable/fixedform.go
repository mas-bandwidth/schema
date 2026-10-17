// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in Squirrel: the
// LAYOUT and the record template of a fixed table.
//
// A fixed-table record is an eight-byte hash of the writer's vocabulary block
// and then the values in declared order, every field at its DECLARED STORAGE
// WIDTH, nothing padded between fields. The algorithm is docs/FIXED-FORM-ALGORITHM.md;
// the layout bytes here are the same run of bytes the C++ reference writes, so
// a writer that emits them byte for byte agrees with the oracle
// (build/fixedform-corpus). The reader path is runtime/fixed.nut, written from
// the writer's layout.
//
// This file joins the table module through [RegisterSurface], as the port's
// design names (docs/PORT-SQUIRREL.md, THE CARD MAP). Nothing here touches form
// 1: the Squirrel table surface is new, so there is no previous form to fall
// back to.
package squirreltable

import (
	"encoding/hex"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	fixedFormByte     = 3
	fixedEntryBytes   = 17
	fixedCountBytes   = 4
	fixedHashBytes    = 8
	fixedKindOptional = 35
)

func init() {
	RegisterSurface(Surface{Name: "fixedform", Table: fixedTable, Module: nil})
}

// fixedRefusal names a table the form is not emitted for, or "".
func fixedRefusal(st *ir.Struct) string {
	if !ir.TableFixedWithinDepth(st) {
		return "nested past the fixed form's depth bound (docs/FIXED-FORM-ALGORITHM.md §1.1)"
	}
	if !ir.TableFixedSupported(st, 0) {
		return "a construct the fixed form refuses (docs/FIXED-FORM-ALGORITHM.md §5.5)"
	}
	if body := fixedTypeBytes(st); body > ir.TableFixedRecordMaxBytes {
		return "record body past the fixed form's ceiling (docs/SPEC-TABLES.md §3.4)"
	}
	return ""
}

// fixedTable emits the fixed-form record template and layout for one table of
// the closure when it is a fixed root within the form's bound.
func fixedTable(m *Module, st *ir.Struct) {
	m.Runtime("fixed")
	if !st.IsTable || st.IsMapEntry() || fixedRefusal(st) != "" || !fixedSupported(st) {
		return
	}
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	body := fixedTypeBytes(st)
	rec := fixedHashBytes + body
	hash := ir.TableFixedLayoutHash(layout, st)

	m.Printf("// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3 for %s.\n", st.Name)
	m.Printf("// The layout is the writer's whole type flattened; the record is the\n")
	m.Printf("// hash and the body.\n")
	m.Printf("_M.%sFixedLayout <- \"%s\";\n", st.Name, byteHex(layout))
	m.Printf("_M.%sFixedHash <- %s;\n", st.Name, squirrel.HexLit(hash))
	m.Printf("_M.%sFixedRecord <- %d;\n", st.Name, rec)

	emitFixedWrite(m, st, w)
	emitFixedRead(m, st, w)
	emitFixedWriteBody(m, st)
	emitFixedDecodeBody(m, st)
}

// fixedSupported is the subset of the form this card's codec carries: no
// union, no enum-keyed array, no wide text (the Squirrel table surface refuses
// wstring anyway), and no 128-bit leaf. A table outside it is simply not a
// root of this card's codec; the layout walk still holds it for a later card.
func fixedSupported(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !fixedFieldSupported(f) {
			return false
		}
	}
	return true
}

func fixedFieldSupported(f *ir.Field) bool {
	if f.KeyEnum != "" {
		return false
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Union:
			return false
		case *ir.Struct:
			return fixedSupported(r)
		case *ir.Enum, *ir.Flags:
			return true
		}
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TInt, ir.TFixed, ir.TBits, ir.TFloat32, ir.TFloat64, ir.TString, ir.TBytes:
		return f.Type.Width <= 64
	case ir.TWString:
		return false
	}
	return false
}

func emitFixedWrite(m *Module, st *ir.Struct, w *fixedWalk) {
	m.Printf("_M.%sFixedSave <- function(out, recs) {\n", st.Name)
	m.Printf("  local b = out;\n")
	m.Printf("  b.seek(0);\n")
	m.Printf("  b.writen(%d, 'b');\n", fixedFormByte)
	m.Printf("  b.writeblob(blob(7));\n")
	m.Printf("  local h = _M.%sFixedHash;\n", st.Name)
	m.Printf("  b.writen(h & 0xFFFFFFFF, 'i'); b.writen((h >> 32) & 0xFFFFFFFF, 'i');\n")
	m.Printf("  local lb = _rt_fixed.blobOfHex(_M.%sFixedLayout);\n", st.Name)
	m.Printf("  b.writen(lb.len(), 'i'); b.writeblob(lb);\n")
	m.Printf("  for (local k = 0; k < recs.len(); k++) {\n")
	m.Printf("    b.writen(h & 0xFFFFFFFF, 'i'); b.writen((h >> 32) & 0xFFFFFFFF, 'i');\n")
	m.Printf("    local body = _M.%sFixedRecord - 8;\n", st.Name)
	m.Printf("    local start = b.tell();\n")
	m.Printf("    for (local z = 0; z < body; z++) b.writen(0, 'b');\n")
	m.Printf("    _M.%sFixedWriteBody(b, start, recs[k]);\n", st.Name)
	m.Printf("  }\n")
	m.Printf("  return b.tell();\n")
	m.Printf("};\n\n")
}

func emitFixedRead(m *Module, st *ir.Struct, w *fixedWalk) {
	m.Printf("_M.%sFixedLoad <- function(b, out, cap) {\n", st.Name)
	m.Printf("  _M.FixedReason <- null;\n")
	m.Printf("  if (b.len() < 20) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  if (b[0] == 1) { _M.FixedReason = \"previous_form\"; return -1; }\n")
	m.Printf("  if (b[0] != 3) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  for (local i = 1; i < 8; i++) if (b[i] != 0) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  local L = _rt_fixed.u32le(b, 16);\n")
	m.Printf("  if (L < 4 || 20 + L > b.len()) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  local n = _rt_fixed.u32le(b, 20);\n")
	m.Printf("  if (n != (L - 4) / 17 || 4 + n * 17 != L) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  local rb = 8 + _rt_fixed.u32le(b, 20 + 4 + 9);\n")
	m.Printf("  if (rb < 8) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  if (rb > 8 + %d) { _M.FixedReason = \"layout_record_too_large\"; return -1; }\n", ir.TableFixedRecordMaxBytes)
	m.Printf("  local rest = b.len() - 20 - L;\n")
	m.Printf("  if (rest %% rb != 0) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  local count = rest / rb;\n")
	m.Printf("  if (cap != null && count > cap) { _M.FixedReason = \"plan_too_large\"; return -1; }\n")
	m.Printf("  local h = _rt_fixed.u64le(b, 8);\n")
	m.Printf("  for (local k = 0; k < count; k++) {\n")
	m.Printf("    local at = 20 + L + k * rb;\n")
	m.Printf("    if (_rt_fixed.u64le(b, at) != h) { _M.FixedReason = \"no_layout\"; return -1; }\n")
	m.Printf("    _M.%sFixedDecodeBody(out[k], b, at + 8, at + rb);\n", st.Name)
	m.Printf("  }\n")
	m.Printf("  return count;\n")
	m.Printf("};\n\n")
}

// emitFixedWriteBody emits the straight line of stores for one type: the
// writer's declared order at the declared storage widths (§3.4).
func emitFixedWriteBody(m *Module, st *ir.Struct) {
	m.Printf("_M.%sFixedWriteBody <- function(b, at, v) {\n", st.Name)
	var cur int64
	for _, f := range st.Fields {
		fixedEmitWriteField(m, f, cur, "v."+squirrel.Member(f), "  ")
		cur += fixedFieldBytes(f)
	}
	m.Printf("};\n\n")
}

func fixedEmitWriteField(m *Module, f *ir.Field, at int64, expr, ind string) {
	if f.Type.Optional {
		m.Printf("%sb[at + %d] = %sPresent ? 1 : 0;\n", ind, at, expr)
		m.Printf("%sif (%sPresent) {\n", ind, expr)
		fixedEmitWritePayload(m, f, at+1, expr, ind+"  ")
		m.Printf("%s}\n", ind)
		return
	}
	fixedEmitWritePayload(m, f, at, expr, ind)
}

func fixedEmitWritePayload(m *Module, f *ir.Field, at int64, expr, ind string) {
	switch {
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		if f.Array == ir.ArrayCounted {
			m.Printf("%s_rt_fixed.puti32(b, at + %d, %sCount);\n", ind, at, expr)
		}
		base := at
		if f.Array == ir.ArrayCounted {
			base += fixedCountBytes
		}
		count := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = expr + "Count"
		}
		m.Printf("%sfor (local i = 0; i < %s; i++) {\n", ind, count)
		fixedEmitWriteElement(m, f, base, expr+"[i]", ind+"  ", "i * "+fmt.Sprint(fixedElementBytes(f)))
		m.Printf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		m.Printf("%s_rt_fixed.puti32(b, at + %d, %s.len());\n", ind, at, expr)
		m.Printf("%sfor (local i = 0; i < %s.len(); i++) b[at + %d + i] = %s[i] & 0xff;\n", ind, expr, at+4, expr)
	case f.Type.Kind == ir.TBytes:
		m.Printf("%s_rt_fixed.puti32(b, at + %d, %s.len());\n", ind, at, expr)
		m.Printf("%sfor (local i = 0; i < %s.len(); i++) b[at + %d + i] = %s[i] & 0xff;\n", ind, expr, at+4, expr)
	default:
		fixedEmitWriteElement(m, f, at, expr, ind, "0")
	}
}

func fixedEmitWriteElement(m *Module, f *ir.Field, at int64, expr, ind, dyn string) {
	off := fmt.Sprintf("%d", at)
	if dyn != "0" {
		off = fmt.Sprintf("%d + %s", at, dyn)
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			m.Printf("%s_M.%sFixedWriteBody(b, at + %s, %s);\n", ind, r.Name, off, expr)
			return
		}
	}
	m.Printf("%s_rt_fixed.put(b, at + %s, %d, %s);\n", ind, off, fixedStorageBytes(f.Type), expr)
}

// emitFixedDecodeBody emits the reader mirrored: every field is decoded only
// where the writer's record carries its bytes, so a record an older schema
// wrote leaves the appended tail at the constructor's declared default.
func emitFixedDecodeBody(m *Module, st *ir.Struct) {
	m.Printf("_M.%sFixedDecodeBody <- function(v, b, at, lim) {\n", st.Name)
	var cur int64
	for _, f := range st.Fields {
		fixedEmitDecodeField(m, f, cur, "v."+squirrel.Member(f), "  ")
		cur += fixedFieldBytes(f)
	}
	m.Printf("};\n\n")
}

func fixedEmitDecodeField(m *Module, f *ir.Field, at int64, expr, ind string) {
	if f.Type.Optional {
		m.Printf("%sif (at + %d <= lim) {\n", ind, at+1)
		m.Printf("%s  %sPresent = b[at + %d] != 0;\n", ind, expr, at)
		m.Printf("%s  if (%sPresent) {\n", ind, expr)
		fixedEmitDecodePayload(m, f, at+1, expr, ind+"    ")
		m.Printf("%s  }\n", ind)
		m.Printf("%s}\n", ind)
		return
	}
	fixedEmitDecodePayload(m, f, at, expr, ind)
}

func fixedEmitDecodePayload(m *Module, f *ir.Field, at int64, expr, ind string) {
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		size--
	}
	m.Printf("%sif (at + %d <= lim) {\n", ind, at+size)
	switch {
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		base := at
		m.Printf("%s  while (%s.len() < %d) %s.append(%s);\n", ind, expr, f.ArrayBound, expr, elemZero(f))
		if f.Array == ir.ArrayCounted {
			m.Printf("%s  local c = _rt_fixed.geti32(b, at + %d);\n", ind, at)
			m.Printf("%s  if (c < 0) c = 0; else if (c > %d) c = %d;\n", ind, f.ArrayBound, f.ArrayBound)
			m.Printf("%s  %sCount = c;\n", ind, expr)
			base += fixedCountBytes
			m.Printf("%s  for (local i = 0; i < c; i++) {\n", ind)
		} else {
			m.Printf("%s  for (local i = 0; i < %d; i++) {\n", ind, f.ArrayBound)
		}
		fixedEmitDecodeElement(m, f, base, expr+"[i]", ind+"    ", "i * "+fmt.Sprint(fixedElementBytes(f)))
		m.Printf("%s  }\n", ind)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		m.Printf("%s  local n = _rt_fixed.geti32(b, at + %d);\n", ind, at)
		m.Printf("%s  if (n < 0) n = 0; else if (n > %d) n = %d;\n", ind, f.Type.Size, f.Type.Size)
		if f.Type.Kind == ir.TString {
			m.Printf("%s  local s = \"\";\n", ind)
			m.Printf("%s  for (local i = 0; i < n; i++) s += b[at + %d + i].tochar();\n", ind, at+4)
			m.Printf("%s  %s = s;\n", ind, expr)
		} else {
			m.Printf("%s  %s = blob(n);\n", ind, expr)
			m.Printf("%s  for (local i = 0; i < n; i++) %s[i] = b[at + %d + i];\n", ind, expr, at+4)
		}
	default:
		fixedEmitDecodeElement(m, f, at, expr, ind+"  ", "0")
	}
	m.Printf("%s}\n", ind)
}

func fixedEmitDecodeElement(m *Module, f *ir.Field, at int64, expr, ind, dyn string) {
	off := fmt.Sprintf("%d", at)
	if dyn != "0" {
		off = fmt.Sprintf("%d + %s", at, dyn)
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			m.Printf("%s_M.%sFixedDecodeBody(%s, b, at + %s, lim);\n", ind, r.Name, expr, off)
			return
		}
	}
	m.Printf("%s%s = _rt_fixed.get(b, at + %s, %d);\n", ind, expr, off, fixedStorageBytes(f.Type))
}

func byteHex(b []byte) string {
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// THE LAYOUT (mirrors internal/codegen/jstable/fixedform.go)
// ---------------------------------------------------------------------------

type fixedLayoutEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	note     string
}

type fixedDst struct {
	dst, stride, aux int64
	counted          int
	arg              int
}

type fixedWalk struct {
	entries []fixedLayoutEntry
	dst     []fixedDst
}

func (w *fixedWalk) push(e fixedLayoutEntry, d fixedDst) {
	w.entries = append(w.entries, e)
	w.dst = append(w.dst, d)
}

func fixedStorageBytes(t ir.FieldType) int64 {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TInt, ir.TFixed:
		return int64(t.Width / 8)
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return int64(r.StorageBits / 8)
		case *ir.Flags:
			return 8
		}
	}
	return 0
}

func fixedTypeBytes(st *ir.Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += fixedFieldBytes(f)
	}
	return n
}

func fixedElementBytes(f *ir.Field) int64 {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedTypeBytes(r)
		case *ir.Union:
			widest := int64(0)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if n := fixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(ir.StorageBitsFor(r.Max)/8) + widest
		}
	}
	return fixedStorageBytes(f.Type)
}

func fixedFieldBytes(f *ir.Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * fixedElementBytes(f)
	case f.Array == ir.ArrayFixed:
		payload = f.ArrayBound * fixedElementBytes(f)
	case f.Array == ir.ArrayCounted:
		payload = fixedCountBytes + f.ArrayBound*fixedElementBytes(f)
	case f.Type.Kind == ir.TString:
		payload = fixedCountBytes + f.Type.Size
	case f.Type.Kind == ir.TWString:
		payload = fixedCountBytes + 2*f.Type.Size
	case f.Type.Kind == ir.TBytes:
		payload = fixedCountBytes + f.Type.Size
	default:
		payload = fixedElementBytes(f)
	}
	if f.Type.Optional {
		return 1 + payload
	}
	return payload
}

func fixedWalkRoot(st *ir.Struct) *fixedWalk {
	w := &fixedWalk{}
	w.push(fixedLayoutEntry{
		id:       ir.TableWireId(st.WireName()),
		kind:     ir.TableKindTable,
		size:     fixedTypeBytes(st),
		children: len(st.Fields),
		note:     st.Name,
	}, fixedDst{})
	var at int64
	for _, f := range st.Fields {
		fixedWalkField(w, f, at)
		at += fixedFieldBytes(f)
	}
	return w
}

func fixedWalkField(w *fixedWalk, f *ir.Field, at int64) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		w.push(fixedLayoutEntry{id: id, kind: fixedKindOptional, size: size, children: 1, note: f.Name + " ?"},
			fixedDst{aux: at})
		fixedWalkPayload(w, f, id, size-1, at+1)
		return
	}
	fixedWalkPayload(w, f, id, size, at)
}

func fixedWalkPayload(w *fixedWalk, f *ir.Field, id uint64, size, at int64) {
	switch {
	case f.KeyEnum != "":
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name},
			fixedDst{dst: at, stride: fixedElementBytes(f)})
		fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum, 0)
		fixedWalkElement(w, f, 0, "element", 0)
	case f.Array != ir.ArrayNone:
		counted, aux, base := 0, int64(0), at
		if f.Array == ir.ArrayCounted {
			counted, aux, base = 1, at, at+fixedCountBytes
		}
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			fixedDst{dst: base, stride: fixedElementBytes(f), aux: aux, counted: counted})
		fixedWalkElement(w, f, 0, "element", 0)
	case f.Type.Kind == ir.TBytes:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			fixedDst{dst: at + fixedCountBytes, stride: 1, aux: at, counted: 1, arg: 3})
		w.push(fixedLayoutEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"}, fixedDst{})
	case f.Type.Kind == ir.TString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name},
			fixedDst{dst: at, aux: at + fixedCountBytes, arg: 1})
	case f.Type.Kind == ir.TWString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name},
			fixedDst{dst: at, aux: at + fixedCountBytes, arg: 2})
	default:
		fixedWalkElement(w, f, id, f.Name, at)
	}
}

func fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note string, at int64) {
	size := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note},
				fixedDst{dst: at})
			var sub int64
			for _, sf := range r.Fields {
				fixedWalkField(w, sf, sub)
				sub += fixedFieldBytes(sf)
			}
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note},
				fixedDst{dst: at, aux: at})
			for _, v := range r.Variants {
				if v.F == nil {
					w.push(fixedLayoutEntry{id: ir.TableWireId(v.WireName()), kind: ir.TableKindNoPayload, size: 0, children: 0, note: v.Name},
						fixedDst{})
					continue
				}
				fixedWalkElement(w, v.F, ir.TableWireId(v.WireName()), v.Name, tag)
			}
			return
		case *ir.Enum:
			fixedWalkEnum(w, r, id, note, at)
			return
		}
	}
	w.push(fixedLayoutEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note},
		fixedDst{dst: at})
}

func fixedWalkEnum(w *fixedWalk, e *ir.Enum, id uint64, note string, at int64) {
	w.push(fixedLayoutEntry{id: id, kind: ir.TableKindEnum, size: int64(e.StorageBits / 8), children: len(e.Variants), note: note},
		fixedDst{dst: at})
	for i := range e.Variants {
		w.push(fixedLayoutEntry{id: ir.TableWireId(e.VariantWireName(i)), kind: ir.TableKindNoPayload, size: 0, children: 0, note: e.Variants[i]},
			fixedDst{})
	}
}

func fixedLayoutBytes(entries []fixedLayoutEntry) []byte {
	out := make([]byte, 0, 4+fixedEntryBytes*len(entries))
	out = appendFixedU32(out, uint32(len(entries)))
	for _, e := range entries {
		out = appendFixedU64(out, e.id)
		out = append(out, byte(e.kind))
		out = appendFixedU32(out, uint32(e.size))
		out = appendFixedU32(out, uint32(e.children))
	}
	return out
}

func appendFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendFixedU64(b []byte, v uint64) []byte {
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

var _ = fmt.Sprintf
var _ = squirrel.StringLit
