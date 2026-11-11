package darttable

// THE MESSAGE FORM'S BODY NAMES EACH FIELD AT MOST ONCE (docs/SPEC-TABLES.md
// §3.3, §4). A body that names the same resolved vocabulary entry twice is
// damage, known or unknown, in a keyed array's slots as much as in the body's
// own fields: the batch is refused and every body it decoded goes back to its
// defaults, exactly as the Go engine refuses the same bytes.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fieldOnceSyntheticUnit = `package probe

enum Key { A, B, C }

fixed table Child
{
    x int32 = 0
}

fixed table Root
{
    child Child
    n     int32 = 0
    slots [Key]Child
}
`

// fieldOnceVector is one batch and the verdict the Go engine gives it.
type fieldOnceVector struct {
	name       string
	wire       []byte
	refused    bool
	cleanCheck string
	dropCheck  string
}

// fieldOnceBit is bit i of b, least significant first.
func fieldOnceBit(b []byte, i int) uint8 { return (b[i>>3] >> uint(i&7)) & 1 }

// fieldOnceWriter writes a form-2 batch bit by bit. Offsets handed to putSpan
// are into data[1:], the count byte then the body, the same frame
// tablewire.MessageSpots reports.
type fieldOnceWriter struct {
	out []byte
	n   int
}

func newFieldOnceBatch(count int) *fieldOnceWriter {
	return &fieldOnceWriter{out: []byte{2, byte(count - 1)}, n: 16}
}

func (w *fieldOnceWriter) put(bit uint8) {
	if w.n%8 == 0 {
		w.out = append(w.out, 0)
	}
	if bit != 0 {
		w.out[w.n>>3] |= 1 << uint(w.n&7)
	}
	w.n++
}

func (w *fieldOnceWriter) putZero(n int) {
	for i := 0; i < n; i++ {
		w.put(0)
	}
}

func (w *fieldOnceWriter) putSpan(data []byte, from, to int) {
	body := data[1:]
	for i := from; i < to; i++ {
		w.put(fieldOnceBit(body, i))
	}
}

func (w *fieldOnceWriter) bytes() []byte { return w.out }

func fieldOnceVocab(t *testing.T, u *ir.Unit) *tablewire.Vocabulary {
	t.Helper()
	v := &tablewire.Vocabulary{}
	if err := v.AnnounceRead(tablewire.Announce(u), &tabletext.Report{}); err != nil {
		t.Fatalf("the Go engine refused the unit's own announcement: %v", err)
	}
	return v
}

func fieldOnceSpots(t *testing.T, m *tabletext.Model, root string, data []byte, v *tablewire.Vocabulary) []tablewire.BitSpot {
	t.Helper()
	spots := tablewire.MessageSpots(m, m.Lookup(root), data, v)
	if len(spots) < 2 || spots[0].Width == 0 {
		t.Fatalf("%s: the engine found no reference spots in the clean body: %+v", root, spots)
	}
	return spots
}

// fieldOnceEncode writes one clean body through the engine's text form.
func fieldOnceEncode(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place: %s", root, r.Reason)
	}
	data, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("%s: the engine did not encode the clean body: %v", root, err)
	}
	return data
}

// repeatFieldInOneBody joins x's body (up to its terminator) to y's whole
// body, so one body names x's fields and then y's under one terminator.
func repeatFieldInOneBody(t *testing.T, m *tabletext.Model, v *tablewire.Vocabulary, root string, x, y []byte) []byte {
	t.Helper()
	sx := fieldOnceSpots(t, m, root, x, v)
	sy := fieldOnceSpots(t, m, root, y, v)
	rb := sx[0].Width
	w := newFieldOnceBatch(1)
	w.putSpan(x, 8, sx[len(sx)-1].Off)
	w.putSpan(y, 8, sy[len(sy)-1].Off+rb)
	return w.bytes()
}

// repeatUnknownField names one announced entry this root does not declare
// twice in one body, with the payload the engine wrote for it.
func repeatUnknownField(t *testing.T, m *tabletext.Model, v *tablewire.Vocabulary, root string, body []byte) []byte {
	t.Helper()
	spots := fieldOnceSpots(t, m, root, body, v)
	rb := spots[0].Width
	end := spots[len(spots)-1].Off
	w := newFieldOnceBatch(1)
	w.putSpan(body, 8, end)
	w.putSpan(body, 8, end)
	w.putZero(rb)
	return w.bytes()
}

// duplicateNestedField repeats the field at fieldSpot inside the nested body
// that terminates at termSpot, leaving the host body's remaining fields.
func duplicateNestedField(t *testing.T, m *tabletext.Model, v *tablewire.Vocabulary, root string, body []byte, fieldSpot, termSpot int) []byte {
	t.Helper()
	spots := fieldOnceSpots(t, m, root, body, v)
	if termSpot >= len(spots) {
		t.Fatalf("%s: no nested terminator at spot %d: %+v", root, termSpot, spots)
	}
	rb := spots[0].Width
	end := spots[len(spots)-1].Off + rb
	w := newFieldOnceBatch(1)
	w.putSpan(body, 8, spots[termSpot].Off)
	w.putSpan(body, spots[fieldSpot].Off, spots[termSpot].Off)
	w.putSpan(body, spots[termSpot].Off, end)
	return w.bytes()
}

// overwriteSpot writes from's value into to's bits, both references at one
// width, the repeated-keyed-slot lever.
func overwriteSpot(b []byte, from, to tablewire.BitSpot) {
	for i := 0; i < to.Width; i++ {
		pos := 8 + to.Off + i
		if (from.Value>>uint(i))&1 != 0 {
			b[pos>>3] |= 1 << uint(pos&7)
		} else {
			b[pos>>3] &^= 1 << uint(pos&7)
		}
	}
}

func fieldOnceNameSlot(u *ir.Unit, name string) int {
	id := ir.TableWireId(name)
	for i, e := range tablewire.MessageVocabulary(u) {
		if e.Id == id && e.Kind == 0 {
			return i + 1
		}
	}
	return 0
}

func findFieldOnceSpot(t *testing.T, spots []tablewire.BitSpot, value, from int) int {
	t.Helper()
	for i := from; i < len(spots); i++ {
		if int(spots[i].Value) == value {
			return i
		}
	}
	t.Fatalf("no reference to slot %d after spot %d: %+v", value, from, spots)
	return -1
}

// fieldOnceGoRefused asks the Go engine for the verdict on these bytes.
func fieldOnceGoRefused(t *testing.T, u *ir.Unit, root string, wire []byte) bool {
	t.Helper()
	m := tabletext.NewModel(u)
	v := fieldOnceVocab(t, u)
	inst := m.New(m.Lookup(root))
	var rep tabletext.Report
	ok, err := tablewire.DecodeMessage(m, inst, wire, v, &rep)
	return !ok || err != nil || rep.Refused
}

// fieldOnceProbeBody emits a probe that loads every vector on its own storage
// and holds the Dart verdict to the Go engine's.
func fieldOnceProbeBody(root string, announce []byte, vectors []fieldOnceVector) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  final ann = Uint8List.fromList(%s);\n", dartBytes(announce))
	fmt.Fprintf(&b, "  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));\n")
	fmt.Fprintf(&b, "  final annRep = h.TableReport();\n")
	fmt.Fprintf(&b, "  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);\n")
	for _, v := range vectors {
		fmt.Fprintf(&b, "  {\n")
		fmt.Fprintf(&b, "    final value = h.%s();\n    h.init%s(value);\n", root, root)
		fmt.Fprintf(&b, "    final rep = h.TableReport();\n")
		fmt.Fprintf(&b, "    final count = r.%sLoadMessages(<h.%s>[value], Uint8List.fromList(%s), vocab, rep);\n",
			lowerFirst(root), root, dartBytes(v.wire))
		if v.refused {
			fmt.Fprintf(&b, "    check(count == 0 && rep.refused, '%s: count=$count refused=${rep.refused} reason=${rep.reason}');\n", v.name)
			if v.dropCheck != "" {
				fmt.Fprintf(&b, "    %s\n", v.dropCheck)
			}
		} else {
			fmt.Fprintf(&b, "    check(count == 1 && !rep.refused, '%s: count=$count reason=${rep.reason}');\n", v.name)
			if v.cleanCheck != "" {
				fmt.Fprintf(&b, "    %s\n", v.cleanCheck)
			}
		}
		fmt.Fprintf(&b, "  }\n")
	}
	return b.String()
}

// backendFieldOnceVectors builds the repeated known field, the clean body and
// the repeated unknown entry for tables/backend's LoginRequest.
func backendFieldOnceVectors(t *testing.T, u *ir.Unit) (string, []fieldOnceVector) {
	t.Helper()
	const root = "LoginRequest"
	m := tabletext.NewModel(u)
	v := fieldOnceVocab(t, u)
	seven := fieldOnceEncode(t, u, root, `{"client_build":7}`)
	nine := fieldOnceEncode(t, u, root, `{"client_build":9}`)
	unknown := fieldOnceEncode(t, u, "MatchResult", `{"match_id":1}`)
	return root, []fieldOnceVector{
		{
			name:      "a field id twice in one body",
			wire:      repeatFieldInOneBody(t, m, v, root, seven, nine),
			refused:   true,
			dropCheck: "check(value.clientBuild == 0, 'the refused batch kept client_build=${value.clientBuild}');",
		},
		{
			name:       "each field named once",
			wire:       seven,
			cleanCheck: "check(value.clientBuild == 7, 'client_build=${value.clientBuild}');",
		},
		{
			name:      "an unknown entry twice in one body",
			wire:      repeatUnknownField(t, m, v, "MatchResult", unknown),
			refused:   true,
			dropCheck: "check(value.clientBuild == 0, 'the refused batch kept client_build=${value.clientBuild}');",
		},
	}
}

// syntheticFieldOnceVectors builds the nested-body duplicate and the keyed
// slot duplicate, each beside the clean body naming every field once.
func syntheticFieldOnceVectors(t *testing.T, u *ir.Unit) (string, []fieldOnceVector) {
	t.Helper()
	const root = "Root"
	clean := fieldOnceEncode(t, u, root, `{"child":{"x":7},"n":3,"slots":{"A":{"x":1},"B":{"x":2}}}`)
	m := tabletext.NewModel(u)
	v := fieldOnceVocab(t, u)
	spots := fieldOnceSpots(t, m, root, clean, v)
	// child is the first field; its body holds x and its own terminator, then
	// n and the keyed array follow.
	child := findTable(t, u, "Child")
	xSlot := int(tablewire.MessageVocabularySlots(u)[ir.TableFieldEntry(child.Fields[0]).Key()])
	xSpot := findFieldOnceSpot(t, spots, xSlot, 0)
	if xSpot+1 >= len(spots) || spots[xSpot+1].Value != 0 {
		t.Fatalf("the nested field is not followed by its body's terminator: %+v", spots)
	}
	keyed := append([]byte(nil), clean...)
	a := findFieldOnceSpot(t, spots, fieldOnceNameSlot(u, "A"), 0)
	b := findFieldOnceSpot(t, spots, fieldOnceNameSlot(u, "B"), a+1)
	if spots[a].Width != spots[b].Width {
		t.Fatalf("the two keyed slots are not the same width: %+v", spots)
	}
	overwriteSpot(keyed, spots[a], spots[b])
	return root, []fieldOnceVector{
		{
			name:      "a nested body field twice",
			wire:      duplicateNestedField(t, m, v, root, clean, xSpot, xSpot+1),
			refused:   true,
			dropCheck: "check(value.child.x == 0 && value.n == 0 && value.slots[0].x == 0 && value.slots[1].x == 0, 'the refused batch kept a value');",
		},
		{
			name:       "each field named once, nested and keyed",
			wire:       clean,
			cleanCheck: "check(value.child.x == 7 && value.n == 3 && value.slots[0].x == 1 && value.slots[1].x == 2, 'clean=${value.child.x},${value.n},${value.slots[0].x},${value.slots[1].x}');",
		},
		{
			name:      "a keyed slot twice in one keyed body",
			wire:      keyed,
			refused:   true,
			dropCheck: "check(value.child.x == 0 && value.n == 0 && value.slots[0].x == 0 && value.slots[1].x == 0, 'the refused batch kept a value');",
		},
	}
}

// fieldOnceTwoShapesUnit announces one field id at two shapes: A.x is int32
// and B.x is int64, both under the same wire id (docs/SPEC-TABLES.md §3.3).
const fieldOnceTwoShapesUnit = `package probe

