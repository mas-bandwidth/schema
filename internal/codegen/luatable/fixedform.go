// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in Lua: the
// LAYOUT walk and the emission of the fixed-form module body. The runtime that
// reads and writes the bytes is runtime/fixed.lua, emitted beside the module by
// the embed glob in luatable.go. The layout arithmetic is the same the C++
// reference does, so the block this file builds is the same run of bytes
// (docs/FIXED-FORM-ALGORITHM.md); the reader path mirrors the JavaScript leg
// (internal/codegen/jstable/fixedjs.go).
package luatable

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table
// (docs/FIXED-FORM-ALGORITHM.md §5.2): the wire hash a file is matched on, the
// layout bytes verbatim, and the whole record size (the eight hash bytes and
// the body). Retired and Reason are the operator's floor marks.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
	Ranges  []ir.TableFixedWriterRange
}

// FixedLineageOf computes the entry a unit's OWN build locks for table `name`.
// It is how a test states an older entry without a lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range fixedRoots(u) {
		if st.Name != name {
			continue
		}
		entries := ir.TableFixedWalkRoot(st)
		layout := ir.TableFixedLayoutBytes(entries)
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: fixedHashBytes + fixedTypeBytes(st),
			Ranges: ir.TableFixedWriterRanges(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// fixedWriterRanges is one lineage entry's WRITER ranges
// (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6): for each ranged integer leaf of
// the reader's root that the older writer bounded at the same scalar kind
// (ir.TableFixedWriterRanges, ir.TableFixedWriterField), the writer's raw
// bounds keyed by the field's wire id.
func fixedWriterRanges(st *ir.Struct, e FixedLineageEntry, own uint64) string {
	if e.Wire == own || len(e.Ranges) == 0 {
		return ""
	}
	var parts []string
	for _, r := range e.Ranges {
		for _, f := range st.Fields {
			if _, ok := ir.TableFixedWriterField(f, r); !ok {
				continue
			}
			parts = append(parts, fmt.Sprintf("[%s] = { lo = %s, hi = %s }",
				idLit(ir.TableFieldWireId(f)),
				lua.IntLiteral(big.NewInt(r.Lo)),
				lua.IntLiteral(big.NewInt(r.Hi))))
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ", ranges = { " + strings.Join(parts, ", ") + " }"
}

// fixedLineageFor is the lineage the emitter lays down for one table: the
// entries the build handed it, OLDEST FIRST, with the table's OWN entry
// appended last when the lineage does not already end on it. The floor is
// 1 + the highest retired index, or 0 when none is (§5.2).
func fixedLineageFor(handed []FixedLineageEntry, own FixedLineageEntry) ([]FixedLineageEntry, int) {
	entries := append([]FixedLineageEntry(nil), handed...)
	last := len(entries) - 1
	if last < 0 || entries[last].Wire != own.Wire {
		entries = append(entries, own)
	}
	floor := 0
	for i, e := range entries {
		if e.Retired {
			floor = i + 1
		}
	}
	return entries, floor
}

// pendingLineage carries the caller's lineage to the registered fixed-form
// pass without widening [Module]. The pass runs inside Generate, which has no
// lineage argument; GenerateLineage stores it for the duration of one call.
var pendingLineage sync.Map // *ir.Unit -> map[string][]FixedLineageEntry

// GenerateLineage is [Generate] with the LINEAGE the build holds for each fixed
// table (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #1). [Generate] is the same
// call with no lineage, so a unit with no lock keeps exactly today's behaviour
// for its own hash.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(lineage) > 0 {
		pendingLineage.Store(u, lineage)
		defer pendingLineage.Delete(u)
	}
	return Generate(u)
}

const (
	fixedEntryBytes   = 17
	fixedCountBytes   = 4
	fixedPresentBytes = 1
	fixedHashBytes    = 8
	fixedFormByte     = 3
)

// fixedStorageBytes is a LEAF's declared storage width (SPEC.md §4.3).
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
		return fixedPresentBytes + payload
	}
	return payload
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

