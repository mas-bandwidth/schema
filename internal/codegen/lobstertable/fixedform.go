// The fixed form, form byte 3: the Lobster writer and reader
// (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md; the per-table
// codegen here and runtime/fixed.lobster). A record is the writer's layout
// hash and then the body in declared order, every field at its DECLARED
// storage width (docs/FIXED-FORM-ALGORITHM.md §3). The layout and its hash
// are the compiler's own IR walk (ir.TableFixedWalkRoot,
// ir.TableFixedLayoutHash), never re-derived here.
package lobstertable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("fixed-form", emitFixedForm) }

// emitFixedForm joins every table module with the form-3 surface of each
// fixed table it declares (docs/PORT-LOBSTER.md, THE CARD MAP): a layout
// constant, a hash constant, a per-table body writer, a per-table body
// reader, and the file save/load pair the byte-oracle test drives.
func emitFixedForm(g *lobster.Gen) error {
	emitted := false
	candidates := append([]*ir.Struct{}, g.File.Tables...)
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			candidates = append(candidates, st)
		}
	}
	seen := map[string]bool{}
	for _, st := range candidates {
		if seen[st.Name] || st.IsMapEntry() || !ir.TableFixedSupported(st, 0) {
			continue
		}
		if ir.TableFixedTypeBytes(st) > ir.TableFixedRecordMaxBytes || !ir.TableFixedWithinDepth(st) {
			continue
		}
		if st.IsTable && (g.Unit.TableUnions[st.Name] != nil || ir.VariableTables(g.Unit)[st.Name]) {
			continue
		}
		if !fixedLobsterSimple(st, 0) {
			continue
		}
		seen[st.Name] = true
		emitFixedTable(g, st)
		emitted = true
	}
	if emitted {
		g.Import("fixed")
	}
	return nil
}

func fixedName(st *ir.Struct) string { return st.Name + "Fixed" }

func emitFixedTable(g *lobster.Gen, st *ir.Struct) {
	name := fixedName(st)
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	size := ir.TableFixedTypeBytes(st)

	g.Pf("// %sLayoutBytes is the writer's layout, exactly as it rides (%s,\n", name, st.Name)
	g.Pf("// docs/FIXED-FORM-ALGORITHM.md §1): a u32 entry count then 17-byte entries.\n")
	g.Pf("def %sLayoutBytes() -> string:\n", name)
	g.Pf("    var b = schema_rt.zeros(%d)\n", len(layout))
	for i, v := range layout {
		g.Pf("    b = write_int8_le(b, %d, %d)\n", i, int(v))
	}
	g.Pf("    return b\n\n")
	g.Pf("// %sHash is fnv1a64 over the layout above (docs/FIXED-FORM-ALGORITHM.md §1).\n", name)
	g.Pf("let %sHash = %s\n", name, lobster.IntLit(new(big.Int).SetUint64(hash)))
	g.Pf("let %sRecordBytes = %d\n\n", name, size)

	g.Pf("// %sPut writes one record body at at, in declared order and at each\n", name)
	g.Pf("// field's declared storage width (docs/FIXED-FORM-ALGORITHM.md §3).\n")
	g.Pf("def %sPut(b:string, at:int, v:%s) -> string:\n", name, st.Name)
	putFields(g, st, "v", "at")
	g.Pf("    return b\n\n")

	g.Pf("// %sGet reads one record body at at (docs/FIXED-FORM-ALGORITHM.md §3).\n", name)
	g.Pf("def %sGet(data:string, at:int) -> %s:\n", name, st.Name)
	g.Pf("    let v = New%s()\n", st.Name)
	getFields(g, st, "v", "at")
	g.Pf("    return v\n\n")

	g.Pf("// %sSave writes values as a form-3 file: the header (%s, §2) once,\n", name, st.Name)
	g.Pf("// then one hash-and-body record per value. A NaN or an infinity in any\n")
	g.Pf("// float refuses the batch: the answer is \"\" (docs/SPEC-TABLES.md §3.3).\n")
	g.Pf("def %sSave(values:[%s]) -> string:\n", name, st.Name)
	if fixedFiniteNeededStruct(st, map[string]bool{}) {
		g.Pf("    for(values) v:\n")
		g.Pf("        if %sFixedFiniteBody(v) != \"\":\n", st.Name)
		g.Pf("            return \"\"\n")
	}
	g.Pf("    var body = \"\"\n")
	g.Pf("    for(values) v: body += schema_rt.fixed_record(%sHash, %sPut(schema_rt.zeros(%d), 0, v))\n", name, name, size)
	g.Pf("    return schema_rt.fixed_file(%sLayoutBytes(), %sHash, body, length(values))\n\n", name, name)

	g.Pf("// %sDecode reads a TRUSTED form-3 file of %s records written by this\n", name, st.Name)
	g.Pf("// build's own layout (%s, §2), asserting the header and every record\n", st.Name)
	g.Pf("// hash. It is not a read of untrusted data: %sLoad is the strict\n", name)
	g.Pf("// read and %sLoadTolerant the tolerant one (docs/SPEC-TABLES.md §6.5).\n", name)
	g.Pf("def %sDecode(data:string) -> [%s]:\n", name, st.Name)
	g.Pf("    let layout = %sLayoutBytes()\n", name)
	g.Pf("    assert read_uint8_le(data, 0) == 3\n")
	g.Pf("    assert read_uint64_le(data, 8) == %sHash\n", name)
	g.Pf("    assert read_uint32_le(data, 16) == length(layout)\n")
	g.Pf("    let start = 20 + length(layout)\n")
	g.Pf("    let stride = 8 + %d\n", size)
	g.Pf("    let out:[%s] = []\n", st.Name)
	g.Pf("    var at = start\n")
	g.Pf("    while at < length(data):\n")
	g.Pf("        assert read_uint64_le(data, at) == %sHash\n", name)
	g.Pf("        push(out, %sGet(data, at + 8))\n", name)
	g.Pf("        at += stride\n")
	g.Pf("    return out\n\n")
}

