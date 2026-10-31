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
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err != nil || !report.Malformed {
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
// whole field before it and after it, makes every public read hand back
// nothing usable. The fixed LoadTolerant answers false with the value at its
// defaults and the strict Load refuses; the region LoadTolerant and LoadRetain
// hand back no root, with the retention store empty; the builder load answers
// false with the builder's root at its defaults; a message batch that declares
// one body more than it carries answers a count of 0 with every value reset,
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

	var v Root
	RootReset(&v)
	var r TableReport
	if RootLoadTolerant(&v, fixed, &r) {
		t.Fatalf("fixed: LoadTolerant answered true on damage")
	}
	if !r.Malformed {
		t.Fatalf("fixed: the report does not say malformed: %%+v", r)
	}
	if v.A != 0 || v.B != 0 || v.Inner.N != 0 {
		t.Fatalf("fixed: a damaged load handed back a=%%d b=%%d n=%%d", v.A, v.B, v.Inner.N)
	}
	var strict Root
	var s TableReport
	if RootLoad(&strict, fixed, &s) || !s.Malformed {
		t.Fatalf("fixed: the strict Load did not refuse: %%+v", s)
	}

	need := GraphLoadMeasure(variable)
	var g TableReport
	if root := GraphLoadTolerant(aligned(need), variable, &g); root != nil {
		t.Fatalf("variable: LoadTolerant handed back a root on damage: a=%%d b=%%d", root.A, root.B)
	}
	if !g.Malformed {
		t.Fatalf("variable: the report does not say malformed: %%+v", g)
	}

	retain := TableRetain{Bytes: make([]byte, 1<<12), Ids: make([]TableRetainID, 16)}
	var rr TableReport
	if GraphLoadRetain(aligned(1<<16), variable, &retain, &rr) != nil {
		t.Fatalf("retain: LoadRetain handed back a root on damage")
	}
	if !rr.Malformed || retain.Used != 0 || retain.Count != 0 {
		t.Fatalf("retain: malformed=%%v used=%%d count=%%d", rr.Malformed, retain.Used, retain.Count)
	}

	var b GraphBuilder
	if !b.Init() {
		t.Fatal("builder: init")
	}
	defer b.Shutdown()
	var br TableReport
	if GraphLoadBuilderTolerant(&b, variable, &br) {
		t.Fatalf("builder: LoadBuilderTolerant answered true on damage")
	}
	if !br.Malformed {
		t.Fatalf("builder: the report does not say malformed: %%+v", br)
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
	decoded := make([]Root, 3)
	var clean TableReport
	if n, ok := RootLoadMessagesTolerant(decoded, &vocab, batch, &clean); !ok || n != 2 || decoded[1].B != 8 {
		t.Fatalf("message: the clean batch did not read: n=%%d ok=%%v %%+v", n, ok, clean)
	}
	short := append([]byte(nil), batch...)
	short[1]++
	var mr TableReport
	n, ok := RootLoadMessagesTolerant(decoded, &vocab, short, &mr)
	if ok || n != 0 {
		t.Fatalf("message: a batch that declares a missing body answered n=%%d ok=%%v", n, ok)
	}
	if !mr.Malformed || decoded[0].A != 0 || decoded[1].B != 0 {
		t.Fatalf("message: a damaged batch handed back malformed=%%v first.a=%%d second.b=%%d", mr.Malformed, decoded[0].A, decoded[1].B)
	}
	if n, ok := RootLoadMessagesTolerant(decoded, &vocab, batch, &clean); !ok || n != 2 {
		t.Fatalf("message: the clean batch did not read again: n=%%d ok=%%v", n, ok)
	}
	trailing := append(append([]byte(nil), batch...), 0xff)
	var tr TableReport
	n, ok = RootLoadMessagesTolerant(decoded, &vocab, trailing, &tr)
	if ok || n != 0 || !tr.Malformed || decoded[0].A != 0 || decoded[1].B != 0 {
		t.Fatalf("message: a batch with a stray byte after its bodies handed back n=%%d ok=%%v malformed=%%v first.a=%%d", n, ok, tr.Malformed, decoded[0].A)
	}

	var text Root
	var jr TableReport
	if RootFromJSONTolerant(&text, []byte(`+"`"+`{"a":5,"inner":{"n":3},"b":9,"&x":1}`+"`"+`), &jr) {
		t.Fatalf("text: FromJSONTolerant answered true on malformed text")
	}
	if !jr.Malformed || text.A != 0 || text.B != 0 || text.Inner.N != 0 {
		t.Fatalf("text: malformed text handed back malformed=%%v a=%%d b=%%d n=%%d", jr.Malformed, text.A, text.B, text.Inner.N)
	}
	var tb GraphBuilder
	if !tb.Init() {
		t.Fatal("text builder: init")
	}
	defer tb.Shutdown()
	var gr TableReport
	if GraphFromJSONTolerant(&tb, []byte(`+"`"+`{"a":5,"inner":{"n":3},"b":9,"&x":1}`+"`"+`), &gr) {
		t.Fatalf("text: the variable FromJSONTolerant answered true on malformed text")
	}
	if root := tb.GetRoot(); !gr.Malformed || root.A != 0 || root.B != 0 || root.Inner.N != 0 {
		t.Fatalf("text: malformed variable text handed back malformed=%%v a=%%d b=%%d", gr.Malformed, root.A, root.B)
	}
}
`, goBytes(fixed), goBytes(variable))
	out, err := runGeneratedUnit(t, u, src, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
