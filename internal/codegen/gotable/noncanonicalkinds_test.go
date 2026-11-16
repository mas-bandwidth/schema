package gotable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// nonCanonicalKindsSchema is the unit the reads' per-kind rows are written
// from: one field of every shape whose default a writer elides by a rule the
// stored value cannot show — an [N] array of pointers, enums or unions, and
// each element kind of an enum-keyed array — beside the message form's
// quantized floats, whose writer elides on the held value (docs/SPEC-TABLES.md
// §3, §3.2, §3.3).
const nonCanonicalKindsSchema = `package kinds

enum Color { Red, Green, Blue }

enum Key { North, South }

type Leaf
{
    tag uint8
}

union Pick
{
    leaf Leaf
    count uint8
}

table Node
{
    value uint8
}

table Root
{
    mode   uint8 = 7
    links  [2]*Node
    hues   [2]Color
    picks  [2]Pick
    counts [Key]uint8
    temps  [Key]int8
    ratios [Key]float32
    leaves [Key]Leaf
    tones  [Key]Color
}

fixed table Msg
{
    mode   uint8 = 7
    q      float32 = 0.0 | min = -10.0, max = 10.0, resolution = 0.01
    pair   [2]float32 | min = -10.0, max = 10.0, resolution = 0.01
    hues   [2]Color
    picks  [2]Pick
    counts [Key]uint8
    leaves [Key]Leaf
    tones  [Key]Color
    qs     [Key]float32 | min = -10.0, max = 10.0, resolution = 0.01
    hue    Color
    tone   Color = Green
    pick   Pick
}
`

// nonCanonicalKindsNewer is the same unit a build later: a variant, an arm, a
// key and a node type the reader cannot name. What it writes lands the
// reader's defaults without riding as them, so the reader reads it.
var nonCanonicalKindsNewer = strings.NewReplacer(
	"enum Color { Red, Green, Blue }", "enum Color { Red, Green, Blue, Purple }",
	"enum Key { North, South }", "enum Key { North, South, West }",
	"    count uint8\n}", "    count uint8\n    extra uint8\n}",
	"table Node\n", "table Other\n",
	"[2]*Node", "[2]*Other",
).Replace(nonCanonicalKindsSchema)

// nonCanonicalKindRow is one row of the per-kind probe.
type nonCanonicalKindRow struct {
	name, form string
	wire       []byte
	read       bool
	resave     bool // the value read re-saves to the same bytes
	other      bool // read under an announcement of another build of the same shape
}

func (r nonCanonicalKindRow) probe() string {
	return fmt.Sprintf("{name: %q, form: %q, wire: %q, read: %v, resave: %v, other: %v},", r.name, r.form, string(r.wire), r.read, r.resave, r.other)
}

type kindsUnit struct {
	t *testing.T
	u *ir.Unit
	m *tabletext.Model
}

func (k kindsUnit) field(table, name string) *ir.Field {
	for _, f := range k.m.Lookup(table).Fields {
		if f.Name == name {
			return f
		}
	}
	k.t.Fatalf("%s has no field %s", table, name)
	return nil
}

// variant is the wire id of an enum's variant, or of a key enum's when the
// field is keyed and key is set.
func variant(f *ir.Field, key bool, name string) uint64 {
	e := tabletext.EnumOf(f)
	if key {
		e = f.KeyEnumRef
	}
	return ir.TableWireId(e.VariantWireNameOf(name))
}

// encode is the engine writer's spelling of text, the reference every
// canonical row is held to.
func (k kindsUnit) encode(table, text string) []byte {
	inst := k.m.New(k.m.Lookup(table))
	var report tabletext.Report
	if !k.m.Read(inst, []byte(text), &report) {
		k.t.Fatalf("%s %s: text refused: %+v", table, text, report)
	}
	var out []byte
	var err error
	if table == "Msg" {
		out, err = tablewire.EncodeMessages(k.m, []*tabletext.Instance{inst})
	} else {
		out, err = tablewire.EncodeVariable(k.m, inst)
	}
	if err != nil {
		k.t.Fatalf("%s %s: %v", table, text, err)
	}
	return out
}

// engineReads is the engine reader's verdict on the same bytes, for the rows
// it is held to.
func (k kindsUnit) engineReads(table string, wire []byte) bool {
	inst := k.m.New(k.m.Lookup(table))
	var report tabletext.Report
	if table == "Msg" {
		var vocabulary tablewire.Vocabulary
		var announced tabletext.Report
		if err := vocabulary.AnnounceRead(tablewire.Announce(k.u), &announced); err != nil {
			k.t.Fatal(err)
		}
		ok, err := tablewire.DecodeMessage(k.m, inst, wire, &vocabulary, &report)
		return ok && err == nil && !report.Refused
	}
	ok, err := tablewire.DecodeRegion(k.m, inst, wire, &report)
	return ok && err == nil && !report.Refused
}

