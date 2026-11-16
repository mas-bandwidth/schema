package tablewire

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// elisionUnit is a variable-form unit with ONE field, `f`, declared by `decl`,
// beside the enum, the type and the table that field can name. `tick` keeps
// the root body non-empty whatever `f` does.
func elisionUnit(modeVariants, decl string) string {
	return fmt.Sprintf(`package elision

enum Mode
{
    %s
}

type Inner
{
    v uint8
}

table Root
{
    tick uint8
    %s
}
`, strings.ReplaceAll(modeVariants, ", ", "\n    "), decl)
}

// elisionVerdict encodes `text` under the writer's unit and answers the
// verdict of the strict decode and the retaining decode under the reader's.
func elisionVerdict(t *testing.T, writerSrc, readerSrc, text string) string {
	t.Helper()
	w := covModel(t, writerSrc)
	wire, err := Encode(w, covPlace(t, w, "Root", text))
	if err != nil {
		t.Fatalf("encode %s: %v", text, err)
	}
	r := covModel(t, readerSrc)
	ok, err := Decode(r, r.New(r.Lookup("Root")), wire, &tabletext.Report{})
	verdict := "read"
	if !ok || err != nil {
		verdict = "refused"
	}
	ok, err = DecodeRetain(r, r.New(r.Lookup("Root")), wire, &Retain{Capacity: 1024, IdCapacity: 64}, &tabletext.Report{})
	retained := "read"
	if !ok || err != nil {
		retained = "refused"
	}
	if retained != verdict {
		t.Errorf("retaining read %s differs from strict read %s: %s", retained, verdict, hex.EncodeToString(wire))
	}
	return verdict
}

// TestReadRefusesEveryKindTheWriterElides holds docs/SPEC-TABLES.md §3 and
// docs/rulings.json reads-refuse-non-canonical over EVERY kind the variable
// form's writer elides, not only the scalar ones: an enum at its default
// variant, a nested table whose body is empty, an all-default fixed array and
// an enum-keyed array with no slot are each a spelling no conforming writer
// produces for a reader that does not declare the field `| required`. A
// `| required` writer is what stores them, so the bytes are a real writer's.
// The same values written by a plain writer, and values a reader substitutes
// (an unknown variant, a deprecated one), read as before.
func TestReadRefusesEveryKindTheWriterElides(t *testing.T) {
	t.Parallel()
	const variants = "Idle, Run, Fly"
	for _, c := range []struct {
		name, writerVariants, writer, readerVariants, reader, text, want string
	}{
		{"enum at its default", variants, "f Mode = Run | required", variants, "f Mode = Run", `{"tick":1,"f":"Run"}`, "refused"},
		{"empty nested table", variants, "f Inner | required", variants, "f Inner", `{"tick":1,"f":{}}`, "refused"},
		{"all-default fixed array", variants, "f [3]uint8 | required", variants, "f [3]uint8", `{"tick":1,"f":[0,0,0]}`, "refused"},
		{"keyed array with no slot", variants, "f [Mode]int32 | required", variants, "f [Mode]int32", `{"tick":1,"f":{}}`, "refused"},

		{"enum off its default", variants, "f Mode", variants, "f Mode", `{"tick":1,"f":"Run"}`, "read"},
		{"nested table with a value", variants, "f Inner", variants, "f Inner", `{"tick":1,"f":{"v":3}}`, "read"},
		{"fixed array with a value", variants, "f [3]uint8", variants, "f [3]uint8", `{"tick":1,"f":[0,4,0]}`, "read"},
		{"keyed array with a slot", variants, "f [Mode]int32", variants, "f [Mode]int32", `{"tick":1,"f":{"Run":5}}`, "read"},
		{"elided at the default", variants, "f Mode", variants, "f Mode", `{"tick":1}`, "read"},

		{"unknown variant", variants, "f Mode", "Idle, Run", "f Mode", `{"tick":1,"f":"Fly"}`, "read"},
		{"deprecated variant", variants, "f Mode", "Idle, Run | deprecated, Fly", "f Mode", `{"tick":1,"f":"Run"}`, "read"},
		{"unknown variant in a fixed array", variants, "f [2]Mode", "Idle, Run", "f [2]Mode", `{"tick":1,"f":["Fly","Idle"]}`, "read"},
		{"unknown variant in a keyed slot", variants, "f [Mode]Mode", "Idle, Run", "f [Mode]Mode", `{"tick":1,"f":{"Run":"Fly"}}`, "read"},
	} {
		got := elisionVerdict(t, elisionUnit(c.writerVariants, c.writer), elisionUnit(c.readerVariants, c.reader), c.text)
		if got != c.want {
			t.Errorf("%s: read verdict %q, want %q", c.name, got, c.want)
		}
	}
}