// fixedTypeBytesAt is the constant-size walk with the layout's own depth
// bound (docs/FIXED-FORM-ALGORITHM.md §1.1 rule 7): a closure that somehow
// reaches past it contributes nothing and never overflows.
func fixedTypeBytesAt(st *ir.Struct, depth int) int64 {
	if depth > ir.TableFixedMaxDepth {
		return 0
	}
	var n int64
	for _, f := range st.Fields {
		n += fixedFieldBytesAt(f, depth)
	}
	return n
}

func fixedFieldBytesAt(f *ir.Field, depth int) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * fixedElementBytesAt(f, depth)
	case f.Array == ir.ArrayFixed:
		payload = f.ArrayBound * fixedElementBytesAt(f, depth)
	case f.Array == ir.ArrayCounted:
		payload = fixedCountBytes + f.ArrayBound*fixedElementBytesAt(f, depth)
	case f.Type.Kind == ir.TString:
		payload = fixedCountBytes + f.Type.Size
	case f.Type.Kind == ir.TBytes:
		payload = fixedCountBytes + f.Type.Size
	default:
		payload = fixedElementBytesAt(f, depth)
	}
	if f.Type.Optional {
		return fixedPresentBytes + payload
	}
	return payload
}

func fixedElementBytesAt(f *ir.Field, depth int) int64 {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedTypeBytesAt(r, depth+1)
		case *ir.Union:
			widest := int64(0)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if n := fixedFieldBytesAt(v.F, depth+1); n > widest {
					widest = n
				}
			}
			return int64(ir.StorageBitsFor(r.Max)/8) + widest
		}
	}
	return fixedStorageBytes(f.Type)
}

// fixedRefusal names a construct the fixed form (docs/SPEC-TABLES.md §3.4)
// refuses, as `Table.field: construct (cite)` (mirrors jstable/fixedjs.go's
// fixedRefusal, the JS leg's own boundary list).
func fixedRefusal(st *ir.Struct) string {
	for _, f := range st.Fields {
		name := st.Name + "." + f.Name
		if r := fixedFieldRefusal(st, f); r != "" {
			return name + r
		}
	}
	return ""
}

func fixedFieldRefusal(st *ir.Struct, f *ir.Field) string {
	switch {
	case f.IsMap() || f.Type.Kind == ir.TMap:
		return ": a map (docs/SPEC-TABLES.md §2.8) is not fixed"
	case f.Type.Pointer && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes:
		return ": a pointer (docs/SPEC-TABLES.md §2.1) is not fixed"
	case f.Array == ir.ArrayList:
		return ": an unbounded array (docs/SPEC-TABLES.md §2.9) is not fixed"

	}
	return ""
}

// fixedFinite refuses a closure whose by-value walk never closes. The layout
// cannot express a cycle (docs/FIXED-FORM-ALGORITHM.md §1), so a root whose
// struct or union references itself keeps form 1 instead of overflowing the
// constant-size walk.
func fixedFinite(st *ir.Struct) bool {
	seen := map[string]bool{}
	unions := map[string]bool{}
	var walkStruct func(*ir.Struct) bool
	var walkField func(*ir.Field) bool
	walkStruct = func(s *ir.Struct) bool {
		if s == nil || seen[s.Name] {
			return s != nil && !seen[s.Name]
		}
		seen[s.Name] = true
		defer delete(seen, s.Name)
		for _, f := range s.Fields {
			if !walkField(f) {
				return false
			}
		}
		return true
	}
	walkField = func(f *ir.Field) bool {
		if f == nil {
			return true
		}
		if f.Type.Pointer || f.IsMap() || f.IsList() || f.Type.Blob() {
			return false
		}
		if f.Type.Kind == ir.TNamed {
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				return walkStruct(r)
			case *ir.Union:
				if unions[r.Name] {
					return false
				}
				unions[r.Name] = true
				defer delete(unions, r.Name)
				for _, v := range r.Variants {
					if v.F != nil && !walkField(v.F) {
						return false
					}
				}
			}
		}
		return true
	}
	return walkStruct(st)
}