// formOne assembles a variable-form file the engine writer does not produce:
// fields as given, then the root's zero reference, the id table in first-use
// order and its count.
type formOne struct {
	ids  []uint64
	body []byte
}

func (w *formOne) ref(id uint64) uint64 {
	for i, x := range w.ids {
		if x == id {
			return uint64(i + 1)
		}
	}
	w.ids = append(w.ids, id)
	return uint64(len(w.ids))
}

func leb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// array writes an [N] field over element kind kind; elems builds the elements
// after the field's own reference, which is first-use order.
func (w *formOne) array(f *ir.Field, kind int, count int, elems func(w *formOne) []byte) *formOne {
	w.body = append(leb(w.body, w.ref(ir.TableFieldWireId(f))), ir.TableKindArray)
	inner := append([]byte{byte(kind)}, leb(nil, uint64(count))...)
	inner = append(inner, elems(w)...)
	w.body = append(leb(w.body, uint64(len(inner))), inner...)
	return w
}

// keyed writes an enum-keyed field: one (key, element) pair per slot, in the
// order given.
func (w *formOne) keyed(f *ir.Field, slots ...keyedSlot) *formOne {
	w.body = append(leb(w.body, w.ref(ir.TableFieldWireId(f))), ir.TableKindKeyed)
	inner := append([]byte{byte(ir.TableWireElemKind(f))}, leb(nil, uint64(len(slots)))...)
	for _, s := range slots {
		inner = leb(inner, w.ref(variant(f, true, s.key)))
		elem := s.elem(w)
		inner = append(leb(inner, uint64(len(elem))), elem...)
	}
	w.body = append(leb(w.body, uint64(len(inner))), inner...)
	return w
}

type keyedSlot struct {
	key  string
	elem func(w *formOne) []byte
}

func slot(key string, elem ...byte) keyedSlot {
	return keyedSlot{key, func(*formOne) []byte { return elem }}
}

func (w *formOne) done() []byte {
	out := append([]byte{ir.TableWireForm}, w.body...)
	out = append(out, 0)
	for _, id := range w.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(w.ids)))
}

// bitBody assembles a batch of one message body the engine writer does not
// produce: references in the unit's vocabulary, values low bit first.
type bitBody struct {
	slots   map[string]uint64
	refBits int
	b       []byte
	n       int
}

func (k kindsUnit) bits() *bitBody {
	w := &bitBody{slots: tablewire.MessageVocabularySlots(k.u), refBits: ir.TableMessageRefBits(len(tablewire.MessageVocabulary(k.u)))}
	return w.put(0, 8) // a batch of one
}

func (w *bitBody) put(v uint64, n int) *bitBody {
	for i := range n {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		w.b[w.n/8] |= byte(v>>uint(i)&1) << uint(w.n%8)
		w.n++
	}
	return w
}

func (w *bitBody) entry(e ir.TableVocabularyEntry) *bitBody {
	return w.put(w.slots[e.Key()], w.refBits)
}

func (w *bitBody) field(f *ir.Field) *bitBody { return w.entry(ir.TableFieldEntry(f)) }

func (w *bitBody) name(id uint64) *bitBody { return w.entry(ir.TableVocabularyEntry{Id: id}) }

func (w *bitBody) none() *bitBody { return w.put(0, w.refBits) }

// keys writes a keyed field's present-slot count.
func (w *bitBody) keys(f *ir.Field, n uint64) *bitBody {
	return w.field(f).put(n, ir.TableMessageBitsRequired(0, ir.TableFieldEntry(f).Shape.Max))
}

func (w *bitBody) done() []byte {
	w.none() // the body's zero reference
	return append([]byte{ir.TableWireMessageForm}, w.b...)
}

