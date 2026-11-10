package gotable

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// discardUnit has a fixed root and a variable root, each carrying a nested
// body between two scalars, so damage inside the nested body leaves the walk
// able to read on past its L: the case where a reader that kept what it
// decoded would hand back a value.
const discardUnit = `package discard

fixed table Inner
{
    n int32 = 0
}

fixed table Root
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
}

table Graph
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

// discardWire encodes the text as the root and then damages the nested body:
// Inner's one field, kind 4 holding 3, gets the kind 37 no reader can skip.
// The engine is asked first, so the vector is damaged by the rule and not by
// assumption.
func discardWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	// A fixed root uses form 3: its positional image has no field-kind byte.
	// Truncating its final record is damage in that form. Variable roots
	// retain the nested-body corruption below.
	if tablewire.FixedRoot(m, inst.Def) {
		wire = wire[:len(wire)-1]
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err == nil || !report.Refused {
			t.Fatalf("%s: truncated fixed record accepted: ok=%v err=%v %+v", root, ok, err, report)
		}
		return wire
	}
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err == nil || !report.Refused {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return wire
}

func goBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return "[]byte{" + strings.Join(parts, ", ") + "}"
}

// TestDamageDiscardsTheWholeLoadGo is the Go leg's gate for DAMAGE DISCARDS
// THE WHOLE LOAD (docs/SPEC-TABLES.md §4): damage inside a nested body, with a
// whole field before it and after it, makes every strict read refuse without
// retaining any decoded value. The fixed load leaves its caller-owned values
// at defaults; the region and retaining loads hand back no root, with the
// retention store empty; the builder load refuses with its root at defaults;
// a message batch whose declared count exceeds its payload answers a count of
// 0 with every value reset,
// as does one with a stray byte after its last body; and malformed text leaves
// the value, fixed or variable, at its defaults. Red if any read hands back
// what it decoded.
func TestDamageDiscardsTheWholeLoadGo(t *testing.T) {
	t.Parallel()
	u := unitOf(t, discardUnit)
	fixed := discardWire(t, u, "Root", `{"a":5,"inner":{"n":3},"b":9}`)
	variable := discardWire(t, u, "Graph", `{"a":5,"inner":{"n":3},"b":9}`)
	src := fmt.Sprintf(`package discard

import (
	"testing"
	"unsafe"
)

func aligned(size int64) []byte {
	if size < 64 {
		size = 64
	}
	raw := make([]byte, size+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(size)]
}