// slotExpr is a companion slot's expression beside a value expression: the
// count, length or presence companion of a field (docs/PORT-LOBSTER.md, THE
// TYPE MAPPING), whichever nesting the value sits at.
func slotExpr(expr string, f *ir.Field, suffix string) string {
	return slotExprFull(expr, f, ir.GoExportName(f.Name)+suffix)
}

// slotExprFull is [slotExpr] for a companion whose Lobster spelling is not the
// field's spelling plus a suffix (an optional's Present).
func slotExprFull(expr string, f *ir.Field, slot string) string {
	name := ir.GoExportName(f.Name)
	if i := strings.LastIndex(expr, name); i >= 0 {
		return expr[:i] + slot + expr[i+len(name):]
	}
	return slot
}

// fixedLobsterSimple reports the closure this emitter spells today: text
// elements of a vector are immutable in Lobster, so a table carrying an array
// of string/bytes is not given a fixed surface here rather than emitting code
// that does not compile. Nested tables and union arms are walked.
func fixedLobsterSimple(st *ir.Struct, depth int) bool {
	if depth > 64 {
		return false
	}
	var fieldOK func(f *ir.Field, d int) bool
	fieldOK = func(f *ir.Field, d int) bool {
		if f.Type.Optional && f.Array != ir.ArrayNone {
			return false // an optional array is a later card's shape
		}
		if f.Array != ir.ArrayNone || f.KeyEnumRef != nil {
			if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
				return false
			}
			if f.Type.Kind == ir.TInt && !f.Type.Signed && f.Type.Width == 8 {
				return false // [N]uint8 stores as a byte string in Lobster
			}
		}
		if f.Type.Kind != ir.TNamed {
			return true
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedLobsterSimple(r, d+1)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && !fieldOK(v.F, d+1) {
					return false
				}
			}
		}
		return true
	}
	for _, f := range st.Fields {
		if !fieldOK(f, depth) {
			return false
		}
	}
	return true
}

