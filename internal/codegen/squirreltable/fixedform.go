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
	if st.IsMapEntry() || fixedRefusal(st) != "" || !fixedSupported(st) {
		return
	}
	if !st.IsTable {
		// A NON-TABLE TYPE REACHED FROM A FIXED ROOT still needs its body
		// helpers: the root's FixedWriteBody/FixedDecodeBody call them by name.
		emitFixedWriteBody(m, st)
		emitFixedDecodeBody(m, st)
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

	lineage := lineageOf(m.Unit)
	strict := lineage != nil
	if strict {
		entries, floor := generateLineage(lineage[st.Name], FixedLineageEntry{
			Wire: hash, Layout: layout, Record: rec,
		})
		emitFixedPlans(m, st, entries, hash)
		emitFixedLineage(m, st, entries, floor)
	}

	emitFixedWrite(m, st, w)
	emitFixedDeprecatedCheck(m, st)
	emitFixedRead(m, st, w, strict)
	emitFixedWriteBody(m, st)
	emitFixedDecodeBody(m, st)
}

// emitFixedPlans lays down ONE decode plan per lineage entry, selected by hash
// at load and never compiled from a file (docs/FIXED-FORM-ALGORITHM.md §5.2,
// §5.3 step 8). The reader's own entry takes the identity plan; an older entry
// moves its fields through the rows [fixedPlanFor] builds.
func emitFixedPlans(m *Module, st *ir.Struct, entries []FixedLineageEntry, own uint64) {
	for i, e := range entries {
		if e.Retired {
			continue
		}
		m.Printf("_M.%sFixedPlan%d <- function(v, b, at, lim) {\n", st.Name, i)
		if e.Wire == own {
			m.Printf("  return _M.%sFixedDecodeBody(v, b, at, lim);\n", st.Name)
			m.Printf("};\n\n")
			continue
		}
		for _, p := range fixedPlanFor(st, e.Layout) {
			expr := "v." + squirrel.Member(p.field)
			if p.exact {
				fixedEmitDecodeField(m, p.field, p.srcAt, expr, "  ")
				continue
			}
			m.Printf("  if (at + %d + %d <= lim) %s = _rt_fixed.get(b, at + %d, %d);\n",
				p.srcAt, p.srcSize, expr, p.srcAt, p.srcSize)
		}
		m.Printf("};\n\n")
	}
}

// emitFixedLineage is §5.2 COMPILE's static data: the hashes, the layout bytes,
// the whole record sizes, the floor and the plans, oldest first with this
// build's own entry last (docs/SPEC-TABLES.md §21, §2.10).
func emitFixedLineage(m *Module, st *ir.Struct, entries []FixedLineageEntry, floor int) {
	m.Printf("_M.%sFixedFloor <- %d;\n", st.Name, floor)
	m.Printf("_M.%sFixedLineage <- [\n", st.Name)
	for i, e := range entries {
		decode := "null"
		if !e.Retired {
			decode = fmt.Sprintf("_M.%sFixedPlan%d", st.Name, i)
		}
		m.Printf("  { hash = %s, layout = _rt_fixed.blobOfHex(\"%s\"), record = %d, decode = %s },\n",
			squirrel.HexLit(e.Wire), byteHex(e.Layout), e.Record, decode)
	}
	m.Printf("];\n\n")
}

// fixedSupported is the subset of the form this card's codec carries. A
// table outside it is simply not a root of this card's codec; the layout walk
// still holds it for a later card.
func fixedSupported(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !fixedFieldSupported(f) {
			return false
		}
	}
	return true
}

func fixedFieldSupported(f *ir.Field) bool {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Union:
			return true
		case *ir.Struct:
			return fixedSupported(r)
		case *ir.Enum, *ir.Flags:
			return true
		}
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TInt, ir.TFixed, ir.TBits, ir.TFloat32, ir.TFloat64, ir.TString, ir.TBytes:
		return f.Type.Width <= 128
	case ir.TWString:
		// WIDE TEXT (docs/SPEC-TABLES.md §3.4, §7.2): an int32 used length in
		// CODE UNITS, then N two-byte units little-endian
		return true
	}
	return false
}