func TestDiscard(t *testing.T) {
	fixed := %s
	variable := %s

	fixedValues := []Root{{}}
	RootReset(&fixedValues[0])
	var r TableReport
	if n := RootFixedLoad(fixedValues, fixed, make([]TableFixedEntry, 4096), &r); n >= 0 {
		t.Fatalf("fixed: RootFixedLoad accepted damaged input: n=%%d", n)
	}
	if r.Err() == nil {
		t.Fatalf("fixed: the report did not record refusal: %%+v", r)
	}
	if got := fixedValues[0]; got.A != 0 || got.B != 0 || got.Inner.N != 0 {
		t.Fatalf("fixed: a damaged load kept values a=%%d b=%%d n=%%d", got.A, got.B, got.Inner.N)
	}

	need := GraphLoadMeasure(variable)
	var g TableReport
	if root := GraphLoad(aligned(need), variable, &g); root != nil {
		t.Fatalf("variable: GraphLoad returned a root on damage: a=%%d b=%%d", root.A, root.B)
	}
	if g.Err() == nil {
		t.Fatalf("variable: the report did not record refusal: %%+v", g)
	}

	retain := TableRetain{Bytes: make([]byte, 1<<12), Ids: make([]TableRetainID, 16)}
	var rr TableReport
	if GraphLoadRetain(aligned(1<<16), variable, &retain, &rr) != nil {
		t.Fatalf("retain: LoadRetain handed back a root on damage")
	}
	if !rr.Refused() || retain.Used != 0 || retain.Count != 0 {
		t.Fatalf("retain: damage did not refuse with an empty store: report=%%+v used=%%d count=%%d", rr, retain.Used, retain.Count)
	}

	var b GraphBuilder
	if !b.Init() {
		t.Fatal("builder: init")
	}
	defer b.Shutdown()
	var br TableReport
	if GraphLoadBuilder(&b, variable, &br) {
		t.Fatalf("builder: GraphLoadBuilder accepted damaged input")
	}
	if !br.Refused() {
		t.Fatalf("builder: the report did not record refusal: %%+v", br)
	}
	if root := b.GetRoot(); root.A != 0 || root.B != 0 || root.Inner.N != 0 {
		t.Fatalf("builder: a damaged load handed back a=%%d b=%%d n=%%d", root.A, root.B, root.Inner.N)
	}

	var w1, w2 Root
	RootReset(&w1)
	RootReset(&w2)
	w1.A, w1.B, w1.Inner.N = 5, 9, 3
	w2.A, w2.B, w2.Inner.N = 6, 8, 4
	values := []*Root{&w1, &w2}
	batch := make([]byte, RootMeasureMessages(values, nil))
	if RootSaveMessages(values, batch, nil) != int64(len(batch)) {
		t.Fatal("message: save")
	}
	announcement := make([]byte, AnnounceMeasure())
	if Announce(announcement) != int64(len(announcement)) {
		t.Fatal("message: announce")
	}
	var storage [64]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	var arep TableReport
	if !AnnounceRead(&vocab, announcement, &arep) {
		t.Fatalf("message: the announcement was refused: %%+v", arep)
	}
	decoded := make([]Root, 16)
	var clean TableReport
	if n, ok := RootLoadMessages(decoded, &vocab, batch, &clean); !ok || n != 2 || decoded[1].B != 8 {
		t.Fatalf("message: the clean batch did not read: n=%%d ok=%%v %%+v", n, ok, clean)
	}
	short := append([]byte(nil), batch...)
	// One additional empty body can consume a terminator from the final zero
	// padding; this count needs more bodies than the payload can pad.
	short[1] = byte(len(decoded) - 1)
	var mr TableReport
	n, ok := RootLoadMessages(decoded, &vocab, short, &mr)
	if ok || n != 0 {
		t.Fatalf("message: a batch that declares a missing body answered n=%%d ok=%%v", n, ok)
	}
	if !mr.Refused() {
		t.Fatalf("message: damaged batch did not refuse: report=%%+v", mr)
	}
	for i, got := range decoded {
		if got.A != 0 || got.B != 0 || got.Inner.N != 0 {
			t.Fatalf("message: damaged batch kept values at index %%d: a=%%d b=%%d n=%%d", i, got.A, got.B, got.Inner.N)
		}
	}
	if n, ok := RootLoadMessages(decoded, &vocab, batch, &clean); !ok || n != 2 {
		t.Fatalf("message: the clean batch did not read again: n=%%d ok=%%v", n, ok)
	}
	trailing := append(append([]byte(nil), batch...), 0xff)
	var tr TableReport
	n, ok = RootLoadMessages(decoded, &vocab, trailing, &tr)
	if ok || n != 0 || !tr.Refused() || decoded[0].A != 0 || decoded[1].B != 0 {
		t.Fatalf("message: batch with a stray byte did not refuse with reset values: n=%%d ok=%%v report=%%+v first.a=%%d", n, ok, tr, decoded[0].A)
	}

	var text Root
	var jr TableReport
	if RootFromJSON(&text, []byte(`+"`"+`{"a":5,"inner":{"n":3},"b":9,"&x":1}`+"`"+`), &jr) {
		t.Fatalf("text: RootFromJSON accepted malformed input")
	}
	if !jr.Refused() || text.A != 0 || text.B != 0 || text.Inner.N != 0 {
		t.Fatalf("text: malformed text did not refuse with reset values: report=%%+v a=%%d b=%%d n=%%d", jr, text.A, text.B, text.Inner.N)
	}
	var tb GraphBuilder
	if !tb.Init() {
		t.Fatal("text builder: init")
	}
	defer tb.Shutdown()
	var gr TableReport
	if GraphFromJSON(&tb, []byte(`+"`"+`{"a":5,"inner":{"n":3},"b":9,"&x":1}`+"`"+`), &gr) {
		t.Fatalf("text: GraphFromJSON accepted malformed input")
	}
	if root := tb.GetRoot(); !gr.Refused() || root.A != 0 || root.B != 0 || root.Inner.N != 0 {
		t.Fatalf("text: malformed variable text did not refuse with reset values: report=%%+v a=%%d b=%%d", gr, root.A, root.B)
	}
}
`, goBytes(fixed), goBytes(variable))
	out, err := runGeneratedUnit(t, u, src, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