fixed table A
{
    x int32
}

fixed table B
{
    x int64
}
`

// twoShapesFieldOnceVectors builds a body of A that names A.x and then B.x:
// one resolved id at two announced shapes, which the Go engine damages.
func twoShapesFieldOnceVectors(t *testing.T, u *ir.Unit) (string, []fieldOnceVector) {
	t.Helper()
	const root = "A"
	m := tabletext.NewModel(u)
	v := fieldOnceVocab(t, u)
	a := fieldOnceEncode(t, u, "A", `{"x":7}`)
	b := fieldOnceEncode(t, u, "B", `{"x":9}`)
	sa := fieldOnceSpots(t, m, "A", a, v)
	sb := fieldOnceSpots(t, m, "B", b, v)
	rb := sa[0].Width
	w := newFieldOnceBatch(1)
	w.putSpan(a, 8, sa[len(sa)-1].Off)
	w.putSpan(b, 8, sb[len(sb)-1].Off+rb)
	return root, []fieldOnceVector{
		{
			name:      "one field id at two shapes in one body",
			wire:      w.bytes(),
			refused:   true,
			dropCheck: "check(value.x == 0, 'the refused batch kept x=${value.x}');",
		},
		{
			name:       "each field named once at its own shape",
			wire:       a,
			cleanCheck: "check(value.x == 7, 'x=${value.x}');",
		},
	}
}

// TestDartMessageBodyFieldOnce holds §3.3's body rule on the emitted Dart: a
// field id twice in one body is damage, an unknown entry included, and so is a
// nested body's field and a keyed array's slot. The Go engine's verdict on the
// same bytes is the expectation, and a body naming each field once still reads.
func TestDartMessageBodyFieldOnce(t *testing.T) {
	t.Parallel()
	t.Run("backend LoginRequest", func(t *testing.T) {
		u := batchUnit(t, "tables/backend")
		root, vectors := backendFieldOnceVectors(t, u)
		fieldOnceHoldToEngine(t, u, root, vectors)
	})
	t.Run("nested and keyed bodies", func(t *testing.T) {
		u := unitFrom(t, fieldOnceSyntheticUnit)
		root, vectors := syntheticFieldOnceVectors(t, u)
		fieldOnceHoldToEngine(t, u, root, vectors)
	})
	t.Run("one id at two shapes", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, fieldOnceTwoShapesUnit)
		root, vectors := twoShapesFieldOnceVectors(t, u)
		fieldOnceHoldToEngine(t, u, root, vectors)
	})
}

func fieldOnceHoldToEngine(t *testing.T, u *ir.Unit, root string, vectors []fieldOnceVector) {
	t.Helper()
	for _, v := range vectors {
		if got := fieldOnceGoRefused(t, u, root, v.wire); got != v.refused {
			t.Fatalf("%s: the Go engine verdict is refused=%v, want %v", v.name, got, v.refused)
		}
	}
	body := fieldOnceProbeBody(root, tablewire.Announce(u), vectors)
	out, err := runBatchProbe(t, u, root, body)
	if err != nil {
		t.Fatalf("the body's field-once rule did not run green: %v\n%s", err, out)
	}
}

// TestDartMessageBodyFieldOnceNegativeControl takes the repeat check out of the
// emitted source in memory and requires the repeated field to be accepted, so
// the red is the rule and not the harness.
func TestDartMessageBodyFieldOnceNegativeControl(t *testing.T) {
	t.Parallel()
	u := batchUnit(t, "tables/backend")
	root, vectors := backendFieldOnceVectors(t, u)
	body := fieldOnceProbeBody(root, tablewire.Announce(u), vectors)
	replaced := 0
	out, err := runBatchProbeWith(t, u, root, body, func(files map[string][]byte) {
		for name, data := range files {
			n := strings.Count(string(data), "r.seenHasId(named)")
			if n == 0 {
				continue
			}
			replaced += n
			files[name] = []byte(strings.ReplaceAll(string(data), "r.seenHasId(named)", "false"))
		}
	})
	if replaced == 0 {
		t.Fatal("the emitted source carries no repeat check, so this control removes nothing")
	}
	if err == nil {
		t.Fatalf("with the repeat check removed the repeated body was still refused:\n%s", out)
	}
}