// emitFixedCRC32C emits CRC-32C, the Castagnoli polynomial
// (docs/SPEC-TABLES.md §2.14): initial value and final XOR 0xFFFFFFFF, the
// reflected polynomial 0x82F63B78. It is a local of the declaring function,
// which computes the trailer over every byte before it.
func emitFixedCRC32C(m *Module) {
	m.Printf("  local crc32c = function(bb, n) {\n")
	m.Printf("    local c = 0xFFFFFFFF;\n")
	m.Printf("    for (local i = 0; i < n; i++) {\n")
	m.Printf("      c = (c ^ bb[i]) & 0xFFFFFFFF;\n")
	m.Printf("      for (local k = 0; k < 8; k++) {\n")
	m.Printf("        c = (c & 1) ? ((c >> 1) ^ 0x82F63B78) & 0xFFFFFFFF : (c >> 1) & 0xFFFFFFFF;\n")
	m.Printf("      }\n")
	m.Printf("    }\n")
	m.Printf("    return c ^ 0xFFFFFFFF;\n")
	m.Printf("  };\n")
}

// deprecatedVariants is an enum's deprecated variants and their ordinals, in
// declaration order (docs/SPEC-TABLES.md §2.15).
func deprecatedVariants(e *ir.Enum) (names []string, ordinals []int64) {
	for i, v := range e.Variants {
		if i < len(e.VariantDeprecated) && e.VariantDeprecated[i] {
			names = append(names, v)
			ordinals = append(ordinals, int64(i+1))
		}
	}
	return names, ordinals
}

// emitFixedDeprecatedCheck writes the pre-write walk that refuses a record
// holding a deprecated variant: a fixed RECORD is the caller's value too, so
// the rule is §2.15's WRITE rule, and the reason names the table and field.
func emitFixedDeprecatedCheck(m *Module, st *ir.Struct) {
	m.Printf("_M.%sFixedDeprecated <- function(v) {\n", st.Name)
	for _, f := range st.Fields {
		fixedEmitDeprecatedField(m, f, "v."+squirrel.Member(f), st.Name+"."+f.Name, "  ")
	}
	m.Printf("  return null;\n};\n\n")
}

func fixedEmitDeprecatedField(m *Module, f *ir.Field, expr, path, ind string) {
	if f.Type.Optional {
		m.Printf("%sif (%sPresent) {\n", ind, expr)
		fixedEmitDeprecatedValue(m, f, expr, path, ind+"  ")
		m.Printf("%s}\n", ind)
		return
	}
	fixedEmitDeprecatedValue(m, f, expr, path, ind)
}

func fixedEmitDeprecatedValue(m *Module, f *ir.Field, expr, path, ind string) {
	switch {
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		// one loop variable per nesting depth: an array of records whose
		// fields are arrays walks loops inside loops, and a shared `i` would
		// index the outer array with the inner count
		iv := fmt.Sprintf("i%d", len(ind)/2)
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			names, _ := deprecatedVariants(e)
			for i := range names {
				m.Printf("%sfor (local %s = 0; %s < %s.len(); %s++) if (%s[%s] == %s) return %q;\n",
					ind, iv, iv, expr, iv, expr, iv, squirrel.StringLit([]byte(names[i])),
					"invalid_value: "+path+" names the deprecated variant "+names[i])
			}
			return
		}
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			m.Printf("%sfor (local %s = 0; %s < %s.len(); %s++) {\n", ind, iv, iv, expr, iv)
			for _, sub := range r.Fields {
				fixedEmitDeprecatedField(m, sub, expr+"["+iv+"]."+squirrel.Member(sub), path+"."+sub.Name, ind+"  ")
			}
			m.Printf("%s}\n", ind)
		}
	case f.Type.Kind == ir.TNamed:
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			names, _ := deprecatedVariants(e)
			for i := range names {
				m.Printf("%sif (%s == %s) return %q;\n", ind, expr,
					squirrel.StringLit([]byte(names[i])),
					"invalid_value: "+path+" names the deprecated variant "+names[i])
			}
			return
		}
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			for _, sub := range r.Fields {
				fixedEmitDeprecatedField(m, sub, expr+"."+squirrel.Member(sub), path+"."+sub.Name, ind)
			}
		}
	}
}

