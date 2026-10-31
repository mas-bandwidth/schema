// The two ACCELERATORS' shared emit (docs/SPEC-TABLES.md §7, §19): the block
// form (block_form.go) and the cook (cook_form.go) both point at bytes another
// build wrote and read records where they lie, through one record layout
// model (ir.RecordLayout, ir.Blocks) and one descriptor shape
// (schema_rt::AccelRecord in runtime/block_form.goose). This file is what they
// share: the descriptor literal of one record, the Goose spelling of one slot
// and the typed accessors that read and write one field of a record at an
// offset into a byte view.
package goosetable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// accelModule is the runtime module both accelerators import.
const accelModule = "block_form"

// sideModuleRefusal refuses a schema file whose module would collide with a
// side module the target writes for another file, folding case
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT): a file X beside a file XBlock.
func sideModuleRefusal(u *ir.Unit, suffix string) error {
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[strings.ToLower(f.Base)] = true
	}
	for _, f := range u.Files {
		if bases[strings.ToLower(f.Base+suffix)] {
			return fmt.Errorf("schema files %s and %s%s collide — the Goose target writes %s%s.goose beside %s's modules; rename one file", f.Base, f.Base, suffix, f.Base, suffix, f.Base)
		}
	}
	return nil
}

// accelRef is the record a field names by value — a nested record, or every
// slot of an inline array of them — or nil.
func accelRef(f *ir.Field) *ir.Struct {
	if f.Type.Kind != ir.TNamed || f.Type.Pointer {
		return nil
	}
	st, _ := f.Type.Ref.(*ir.Struct)
	return st
}

// accelPointee is the record a `*T` field points at, or nil.
func accelPointee(f *ir.Field) *ir.Struct {
	if f.Type.Kind != ir.TNamed || !f.Type.Pointer {
		return nil
	}
	st, _ := f.Type.Ref.(*ir.Struct)
	return st
}

func accelIsUnion(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Union)
	return ok
}

// accelField is one AccelField literal. index resolves a record name to its
// position in the unit's record array (-1 for none).
type accelField struct {
	name                                             string
	offset, size                                     int64
	kind                                             int
	elemSize, arrayBound, countOffset, presentOffset int64
	isArray, counted, optional, text, bytes, pointer bool
	union, outOfLine                                 bool
	record, offsetOfOffset, strideOffset, stride     int64
}

func (a accelField) render() string {
	return fmt.Sprintf("%s::AccelField { name: %q, offset: %d, size: %d, kind: %d, elem_size: %d, is_array: %t, array_bound: %d, counted: %t, count_offset: %d, optional: %t, present_offset: %d, is_text: %t, is_bytes: %t, is_pointer: %t, is_union: %t, record: %d, out_of_line: %t, offset_of_offset: %d, stride_offset: %d, stride: %d }",
		goose.RuntimeNamespace, a.name, a.offset, a.size, a.kind, a.elemSize, a.isArray, a.arrayBound, a.counted, a.countOffset,
		a.optional, a.presentOffset, a.text, a.bytes, a.pointer, a.union, a.record, a.outOfLine, a.offsetOfOffset, a.strideOffset, a.stride)
}

// accelFieldOf is a field of a record (projection false) or of a block's
// projection that stays inline, from the shared layout model.
func accelFieldOf(u *ir.Unit, fl ir.FieldLayout, projection bool, index func(*ir.Struct) int64) accelField {
	f := fl.Field
	facts := ir.BlockFieldOf(u, f, fl.Offset, projection)
	a := accelField{
		name: f.Name, offset: fl.Offset, size: fl.Size, kind: ir.TableScalarKind(f),
		elemSize: facts.ElemSize, arrayBound: facts.ArrayBound, countOffset: facts.CountOffset,
		presentOffset: facts.PresentOffset, isArray: facts.IsArray, counted: facts.Counted,
		optional: facts.Optional, text: f.Type.Kind == ir.TString, bytes: f.Type.Kind == ir.TBytes,
		pointer: f.Type.Pointer, union: accelIsUnion(f), record: -1, offsetOfOffset: -1, strideOffset: -1,
	}
	if a.elemSize == 0 {
		a.elemSize = fl.Size
	}
	if st := accelRef(f); st != nil {
		a.record = index(st)
	}
	if st := accelPointee(f); st != nil {
		a.record = index(st)
	}
	return a
}