// nonCanonicalKindRows is the per-kind probe's rows. For each kind: the
// spelling at the default is refused, and the nearest values on each side of
// it read, at the engine writer's bytes, which the engine reader reads too. The
// file form refuses with no gate; the message form refuses only under its own
// shape and build version, and the same bytes from another build read. The
// assembled refused rows are held against the engine's spelling of their
// nearest canonical neighbour, so the assembly is the engine's framing.
func nonCanonicalKindRows(t *testing.T) (rows []string, announcement []byte) {
	u := kindsUnit{t: t, u: unitFrom(t, nonCanonicalKindsSchema)}
	u.m = tabletext.NewModel(u.u)
	newer := kindsUnit{t: t, u: unitFrom(t, nonCanonicalKindsNewer)}
	newer.m = tabletext.NewModel(newer.u)
	var all []nonCanonicalKindRow
	canonical := func(form, table string, texts ...string) {
		for _, text := range texts {
			wire := u.encode(table, text)
			if !u.engineReads(table, wire) {
				t.Errorf("%s: the engine reader refuses its own writer's bytes %x", text, wire)
			}
			all = append(all, nonCanonicalKindRow{name: text, form: form, wire: wire, read: true, resave: true})
		}
	}
	same := func(want []byte, wire []byte, what string) {
		if string(want) != string(wire) {
			t.Errorf("%s: the assembly spells %x, the engine %x", what, wire, want)
		}
	}
	refused := func(form, name string, wire []byte) {
		all = append(all, nonCanonicalKindRow{name: name, form: form, wire: wire})
		if form == "message" {
			// the same bytes under another build of the same shape read
			all = append(all, nonCanonicalKindRow{name: name + " (another build)", form: form, wire: wire, read: true, other: true})
		}
	}

	// THE FILE FORM: [N] arrays of pointers, enums and unions, and every keyed
	// element kind, at their engine spellings either side of the default
	canonical("variable", "Root", `{}`, `{"mode":6}`, `{"mode":8}`,
		`{"links":[{"value":0},null]}`, `{"links":[null,{"value":1}]}`,
		`{"hues":["Red","None"]}`, `{"hues":["None","Blue"]}`,
		`{"picks":[{"count":0},{}]}`, `{"picks":[{},{"leaf":{"tag":0}}]}`,
		`{"counts":{"North":1}}`, `{"counts":{"North":1,"South":255}}`,
		`{"temps":{"North":-1}}`, `{"temps":{"South":1}}`,
		`{"ratios":{"North":-0.0}}`, `{"ratios":{"North":1e-45}}`, `{"ratios":{"South":-1e-45}}`,
		`{"leaves":{"North":{"tag":1}}}`, `{"tones":{"North":"Red"}}`, `{"tones":{"South":"Blue"}}`)
	links, hues, picks := u.field("Root", "links"), u.field("Root", "hues"), u.field("Root", "picks")
	counts, temps, ratios := u.field("Root", "counts"), u.field("Root", "temps"), u.field("Root", "ratios")
	leaves, tones := u.field("Root", "leaves"), u.field("Root", "tones")
	enumKind := ir.TableWireScalarKind(hues)
	same(u.encode("Root", `{"hues":["Red","None"]}`), (&formOne{}).array(hues, enumKind, 2, func(w *formOne) []byte { return append(leb(nil, w.ref(variant(hues, false, "Red"))), 0) }).done(), "hues")
	nulls := func(*formOne) []byte { return []byte{0, 0} }
	refused("variable", "an [N] array of pointers every element of which is null", (&formOne{}).array(links, ir.TableKindPointer, 2, nulls).done())
	refused("variable", "an [N] array of enums every element of which is None", (&formOne{}).array(hues, enumKind, 2, nulls).done())
	refused("variable", "an [N] array of unions every element of which is None", (&formOne{}).array(picks, ir.TableWireScalarKind(picks), 2, nulls).done())
	same(u.encode("Root", `{"counts":{"North":1}}`), (&formOne{}).keyed(counts, slot("North", 1)).done(), "counts")
	refused("variable", "a keyed integer slot at its default", (&formOne{}).keyed(counts, slot("North", 0)).done())
	refused("variable", "a keyed signed slot at its default", (&formOne{}).keyed(temps, slot("South", 0)).done())
	refused("variable", "a keyed float slot at its default", (&formOne{}).keyed(ratios, slot("North", 0, 0, 0, 0)).done())
	refused("variable", "a keyed table slot that is its terminator alone", (&formOne{}).keyed(leaves, slot("North", 0)).done())
	refused("variable", "a keyed enum slot at None", (&formOne{}).keyed(tones, slot("North", 0)).done())
	refused("variable", "keyed slots out of ascending variant order", (&formOne{}).keyed(counts, slot("South", 1), slot("North", 1)).done())
	refused("variable", "a keyed slot written twice", (&formOne{}).keyed(counts, slot("North", 1), slot("North", 2)).done())

	// what a newer build writes lands the default without riding as it
	for _, text := range []string{`{"links":[{"value":1},null]}`, `{"hues":["Purple","None"]}`, `{"picks":[{"extra":1},{}]}`, `{"tones":{"North":"Purple"}}`, `{"counts":{"West":1}}`} {
		wire := newer.encode("Root", text)
		if !u.engineReads("Root", wire) {
			t.Errorf("%s: the engine reader refuses a newer build's bytes %x", text, wire)
		}
		all = append(all, nonCanonicalKindRow{name: "a newer build's " + text, form: "variable", wire: wire, read: true})
	}

	// THE MESSAGE FORM
	canonical("message", "Msg", `{}`, `{"mode":6}`, `{"mode":8}`, `{"q":0.01}`, `{"q":-0.01}`,
		`{"hues":["Red","None"]}`, `{"picks":[{},{"count":0}]}`, `{"counts":{"North":1}}`,
		`{"leaves":{"South":{"tag":1}}}`, `{"tones":{"North":"Red"}}`, `{"qs":{"North":0.01}}`,
		`{"hue":"Red"}`, `{"tone":"Red"}`, `{"tone":"Blue"}`, `{"pick":{"count":0}}`)
	// a quantized float held off its default rides at the default's step: the
	// writer's own bytes, which read under the reader's own build
	for _, text := range []string{`{"q":0.001}`, `{"q":-0.001}`, `{"pair":[0.001,-0.001]}`, `{"qs":{"North":0.001}}`} {
		all = append(all, nonCanonicalKindRow{name: text, form: "message", wire: u.encode("Msg", text), read: true})
	}
	mode, mhues, mpicks := u.field("Msg", "mode"), u.field("Msg", "hues"), u.field("Msg", "picks")
	mcounts, mleaves, mtones := u.field("Msg", "counts"), u.field("Msg", "leaves"), u.field("Msg", "tones")
	hue, tone, pick := u.field("Msg", "hue"), u.field("Msg", "tone"), u.field("Msg", "pick")
	red, green := variant(mhues, false, "Red"), variant(tone, false, "Green")
	north, south := variant(mcounts, true, "North"), variant(mcounts, true, "South")
	same(u.encode("Msg", `{"mode":8}`), u.bits().field(mode).put(8, 8).done(), "mode")
	same(u.encode("Msg", `{"hues":["Red","None"]}`), u.bits().field(mhues).name(red).none().done(), "hues")
	same(u.encode("Msg", `{"counts":{"North":1}}`), u.bits().keys(mcounts, 1).name(north).put(1, 8).done(), "counts")
	refused("message", "a scalar at its default", u.bits().field(mode).put(7, 8).done())
	refused("message", "an enum at its default", u.bits().field(tone).name(green).done())
	refused("message", "an enum at its None default", u.bits().field(hue).none().done())

	refused("message", "an [N] array of enums every element of which is None", u.bits().field(mhues).none().none().done())
	refused("message", "an [N] array of unions every element of which is None", u.bits().field(mpicks).none().none().done())
	refused("message", "a keyed integer slot at its default", u.bits().keys(mcounts, 1).name(north).put(0, 8).done())
	refused("message", "a keyed table slot that is its terminator alone", u.bits().keys(mleaves, 1).name(north).none().done())
	refused("message", "a keyed enum slot at None", u.bits().keys(mtones, 1).name(north).none().done())
	// slot order is the variant ordinal, which is for life, and a union field
	// at None elides in every build: no build writes either, so each is refused
	// under any announcement
	for _, other := range []bool{false, true} {
		all = append(all, nonCanonicalKindRow{name: fmt.Sprintf("keyed slots out of ascending variant order (another build %v)", other), form: "message",
			wire: u.bits().keys(mcounts, 2).name(south).put(1, 8).name(north).put(1, 8).done(), other: other},
			nonCanonicalKindRow{name: fmt.Sprintf("a union at None (another build %v)", other), form: "message", wire: u.bits().field(pick).none().done(), other: other})
	}

	for _, r := range all {
		rows = append(rows, r.probe())
	}
	return rows, tablewire.Announce(u.u)
}

// nonCanonicalFirstUseInTheWalk holds the id table's first-use check to the
// load's own walk: the open runs no walk of its own over the body before the
// load decodes it, and the reference the load resolves is the one counted.
func nonCanonicalFirstUseInTheWalk(t *testing.T, src string) {
	if strings.Contains(src, "tableFirstUseHolds") {
		t.Error("the open walks the body for the id table's first-use order before the load walks it")
	}
	if !strings.Contains(generatedFunc(src, "(r *TableReader) Resolve"), "meetFirstUse") {
		t.Error("the reference the load resolves is not counted for the id table's first-use order")
	}
}
