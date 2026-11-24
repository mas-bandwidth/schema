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

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type msgGen struct {
	u     *ir.Unit
	b     strings.Builder
	n     int
	slots map[string]uint64
	// orig maps a field line the walk holds as a COPY (a union arm, an array's
	// element) back to the declaration, so a non_finite_float refusal names
	// <Owner>.<field> by the declaration and never by a guess (§3.3).
	orig map[*ir.Field]*ir.Field
}

// origOf is the declaration a field line was copied from, or the line itself.
func (m *msgGen) origOf(f *ir.Field) *ir.Field {
	if o, ok := m.orig[f]; ok {
		return o
	}
	return f
}

func (m *msgGen) pf(format string, args ...any) { fmt.Fprintf(&m.b, format, args...) }

// tmp is a fresh numbered name. A binding takes the reserved schema_ spelling
// (docs/SPEC.md §4.6): the message file glob-imports the crate, and a user
// enum named arm7 is a tuple struct no binding may shadow. A loop label ('f,
// 'a) lives in its own namespace and keeps its spelling.
func (m *msgGen) tmp(prefix string) string {
	m.n++
	if strings.HasPrefix(prefix, "'") {
		return fmt.Sprintf("%s%d", prefix, m.n)
	}
	return fmt.Sprintf("schema_%s%d", prefix, m.n)
}

const msgBad = "{ schema_report.damaged = true; return false; }"

