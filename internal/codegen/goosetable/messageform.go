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
// THE READ IS BY PEER ENTRY (§3.3, §4): a connection's reader first reads the
// PEER's announcement with AnnounceRead (runtime/message.goose: a form-1 file
// read tolerantly into a vocabulary of the peer's build, each entry's id, kind
// and shape), and every reference of a body resolves against that vocabulary.
// The entry's id selects this reader's declaration through a match on the
// 64-bit ids, and the payload decodes at the SENDER's announced shape: an id
// this reader cannot name is `unknown` and is stepped over by its shape, a
// kind below this reader's on the same ladder is `widened` and decodes, any
// other kind is `kind_mismatch` and is stepped over, and this reader's own
// bounds clamp what lands — the 128-bit kinds across both lanes. The Go
// reference (internal/tablewire, messagedecode.go) is the oracle, decision for
// decision.
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
	"slices"
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
// THE READ (against the PEER's announced vocabulary, docs/SPEC-TABLES.md §3.3)
// ---------------------------------------------------------------------------
//
// A body is read the way the Go reference reads it (internal/tablewire,
// messagedecode.go): every reference resolves against the vocabulary
// AnnounceRead set from the sender's announcement, the entry's id selects this
// reader's declaration, and the payload decodes at the SENDER's announced
// shape before this reader's own bounds apply. An id this reader cannot name
// is counted `unknown` and stepped over by its shape; an announced kind below
// this reader's on the same ladder is `widened` and decodes; any other kind is
// `kind_mismatch` and is stepped over. The dispatch is a match on the 64-bit
// ids, compile-time constants.

// readClamp emits the declared-bound clamp of one integer value held in the
// local `x` (i64 for a signed storage, u64 for an unsigned one), counting each
// clamp (internal/tablewire scalar: a declared range, and a bits(N) below its
// storage).
func (e *msgEmit) readClamp(ind int, f *ir.Field, x string) {
	t := f.Type
	if lo, hi, ok := ir.TableRawRange(f); ok && (t.Kind == ir.TInt || t.Kind == ir.TFixed) {
		if t.Signed {
			l, h := goose.IntLit(lo), goose.IntLit(hi)
			e.p(ind, "if %s < %s {", x, l)
			e.p(ind+1, "%s = %s;", x, l)
			e.p(ind+1, "r.clamped += 1;")
			e.p(ind, "} else if %s > %s {", x, h)
			e.p(ind+1, "%s = %s;", x, h)
			e.p(ind+1, "r.clamped += 1;")
			e.p(ind, "}")
			return
		}
		l, h := u64Lit(lo), u64Lit(hi)
		if lo.Sign() > 0 {
			e.p(ind, "if %s < %s {", x, l)
			e.p(ind+1, "%s = %s;", x, l)
			e.p(ind+1, "r.clamped += 1;")
			e.p(ind, "} else if %s > %s {", x, h)
		} else {
			e.p(ind, "if %s > %s {", x, h)
		}
		e.p(ind+1, "%s = %s;", x, h)
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind, "}")
		return
	}
	if t.Kind == ir.TBits && t.Width < 64 {
		m := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1))
		e.p(ind, "if %s > %s {", x, m.String())
		e.p(ind+1, "%s = %s;", x, m.String())
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind, "}")
	}
}

// clamps reports whether a 64-bit-or-narrower integer declaration has a
// bound to apply on the read.
func clamps(f *ir.Field) bool {
	_, _, ok := ir.TableRawRange(f)
	return (ok && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed)) || (f.Type.Kind == ir.TBits && f.Type.Width < 64)
}

// readClamp128 is THE 128-BIT CLAMP: a value decoded into an int128,
// uint128 or 128-bit fixed declaration takes this reader's declared range on
// the raw scale (tabletext.WideClamp), compared across both lanes, signed for
// a signed declaration.
func (e *msgEmit) readClamp128(ind int, f *ir.Field, v string) {
	lo, hi, ok := ir.TableRawRange(f)
	if !ok {
		return
	}
	signed := f.Type.Signed
	less := func(a, b string) string { // a < b, as 128-bit values
		if signed {
			return fmt.Sprintf("((%s.hi as! i64) < (%s.hi as! i64) || (%s.hi == %s.hi && %s.lo < %s.lo))", a, b, a, b, a, b)
		}
		return fmt.Sprintf("(%s.hi < %s.hi || (%s.hi == %s.hi && %s.lo < %s.lo))", a, b, a, b, a, b)
	}
	blo, bhi := lanes128(lo)
	tlo, thi := lanes128(hi)
	e.p(ind, "let floor%s = schema_rt::Int128 { lo: %s, hi: %s };", v, blo, bhi)
	e.p(ind, "let ceil%s = schema_rt::Int128 { lo: %s, hi: %s };", v, tlo, thi)
	e.p(ind, "if %s {", less(v, "floor"+v))
	e.p(ind+1, "%s = floor%s;", v, v)
	e.p(ind+1, "r.clamped += 1;")
	e.p(ind, "} else if %s {", less("ceil"+v, v))
	e.p(ind+1, "%s = ceil%s;", v, v)
	e.p(ind+1, "r.clamped += 1;")
	e.p(ind, "}")
}