func emitFixedWrite(m *Module, st *ir.Struct, w *fixedWalk) {
	m.Printf("_M.%sFixedSave <- function(out, recs) {\n", st.Name)
	m.Printf("  _M.FixedReason <- null;\n")
	m.Printf("  _M.FixedField <- null;\n")
	m.Printf("  local b = out;\n")
	m.Printf("  b.seek(0);\n")
	m.Printf("  b.writen(%d, 'b');\n", fixedFormByte)
	m.Printf("  b.writeblob(blob(7));\n")
	m.Printf("  local h = _M.%sFixedHash;\n", st.Name)
	m.Printf("  b.writen(h & 0xFFFFFFFF, 'i'); b.writen((h >> 32) & 0xFFFFFFFF, 'i');\n")
	m.Printf("  local lb = _rt_fixed.blobOfHex(_M.%sFixedLayout);\n", st.Name)
	m.Printf("  b.writen(lb.len(), 'i'); b.writeblob(lb);\n")
	// one zeroed body, written whole per record: the record template the
	// body writer fills, with no per-byte loop
	m.Printf("  local zero = blob(_M.%sFixedRecord - 8);\n", st.Name)
	m.Printf("  for (local k = 0; k < recs.len(); k++) {\n")
	m.Printf("    local why = _M.%sFixedDeprecated(recs[k]);\n", st.Name)
	m.Printf("    if (why != null) { _M.FixedReason = why; return -1; }\n")
	m.Printf("    b.writen(h & 0xFFFFFFFF, 'i'); b.writen((h >> 32) & 0xFFFFFFFF, 'i');\n")
	m.Printf("    local start = b.tell();\n")
	m.Printf("    b.writeblob(zero);\n")
	m.Printf("    why = _M.%sFixedWriteBody(b, start, recs[k]);\n", st.Name)
	m.Printf("    if (why != null) { _M.FixedReason = why; return -1; }\n")
	m.Printf("  }\n")
	if st.Integrity != "" {
		// THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING):
		// four bytes, little-endian, the CRC-32C of every byte before them,
		// after the last record, so the file ends with it. The CRC-32C is
		// emitted with the declaring function, so a keyless unit carries none.
		emitFixedCRC32C(m)
		m.Printf("  local n = b.tell();\n")
		m.Printf("  _rt_fixed.puti32(b, n, crc32c(b, n));\n")
		m.Printf("  return n + 4;\n")
	} else {
		m.Printf("  return b.tell();\n")
	}
	m.Printf("};\n\n")
}