// messageModule is <base>_message.rs: the body walkers of every type this
// file declares that a fixed root reaches, and the three batch verbs of every
// fixed root it declares. "`fixed table T` may ride here and a variable table
// may not" (§3.3), so a variable table grows no verb.
func (g *gen) messageModule() []byte {
	reach, _ := unitFixedClosure(g.unit, g.closure)
	var addReach func(st *ir.Struct)
	addReach = func(st *ir.Struct) {
		if st == nil || reach[st.Name] || !g.rowable(st.Name) {
			return
		}
		reach[st.Name] = true
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				addReach(r)
			case *ir.Union:
				for _, v := range r.Variants {
					if v.F != nil && v.F.Type.Kind == ir.TNamed {
						if sub, ok := v.F.Type.Ref.(*ir.Struct); ok {
							addReach(sub)
						}
					}
				}
			}
		}
	}
	for _, f := range g.unit.Files {
		for _, st := range f.Tables {
			if st.FixedDeclared && !ir.TableFixedEmitted(g.unit, st) && g.rowable(st.Name) {
				addReach(st)
			}
		}
	}
	var members []*ir.Struct
	for _, st := range g.cookRoots() {
		if reach[st.Name] {
			members = append(members, st)
		}
	}
	if len(members) == 0 {
		return nil
	}
	m := &msgGen{u: g.unit, slots: tablewire.MessageVocabularySlots(g.unit), orig: map[*ir.Field]*ir.Field{}}
	m.pf("%s", header(g.file.Base, g.unit.Package, "THE MESSAGE FORM's bodies, form byte 2 (docs/SPEC-TABLES.md §3.3)"))
	m.pf(`//
// A body is a run of (reference, value) pairs under the announced vocabulary,
// closed by the zero reference; a field at its declared default does not ride.
// The bodies of one batch are one continuous bit stream, padded to a byte once
// at its end (§3.3).

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
	if !ir.TableFixedEmitted(m.u, st) {
		m.pf("/// %s at its declared defaults (SPEC §4.2): the value a body that carries\n/// no field of it decodes to (§3.3).\n", st.Name)
		m.pf("pub fn %s_message_default() -> %sRow {\n", snake, st.Name)
		var assigns []string
		for _, f := range st.Fields {
			if f.Type.Optional || f.Array != ir.ArrayNone || f.KeyEnum != "" {
				continue
			}
			if f.HasDefault {
				switch f.Type.Kind {
				case ir.TBool:
					if f.DefBool {
						assigns = append(assigns, fmt.Sprintf("        %s: true,\n", f.Name))
					}
				case ir.TInt, ir.TFixed:
					if f.DefInt != nil && f.DefInt.Sign() != 0 {
						// The IR already holds the exact storage value, including
						// the fixed-point scale. A wide row field wraps that value.
						value := f.DefInt.String()
						if f.Type.Width > 64 {
							value = fmt.Sprintf("%s(%s)", cookElemType(f), value)
						}
						assigns = append(assigns, fmt.Sprintf("        %s: %s,\n", f.Name, value))
					}
				case ir.TFloat32, ir.TFloat64:
					if f.DefFloat != 0 {
						assigns = append(assigns, fmt.Sprintf("        %s: %s,\n", f.Name, fixedFloatLit(f.DefFloat)))
					}
				}
			}
		}
		if len(assigns) == 0 {
			m.pf("    %sRow::default()\n}\n\n", st.Name)
			return
		}
		m.pf("    %sRow {\n", st.Name)
		for _, line := range assigns {
			m.pf("%s", line)
		}
		if len(assigns) < len(st.Fields) {
			m.pf("        ..Default::default()\n")
		}
		m.pf("    }\n}\n\n")
		return
	}
	up := strings.ToUpper(snake)
	img := fixedDefaultImage(st)
	m.pf("// This immutable wire image is compiler-settled data of the form (§3.3).\n")
	m.pf("static %s_MESSAGE_DEFAULTS: [u8; %d] = [", up, len(img))
	for i, v := range img {
		if i%16 == 0 {
			m.pf("\n    ")
		}
		m.pf("0x%02x,", v)
	}
	m.pf("\n];\n\n")
	m.pf("/// %s at its declared defaults (SPEC §4.2): the value a body that carries\n/// no field of it decodes to (§3.3).\n", st.Name)
	m.pf("pub fn %s_message_default() -> %sRow {\n", snake, st.Name)
	m.pf("    let mut schema_value = %sRow::default();\n", st.Name)
	m.pf("    %s_fixed_scatter(&%s_MESSAGE_DEFAULTS, &mut schema_value, &mut TableFixedReport::default());\n", snake, up)
	m.pf("    schema_value\n}\n\n")
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
	for i, e := range tablewire.MessageVocabulary(m.u) {
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
	// Generate the field bodies aside, so the default image is emitted only
	// when a ride test reads it: a body whose fields are all optional or
	// always ride carries no unused binding.
	body := &msgGen{u: m.u, slots: m.slots, orig: m.orig, n: m.n}
	for _, f := range st.Fields {
		expr := "schema_value." + f.Name
		ind := "        "
		body.pf("    {\n")
		if f.Type.Optional {
			body.pf("%sif %s_present {\n", ind, expr)
		}
		body.storageCheck(f, expr, ind)
		rides := "true"
		if !f.Type.Optional {
			rides = body.rides(f, expr, "schema_dflt."+f.Name, ind)
		}
		entry := ir.TableFieldEntry(f)
		if rides == "true" {
			body.pf("%sschema_w.put(%d, TABLE_MESSAGE_REF_BITS);\n", ind, body.slot(entry))
			body.writeValue(f, entry, expr, "schema_dflt."+f.Name, ind)
		} else {
			body.pf("%sif %s {\n%s    schema_w.put(%d, TABLE_MESSAGE_REF_BITS);\n", ind, rides, ind, body.slot(entry))
			body.writeValue(f, entry, expr, "schema_dflt."+f.Name, ind+"    ")
			body.pf("%s}\n", ind)
		}
		if f.Type.Optional {
			body.pf("%s}\n", ind)
		}
		body.pf("    }\n")
	}
	m.n = body.n
	generated := body.b.String()
	m.pf("/// One %s body (§3.3): each field off its declared default, then the zero\n/// reference.\n", st.Name)
	if strings.Contains(generated, "for schema_i in 0..") {
		// The emitted ride loop indexes the row's storage by the declared
		// count: an iterator would turn an off-contract short row into a
		// silent short write instead of the indexing failure the contract
		// names.
		m.pf("// The ride loop indexes the row's storage by the declared count: an\n// iterator would turn an off-contract short row into a silent short write.\n")
		m.pf("#[allow(clippy::needless_range_loop)]\n")
	}
	m.pf("pub fn %s_save_message_body(schema_w: &mut TableBitWriter, schema_value: &%sRow) -> bool {\n", snake, st.Name)
	if strings.Contains(generated, "schema_dflt.") {
		m.pf("    let schema_dflt = %s_message_default();\n", snake)
	}
	m.pf("%s", generated)
	m.pf("    schema_w.put(0, TABLE_MESSAGE_REF_BITS);\n    !schema_w.overflow\n}\n\n")
}

// storageCheck holds a used length or a live count outside its storage,
// which no wire can spell (gotable emitStorageCheck). It is a WRITE check,
// so by rule it is a debug_assert! and nothing else: a release build carries
// no write-side check at all (docs/SPEC.md §5.1, an assert leg).
func (m *msgGen) storageCheck(f *ir.Field, expr, ind string) {
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		m.pf("%sdebug_assert!(%s_length >= 0 && %s_length <= %d, \"the declared length is the bound (docs/SPEC-TABLES.md §3.4)\");\n", ind, expr, expr, f.Type.Size)
	case f.Array == ir.ArrayCounted:
		// a counted array's floor is its declared Min (docs/SPEC.md §5.1 row 3)
		m.pf("%sdebug_assert!(%s_count >= %d && %s_count <= %d, \"the declared count is the bound (docs/SPEC-TABLES.md §3.4)\");\n", ind, expr, f.ArrayMin, expr, f.ArrayBound)
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
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		// TEXT AND BYTES ELIDE ONLY AT THEIR DECLARED DEFAULT, length and
		// bytes both (docs/SPEC-TABLES.md §3.3; the fixed engine's
		// internal/tablewire/messageencode.go, messageElides,
		// `bytes.Equal(fv.Cell.Str, f.DefBytes)`). An empty value under a
		// non-empty default rides, and the default itself does not, so the
		// reader's declared default is what an elided field reads. This is
		// gotable's byteValueRides: empty when no default is declared.
		if len(f.DefBytes) == 0 {
			return expr + "_length > 0"
		}
		return fmt.Sprintf("%s_length != %d || %s[..%d] != %s", expr, len(f.DefBytes), expr, len(f.DefBytes), rustByteArray(f.DefBytes))
	case f.Array == ir.ArrayCounted && f.ArrayMin > 0 && !f.Required && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes:
		// A [Min..N]T ARRAY AT ITS BIRTH ELIDES (internal/tablewire
		// messageElides): Min elements, each at its default — a table
		// element by a body that is its terminator alone, a union by None.
		var live string
		switch {
		case isStructRef(f.Type):
			// a body the save refuses is live: the write that follows says so
			live = fmt.Sprintf("{ let mut probe = TableBitWriter::measure(); !%s_save_message_body(&mut probe, &%s[schema_i]) || probe.bits > TABLE_MESSAGE_REF_BITS as u64 }", ir.RustSnake(f.Type.Name), expr)
		case isUnionRef(f.Type):
			live = expr + "[schema_i].tag() != 0"
		default:
			live = m.rides(msgElement(f), expr+"[schema_i]", dexpr+"[schema_i]", ind)
		}
		return fmt.Sprintf("%s_count > 0 && (%s_count != %d || (0..%d).any(|schema_i| %s))", expr, expr, f.ArrayMin, f.ArrayMin, live)
	case f.Array == ir.ArrayCounted:
		return m.countedRides(f, expr, dexpr, ind)
	case f.Array == ir.ArrayFixed:
		if isStructRef(f.Type) {
			return "true"
		}
		if isUnionRef(f.Type) {
			return fmt.Sprintf("%s.iter().any(|schema_x| schema_x.tag() != 0)", expr)
		}
		return fmt.Sprintf("(0..%d).any(|schema_i| %s)", f.ArrayBound, scalarDiffers(f, expr+"[schema_i]", dexpr+"[schema_i]"))
	case isUnionRef(f.Type):
		return expr + ".tag() != 0"
	}
	// a plain float compares bits, so a -0 under a +0 default rides (§3.3)
	return scalarDiffers(f, expr, dexpr)
}

// countedRides is a counted array's ride test, the Go reference's
// messageElides: it elides at count 0, and a [Min..N] array with Min > 0 also
// elides at its birth, count Min with every element at its default
// (docs/SPEC-TABLES.md §3.3, §22.2; SPEC.md §4.2). An element's default is the
// writer's own: a struct whose body measures its terminator alone, a union at
// None (tag 0, an arm with a zero payload rides), and a scalar or enum at the
// element's declared default.
func (m *msgGen) countedRides(f *ir.Field, expr, dexpr, ind string) string {
	born := f.BornCount()
	if born <= 0 {
		return expr + "_count > 0"
	}
	var live string
	switch {
	case isStructRef(f.Type):
		live = m.tmp("live")
		probe := m.tmp("probe")
		m.pf("%slet mut %s = false;\n", ind, live)
		m.pf("%sif %s_count == %d {\n", ind, expr, born)
		m.pf("%s    for schema_e in %s.iter().take(%d) {\n", ind, expr, born)
		m.pf("%s        let mut %s = TableBitWriter::measure();\n", ind, probe)
		m.pf("%s        if !%s_save_message_body(&mut %s, schema_e) { return false; }\n", ind, ir.RustSnake(f.Type.Name), probe)
		m.pf("%s        if %s.bits > TABLE_MESSAGE_REF_BITS as u64 { %s = true; break; }\n", ind, probe, live)
		m.pf("%s    }\n%s}\n", ind, ind)
	case isUnionRef(f.Type):
		live = fmt.Sprintf("%s.iter().take(%d).any(|schema_e| schema_e.tag() != 0)", expr, born)
	default:
		live = fmt.Sprintf("%s.iter().zip(%s.iter()).take(%d).any(|(schema_x, d)| %s)", expr, dexpr, born, scalarDiffers(f, "(*schema_x)", "(*d)"))
	}
	return fmt.Sprintf("%s_count > 0 && (%s_count != %d || %s)", expr, expr, born, live)
}

// scalarWriteCheck is the debug-only half of the message write contract
// (docs/SPEC.md §5.1 rows 1–3 and 8): a ranged integer or a fixed/ufixed raw
// value off its raw min/max, a bits(N) value wider than N, and (at the text
// site) an ill-formed used prefix. A count or length past its storage, an enum
// ordinal or a union tag outside its set, and a NaN or infinity refuse in every
// build elsewhere.
func (m *msgGen) scalarWriteCheck(f *ir.Field, expr, ind string) {
	if a := rustFloatRangeAssert(f, expr, ind); a != "" {
		m.pf("%s", a)
		return
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TFloat32, ir.TFloat64:
		return
	case ir.TNamed:
		if fl, ok := f.Type.Ref.(*ir.Flags); ok && len(fl.Variants) < 64 {
			m.pf("%sdebug_assert!(%s >> %d == 0, \"flags %s bits name no variant\");\n", ind, expr, len(fl.Variants), fl.Name)
		}
		return
	}
	x := expr
	if (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Width > 64 {
		x += ".0"
	}
	if lo, hi, ok := ir.TableRawRange(f); ok {
		minBits, maxBits, _ := storageLimits(f)
		var conds []string
		what := f.Name + " out of range"
		if f.Type.Kind == ir.TFixed {
			what = f.Name + " raw value out of range"
		}
		low, high := lo.Cmp(minBits) > 0, hi.Cmp(maxBits) < 0
		switch {
		case low && high && lo.Cmp(hi) == 0:
			conds = append(conds, fmt.Sprintf("%s == %s", x, lo.String()))
		case low && high && !strings.ContainsAny(x, ".[*"):
			conds = append(conds, fmt.Sprintf("(%s..=%s).contains(&%s)", lo.String(), hi.String(), x))
		default:
			if low {
				conds = append(conds, fmt.Sprintf("%s >= %s", x, lo.String()))
			}
			if high {
				conds = append(conds, fmt.Sprintf("%s <= %s", x, hi.String()))
			}
		}
		if len(conds) > 0 {
			m.pf("%sdebug_assert!(%s, \"%s [%s, %s]\");\n", ind, strings.Join(conds, " && "), what, lo.String(), hi.String())
		}
	}
	if f.Type.Kind == ir.TBits && int64(f.Type.Width) < int64(rustTypeWidth(scalarRust(f))) {
		maxv := (uint64(1) << f.Type.Width) - 1
		m.pf("%sdebug_assert!(%s <= %d, \"%s exceeds bits(%d) width\");\n", ind, x, maxv, f.Name, f.Type.Width)
	}
}

// keyedPairs counts the slots of a keyed array that ride (§3.3).
func (m *msgGen) keyedPairs(f *ir.Field, expr, dexpr, ind string) string {
	pairs := m.tmp("pairs")
	m.pf("%slet mut %s = 0u64;\n%sfor schema_i in 0..%d {\n", ind, pairs, ind, f.ArrayBound)
	e := msgElement(f)
	cond := m.rides(e, expr+"[schema_i]", dexpr+"[schema_i]", ind+"    ")
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

// rustCount types a count expression: a literal carries the suffix, so the
// generated arithmetic casts no constant.
func rustCount(count, typ string) string {
	if count != "" && strings.IndexFunc(count, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		return count + typ
	}
	return "(" + count + " as " + typ + ")"
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
		typedCount := rustCount(count, "u64")
		if f.KeyEnum != "" {
			count = m.keyedPairs(f, expr, dexpr, ind)
			typedCount = count
			loop = fmt.Sprint(f.ArrayBound)
		}
		m.pf("%sschema_w.put(%s.wrapping_sub(%d), %d);\n", ind, typedCount, entry.Shape.Min, ir.TableMessageCountBits(entry.Shape))
		if ir.TableMessageAligns(entry.Kind, entry.Shape) {
			m.pf("%sschema_w.align();\n", ind)
		}
		m.pf("%sfor schema_i in 0..%s {\n", ind, rustCount(loop, "usize"))
		e := msgElement(f)
		if f.KeyEnum != "" {
			cond := m.rides(e, expr+"[schema_i]", dexpr+"[schema_i]", ind+"    ")
			m.pf("%s    if !(%s) { continue; }\n%s    let schema_slot: u64 = match schema_i + 1 {", ind, cond, ind)
			for i := range f.KeyEnumRef.Variants {
				m.pf(" %d => %d,", i+1, m.nameSlot(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))
			}
			m.pf(" _ => 0 };\n%s    if schema_slot == 0 { return false; }\n%s    schema_w.put(schema_slot, TABLE_MESSAGE_REF_BITS);\n", ind, ind)
		}
		m.writeValue(e, msgElementEntry(entry), expr+"[schema_i]", dexpr+"[schema_i]", ind+"    ")
		m.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		m.pf("%sdebug_assert!(table_message_utf8(&%s[..%s_length as usize]), \"%s is ill-formed text\");\n", ind, expr, expr, f.Name)
		m.pf("%sschema_w.put(%s_length as u64, %d);\n%sschema_w.raw(&%s[..%s_length as usize]);\n", ind, expr, ir.TableMessageCountBits(entry.Shape), ind, expr, expr)
	case isStructRef(f.Type):
		m.pf("%sif !%s_save_message_body(schema_w, &%s) { return false; }\n", ind, ir.RustSnake(f.Type.Name), expr)
	case isUnionRef(f.Type):
		un := f.Type.Ref.(*ir.Union)
		m.pf("%smatch %s.tag() {\n%s    0 => schema_w.put(0, TABLE_MESSAGE_REF_BITS),\n", ind, expr, ind)
		for i, v := range un.Variants {
			arm := ir.TableArmEntry(v)
			m.pf("%s    %d => {\n%s        schema_w.put(%d, TABLE_MESSAGE_REF_BITS);\n", ind, i+1, ind, m.slot(arm))
			if !v.Void() {
				local := m.tmp("arm")
				m.pf("%s        let Some(%s) = %s.%s() else { return false; };\n", ind, local, expr, unionRowGetter(un, v.Name))
				armExpr := local
				if _, ok := unionArmCompanionType(un, v); ok {
					armExpr = local + ".value"
				}
				af := *v.F
				if af.Array == ir.ArrayCounted && af.ArrayMin > 0 {
					// a counted arm's floor is its declared Min as a field's
					// is (ruling R10, docs/SPEC.md §5.1 row 3): the count
					// below rides as Min-relative bits no reader accepts
					m.storageCheck(&af, armExpr, ind+"        ")
				}
				m.writeValue(&af, arm, armExpr, "", ind+"        ")
			}
			m.pf("%s    }\n", ind)
		}
		m.pf("%s    _ => return false,\n%s}\n", ind, ind)
	case msgEnum(f) != nil:
		e := msgEnum(f)
		if dep := fixedEnumNotDeprecated(f, expr); dep != "" {
			// A DEPRECATED VARIANT IS NEVER WRITTEN (docs/SPEC-TABLES.md §2.15
			// WRITE): the Row holds the ordinal as plain u8 storage, so the
			// message writer refuses it here, element and scalar alike; Rust
			// write checks are debug asserts (docs/SPEC.md §5.1), as the fixed
			// writer's fixedScalarWriteCheck and the variable writer's enumRef
			// are.
			m.pf("%sdebug_assert!(%s, \"invalid_value: %s holds a deprecated variant\");\n", ind, dep, f.Name)
		}
		m.pf("%smatch %s {\n%s    0 => schema_w.put(0, TABLE_MESSAGE_REF_BITS),\n", ind, expr, ind)
		for i := range e.Variants {
			if s := m.nameSlot(ir.TableWireId(e.VariantWireName(i))); s != 0 {
				m.pf("%s    %d => schema_w.put(%d, TABLE_MESSAGE_REF_BITS),\n", ind, i+1, s)
			}
		}
		m.pf("%s    _ => return false,\n%s}\n", ind, ind)
	case entry.Kind == 1:
		m.pf("%sschema_w.put(if %s { 1 } else { 0 }, 1);\n", ind, expr)
	default:
		m.scalarWriteCheck(f, expr, ind)
		lo, hi := expr+" as u64", "0"
		if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
			// A NaN OR AN INFINITY IS REFUSED, never quantized or held
			// (docs/SPEC-TABLES.md §3.3, both special-values rules): the writer
			// answers false before the quantizer runs, so measure and save both
			// return -1.
			m.pf("%sif !%s.is_finite() { return false; }\n", ind, expr)
		}
		switch {
		case f.Type.Kind == ir.TFloat32:
			lo = expr + ".to_bits() as u64"
		case f.Type.Kind == ir.TFloat64:
			lo = expr + ".to_bits()"
		case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Width > 64:
			lo = expr + ".0 as u64"
			hi = "(" + expr + ".0 >> 64) as u64"
		case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed && cookElemType(f) == "i64":
			lo = expr + " as u64"
		case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed:
			lo = expr + " as i64 as u64"
		case cookElemType(f) == "u64":
			lo = expr
		}
		m.pf("%stable_message_write_scalar(schema_w, &%s, %s, %s);\n", ind, msgShapeLiteral(entry.Kind, entry.Shape), lo, hi)
	}
}

// ---- the read --------------------------------------------------------------

func (m *msgGen) emitLoadBody(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	// The arms are generated aside, so the default image and loop allows are
	// emitted only when the generated arms need them.
	body := &msgGen{u: m.u, slots: m.slots, orig: m.orig, n: m.n}
	for i, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		ind := "                "
		body.pf("            0x%016x => {\n", mine.Id)
		body.pf("%sif schema_same_build && schema_next_field > %d %s // out of the writer's order (§3.3 (b))\n", ind, i, msgBad)
		body.pf("%sschema_next_field = %d;\n", ind, i+1)
		label := body.tmp("'f")
		widens := msgCanWiden(f, mine)
		if widens {
			body.pf("%slet mut schema_widened = false;\n", ind)
		}
		if !f.Type.Optional && !msgQuantizedExempt(f, mine) {
			body.pf("%slet schema_unknown_before = schema_report.unknown;\n", ind)
		}
		body.pf("%s%s: {\n", ind, label)
		body.compat(f, mine, "schema_entry", "", label, ind+"    ")
		nested := isStructRef(f.Type) && f.Array == ir.ArrayNone && f.KeyEnum == "" && !f.Type.Optional
		if nested {
			body.pf("%s    let schema_start = schema_r.offset;\n", ind)
		}
		body.readValue(f, "schema_value."+f.Name, "schema_entry", ind+"    ")
		if f.Type.Optional {
			body.pf("%s    schema_value.%s_present = true;\n", ind, f.Name)
		} else {
			if isUnionRef(f.Type) && f.Array == ir.ArrayNone && f.KeyEnum == "" && !f.Required {
				// A UNION FIELD WRITTEN WITH ARM REFERENCE 0 (§3.3 (c)): None
				// elides, so its explicit spelling is refused at every build.
				// An arm this build cannot name also reads None, and counts
				// one unknown; a None element of an array of unions is
				// positional and reads.
				body.pf("%s    if schema_value.%s.tag() == 0 && schema_report.unknown == schema_unknown_before %s // a union written with arm reference 0 (§3.3 (c))\n", ind, f.Name, msgBad)
			}
			// A FIELD STORED AT ITS DEFAULT (§3.3 (a), §4), at this reader's
			// own build only: the save's own ride test, so a value it would
			// elide is a spelling no conforming writer produces. A field whose
			// read met a name this build cannot resolve (an unknown arm,
			// variant or nested field) reads a default lawfully and is not
			// held to it.
			//
			// A COMPRESSED FLOAT ELIDES BY ITS HELD VALUE, compared with its
			// default before quantization (docs/SPEC-TABLES.md §3.3), so a held
			// value that quantizes onto the default's step still rides and reads
			// back as the default: a quantized field, and a fixed array of
			// quantized elements, is never judged here (internal/tablewire's
			// `quantized` term). A counted array is judged by its count alone. A
			// NESTED BODY is judged by its wire spelling, the terminator alone,
			// never by re-encoding the value it decoded.
			switch {
			case msgQuantizedExempt(f, mine):
			case nested:
				body.pf("%s    if schema_same_build && schema_report.unknown == schema_unknown_before && schema_r.offset - schema_start <= schema_v.ref_bits as u64 %s // a field stored at its default (§3.3 (a))\n", ind, msgBad)
			default:
				body.pf("%s    if schema_same_build && schema_report.unknown == schema_unknown_before {\n", ind)
				rides := body.rides(f, "schema_value."+f.Name, "schema_dflt."+f.Name, ind+"        ")
				body.pf("%s        let schema_rides = %s;\n%s        if !schema_rides %s // a field stored at its default (§3.3 (a))\n", ind, rides, ind, msgBad)
				body.pf("%s    }\n", ind)
			}
		}
		if widens {
			body.pf("%s    if schema_widened { schema_report.widened += 1; }\n", ind)
		}
		body.pf("%s}\n", ind)
		body.pf("            }\n")
	}
	m.n = body.n
	arms := body.b.String()
	m.pf("/// One %s body against the ENDPOINT's vocabulary (§3.3): a field this build\n/// cannot name is skipped by the endpoint's shape and counted unknown, a kind the\n/// declaration widened is decoded exactly and counted widened, and any other\n/// kind is a kind mismatch, skipped.\n", st.Name)
	if strings.Contains(arms, "for schema_i in 0..") {
		m.pf("#[allow(clippy::needless_range_loop)]\n")
	}
	m.pf("pub fn %s_load_message_body(schema_r: &mut TableBitReader, schema_v: &TableVocabulary, schema_report: &mut TableReport, schema_value: &mut %sRow) -> bool {\n", snake, st.Name)
	m.pf("    *schema_value = %s_message_default();\n", snake)
	// A FIELD ID TWICE IN ONE BODY IS DAMAGE (docs/SPEC-TABLES.md §3.3), known
	// or unknown: the ids this body has carried are one bit each by the id's
	// ordinal, whichever slot named it, in a fixed stack set, so the test is
	// constant work per reference and the body allocates nothing.
	m.pf("    let mut schema_seen = TableMessageSeen::default();\n")
	// THE SENDER'S OWN BUILD IS THIS READER'S (docs/SPEC-TABLES.md §3.3 (a),
	// (b); docs/rulings.json reads-refuse-non-canonical): every field's
	// default and every record's field order are digested into the build
	// version (§20.1, §20.2), so there, and only there, this reader's
	// declaration is the sender's. A field the record does not declare, a
	// field out of the declaration order and a field stored at its declared
	// default are spellings no conforming writer produces. Under another build
	// version none of the three is judged. next_field is one past the ordinal
	// of the last declared field this body carried.
	m.pf("    let schema_same_build = schema_v.build_version == BUILD_VERSION;\n")
	m.pf("    let mut schema_next_field = 0usize;\n")
	if strings.Contains(arms, "schema_dflt.") {
		m.pf("    let schema_dflt = %s_message_default();\n", snake)
	}
	m.pf("    loop {\n")
	m.pf("        let Some(schema_reference) = schema_r.get(schema_v.ref_bits) else %s;\n", msgBad)
	m.pf("        if schema_reference == 0 { return true; }\n")
	m.pf("        let Some(schema_entry) = schema_v.entry(schema_reference).copied() else %s;\n", msgBad)
	m.pf("        if schema_entry.id >= 0xffff_ffff_ffff_fffd %s\n", msgBad)
	m.pf("        if !schema_seen.once(schema_v.seen_key(schema_reference)) %s\n", msgBad)
	m.pf("        match schema_entry.id {\n")
	m.pf("%s", arms)
	// A FIELD THE RECORD DOES NOT DECLARE, at this reader's own build, has no
	// place in the sender's record (§3.3 (b)); under another build it is a
	// newer writer's field, skipped and counted (§4).
	m.pf("            _ => {\n                if schema_same_build %s // a field the record does not declare (§3.3 (b))\n                schema_report.unknown += 1;\n                if !table_message_skip_field(schema_r, schema_v, &schema_entry, 0) %s\n            }\n", msgBad, msgBad)
	m.pf("        }\n    }\n}\n\n")
}

// msgQuantizedExempt is whether a field's stored-at-default test is waived
// because its value is quantized (internal/tablewire messagedecode.go's
// `quantized` term): a compressed float, or a fixed or keyed array whose
// element is one. Such a value elides by its held value before quantization
// (docs/SPEC-TABLES.md §3.3), so its decoded value at the default's step is
// lawful. A counted array is judged by its count, which is not quantized.
func msgQuantizedExempt(f *ir.Field, mine ir.TableVocabularyEntry) bool {
	if f.Type.Optional || isUnionRef(f.Type) {
		return false
	}
	if f.Array == ir.ArrayNone && f.KeyEnum == "" {
		return mine.Shape.Packing == ir.TableMessageQuantized
	}
	if f.Array == ir.ArrayCounted && f.KeyEnum == "" && f.ArrayMin == 0 {
		return false // judged by its count alone; a [Min..N] count is never zero
	}
	return mine.Shape.Inner != nil && mine.Shape.Inner.Packing == ir.TableMessageQuantized
}

func wireWidenable(kind int) bool {
	return kind >= 3 && kind <= 5 || kind >= 7 && kind <= 9 || kind == 11 || kind == 18 || kind == 19
}

// msgCanWiden is whether compat can set the widened flag for a field against
// the entry it reads: the field's own kind widens, or an array's element kind
// does (§4). When it cannot, the loader emits neither the flag nor its count,
// so the generated module carries no state an allow would otherwise hide.
func msgCanWiden(f *ir.Field, mine ir.TableVocabularyEntry) bool {
	if f == nil {
		return false
	}
	if mine.Kind == 14 && wireWidenable(int(mine.Shape.Elem)) {
		return true
	}
	return wireWidenable(int(mine.Kind))
}

// msgFullyAssigned is whether readValue assigns the whole slot on every path,
// so a union arm's local needs no default image for an assignment to replace.
func msgFullyAssigned(f *ir.Field) bool {
	if f == nil {
		return false
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		return false
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return false
	}
	return !isStructRef(f.Type) && !isUnionRef(f.Type)
}

// msgNeedsEntry is whether readValue of an array element reads the element's
// vocabulary entry: a struct, a union and an enum decode by their own id and
// shape, so their element entry is never named.
func msgNeedsEntry(f *ir.Field) bool {
	return !isStructRef(f.Type) && !isUnionRef(f.Type) && msgEnum(f) == nil
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
		m.pf("%sif %s {\n%s    schema_widened = true;\n%s} else {\n", inner, compat, inner, inner)
		inner += "    "
	}
	// A FIELD's mismatch is skipped as a field, so a union field's explicit
	// None is damage there too (§3.3 (c)); a union arm (reset names its None)
	// is a positional payload.
	skip := "table_message_skip_field"
	if reset != "" {
		m.pf("%s%s;\n", inner, reset)
		skip = "table_message_skip"
	}
	m.pf("%sschema_report.damaged = true;\n%sif !%s(schema_r, schema_v, &%s, 0) %s\n%sbreak %s;\n", inner, inner, skip, entry, msgBad, inner, label)
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
		m.pf("%s    let Some(schema_n) = table_message_count(schema_r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if !schema_r.align() %s\n", ind, msgBad)
		m.pf("%s    let schema_buf = schema_r.buffer;\n%s    let schema_start = (schema_r.offset / 8) as usize;\n", ind, ind)
		m.pf("%s    if !schema_r.skip(schema_n * 8) %s\n", ind, msgBad)
		m.pf("%s    let schema_text = &schema_buf[schema_start..schema_start + schema_n as usize];\n", ind)
		m.pf("%s    if !table_message_utf8(schema_text) %s\n", ind, msgBad)
		m.pf("%s    let schema_kept = table_message_utf8_clamp(schema_text, core::cmp::min(schema_n as usize, %d));\n", ind, f.Type.Size)
		m.pf("%s    if schema_kept < schema_n as usize { schema_report.damaged = true; }\n", ind)
		m.pf("%s    %s.fill(0);\n%s    %s[..schema_kept].copy_from_slice(&schema_text[..schema_kept]);\n%s    %s_length = schema_kept as i32;\n%s}\n", ind, expr, ind, expr, ind, expr, ind)
	case f.Type.Kind == ir.TBytes:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(schema_n) = table_message_count(schema_r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if !schema_r.align() %s\n", ind, msgBad)
		m.pf("%s    let schema_buf = schema_r.buffer;\n%s    let schema_start = (schema_r.offset / 8) as usize;\n", ind, ind)
		m.pf("%s    if !schema_r.skip(schema_n * 8) %s\n", ind, msgBad)
		m.pf("%s    let schema_kept = core::cmp::min(schema_n as usize, %d);\n", ind, f.Type.Size)
		m.pf("%s    if schema_kept < schema_n as usize { schema_report.damaged = true; }\n", ind)
		m.pf("%s    %s.fill(0);\n%s    %s[..schema_kept].copy_from_slice(&schema_buf[schema_start..schema_start + schema_kept]);\n%s    %s_length = schema_kept as i32;\n%s}\n", ind, expr, ind, expr, ind, expr, ind)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		m.readArray(f, expr, entry, ind)
	case isStructRef(f.Type):
		m.pf("%sif !%s_load_message_body(schema_r, schema_v, schema_report, &mut %s) { return false; }\n", ind, ir.RustSnake(f.Type.Name), expr)
	case isUnionRef(f.Type):
		un := f.Type.Ref.(*ir.Union)
		arm := m.tmp("arm_entry")
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(schema_reference) = schema_r.get(schema_v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s    if schema_reference == 0 {\n%s        %s.set_none();\n%s    } else {\n", ind, ind, expr, ind)
		m.pf("%s        let Some(%s) = schema_v.entry(schema_reference).copied() else %s;\n", ind, arm, msgBad)
		m.pf("%s        if %s.id >= 0xffff_ffff_ffff_fffd || %s.kind == 0 %s\n", ind, arm, arm, msgBad)
		m.pf("%s        match %s.id {\n", ind, arm)
		for _, v := range un.Variants {
			mine := ir.TableArmEntry(v)
			in := ind + "                "
			label := m.tmp("'a")
			widens := msgCanWiden(v.F, mine)
			m.pf("%s            0x%016x => {\n", ind, mine.Id)
			if widens {
				m.pf("%slet mut schema_widened = false;\n", in)
			}
			m.pf("%s%s: {\n", in, label)
			m.compat(v.F, mine, arm, expr+".set_none()", label, in+"    ")
			if v.Void() {
				m.pf("%s    %s.set_%s();\n", in, expr, v.Name)
			} else {
				local := m.tmp("arm")
				if msgFullyAssigned(v.F) {
					m.pf("%s    let %s: %s;\n", in, local, unionArmRowType(un, v))
				} else {
					m.pf("%s    let mut %s: %s = Default::default();\n", in, local, unionArmRowType(un, v))
				}
				armExpr := local
				if _, ok := unionArmCompanionType(un, v); ok {
					armExpr = local + ".value"
				}
				af := *v.F
				m.orig[&af] = v.F
				m.readValue(&af, armExpr, arm, in+"    ")
				m.pf("%s    %s.set_%s(%s);\n", in, expr, v.Name, local)
			}
			if widens {
				m.pf("%s    if schema_widened { schema_report.widened += 1; }\n", in)
			}
			m.pf("%s}\n%s            }\n", in, ind)
		}
		m.pf("%s            _ => {\n%s                %s.set_none();\n%s                schema_report.unknown += 1;\n%s                if !table_message_skip(schema_r, schema_v, &%s, 0) %s\n%s            }\n", ind, ind, expr, ind, ind, arm, msgBad, ind)
		m.pf("%s        }\n%s    }\n%s}\n", ind, ind, ind)
	case msgEnum(f) != nil:
		e := msgEnum(f)
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(schema_reference) = schema_r.get(schema_v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s    if schema_reference == 0 {\n%s        %s = 0;\n%s    } else {\n", ind, ind, expr, ind)
		m.pf("%s        let Some(id) = schema_v.name(schema_reference) else %s;\n", ind, msgBad)
		m.pf("%s        %s = match id {\n", ind, expr)
		for i := range e.Variants {
			m.pf("%s            0x%016x => %d,\n", ind, ir.TableWireId(e.VariantWireName(i)), i+1)
		}
		m.pf("%s            _ => { schema_report.unknown += 1; 0 }\n%s        };\n%s    }\n%s}\n", ind, ind, ind, ind)
	default:
		m.pf("%s{\n", ind)
		hi := "schema_hi"
		if f.Type.Kind == ir.TBool || f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
			hi = "_hi"
		}
		m.pf("%s    let Some((schema_lo, %s)) = table_message_read_scalar(schema_r, %s.kind, &%s.shape) else %s;\n", ind, hi, entry, entry, msgBad)
		m.convertScalar(f, expr, entry, ind+"    ")
		m.pf("%s}\n", ind)
	}
}

// convertScalar is the file form's exact clamps applied to a value the endpoint
// spelled at its announced width (gotable emitReadScalar).
func (m *msgGen) convertScalar(f *ir.Field, expr, entry, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		m.pf("%s%s = (schema_lo as u8) != 0;\n", ind, expr)
		return
	case ir.TFloat32, ir.TFloat64:
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits
		// become a value (docs/SPEC-TABLES.md §3.3), as a file read refuses it:
		// the read names the field and the batch refuses as non_finite_float,
		// so the 0.0 that stands in is never delivered. A DEQUANTIZED VALUE IS
		// A FLOAT READ LIKE ANY OTHER: the peer's announced triple can name a
		// top step past the largest float32, so the finite test cannot ride the
		// announced packing. Every finite float32 widens exactly through the
		// ordinary conversion (§4).
		owner := floatOwnerIn(m.u, m.origOf(f))
		mut := ""
		if f.HasFloatRange {
			mut = "mut "
		}
		if f.Type.Kind == ir.TFloat32 {
			m.pf("%slet %sschema_x = if table_finite_f32(schema_lo as u32) { f32::from_bits(schema_lo as u32) } else { schema_report.non_finite(%q); 0.0 };\n", ind, mut, owner)
		} else {
			m.pf("%slet %sschema_x = if %s.kind == 10 { if table_finite_f32(schema_lo as u32) { f64::from(f32::from_bits(schema_lo as u32)) } else { schema_report.non_finite(%q); 0.0 } } else if table_finite_f64(schema_lo) { f64::from_bits(schema_lo) } else { schema_report.non_finite(%q); 0.0 };\n", ind, mut, entry, owner, owner)
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
			// ONE PAST AN END REFUSES out_of_range BY NAME (§3.3), as the Go
			// reference's message read does.
			if lo == hi {
				m.pf("%sif schema_x != %s { schema_x = %s; schema_report.out_of_range(%q); }\n", ind, lo, lo, owner)
			} else {
				m.pf("%sif schema_x < %s { schema_x = %s; schema_report.out_of_range(%q); } else if schema_x > %s { schema_x = %s; schema_report.out_of_range(%q); }\n", ind, lo, lo, owner, hi, hi, owner)
			}
			// a compressed float's -0 lands as +0 (docs/rulings.json
			// fixed-compressed-negzero): a plain float32 announced for it
			// carries the sign bit
			m.pf("%sif schema_x == 0.0 { schema_x = 0.0; }\n", ind)
		}
		m.pf("%s%s = schema_x;\n", ind, expr)
		return
	}
	// A VALUE OFF ITS DECLARED RANGE, OR A bits(N) VALUE PAST ITS WIDTH, IS
	// REFUSED BY NAME as out_of_range (§3.3), as the Go reference's message
	// read does; it is not wire damage.
	owner := floatOwnerIn(m.u, m.origOf(f))
	signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
	rlo, rhi, hasRange := ir.TableRawRange(f)
	hasRange = hasRange && f.HasIntRange
	bitsClamp := f.Type.Kind == ir.TBits && f.Type.Width < 64
	mut := ""
	if hasRange || bitsClamp {
		mut = "mut "
	}
	if signed {
		m.pf("%slet %sschema_x = table_message_signed(%s.kind, schema_lo, schema_hi);\n", ind, mut, entry)
	} else {
		m.pf("%slet %sschema_x = table_message_unsigned(%s.kind, schema_lo, schema_hi);\n", ind, mut, entry)
	}
	if hasRange {
		h := rustBig(rhi, signed)
		if !signed && rlo.Sign() == 0 {
			// An unsigned value is never below zero, so only the upper bound can fire.
			m.pf("%sif schema_x > %s { schema_x = %s; schema_report.out_of_range(%q); }\n", ind, h, h, owner)
		} else {
			l := rustBig(rlo, signed)
			if l == h {
				m.pf("%sif schema_x != %s { schema_x = %s; schema_report.out_of_range(%q); }\n", ind, l, l, owner)
			} else {
				m.pf("%sif schema_x < %s { schema_x = %s; schema_report.out_of_range(%q); } else if schema_x > %s { schema_x = %s; schema_report.out_of_range(%q); }\n", ind, l, l, owner, h, h, owner)
			}
		}
	}
	if bitsClamp {
		max := uint64(1)<<f.Type.Width - 1
		m.pf("%sif schema_x > %d { schema_x = %d; schema_report.out_of_range(%q); }\n", ind, max, max, owner)
	}
	if fl := isFlags(f); fl != nil && fl.WireBits < 64 {
		// A NEWER WRITER ANNOUNCES A WIDER MASK, and the reader holds each
		// value to its OWN W (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK
		// TO ITS WIDTH): a bit at or above it refuses by name as out_of_range.
		m.pf("%sif schema_x >> %d != 0 { schema_report.out_of_range(%q); }\n", ind, fl.WireBits, owner)
	}
	typ := cookElemType(f)
	switch typ {
	case "TableI128", "TableU128":
		m.pf("%s%s = %s(schema_x);\n", ind, expr, typ)
	default:
		m.pf("%s%s = schema_x as %s;\n", ind, expr, typ)
	}
}

// readArray is gotable's emitMessageReadArray: a count, then the elements,
// a fixed or counted array clamping to its bound and a keyed array landing
// each named slot in its own place.
func (m *msgGen) readArray(f *ir.Field, expr, entry, ind string) {
	el := m.tmp("element")
	elemType := cookElemType(f)
	m.pf("%s{\n", ind)
	m.pf("%s    let Some(schema_n) = table_message_count(schema_r, &%s.shape) else %s;\n", ind, entry, msgBad)
	if f.KeyEnum == "" {
		m.pf("%s    if %s.shape.elem == 6 && !schema_r.align() %s\n", ind, entry, msgBad)
	}
	e := msgElement(f)
	elArg := el
	if f.KeyEnum == "" || msgNeedsEntry(e) {
		m.pf("%s    let %s = TableMessageEntry { id: 0, kind: %s.shape.elem, shape: %s.element, element: TableMessageShape::default(), first: 0 };\n", ind, el, entry, entry)
	} else {
		elArg = entry
	}
	m.orig[e] = m.origOf(f)
	if f.KeyEnum != "" {
		// A KEYED SLOT TWICE IN ONE KEYED ARRAY IS DAMAGE (docs/SPEC-TABLES.md
		// §3.2, §3.3): the slots this array has named are held in a fixed set.
		m.pf("%s    let mut schema_seen_keys = TableMessageSeen::default();\n", ind)
		m.pf("%s    for _ in 0..schema_n {\n", ind)
		m.pf("%s        let Some(schema_reference) = schema_r.get(schema_v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s        let Some(id) = schema_v.name(schema_reference) else %s;\n", ind, msgBad)
		m.pf("%s        if !schema_seen_keys.once(schema_v.seen_key(schema_reference)) %s\n", ind, msgBad)
		m.pf("%s        let schema_key: usize = match id {", ind)
		for i := range f.KeyEnumRef.Variants {
			m.pf(" 0x%016x => %d,", ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i+1)
		}
		m.pf(" _ => 0 };\n")
		m.pf("%s        let mut schema_scratch: %s = Default::default();\n", ind, elemType)
		m.pf("%s        let schema_p: &mut %s = if schema_key != 0 { &mut %s[schema_key - 1] } else { schema_report.unknown += 1; &mut schema_scratch };\n", ind, elemType, expr)
		m.readValue(e, "(*schema_p)", elArg, ind+"        ")
		m.pf("%s    }\n", ind)
	} else {
		if f.Array == ir.ArrayCounted && f.ArrayMin > 0 {
			// A COUNT BELOW THE READER's DECLARED MIN IS REFUSED (docs/SPEC.md
			// §4.2; internal/tablewire refuses it as count_over_bound)
			m.pf("%s    if schema_n < %d %s\n", ind, f.ArrayMin, msgBad)
		}
		m.pf("%s    let schema_keep = core::cmp::min(schema_n, %d);\n", ind, f.ArrayBound)
		m.pf("%s    if schema_keep < schema_n { schema_report.damaged = true; }\n", ind)
		m.pf("%s    let schema_width = table_message_value_bits(%s.kind, &%s.shape);\n", ind, el, el)
		m.pf("%s    let schema_walk = if schema_width >= 0 { schema_keep } else { schema_n };\n", ind)
		m.pf("%s    for schema_i in 0..schema_walk {\n", ind)
		m.pf("%s        let mut schema_scratch: %s = Default::default();\n", ind, elemType)
		m.pf("%s        let schema_p: &mut %s = if schema_i < schema_keep { &mut %s[schema_i as usize] } else { &mut schema_scratch };\n", ind, elemType, expr)
		m.readValue(e, "(*schema_p)", elArg, ind+"        ")
		m.pf("%s    }\n", ind)
		m.pf("%s    if schema_walk < schema_n && !schema_r.skip((schema_n - schema_walk) * schema_width as u64) %s\n", ind, msgBad)
		if f.Array == ir.ArrayCounted {
			m.pf("%s    %s_count = schema_keep as i32;\n", ind, expr)
		}
	}
	m.pf("%s}\n", ind)
}

// ---- the batch -------------------------------------------------------------

func (m *msgGen) emitBatch(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	row := st.Name + "Row"
	measureExtra, savePrefix, saveEnd, loadPrefix := "", "", "2 + (schema_w.bits / 8) as i64", ""
	if st.Integrity == "crc32c" {
		measureExtra = "4 + "
		savePrefix = fmt.Sprintf("    let schema_need = %s_measure_messages(schema_values, schema_report);\n    if schema_need < 0 || buffer.len() < schema_need as usize { return -1; }\n", snake)
		saveEnd = "let end = 2 + (schema_w.bits / 8) as usize;\n    let sum = table_integrity_checksum(&buffer[..end]);\n    buffer[end..end + 4].copy_from_slice(&sum.to_le_bytes());\n    (end + 4) as i64"
		loadPrefix = "    let data = if data.first() == Some(&TABLE_WIRE_MESSAGE_FORM) {\n        let Some(schema_body) = table_integrity_body(data, 6) else {\n            schema_report.damaged = true; schema_report.reason = TableReason::IntegrityMismatch;\n            return (0, false);\n        };\n        schema_body\n    } else { data };\n"
	}
	m.pf(`/// MeasureMessages (§3.3): the byte count SaveMessages writes for this batch,
/// or -1. "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME".
pub fn %[1]s_measure_messages(schema_values: &[%[2]s], schema_report: &mut TableReport) -> i64 {
    if schema_values.is_empty() || schema_values.len() > TABLE_MESSAGE_BATCH_MAX {
        schema_report.refuse(TableReason::BatchTooLarge);
        return -1;
    }
    let mut schema_w = TableBitWriter::measure();
    for schema_value in schema_values {
        if !%[1]s_save_message_body(&mut schema_w, schema_value) {
            return -1;
        }
    }
    schema_w.align();
    if schema_w.overflow {
        return -1;
    }
    %[3]s2 + (schema_w.bits / 8) as i64
}

/// SaveMessages (§3.3): the form byte, M - 1, then every body as one continuous
/// bit stream padded to a byte once at its end. The byte count, or -1 for a
/// refusal or a buffer too short.
pub fn %[1]s_save_messages(schema_values: &[%[2]s], buffer: &mut [u8], schema_report: &mut TableReport) -> i64 {
%[4]s    if schema_values.is_empty() || schema_values.len() > TABLE_MESSAGE_BATCH_MAX {
        schema_report.refuse(TableReason::BatchTooLarge);
        return -1;
    }
    if buffer.len() < 2 {
        return -1;
    }
    buffer[0] = TABLE_WIRE_MESSAGE_FORM;
    buffer[1] = (schema_values.len() - 1) as u8;
    let mut schema_w = TableBitWriter::new(&mut buffer[2..]);
    for schema_value in schema_values {
        if !%[1]s_save_message_body(&mut schema_w, schema_value) {
            return -1;
        }
    }
    schema_w.align();
    if schema_w.overflow {
        return -1;
    }
    %[5]s
}

/// LoadMessages (§3.3): one batch into the caller's storage against the ENDPOINT's
/// announced vocabulary. The count of bodies delivered and whether the batch
/// read whole. DAMAGE ANYWHERE IN THE BATCH DISCARDS THE WHOLE BATCH (§3.3, §4):
/// the count is zero, malformed is set; "M ABOVE THE CALLER'S CAPACITY ON THE
/// READ SIDE IS A REFUSAL BY NAME", nothing decoded and a count of 0, as every
/// refused batch returns; "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS
/// REFUSED BY NAME"; and "BYTES AFTER THE PAD ARE MALFORMED".
///
/// A NON-FINITE FLOAT IN ANY BODY REFUSES THE BATCH (docs/SPEC-TABLES.md §3.3):
/// no body of it is delivered, so the count is zero, and the reason is
/// non_finite_float. A VALUE OUTSIDE ITS DECLARED RANGE, met before any
/// damage, refuses the batch the same way BY NAME as out_of_range (§3.3), the
/// field in oor_field.
///
/// RULING R4: after ANY refusal, every value handed in (the whole slice, not
/// only the batch's count) holds its declared default.
pub fn %[1]s_load_messages(schema_values: &mut [%[2]s], schema_vocabulary: &TableVocabulary, data: &[u8], schema_report: &mut TableReport) -> (usize, bool) {
    let (schema_count, schema_whole) = %[1]s_load_messages_walk(schema_values, schema_vocabulary, data, schema_report);
    if !schema_whole {
        for schema_value in schema_values.iter_mut() {
            *schema_value = %[1]s_message_default();
        }
        return (0, false);
    }
    (schema_count, true)
}

/// The walk under LoadMessages: on a refusal it returns (0, false) and leaves
/// the reset of every value handed in to the caller above (R4).
fn %[1]s_load_messages_walk(schema_values: &mut [%[2]s], schema_vocabulary: &TableVocabulary, data: &[u8], schema_report: &mut TableReport) -> (usize, bool) {
    *schema_report = TableReport::default();
%[6]s    let Some(schema_count) = table_message_batch_open(schema_vocabulary, data, schema_report) else {
        return (0, false);
    };
    if schema_count > schema_values.len() {
        return (0, schema_report.refuse(TableReason::BatchTooLarge)); // a refused batch returns a count of 0 (§3.3)
    }
    let mut schema_r = TableBitReader::new(&data[2..]);
    for schema_value in schema_values.iter_mut().take(schema_count) {
        if !%[1]s_load_message_body(&mut schema_r, schema_vocabulary, schema_report, schema_value) || schema_report.damaged || !schema_report.non_finite_float.is_empty() {
            if !schema_report.non_finite_float.is_empty() {
                return (0, schema_report.refuse(TableReason::NonFiniteFloat));
            }
            if !schema_report.oor_field.is_empty() {
                return (0, schema_report.refuse(TableReason::OutOfRange));
            }
            return (0, false);
        }
    }
    if !table_message_batch_close(&mut schema_r, schema_report) {
        return (0, false);
    }
    (schema_count, true)
}

`, snake, row, measureExtra, savePrefix, saveEnd, loadPrefix)
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

    pub fn put(&mut self, schema_value: u64, schema_n: u32) {
        if schema_n > 64 {
            self.overflow = true;
            return;
        }
        if schema_n == 0 {
            return;
        }
        let schema_at = self.bits;
        self.bits += schema_n as u64;
        if self.measuring {
            return;
        }
        if self.bits.div_ceil(8) > self.buffer.len() as u64 {
            self.overflow = true;
            return;
        }
        let schema_value = if schema_n < 64 { schema_value & ((1u64 << schema_n) - 1) } else { schema_value };
        let mut schema_done = 0u32;
        let mut schema_pos = schema_at;
        while schema_done < schema_n {
            let schema_index = (schema_pos >> 3) as usize;
            let schema_shift = (schema_pos & 7) as u32;
            let schema_take = core::cmp::min(8 - schema_shift, schema_n - schema_done);
            let schema_chunk = ((schema_value >> schema_done) as u8) & (((1u16 << schema_take) - 1) as u8);
            let schema_keep = ((1u16 << schema_shift) - 1) as u8;
            self.buffer[schema_index] = (self.buffer[schema_index] & schema_keep) | (schema_chunk << schema_shift);
            schema_done += schema_take;
            schema_pos += schema_take as u64;
        }
    }

    pub fn align(&mut self) {
        let pad = ((8 - (self.bits & 7)) & 7) as u32;
        self.put(0, pad);
    }

    /// Raw bytes on a byte boundary: a string's content (§3.3).
    pub fn raw(&mut self, data: &[u8]) {
        self.align();
        for &schema_b in data {
            self.put(schema_b as u64, 8);
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

    pub fn get(&mut self, schema_n: u32) -> Option<u64> {
        let schema_total = (self.buffer.len() as u64) * 8;
        if schema_n > 64 || schema_n as u64 > schema_total - self.offset {
            return None;
        }
        let mut schema_value = 0u64;
        for schema_i in 0..schema_n as u64 {
            let schema_at = self.offset + schema_i;
            schema_value |= (((self.buffer[(schema_at >> 3) as usize] >> (schema_at & 7)) & 1) as u64) << schema_i;
        }
        self.offset += schema_n as u64;
        Some(schema_value)
    }

    pub fn skip(&mut self, schema_n: u64) -> bool {
        if schema_n > (self.buffer.len() as u64) * 8 - self.offset {
            return false;
        }
        self.offset += schema_n;
        true
    }

    /// The pad to a byte boundary, which must be zero bits (§3.3).
    pub fn align(&mut self) -> bool {
        let pad = ((8 - (self.offset & 7)) & 7) as u32;
        self.get(pad) == Some(0)
    }
}

/// bits_required(0, n): the width of a value in [0, n], zero for n = 0 (§3.3).
pub fn table_message_bits(schema_n: u64) -> u32 {
    64 - schema_n.leading_zeros()
}

/// A fixed-width kind's storage bytes (§3), zero for a kind with none.
pub fn table_message_kind_width(schema_kind: u8) -> u32 {
    match schema_kind {
        1 | 2 | 6 | 20 | 25 => 1,
        3 | 7 | 21 | 26 => 2,
        4 | 8 | 10 | 22 | 27 => 4,
        5 | 9 | 11 | 23 | 28 | 36 => 8,
        18 | 19 | 24 | 29 => 16,
        _ => 0,
    }
}

/// The bits one value of an entry takes on a body (§3.3's shape table): the
/// packed width under a ranged or quantized packing, the storage width
/// otherwise, and -1 for a kind whose value is not one fixed width.
pub fn table_message_value_bits(schema_kind: u8, schema_s: &TableMessageShape) -> i64 {
    match schema_kind {
        0 | 32 => 0,
        1 => 1,
        _ => {
            if (schema_kind == 10 && schema_s.packing == 2) || (schema_s.packing == 1 && ((2..=9).contains(&schema_kind) || (18..=29).contains(&schema_kind))) {
                schema_s.bits as i64
            } else {
                match table_message_kind_width(schema_kind) {
                    0 => -1,
                    schema_w => (schema_w * 8) as i64,
                }
            }
        }
    }
}

/// An array's or a text's count: its offset from the declared minimum, at the
/// width the range [min, max] needs (§3.3).
pub fn table_message_count(schema_r: &mut TableBitReader, schema_s: &TableMessageShape) -> Option<u64> {
    let schema_raw = schema_r.get(table_message_bits(schema_s.max - schema_s.min))?;
    Some(schema_raw + schema_s.min)
}

/// The widening ladder (§4): a narrower integer of the same signedness, or f32
/// into f64, decoded exactly.
pub fn table_message_widens(from: u8, schema_to: u8) -> bool {
    ((2..=5).contains(&from) && ((schema_to > from && schema_to <= 5) || schema_to == 18))
        || ((6..=9).contains(&from) && ((schema_to > from && schema_to <= 9) || schema_to == 19))
        || (from == 10 && schema_to == 11)
}

/// §3's one content rule for a string: well-formed UTF-8 with no zero byte.
pub fn table_message_utf8(schema_text: &[u8]) -> bool {
    core::str::from_utf8(schema_text).is_ok() && !schema_text.contains(&0)
}

/// A clamp of well-formed UTF-8 to at most n bytes, never inside a character.
pub fn table_message_utf8_clamp(schema_text: &[u8], schema_n: usize) -> usize {
    let mut schema_n = schema_n;
    while schema_n > 0 && schema_n < schema_text.len() && schema_text[schema_n] & 0xc0 == 0x80 {
        schema_n -= 1;
    }
    schema_n
}

/// A value the endpoint spelled, sign-extended from its announced kind's width.
pub fn table_message_signed(schema_kind: u8, schema_lo: u64, schema_hi: u64) -> i128 {
    match table_message_kind_width(schema_kind) {
        1 => schema_lo as i8 as i128,
        2 => schema_lo as i16 as i128,
        4 => schema_lo as i32 as i128,
        8 => schema_lo as i64 as i128,
        _ => (((schema_hi as u128) << 64) | schema_lo as u128) as i128,
    }
}

/// A value the endpoint spelled, zero-extended from its announced kind's width.
pub fn table_message_unsigned(schema_kind: u8, schema_lo: u64, schema_hi: u64) -> u128 {
    match table_message_kind_width(schema_kind) {
        1 => schema_lo as u8 as u128,
        2 => schema_lo as u16 as u128,
        4 => schema_lo as u32 as u128,
        8 => schema_lo as u128,
        _ => ((schema_hi as u128) << 64) | schema_lo as u128,
    }
}

/// A kind whose domain is two's complement (§3.3).
pub fn table_message_signed_kind(schema_kind: u8) -> bool {
    (2..=5).contains(&schema_kind) || schema_kind == 18 || (20..=24).contains(&schema_kind)
}

/// A RANGED value's skip holds base plus offset to the kind's own domain, the
/// bound a named read holds it to: the offset's bits can spell a value past the
/// sender's range (§3.3). The base and the offset are added exactly as the
/// named read adds them, the wide lo/hi included, and a width of zero still
/// holds the base.
pub fn table_message_skip_ranged(schema_r: &mut TableBitReader, schema_kind: u8, schema_width: i64, schema_s: &TableMessageShape) -> bool {
    if schema_width < 0 {
        return false;
    }
    let schema_width = schema_width as u32;
    let Some(schema_lo) = schema_r.get(core::cmp::min(schema_width, 64)) else { return false };
    let schema_hi = if schema_width > 64 {
        match schema_r.get(schema_width - 64) {
            Some(schema_v) => schema_v,
            None => return false,
        }
    } else {
        0
    };
    table_message_ranged(schema_kind, ((schema_hi as u128) << 64) | schema_lo as u128, schema_s).is_some()
}

/// A RANGED value's base plus offset, HELD TO THE ANNOUNCED KIND's STORAGE
/// DOMAIN and never wrapped (§3.3): the 128-bit two's-complement image of the
/// value, or None when the sum lands outside the kind. The named read and the
/// skip both take it, so the two give one verdict.
pub fn table_message_ranged(schema_kind: u8, schema_offset: u128, schema_s: &TableMessageShape) -> Option<u128> {
    let bits = kind_bits(schema_kind);
    if table_message_signed_kind(schema_kind) {
        // Narrow announcements store a sign-extended 64-bit base; wide
        // announcements store both halves. Bias the signed domain so the
        // full unsigned offset can be added without narrowing it to i128.
        let base = if bits > 64 {
            (((schema_s.base_hi as u128) << 64) | schema_s.base_lo as u128) as i128
        } else {
            schema_s.base_lo as i64 as i128
        };
        let schema_biased = (base as u128) ^ (1u128 << 127);
        let sum = schema_biased.checked_add(schema_offset)?;
        let schema_value = (sum ^ (1u128 << 127)) as i128;
        let schema_bottom = if bits >= 128 { i128::MIN } else { -(1i128 << (bits - 1)) };
        let schema_top = if bits >= 128 { i128::MAX } else { (1i128 << (bits - 1)) - 1 };
        if schema_value >= schema_bottom && schema_value <= schema_top { Some(schema_value as u128) } else { None }
    } else {
        let base = if bits > 64 { ((schema_s.base_hi as u128) << 64) | schema_s.base_lo as u128 } else { schema_s.base_lo as u128 };
        let schema_value = base.checked_add(schema_offset)?;
        let schema_top = if bits >= 128 { u128::MAX } else { (1u128 << bits) - 1 };
        if schema_value <= schema_top { Some(schema_value) } else { None }
    }
}

/// One scalar by its shape (§3.3): the value bits, the ranged base added back,
/// a quantized step mapped back onto its float32 range — all at the announced
/// storage width, before the file form's clamps.
pub fn table_message_read_scalar(schema_r: &mut TableBitReader, schema_kind: u8, schema_s: &TableMessageShape) -> Option<(u64, u64)> {
    let bits = table_message_value_bits(schema_kind, schema_s);
    if bits < 0 {
        return None;
    }
    let bits = bits as u32;
    let mut schema_lo = schema_r.get(core::cmp::min(bits, 64))?;
    let mut schema_hi = if bits > 64 { schema_r.get(bits - 64)? } else { 0 };
    if schema_s.packing == 1 && schema_kind != 10 {
        // BASE PLUS OFFSET IS HELD TO THE ANNOUNCED KIND, never wrapped and
        // never truncated to its width (§3.3): a sum outside the kind is damage.
        let schema_value = table_message_ranged(schema_kind, ((schema_hi as u128) << 64) | schema_lo as u128, schema_s)?;
        schema_lo = schema_value as u64;
        schema_hi = (schema_value >> 64) as u64;
    }
    if schema_s.packing == 2 && schema_kind == 10 {
        // AN INDEX ABOVE THE ENDPOINT's ANNOUNCED count IS BATCH DAMAGE (§3.3,
        // SPEC.md §4.3), checked on the whole wire value before any narrowing
        // and never reconstructed and held: the width spells such an index
        // whenever count is not one less than a power of two (eight bits
        // spelling 255 over a count of 200). internal/tablewire's
        // bitDecoder.float32 is the oracle.
        if schema_hi != 0 || schema_lo > schema_s.qcount as u64 {
            return None;
        }
        let schema_index = schema_lo as u32;
        let schema_ratio = schema_index as f32 / schema_s.qcount as f32;
        let schema_scaled = schema_ratio * schema_s.qdelta;
        schema_lo = (schema_scaled + schema_s.qmin).to_bits() as u64;
    }
    Some((schema_lo, schema_hi))
}

/// One scalar by this build's own shape (§3.3): the ranged base taken off, a
/// float32 quantized onto its steps, then the value bits.
pub fn table_message_write_scalar(schema_w: &mut TableBitWriter, schema_s: &TableMessageShape, schema_lo: u64, schema_hi: u64) {
    let (mut schema_lo, mut schema_hi) = (schema_lo, schema_hi);
    if schema_s.packing == 1 {
        let (schema_diff, schema_borrow) = schema_lo.overflowing_sub(schema_s.base_lo);
        schema_lo = schema_diff;
        schema_hi = schema_hi.wrapping_sub(schema_s.base_hi).wrapping_sub(schema_borrow as u64);
    }
    if schema_s.packing == 2 {
        let schema_value = f32::from_bits(schema_lo as u32);
        let mut schema_ratio = (schema_value - schema_s.qmin) / schema_s.qdelta;
        // a NaN never reaches here: the writer refuses one before the
        // quantizer runs (§3.3); the test stays so no input moves the index
        // outside [0, count]
        if schema_ratio.is_nan() || schema_ratio < 0.0 {
            schema_ratio = 0.0;
        } else if schema_ratio > 1.0 {
            schema_ratio = 1.0;
        }
        let schema_scaled = schema_ratio * schema_s.qcount as f32;
        schema_lo = core::cmp::min(((schema_scaled + 0.5) as f64).floor() as u32, schema_s.qcount) as u64;
    }
    schema_w.put(schema_lo, core::cmp::min(schema_s.bits, 64) as u32);
    if schema_s.bits > 64 {
        schema_w.put(schema_hi, (schema_s.bits - 64) as u32);
    }
}

/// THE KEYS A BODY HAS NAMED (§3.3): one bit per id ordinal up to
/// TABLE_MESSAGE_SEEN_KEYS (the conforming default vocabulary, 4096), on the
/// stack, so a body's repeat test is constant work per reference and
/// allocates nothing. THE CALLER-DECLARED CAPACITY IS THE BOUND (§3.3), so a
/// vocabulary declared larger admits every distinct id it holds: a key past
/// the bitset goes to an ordered set beside it, which holds only the keys
/// this body named past 4096, is built only when the first such key arrives,
/// and costs logarithmic work per reference, with no cap of its own.
pub const TABLE_MESSAGE_SEEN_KEYS: u64 = 4096;

pub struct TableMessageSeen {
    bits: [u64; 64],
    past: std::collections::BTreeSet<u64>,
}

impl Default for TableMessageSeen {
    fn default() -> Self {
        TableMessageSeen { bits: [0u64; 64], past: std::collections::BTreeSet::new() }
    }
}

impl TableMessageSeen {
    /// Marks a key: false when the key was named before, or is zero.
    pub fn once(&mut self, schema_key: u64) -> bool {
        if schema_key == 0 {
            return false;
        }
        if schema_key > TABLE_MESSAGE_SEEN_KEYS {
            return self.past.insert(schema_key);
        }
        let schema_i = (schema_key - 1) as usize;
        let schema_bit = 1u64 << (schema_i & 63);
        if self.bits[schema_i >> 6] & schema_bit != 0 {
            return false;
        }
        self.bits[schema_i >> 6] |= schema_bit;
        true
    }
}

/// A strict skip of one POSITIONAL value by the ENDPOINT's shape (§3.3): an
/// array element or a union arm's payload, where a union's None is lawful.
pub fn table_message_skip(schema_r: &mut TableBitReader, schema_v: &TableVocabulary, schema_e: &TableMessageEntry, schema_depth: u32) -> bool {
    table_message_skip_at(schema_r, schema_v, schema_e, schema_depth, false)
}

/// A strict skip of one FIELD by the ENDPOINT's shape (§3.3): what a reader
/// does with a field it cannot name, or one whose kind is not the
/// declaration's. A message field is never required, so a union FIELD's None
/// elides and its explicit arm reference 0 is damage (§3.3 (c)), whether this
/// reader names the field or not.
pub fn table_message_skip_field(schema_r: &mut TableBitReader, schema_v: &TableVocabulary, schema_e: &TableMessageEntry, schema_depth: u32) -> bool {
    table_message_skip_at(schema_r, schema_v, schema_e, schema_depth, true)
}

fn table_message_skip_at(schema_r: &mut TableBitReader, schema_v: &TableVocabulary, schema_e: &TableMessageEntry, schema_depth: u32, schema_field: bool) -> bool {
    if schema_depth > 128 {
        return false;
    }
    let schema_s = &schema_e.shape;
    match schema_e.kind {
        0 | 32 => true,
        30 => match schema_r.get(schema_v.ref_bits) {
            None => false,
            Some(0) => true,
            Some(schema_reference) => schema_v.name(schema_reference).is_some(),
        },
        13 => table_message_skip_body(schema_r, schema_v, schema_depth + 1),
        // a node index: the fixed form's roots carry no pointer, so a body
        // that spells one names nothing this reader can resolve
        17 => false,
        15 => match schema_r.get(schema_v.ref_bits) {
            None => false,
            Some(0) => !schema_field,
            Some(schema_reference) => match schema_v.entry(schema_reference).copied() {
                Some(schema_arm) => schema_arm.id < 0xffff_ffff_ffff_fffd && schema_arm.kind != 0 &&
                    table_message_skip(schema_r, schema_v, &schema_arm, schema_depth + u32::from(matches!(schema_arm.kind, 13..=16))),
                None => false,
            },
        },
        12 => {
            let Some(schema_n) = table_message_count(schema_r, schema_s) else { return false };
            if schema_n > schema_s.max || !schema_r.align() {
                return false;
            }
            let schema_start = (schema_r.offset / 8) as usize;
            if !schema_r.skip(schema_n * 8) {
                return false;
            }
            // a skipped string's bytes are held to the text rule a named read
            // applies to them, and ill-formed text is damage (§3.3, §4)
            table_message_utf8(&schema_r.buffer[schema_start..schema_start + schema_n as usize])
        }
        // 31 is held by name and never written (R17): met only as damage
        31 => false,
        10 => {
            // A PLAIN FLOAT IS FINITE, AND A QUANTIZED INDEX IS AT MOST ITS
            // COUNT (§3.3): the same raw-bits rule a named float read applies.
            let Some(schema_raw) = schema_r.get(table_message_value_bits(10, schema_s) as u32) else { return false };
            if schema_s.packing == 2 {
                schema_raw <= schema_s.qcount as u64
            } else {
                table_finite_f32(schema_raw as u32)
            }
        }
        11 => match schema_r.get(64) {
            Some(schema_raw) => table_finite_f64(schema_raw),
            None => false,
        },
        14 | 16 => {
            let Some(schema_n) = table_message_count(schema_r, schema_s) else { return false };
            // THE COUNT'S BITS CAN SPELL ABOVE THE ANNOUNCED MAX (§3.3).
            if schema_n > schema_s.max || (schema_e.kind == 14 && schema_s.elem == 6 && !schema_r.align()) {
                return false;
            }
            let schema_element = TableMessageEntry { id: 0, kind: schema_s.elem, shape: schema_e.element, element: TableMessageShape::default(), first: 0 };
            // A RUN OF FIXED-WIDTH ELEMENTS IS ARITHMETIC (§3.3): a float, a
            // ranged or quantized element carries a value condition, and a
            // keyed entry carries a key reference, so those are walked.
            if schema_e.kind == 14
                && !matches!(schema_element.kind, 10 | 11 | 12 | 13 | 15 | 17 | 30)
                && schema_element.shape.packing == 0
            {
                match table_message_value_bits(schema_element.kind, &schema_element.shape) {
                    0 => return true,
                    schema_width if schema_width > 0 => return schema_r.skip(schema_n * schema_width as u64),
                    _ => {}
                }
            }
            // A KEYED ARRAY'S KEY NAMES A DECLARED NO-PAYLOAD ENTRY AND NO KEY
            // REPEATS (§3.2, §3.3): the set is one bit per id, on the stack.
            let mut schema_seen = TableMessageSeen::default();
            for _ in 0..schema_n {
                // THE KEY IS PART OF THE WALKED ELEMENT: the offset is sampled
                // before it so a width-0 slot cannot accept the run unread.
                let schema_at = schema_r.offset;
                if schema_e.kind == 16 {
                    let Some(schema_reference) = schema_r.get(schema_v.ref_bits) else { return false };
                    if schema_v.name(schema_reference).is_none() || !schema_seen.once(schema_v.seen_key(schema_reference)) {
                        return false;
                    }
                }
                if !table_message_skip(schema_r, schema_v, &schema_element, schema_depth + 1) {
                    return false;
                }
                // A WALKED ELEMENT THAT CONSUMES NO BITS, its key included, is
                // damage (§3.3): the plain-array width-0 run is the arithmetic
                // path above, not this one.
                if schema_r.offset == schema_at {
                    return false;
                }
            }
            true
        }
        schema_kind => {
            let bits = table_message_value_bits(schema_kind, schema_s);
            if bits < 0 {
                return false;
            }
            if schema_s.packing == 1 && (integer_kind(schema_kind) || fixed_kind(schema_kind)) {
                return table_message_skip_ranged(schema_r, schema_kind, bits, schema_s);
            }
            schema_r.skip(bits as u64)
        }
    }
}

/// A nested body skipped whole, entry by entry, to its zero reference (§3.3).
pub fn table_message_skip_body(schema_r: &mut TableBitReader, schema_v: &TableVocabulary, schema_depth: u32) -> bool {
    if schema_depth > 128 {
        return false;
    }
    // A FIELD ID TWICE IN ONE BODY IS DAMAGE (§3.3), named or skipped: the set
    // is one bit per id, whichever slot carried it, on the stack. A union
    // field of a skipped body is a field, so its explicit None is damage.
    let mut schema_seen = TableMessageSeen::default();
    loop {
        match schema_r.get(schema_v.ref_bits) {
            None => return false,
            Some(0) => return true,
            Some(schema_reference) => {
                let Some(schema_e) = schema_v.entry(schema_reference).copied() else { return false };
                if schema_e.id >= 0xffff_ffff_ffff_fffd {
                    return false;
                }
                if !schema_seen.once(schema_v.seen_key(schema_reference)) {
                    return false;
                }
                if !table_message_skip_field(schema_r, schema_v, &schema_e, schema_depth) {
                    return false;
                }
            }
        }
    }
}

/// The batch's OPEN (§3.3): "the FORM BYTE is a whole byte and is read FIRST",
/// a body from an endpoint that never announced is refused by name, then M - 1.
pub fn table_message_batch_open(schema_v: &TableVocabulary, data: &[u8], schema_report: &mut TableReport) -> Option<usize> {
    if data.is_empty() {
        schema_report.damaged = true;
        return None;
    }
    if data[0] != TABLE_WIRE_MESSAGE_FORM {
        schema_report.refuse(TableReason::NewerForm);
        return None;
    }
    if !schema_v.announced {
        schema_report.refuse(TableReason::NoVocabulary);
        return None;
    }
    if data.len() < 2 {
        schema_report.damaged = true;
        return None;
    }
    Some(data[1] as usize + 1)
}

/// The batch's CLOSE (§3.3): the pad is zero bits and "BYTES AFTER THE PAD ARE
/// MALFORMED".
pub fn table_message_batch_close(schema_r: &mut TableBitReader, schema_report: &mut TableReport) -> bool {
    if !schema_r.align() || schema_r.offset != (schema_r.buffer.len() as u64) * 8 {
        schema_report.damaged = true;
        return false;
    }
    true
}
`
