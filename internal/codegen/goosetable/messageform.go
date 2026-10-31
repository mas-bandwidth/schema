// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, for Goose — the
// schema-goose-message-form card of docs/PORT-GOOSE.md, THE CARD MAP. A batch
// is the form byte, the body count minus one in eight bits, then the bodies as
// ONE continuous bit stream, zero-padded to the next byte: a body is fields,
// each a REFERENCE (a slot of the unit's announced vocabulary, at
// bits_required(0, E) bits) then the payload the packet wire writes for that
// declaration, ending at the zero reference.
//
// THE WRITER'S SLOT NUMBERS ARE COMPILE-TIME CONSTANTS (§3.3): the vocabulary
// is a pure function of the build version, so every field header is a named
// constant at a named constant width and a save does no lookup. The numbers
// are the UNIT's (a file that joins renumbers them), so they live in the
// message form's package module, <package>_message.goose (docs/PORTING.md J2):
// MessageRefBits, one MessageSlot_<id>_<kind>_<key> per vocabulary entry,
// named for the entry and never for its place, and the announcement,
// MessageAnnouncement. Each file's module names them, so no byte of it moves
// when the unit gains a file; every use folds as a constant (Goose's named
// constants). `<Root>Announce` is the announcement under the root's name.
//
// THE READ HERE IS AGAINST THE UNIT'S OWN VOCABULARY — a peer at this build
// version, whose announcement is byte for byte `<Root>Announce` — and it
// dispatches every reference by the same compile-time slots. Resolving a
// PEER's announcement (AnnounceRead: a form-1 file read tolerantly, a
// vocabulary of another build, its shapes, the skip of an entry this reader
// cannot name) is owed to the id-table wire's card and lands stacked on it
// (docs/PORT-GOOSE.md, THE CARD MAP: schema-goose-table-wire-load); this file
// owes it nothing but the dispatch table it will fill.
//
// THE FORM IS A FIXED TABLE'S (§3.3): its roots are the unit's fixed roots,
// and every struct and union their closure reaches by value gets a body
// writer and a body reader, joined to the table surface through [Register].
package goosetable

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("messageform", emitMessageFormPart)
	RegisterUnit("messageform", "message", emitMessagePackage)
}

// msgRefBits is the package module's reference width.
const msgRefBits = "MessageRefBits"

// msgSlotName is an entry's slot constant: named for the entry's content (its
// id, kind and encoded key), so the name never depends on the entry's place.
func msgSlotName(entry ir.TableVocabularyEntry) string {
	h := fnv.New32a()
	h.Write([]byte(entry.Key()))
	return fmt.Sprintf("MessageSlot_%016x_%d_%08x", entry.Id, entry.Kind, h.Sum32())
}

// emitMessagePackage writes the unit's vocabulary into <package>_message.goose:
// the reference width, every entry's slot, and the announcement.
func emitMessagePackage(g *goose.Gen) error {
	structs, _ := fixedClosure(g.Unit)
	if len(structs) == 0 {
		return nil
	}
	vocabulary := ir.TableVocabulary(g.Unit)
	bits := ir.TableMessageRefBits(len(vocabulary))
	g.Pf("// THE UNIT'S MESSAGE VOCABULARY (docs/SPEC-TABLES.md §3.3): %d entries, %d-bit references.\n", len(vocabulary), bits)
	g.Declare(msgRefBits)
	g.Pf("let %s = %d;\n", msgRefBits, bits)
	for i, entry := range vocabulary {
		name := msgSlotName(entry)
		g.Declare(name)
		g.Pf("let %s = %d;\n", name, i+1)
	}
	g.Pf("// The announcement: the form-1 file carrying the build version and the vocabulary.\n")
	g.Declare("MessageAnnouncement")
	g.Pf("let MessageAnnouncement: const u8[:] = \"%s\";\n\n", fixedGooseBytes(ir.TableAnnouncement(g.Unit)))
	return nil
}

// msgEmit writes one module's message-form functions.
type msgEmit struct {
	g     *goose.Gen
	fx    *fixedEmit
	slots map[string]uint64
	loop  int
	err   error
}

func (e *msgEmit) p(ind int, format string, args ...any) {
	e.g.Pf(strings.Repeat("    ", ind)+format+"\n", args...)
}

func (e *msgEmit) loopVar() string {
	e.loop++
	return fmt.Sprintf("m%d", e.loop)
}

func (e *msgEmit) fail(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf(format, args...)
	}
}

// slot is an entry's reference, by name: its 1-based place in the unit's
// vocabulary is the package module's constant.
func (e *msgEmit) slot(entry ir.TableVocabularyEntry) string {
	if e.slots[entry.Key()] == 0 {
		e.fail("goose message form: the unit's vocabulary names no slot for id %016x at kind %d, and the vocabulary is the closure's whole entry set (docs/SPEC-TABLES.md §3.3)", entry.Id, entry.Kind)
	}
	return msgSlotName(entry)
}

func (e *msgEmit) nameSlot(id uint64) string { return e.slot(ir.TableVocabularyEntry{Id: id}) }

func (e *msgEmit) put(ind int, expr string, bits int64) {
	if bits <= 0 {
		return
	}
	e.p(ind, "schema_rt::msg_put(w, out, %s, %d);", expr, bits)
}

func (e *msgEmit) ref(ind int, slot string) {
	e.p(ind, "schema_rt::msg_put(w, out, %s, %s);", slot, msgRefBits)
}