// fixedRoots is every table of a unit the fixed form is emitted for.
func fixedRoots(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range ir.TableFixedFormRoots(u) {
		if fixedRefusal(st) != "" || !ir.TableFixedSupported(st, 0) || !fixedFinite(st) {
			continue
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// init registers the fixed-form pass: it emits the fixed form's header and a
// per-root call into the runtime, which reads and writes the bytes against the
// module's own descriptors.
func init() {
	RegisterPass(emitFixedForm)
}

// emitFixedForm is the fixed form's emission step (docs/PORT-LUA.md, THE CARD
// MAP): for every fixed root it emits a small __fixed table carrying the
// record's byte size and the runtime calls. The runtime (runtime/fixed.lua)
// builds the identity plan from the descriptors the shape walks.
func emitFixedForm(m *Module) error {
	roots := fixedRoots(m.Unit)
	if len(roots) == 0 {
		return nil
	}
	m.Printf("-- the fixed form (docs/SPEC-TABLES.md §3.4), byte %d: reads and writes\n", fixedFormByte)
	m.Printf("-- records against the descriptors above; the runtime is runtime/fixed.lua\n")
	m.Printf("local F = require(\"fixed\")\n\n")
	m.Printf("_M.Fixed = {}\n")
	m.Printf("_M.FixedFields = {}\n")
	m.Printf("_M.FixedEnums = {}\n")
	m.Printf("_M.FixedUnions = {}\n\n")
	for _, st := range roots {
		size := fixedTypeBytes(st)
		m.Printf("_M.Fixed[%q] = { size = %d, table = %q }\n", st.Name, size, st.Name)
	}
	lineage, _ := pendingLineage.Load(m.Unit)
	handed, _ := lineage.(map[string][]FixedLineageEntry)
	// THE LAYOUT AND ITS HASH ride in the header (docs/FIXED-FORM-ALGORITHM.md
	// §1, §2): the same walk the C++ reference takes, so the bytes this leg
	// writes and the header it names are the oracle's own.
	for _, st := range roots {
		entries := ir.TableFixedWalkRoot(st)
		layout := ir.TableFixedLayoutBytes(entries)
		hash := ir.TableFixedLayoutHash(layout, st)
		m.Printf("_M.Fixed[%q].layout = %s\n", st.Name, lua.ByteString(layout))
		m.Printf("_M.Fixed[%q].hash = %s\n", st.Name, idLit(hash))
		// THE KNOWN LAYOUTS (docs/FIXED-FORM-ALGORITHM.md §5.2): one entry per
		// locked layout, OLDEST FIRST, the current one LAST. A file is matched
		// on the hash and read under THE LOCK'S layout bytes; its own are
		// compared and never walked. A RETIRED entry stays here forever — the
		// floor cuts it.
		known, floor := fixedLineageFor(handed[st.Name], FixedLineageEntry{
			Wire: hash, Layout: layout, Record: fixedHashBytes + fixedTypeBytes(st),
		})
		m.Printf("_M.Fixed[%q].known = {\n", st.Name)
		for _, e := range known {
			if e.Retired {
				m.Printf("  -- RETIRED: %s\n", e.Reason)
			}
			ranges := fixedWriterRanges(st, e, hash)
			if len(e.Layout) > 1024 {
				m.Printf("  {\n    hash = %s,\n    layout = table.concat({\n", idLit(e.Wire))
				for i := 0; i < len(e.Layout); i += 20 {
					end := min(i+20, len(e.Layout))
					m.Printf("      %s,\n", lua.ByteString(e.Layout[i:end]))
				}
				m.Printf("    }),\n    record = %d%s,\n  },\n", e.Record, ranges)
			} else {
				m.Printf("  { hash = %s, layout = %s, record = %d%s },\n", idLit(e.Wire), lua.ByteString(e.Layout), e.Record, ranges)
			}
		}
		m.Printf("}\n")
		m.Printf("_M.Fixed[%q].floor = %d\n", st.Name, floor)
	}
	emitFixedTables(m, roots)
	// THE PLAIN NAME IS THE STRICT READ (docs/SPEC-TABLES.md §6.5):
	// ReadFixed passes the strict decision into runtime/fixed.lua.
	m.Printf("\nfunction _M.ReadFixed(name, buf)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.read(_M, f.table, buf, true)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.WriteFixed(name, value)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.write(_M, f.table, value)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.FixedMeasure(name, count)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.measure(_M, f.table, count)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.FixedSave(name, values)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.save(_M, f.table, values)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.FixedLoad(name, buf, cap)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.load(_M, f.table, buf, true, cap)\n")
	m.Printf("end\n\n")
	return nil
}

// emitFixedTables lays the per-table field sizes and the enum/union widths the
// runtime walks (docs/FIXED-FORM-ALGORITHM.md §3: every field rides at its
// declared storage width).
func emitFixedTables(m *Module, roots []*ir.Struct) {
	m.Printf("\n-- fixed storage widths (docs/FIXED-FORM-ALGORITHM.md §3)\n")
	emittedStruct := map[string]bool{}
	emitStructWidths := func(st *ir.Struct) {
		if st == nil || st.IsMapEntry() || emittedStruct[st.Name] {
			return
		}
		if fixedRefusal(st) != "" || !ir.TableFixedSupported(st, 0) || !fixedFinite(st) {
			return
		}
		emittedStruct[st.Name] = true
		m.Printf("_M.Tables[%q].fixed_size = %d\n", st.Name, fixedTypeBytesAt(st, 0))
		m.Printf("_M.FixedFields[%q] = {", st.Name)
		for i, f := range st.Fields {
			if i > 0 {
				m.Printf(",")
			}
			m.Printf(" { size = %d, elem = %d }", fixedFieldBytesAt(f, 0), fixedElementBytesAt(f, 0))
		}
		m.Printf(" }\n")
	}
	for _, st := range roots {
		emitStructWidths(st)
	}
	seen := map[string]bool{}
	var fixedUnions []*ir.Union
	var visitField func(f *ir.Field)
	var visitType func(t ir.FieldType)
	visitField = func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.KeyEnumRef != nil && !seen["E:"+f.KeyEnumRef.Name] {
			seen["E:"+f.KeyEnumRef.Name] = true
			m.Printf("_M.FixedEnums[%q] = %d\n", f.KeyEnumRef.Name, f.KeyEnumRef.StorageBits/8)
		}
		visitType(f.Type)
	}
	visitType = func(t ir.FieldType) {
		if t.Kind == ir.TNamed {
			switch r := t.Ref.(type) {
			case *ir.Enum:
				if !seen["E:"+r.Name] {
					seen["E:"+r.Name] = true
					m.Printf("_M.FixedEnums[%q] = %d\n", r.Name, r.StorageBits/8)
				}
			case *ir.Struct:
				if !seen["S:"+r.Name] {
					seen["S:"+r.Name] = true
					emitStructWidths(r)
					for _, f := range r.Fields {
						visitField(f)
					}
				}
			case *ir.Union:
				if !seen["U:"+r.Name] {
					seen["U:"+r.Name] = true
					fixedUnions = append(fixedUnions, r)
					for _, v := range r.Variants {
						if v.F != nil {
							visitField(v.F)
						}
					}
				}
			}
		}
	}
	for _, st := range roots {
		seen["S:"+st.Name] = true
		for _, f := range st.Fields {
			visitField(f)
		}
	}
	for _, un := range fixedUnions {
		slots := armSlots(un)
		m.Printf("_M.FixedUnions[%q] = { tag = %d, arms = {", un.Name, ir.StorageBitsFor(un.Max)/8)
		for i, v := range un.Variants {
			if i > 0 {
				m.Printf(",")
			}
			var sz, el int64
			if v.F != nil {
				sz = fixedFieldBytesAt(v.F, 0)
				el = fixedElementBytesAt(v.F, 0)
			}
			m.Printf(" { slot = %q, size = %d, elem = %d }", slots[i], sz, el)
		}
		m.Printf(" } }\n")
	}
}

var _ = fmt.Sprintf
