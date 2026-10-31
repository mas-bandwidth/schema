// THE MESSAGE FORM'S BATCH ON THE RUST LEG (docs/SPEC-TABLES.md §3.3).
//
// "A BATCH OF BODIES IS THE UNIT THIS FORM READS AND WRITES, and a single
// message is the batch of one" (§3.3). The announcement half (message.go) is
// the vocabulary; this file is the bitpacked bodies under it: each fixed root's
// measure_messages, save_messages and load_messages, and the body walkers of
// every type the root reaches, emitted per file into <base>_message.rs.
//
// THE BYTES ARE THE GO ORACLE's (internal/codegen/gotable messagewrite.go and
// messageread.go), which reproduces the C++ reference's: a body is a run of
// (reference, value) pairs closed by the zero reference, a field at its
// declared default does not ride, every value is spelled by the shape the
// vocabulary entry names, and a reader decodes against the table THE ENDPOINT
// announced, skipping by the endpoint's shape what it cannot name.
//
// The Rust spelling of the Go oracle's names is snake_case (SPEC.md §6.1):
// MeasureMessages, SaveMessages and LoadMessages land as
// `<root>_measure_messages`, `<root>_save_messages` and `<root>_load_messages`
// over the unit's `<Name>Row` records, exactly as the fixed form spells
// `<root>_fixed_load`.
package rusttable

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type msgGen struct {
	u     *ir.Unit
	b     strings.Builder
	n     int
	slots map[string]uint64
}

func (m *msgGen) pf(format string, args ...any) { fmt.Fprintf(&m.b, format, args...) }

func (m *msgGen) tmp(prefix string) string {
	m.n++
	return fmt.Sprintf("%s%d", prefix, m.n)
}

const msgBad = "{ report.malformed = true; return false; }"

// messageModule is <base>_message.rs: the body walkers of every type this
// file declares that a fixed root reaches, and the three batch verbs of every
// fixed root it declares. "`fixed table T` may ride here and a variable table
// may not" (§3.3), so a variable table grows no verb.
func (g *gen) messageModule() []byte {
	reach, _ := unitFixedClosure(g.unit, g.closure)
	var members []*ir.Struct
	for _, st := range g.cookRoots() {
		if reach[st.Name] {
			members = append(members, st)
		}
	}
	if len(members) == 0 {
		return nil
	}
	m := &msgGen{u: g.unit, slots: ir.TableVocabularySlots(g.unit)}
	m.pf("%s", header(g.file.Base, g.unit.Package, "THE MESSAGE FORM's bodies, form byte 2 (docs/SPEC-TABLES.md §3.3)"))
	m.pf(`//
// A body is a run of (reference, value) pairs under the announced vocabulary,
// closed by the zero reference; a field at its declared default does not ride.
// The bodies of one batch are one continuous bit stream, padded to a byte once
// at its end (§3.3).
#![allow(unused_imports)]
#![allow(unused_variables)]
#![allow(unused_mut)]
#![allow(unused_assignments)]
#![allow(unused_labels)]
#![allow(unused_comparisons)]
#![allow(clippy::all)]

// THE ONE REPORT (§4, §3.3): the crate root exports one TableReport, the
// variable runtime's when the unit carries that form and the message
// runtime's when it does not, so the glob names it either way.
use crate::*;

`)
	for _, st := range members {
		m.emitDefaults(st)
		m.emitSaveBody(st)
		m.emitLoadBody(st)
		if st.IsTable && st.FixedDeclared && !st.IsMapEntry() {
			m.emitBatch(st)
		}
	}
	return []byte(m.b.String())
}

// emitDefaults is the declared defaults of one record, the PREFILL image the
// fixed form already lays down (fixedform.go's fixedDefaultImage) scattered
// into a Row: Rust has no by-value Reset, and a body's absent field is its
// declared default (§3.3, SPEC §4.2).
func (m *msgGen) emitDefaults(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	up := strings.ToUpper(snake)
	img := fixedDefaultImage(st)
	m.pf("const %s_MESSAGE_DEFAULTS: [u8; %d] = [", up, len(img))
	for i, v := range img {
		if i%16 == 0 {
			m.pf("\n    ")
		}
		m.pf("0x%02x,", v)
	}
	m.pf("\n];\n\n")
	m.pf("/// %s at its declared defaults (SPEC §4.2): the value a body that carries\n/// no field of it decodes to (§3.3).\n", st.Name)
	m.pf("pub fn %s_message_default() -> %sRow {\n", snake, st.Name)
	m.pf("    let mut value = %sRow::default();\n", st.Name)
	m.pf("    %s_fixed_scatter(&%s_MESSAGE_DEFAULTS, &mut value, &mut TableFixedReport::default());\n", snake, up)
	m.pf("    value\n}\n\n")
}

// ---- the write -------------------------------------------------------------

func (m *msgGen) slot(e ir.TableVocabularyEntry) uint64 {
	s, ok := m.slots[e.Key()]
	if !ok {
		panic("rusttable: message entry outside the unit's vocabulary")
	}
	return s
}

// nameSlot is the own-vocabulary slot of a NAME entry (kind 0) for a variant
// id, which is how an enum value and a keyed slot ride (§3.3); 0 is a name the
// vocabulary does not carry.
func (m *msgGen) nameSlot(id uint64) uint64 {
	for i, e := range ir.TableVocabulary(m.u) {
		if e.Id == id && e.Kind == 0 {
			return uint64(i + 1)
		}
	}
	return 0
}

func msgElementEntry(e ir.TableVocabularyEntry) ir.TableVocabularyEntry {
	return ir.TableVocabularyEntry{Kind: e.Shape.Elem, Shape: *e.Shape.Inner}
}

// msgElement is gotable's messageElement: one element of an array field, as a
// field of its own (a bytes(N) element is a uint8).
func msgElement(f *ir.Field) *ir.Field {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.Type.Optional = false
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		e.Type = ir.FieldType{Kind: ir.TInt, Width: 8}
	}
	return &e
}