// getScalar lands ONE value of declaration f from the entry `en` (a local
// holding its vocabulary index), decoded at the sender's shape: a bool's bit,
// a float (an f32 raw or quantized, an f64, or an f32 widened into one), an
// integer as two's-complement lanes then this reader's bounds, an enum by its
// variant reference.
func (e *msgEmit) getScalar(ind int, f *ir.Field, en, dst string, element bool) {
	if enum := enumOf(f); enum != nil {
		e.getEnum(ind, f, enum, dst, element)
		return
	}
	v := e.loopVar()
	switch f.Type.Kind {
	case ir.TBool:
		e.p(ind, "%s = schema_rt::msg_bool(r);", dst)
		return
	case ir.TFloat64:
		// an f64 lands as read, and an f32 widened into it is exact: neither
		// takes this reader's declared range (internal/tablewire scalar)
		e.p(ind, "%s = schema_rt::msg_f64(r, %s);", dst, en)
		return
	case ir.TFloat32:
		if !f.HasFloatRange {
			e.p(ind, "%s = schema_rt::msg_f32(r, %s);", dst, en)
			return
		}
		// THE ONE RULE (§3.3): compared as float64, one past an end clamps
		// and counts, and a -0 lands as +0, uncounted
		e.p(ind, "var q%s = schema_rt::msg_f32(r, %s);", v, en)
		e.p(ind, "if !r.ok { return false; }")
		e.p(ind, "let wide%s: f64 = q%s;", v, v)
		e.p(ind, "if wide%s < schema_rt::fixed_bits_f64(0x%016x) {", v, math.Float64bits(f.FMin))
		e.p(ind+1, "q%s = %s;", v, f32Lit(float32(f.FMin)))
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind, "} else if wide%s > schema_rt::fixed_bits_f64(0x%016x) {", v, math.Float64bits(f.FMax))
		e.p(ind+1, "q%s = %s;", v, f32Lit(float32(f.FMax)))
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind, "} else if wide%s == 0.0 {", v)
		e.p(ind+1, "q%s = 0.0;", v)
		e.p(ind, "}")
		e.p(ind, "%s = q%s;", dst, v)
		return
	}
	e.p(ind, "let lo%s, hi%s = schema_rt::msg_number(r, %s);", v, v, en)
	if e.fx.kindOf(f) == kInt128 || clamps(f) {
		e.p(ind, "if !r.ok { return false; }") // damage is never clamped
	}
	if e.fx.kindOf(f) == kInt128 {
		e.p(ind, "var w%s = schema_rt::Int128 { lo: lo%s, hi: hi%s };", v, v, v)
		e.readClamp128(ind, f, "w"+v)
		e.p(ind, "%s = w%s;", dst, v)
		return
	}
	typ := e.fx.storageType(f)
	if (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed {
		if clamps(f) {
			e.p(ind, "var x%s = lo%s as! i64;", v, v)
			e.readClamp(ind, f, "x"+v)
		} else {
			e.p(ind, "let x%s = lo%s as! i64;", v, v)
		}
		if typ == "i64" {
			e.p(ind, "%s = x%s;", dst, v)
		} else {
			e.p(ind, "%s = x%s as! %s;", dst, v, typ)
		}
		return
	}
	src := "lo" + v
	if clamps(f) {
		e.p(ind, "var x%s = lo%s;", v, v)
		e.readClamp(ind, f, "x"+v)
		src = "x" + v
	}
	if e.wide64(f) {
		e.p(ind, "%s = %s;", dst, src)
	} else {
		e.p(ind, "%s = %s as! %s;", dst, src, typ)
	}
}

