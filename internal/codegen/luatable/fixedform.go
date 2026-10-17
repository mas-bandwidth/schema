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

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedKindOptional is the ONE kind §3.4's block adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose size is one present byte plus the child's.
const fixedKindOptional = 35

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
	case f.Type.Kind == ir.TWString:
		payload = fixedCountBytes + 2*f.Type.Size
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
	case f.Type.Kind == ir.TFixed:
		return ": a fixed/ufixed (docs/SPEC-TABLES.md §15) is not fixed"
	case f.Type.Kind == ir.TInt && f.Type.Width == 128:
		return ": an int128 (docs/SPEC-TABLES.md §3) is not fixed"
	case f.Type.Kind == ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && v.F.Type.Kind == ir.TFixed {
					return ": a union arm holding a fixed/ufixed (docs/SPEC-TABLES.md §15) is not fixed"
				}
			}
		}
	}
	return ""
}

// fixedRoots is every table of a unit the fixed form is emitted for.
func fixedRoots(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range u.Tables {
		if !st.IsTable || st.IsMapEntry() || fixedRefusal(st) != "" {
			continue
		}
		out = append(out, st)
	}
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
	m.Printf("_M.Fixed = {}\n\n")
	for _, st := range roots {
		size := fixedTypeBytes(st)
		m.Printf("_M.Fixed[%q] = { size = %d, table = %q }\n", st.Name, size, st.Name)
	}
	m.Printf("\nfunction _M.ReadFixed(name, buf)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.read(_M, f.table, buf)\n")
	m.Printf("end\n\n")
	m.Printf("function _M.WriteFixed(name, value)\n")
	m.Printf("  local f = _M.Fixed[name]\n")
	m.Printf("  return F.write(_M, f.table, value)\n")
	m.Printf("end\n\n")
	return nil
}

var _ = fmt.Sprintf
