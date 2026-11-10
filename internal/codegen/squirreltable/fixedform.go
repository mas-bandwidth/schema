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
	"math"
	"math/big"
	"strconv"
	"strings"

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
	if !st.IsTable || !st.FixedDeclared {
		// A NON-TABLE TYPE (OR NESTED VARIABLE TABLE) REACHED FROM A FIXED ROOT
		// still needs its body helpers: the root's FixedWriteBody/FixedDecodeBody
		// call them by name.
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
	trailer := int64(0)
	if st.Integrity != "" {
		trailer = 4
	}
	m.Printf("_M.%sFixedMeasure <- function(count = 1) { return count < 0 ? -1 : %d + count * %d; };\n", st.Name, int64(20+len(layout))+trailer, rec)

	// EVERY READ SELECTS BY HASH AND HOLDS THE LAYOUT TO ITS BYTES
	// (docs/FIXED-FORM-ALGORITHM.md §5.3; strict-only): a build with no lineage
	// lock reads its own entry only, so a header whose hash is not this
	// table's, or whose layout is not this table's byte for byte, refuses
	// rather than being parsed loosely.
	lineage := lineageOf(m.Unit)
	var handed []FixedLineageEntry
	if lineage != nil {
		handed = lineage[st.Name]
	}
	entries, floor := generateLineage(handed, FixedLineageEntry{
		Wire: hash, Layout: layout, Record: rec,
	})
	emitFixedPlans(m, st, entries, hash)
	emitFixedLineage(m, st, entries, floor)

	emitFixedWrite(m, st, w)
	emitFixedDeprecatedCheck(m, st)
	emitFixedRead(m, st)
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
		if fixedValidateLayout(e.Layout) != "" {
			continue
		}
		m.Printf("_M.%sFixedPlan%d <- function(v, b, at, lim) {\n", st.Name, i)
		if e.Wire == own {
			m.Printf("  return _M.%sFixedDecodeBody(v, b, at, lim);\n", st.Name)
			m.Printf("};\n\n")
			continue
		}
		emitFixedPlanBody(m, st, e)
		m.Printf("  return null;\n};\n\n")
	}
}