// emitMessageFormPart is the [Register]ed part, over the same closure and the
// same module partition as the fixed form.
func emitMessageFormPart(g *goose.Gen) error {
	structs, unions := fixedClosure(g.Unit)
	if len(structs) == 0 {
		return nil
	}
	g.Import("message")
	g.Import(PackageModule(g.Unit, "message"))
	e := &msgEmit{g: g, fx: &fixedEmit{g: g}, slots: ir.TableVocabularySlots(g.Unit)}
	var mine []string
	for _, t := range g.File.Tables {
		if structs[t.Name] != nil {
			mine = append(mine, t.Name)
		}
	}
	for _, un := range g.File.TableUnions {
		if unions[un.Name] != nil {
			mine = append(mine, un.Name)
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if structs[d.Name] != nil && !d.IsTable {
				mine = append(mine, d.Name)
			}
		case *ir.Union:
			if unions[d.Name] != nil && g.Unit.TableUnions[d.Name] == nil {
				mine = append(mine, d.Name)
			}
		}
	}
	for _, name := range mine {
		if st := structs[name]; st != nil {
			e.emitStruct(st)
		} else {
			e.emitUnion(unions[name])
		}
	}
	roots := fixedRootsOf(g)
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for _, st := range roots {
		e.emitRoot(st)
	}
	return e.err
}

// ---------------------------------------------------------------------------
// THE WRITE
// ---------------------------------------------------------------------------

// u64Lit is a ranged base as the u64 the wire subtracts: two's complement
// for a negative base (docs/SPEC-TABLES.md §3.3: the offset is value - base,
// unsigned at the stored width).
func u64Lit(b *big.Int) string {
	if b == nil {
		return "0"
	}
	v := new(big.Int).Set(b)
	if v.Sign() < 0 {
		v.Add(v, new(big.Int).Lsh(big.NewInt(1), 64))
	}
	return v.String()
}

// asU64 is a stored integer as the u64 the bit writer takes: an unsigned
// storage converts implicitly, a signed one reinterprets its two's complement.
func (e *msgEmit) asU64(f *ir.Field, expr string) string {
	if f.Type.Kind == ir.TNamed || f.Type.Kind == ir.TBits || !f.Type.Signed {
		return expr
	}
	return "(" + expr + " as! u64)"
}