func emitFixedRead(m *Module, st *ir.Struct, w *fixedWalk, strict bool) {
	m.Printf("_M.%sFixedLoadTolerant <- function(b, out, cap) {\n", st.Name)
	m.Printf("  _M.FixedReason <- null;\n")
	m.Printf("  _M.FixedField <- null;\n")
	// EVERY REFUSAL OF THE LOAD HAS A NAME, THE FRAMING-DAMAGE RETURNS INCLUDED
	// (docs/FIXED-FORM-ALGORITHM.md §5.3, docs/SPEC-TABLES.md §3.4): the
	// reasons answer the Go fixed reader's in its order — the form byte and the
	// file's length each get their own name (§5.3), then the integrity trailer,
	// then the layout.
	m.Printf("  if (b.len() < 1) { _M.FixedReason = \"truncated\"; return -1; }\n")
	m.Printf("  if (b[0] == 1) { _M.FixedReason = \"previous_form\"; return -1; }\n")
	m.Printf("  if (b[0] == 2) { _M.FixedReason = \"message_form_as_file\"; return -1; }\n")
	m.Printf("  if (b[0] != 3) { _M.FixedReason = \"newer_form\"; return -1; }\n")
	if st.Integrity != "" {
		// THE INTEGRITY TRAILER (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING):
		// checked after the form byte and BEFORE the layout or any record; a
		// mismatch is whole-file damage named integrity_mismatch and the rest
		// is read without the trailer.
		emitFixedCRC32C(m)
		m.Printf("  if (b.len() < 5 || _rt_fixed.u32le(b, b.len() - 4) != crc32c(b, b.len() - 4)) {\n")
		m.Printf("    _M.FixedReason = \"integrity_mismatch\"; return -1;\n")
		m.Printf("  }\n")
		m.Printf("  local len = b.len() - 4;\n")
	} else {
		m.Printf("  local len = b.len();\n")
	}
	m.Printf("  if (len < 20) { _M.FixedReason = \"truncated\"; return -1; }\n")
	m.Printf("  for (local i = 1; i < 8; i++) if (b[i] != 0) { _M.FixedReason = \"reserved_not_zero\"; return -1; }\n")
	m.Printf("  local L = _rt_fixed.u32le(b, 16);\n")
	m.Printf("  if (L < 4 || 20 + L > len) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  local n = _rt_fixed.u32le(b, 20);\n")
	m.Printf("  if (n != (L - 4) / 17 || 4 + n * 17 != L) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  local rb = 8 + _rt_fixed.u32le(b, 20 + 4 + 9);\n")
	m.Printf("  if (rb < 8) { _M.FixedReason = \"truncated\"; return -1; }\n")
	m.Printf("  if (rb > 8 + %d) { _M.FixedReason = \"layout_record_too_large\"; return -1; }\n", ir.TableFixedRecordMaxBytes)
	if !strict {
		m.Printf("  local rest = len - 20 - L;\n")
		m.Printf("  if (rest %% rb != 0) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
		m.Printf("  local count = rest / rb;\n")
		m.Printf("  if (cap != null && count > cap) { _M.FixedReason = \"plan_too_large\"; return -1; }\n")
		m.Printf("  local h = _rt_fixed.u64le(b, 8);\n")
		m.Printf("  for (local k = 0; k < count; k++) {\n")
		m.Printf("    local at = 20 + L + k * rb;\n")
		m.Printf("    if (_rt_fixed.u64le(b, at) != h) { _M.FixedReason = \"no_layout\"; return -1; }\n")
		m.Printf("    local why = _M.%sFixedDecodeBody(out[k], b, at + 8, at + rb);\n", st.Name)
		m.Printf("    if (why != null) { _M.FixedReason = why; return -1; }\n")
		m.Printf("  }\n")
		m.Printf("  return count;\n")
		m.Printf("};\n\n")
		emitFixedPlainRead(m, st)
		return
	}
	// SELECT BY HASH, never by parsing a stranger's layout (§5.3 steps 4-8).
	m.Printf("  local h = _rt_fixed.u64le(b, 8);\n")
	m.Printf("  local pick = -1;\n")
	m.Printf("  for (local i = 0; i < _M.%sFixedLineage.len(); i++)\n", st.Name)
	m.Printf("    if (_M.%sFixedLineage[i].hash == h) { pick = i; break; }\n", st.Name)
	m.Printf("  if (pick < 0) { _M.FixedReason = \"layout_unknown\"; return -1; }\n")
	m.Printf("  if (pick < _M.%sFixedFloor) { _M.FixedReason = \"layout_unsupported\"; return -1; }\n", st.Name)
	m.Printf("  local e = _M.%sFixedLineage[pick];\n", st.Name)
	m.Printf("  if (L != e.layout.len()) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  for (local i = 0; i < L; i++) if (b[20 + i] != e.layout[i]) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  rb = e.record;\n")
	m.Printf("  if (rb <= 8) { _M.FixedReason = \"truncated\"; return -1; }\n")
	m.Printf("  local rest = len - 20 - L;\n")
	m.Printf("  if (rest %% rb != 0) { _M.FixedReason = \"truncated\"; return -1; }\n")
	m.Printf("  local count = rest / rb;\n")
	m.Printf("  if (cap != null && count > cap) { _M.FixedReason = \"plan_too_large\"; return -1; }\n")
	m.Printf("  for (local k = 0; k < count; k++) {\n")
	m.Printf("    local at = 20 + L + k * rb;\n")
	m.Printf("    if (_rt_fixed.u64le(b, at) != h) { _M.FixedReason = \"no_layout\"; return -1; }\n")
	m.Printf("    local why = e.decode == null ? null : e.decode(out[k], b, at + 8, at + rb);\n")
	m.Printf("    if (why != null) { _M.FixedReason = why; return -1; }\n")
	m.Printf("  }\n")
	m.Printf("  return count;\n")
	m.Printf("};\n\n")
	emitFixedPlainRead(m, st)
}

// emitFixedPlainRead lays the plain <Root>FixedLoad over the tolerant reader
// (docs/SPEC-TABLES.md §6.5): the strict fixed read answers -1 on every
// refusal and never hands back the records the tolerant read recovered. The
// fixed reader names its own refusal through FixedReason (§3.4).
func emitFixedPlainRead(m *Module, st *ir.Struct) {
	m.Printf("_M.%sFixedLoad <- function(b, out, cap) {\n", st.Name)
	m.Printf("  return _M.%sFixedLoadTolerant(b, out, cap);\n", st.Name)
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
	// a written body answers null unless a value refused: a NaN or an
	// infinity is non_finite_float, the field named on FixedField
	// (docs/SPEC-TABLES.md §3.3, §3.4)
	m.Printf("  return null;\n")
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
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4): there is no count and no
		// slack, so the declared bound IS the loop.
		count := fmt.Sprintf("%d", f.KeyEnumRef.Max)
		m.Printf("%sfor (local i = 0; i < %s; i++) {\n", ind, count)
		fixedEmitWriteElement(m, f, at, expr+"[i]", ind+"  ", "i * "+fmt.Sprint(fixedElementBytes(f)))
		m.Printf("%s}\n", ind)
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			// the COUNT is the array's own length (docs/SPEC-TABLES.md §2.9):
			// the Squirrel table surface stores a counted array as the array
			// alone, born at its min length, so len() is the live extent and
			// the slack stays the template's zeros.
			m.Printf("%s_rt_fixed.puti32(b, at + %d, %s.len());\n", ind, at, expr)
			count = expr + ".len()"
		}
		base := at
		if f.Array == ir.ArrayCounted {
			base += fixedCountBytes
		}
		m.Printf("%sfor (local i = 0; i < %s; i++) {\n", ind, count)
		fixedEmitWriteElement(m, f, base, expr+"[i]", ind+"  ", "i * "+fmt.Sprint(fixedElementBytes(f)))
		m.Printf("%s}\n", ind)
	case fixedIsText(f):
		fixedEmitWriteText(m, f, fmt.Sprint(at), expr, ind)
	default:
		fixedEmitWriteElement(m, f, at, expr, ind, "0")
	}
}