// accelSlot is the Goose spelling of one scalar slot: its type, and how a
// value of it is read from and written to a byte view. ok is false for a slot
// no scalar accessor spells (a record, a union, a pointer, text).
type accelSlot struct {
	typ, get, put string // get/put take %[1]s = view, %[2]s = offset, put %[3]s = value
}

func accelSlotOf(f *ir.Field, width int64) (accelSlot, bool) {
	rt := goose.RuntimeNamespace
	kind := ir.TableScalarKind(f)
	switch {
	case f.Type.Pointer, accelIsUnion(f), accelRef(f) != nil, f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return accelSlot{}, false
	case kind == ir.TableKindBool:
		return accelSlot{"bool", rt + "::accel_u8(%[1]s, %[2]s) != 0", rt + "::accel_put(%[1]s, %[2]s, if %[3]s { 1 } else { 0 }, 1)"}, true
	case kind == ir.TableKindF32:
		return accelSlot{"f32", rt + "::accel_bits_f32(" + rt + "::accel_u32(%[1]s, %[2]s))", rt + "::accel_put(%[1]s, %[2]s, " + rt + "::accel_f32_bits(%[3]s), 4)"}, true
	case kind == ir.TableKindF64:
		return accelSlot{"f64", rt + "::accel_bits_f64(" + rt + "::accel_u64(%[1]s, %[2]s))", rt + "::accel_put(%[1]s, %[2]s, " + rt + "::accel_f64_bits(%[3]s), 8)"}, true
	case width == 16:
		return accelSlot{rt + "::Int128", rt + "::Int128 { lo: " + rt + "::accel_u64(%[1]s, %[2]s), hi: " + rt + "::accel_u64(%[1]s, %[2]s + 8) }",
			rt + "::accel_put(%[1]s, %[2]s, %[3]s.lo, 8); " + rt + "::accel_put(%[1]s, %[2]s + 8, %[3]s.hi, 8)"}, true
	case width == 1 || width == 2 || width == 4 || width == 8:
		bits := width * 8
		if accelSigned(kind) {
			typ := fmt.Sprintf("i%d", bits)
			get := fmt.Sprintf("%s::accel_get_signed(%%[1]s, %%[2]s, %d)", rt, width)
			if bits != 64 {
				get += " as! " + typ
			}
			return accelSlot{typ, get, fmt.Sprintf("%s::accel_put(%%[1]s, %%[2]s, %%[3]s as! u64, %d)", rt, width)}, true
		}
		typ := fmt.Sprintf("u%d", bits)
		get := fmt.Sprintf("%s::accel_get(%%[1]s, %%[2]s, %d)", rt, width)
		put := fmt.Sprintf("%s::accel_put(%%[1]s, %%[2]s, %%[3]s, %d)", rt, width)
		if bits != 64 {
			get += " as! " + typ
		}
		return accelSlot{typ, get, put}, true
	}
	return accelSlot{}, false
}

// accelSigned is the reference dump's signedness rule: the four signed
// integer kinds, and the signed fixed-point and 128-bit kinds and the
// timestamp beside them, read sign-extended.
func accelSigned(kind int) bool {
	switch kind {
	case ir.TableKindI8, ir.TableKindI16, ir.TableKindI32, ir.TableKindI64,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64,
		ir.TableKindTimestamp:
		return true
	}
	return false
}