// lanes128 is a 128-bit base as its two u64 lanes, two's complement.
func lanes128(b *big.Int) (string, string) {
	v := new(big.Int)
	if b != nil {
		v.Set(b)
	}
	if v.Sign() < 0 {
		v.Add(v, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).SetUint64(math.MaxUint64)
	return new(big.Int).And(v, mask).String(), new(big.Int).Rsh(v, 64).String()
}

func f32Lit(v float32) string {
	return fmt.Sprintf("schema_rt::fixed_bits_f32(0x%08x)", math.Float32bits(v))
}

// putScalar writes ONE value at the width its declaration states (§3.3's
// payload table): a bool one bit, a float finite-only (quantized by the
// packet wire's rule when compressed), an integer raw at its storage width or
// ranged as value - base, an enum as the reference naming its variant.
func (e *msgEmit) putScalar(ind int, f *ir.Field, kind uint8, shape ir.TableMessageShape, expr string) {
	if en := enumOf(f); en != nil {
		e.putEnum(ind, en, expr)
		return
	}
	switch int(kind) {
	case ir.TableKindBool:
		e.p(ind, "schema_rt::msg_put_bool(w, out, %s);", expr)
		return
	case ir.TableKindF64:
		e.p(ind, "if !schema_rt::msg_finite64(schema_rt::fixed_f64_bits(%s)) { return schema_rt::msg_refuse(w, schema_rt::MsgRefuseNonFiniteFloat); }", expr)
		e.put(ind, "schema_rt::fixed_f64_bits("+expr+")", 64)
		return
	case ir.TableKindF32:
		e.p(ind, "if !schema_rt::msg_finite32(schema_rt::fixed_f32_bits(%s)) { return schema_rt::msg_refuse(w, schema_rt::MsgRefuseNonFiniteFloat); }", expr)
		if shape.Packing == ir.TableMessageQuantized {
			count, delta, ok := ir.TableMessageQuantization(shape)
			if !ok {
				e.fail("goose message form: %s's compressed-float triple is non-conforming (SPEC.md §4.3)", f.Name)
				return
			}
			e.put(ind, fmt.Sprintf("schema_rt::msg_quantize(%s, %s, %s, %d)", expr, f32Lit(shape.QMin), f32Lit(delta), count), shape.Bits)
			return
		}
		e.put(ind, "schema_rt::fixed_f32_bits("+expr+")", 32)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	if e.fx.kindOf(f) == kInt128 {
		// a 128-bit value is its two lanes, low first, as the wire's
		// little-endian bytes are; a ranged one is value - base across both
		// lanes, the borrow carried (§3.3)
		if shape.Packing != ir.TableMessageRanged {
			e.put(ind, expr+".lo", 64)
			e.put(ind, expr+".hi", 64)
			return
		}
		blo, bhi := lanes128(shape.Base)
		v := e.loopVar()
		e.p(ind, "let lo%s: u64 = %s.lo - %s;", v, expr, blo)
		e.p(ind, "let hi%s: u64 = %s.hi - %s - (if %s.lo < %s { 1 } else { 0 });", v, expr, bhi, expr, blo)
		e.put(ind, "lo"+v, min(bits, 64))
		e.put(ind, "hi"+v, bits-64)
		return
	}
	if shape.Packing == ir.TableMessageRanged {
		e.put(ind, fmt.Sprintf("%s - %s", e.asU64(f, expr), u64Lit(shape.Base)), bits)
		return
	}
	e.put(ind, e.asU64(f, expr), bits)
}

// wide64 reports a storage the bit reader's u64 lands in without a cast: a
// u64, or a flags set (whose type is a u64 alias).
func (e *msgEmit) wide64(f *ir.Field) bool {
	if f.Type.Kind == ir.TNamed {
		_, isFlags := f.Type.Ref.(*ir.Flags)
		return isFlags
	}
	return e.fx.storageType(f) == "u64"
}

func enumOf(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	en, _ := f.Type.Ref.(*ir.Enum)
	return en
}

// putEnum writes the reference naming the value's VARIANT, 0 for None
// (§3.3); a value past the declared variants names no entry and the write
// refuses invalid_value.
func (e *msgEmit) putEnum(ind int, en *ir.Enum, expr string) {
	e.p(ind, "if %s == 0 {", expr)
	e.ref(ind+1, "0")
	for i := range en.Variants {
		e.p(ind, "} else if %s == %d {", expr, i+1)
		e.ref(ind+1, e.nameSlot(ir.TableWireId(en.VariantWireName(i))))
	}
	e.p(ind, "} else {")
	e.p(ind+1, "return schema_rt::msg_refuse(w, schema_rt::MsgRefuseInvalidValue);")
	e.p(ind, "}")
}

// putElement writes one element at its element kind's own framing: a nested
// body, a union's arm reference and payload, or a value.
func (e *msgEmit) putElement(ind int, f *ir.Field, kind uint8, shape ir.TableMessageShape, expr string) {
	switch int(kind) {
	case ir.TableKindTable:
		e.p(ind, "if !_%sMsgSave(w, out, %s) { return false; }", f.Type.Name, expr)
	case ir.TableKindUnion:
		e.p(ind, "if !_%sMsgSave(w, out, %s) { return false; }", f.Type.Name, expr)
	default:
		e.putScalar(ind, f, kind, shape, expr)
	}
}

func inner(s ir.TableMessageShape) ir.TableMessageShape {
	if s.Inner == nil {
		return ir.TableMessageShape{}
	}
	return *s.Inner
}

// putValue writes a field's payload under its entry, its header already
// written: text, an array's count and elements, a keyed array's present slots,
// or one element.
func (e *msgEmit) putValue(ind int, f *ir.Field, entry ir.TableVocabularyEntry, pl place) {
	el := elemOf(f)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		bits := ir.TableMessageBitsRequired(0, entry.Shape.Max)
		if f.Type.Kind == ir.TBytes {
			bits = ir.TableMessageCountBits(entry.Shape)
		}
		e.p(ind, "if %s < 0 || %s > %d { return schema_rt::msg_refuse(w, schema_rt::MsgRefuseInvalidValue); }", pl.length, pl.length, f.Type.Size)
		e.put(ind, fmt.Sprintf("(%s as! u64)", pl.length), int64(bits))
		e.p(ind, "schema_rt::msg_align(w, out);") // THE TEXT ALIGN (§3.3): a string(N) and a bytes(N) align before their bytes
		e.p(ind, "schema_rt::msg_put_bytes(w, out, %s, %s);", pl.val, pl.length)
	case f.Type.Kind == ir.TWString:
		e.p(ind, "if %s < 0 || %s > %d { return schema_rt::msg_refuse(w, schema_rt::MsgRefuseInvalidValue); }", pl.length, pl.length, f.Type.Size)
		e.put(ind, fmt.Sprintf("(%s as! u64)", pl.length), int64(ir.TableMessageBitsRequired(0, entry.Shape.Max)))
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %s {", i, pl.length)
		e.put(ind+1, fmt.Sprintf("%s[%s]", pl.val, i), 16)
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.KeyEnum != "":
		e.putKeyed(ind, f, entry, pl)
	case f.Array != ir.ArrayNone:
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = pl.val + "Count"
			e.p(ind, "if %s < 0 || %s > %d { return schema_rt::msg_refuse(w, schema_rt::MsgRefuseInvalidValue); }", count, count, f.ArrayBound)
		}
		if bits := ir.TableMessageCountBits(entry.Shape); bits > 0 {
			e.put(ind, fmt.Sprintf("(%s as! u64) - %d", count, entry.Shape.Min), int64(bits))
		}
		if ir.TableMessageAligns(entry.Kind, entry.Shape) {
			e.p(ind, "schema_rt::msg_align(w, out);")
		}
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < %s {", i, count)
		e.putElement(ind+1, el, entry.Shape.Elem, inner(entry.Shape), pl.at(i).val)
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	default:
		e.putElement(ind, el, entry.Kind, entry.Shape, pl.val)
	}
}

// slotRides is the elision test of one keyed slot or fixed-array element
// (§3.3, unchanged from the variable form): a table element rides when its
// body writes more than the terminator, a union element when it is not None,
// anything else when it is not its declared default.
func (e *msgEmit) slotRides(ind int, el *ir.Field, expr, def string) string {
	switch {
	case e.isStruct(el):
		v := e.loopVar()
		e.p(ind, "var probe%s = schema_rt::MsgWriter { .. };", v)
		e.p(ind, "probe%s.measure = true;", v)
		e.p(ind, "if !_%sMsgSave(probe%s, out, %s) { return schema_rt::msg_refuse(w, probe%s.reason); }", el.Type.Name, v, expr, v)
		return fmt.Sprintf("probe%s.bits > %s", v, msgRefBits)
	case e.isUnion(el):
		return expr + ".Type != 0"
	}
	return e.differs(el, expr, def)
}

// differs is "not the declared default", value against value: a float by its
// bits, so a -0.0 against a 0.0 default rides.
func (e *msgEmit) differs(f *ir.Field, expr, def string) string {
	switch f.Type.Kind {
	case ir.TFloat32:
		return fmt.Sprintf("schema_rt::fixed_f32_bits(%s) != schema_rt::fixed_f32_bits(%s)", expr, def)
	case ir.TFloat64:
		return fmt.Sprintf("schema_rt::fixed_f64_bits(%s) != schema_rt::fixed_f64_bits(%s)", expr, def)
	case ir.TBool:
		return fmt.Sprintf("%s != %s", expr, def)
	}
	if e.fx.kindOf(f) == kInt128 {
		return fmt.Sprintf("(%s.lo != %s.lo || %s.hi != %s.hi)", expr, def, expr, def)
	}
	return fmt.Sprintf("%s != %s", expr, def)
}

func (e *msgEmit) isStruct(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Struct)
	return ok
}

func (e *msgEmit) isUnion(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Union)
	return ok
}