// fixedIsText is a lone string(N), bytes(N) or wstring(N): a length and its
// buffer, where an array element or a union arm holds one too.
func fixedIsText(f *ir.Field) bool {
	return f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString)
}

// fixedEmitWriteText stores a text field at `at + off`: the int32 used length,
// then that many units, the slack left at the template's zeros. A byte or a
// string unit is one byte; a WIDE unit is two, little-endian, and the length
// counts CODE UNITS (docs/SPEC-TABLES.md §3.4, §7.2).
func fixedEmitWriteText(m *Module, f *ir.Field, off, expr, ind string) {
	m.Printf("%s_rt_fixed.puti32(b, at + %s, %s.len());\n", ind, off, expr)
	if f.Type.Kind == ir.TWString {
		m.Printf("%sfor (local i = 0; i < %s.len(); i++) { b[at + %s + %d + 2 * i] = %s[i] & 0xff; b[at + %s + %d + 2 * i + 1] = (%s[i] >> 8) & 0xff; }\n",
			ind, expr, off, fixedCountBytes, expr, off, fixedCountBytes, expr)
		return
	}
	m.Printf("%sfor (local i = 0; i < %s.len(); i++) b[at + %s + %d + i] = %s[i] & 0xff;\n", ind, expr, off, fixedCountBytes, expr)
}