// elisionMessageUnit is a message-form unit holding every kind the message
// writer elides, beside `tick`, which keeps the body non-empty.
const elisionMessageUnit = `package elisionmsg

enum Mode { Idle, Run, Fly }

fixed table Inner
{
    v uint8
}

fixed table Root
{
    tick uint8
    mode Mode
    kid  Inner
    trio [3]uint8
    by   [Mode]int32
}
`

// storedDefaultMessage writes a batch of one whose body carries `tick` and the
// field `name` AT ITS DEFAULT, under the message writer's own framing: the
// spelling a conforming writer elides (§3.3 (a)).
func storedDefaultMessage(t *testing.T, m *tabletext.Model, name string) []byte {
	t.Helper()
	inst := covPlace(t, m, "Root", `{"tick":1}`)
	vocabulary := MessageVocabulary(m.Unit)
	e := &bitEncoder{m: m, slots: MessageVocabularySlots(m.Unit), refBits: ir.TableMessageRefBits(len(vocabulary))}
	w := &bitWriter{}
	w.put(0, 8) // a batch of one
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if fv.Def.Name != name {
			if err := encodeBitField(e, w, fv); err != nil {
				t.Fatal(err)
			}
			continue
		}
		entry := ir.TableFieldEntry(fv.Def)
		e.ref(w, entry)
		var err error
		switch {
		case fv.Def.KeyEnum != "":
			w.put(0, ir.TableMessageBitsRequired(0, entry.Shape.Max)) // no slot
		case fv.Def.Array == ir.ArrayFixed:
			err = encodeBitArray(e, w, fv, entry, int(fv.Def.ArrayBound))
		case tabletext.EnumOf(fv.Def) != nil:
			err = encodeBitEnum(e, w, fv.Def, &fv.Cell)
		case tabletext.StructOf(fv.Def) != nil:
			err = encodeBitBody(e, w, subInstanceOf(m, fv.Def, &fv.Cell))
		default:
			t.Fatalf("no stored-default spelling for %s", name)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	w.put(0, e.refBits) // the body's terminator
	if e.missing != "" {
		t.Fatal(e.missing)
	}
	w.align()
	return append([]byte{ir.TableWireMessageForm}, w.b...)
}

// TestMessageReadRefusesEveryKindTheWriterElides holds §3.3 (a) over every
// kind the message writer elides beyond the scalars: an enum at its default
// variant, an empty nested table, an all-default fixed array and an
// enum-keyed array with no slot, each met at the reader's own build version,
// are refused; the canonical batch of the same value reads.
func TestMessageReadRefusesEveryKindTheWriterElides(t *testing.T) {
	t.Parallel()
	m := covModel(t, elisionMessageUnit)
	var voc Vocabulary
	if err := voc.AnnounceRead(Announce(m.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	canonical, err := EncodeMessage(m, covPlace(t, m, "Root", `{"tick":1}`))
	if err != nil {
		t.Fatal(err)
	}
	v := voc
	if ok, err := DecodeMessage(m, m.New(m.Lookup("Root")), canonical, &v, &tabletext.Report{}); !ok || err != nil {
		t.Fatalf("the canonical batch was refused: %v", err)
	}
	for _, name := range []string{"mode", "kid", "trio", "by"} {
		data := storedDefaultMessage(t, m, name)
		v := voc
		ok, err := DecodeMessage(m, m.New(m.Lookup("Root")), data, &v, &tabletext.Report{})
		if ok || err == nil {
			t.Errorf("%s stored at its default: ok=%v err=%v: %s", name, ok, err, hex.EncodeToString(data))
		}
	}
}

// TestMessageOrderIsTheSendersDeclarationOrder holds §3.3 (b): the order a
// body's fields ride in is the SENDER's declaration order. A sender under
// another build version that lawfully reordered its fields (docs/VERSIONING.md,
// "Reorder freely") is read, every value landing; under the reader's own build
// version a field the record does not declare has no place in that order and
// is refused.
func TestMessageOrderIsTheSendersDeclarationOrder(t *testing.T) {
	t.Parallel()
	reader := covModel(t, "package order\n\nfixed table Root\n{\n    a uint8\n    b uint8\n}\n")
	writer := covModel(t, "package order\n\nfixed table Root\n{\n    b uint8\n    a uint8\n}\n")
	var wvoc Vocabulary
	if err := wvoc.AnnounceRead(Announce(writer.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	data, err := EncodeMessage(writer, covPlace(t, writer, "Root", `{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	inst := reader.New(reader.Lookup("Root"))
	v := wvoc
	if ok, err := DecodeMessage(reader, inst, data, &v, &tabletext.Report{}); !ok || err != nil {
		t.Errorf("a lawfully reordered sender was refused: ok=%v err=%v: %s", ok, err, hex.EncodeToString(data))
	} else {
		a, _ := inst.FieldByKey("a")
		b, _ := inst.FieldByKey("b")
		if a.Cell.U != 1 || b.Cell.U != 2 {
			t.Errorf("a reordered sender's values: a=%d b=%d, want 1 and 2", a.Cell.U, b.Cell.U)
		}
	}

	// The same bytes under this reader's build claim its declaration order.
	v = wvoc
	v.buildVersion = ir.BuildVersion(reader.Unit)
	if ok, err := DecodeMessage(reader, reader.New(reader.Lookup("Root")), data, &v, &tabletext.Report{}); ok || err == nil {
		t.Errorf("reordered body under the same build read: ok=%v err=%v", ok, err)
	}
	// Vocabulary slots are shared across records; slot order is not body order.
	both := covModel(t, `package order
    fixed table A {x uint16
 y uint32}
    fixed table B {y uint32
 x uint16}
    fixed table Root {a A
 b B}
    `)
	value := covPlace(t, both, "Root", `{"a":{"x":1,"y":2},"b":{"y":3,"x":4}}`)
	wire, err := EncodeMessage(both, value)
	if err != nil {
		t.Fatal(err)
	}
	var slots Vocabulary
	if err := slots.AnnounceRead(Announce(both.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	if ok, err := DecodeMessage(both, both.New(value.Def), wire, &slots, &tabletext.Report{}); !ok || err != nil {
		t.Errorf("two records with opposite declaration orders refused: %v", err)
	}

	// the reader's own build: a body naming Inner's field `v` inside Root
	m := covModel(t, elisionMessageUnit)
	var voc Vocabulary
	if err := voc.AnnounceRead(Announce(m.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	e := &bitEncoder{m: m, slots: MessageVocabularySlots(m.Unit), refBits: voc.RefBits()}
	w := &bitWriter{}
	w.put(0, 8) // a batch of one
	inner := m.Lookup("Inner")
	e.ref(w, ir.TableFieldEntry(inner.Fields[0]))
	w.put(3, 8)
	w.put(0, e.refBits)
	w.align()
	stray := append([]byte{ir.TableWireMessageForm}, w.b...)
	v = voc
	ok, err := DecodeMessage(m, m.New(m.Lookup("Root")), stray, &v, &tabletext.Report{})
	if ok || err == nil {
		t.Errorf("a field the record does not declare, at the reader's own build version: ok=%v err=%v: %s", ok, err, hex.EncodeToString(stray))
	}
}

// A nonempty declared text default is as elidable as a scalar default. An
// empty value distinct from that default must still ride and survive the read.
func TestMessageReadRefusesDeclaredTextDefaults(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{`f string(8) = "abc"`, `f bytes(8) = "abc"`} {
		m := covModel(t, "package textdefault\nfixed table Root {\n"+decl+"\n}\n")
		inst := m.New(m.Lookup("Root"))
		f := &inst.Fields[0]
		entry := ir.TableFieldEntry(f.Def)
		var voc Vocabulary
		if err := voc.AnnounceRead(Announce(m.Unit), &tabletext.Report{}); err != nil {
			t.Fatal(err)
		}
		e := &bitEncoder{m: m, slots: MessageVocabularySlots(m.Unit), refBits: voc.RefBits()}
		w := &bitWriter{}
		w.put(0, 8)
		e.ref(w, entry)
		if f.Def.Type.Kind == ir.TBytes {
			w.put(3, ir.TableMessageCountBits(entry.Shape))
		} else {
			w.put(3, ir.TableMessageBitsRequired(0, entry.Shape.Max))
		}
		w.align()
		w.bytes([]byte("abc"))
		w.put(0, e.refBits)
		w.align()
		stored := append([]byte{ir.TableWireMessageForm}, w.b...)
		v := voc
		if ok, err := DecodeMessage(m, m.New(inst.Def), stored, &v, &tabletext.Report{}); ok || err == nil {
			t.Errorf("%s: stored declared default read: ok=%v err=%v", decl, ok, err)
		}
		// Under a different build the announcement does not describe defaults.
		v = voc
		v.buildVersion ^= 1
		if ok, err := DecodeMessage(m, m.New(inst.Def), stored, &v, &tabletext.Report{}); !ok || err != nil {
			t.Errorf("%s: another build's value refused: %v", decl, err)
		}
		for _, text := range []string{`{}`, `{"f":""}`} {
			value := covPlace(t, m, "Root", text)
			wire, err := EncodeMessage(m, value)
			if err != nil {
				t.Fatal(err)
			}
			back := m.New(inst.Def)
			v = voc
			if ok, err := DecodeMessage(m, back, wire, &v, &tabletext.Report{}); !ok || err != nil {
				t.Errorf("%s %s: writer output refused: %v", decl, text, err)
			}
			if string(back.Fields[0].Cell.Str) != string(value.Fields[0].Cell.Str) {
				t.Errorf("%s %s: value changed", decl, text)
			}
		}
	}
}

// A substituted unknown or deprecated variant is evolution, rather than a
// field the writer stored at its default. The deprecated case also holds at
// the reader's build version so the substitution exclusion is exercised.
func TestMessageReadAllowsSubstitutedDefaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, variants, value string
		sameBuild             bool
	}{
		{"new variant", "Idle, Run", "Fly", false},
		{"deprecated variant", "Idle, Run | deprecated, Fly", "Run", true},
	} {
		writer := covModel(t, "package evolution\nenum Mode {Idle, Run, Fly}\nfixed table Root {tick uint8\nf Mode}\n")
		reader := covModel(t, "package evolution\nenum Mode {\n"+strings.ReplaceAll(tc.variants, ", ", "\n")+"\n}\nfixed table Root {tick uint8\nf Mode}\n")
		wire, err := EncodeMessage(writer, covPlace(t, writer, "Root", `{"tick":1,"f":"`+tc.value+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		var vocabulary Vocabulary
		if err := vocabulary.AnnounceRead(Announce(writer.Unit), &tabletext.Report{}); err != nil {
			t.Fatal(err)
		}
		if tc.sameBuild {
			vocabulary.buildVersion = ir.BuildVersion(reader.Unit)
		}
		spots := MessageSpots(reader, reader.Lookup("Root"), wire, &vocabulary)
		if len(spots) == 0 || spots[len(spots)-1].Value != 0 {
			t.Errorf("%s: admitted message scan lost its body terminator: %+v", tc.name, spots)
		}
		for _, batch := range []bool{false, true} {
			v := vocabulary
			value := reader.New(reader.Lookup("Root"))
			var report tabletext.Report
			var ok bool
			if batch {
				_, ok, err = DecodeMessages(reader, []*tabletext.Instance{value}, wire, &v, &report)
			} else {
				ok, err = DecodeMessage(reader, value, wire, &v, &report)
			}
			if !ok || err != nil {
				t.Errorf("%s batch=%v refused: %v", tc.name, batch, err)
			}
			field, _ := value.FieldByKey("f")
			if field.Cell.U != 0 || report.Refused {
				t.Errorf("%s batch=%v did not land its substituted default", tc.name, batch)
			}
		}
	}
}

// Required fields ride even at None, null and a scalar default (§2.3,
// §22.2). An older reader cannot establish whether an unknown field is
// required, so those spellings remain lawful unknown-field evolution.
func TestReadAllowsRequiredAndUnknownDefaults(t *testing.T) {
	t.Parallel()
	writer := covModel(t, `package requireddefaults
union Choice { item uint8 }
table Node { n uint8 }
table Root {
    tick uint8
    choice Choice | required
    link *Node | required
    zero uint8 | required
}
`)
	older := covModel(t, "package requireddefaults\ntable Root {tick uint8}\n")
	value := writer.New(writer.Lookup("Root"))
	tick, _ := value.FieldByKey("tick")
	tick.Cell.U = 1
	wire, err := Encode(writer, value)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []*tabletext.Model{writer, older} {
		for _, retain := range []bool{false, true} {
			var report tabletext.Report
			back := model.New(model.Lookup("Root"))
			var ok bool
			if retain {
				ok, err = DecodeRetain(model, back, wire, &Retain{Capacity: 1024, IdCapacity: 64}, &report)
			} else {
				ok, err = Decode(model, back, wire, &report)
			}
			want := 0
			if model == older {
				want = 3
			}
			if !ok || err != nil || report.Unknown != want {
				t.Errorf("required defaults older=%v retaining=%v: ok=%v err=%v report=%+v", model == older, retain, ok, err, report)
			}
		}
	}
}

// Fixed-only message declarations cannot be required (§2.3, §3.3). Thus an
// unknown union field's None is decidably noncanonical too. None remains
// lawful as an array element and as a nested union arm's value.
func TestMessageReadRefusesUnknownUnionNoneFields(t *testing.T) {
	t.Parallel()
	writer := covModel(t, `package messageunion
union Choice { item uint8 }
union Outer { inner Choice }
fixed table Child { choice Choice }
fixed table Root {
    tick uint8
    choice Choice
    choices [..2]Choice
    outer Outer
    kid Child
}
`)
	older := covModel(t, "package messageunion\nfixed table Root {tick uint8}\n")
	var vocabulary Vocabulary
	if err := vocabulary.AnnounceRead(Announce(writer.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`{"tick":1,"choice":{"item":5}}`, `{"tick":1,"choices":[{}]}`, `{"tick":1,"outer":{"inner":{}}}`} {
		wire, err := EncodeMessage(writer, covPlace(t, writer, "Root", text))
		if err != nil {
			t.Fatal(err)
		}
		v := vocabulary
		var report tabletext.Report
		if ok, err := DecodeMessage(older, older.New(older.Lookup("Root")), wire, &v, &report); !ok || err != nil || report.Unknown != 1 {
			t.Errorf("lawful unknown union position %s refused: %v %+v", text, err, report)
		}
	}
	for _, nested := range []bool{false, true} {
		value := covPlace(t, writer, "Root", `{"tick":1}`)
		e := &bitEncoder{m: writer, slots: MessageVocabularySlots(writer.Unit), refBits: vocabulary.RefBits()}
		w := &bitWriter{}
		w.put(0, 8)
		tick, _ := value.FieldByKey("tick")
		if err := encodeBitField(e, w, tick); err != nil {
			t.Fatal(err)
		}
		if nested {
			kid, _ := value.FieldByKey("kid")
			e.ref(w, ir.TableFieldEntry(kid.Def))
		}
		choice, _ := value.FieldByKey("choice")
		e.ref(w, ir.TableFieldEntry(choice.Def))
		w.put(0, e.refBits) // None stored at a union FIELD
		if nested {
			w.put(0, e.refBits) // Child terminator
		}
		w.put(0, e.refBits)
		w.align()
		wire := append([]byte{ir.TableWireMessageForm}, w.b...)
		for _, batch := range []bool{false, true} {
			v := vocabulary
			back := older.New(older.Lookup("Root"))
			var report tabletext.Report
			var ok bool
			var err error
			if batch {
				_, ok, err = DecodeMessages(older, []*tabletext.Instance{back}, wire, &v, &report)
			} else {
				ok, err = DecodeMessage(older, back, wire, &v, &report)
			}
			if ok || err == nil || !report.Refused {
				t.Errorf("unknown union None field nested=%v batch=%v read: %v %+v", nested, batch, err, report)
			}
			landed, _ := back.FieldByKey("tick")
			if landed.Cell.U != 0 {
				t.Errorf("refused union None field delivered an earlier value: %d", landed.Cell.U)
			}
		}
	}
}

// A variable file identifies fields but carries no writer build or record
// declaration. Reordering the reader's declaration is lawful evolution (§4).
func TestVariableReadPreservesFieldReordering(t *testing.T) {
	t.Parallel()
	writer := covModel(t, "package fileorder\ntable Root {a uint8\nb uint8}\n")
	reader := covModel(t, "package fileorder\ntable Root {b uint8\na uint8}\n")
	data, err := Encode(writer, covPlace(t, writer, "Root", `{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, region := range []bool{false, true} {
		for _, retaining := range []bool{false, true} {
			value := reader.New(reader.Lookup("Root"))
			var report tabletext.Report
			rt := &Retain{Capacity: 1024, IdCapacity: 64}
			var ok bool
			switch {
			case region && retaining:
				ok, err = DecodeRegionRetain(reader, value, data, rt, &report)
			case region:
				ok, err = DecodeRegion(reader, value, data, &report)
			case retaining:
				ok, err = DecodeRetain(reader, value, data, rt, &report)
			default:
				ok, err = Decode(reader, value, data, &report)
			}
			if !ok || err != nil || !report.Silent() {
				t.Errorf("region=%v retain=%v reordered reader refused: %v %+v", region, retaining, err, report)
				continue
			}
			a, _ := value.FieldByKey("a")
			b, _ := value.FieldByKey("b")
			if a.Cell.U != 1 || b.Cell.U != 2 {
				t.Errorf("reordered reader changed values: a=%d b=%d", a.Cell.U, b.Cell.U)
			}
			saved, err := Encode(reader, value)
			if err != nil {
				t.Fatal(err)
			}
			back := writer.New(writer.Lookup("Root"))
			if ok, err := Decode(writer, back, saved, &tabletext.Report{}); !ok || err != nil {
				t.Errorf("reordered writer output refused by older reader: %v", err)
			}
		}
	}
}