func msgShapeLiteral(kind uint8, s ir.TableMessageShape) string {
	lo, hi := uint64(0), uint64(0)
	if s.Base != nil {
		v := new(big.Int).Set(s.Base)
		v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
		lo = new(big.Int).And(v, new(big.Int).SetUint64(math.MaxUint64)).Uint64()
		hi = new(big.Int).Rsh(v, 64).Uint64()
	}
	if kind < 18 {
		hi = 0
	}
	count, delta, _ := ir.TableMessageQuantization(s)
	bits := max(ir.TableMessageValueBits(kind, s), 0)
	return fmt.Sprintf("TableMessageShape { packing: %d, bits: %d, base_lo: 0x%x, base_hi: 0x%x, min: %d, max: %d, elem: %d, qmin: f32::from_bits(0x%x), qdelta: f32::from_bits(0x%x), qcount: %d }",
		s.Packing, bits, lo, hi, s.Min, s.Max, s.Elem, math.Float32bits(s.QMin), math.Float32bits(delta), count)
}

func (m *msgGen) emitSaveBody(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	m.pf("/// One %s body (§3.3): each field off its declared default, then the zero\n/// reference.\n", st.Name)
	m.pf("pub fn %s_save_message_body(w: &mut TableBitWriter, value: &%sRow) -> bool {\n", snake, st.Name)
	m.pf("    let d = %s_message_default();\n", snake)
	for _, f := range st.Fields {
		expr := "value." + f.Name
		ind := "        "
		m.pf("    {\n")
		if f.Type.Optional {
			m.pf("%sif %s_present {\n", ind, expr)
		}
		m.storageCheck(f, expr, ind)
		rides := "true"
		if !f.Type.Optional {
			rides = m.rides(f, expr, "d."+f.Name, ind)
		}
		entry := ir.TableFieldEntry(f)
		m.pf("%sif %s {\n%s    w.put(%d, TABLE_MESSAGE_REF_BITS);\n", ind, rides, ind, m.slot(entry))
		m.writeValue(f, entry, expr, "d."+f.Name, ind+"    ")
		m.pf("%s}\n", ind)
		if f.Type.Optional {
			m.pf("%s}\n", ind)
		}
		m.pf("    }\n")
	}
	m.pf("    w.put(0, TABLE_MESSAGE_REF_BITS);\n    !w.overflow\n}\n\n")
}

// storageCheck refuses a used length or a live count outside its storage,
// which no wire can spell (gotable emitStorageCheck).
func (m *msgGen) storageCheck(f *ir.Field, expr, ind string) {
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		m.pf("%sif %s_length < 0 || %s_length > %d { return false; }\n", ind, expr, expr, f.Type.Size)
	case f.Array == ir.ArrayCounted:
		m.pf("%sif %s_count < 0 || %s_count > %d { return false; }\n", ind, expr, expr, f.ArrayBound)
	}
}

// rides is whether a field is OFF its declared default and so rides the body
// (§3.3: the message form elides exactly as the file form does, SPEC §4.2);
// gotable's emitMessageRides and emitFieldRides.
func (m *msgGen) rides(f *ir.Field, expr, dexpr, ind string) string {
	if f.KeyEnum != "" {
		return m.keyedPairs(f, expr, dexpr, ind) + " > 0"
	}
	if isStructRef(f.Type) && f.Array == ir.ArrayNone {
		probe := m.tmp("probe")
		m.pf("%slet mut %s = TableBitWriter::measure();\n%sif !%s_save_message_body(&mut %s, &%s) { return false; }\n", ind, probe, ind, ir.RustSnake(f.Type.Name), probe, expr)
		return probe + ".bits > TABLE_MESSAGE_REF_BITS as u64"
	}
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		// TEXT AND BYTES RIDE WHEN NON-EMPTY, declared default or not: the
		// C++ reference's rule (cpptable messagecodec.go, `value.x_length >
		// 0`) and the repository's codec's (tablewire messageencode.go,
		// `fv.Count == 0` elides). gotable's byteValueRides elides a string AT
		// its declared default instead, which is the file form's rule and
		// not this form's; the reference is the one followed here.
		return expr + "_length > 0"
	case f.Array == ir.ArrayCounted:
		return expr + "_count > 0"
	case f.Array == ir.ArrayFixed:
		if isStructRef(f.Type) {
			return "true"
		}
		if isUnionRef(f.Type) {
			return fmt.Sprintf("%s.iter().any(|x| x.tag() != 0)", expr)
		}
		return fmt.Sprintf("(0..%d).any(|i| %s[i] != %s[i])", f.ArrayBound, expr, dexpr)
	case isUnionRef(f.Type):
		return expr + ".tag() != 0"
	}
	return fmt.Sprintf("%s != %s", expr, dexpr)
}

// keyedPairs counts the slots of a keyed array that ride (§3.3).
func (m *msgGen) keyedPairs(f *ir.Field, expr, dexpr, ind string) string {
	pairs := m.tmp("pairs")
	m.pf("%slet mut %s = 0u64;\n%sfor i in 0..%d {\n", ind, pairs, ind, f.ArrayBound)
	e := msgElement(f)
	cond := m.rides(e, expr+"[i]", dexpr+"[i]", ind+"    ")
	m.pf("%s    if %s { %s += 1; }\n%s}\n", ind, cond, pairs, ind)
	return pairs
}

func isStructRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

func isUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