// emitFixedLineage is §5.2 COMPILE's static data: the hashes, the layout bytes,
// the whole record sizes, the floor and the plans, oldest first with this
// build's own entry last (docs/SPEC-TABLES.md §21, §2.10).
func emitFixedLineage(m *Module, st *ir.Struct, entries []FixedLineageEntry, floor int) {
	m.Printf("_M.%sFixedFloor <- %d;\n", st.Name, floor)
	m.Printf("_M.%sFixedLineage <- [\n", st.Name)
	for i, e := range entries {
		refuse := fixedValidateLayout(e.Layout)
		if refuse == "" && e.Record > 0 && int64(len(e.Layout)) >= 4+17 {
			rootSz := int64(uint32(e.Layout[4+9]) | uint32(e.Layout[4+10])<<8 | uint32(e.Layout[4+11])<<16 | uint32(e.Layout[4+12])<<24)
			if 8+rootSz != e.Record {
				refuse = "layout_record_too_large"
			}
		}
		decode := "null"
		if !e.Retired && refuse == "" {
			decode = fmt.Sprintf("_M.%sFixedPlan%d", st.Name, i)
		}
		m.Printf("  { hash = %s, layoutHex = \"%s\", layout = null, record = %d, refuse = %s, decode = %s },\n",
			squirrel.HexLit(e.Wire), byteHex(e.Layout), e.Record, squirrel.StringLit([]byte(refuse)), decode)
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

func emitFixedRead(m *Module, st *ir.Struct) {
	m.Printf("_M.%sFixedLoad <- function(b, out, cap) {\n", st.Name)
	m.Printf("  _M.FixedReason <- null;\n")
	m.Printf("  _M.FixedField <- null;\n")
	m.Printf("  _M.FixedWidened <- 0;\n")
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
	m.Printf("  if (L < 21 || 20 + L > len) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	// SELECT BY HASH, never by parsing a stranger's layout (§5.3 steps 4-8).
	m.Printf("  local h = _rt_fixed.u64le(b, 8);\n")
	m.Printf("  local pick = -1;\n")
	m.Printf("  for (local i = 0; i < _M.%sFixedLineage.len(); i++)\n", st.Name)
	m.Printf("    if (_M.%sFixedLineage[i].hash == h) { pick = i; break; }\n", st.Name)
	m.Printf("  if (pick < 0) { _M.FixedReason = (_M.%sFixedLineage.len() == 0 ? \"layout_unsupported\" : \"layout_unknown\"); return -1; }\n", st.Name)
	m.Printf("  if (pick < _M.%sFixedFloor) { _M.FixedReason = \"layout_unsupported\"; return -1; }\n", st.Name)
	m.Printf("  local e = _M.%sFixedLineage[pick];\n", st.Name)
	m.Printf("  if (L * 2 != e.layoutHex.len()) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  if (e.layout == null) e.layout = _rt_fixed.blobOfHex(e.layoutHex);\n")
	m.Printf("  for (local i = 0; i < L; i++) if (b[20 + i] != e.layout[i]) { _M.FixedReason = \"layout_malformed\"; return -1; }\n")
	m.Printf("  if (e.refuse != \"\") { _M.FixedReason = e.refuse; return -1; }\n")
	m.Printf("  local rb = e.record;\n")
	m.Printf("  if (rb <= 8 || rb > 8 + %d) { _M.FixedReason = \"layout_record_too_large\"; return -1; }\n", ir.TableFixedRecordMaxBytes)
	m.Printf("  local rest = len - 20 - L;\n")
	m.Printf("  if (rest %% rb != 0) { _M.FixedReason = \"truncated\"; return -1; }\n")
	m.Printf("  local count = rest / rb;\n")
	m.Printf("  if (cap != null && count > cap) { _M.FixedReason = \"plan_too_large\"; return -1; }\n")
	m.Printf("  if (count > out.len()) { _M.FixedReason = \"batch_too_large\"; return -1; }\n")
	// TOTAL OVER THE BATCH (docs/FIXED-FORM-ALGORITHM.md §5.3; tla/FixedBatch):
	// every record's hash is checked, and the record decoded and checked
	// (live counts, lengths, text, bool and present bytes, floats, this
	// reader's and the writer's ranges) into
	// a FRESH row of the scratch batch, and only when the last record has
	// passed does the commit loop land the scratch rows into out[0..count-1]
	// (FIXED-FORM-VERSIONING-TESTS row 9: a forged hash on the LAST record
	// writes not one destination byte). A refusal on record k leaves every row of the caller's `out` untouched,
	// out[0..k-1] exactly as much as out[k], and the counters zero. A fresh
	// row also holds a field an older writer did not carry at the
	// constructor's declared default (§5.2), never at a reused row's value.
	m.Printf("  local d = e.decode;\n")
	m.Printf("  local s = array(count);\n")
	m.Printf("  for (local k = 0, at = 20 + L; k < count; k++, at += rb) {\n")
	m.Printf("    if (_rt_fixed.u64le(b, at) != h) { _M.FixedReason = \"no_layout\"; return -1; }\n")
	m.Printf("    local v = _M.%s();\n", st.Name)
	m.Printf("    local why = d(v, b, at + 8, at + rb);\n")
	m.Printf("    if (why != null) { _M.FixedReason = why; _M.FixedWidened <- 0; return -1; }\n")
	m.Printf("    s[k] = v;\n")
	m.Printf("  }\n")
	// The commit lands each scratch row INTO the caller's row, member by
	// member, so out[k] keeps its identity (a caller that holds out[k] reads
	// the landed values) and a nested row is the scratch row's own.
	m.Printf("  for (local k = 0; k < count; k++) {\n")
	m.Printf("    local o = out[k], v = s[k];\n")
	for _, f := range st.Fields {
		mem := squirrel.Member(f)
		m.Printf("    o.%s = v.%s;\n", mem, mem)
		if f.Type.Optional {
			m.Printf("    o.%sPresent = v.%sPresent;\n", mem, mem)
		}
	}
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
			m.Printf("%sif (%s.len() < 0 || %s.len() > %d) return \"count_over_bound\";\n", ind, expr, expr, f.ArrayBound)
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

// fixedIsText is a lone string(N) or bytes(N): a length and its
// buffer, where an array element or a union arm holds one too.
func fixedIsText(f *ir.Field) bool {
	return f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes)
}

// fixedEmitWriteText stores a text field at `at + off`: the int32 used length,
// then that many bytes, the slack left at the template's zeros
// (docs/SPEC-TABLES.md §3.4, §7.2).
func fixedEmitWriteText(m *Module, f *ir.Field, off, expr, ind string) {
	m.Printf("%s_rt_fixed.puti32(b, at + %s, %s.len());\n", ind, off, expr)
	m.Printf("%sfor (local i = 0; i < %s.len(); i++) b[at + %s + %d + i] = %s[i] & 0xff;\n", ind, expr, off, fixedCountBytes, expr)
}

// fixedEmitDecodeText reads a text field at `at + off` back into expr: a
// length past the PLAN's bound refuses length_over_bound, and ill-formed text
// refuses text_ill_formed — UTF-8 for a string (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5, §5.4 fix 11).
func fixedEmitDecodeText(m *Module, f *ir.Field, off, expr, ind string) {
	m.Printf("%slocal n = _rt_fixed.geti32(b, at + %s);\n", ind, off)
	m.Printf("%sif (n < 0 || n > %d) return \"length_over_bound\";\n", ind, f.Type.Size)
	switch f.Type.Kind {
	case ir.TString:
		m.Printf("%sif (_rt_fixed.textIllFormed(b, at + %s + %d, n)) return \"text_ill_formed\";\n", ind, off, fixedCountBytes)
		m.Printf("%slocal s = \"\";\n", ind)
		m.Printf("%sfor (local i = 0; i < n; i++) s += b[at + %s + %d + i].tochar();\n", ind, off, fixedCountBytes)
		m.Printf("%s%s = s;\n", ind, expr)
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
				armExpr := expr + "." + ir.GoExportName(v.Name)
				armInd := ind + "  "
				switch {
				case v.F.KeyEnum != "":
					m.Printf("%sfor (local i = 0; i < %d; i++) {\n", armInd, v.F.KeyEnumRef.Max)
					fixedEmitWriteElement(m, v.F, at+tag, armExpr+"[i]", armInd+"  ", dynAdd(dyn, "i * "+fmt.Sprint(fixedElementBytes(v.F))))
					m.Printf("%s}\n", armInd)
				case v.F.Array == ir.ArrayFixed || v.F.Array == ir.ArrayCounted:
					base := at + tag
					if v.F.Array == ir.ArrayCounted {
						m.Printf("%slocal c = %s.len();\n", armInd, armExpr)
						m.Printf("%sif (c < 0 || c > %d) return \"count_over_bound\";\n", armInd, v.F.ArrayBound)
						m.Printf("%s_rt_fixed.puti32(b, at + %s + %d, c);\n", armInd, off, tag)
						base += fixedCountBytes
						m.Printf("%sfor (local i = 0; i < c; i++) {\n", armInd)
					} else {
						m.Printf("%sfor (local i = 0; i < %d; i++) {\n", armInd, v.F.ArrayBound)
					}
					fixedEmitWriteElement(m, v.F, base, armExpr+"[i]", armInd+"  ", dynAdd(dyn, "i * "+fmt.Sprint(fixedElementBytes(v.F))))
					m.Printf("%s}\n", armInd)
				case fixedIsText(v.F):
					fixedEmitWriteText(m, v.F, fmt.Sprintf("%s + %d", off, tag), armExpr, armInd)
				default:
					fixedEmitWriteElement(m, v.F, at+tag, armExpr, armInd, dyn)
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
	if f.Type.Kind == ir.TBool {
		m.Printf("%sb[at + %s] = (%s ? 1 : 0);\n", ind, off, expr)
		return
	}
	m.Printf("%s_rt_fixed.put(b, at + %s, %d, %s);\n", ind, off, fixedStorageBytes(f.Type), expr)
}

func dynAdd(dyn, term string) string {
	if dyn == "0" {
		return term
	}
	return dyn + " + " + term
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
		// a present byte other than 0 or 1 is damage (§3.4)
		m.Printf("%s  if (b[at + %d] > 1) return \"wire_damaged\";\n", ind, at)
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
			m.Printf("%s  if (%s.len() > c) %s.resize(c); while (%s.len() < c) %s.append(%s);\n", ind, expr, expr, expr, expr, elemZero(f))
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
			m.Printf("%s%s.Type = _rt_fixed.getu(b, at + %s, %d);\n", ind, expr, off, tag)
			// A FORGED TAG, outside this reader's own set, refuses
			// (docs/FIXED-FORM-ALGORITHM.md §5.4; tablewire fixed.go element)
			m.Printf("%sif (%s.Type < 0 || %s.Type > %d) return \"out_of_range\";\n", ind, expr, expr, len(r.Variants))
			for i, v := range r.Variants {
				if v.F == nil {
					continue
				}
				m.Printf("%sif (%s.Type == %d) {\n", ind, expr, i+1)
				armExpr := expr + "." + ir.GoExportName(v.Name)
				armInd := ind + "  "
				switch {
				case v.F.KeyEnum != "":
					m.Printf("%sfor (local i = 0; i < %d; i++) {\n", armInd, v.F.KeyEnumRef.Max)
					fixedEmitDecodeElement(m, v.F, at+tag, armExpr+"[i]", armInd+"  ", dynAdd(dyn, "i * "+fmt.Sprint(fixedElementBytes(v.F))), true)
					m.Printf("%s}\n", armInd)
				case v.F.Array == ir.ArrayFixed || v.F.Array == ir.ArrayCounted:
					base := at + tag
					if v.F.Array == ir.ArrayCounted {
						m.Printf("%slocal c = _rt_fixed.geti32(b, at + %s + %d);\n", armInd, off, tag)
						m.Printf("%sif (c < 0 || c > %d) return \"count_over_bound\";\n", armInd, v.F.ArrayBound)
						m.Printf("%sif (%s.len() > c) %s.resize(c); while (%s.len() < c) %s.append(%s);\n", armInd, armExpr, armExpr, armExpr, armExpr, elemZero(v.F))
						base += fixedCountBytes
						m.Printf("%sfor (local i = 0; i < c; i++) {\n", armInd)
					} else {
						m.Printf("%swhile (%s.len() < %d) %s.append(%s);\n", armInd, armExpr, v.F.ArrayBound, armExpr, elemZero(v.F))
						m.Printf("%sfor (local i = 0; i < %d; i++) {\n", armInd, v.F.ArrayBound)
					}
					fixedEmitDecodeElement(m, v.F, base, armExpr+"[i]", armInd+"  ", dynAdd(dyn, "i * "+fmt.Sprint(fixedElementBytes(v.F))), true)
					m.Printf("%s}\n", armInd)
				case fixedIsText(v.F):
					fixedEmitDecodeText(m, v.F, fmt.Sprintf("%s + %d", off, tag), armExpr, armInd)
				default:
					fixedEmitDecodeElement(m, v.F, at+tag, armExpr, armInd, dyn, false)
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
			m.Printf("%s  local ev = _rt_fixed.getu(b, at + %s, %d);\n", ind, off, fixedStorageBytes(f.Type))
			// A FORGED ORDINAL refuses (docs/FIXED-FORM-ALGORITHM.md §5.4)
			m.Printf("%s  if (ev < 0 || ev > %d) return \"out_of_range\";\n", ind, len(e.Variants))
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
		fixedEmitRange128Guard(m, f, expr, ind)
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
		fixedEmitRangeGuard(m, f, "x", ind+"  ", fixedStorageBytes(f.Type))
		m.Printf("%s  %s = x;\n%s}\n", ind, expr, ind)
		return
	}
	if f.Type.Kind == ir.TBool {
		// a bool byte other than 0 or 1 is damage (§3.4; tablewire fixed.go)
		m.Printf("%sif (b[at + %s] > 1) return \"wire_damaged\";\n", ind, off)
		m.Printf("%s%s = b[at + %s] != 0;\n", ind, expr, off)
		return
	}
	if (f.Type.Kind == ir.TInt && !f.Type.Signed) || (f.Type.Kind == ir.TFixed && !f.Type.Signed) || f.Type.Kind == ir.TBits || (f.Type.Kind == ir.TNamed && f.Type.Ref != nil) {
		m.Printf("%s%s = _rt_fixed.getu(b, at + %s, %d);\n", ind, expr, off, fixedStorageBytes(f.Type))
		fixedEmitRangeGuard(m, f, expr, ind, fixedStorageBytes(f.Type))
		return
	}
	m.Printf("%s%s = _rt_fixed.get(b, at + %s, %d);\n", ind, expr, off, fixedStorageBytes(f.Type))
	fixedEmitRangeGuard(m, f, expr, ind, fixedStorageBytes(f.Type))
}

// fixedEmitRangeGuard emits the declared-range refusal for one decoded leaf,
// the Go engine's scalarAt rule over the fixed form's storage bytes
// (docs/SPEC-TABLES.md §3.3, §4; docs/plans/strict-only.md: a value out of
// range refuses, never clamps): a compressed float32 outside [min, max], an
// integer or fixed-point raw value outside its declared range, and a bits(N)
// value wider than N bits each answer out_of_range. A compressed float's -0
// lands as +0. An end the storage width already holds is not spelled.
func fixedEmitRangeGuard(m *Module, f *ir.Field, expr, ind string, storage int64) {
	if f.Type.Kind == ir.TFloat32 {
		if f.HasFloatRange {
			lo := strconv.FormatFloat(f.FMin, 'e', -1, 64)
			hi := strconv.FormatFloat(f.FMax, 'e', -1, 64)
			m.Printf("%sif (%s < %s || %s > %s) return \"out_of_range\";\n", ind, expr, lo, expr, hi)
			m.Printf("%sif (%s == 0.0) %s = 0.0;\n", ind, expr, expr)
		}
		return
	}
	if f.Type.Kind == ir.TFloat64 || storage < 1 || storage > 8 {
		return
	}
	if f.Type.Kind == ir.TNamed {
		return
	}
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	bits := uint(storage * 8)
	if lo, hi, ok := ir.TableRawRange(f); ok {
		var smin, smax *big.Int
		if signed {
			smin = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
			smax = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
		} else {
			smin = big.NewInt(0)
			smax = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
		}
		var conds []string
		if !signed && storage == 8 {
			// a uint64 is a signed Squirrel integer: compare with the sign
			// bit flipped, so the unsigned order is the signed one
			flip := func(v *big.Int) string {
				return strconv.FormatInt(int64(v.Uint64()^(uint64(1)<<63)), 10)
			}
			if lo.Cmp(smin) > 0 {
				conds = append(conds, fmt.Sprintf("(%s ^ (1 << 63)) < %s", expr, flip(lo)))
			}
			if hi.Cmp(smax) < 0 {
				conds = append(conds, fmt.Sprintf("(%s ^ (1 << 63)) > %s", expr, flip(hi)))
			}
		} else {
			if lo.Cmp(smin) > 0 {
				conds = append(conds, fmt.Sprintf("%s < %s", expr, lo.String()))
			}
			if hi.Cmp(smax) < 0 {
				conds = append(conds, fmt.Sprintf("%s > %s", expr, hi.String()))
			}
		}
		if len(conds) == 2 {
			m.Printf("%sif (%s || %s) return \"out_of_range\";\n", ind, conds[0], conds[1])
		} else if len(conds) == 1 {
			m.Printf("%sif (%s) return \"out_of_range\";\n", ind, conds[0])
		}
	}
	if f.Type.Kind == ir.TBits && uint(f.Type.Width) < bits {
		mask := (uint64(1) << uint(f.Type.Width)) - 1
		m.Printf("%sif (%s < 0 || %s > %d) return \"out_of_range\";\n", ind, expr, expr, mask)
	}
}

// fixedEmitRange128Guard is fixedEmitRangeGuard for a 128-bit leaf, held as
// the [lo, hi] pair of 64-bit halves (docs/FIXED-FORM-ALGORITHM.md §4.6: the
// comparison is in the field's own type). The high halves compare signed for a
// signed kind and unsigned otherwise; the low halves always unsigned, by the
// sign-bit flip. An end the 128-bit storage already holds is not spelled.
func fixedEmitRange128Guard(m *Module, f *ir.Field, expr, ind string) {
	if f.Type.Kind != ir.TInt && f.Type.Kind != ir.TFixed {
		return
	}
	lo, hi, ok := ir.TableRawRange(f)
	if !ok {
		return
	}
	signed := f.Type.Signed
	one := big.NewInt(1)
	smin, smax := big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, 128), one)
	if signed {
		smin = new(big.Int).Neg(new(big.Int).Lsh(one, 127))
		smax = new(big.Int).Sub(new(big.Int).Lsh(one, 127), one)
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(one, 64), one)
	lit := func(v int64) string {
		if v == math.MinInt64 {
			return "(-9223372036854775807 - 1)"
		}
		return strconv.FormatInt(v, 10)
	}
	flip := func(u uint64) string { return lit(int64(u ^ (uint64(1) << 63))) }
	// halves splits a bound into the literal its high half compares against
	// (flipped for an unsigned kind) and its low half's flipped literal.
	halves := func(v *big.Int) (string, string) {
		l := new(big.Int).And(v, mask).Uint64()
		h := new(big.Int).Rsh(v, 64)
		if signed {
			return lit(h.Int64()), flip(l)
		}
		return flip(h.Uint64()), flip(l)
	}
	H := expr + "[1]"
	if !signed {
		H = "(" + expr + "[1] ^ (1 << 63))"
	}
	L := "(" + expr + "[0] ^ (1 << 63))"
	var conds []string
	if lo.Cmp(smin) > 0 {
		bh, bl := halves(lo)
		conds = append(conds, fmt.Sprintf("(%s < %s || (%s == %s && %s < %s))", H, bh, H, bh, L, bl))
	}
	if hi.Cmp(smax) < 0 {
		bh, bl := halves(hi)
		conds = append(conds, fmt.Sprintf("(%s > %s || (%s == %s && %s > %s))", H, bh, H, bh, L, bl))
	}
	if len(conds) > 0 {
		m.Printf("%sif (%s) return \"out_of_range\";\n", ind, strings.Join(conds, " || "))
	}
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
				fixedWalkPayload(w, v.F, ir.TableWireId(v.WireName()), fixedFieldBytes(v.F), tag)
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

func emitFixedFacts(m *Module, st *ir.Struct) {
	if !st.FixedDeclared || !ir.TableFixedSupported(st, 0) {
		m.Printf("  femit = false,\n")
		return
	}
	m.Printf("  fsize = %d,\n", ir.TableFixedTypeBytes(st))
	if !ir.TableFixedEmitted(m.Unit, st) {
		m.Printf("  femit = false,\n")
		return
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	hash := ir.TableFixedLayoutHash(layout, st)
	m.Printf("  femit = true,\n")
	m.Printf("  lhash = %s,\n", squirrel.HexLit(hash))
	m.Printf("  layout = _rt_descriptor.blobOf(%s),\n", squirrel.StringLit(layout))
}

func fixedFieldCarried(f *ir.Field, depth int) bool {
	if depth > 64 || f.Type.Pointer || f.IsMap() || f.Array == ir.ArrayList || f.Type.TextUnbounded() {
		return false
	}
	if f.Type.Kind != ir.TNamed {
		return true
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return ir.TableFixedSupported(r, 0)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && !fixedFieldCarried(v.F, depth+1) {
				return false
			}
		}
	}
	return true
}

var _ = fmt.Sprintf
var _ = squirrel.StringLit