// putKeyed writes an enum-keyed array (§3.2, §3.3): the present-slot count at
// bits_required(0, slots), then per present slot the key's variant reference
// and the element, ascending. Slot k-1 holds key k.
func (e *msgEmit) putKeyed(ind int, f *ir.Field, entry ir.TableVocabularyEntry, pl place) {
	el := elemOf(f)
	slots := slotCount(f)
	pres := "pres" + e.loopVar()
	e.p(ind, "var %s: u64 = 0;", pres)
	i := e.loopVar()
	e.p(ind, "var %s: i64 = 0;", i)
	e.p(ind, "while %s < %d {", i, slots)
	rides := e.slotRides(ind+1, el, pl.at(i).val, strings.Replace(pl.at(i).val, "v.", "d.", 1))
	e.p(ind+1, "if %s { %s += 1; }", rides, pres)
	e.p(ind+1, "%s += 1;", i)
	e.p(ind, "}")
	e.put(ind, pres, int64(ir.TableMessageBitsRequired(0, entry.Shape.Max)))
	for k := int64(0); k < slots; k++ {
		at := pl.at(fmt.Sprint(k)).val
		rides := e.slotRides(ind, el, at, strings.Replace(at, "v.", "d.", 1))
		e.p(ind, "if %s {", rides)
		if int(k) >= len(f.KeyEnumRef.Variants) {
			e.p(ind+1, "return schema_rt::msg_refuse(w, schema_rt::MsgRefuseInvalidValue);")
		} else {
			e.ref(ind+1, e.nameSlot(ir.TableWireId(f.KeyEnumRef.VariantWireName(int(k)))))
			e.putElement(ind+1, el, entry.Shape.Elem, inner(entry.Shape), at)
		}
		e.p(ind, "}")
	}
}

// putField is one field of a body: its elision test, then its header and
// payload (§3.3: a field at its declared default is not written; an empty
// string, array or union, and an all-default nested table, are not written;
// presence decides a `?T`).
func (e *msgEmit) putField(ind int, f *ir.Field) {
	entry := ir.TableFieldEntry(f)
	pl := fieldPlace("v", f)
	def := fieldPlace("d", f)
	slot := e.slot(entry)
	header := func(ind int) {
		e.ref(ind, slot)
		e.putValue(ind, f, entry, pl)
	}
	switch {
	case f.Type.Optional:
		e.p(ind, "if %s {", presentOf("v", f))
		header(ind + 1)
		e.p(ind, "}")
	case f.KeyEnum != "":
		// the keyed array's own walk decides: no present slot, no field
		e.p(ind, "{")
		el := elemOf(f)
		i := e.loopVar()
		e.p(ind+1, "var rides%s = false;", i)
		e.p(ind+1, "var %s: i64 = 0;", i)
		e.p(ind+1, "while %s < %d {", i, slotCount(f))
		r := e.slotRides(ind+2, el, pl.at(i).val, def.at(i).val)
		e.p(ind+2, "if %s { rides%s = true; }", r, i)
		e.p(ind+2, "%s += 1;", i)
		e.p(ind+1, "}")
		e.p(ind+1, "if rides%s {", i)
		header(ind + 2)
		e.p(ind+1, "}")
		e.p(ind, "}")
	case f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
		e.p(ind, "if %s != 0 {", pl.length)
		header(ind + 1)
		e.p(ind, "}")
	case f.Array == ir.ArrayCounted:
		e.p(ind, "if %sCount != 0 {", pl.val)
		header(ind + 1)
		e.p(ind, "}")
	case f.Array == ir.ArrayFixed && e.isStruct(f):
		header(ind)
	case f.Array == ir.ArrayFixed:
		e.p(ind, "{")
		el := elemOf(f)
		i := e.loopVar()
		e.p(ind+1, "var rides%s = false;", i)
		e.p(ind+1, "var %s: i64 = 0;", i)
		e.p(ind+1, "while %s < %d {", i, f.ArrayBound)
		r := e.slotRides(ind+2, el, pl.at(i).val, def.at(i).val)
		e.p(ind+2, "if %s { rides%s = true; }", r, i)
		e.p(ind+2, "%s += 1;", i)
		e.p(ind+1, "}")
		e.p(ind+1, "if rides%s {", i)
		header(ind + 2)
		e.p(ind+1, "}")
		e.p(ind, "}")
	case e.isUnion(f):
		e.p(ind, "if %s.Type != 0 {", pl.val)
		e.ref(ind+1, slot)
		e.p(ind+1, "if !_%sMsgSave(w, out, %s) { return false; }", f.Type.Name, pl.val)
		e.p(ind, "}")
	case e.isStruct(f):
		r := e.slotRides(ind, f, pl.val, def.val)
		e.p(ind, "if %s {", r)
		header(ind + 1)
		e.p(ind, "}")
	default:
		e.p(ind, "if %s {", e.differs(f, pl.val, def.val))
		header(ind + 1)
		e.p(ind, "}")
	}
}