func msgEnum(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// writeValue is one value by its entry's shape (gotable emitMessageValue).
func (m *msgGen) writeValue(f *ir.Field, entry ir.TableVocabularyEntry, expr, dexpr, ind string) {
	switch {
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = expr + "_count"
		}
		if f.Type.Kind == ir.TBytes {
			count = expr + "_length"
		}
		loop := count
		if f.KeyEnum != "" {
			count = m.keyedPairs(f, expr, dexpr, ind)
			loop = fmt.Sprint(f.ArrayBound)
		}
		m.pf("%sw.put((%s as u64).wrapping_sub(%d), %d);\n", ind, count, entry.Shape.Min, ir.TableMessageCountBits(entry.Shape))
		if ir.TableMessageAligns(entry.Kind, entry.Shape) {
			m.pf("%sw.align();\n", ind)
		}
		m.pf("%sfor i in 0..(%s as usize) {\n", ind, loop)
		e := msgElement(f)
		if f.KeyEnum != "" {
			cond := m.rides(e, expr+"[i]", dexpr+"[i]", ind+"    ")
			m.pf("%s    if !(%s) { continue; }\n%s    let slot: u64 = match i + 1 {", ind, cond, ind)
			for i := range f.KeyEnumRef.Variants {
				m.pf(" %d => %d,", i+1, m.nameSlot(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))
			}
			m.pf(" _ => 0 };\n%s    if slot == 0 { return false; }\n%s    w.put(slot, TABLE_MESSAGE_REF_BITS);\n", ind, ind)
		}
		m.writeValue(e, msgElementEntry(entry), expr+"[i]", dexpr+"[i]", ind+"    ")
		m.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		m.pf("%sw.put(%s_length as u64, %d);\n%sw.raw(&%s[..%s_length as usize]);\n", ind, expr, ir.TableMessageCountBits(entry.Shape), ind, expr, expr)
	case f.Type.Kind == ir.TWString:
		m.pf("%sw.put(%s_length as u64, %d);\n%sfor i in 0..(%s_length as usize) { w.put(%s[i] as u64, 16); }\n", ind, expr, ir.TableMessageCountBits(entry.Shape), ind, expr, expr)
	case isStructRef(f.Type):
		m.pf("%sif !%s_save_message_body(w, &%s) { return false; }\n", ind, ir.RustSnake(f.Type.Name), expr)
	case isUnionRef(f.Type):
		un := f.Type.Ref.(*ir.Union)
		m.pf("%smatch %s.tag() {\n%s    0 => w.put(0, TABLE_MESSAGE_REF_BITS),\n", ind, expr, ind)
		for i, v := range un.Variants {
			arm := ir.TableArmEntry(v)
			m.pf("%s    %d => {\n%s        w.put(%d, TABLE_MESSAGE_REF_BITS);\n", ind, i+1, ind, m.slot(arm))
			if !v.Void() {
				local := m.tmp("arm")
				m.pf("%s        let Some(%s) = %s.%s() else { return false; };\n", ind, local, expr, v.Name)
				armExpr := local
				if _, ok := unionArmCompanionType(un, v); ok {
					armExpr = local + ".value"
				}
				af := *v.F
				m.writeValue(&af, arm, armExpr, "", ind+"        ")
			}
			m.pf("%s    }\n", ind)
		}
		m.pf("%s    _ => return false,\n%s}\n", ind, ind)
	case msgEnum(f) != nil:
		e := msgEnum(f)
		m.pf("%smatch %s {\n%s    0 => w.put(0, TABLE_MESSAGE_REF_BITS),\n", ind, expr, ind)
		for i := range e.Variants {
			if s := m.nameSlot(ir.TableWireId(e.VariantWireName(i))); s != 0 {
				m.pf("%s    %d => w.put(%d, TABLE_MESSAGE_REF_BITS),\n", ind, i+1, s)
			}
		}
		m.pf("%s    _ => return false,\n%s}\n", ind, ind)
	case entry.Kind == 1:
		m.pf("%sw.put(if %s { 1 } else { 0 }, 1);\n", ind, expr)
	default:
		lo, hi := expr+" as u64", "0"
		switch {
		case f.Type.Kind == ir.TFloat32:
			lo = expr + ".to_bits() as u64"
		case f.Type.Kind == ir.TFloat64:
			lo = expr + ".to_bits()"
		case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Width > 64:
			lo = expr + ".0 as u64"
			hi = "(" + expr + ".0 >> 64) as u64"
		case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed:
			lo = expr + " as i64 as u64"
		}
		m.pf("%stable_message_write_scalar(w, &%s, %s, %s);\n", ind, msgShapeLiteral(entry.Kind, entry.Shape), lo, hi)
	}
}

// ---- the read --------------------------------------------------------------

func (m *msgGen) emitLoadBody(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	m.pf("/// One %s body against the ENDPOINT's vocabulary (§3.3): a field this build\n/// cannot name is skipped by the endpoint's shape and counted unknown, a kind the\n/// declaration widened is decoded exactly and counted widened, and any other\n/// kind is a kind mismatch, skipped.\n", st.Name)
	m.pf("pub fn %s_load_message_body(r: &mut TableBitReader, v: &TableVocabulary, report: &mut TableReport, value: &mut %sRow) -> bool {\n", snake, st.Name)
	m.pf("    *value = %s_message_default();\n", snake)
	m.pf("    loop {\n")
	m.pf("        let Some(reference) = r.get(v.ref_bits) else %s;\n", msgBad)
	m.pf("        if reference == 0 { return true; }\n")
	m.pf("        let Some(entry) = v.entry(reference).copied() else %s;\n", msgBad)
	m.pf("        if entry.id >= 0xffff_ffff_ffff_fffd %s\n", msgBad)
	m.pf("        match entry.id {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		ind := "                "
		m.pf("            0x%016x => {\n", mine.Id)
		label := m.tmp("'f")
		m.pf("%slet mut widened = false;\n%s%s: {\n", ind, ind, label)
		m.compat(f, mine, "entry", "", label, ind+"    ")
		m.readValue(f, "value."+f.Name, "entry", ind+"    ")
		if f.Type.Optional {
			m.pf("%s    value.%s_present = true;\n", ind, f.Name)
		}
		m.pf("%s    if widened { report.widened += 1; }\n%s}\n", ind, ind)
		m.pf("            }\n")
	}
	m.pf("            _ => {\n                report.unknown += 1;\n                if !table_message_skip(r, v, &entry, 0) %s\n            }\n", msgBad)
	m.pf("        }\n    }\n}\n\n")
}

func wireWidenable(kind int) bool {
	return kind >= 3 && kind <= 5 || kind >= 7 && kind <= 9 || kind == 11 || kind == 18 || kind == 19
}