// emitAccessors writes the typed accessors of one record's fields at an
// offset: prefix names them (<Row>Row for a row, <Table>Block for a
// projection, whose offset is zero and whose accessors take no `at`). A
// nested record answers its OFFSET (<prefix><F>At), so a consumer descends by
// one add and reads the nested record's own accessors there; a string or a
// `bytes` answers a VIEW of its used bytes, nothing copied (docs/SPEC-TABLES.md
// §19.2). Every write goes through a mutable view, the producer's (§19.1).
func emitAccessors(g *goose.Gen, u *ir.Unit, prefix string, fields []ir.FieldLayout, projection bool) {
	rt := goose.RuntimeNamespace
	atParam, atArg := ", at: i64", "at + "
	if projection {
		atParam, atArg = "", ""
	}
	decl := func(name string) string {
		g.Declare(name)
		return name
	}
	for _, fl := range fields {
		f := fl.Field
		if projection && ir.BlockOutOfLine(f) {
			continue
		}
		facts := ir.BlockFieldOf(u, f, fl.Offset, projection)
		member := prefix + ir.GoExportName(f.Name)
		elem := facts.ElemSize
		if elem == 0 {
			elem = fl.Size
		}
		indexed := facts.IsArray && f.Type.Kind != ir.TBytes
		idxParam, idxTerm := "", ""
		if indexed {
			idxParam, idxTerm = ", i: i64", fmt.Sprintf(" + i * %d", elem)
		}
		at := fmt.Sprintf("%s%d%s", atArg, fl.Offset, idxTerm)
		switch {
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			g.Pf("fn %s(b: const u8[:]%s) -> const u8[:] { return %s::accel_text(b, %s%d, %s%d, %d); }\n",
				decl(member), atParam, rt, atArg, fl.Offset, atArg, facts.CountOffset, facts.ArrayBound)
			g.Pf("fn %s(b: u8[:]%s, v: const u8[:]) { %s::accel_put_text(b, %s%d, %s%d, %d, v); }\n",
				decl(prefix+"Set"+ir.GoExportName(f.Name)), atParam, rt, atArg, fl.Offset, atArg, facts.CountOffset, facts.ArrayBound)
		case f.Type.Pointer && f.Type.Kind == ir.TNamed:
			// a cooked `*T`: the target's offset in the region, -1 for null or
			// a delta that leaves it (runtime/cook_form.goose, cook_deref)
			g.Import(cookModule)
			g.Pf("fn %s(b: const u8[:]%s%s) -> i64 { return %s::cook_deref(b, %s); }\n", decl(member), atParam, idxParam, rt, at)
		case accelRef(f) != nil:
			g.Pf("fn %s(%s%s) -> i64 { return %s; }\n", decl(member+"At"), strings.TrimPrefix(atParam, ", "), idxParamFor(atParam, idxParam), atOrZero(at))
		default:
			slot, ok := accelSlotOf(f, elem)
			if !ok {
				g.Pf("// %s: a %s has no scalar accessor; the descriptors read it\n", f.Name, describeSlot(f))
				break
			}
			g.Pf("fn %s(b: const u8[:]%s%s) -> %s { return %s; }\n", decl(member), atParam, idxParam, slot.typ, fmt.Sprintf(slot.get, "b", at))
			g.Pf("fn %s(b: u8[:]%s%s, v: %s) { %s; }\n", decl(prefix+"Set"+ir.GoExportName(f.Name)), atParam, idxParam, slot.typ, fmt.Sprintf(slot.put, "b", at, "v"))
		}
		if facts.Counted && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
			g.Pf("fn %s(b: const u8[:]%s) -> i32 { return %s::accel_get_signed(b, %s%d, 4) as! i32; }\n", decl(member+"Count"), atParam, rt, atArg, facts.CountOffset)
			g.Pf("fn %s(b: u8[:]%s, n: i32) { %s::accel_put(b, %s%d, n as! u64, 4); }\n", decl(prefix+"Set"+ir.GoExportName(f.Name)+"Count"), atParam, rt, atArg, facts.CountOffset)
		}
		if facts.Optional {
			g.Pf("fn %s(b: const u8[:]%s) -> bool { return %s::accel_u8(b, %s%d) != 0; }\n", decl(member+"Present"), atParam, rt, atArg, facts.PresentOffset)
			g.Pf("fn %s(b: u8[:]%s, v: bool) { %s::accel_put(b, %s%d, if v { 1 } else { 0 }, 1); }\n", decl(prefix+"Set"+ir.GoExportName(f.Name)+"Present"), atParam, rt, atArg, facts.PresentOffset)
		}
	}
}

func idxParamFor(atParam, idxParam string) string {
	if atParam == "" {
		return strings.TrimPrefix(idxParam, ", ")
	}
	return idxParam
}

func atOrZero(at string) string {
	if at == "" {
		return "0"
	}
	return at
}

func describeSlot(f *ir.Field) string {
	switch {
	case f.Type.Pointer:
		return "pointer"
	case accelIsUnion(f):
		return "union"
	}
	return "slot"
}