// usesDefaults reports whether a body's elision reads its fresh value `d`.
func (e *msgEmit) usesDefaults(st *ir.Struct) bool {
	for _, f := range st.Fields {
		switch {
		case f.Type.Optional, e.isUnion(f) && f.Array == ir.ArrayNone, e.isStruct(f):
		case f.Array == ir.ArrayCounted:
		case f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
		case (f.Array == ir.ArrayFixed || f.KeyEnum != "") && e.isUnion(f):
		default:
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// THE READ (against the unit's own vocabulary)
// ---------------------------------------------------------------------------

// getScalar lands one value: the inverse of putScalar, with the reader's own
// bound applied (a ranged offset past the range reconstructs and clamps,
// §3.3), a quantized index above its count refused as damage, and a
// non-finite float refusing the batch.
func (e *msgEmit) getScalar(ind int, f *ir.Field, kind uint8, shape ir.TableMessageShape, dst string) {
	if en := enumOf(f); en != nil {
		e.getEnum(ind, en, dst)
		return
	}
	v := e.loopVar()
	switch int(kind) {
	case ir.TableKindBool:
		e.p(ind, "%s = schema_rt::msg_get(r, 1) != 0;", dst)
		return
	case ir.TableKindF64:
		e.p(ind, "let raw%s = schema_rt::msg_get(r, 64);", v)
		e.p(ind, "if !schema_rt::msg_finite64(raw%s) { return schema_rt::msg_refuse_read(r, schema_rt::MsgRefuseNonFiniteFloat); }", v)
		e.p(ind, "%s = schema_rt::fixed_bits_f64(raw%s);", dst, v)
		return
	case ir.TableKindF32:
		if shape.Packing == ir.TableMessageQuantized {
			count, delta, _ := ir.TableMessageQuantization(shape)
			e.p(ind, "let index%s = schema_rt::msg_get(r, %d);", v, shape.Bits)
			e.p(ind, "if index%s > %d { return schema_rt::msg_damage(r); }", v, count)
			e.p(ind, "var q%s: f32 = schema_rt::msg_dequantize(index%s, %s, %s, %d);", v, v, f32Lit(shape.QMin), f32Lit(delta), count)
			if f.HasFloatRange {
				lo, hi := f32Lit(float32(f.FMin)), f32Lit(float32(f.FMax))
				e.p(ind, "if q%s < %s {", v, lo)
				e.p(ind+1, "q%s = %s;", v, lo)
				e.p(ind+1, "r.clamped += 1;")
				e.p(ind, "} else if q%s > %s {", v, hi)
				e.p(ind+1, "q%s = %s;", v, hi)
				e.p(ind+1, "r.clamped += 1;")
				e.p(ind, "}")
			}
			e.p(ind, "%s = q%s;", dst, v)
			return
		}
		e.p(ind, "let raw%s = schema_rt::msg_get(r, 32) as! u32;", v)
		e.p(ind, "if !schema_rt::msg_finite32(raw%s) { return schema_rt::msg_refuse_read(r, schema_rt::MsgRefuseNonFiniteFloat); }", v)
		e.p(ind, "%s = schema_rt::fixed_bits_f32(raw%s);", dst, v)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	if e.fx.kindOf(f) == kInt128 {
		if shape.Packing != ir.TableMessageRanged {
			e.p(ind, "%s.lo = schema_rt::msg_get(r, 64);", dst)
			e.p(ind, "%s.hi = schema_rt::msg_get(r, 64);", dst)
			return
		}
		// the offset across both lanes, then the base added with its carry;
		// the reader's own 128-bit bound is not applied here (owed, #2236)
		blo, bhi := lanes128(shape.Base)
		e.p(ind, "let lo%s = schema_rt::msg_get(r, %d);", v, min(bits, 64))
		if bits > 64 {
			e.p(ind, "let hi%s = schema_rt::msg_get(r, %d);", v, bits-64)
		} else {
			e.p(ind, "let hi%s: u64 = 0;", v)
		}
		e.p(ind, "%s.lo = lo%s + %s;", dst, v, blo)
		e.p(ind, "%s.hi = hi%s + %s + (if %s.lo < lo%s { 1 } else { 0 });", dst, v, bhi, dst, v)
		return
	}
	typ := e.fx.storageType(f)
	if shape.Packing == ir.TableMessageRanged {
		// the span the declaration allows; an offset past it clamps (§3.3)
		span := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		span.Sub(span, big.NewInt(1))
		if f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed {
			if lo, hi, ok := ir.TableRawRange(f); ok {
				span = new(big.Int).Sub(hi, lo)
			}
		}
		e.p(ind, "var off%s = schema_rt::msg_get(r, %d);", v, bits)
		if bits < 64 && span.Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))) < 0 {
			e.p(ind, "if off%s > %s {", v, span.String())
			e.p(ind+1, "off%s = %s;", v, span.String())
			e.p(ind+1, "r.clamped += 1;")
			e.p(ind, "}")
		}
		sum := "off" + v
		if base := u64Lit(shape.Base); base != "0" {
			sum = fmt.Sprintf("(off%s + %s)", v, base)
		}
		if e.wide64(f) {
			e.p(ind, "%s = %s;", dst, sum)
		} else {
			e.p(ind, "%s = %s as! %s;", dst, sum, typ)
		}
		return
	}
	if e.wide64(f) {
		e.p(ind, "%s = schema_rt::msg_get(r, %d);", dst, bits)
		return
	}
	e.p(ind, "%s = schema_rt::msg_get(r, %d) as! %s;", dst, bits, typ)
}

// getEnum lands a variant reference: 0 is None, a variant's slot its
// ordinal; any other reference contradicts the position it was used in, which
// is damage (§3.3).
func (e *msgEmit) getEnum(ind int, en *ir.Enum, dst string) {
	v := e.loopVar()
	e.p(ind, "let ref%s = schema_rt::msg_get(r, %s);", v, msgRefBits)
	e.p(ind, "if ref%s == 0 {", v)
	e.p(ind+1, "%s = 0;", dst)
	for i := range en.Variants {
		e.p(ind, "} else if ref%s == %s {", v, e.nameSlot(ir.TableWireId(en.VariantWireName(i))))
		e.p(ind+1, "%s = %d;", dst, i+1)
	}
	e.p(ind, "} else {")
	e.p(ind+1, "return schema_rt::msg_damage(r);")
	e.p(ind, "}")
}

func (e *msgEmit) getElement(ind int, f *ir.Field, kind uint8, shape ir.TableMessageShape, dst string) {
	switch int(kind) {
	case ir.TableKindTable, ir.TableKindUnion:
		e.p(ind, "if !_%sMsgLoad(r, %s) { return false; }", f.Type.Name, dst)
	default:
		e.getScalar(ind, f, kind, shape, dst)
	}
}

// getValue lands a field's payload under its own entry: text (kept to the
// reader's bound, the excess counted clamped), an array's count and elements
// (the excess past the bound stepped over and counted), a keyed array's
// present slots, or one element.
func (e *msgEmit) getValue(ind int, f *ir.Field, entry ir.TableVocabularyEntry, pl place) {
	el := elemOf(f)
	v := e.loopVar()
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		bits := ir.TableMessageBitsRequired(0, entry.Shape.Max)
		if f.Type.Kind == ir.TBytes {
			bits = ir.TableMessageCountBits(entry.Shape)
		}
		e.p(ind, "let n%s = schema_rt::msg_get(r, %d) as! i64;", v, bits)
		e.p(ind, "if !schema_rt::msg_align_read(r) || !schema_rt::msg_has(r, n%s * 8) { return schema_rt::msg_damage(r); }", v)
		e.p(ind, "let at%s = r.pos >> 3;", v)
		keep := fmt.Sprintf("n%s", v)
		if f.Type.Kind == ir.TString {
			e.p(ind, "if !schema_rt::fixed_utf8_ok(r.b, at%s, n%s) { return schema_rt::msg_damage(r); }", v, v)
			e.p(ind, "let k%s = schema_rt::msg_utf8_keep(r.b, at%s, n%s, %d);", v, v, v, f.Type.Size)
		} else {
			e.p(ind, "var k%s = n%s;", v, v)
			e.p(ind, "if k%s > %d { k%s = %d; }", v, f.Type.Size, v, f.Type.Size)
		}
		e.p(ind, "if k%s < %s { r.clamped += 1; }", v, keep)
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < k%s {", i, v)
		e.p(ind+1, "%s[%s] = r.b[at%s + %s];", pl.val, i, v, i)
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
		e.p(ind, "%s = k%s as! i32;", pl.length, v)
		e.p(ind, "r.pos += n%s * 8;", v)
	case f.Type.Kind == ir.TWString:
		e.p(ind, "let n%s = schema_rt::msg_get(r, %d) as! i64;", v, ir.TableMessageBitsRequired(0, entry.Shape.Max))
		e.p(ind, "if !schema_rt::msg_has(r, n%s * 16) { return schema_rt::msg_damage(r); }", v)
		e.p(ind, "var high%s = false;", v)
		e.p(ind, "var k%s: i64 = 0;", v)
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < n%s {", i, v)
		e.p(ind+1, "let cu = schema_rt::msg_get(r, 16) as! u16;")
		e.p(ind+1, "if cu == 0 { return schema_rt::msg_damage(r); }")
		e.p(ind+1, "if high%s {", v)
		e.p(ind+2, "if cu < 0xDC00 || cu > 0xDFFF { return schema_rt::msg_damage(r); }")
		e.p(ind+2, "high%s = false;", v)
		e.p(ind+1, "} else if cu >= 0xD800 && cu <= 0xDBFF {")
		e.p(ind+2, "high%s = true;", v)
		e.p(ind+1, "} else if cu >= 0xDC00 && cu <= 0xDFFF {")
		e.p(ind+2, "return schema_rt::msg_damage(r);")
		e.p(ind+1, "}")
		e.p(ind+1, "if %s < %d {", i, f.Type.Size)
		e.p(ind+2, "%s[%s] = cu;", pl.val, i)
		e.p(ind+2, "k%s += 1;", v)
		e.p(ind+1, "}")
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
		e.p(ind, "if high%s { return schema_rt::msg_damage(r); }", v)
		e.p(ind, "if k%s < n%s {", v, v)
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind+1, "if k%s > 0 && %s[k%s - 1] >= 0xD800 && %s[k%s - 1] <= 0xDBFF { k%s -= 1; }", v, pl.val, v, pl.val, v, v)
		e.p(ind, "}")
		e.p(ind, "%s = k%s as! i32;", pl.length, v)
	case f.KeyEnum != "":
		e.p(ind, "let n%s = schema_rt::msg_get(r, %d);", v, ir.TableMessageBitsRequired(0, entry.Shape.Max))
		i := e.loopVar()
		e.p(ind, "var %s: u64 = 0;", i)
		e.p(ind, "while %s < n%s {", i, v)
		e.p(ind+1, "if !r.ok { return false; }")
		e.p(ind+1, "let ky = schema_rt::msg_get(r, %s);", msgRefBits)
		for k := range f.KeyEnumRef.Variants {
			kw := "} else if"
			if k == 0 {
				kw = "if"
			}
			e.p(ind+1, "%s ky == %s {", kw, e.nameSlot(ir.TableWireId(f.KeyEnumRef.VariantWireName(k))))
			e.getElement(ind+2, el, entry.Shape.Elem, inner(entry.Shape), pl.at(fmt.Sprint(k)).val)
		}
		if len(f.KeyEnumRef.Variants) > 0 {
			e.p(ind+1, "} else {")
			e.p(ind+2, "return schema_rt::msg_damage(r);")
			e.p(ind+1, "}")
		} else {
			e.p(ind+1, "return schema_rt::msg_damage(r);")
		}
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array != ir.ArrayNone:
		bits := ir.TableMessageCountBits(entry.Shape)
		e.p(ind, "let n%s = (schema_rt::msg_get(r, %d) as! i64) + %d;", v, bits, entry.Shape.Min)
		if ir.TableMessageAligns(entry.Kind, entry.Shape) {
			e.p(ind, "if !schema_rt::msg_align_read(r) { return false; }")
		}
		e.p(ind, "var k%s = n%s;", v, v)
		e.p(ind, "if k%s > %d {", v, f.ArrayBound)
		e.p(ind+1, "k%s = %d;", v, f.ArrayBound)
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind, "}")
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < k%s {", i, v)
		e.p(ind+1, "if !r.ok { return false; }")
		e.getElement(ind+1, el, entry.Shape.Elem, inner(entry.Shape), pl.at(i).val)
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
		if w := ir.TableMessageValueBits(entry.Shape.Elem, inner(entry.Shape)); w >= 0 && enumOf(el) == nil {
			e.p(ind, "if !schema_rt::msg_skip(r, (n%s - k%s) * %d) { return false; }", v, v, w)
		} else {
			e.p(ind, "if k%s < n%s { return schema_rt::msg_damage(r); }", v, v)
		}
		if f.Array == ir.ArrayCounted {
			e.p(ind, "%sCount = k%s as! i32;", pl.val, v)
		}
	default:
		e.getElement(ind, el, entry.Kind, entry.Shape, pl.val)
	}
}

// ---------------------------------------------------------------------------
// THE PER-TYPE FUNCTIONS
// ---------------------------------------------------------------------------

func (e *msgEmit) emitStruct(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.Guard != "" {
			e.fail("goose message form: %s.%s is under a guarded branch, which the fixed form refuses (docs/SPEC-TABLES.md §3.4)", st.Name, f.Name)
			return
		}
	}
	n := st.Name
	typ := e.fx.structType(st)
	e.g.Declare("_" + n + "MsgSave")
	e.g.Declare("_" + n + "MsgLoad")
	e.p(0, "// ---- %s's message body (docs/SPEC-TABLES.md §3.3): fields, each a reference and a payload, then the zero reference ----", n)
	e.loop = 0
	e.p(0, "fn _%sMsgSave(w: schema_rt::MsgWriter&, out: u8[>..]&, v: const %s&) -> bool {", n, typ)
	if e.usesDefaults(st) {
		e.p(1, "let d = %s { .. };", typ)
	}
	for _, f := range st.Fields {
		e.p(1, "// %s", f.Name)
		e.putField(1, f)
	}
	e.ref(1, "0")
	e.p(1, "return w.ok;")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "fn _%sMsgLoad(r: schema_rt::MsgReader&, v: %s&) -> bool {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "while true {")
	e.p(2, "let sl = schema_rt::msg_get(r, %s);", msgRefBits)
	e.p(2, "if !r.ok { return false; }")
	e.p(2, "if sl == 0 { return true; }")
	for i, f := range st.Fields {
		kw := "} else if"
		if i == 0 {
			kw = "if"
		}
		entry := ir.TableFieldEntry(f)
		e.p(2, "%s sl == %s {", kw, e.slot(entry))
		e.getValue(3, f, entry, fieldPlace("v", f))
		if f.Type.Optional {
			e.p(3, "%s = true;", presentOf("v", f))
		}
	}
	if len(st.Fields) > 0 {
		e.p(2, "} else {")
		e.p(3, "return schema_rt::msg_damage(r);")
		e.p(2, "}")
	} else {
		e.p(2, "return schema_rt::msg_damage(r);")
	}
	e.p(2, "if !r.ok { return false; }")
	e.p(1, "}")
	e.p(1, "return false;")
	e.p(0, "}\n")
}

// armPlace is an arm's storage inside its union, under the arm's own name.
func armPlace(base string, v ir.UnionVariant) place {
	return fieldPlace(base, armField(v))
}

// emitUnion writes a union's arm: the reference naming the ARM, 0 for None,
// then the arm's payload under the arm entry's own kind and shape (§3.3).
func (e *msgEmit) emitUnion(un *ir.Union) {
	n := un.Name
	typ := e.fx.unionType(un)
	e.g.Declare("_" + n + "MsgSave")
	e.g.Declare("_" + n + "MsgLoad")
	e.p(0, "// ---- union %s on the message form: the arm's reference, then its payload ----", n)
	e.loop = 0
	e.p(0, "fn _%sMsgSave(w: schema_rt::MsgWriter&, out: u8[>..]&, v: const %s&) -> bool {", n, typ)
	for _, v := range un.Variants {
		if v.F != nil && v.F.KeyEnum != "" && !e.isUnion(v.F) {
			e.p(1, "let d = %s { .. };", typ)
			break
		}
	}
	e.p(1, "if v.Type == 0 {")
	e.ref(2, "0")
	for i, v := range un.Variants {
		e.p(1, "} else if v.Type == %d {", i+1)
		entry := ir.TableArmEntry(v)
		e.ref(2, e.slot(entry))
		switch {
		case v.Void():
		case v.Body():
			e.p(2, "if !_%sMsgSave(w, out, v.%s) { return false; }", v.Ref.Name, ir.GoExportName(v.Name))
		default:
			e.putValue(2, armField(v), entry, armPlace("v", v))
		}
	}
	e.p(1, "} else {")
	e.p(2, "return schema_rt::msg_refuse(w, schema_rt::MsgRefuseInvalidValue);")
	e.p(1, "}")
	e.p(1, "return w.ok;")
	e.p(0, "}\n")

	e.loop = 0
	e.p(0, "fn _%sMsgLoad(r: schema_rt::MsgReader&, v: %s&) -> bool {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "let sl = schema_rt::msg_get(r, %s);", msgRefBits)
	e.p(1, "if !r.ok { return false; }")
	e.p(1, "if sl == 0 { return true; }")
	for i, v := range un.Variants {
		kw := "} else if"
		if i == 0 {
			kw = "if"
		}
		entry := ir.TableArmEntry(v)
		e.p(1, "%s sl == %s {", kw, e.slot(entry))
		e.p(2, "v.Type = %d;", i+1)
		switch {
		case v.Void():
		case v.Body():
			e.p(2, "if !_%sMsgLoad(r, v.%s) { return false; }", v.Ref.Name, ir.GoExportName(v.Name))
		default:
			e.getValue(2, armField(v), entry, armPlace("v", v))
		}
	}
	if len(un.Variants) > 0 {
		e.p(1, "} else {")
		e.p(2, "return schema_rt::msg_damage(r);")
		e.p(1, "}")
	} else {
		e.p(1, "return schema_rt::msg_damage(r);")
	}
	e.p(1, "return r.ok;")
	e.p(0, "}\n")
}

// emitRoot writes a fixed root's batch API (§3.3): the announcement as a
// constant, MeasureMessages, SaveMessages and LoadMessages over the unit's own
// vocabulary.
func (e *msgEmit) emitRoot(st *ir.Struct) {
	n := st.Name
	typ := e.fx.structType(st)
	e.g.Declare(n + "Announce")
	e.g.Declare(n + "MeasureMessages")
	e.g.Declare(n + "SaveMessages")
	e.g.Declare(n + "LoadMessages")
	e.p(0, "// ---- %s, the message form, form byte 2 (docs/SPEC-TABLES.md §3.3) ----", n)
	e.p(0, "// %sAnnounce is the unit's announcement (MessageAnnouncement, the message form's package module).", n)
	e.p(0, "let %sAnnounce: const u8[:] = MessageAnnouncement;\n", n)

	for _, measure := range []bool{true, false} {
		e.loop = 0
		if measure {
			e.p(0, "// %sMeasureMessages is the byte size of the batch SaveMessages writes, or -1 refused.", n)
			e.p(0, "fn %sMeasureMessages(values: const %s[:], report: schema_rt::MsgReport&) -> i64 {", n, typ)
			e.p(1, "var out: u8[>..] = [];")
		} else {
			e.p(0, "// %sSaveMessages appends ONE batch of values.len bodies to out and returns its bytes,", n)
			e.p(0, "// or -1 refused: a batch of zero or above 256 is batch_too_large (§3.3).")
			e.p(0, "fn %sSaveMessages(values: const %s[:], out: u8[>..]&, report: schema_rt::MsgReport&) -> i64 {", n, typ)
		}
		e.p(1, "report = schema_rt::MsgReport { .. };")
		e.p(1, "if values.len < 1 || values.len > schema_rt::MsgBatchMax {")
		e.p(2, "report.reason = schema_rt::MsgRefuseBatchTooLarge;")
		e.p(2, "return -1;")
		e.p(1, "}")
		if !measure {
			// the whole batch is measured first, so a refusal writes nothing
			e.p(1, "let size = %sMeasureMessages(values, report);", n)
			e.p(1, "if size < 0 { return -1; }")
		}
		e.p(1, "var w = schema_rt::MsgWriter { .. };")
		if measure {
			e.p(1, "w.measure = true;")
		}
		e.p(1, "schema_rt::msg_put(w, out, 2, 8);")
		e.p(1, "schema_rt::msg_put(w, out, (values.len - 1) as! u64, 8);")
		e.p(1, "var i: i64 = 0;")
		e.p(1, "while i < values.len {")
		e.p(2, "if !_%sMsgSave(w, out, values[i]) {", n)
		e.p(3, "report.reason = w.reason;")
		e.p(3, "return -1;")
		e.p(2, "}")
		e.p(2, "i += 1;")
		e.p(1, "}")
		e.p(1, "schema_rt::msg_align(w, out);")
		e.p(1, "return w.bits / 8;")
		e.p(0, "}\n")
	}

	e.loop = 0
	e.p(0, "// %sLoadMessages reads ONE batch into values against the unit's own vocabulary and", n)
	e.p(0, "// returns the bodies delivered: a capacity short of the wire's count is batch_too_large")
	e.p(0, "// with report.count set and nothing decoded; damage inside body k delivers bodies 1 to")
	e.p(0, "// k - 1 and sets malformed; a non-finite float refuses the batch (§3.3).")
	e.p(0, "fn %sLoadMessages(values: %s[:], data: const u8[:], report: schema_rt::MsgReport&) -> i64 {", n, typ)
	e.p(1, "report = schema_rt::MsgReport { .. };")
	e.p(1, "if data.len < 1 {")
	e.p(2, "report.malformed = true;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "if data[0] != schema_rt::MsgForm {")
	e.p(2, "report.reason = schema_rt::MsgRefuseNewerForm;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "var r = schema_rt::MsgReader { b: data, pos: 8, end: data.len * 8, .. };")
	e.p(1, "let count = (schema_rt::msg_get(r, 8) as! i64) + 1;")
	e.p(1, "if !r.ok {")
	e.p(2, "report.malformed = true;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "if count > values.len {")
	e.p(2, "report.count = count;")
	e.p(2, "report.reason = schema_rt::MsgRefuseBatchTooLarge;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "var i: i64 = 0;")
	e.p(1, "while i < count {")
	e.p(2, "if !_%sMsgLoad(r, values[i]) {", n)
	e.p(3, "if r.reason != 0 {")
	e.p(4, "report.reason = r.reason;")
	e.p(4, "return -1;")
	e.p(3, "}")
	e.p(3, "report.malformed = true;")
	e.p(3, "report.clamped = r.clamped;")
	e.p(3, "return i;")
	e.p(2, "}")
	e.p(2, "i += 1;")
	e.p(1, "}")
	e.p(1, "report.clamped = r.clamped;")
	e.p(1, "if !schema_rt::msg_align_read(r) || r.pos != r.end { report.malformed = true; }")
	e.p(1, "return count;")
	e.p(0, "}\n")
}