// compat is gotable's emitMessageCompatibility: the endpoint's kind is the field's,
// or one the field widens (§4), or a kind mismatch skipped by the endpoint's shape.
func (m *msgGen) compat(f *ir.Field, mine ir.TableVocabularyEntry, entry, reset, label, ind string) {
	compat := ""
	if f != nil && wireWidenable(int(mine.Kind)) {
		compat = fmt.Sprintf("%s.shape.elem == %d && table_message_widens(%s.kind, %d)", entry, mine.Shape.Elem, entry, mine.Kind)
	}
	if f != nil && mine.Kind == 14 && wireWidenable(int(mine.Shape.Elem)) {
		compat = fmt.Sprintf("%s.kind == 14 && table_message_widens(%s.shape.elem, %d)", entry, entry, mine.Shape.Elem)
	}
	m.pf("%sif %s.kind != %d || %s.shape.elem != %d {\n", ind, entry, mine.Kind, entry, mine.Shape.Elem)
	inner := ind + "    "
	if compat != "" {
		m.pf("%sif %s {\n%s    widened = true;\n%s} else {\n", inner, compat, inner, inner)
		inner += "    "
	}
	if reset != "" {
		m.pf("%s%s;\n", inner, reset)
	}
	m.pf("%sreport.kind_mismatch += 1;\n%sif !table_message_skip(r, v, &%s, 0) %s\n%sbreak %s;\n", inner, inner, entry, msgBad, inner, label)
	if compat != "" {
		m.pf("%s    }\n", ind)
	}
	m.pf("%s}\n", ind)
}

func rustBig(v *big.Int, signed bool) string {
	if signed {
		min := new(big.Int).Lsh(big.NewInt(1), 127)
		min.Neg(min)
		if v.Cmp(min) == 0 {
			return "i128::MIN"
		}
		return v.String() + "i128"
	}
	return v.String() + "u128"
}