// loopVar is a loop variable unique to a field and offset, so nested loops
// never shadow one another in the generated function.
func loopVar(f *ir.Field, off int64) string {
	return fmt.Sprintf("i_%s_%d", ir.GoExportName(f.Name), off)
}

// putFields emits the stores for a type's fields at the constant offsets.
func putFields(g *lobster.Gen, st *ir.Struct, val, base string) {
	off := int64(0)
	for _, f := range st.Fields {
		putField(g, st, f, val+"."+ir.GoExportName(f.Name), base, off, "", "    ")
		off += ir.TableFixedFieldBytes(f)
	}
}

// putField stores one field. stem is an array element's lvalue suffix and
// aux the length/count slot names beside it ("" for a plain field).
func putField(g *lobster.Gen, owner *ir.Struct, f *ir.Field, expr, base string, off int64, aux, ind string) {
	if f.Type.Optional {
		present := slotExprFull(expr, f, ir.GoExportName(f.Name+"_present"))
		if aux != "" {
			present = aux
		}
		g.Pf("%sb = write_int8_le(b, %s + %d, if %s: 1 else: 0)\n", ind, base, off, present)
		g.Pf("%sif %s:\n", ind, present)
		putPayload(g, owner, f, expr, base, off+ir.TableFixedPresentBytes, aux, ind+"    ")
		return
	}
	putPayload(g, owner, f, expr, base, off, aux, ind)
}

func putPayload(g *lobster.Gen, owner *ir.Struct, f *ir.Field, expr, base string, off int64, aux, ind string) {
	switch {
	case f.KeyEnumRef != nil:
		n := f.KeyEnumRef.Max
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%d) %s:\n", ind, n, iv)
		putElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off, elem, iv, ind+"    ")
		return
	case f.Array == ir.ArrayFixed:
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%d) %s:\n", ind, f.ArrayBound, iv)
		putElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off, elem, iv, ind+"    ")
		return
	case f.Array == ir.ArrayCounted:
		count := slotExpr(expr, f, "Count")
		if aux != "" {
			count = aux
		}
		g.Pf("%sb = write_int32_le(b, %s + %d, %s)\n", ind, base, off, count)
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%s) %s:\n", ind, count, iv)
		putElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off+ir.TableFixedCountBytes, elem, iv, ind+"    ")
		return
	}
	putElement(g, f, expr, base, off, 0, "", ind)
}

// putElement stores a scalar or a nested value at base+off (plus i*stride
// for an array).
func putElement(g *lobster.Gen, f *ir.Field, expr, base string, off, stride int64, idx, ind string) {
	at := fmt.Sprintf("%s + %d", base, off)
	if idx != "" {
		at = fmt.Sprintf("%s + %d + %s * %d", base, off, idx, stride)
	}
	t := f.Type
	switch t.Kind {
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			g.Pf("%sb = %sPut(b, %s, %s)\n", ind, fixedName(r), at, expr)
			return
		case *ir.Union:
			putUnion(g, r, expr, at, ind)
			return
		case *ir.Enum:
			w := r.StorageBits / 8
			g.Pf("%sb = %s(b, %s, int(%s))\n", ind, writeInt(w, false), at, expr)
			return
		case *ir.Flags:
			g.Pf("%sb = write_int64_le(b, %s, %s)\n", ind, at, expr)
			return
		}
	case ir.TBool:
		g.Pf("%sb = write_int8_le(b, %s, if %s: 1 else: 0)\n", ind, at, expr)
		return
	case ir.TFloat32:
		g.Pf("%sb = write_float32_le(b, %s, %s)\n", ind, at, expr)
		return
	case ir.TFloat64:
		g.Pf("%sb = write_float64_le(b, %s, %s)\n", ind, at, expr)
		return
	case ir.TString, ir.TBytes, ir.TWString:
		unit := int64(1)
		if t.Kind == ir.TWString {
			unit = 2
		}
		length := slotExpr(expr, f, "Length")
		g.Pf("%sb = write_int32_le(b, %s, %s)\n", ind, at, length)
		g.Pf("%sfor(%s * %d) j_%s_%d: b = write_int8_le(b, %s + 4 + j_%s_%d, read_uint8_le(%s, j_%s_%d))\n", ind, length, unit, ir.GoExportName(f.Name), off, at, ir.GoExportName(f.Name), off, expr, ir.GoExportName(f.Name), off)
		return
	case ir.TInt, ir.TFixed, ir.TBits:
		if t.Kind == ir.TBits {
			w := 8
			if t.Width <= 32 {
				w = 4
			}
			g.Pf("%sb = %s(b, %s, %s)\n", ind, writeInt(w, false), at, expr)
			return
		}
		if t.Width > 64 {
			g.Pf("%sb = write_int64_le(b, %s, %s.lo)\n", ind, at, expr)
			g.Pf("%sb = write_int64_le(b, %s + 8, %s.hi)\n", ind, at, expr)
			return
		}
		g.Pf("%sb = %s(b, %s, %s)\n", ind, writeInt(t.Width/8, t.Signed), at, expr)
		return
	}
}