// getEnum lands a variant reference: 0 is None, a name this enum cannot name
// is §4's ordinary `unknown` (None lands), and a deprecated variant reads as
// the field's declared default, or an element's 0 (§2.15).
func (e *msgEmit) getEnum(ind int, f *ir.Field, en *ir.Enum, dst string, element bool) {
	v := e.loopVar()
	e.p(ind, "let name%s = schema_rt::msg_name(r);", v)
	e.p(ind, "if !r.ok { return false; }")
	e.p(ind, "if name%s < 0 {", v)
	e.p(ind+1, "%s = 0;", dst)
	e.p(ind, "} else {")
	e.p(ind+1, "match schema_rt::msg_id(r, name%s) {", v)
	for i := range en.Variants {
		land := fmt.Sprint(i + 1)
		if i < len(en.VariantTags) && slices.Contains(en.VariantTags[i], "deprecated") {
			land = "0"
			if !element {
				land = e.enumDefault(f)
			}
		}
		e.p(ind+2, "0x%016x => { %s = %s; },", ir.TableWireId(en.VariantWireName(i)), dst, land)
	}
	e.p(ind+2, "_ => {")
	e.p(ind+3, "%s = 0;", dst)
	e.p(ind+3, "r.unknown += 1;")
	e.p(ind+2, "},")
	e.p(ind+1, "}")
	e.p(ind, "}")
}

// enumDefault is an enum field's declared default as its ordinal.
func (e *msgEmit) enumDefault(f *ir.Field) string {
	if !f.HasDefault || f.DefVariant == "" {
		return "0"
	}
	for i, vn := range enumOf(f).Variants {
		if vn == f.DefVariant {
			return fmt.Sprint(i + 1)
		}
	}
	return "0"
}

// elemType is one element's Goose type, for the scratch an element past this
// reader's bound is read into.
func (e *msgEmit) elemType(el *ir.Field) string {
	switch {
	case e.isStruct(el):
		return e.fx.structType(el.Type.Ref.(*ir.Struct))
	case e.isUnion(el):
		return e.fx.unionType(el.Type.Ref.(*ir.Union))
	case el.Type.Kind == ir.TBool:
		return "bool"
	case el.Type.Kind == ir.TFloat32:
		return "f32"
	case el.Type.Kind == ir.TFloat64:
		return "f64"
	case e.fx.kindOf(el) == kInt128:
		return "schema_rt::Int128"
	}
	return e.fx.storageType(el)
}

// getElement reads one element from the element entry `en` at its element
// kind's own framing: a nested body, a union's arm, or a value.
func (e *msgEmit) getElement(ind int, el *ir.Field, en, dst string) {
	switch {
	case e.isStruct(el), e.isUnion(el):
		e.p(ind, "if !_%sMsgLoad(r, %s) { return false; }", el.Type.Name, dst)
	default:
		e.getScalar(ind, el, en, dst, true)
	}
}