// readValue is one value decoded by the ENDPOINT's entry (gotable
// emitMessageReadValue); entry names a TableMessageEntry in scope.
func (m *msgGen) readValue(f *ir.Field, expr, entry, ind string) {
	switch {
	case f.Type.Kind == ir.TString:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if !r.align() %s\n", ind, msgBad)
		m.pf("%s    let buf = r.buffer;\n%s    let start = (r.offset / 8) as usize;\n", ind, ind)
		m.pf("%s    if !r.skip(n * 8) %s\n", ind, msgBad)
		m.pf("%s    let text = &buf[start..start + n as usize];\n", ind)
		m.pf("%s    if !table_message_utf8(text) %s\n", ind, msgBad)
		m.pf("%s    let kept = table_message_utf8_clamp(text, core::cmp::min(n as usize, %d));\n", ind, f.Type.Size)
		m.pf("%s    if kept < n as usize { report.clamped += 1; }\n", ind)
		m.pf("%s    %s.fill(0);\n%s    %s[..kept].copy_from_slice(&text[..kept]);\n%s    %s_length = kept as i32;\n%s}\n", ind, expr, ind, expr, ind, expr, ind)
	case f.Type.Kind == ir.TBytes:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if !r.align() %s\n", ind, msgBad)
		m.pf("%s    let buf = r.buffer;\n%s    let start = (r.offset / 8) as usize;\n", ind, ind)
		m.pf("%s    if !r.skip(n * 8) %s\n", ind, msgBad)
		m.pf("%s    let kept = core::cmp::min(n as usize, %d);\n", ind, f.Type.Size)
		m.pf("%s    if kept < n as usize { report.clamped += 1; }\n", ind)
		m.pf("%s    %s.fill(0);\n%s    %s[..kept].copy_from_slice(&buf[start..start + kept]);\n%s    %s_length = kept as i32;\n%s}\n", ind, expr, ind, expr, ind, expr, ind)
	case f.Type.Kind == ir.TWString:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if n * 16 > (r.buffer.len() as u64) * 8 - r.offset %s\n", ind, msgBad)
		m.pf("%s    let mut kept = core::cmp::min(n as usize, %d);\n", ind, f.Type.Size)
		m.pf("%s    if kept < n as usize { report.clamped += 1; }\n", ind)
		m.pf("%s    %s.fill(0);\n%s    let mut high = false;\n", ind, expr, ind)
		m.pf("%s    for i in 0..(n as usize) {\n", ind)
		m.pf("%s        let unit = r.get(16).unwrap_or(0);\n", ind)
		m.pf("%s        if unit == 0 || ((0xdc00..=0xdfff).contains(&unit) && !high) || (high && !(0xdc00..=0xdfff).contains(&unit)) %s\n", ind, msgBad)
		m.pf("%s        high = (0xd800..=0xdbff).contains(&unit);\n", ind)
		m.pf("%s        if i < kept { %s[i] = unit as u16; }\n%s    }\n", ind, expr, ind)
		m.pf("%s    if high %s\n", ind, msgBad)
		m.pf("%s    if kept > 0 && (0xd800..=0xdbff).contains(&%s[kept - 1]) { kept -= 1; %s[kept] = 0; }\n", ind, expr, expr)
		m.pf("%s    %s_length = kept as i32;\n%s}\n", ind, expr, ind)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		m.readArray(f, expr, entry, ind)
	case isStructRef(f.Type):
		m.pf("%sif !%s_load_message_body(r, v, report, &mut %s) { return false; }\n", ind, ir.RustSnake(f.Type.Name), expr)
	case isUnionRef(f.Type):
		un := f.Type.Ref.(*ir.Union)
		arm := m.tmp("arm_entry")
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(reference) = r.get(v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s    if reference == 0 {\n%s        %s.set_none();\n%s    } else {\n", ind, ind, expr, ind)
		m.pf("%s        let Some(%s) = v.entry(reference).copied() else %s;\n", ind, arm, msgBad)
		m.pf("%s        if %s.id >= 0xffff_ffff_ffff_fffd || %s.kind == 0 %s\n", ind, arm, arm, msgBad)
		m.pf("%s        match %s.id {\n", ind, arm)
		for _, v := range un.Variants {
			mine := ir.TableArmEntry(v)
			in := ind + "                "
			label := m.tmp("'a")
			m.pf("%s            0x%016x => {\n%slet mut widened = false;\n%s%s: {\n", ind, mine.Id, in, in, label)
			m.compat(v.F, mine, arm, expr+".set_none()", label, in+"    ")
			if v.Void() {
				m.pf("%s    %s.set_%s();\n", in, expr, v.Name)
			} else {
				local := m.tmp("arm")
				m.pf("%s    let mut %s: %s = Default::default();\n", in, local, unionArmRowType(un, v))
				armExpr := local
				if _, ok := unionArmCompanionType(un, v); ok {
					armExpr = local + ".value"
				}
				af := *v.F
				m.readValue(&af, armExpr, arm, in+"    ")
				m.pf("%s    %s.set_%s(%s);\n", in, expr, v.Name, local)
			}
			m.pf("%s    if widened { report.widened += 1; }\n%s}\n%s            }\n", in, in, ind)
		}
		m.pf("%s            _ => {\n%s                %s.set_none();\n%s                report.unknown += 1;\n%s                if !table_message_skip(r, v, &%s, 0) %s\n%s            }\n", ind, ind, expr, ind, ind, arm, msgBad, ind)
		m.pf("%s        }\n%s    }\n%s}\n", ind, ind, ind)
	case msgEnum(f) != nil:
		e := msgEnum(f)
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(reference) = r.get(v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s    if reference == 0 {\n%s        %s = 0;\n%s    } else {\n", ind, ind, expr, ind)
		m.pf("%s        let Some(id) = v.name(reference) else %s;\n", ind, msgBad)
		m.pf("%s        %s = match id {\n", ind, expr)
		for i := range e.Variants {
			m.pf("%s            0x%016x => %d,\n", ind, ir.TableWireId(e.VariantWireName(i)), i+1)
		}
		m.pf("%s            _ => { report.unknown += 1; 0 }\n%s        };\n%s    }\n%s}\n", ind, ind, ind, ind)
	default:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some((lo, hi)) = table_message_read_scalar(r, %s.kind, &%s.shape) else %s;\n", ind, entry, entry, msgBad)
		m.convertScalar(f, expr, entry, ind+"    ")
		m.pf("%s}\n", ind)
	}
}

// convertScalar is the file form's exact clamps applied to a value the endpoint
// spelled at its announced width (gotable emitReadScalar).
func (m *msgGen) convertScalar(f *ir.Field, expr, entry, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		m.pf("%s%s = (lo as u8) != 0;\n", ind, expr)
		return
	case ir.TFloat32, ir.TFloat64:
		if f.Type.Kind == ir.TFloat32 {
			m.pf("%slet mut x = f32::from_bits(lo as u32);\n", ind)
		} else {
			m.pf("%slet mut x = if %s.kind == 10 { f64::from_bits(table_message_widen_f32(lo as u32)) } else { f64::from_bits(lo) };\n", ind, entry)
		}
		if f.HasFloatRange {
			var lo, hi string
			if f.Type.Kind == ir.TFloat32 {
				lo = fmt.Sprintf("f32::from_bits(0x%x)", math.Float32bits(float32(f.FMin)))
				hi = fmt.Sprintf("f32::from_bits(0x%x)", math.Float32bits(float32(f.FMax)))
			} else {
				lo = fmt.Sprintf("f64::from_bits(0x%x)", math.Float64bits(f.FMin))
				hi = fmt.Sprintf("f64::from_bits(0x%x)", math.Float64bits(f.FMax))
			}
			m.pf("%sif x < %s { x = %s; report.clamped += 1; } else if x > %s { x = %s; report.clamped += 1; }\n", ind, lo, lo, hi, hi)
		}
		m.pf("%s%s = x;\n", ind, expr)
		return
	}
	signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
	if signed {
		m.pf("%slet mut x = table_message_signed(%s.kind, lo, hi);\n", ind, entry)
	} else {
		m.pf("%slet mut x = table_message_unsigned(%s.kind, lo, hi);\n", ind, entry)
	}
	if rlo, rhi, ok := ir.TableRawRange(f); ok && f.HasIntRange {
		l, h := rustBig(rlo, signed), rustBig(rhi, signed)
		m.pf("%sif x < %s { x = %s; report.clamped += 1; } else if x > %s { x = %s; report.clamped += 1; }\n", ind, l, l, h, h)
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		max := uint64(1)<<f.Type.Width - 1
		m.pf("%sif x > %d { x = %d; report.clamped += 1; }\n", ind, max, max)
	}
	typ := cookElemType(f)
	switch typ {
	case "TableI128", "TableU128":
		m.pf("%s%s = %s(x);\n", ind, expr, typ)
	default:
		m.pf("%s%s = x as %s;\n", ind, expr, typ)
	}
}

// readArray is gotable's emitMessageReadArray: a count, then the elements,
// a fixed or counted array clamping to its bound and a keyed array landing
// each named slot in its own place.
func (m *msgGen) readArray(f *ir.Field, expr, entry, ind string) {
	el := m.tmp("element")
	elemType := cookElemType(f)
	m.pf("%s{\n", ind)
	m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
	if f.KeyEnum == "" {
		m.pf("%s    if %s.shape.elem == 6 && !r.align() %s\n", ind, entry, msgBad)
	}
	m.pf("%s    let %s = TableMessageEntry { id: 0, kind: %s.shape.elem, shape: %s.element, element: TableMessageShape::default() };\n", ind, el, entry, entry)
	e := msgElement(f)
	if f.KeyEnum != "" {
		m.pf("%s    for _ in 0..n {\n", ind)
		m.pf("%s        let Some(reference) = r.get(v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s        let Some(id) = v.name(reference) else %s;\n", ind, msgBad)
		m.pf("%s        let key: usize = match id {", ind)
		for i := range f.KeyEnumRef.Variants {
			m.pf(" 0x%016x => %d,", ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i+1)
		}
		m.pf(" _ => 0 };\n")
		m.pf("%s        let mut scratch: %s = Default::default();\n", ind, elemType)
		m.pf("%s        let p: &mut %s = if key != 0 { &mut %s[key - 1] } else { report.unknown += 1; &mut scratch };\n", ind, elemType, expr)
		m.readValue(e, "(*p)", el, ind+"        ")
		m.pf("%s    }\n", ind)
	} else {
		m.pf("%s    let keep = core::cmp::min(n, %d);\n", ind, f.ArrayBound)
		m.pf("%s    if keep < n { report.clamped += 1; }\n", ind)
		m.pf("%s    let width = table_message_value_bits(%s.kind, &%s.shape);\n", ind, el, el)
		m.pf("%s    let walk = if width >= 0 { keep } else { n };\n", ind)
		m.pf("%s    for i in 0..walk {\n", ind)
		m.pf("%s        let mut scratch: %s = Default::default();\n", ind, elemType)
		m.pf("%s        let p: &mut %s = if i < keep { &mut %s[i as usize] } else { &mut scratch };\n", ind, elemType, expr)
		m.readValue(e, "(*p)", el, ind+"        ")
		m.pf("%s    }\n", ind)
		m.pf("%s    if walk < n && !r.skip((n - walk) * width as u64) %s\n", ind, msgBad)
		if f.Array == ir.ArrayCounted {
			m.pf("%s    %s_count = keep as i32;\n", ind, expr)
		}
	}
	m.pf("%s}\n", ind)
}

// ---- the batch -------------------------------------------------------------

func (m *msgGen) emitBatch(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	row := st.Name + "Row"
	m.pf(`/// MeasureMessages (§3.3): the byte count SaveMessages writes for this batch,
/// or -1. "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME".
pub fn %[1]s_measure_messages(values: &[%[2]s], report: &mut TableReport) -> i64 {
    if values.is_empty() || values.len() > TABLE_MESSAGE_BATCH_MAX {
        report.refuse(TableReason::BatchTooLarge);
        return -1;
    }
    let mut w = TableBitWriter::measure();
    for value in values {
        if !%[1]s_save_message_body(&mut w, value) {
            return -1;
        }
    }
    w.align();
    if w.overflow {
        return -1;
    }
    2 + (w.bits / 8) as i64
}

/// SaveMessages (§3.3): the form byte, M - 1, then every body as one continuous
/// bit stream padded to a byte once at its end. The byte count, or -1 for a
/// refusal or a buffer too short.
pub fn %[1]s_save_messages(values: &[%[2]s], buffer: &mut [u8], report: &mut TableReport) -> i64 {
    if values.is_empty() || values.len() > TABLE_MESSAGE_BATCH_MAX {
        report.refuse(TableReason::BatchTooLarge);
        return -1;
    }
    if buffer.len() < 2 {
        return -1;
    }
    buffer[0] = TABLE_WIRE_MESSAGE_FORM;
    buffer[1] = (values.len() - 1) as u8;
    let mut w = TableBitWriter::new(&mut buffer[2..]);
    for value in values {
        if !%[1]s_save_message_body(&mut w, value) {
            return -1;
        }
    }
    w.align();
    if w.overflow {
        return -1;
    }
    2 + (w.bits / 8) as i64
}

/// LoadMessages (§3.3): one batch into the caller's storage against the ENDPOINT's
/// announced vocabulary. The count of bodies delivered and whether the batch
/// read whole. "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1"; "M ABOVE THE
/// CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME", with the count set
/// to the wire's M; "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY
/// NAME"; and "BYTES AFTER THE PAD ARE MALFORMED".
pub fn %[1]s_load_messages(values: &mut [%[2]s], vocabulary: &TableVocabulary, data: &[u8], report: &mut TableReport) -> (usize, bool) {
    let Some(count) = table_message_batch_open(vocabulary, data, report) else {
        return (0, false);
    };
    if count > values.len() {
        return (count, report.refuse(TableReason::BatchTooLarge));
    }
    let mut r = TableBitReader::new(&data[2..]);
    for (i, value) in values.iter_mut().take(count).enumerate() {
        if !%[1]s_load_message_body(&mut r, vocabulary, report, value) {
            return (i, false);
        }
    }
    (count, table_message_batch_close(&mut r, report))
}

`, snake, row)
}

// messageBatchRuntime is the batch's shared Rust runtime (§3.3): the bit
// stream, the generic skip a reader uses for a field it cannot name, the
// scalar spelling by shape, and the batch's open and close.
const messageBatchRuntime = `
// ---- the batch --------------------------------------------------------------

/// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME" (§3.3): a batch's body
/// count rides in one byte as M - 1.
pub const TABLE_MESSAGE_BATCH_MAX: usize = 256;

/// THE BIT WRITER, the packet wire's low-bit-first order (§3.3). Measuring
/// walks the identical stream, alignment included, without a buffer. It WRITES
/// the caller's bytes and never ORs into them.
pub struct TableBitWriter<'a> {
    pub buffer: &'a mut [u8],
    pub bits: u64,
    pub measuring: bool,
    pub overflow: bool,
}

impl<'a> TableBitWriter<'a> {
    pub fn new(buffer: &'a mut [u8]) -> Self {
        TableBitWriter { buffer, bits: 0, measuring: false, overflow: false }
    }

    pub fn measure() -> TableBitWriter<'static> {
        TableBitWriter { buffer: Default::default(), bits: 0, measuring: true, overflow: false }
    }

    pub fn put(&mut self, value: u64, n: u32) {
        if n > 64 {
            self.overflow = true;
            return;
        }
        if n == 0 {
            return;
        }
        let at = self.bits;
        self.bits += n as u64;
        if self.measuring {
            return;
        }
        if self.bits.div_ceil(8) > self.buffer.len() as u64 {
            self.overflow = true;
            return;
        }
        let value = if n < 64 { value & ((1u64 << n) - 1) } else { value };
        let mut done = 0u32;
        let mut pos = at;
        while done < n {
            let index = (pos >> 3) as usize;
            let shift = (pos & 7) as u32;
            let take = core::cmp::min(8 - shift, n - done);
            let chunk = ((value >> done) as u8) & (((1u16 << take) - 1) as u8);
            let keep = ((1u16 << shift) - 1) as u8;
            self.buffer[index] = (self.buffer[index] & keep) | (chunk << shift);
            done += take;
            pos += take as u64;
        }
    }

    pub fn align(&mut self) {
        let pad = ((8 - (self.bits & 7)) & 7) as u32;
        self.put(0, pad);
    }

    /// Raw bytes on a byte boundary: a string's content (§3.3).
    pub fn raw(&mut self, data: &[u8]) {
        self.align();
        for &b in data {
            self.put(b as u64, 8);
        }
    }
}

/// THE BIT READER over one batch's bodies (§3.3).
pub struct TableBitReader<'a> {
    pub buffer: &'a [u8],
    pub offset: u64,
}

impl<'a> TableBitReader<'a> {
    pub fn new(buffer: &'a [u8]) -> Self {
        TableBitReader { buffer, offset: 0 }
    }

    pub fn get(&mut self, n: u32) -> Option<u64> {
        let total = (self.buffer.len() as u64) * 8;
        if n > 64 || n as u64 > total - self.offset {
            return None;
        }
        let mut value = 0u64;
        for i in 0..n as u64 {
            let at = self.offset + i;
            value |= (((self.buffer[(at >> 3) as usize] >> (at & 7)) & 1) as u64) << i;
        }
        self.offset += n as u64;
        Some(value)
    }

    pub fn skip(&mut self, n: u64) -> bool {
        if n > (self.buffer.len() as u64) * 8 - self.offset {
            return false;
        }
        self.offset += n;
        true
    }

    /// The pad to a byte boundary, which must be zero bits (§3.3).
    pub fn align(&mut self) -> bool {
        let pad = ((8 - (self.offset & 7)) & 7) as u32;
        self.get(pad) == Some(0)
    }
}

/// bits_required(0, n): the width of a value in [0, n], zero for n = 0 (§3.3).
pub fn table_message_bits(n: u64) -> u32 {
    64 - n.leading_zeros()
}

/// A fixed-width kind's storage bytes (§3), zero for a kind with none.
pub fn table_message_kind_width(kind: u8) -> u32 {
    match kind {
        1 | 2 | 6 | 20 | 25 => 1,
        3 | 7 | 21 | 26 => 2,
        4 | 8 | 10 | 22 | 27 => 4,
        5 | 9 | 11 | 23 | 28 => 8,
        18 | 19 | 24 | 29 => 16,
        _ => 0,
    }
}

/// The bits one value of an entry takes on a body (§3.3's shape table): the
/// packed width under a ranged or quantized packing, the storage width
/// otherwise, and -1 for a kind whose value is not one fixed width.
pub fn table_message_value_bits(kind: u8, s: &TableMessageShape) -> i64 {
    match kind {
        0 | 32 => 0,
        1 => 1,
        _ => {
            if (kind == 10 && s.packing == 2) || (s.packing == 1 && ((2..=9).contains(&kind) || (18..=29).contains(&kind))) {
                s.bits as i64
            } else {
                match table_message_kind_width(kind) {
                    0 => -1,
                    w => (w * 8) as i64,
                }
            }
        }
    }
}

/// An array's or a text's count: its offset from the declared minimum, at the
/// width the range [min, max] needs (§3.3).
pub fn table_message_count(r: &mut TableBitReader, s: &TableMessageShape) -> Option<u64> {
    let raw = r.get(table_message_bits(s.max - s.min))?;
    Some(raw + s.min)
}

/// The widening ladder (§4): a narrower integer of the same signedness, or f32
/// into f64, decoded exactly.
pub fn table_message_widens(from: u8, to: u8) -> bool {
    ((2..=5).contains(&from) && ((to > from && to <= 5) || to == 18))
        || ((6..=9).contains(&from) && ((to > from && to <= 9) || to == 19))
        || (from == 10 && to == 11)
}

/// A float32 widened into a float64 bit-exactly, NaN payload included (§4).
pub fn table_message_widen_f32(b: u32) -> u64 {
    if b & 0x7f80_0000 == 0x7f80_0000 {
        return ((b >> 31) as u64) << 63 | 0x7ff0_0000_0000_0000 | ((b & 0x7f_ffff) as u64) << 29;
    }
    (f32::from_bits(b) as f64).to_bits()
}

/// §3's one content rule for a string: well-formed UTF-8 with no zero byte.
pub fn table_message_utf8(text: &[u8]) -> bool {
    core::str::from_utf8(text).is_ok() && !text.contains(&0)
}

/// A clamp of well-formed UTF-8 to at most n bytes, never inside a character.
pub fn table_message_utf8_clamp(text: &[u8], n: usize) -> usize {
    let mut n = n;
    while n > 0 && n < text.len() && text[n] & 0xc0 == 0x80 {
        n -= 1;
    }
    n
}

/// A value the endpoint spelled, sign-extended from its announced kind's width.
pub fn table_message_signed(kind: u8, lo: u64, hi: u64) -> i128 {
    match table_message_kind_width(kind) {
        1 => lo as i8 as i128,
        2 => lo as i16 as i128,
        4 => lo as i32 as i128,
        8 => lo as i64 as i128,
        _ => (((hi as u128) << 64) | lo as u128) as i128,
    }
}

/// A value the endpoint spelled, zero-extended from its announced kind's width.
pub fn table_message_unsigned(kind: u8, lo: u64, hi: u64) -> u128 {
    match table_message_kind_width(kind) {
        1 => lo as u8 as u128,
        2 => lo as u16 as u128,
        4 => lo as u32 as u128,
        8 => lo as u128,
        _ => ((hi as u128) << 64) | lo as u128,
    }
}

/// One scalar by its shape (§3.3): the value bits, the ranged base added back,
/// a quantized step mapped back onto its float32 range — all at the announced
/// storage width, before the file form's clamps.
pub fn table_message_read_scalar(r: &mut TableBitReader, kind: u8, s: &TableMessageShape) -> Option<(u64, u64)> {
    let bits = table_message_value_bits(kind, s);
    if bits < 0 {
        return None;
    }
    let bits = bits as u32;
    let mut lo = r.get(core::cmp::min(bits, 64))?;
    let mut hi = if bits > 64 { r.get(bits - 64)? } else { 0 };
    if s.packing == 1 && kind != 10 {
        let (sum, carry) = lo.overflowing_add(s.base_lo);
        lo = sum;
        hi = hi.wrapping_add(s.base_hi).wrapping_add(carry as u64);
    }
    if s.packing == 2 && kind == 10 {
        // AN INDEX ABOVE THE ENDPOINT's ANNOUNCED count IS BATCH DAMAGE (§3.3,
        // SPEC.md §4.3), checked on the whole wire value before any narrowing
        // and never reconstructed and clamped: the width spells such an index
        // whenever count is not one less than a power of two (eight bits
        // spelling 255 over a count of 200). internal/tablewire's
        // bitDecoder.float32 is the oracle.
        if hi != 0 || lo > s.qcount as u64 {
            return None;
        }
        let index = lo as u32;
        let ratio = index as f32 / s.qcount as f32;
        let scaled = ratio * s.qdelta;
        lo = (scaled + s.qmin).to_bits() as u64;
    }
    Some((lo, hi))
}

/// One scalar by this build's own shape (§3.3): the ranged base taken off, a
/// float32 quantized onto its steps, then the value bits.
pub fn table_message_write_scalar(w: &mut TableBitWriter, s: &TableMessageShape, lo: u64, hi: u64) {
    let (mut lo, mut hi) = (lo, hi);
    if s.packing == 1 {
        let (diff, borrow) = lo.overflowing_sub(s.base_lo);
        lo = diff;
        hi = hi.wrapping_sub(s.base_hi).wrapping_sub(borrow as u64);
    }
    if s.packing == 2 {
        let value = f32::from_bits(lo as u32);
        let mut ratio = (value - s.qmin) / s.qdelta;
        // NaN clamps low, as the negated comparisons this replaces did
        if ratio.is_nan() || ratio < 0.0 {
            ratio = 0.0;
        } else if ratio > 1.0 {
            ratio = 1.0;
        }
        let scaled = ratio * s.qcount as f32;
        lo = core::cmp::min(((scaled + 0.5) as f64).floor() as u32, s.qcount) as u64;
    }
    w.put(lo, core::cmp::min(s.bits, 64) as u32);
    if s.bits > 64 {
        w.put(hi, (s.bits - 64) as u32);
    }
}

/// A tolerant skip of one value by the ENDPOINT's shape (§3.3): what a reader does
/// with a field it cannot name, or one whose kind is not the declaration's.
pub fn table_message_skip(r: &mut TableBitReader, v: &TableVocabulary, e: &TableMessageEntry, depth: u32) -> bool {
    if depth > 128 {
        return false;
    }
    let s = &e.shape;
    match e.kind {
        0 | 32 => true,
        30 => match r.get(v.ref_bits) {
            None => false,
            Some(0) => true,
            Some(reference) => v.name(reference).is_some(),
        },
        13 => table_message_skip_body(r, v, depth + 1),
        // a node index: the fixed form's roots carry no pointer, so a body
        // that spells one names nothing this reader can resolve
        17 => false,
        15 => match r.get(v.ref_bits) {
            None => false,
            Some(0) => true,
            Some(reference) => match v.entry(reference).copied() {
                Some(arm) => arm.id < 0xffff_ffff_ffff_fffd && arm.kind != 0 && table_message_skip(r, v, &arm, depth + 1),
                None => false,
            },
        },
        12 | 33 => {
            let Some(n) = table_message_count(r, s) else { return false };
            let mut width = 16u64;
            if e.kind == 12 {
                width = 8;
                if !r.align() {
                    return false;
                }
            }
            r.skip(n * width)
        }
        31 => {
            if !r.align() {
                return false;
            }
            match r.get(32) {
                Some(n) => r.skip(n * 8),
                None => false,
            }
        }
        14 | 16 => {
            let Some(n) = table_message_count(r, s) else { return false };
            if e.kind == 14 && s.elem == 6 && !r.align() {
                return false;
            }
            let element = TableMessageEntry { id: 0, kind: s.elem, shape: e.element, element: TableMessageShape::default() };
            let width = table_message_value_bits(element.kind, &element.shape);
            if width >= 0 {
                let mut w = width as u64;
                if e.kind == 16 {
                    w += v.ref_bits as u64;
                }
                return r.skip(n * w);
            }
            for _ in 0..n {
                if e.kind == 16 && !r.skip(v.ref_bits as u64) {
                    return false;
                }
                if !table_message_skip(r, v, &element, depth + 1) {
                    return false;
                }
            }
            true
        }
        kind => {
            let bits = table_message_value_bits(kind, s);
            bits >= 0 && r.skip(bits as u64)
        }
    }
}

/// A nested body skipped whole, entry by entry, to its zero reference (§3.3).
pub fn table_message_skip_body(r: &mut TableBitReader, v: &TableVocabulary, depth: u32) -> bool {
    if depth > 128 {
        return false;
    }
    loop {
        match r.get(v.ref_bits) {
            None => return false,
            Some(0) => return true,
            Some(reference) => match v.entry(reference).copied() {
                Some(e) => {
                    if !table_message_skip(r, v, &e, depth) {
                        return false;
                    }
                }
                None => return false,
            },
        }
    }
}

/// The batch's OPEN (§3.3): "the FORM BYTE is a whole byte and is read FIRST",
/// a body from an endpoint that never announced is refused by name, then M - 1.
pub fn table_message_batch_open(v: &TableVocabulary, data: &[u8], report: &mut TableReport) -> Option<usize> {
    if data.is_empty() {
        report.malformed = true;
        return None;
    }
    if data[0] != TABLE_WIRE_MESSAGE_FORM {
        report.refuse(TableReason::NewerForm);
        return None;
    }
    if !v.announced {
        report.refuse(TableReason::NoVocabulary);
        return None;
    }
    if data.len() < 2 {
        report.malformed = true;
        return None;
    }
    Some(data[1] as usize + 1)
}

/// The batch's CLOSE (§3.3): the pad is zero bits and "BYTES AFTER THE PAD ARE
/// MALFORMED".
pub fn table_message_batch_close(r: &mut TableBitReader, report: &mut TableReport) -> bool {
    if !r.align() || r.offset != (r.buffer.len() as u64) * 8 {
        report.malformed = true;
        return false;
    }
    true
}
`