func putUnion(g *lobster.Gen, un *ir.Union, expr, at, ind string) {
	tag := ir.TableFixedUnionTagBytes(un)
	g.Pf("%sb = %s(b, %s, int(%s.Type))\n", ind, writeInt(int(tag), false), at, expr)
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		g.Pf("%sif int(%s.Type) == %d:\n", ind, expr, i+1)
		f := *v.F
		f.Name = v.Name
		putPayload(g, nil, &f, expr+"."+ir.GoExportName(v.Name), at+fmt.Sprintf(" + %d", tag), 0, "", ind+"    ")
	}
}

func getFields(g *lobster.Gen, st *ir.Struct, val, base string) {
	off := int64(0)
	for _, f := range st.Fields {
		getField(g, st, f, val+"."+ir.GoExportName(f.Name), base, off, "    ")
		off += ir.TableFixedFieldBytes(f)
	}
}

func getField(g *lobster.Gen, owner *ir.Struct, f *ir.Field, expr, base string, off int64, ind string) {
	if f.Type.Optional {
		present := slotExprFull(expr, f, ir.GoExportName(f.Name+"_present"))
		g.Pf("%s%s = read_uint8_le(data, %s + %d) != 0\n", ind, present, base, off)
		getPayload(g, owner, f, expr, base, off+ir.TableFixedPresentBytes, present, ind)
		return
	}
	getPayload(g, owner, f, expr, base, off, "", ind)
}

func getPayload(g *lobster.Gen, owner *ir.Struct, f *ir.Field, expr, base string, off int64, present, ind string) {
	guard := ""
	if present != "" {
		guard = ind + "if " + present + ":\n"
	}
	switch {
	case f.KeyEnumRef != nil:
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%d) %s:\n", ind, f.KeyEnumRef.Max, iv)
		getElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off, elem, iv, ind+"    ")
		return
	case f.Array == ir.ArrayFixed:
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%d) %s:\n", ind, f.ArrayBound, iv)
		getElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off, elem, iv, ind+"    ")
		return
	case f.Array == ir.ArrayCounted:
		count := slotExpr(expr, f, "Count")
		g.Pf("%s%s = read_int32_le(data, %s + %d)\n", ind, count, base, off)
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%s) %s:\n", ind, count, iv)
		getElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off+ir.TableFixedCountBytes, elem, iv, ind+"    ")
		return
	}
	if guard != "" {
		g.Pf("%s", guard)
		getElementIndented(g, f, expr, base, off, 0, "", ind+"    ")
		return
	}
	getElement(g, f, expr, base, off, 0, "", ind)
}

func getElement(g *lobster.Gen, f *ir.Field, expr, base string, off, stride int64, idx, ind string) {
	getElementIndented(g, f, expr, base, off, stride, idx, ind)
}