// fixedEmitDecodeText reads a text field at `at + off` back into expr: a
// length past the PLAN's bound refuses length_over_bound, and ill-formed text
// refuses text_ill_formed — UTF-8 for a string, paired surrogates and no zero
// unit for wide text (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md
// §4.5, §5.4 fix 11).
func fixedEmitDecodeText(m *Module, f *ir.Field, off, expr, ind string) {
	m.Printf("%slocal n = _rt_fixed.geti32(b, at + %s);\n", ind, off)
	m.Printf("%sif (n < 0 || n > %d) return \"length_over_bound\";\n", ind, f.Type.Size)
	switch f.Type.Kind {
	case ir.TString:
		m.Printf("%sif (_rt_fixed.textIllFormed(b, at + %s + %d, n)) return \"text_ill_formed\";\n", ind, off, fixedCountBytes)
		m.Printf("%slocal s = \"\";\n", ind)
		m.Printf("%sfor (local i = 0; i < n; i++) s += b[at + %s + %d + i].tochar();\n", ind, off, fixedCountBytes)
		m.Printf("%s%s = s;\n", ind, expr)
	case ir.TWString:
		m.Printf("%slocal w = _rt_fixed.wideUnits(b, at + %s + %d, n);\n", ind, off, fixedCountBytes)
		m.Printf("%sif (w == null) return \"text_ill_formed\";\n", ind)
		m.Printf("%s%s = w;\n", ind, expr)
	default:
		m.Printf("%s%s = blob(n);\n", ind, expr)
		m.Printf("%sfor (local i = 0; i < n; i++) %s[i] = b[at + %s + %d + i];\n", ind, expr, off, fixedCountBytes)
	}
}

