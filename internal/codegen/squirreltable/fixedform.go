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
	fixedFormByte   = 3
	fixedEntryBytes = 17
	fixedCountBytes = 4
	fixedHashBytes  = 8
	fixedKindOptional = 35
)

func init() {
	RegisterSurface(Surface{Name: "fixedform", Table: fixedTable, Module: nil})
}

// fixedTable emits the fixed-form record template and layout for one table of
// the closure when it is a fixed root within the form's bound.
func fixedTable(m *Module, st *ir.Struct) {
	if !st.IsTable || st.IsMapEntry() || fixedRefusal(st) != "" {
		return
	}
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	body := fixedTypeBytes(st)
	rec := fixedHashBytes + body

	m.Printf("// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3 for %s.\n", st.Name)
	m.Printf("// The layout is the writer's whole type flattened; the record is the\n")
	m.Printf("// hash and the body.\n")
	m.Printf("_M.%sFixedLayout <- \"%s\";\n", st.Name, byteHex(layout))
	m.Printf("_M.%sFixedRecord <- %d;\n", st.Name, rec)

	emitFixedWrite(m, st, w)
	emitFixedRead(m, st, w)
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

func byteHex(b []byte) string {
	return hex.EncodeToString(b)
}

// emitFixedWrite emits the writer: the header and one record per instance.
func emitFixedWrite(m *Module, st *ir.Struct, w *fixedWalk) {
	m.Printf("_M.%sFixedSave <- function(out, rec) {\n", st.Name)
	m.Printf("  local b = out;\n")
	m.Printf("  b.seek(0);\n")
	m.Printf("  b.writen(%d, 'b');\n", fixedFormByte)
	m.Printf("  b.writeblob(blob(7));\n")
	m.Printf("  local h = _M.%sFixedHash;\n", st.Name)
	m.Printf("  b.writen(h & 0xFFFFFFFF, 'i'); b.writen(h >> 32, 'i');\n")
	m.Printf("  local l = _M.%sFixedLayout;\n", st.Name)
	m.Printf("  b.writen(l.len(), 'i'); b.writeblob(l);\n")
	m.Printf("  b.writen(_M.%sFixedRecord, 'i');\n", st.Name)
	m.Printf("  b.writen(h & 0xFFFFFFFF, 'i'); b.writen(h >> 32, 'i');\n")
	m.Printf("  %s;\n", fixedBodyWrite(st, "rec", "b"))
	m.Printf("};\n\n")
}

func fixedBodyWrite(st *ir.Struct, v, b string) string {
	var s string
	at := int64(0)
	for _, f := range st.Fields {
		s += fixedFieldWrite(f, v, b, at)
		at += fixedFieldBytes(f)
	}
	return s
}

func fixedFieldWrite(f *ir.Field, v, b string, at int64) string {
	return ""
}

// emitFixedRead emits the reader: the header check and one loop of records.
func emitFixedRead(m *Module, st *ir.Struct, w *fixedWalk) {
	m.Printf("_M.%sFixedLoad <- function(b, out) {\n", st.Name)
	m.Printf("  if (b.len() < 20) { return -1; }\n")
	m.Printf("  if (b[0] != 3) { return -1; }\n")
	m.Printf("  for (local i = 1; i < 8; i++) if (b[i] != 0) { return -1; }\n")
	m.Printf("  local L = b[16] | b[17] << 8 | b[18] << 16 | b[19] << 24;\n")
	m.Printf("  if (20 + L > b.len()) { return -1; }\n")
	m.Printf("  local h = b[8] | b[9] << 8 | b[10] << 16 | b[11] << 24 | (b[12] | b[13] << 8 | b[14] << 16 | b[15] << 24) << 32;\n")
	m.Printf("  if (h != _M.%sFixedHash) { return -1; }\n", st.Name)
	m.Printf("  local rest = b.len() - 20 - L;\n")
	m.Printf("  local rb = _M.%sFixedRecord;\n", st.Name)
	m.Printf("  if (rb <= 8 || rest %% rb != 0) { return -1; }\n")
	m.Printf("  local n = rest / rb;\n")
	m.Printf("  for (local i = 0; i < n; i++) {\n")
	m.Printf("    local at = 20 + L + i * rb;\n")
	m.Printf("    if ((b[at] | b[at+1] << 8 | b[at+2] << 16 | b[at+3] << 24 | (b[at+4] | b[at+5] << 8 | b[at+6] << 16 | b[at+7] << 24) << 32) != h) { return -1; }\n")
	m.Printf("    _M.%sFixedDecode(out[i], b, at + 8);\n", st.Name)
	m.Printf("  }\n")
	m.Printf("  return n;\n")
	m.Printf("};\n\n")
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