func getElementIndented(g *lobster.Gen, f *ir.Field, expr, base string, off, stride int64, idx, ind string) {
	at := fmt.Sprintf("%s + %d", base, off)
	if idx != "" {
		at = fmt.Sprintf("%s + %d + %s * %d", base, off, idx, stride)
	}
	t := f.Type
	switch t.Kind {
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			g.Pf("%s%s = %sGet(data, %s)\n", ind, expr, fixedName(r), at)
			return
		case *ir.Union:
			getUnion(g, r, expr, at, ind)
			return
		case *ir.Enum:
			w := r.StorageBits / 8
			g.Pf("%s%s = %s(%s(data, %s))\n", ind, expr, r.Name, readInt(w, false), at)
			return
		case *ir.Flags:
			g.Pf("%s%s = read_uint64_le(data, %s)\n", ind, expr, at)
			return
		}
	case ir.TBool:
		g.Pf("%s%s = read_uint8_le(data, %s) != 0\n", ind, expr, at)
		return
	case ir.TFloat32:
		g.Pf("%s%s = read_float32_le(data, %s)\n", ind, expr, at)
		return
	case ir.TFloat64:
		g.Pf("%s%s = read_float64_le(data, %s)\n", ind, expr, at)
		return
	case ir.TString, ir.TBytes, ir.TWString:
		unit := int64(1)
		if t.Kind == ir.TWString {
			unit = 2
		}
		length := slotExpr(expr, f, "Length")
		tmp := fmt.Sprintf("fbuf_%s_%d", ir.GoExportName(f.Name), off)
		g.Pf("%s%s = read_int32_le(data, %s)\n", ind, length, at)
		g.Pf("%svar %s = schema_rt.zeros(%d)\n", ind, tmp, unit*t.Size)
		g.Pf("%sfor(%s * %d) j_%s_%d: %s = write_int8_le(%s, j_%s_%d, read_uint8_le(data, %s + 4 + j_%s_%d))\n", ind, length, unit, ir.GoExportName(f.Name), off, tmp, tmp, ir.GoExportName(f.Name), off, at, ir.GoExportName(f.Name), off)
		g.Pf("%s%s = %s\n", ind, expr, tmp)
		return
	case ir.TInt, ir.TFixed, ir.TBits:
		if t.Kind == ir.TBits {
			w := 8
			if t.Width <= 32 {
				w = 4
			}
			g.Pf("%s%s = %s(data, %s)\n", ind, expr, readInt(w, false), at)
			return
		}
		if t.Width > 64 {
			g.Pf("%s%s = schema_rt.Int128 { lo: read_uint64_le(data, %s), hi: read_uint64_le(data, %s + 8) }\n", ind, expr, at, at)
			return
		}
		g.Pf("%s%s = %s(data, %s)\n", ind, expr, readInt(t.Width/8, t.Signed), at)
		return
	}
}

func getUnion(g *lobster.Gen, un *ir.Union, expr, at, ind string) {
	tag := ir.TableFixedUnionTagBytes(un)
	g.Pf("%s%s.Type = %sType(%s(data, %s))\n", ind, expr, un.Name, readInt(int(tag), false), at)
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		g.Pf("%sif int(%s.Type) == %d:\n", ind, expr, i+1)
		f := *v.F
		f.Name = v.Name
		getElementIndented(g, &f, expr+"."+ir.GoExportName(v.Name), at, tag, 0, "", ind+"    ")
	}
}

func writeInt(width int, signed bool) string {
	switch width {
	case 1:
		return "write_int8_le"
	case 2:
		return "write_int16_le"
	case 4:
		return "write_int32_le"
	case 8:
		return "write_int64_le"
	}
	return "write_int64_le"
}

func readInt(width int, signed bool) string {
	if signed {
		switch width {
		case 1:
			return "read_int8_le"
		case 2:
			return "read_int16_le"
		case 4:
			return "read_int32_le"
		case 8:
			return "read_int64_le"
		}
	}
	switch width {
	case 1:
		return "read_uint8_le"
	case 2:
		return "read_uint16_le"
	case 4:
		return "read_uint32_le"
	case 8:
		return "read_uint64_le"
	}
	return "read_int64_le"
}