func fixedEmitWriteElement(m *Module, f *ir.Field, at int64, expr, ind, dyn string) {
	off := fmt.Sprintf("%d", at)
	if dyn != "0" {
		off = fmt.Sprintf("%d + %s", at, dyn)
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			m.Printf("%s{\n%s  local why = _M.%sFixedWriteBody(b, at + %s, %s);\n%s  if (why != null) return why;\n%s}\n", ind, ind, r.Name, off, expr, ind, ind)
			return
		}
		if r, ok := f.Type.Ref.(*ir.Union); ok {
			// THE GUARD CHAIN ANSWERS THE OUTER TAG (docs/FIXED-FORM-ALGORITHM.md
			// §4.1): the tag ordinal at the tag type's storage width, then the
			// WIDEST arm, the slack behind a narrower arm zero-filled.
			tag := fixedUnionTagBytes(r)
			m.Printf("%s_rt_fixed.put(b, at + %s, %d, %s.Type);\n", ind, off, tag, expr)
			for i, v := range r.Variants {
				if v.F == nil {
					continue
				}
				m.Printf("%sif (%s.Type == %d) {\n", ind, expr, i+1)
				if fixedIsText(v.F) {
					// a text arm is a length and its buffer at the arm's start
					fixedEmitWriteText(m, v.F, fmt.Sprintf("%s + %d", off, tag), expr+"."+ir.GoExportName(v.Name), ind+"  ")
				} else {
					fixedEmitWriteElement(m, v.F, at+tag, expr+"."+ir.GoExportName(v.Name), ind+"  ", dyn)
				}
				m.Printf("%s}\n", ind)
			}
			return
		}
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			// an enum's table storage is its variant NAME; form 3 carries the
			// variant's ordinal (docs/SPEC-TABLES.md §2.15, §5)
			m.Printf("%s{\n", ind)
			m.Printf("%s  local ev = 0;\n", ind)
			for i, v := range e.Variants {
				m.Printf("%s  if (%s == %s) ev = %d;\n", ind, expr, squirrel.StringLit([]byte(v)), i+1)
			}
			m.Printf("%s  _rt_fixed.put(b, at + %s, %d, ev);\n", ind, off, fixedStorageBytes(f.Type))
			m.Printf("%s}\n", ind)
			return
		}
	}
	// a 128-bit leaf is a sixteen-byte store, the low 64-bit half first
	// (docs/SPEC-TABLES.md §3.4): the Squirrel value is the [lo, hi] pair, so
	// the low half lands in its own eight-byte store before the high half.
	if fixedStorageBytes(f.Type) == 16 {
		m.Printf("%s_rt_fixed.put(b, at + %s, 8, %s[0]);\n", ind, off, expr)
		m.Printf("%s_rt_fixed.put(b, at + %s + 8, 8, %s[1]);\n", ind, off, expr)
		return
	}
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		put := "putF64"
		if f.Type.Kind == ir.TFloat32 {
			put = "putF32"
		}
		m.Printf("%sif (!_rt_fixed.%s(b, at + %s, %s)) { _M.FixedField <- %s; return \"non_finite_float\"; }\n",
			ind, put, off, expr, squirrel.StringLit([]byte(FloatOwner(m.Unit, f))))
		return
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
	// a decoded body answers null unless a field refused (count_over_bound,
	// length_over_bound, text_ill_formed, or a nested body's reason), which the
	// load propagates to FixedReason (docs/FIXED-FORM-ALGORITHM.md §5.3,
	// docs/SPEC-TABLES.md §3.4).
	m.Printf("  return null;\n")
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
	case f.KeyEnum != "":
		m.Printf("%s  for (local i = 0; i < %d; i++) {\n", ind, f.KeyEnumRef.Max)
		fixedEmitDecodeElement(m, f, at, expr+"[i]", ind+"    ", "i * "+fmt.Sprint(fixedElementBytes(f)), true)
		m.Printf("%s  }\n", ind)
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		base := at
		if f.Array == ir.ArrayCounted {
			// the count is the array's OWN LENGTH: a count past the PLAN's bound
			// refuses count_over_bound instead of clamping, and resizing to the
			// wire count reproduces the oracle byte for byte
			// (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §4.5).
			m.Printf("%s  local c = _rt_fixed.geti32(b, at + %d);\n", ind, at)
			m.Printf("%s  if (c < 0 || c > %d) return \"count_over_bound\";\n", ind, f.ArrayBound)
			m.Printf("%s  %s.resize(c);\n", ind, expr)
			base += fixedCountBytes
			m.Printf("%s  for (local i = 0; i < c; i++) {\n", ind)
		} else {
			m.Printf("%s  while (%s.len() < %d) %s.append(%s);\n", ind, expr, f.ArrayBound, expr, elemZero(f))
			m.Printf("%s  for (local i = 0; i < %d; i++) {\n", ind, f.ArrayBound)
		}
		fixedEmitDecodeElement(m, f, base, expr+"[i]", ind+"    ", "i * "+fmt.Sprint(fixedElementBytes(f)), true)
		m.Printf("%s  }\n", ind)
	case fixedIsText(f):
		fixedEmitDecodeText(m, f, fmt.Sprint(at), expr, ind+"  ")
	default:
		fixedEmitDecodeElement(m, f, at, expr, ind+"  ", "0", false)
	}
	m.Printf("%s}\n", ind)
}