// getValue lands a field's payload from the entry `en`: text (kept to the
// reader's bound, the excess counted clamped), an array's elements (the
// excess past the bound stepped over by the announced width, or read into a
// scratch and dropped where each element resolves something), a keyed array's
// present slots, or one element.
func (e *msgEmit) getValue(ind int, f *ir.Field, en string, pl place) {
	el := elemOf(f)
	v := e.loopVar()
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		e.p(ind, "let n%s = schema_rt::msg_text_len(r, %s);", v, en)
		e.p(ind, "if !r.ok { return false; }")
		e.p(ind, "let at%s = r.pos >> 3;", v)
		if f.Type.Kind == ir.TString {
			e.p(ind, "if !schema_rt::fixed_utf8_ok(r.b, at%s, n%s) { return schema_rt::msg_damage(r); }", v, v)
			e.p(ind, "let k%s = schema_rt::msg_utf8_keep(r.b, at%s, n%s, %d);", v, v, v, f.Type.Size)
		} else {
			e.p(ind, "var k%s = n%s;", v, v)
			e.p(ind, "if k%s > %d { k%s = %d; }", v, f.Type.Size, v, f.Type.Size)
		}
		e.p(ind, "if k%s < n%s { r.clamped += 1; }", v, v)
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < k%s {", i, v)
		e.p(ind+1, "%s[%s] = r.b[at%s + %s];", pl.val, i, v, i)
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
		e.p(ind, "%s = k%s as! i32;", pl.length, v)
		e.p(ind, "r.pos += n%s * 8;", v)
	case f.Type.Kind == ir.TWString:
		e.p(ind, "let n%s = schema_rt::msg_wide_len(r, %s);", v, en)
		e.p(ind, "if !r.ok { return false; }")
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
		slots := slotCount(f)
		e.p(ind, "let n%s = schema_rt::msg_keyed_count(r, %s);", v, en)
		e.p(ind, "let el%s = schema_rt::msg_inner(r, %s);", v, en)
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < n%s {", i, v)
		e.p(ind+1, "if !r.ok { return false; }")
		e.p(ind+1, "let key%s = schema_rt::msg_name(r);", v)
		e.p(ind+1, "if key%s < 0 { return schema_rt::msg_damage(r); }", v)
		e.p(ind+1, "match schema_rt::msg_id(r, key%s) {", v)
		for k := range f.KeyEnumRef.Variants {
			id := ir.TableWireId(f.KeyEnumRef.VariantWireName(k))
			e.p(ind+2, "0x%016x => {", id)
			if int64(k) < slots {
				e.getElement(ind+3, el, "el"+v, pl.at(fmt.Sprint(k)).val)
			} else {
				// a key past this build's slots is dropped and counted clamped
				e.p(ind+3, "r.clamped += 1;")
				e.p(ind+3, "var sc: %s = %s;", e.elemType(el), e.scratchInit(el))
				e.getElement(ind+3, el, "el"+v, "sc")
			}
			e.p(ind+2, "},")
		}
		// a key this enum cannot name drops its element, read all the same
		e.p(ind+2, "_ => {")
		e.p(ind+3, "r.unknown += 1;")
		e.p(ind+3, "var sc: %s = %s;", e.elemType(el), e.scratchInit(el))
		e.getElement(ind+3, el, "el"+v, "sc")
		e.p(ind+2, "},")
		e.p(ind+1, "}")
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
	case f.Array != ir.ArrayNone:
		e.p(ind, "let n%s = schema_rt::msg_count(r, %s);", v, en)
		e.p(ind, "if !r.ok { return false; }")
		e.p(ind, "let el%s = schema_rt::msg_inner(r, %s);", v, en)
		e.p(ind, "var k%s = n%s;", v, v)
		e.p(ind, "if k%s > %d {", v, f.ArrayBound)
		e.p(ind+1, "k%s = %d;", v, f.ArrayBound)
		e.p(ind+1, "r.clamped += 1;")
		e.p(ind, "}")
		// THE SURPLUS OF A FIXED-WIDTH ELEMENT IS ARITHMETIC (§3.3); an
		// element that resolves something is read into a scratch and dropped
		e.p(ind, "let run%s = schema_rt::msg_run_bits(r, %s);", v, en)
		e.p(ind, "var walk%s = n%s;", v, v)
		e.p(ind, "if run%s >= 0 && walk%s > k%s { walk%s = k%s; }", v, v, v, v, v)
		i := e.loopVar()
		e.p(ind, "var %s: i64 = 0;", i)
		e.p(ind, "while %s < walk%s {", i, v)
		e.p(ind+1, "if !r.ok { return false; }")
		if e.isStruct(el) || e.isUnion(el) || enumOf(el) != nil {
			e.p(ind+1, "if %s < k%s {", i, v)
			e.getElement(ind+2, el, "el"+v, pl.at(i).val)
			e.p(ind+1, "} else {")
			e.p(ind+2, "var sc: %s = %s;", e.elemType(el), e.scratchInit(el))
			e.getElement(ind+2, el, "el"+v, "sc")
			e.p(ind+1, "}")
		} else {
			// a value element's width is announced, so the walk stops at k
			e.getElement(ind+1, el, "el"+v, pl.at(i).val)
		}
		e.p(ind+1, "%s += 1;", i)
		e.p(ind, "}")
		e.p(ind, "if walk%s < n%s && !schema_rt::msg_skip_run(r, (n%s - walk%s) as! u64, run%s) { return schema_rt::msg_damage(r); }", v, v, v, v, v)
		if f.Array == ir.ArrayCounted {
			e.p(ind, "%sCount = k%s as! i32;", pl.val, v)
		}
	default:
		e.getElement(ind, el, en, pl.val)
	}
}

// scratchInit is a fresh value of one element's type, for a read whose value
// is dropped.
func (e *msgEmit) scratchInit(el *ir.Field) string {
	switch e.elemType(el) {
	case "bool":
		return "false"
	case "f32", "f64":
		return "0.0"
	}
	if e.isStruct(el) || e.isUnion(el) || e.fx.kindOf(el) == kInt128 {
		return e.elemType(el) + " { .. }"
	}
	return "0"
}

// accept emits the kind check of one entry `en` against this reader's own
// entry for the same id; on a mismatch the entry was counted and stepped over
// and `onSkip` runs.
func (e *msgEmit) accept(ind int, en string, mine ir.TableVocabularyEntry, onSkip string) {
	v := e.loopVar()
	e.p(ind, "let acc%s = schema_rt::msg_accept(r, %s, %d, %d);", v, en, mine.Kind, mine.Shape.Elem)
	e.p(ind, "if acc%s < 0 { return false; }", v)
	e.p(ind, "if acc%s == 2 { %s }", v, onSkip)
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
	e.p(1, "loop {")
	e.p(2, "let en = schema_rt::msg_field(r);")
	e.p(2, "if en < 0 { return r.ok; }")
	e.p(2, "match schema_rt::msg_id(r, en) {")
	for _, f := range st.Fields {
		entry := ir.TableFieldEntry(f)
		e.p(3, "0x%016x => {", entry.Id)
		e.accept(4, "en", entry, "continue;")
		e.getValue(4, f, "en", fieldPlace("v", f))
		if f.Type.Optional {
			e.p(4, "%s = true;", presentOf("v", f))
		}
		e.p(3, "},")
	}
	e.p(3, "_ => {")
	e.p(4, "if !schema_rt::msg_unknown(r, en) { return false; }")
	e.p(3, "},")
	e.p(2, "}")
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
	e.p(1, "let en = schema_rt::msg_arm(r);")
	e.p(1, "if en < 0 { return r.ok; }")
	e.p(1, "match schema_rt::msg_id(r, en) {")
	for i, v := range un.Variants {
		entry := ir.TableArmEntry(v)
		e.p(2, "0x%016x => {", entry.Id)
		// a WIDENED arm is selected and decodes; a mismatched one is None
		e.accept(3, "en", entry, "return true;")
		e.p(3, "v.Type = %d;", i+1)
		switch {
		case v.Void():
		case v.Body():
			e.p(3, "if !_%sMsgLoad(r, v.%s) { return false; }", v.Ref.Name, ir.GoExportName(v.Name))
		default:
			e.getValue(3, armField(v), "en", armPlace("v", v))
		}
		e.p(2, "},")
	}
	// an arm this union cannot name: None, counted unknown, stepped over
	e.p(2, "_ => { return schema_rt::msg_unknown(r, en); },")
	e.p(1, "}")
	e.p(1, "return r.ok;")
	e.p(0, "}\n")
}

// emitRoot writes a fixed root's batch API (§3.3): the announcement as a
// constant, MeasureMessages and SaveMessages under the unit's own vocabulary,
// and LoadMessages against the peer's (schema_rt::announce_read).
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
	e.p(0, "// %sLoadMessages reads ONE batch into values against the peer's vocabulary (AnnounceRead) and", n)
	e.p(0, "// returns the bodies delivered: a capacity short of the wire's count is batch_too_large")
	e.p(0, "// with report.count set and nothing decoded; damage anywhere in the batch discards the")
	e.p(0, "// whole batch, returning 0 with every body at its declared defaults and malformed set;")
	e.p(0, "// a non-finite float refuses the batch (§3.3, §4).")
	e.p(0, "fn %sLoadMessages(values: %s[:], voc: const schema_rt::MsgVocabulary&, data: const u8[:], report: schema_rt::MsgReport&) -> i64 {", n, typ)
	e.p(1, "report = schema_rt::MsgReport { .. };")
	e.p(1, "if data.len < 1 {")
	e.p(2, "report.malformed = true;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "if data[0] != schema_rt::MsgForm {")
	e.p(2, "report.reason = schema_rt::MsgRefuseNewerForm;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "if !voc.announced {")
	e.p(2, "report.reason = schema_rt::MsgRefuseNoVocabulary;")
	e.p(2, "return -1;")
	e.p(1, "}")
	e.p(1, "var r = schema_rt::MsgReader { b: data, pos: 8, end: data.len * 8, voc: voc, .. };")
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
	e.p(3, "break;")
	e.p(2, "}")
	e.p(2, "i += 1;")
	e.p(1, "}")
	e.p(1, "report.unknown = r.unknown;")
	e.p(1, "report.kind_mismatch = r.kind_mismatch;")
	e.p(1, "report.widened = r.widened;")
	e.p(1, "report.clamped = r.clamped;")
	e.p(1, "if !report.malformed && (!schema_rt::msg_align_read(r) || r.pos != r.end) { report.malformed = true; }")
	e.p(1, "if report.malformed {")
	e.p(2, "var j: i64 = 0;")
	e.p(2, "while j < count {")
	e.p(3, "values[j] = %s { .. };", typ)
	e.p(3, "j += 1;")
	e.p(2, "}")
	e.p(2, "return 0;")
	e.p(1, "}")
	e.p(1, "return count;")
	e.p(0, "}\n")
}