func fixedEmitDecodeElement(m *Module, f *ir.Field, at int64, expr, ind, dyn string, element bool) {
	off := fmt.Sprintf("%d", at)
	if dyn != "0" {
		off = fmt.Sprintf("%d + %s", at, dyn)
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			// a nested body's refusal propagates to the caller, which carries it
			// up to the load's FixedReason (docs/FIXED-FORM-ALGORITHM.md §5.3).
			m.Printf("%s  local why = _M.%sFixedDecodeBody(%s, b, at + %s, lim);\n", ind, r.Name, expr, off)
			m.Printf("%s  if (why != null) return why;\n", ind)
			return
		}
		if r, ok := f.Type.Ref.(*ir.Union); ok {
			tag := fixedUnionTagBytes(r)
			m.Printf("%s%s.Type = _rt_fixed.get(b, at + %s, %d);\n", ind, expr, off, tag)
			m.Printf("%sif (%s.Type > %d) %s.Type = 0;\n", ind, expr, len(r.Variants), expr)
			for i, v := range r.Variants {
				if v.F == nil {
					continue
				}
				m.Printf("%sif (%s.Type == %d) {\n", ind, expr, i+1)
				if fixedIsText(v.F) {
					fixedEmitDecodeText(m, v.F, fmt.Sprintf("%s + %d", off, tag), expr+"."+ir.GoExportName(v.Name), ind+"  ")
				} else {
					fixedEmitDecodeElement(m, v.F, at+tag, expr+"."+ir.GoExportName(v.Name), ind+"  ", dyn, false)
				}
				m.Printf("%s}\n", ind)
			}
			return
		}
		if e, ok := f.Type.Ref.(*ir.Enum); ok {
			// ordinal back to the variant NAME table storage holds; a
			// DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, and an
			// array element as the ELEMENT's default, None
			// (docs/SPEC-TABLES.md §2.15)
			land := "null"
			if !element && f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
				land = squirrel.StringLit([]byte(f.DefVariant))
			}
			m.Printf("%s{\n", ind)
			m.Printf("%s  local ev = _rt_fixed.get(b, at + %s, %d);\n", ind, off, fixedStorageBytes(f.Type))
			m.Printf("%s  local val = null;\n", ind)
			for i, v := range e.Variants {
				kw := "if"
				if i > 0 {
					kw = "else if"
				}
				value := squirrel.StringLit([]byte(v))
				if i < len(e.VariantDeprecated) && e.VariantDeprecated[i] {
					value = land
				}
				m.Printf("%s  %s (ev == %d) val = %s;\n", ind, kw, i+1, value)
			}
			m.Printf("%s  %s = val;\n%s}\n", ind, expr, ind)
			return
		}
	}
	// a 128-bit leaf is a sixteen-byte store, the low 64-bit half first
	// (docs/SPEC-TABLES.md §3.4): read the two eight-byte halves back into
	// the [lo, hi] pair the table surface uses.
	if fixedStorageBytes(f.Type) == 16 {
		m.Printf("%s%s = [_rt_fixed.u64le(b, at + %s), _rt_fixed.u64le(b, at + %s + 8)];\n", ind, expr, off, off)
		return
	}
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		get := "getF64"
		if f.Type.Kind == ir.TFloat32 {
			get = "getF32"
		}
		// refused on its raw bits before the field is written
		// (docs/SPEC-TABLES.md §3.3): the load answers non_finite_float
		m.Printf("%s{\n%s  local x = _rt_fixed.%s(b, at + %s);\n", ind, ind, get, off)
		m.Printf("%s  if (x == null) { _M.FixedField <- %s; return \"non_finite_float\"; }\n", ind, squirrel.StringLit([]byte(FloatOwner(m.Unit, f))))
		m.Printf("%s  %s = x;\n%s}\n", ind, expr, ind)
		return
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

// fixedUnionTagBytes is the storage width of a union's tag ordinal.
func fixedUnionTagBytes(u *ir.Union) int64 { return int64(ir.StorageBitsFor(u.Max) / 8) }

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
	for i := range 8 {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

var _ = fmt.Sprintf
var _ = squirrel.StringLit
