package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// jsonrefModel compiles one small unit for the reference JSON read's own
// tests (review 2026-11-21, TABLETEXT-1 to -6).
func jsonrefModel(t *testing.T, src string) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func jsonrefRead(t *testing.T, m *tabletext.Model, root, text string) (bool, tabletext.Report) {
	t.Helper()
	st := m.Lookup(root)
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	var r tabletext.Report
	ok := m.Read(m.New(st), []byte(text), &r)
	return ok, r
}

// jsonrefDepthSchema is a map-of-pointer chain and a union chain, both
// recursive, so a text can reach the 128-level cap through either.
const jsonrefDepthSchema = `package probe
table Node
{
    kids map[string(8)]*Node
}
table Link
{
    u Choice
}
union Choice
{
    w Wrap
}
table Wrap
{
    next *Link
}
`

// mapChainText is n Node objects nested through the map: 2n-1 containers,
// the deepest at depth 2n-2 below the root.
func mapChainText(n int) string {
	return strings.Repeat(`{"kids":{"a":`, n-1) + `{}` + strings.Repeat(`}}`, n-1)
}

// unionChainText is n Link -> Choice -> Wrap cycles: 3n+1 containers, the
// deepest at depth 3n below the root.
func unionChainText(n int) string {
	return strings.Repeat(`{"u":{"w":{"next":`, n) + `{}` + strings.Repeat(`}}}`, n)
}

// TABLETEXT-1. docs/SPEC-TABLES.md §16.2: "The read's cap counts every object
// and array the reader walks ... 128 container levels are read and 129
// refuse." A map object and a union object are containers like any other: the
// map chain's 65th node sits at depth 128 and reads, the 66th at 130 refuses;
// the union chain's 42nd cycle ends at depth 126 and reads, the 43rd at 129
// refuses. The reference counted a map and a union as no level and read both
// chains to twice the cap.
func TestJsonrefDepthCountsMapAndUnionObjects(t *testing.T) {
	t.Parallel()
	m := jsonrefModel(t, jsonrefDepthSchema)
	for _, c := range []struct {
		name, root, text string
		accept           bool
	}{
		{"map chain 65 nodes, depth 128", "Node", mapChainText(65), true},
		{"map chain 66 nodes, depth 130", "Node", mapChainText(66), false},
		{"union chain 42 cycles, depth 126", "Link", unionChainText(42), true},
		{"union chain 43 cycles, depth 129", "Link", unionChainText(43), false},
	} {
		ok, r := jsonrefRead(t, m, c.root, c.text)
		if ok != c.accept {
			t.Errorf("%s: read=%v (%+v), want %v", c.name, ok, r, c.accept)
		}
	}
}

// TABLETEXT-1, the skip path: an unknown arm's value sits two levels below
// the union's holder, so it is skipped at that depth. A value one level past
// the cap refuses there exactly as it would at a known arm.
func TestJsonrefUnknownArmSkipCountsTheUnionObject(t *testing.T) {
	t.Parallel()
	m := jsonrefModel(t, jsonrefDepthSchema)
	// 42 cycles put the innermost Link at depth 126; its union object is at
	// 127 and an unknown arm's value at 128 (reads) or one array deeper, 129
	// (refuses).
	head := strings.Repeat(`{"u":{"w":{"next":`, 42)
	tail := strings.Repeat(`}}}`, 42)
	if ok, r := jsonrefRead(t, m, "Link", head+`{"u":{"zz":{}}}`+tail); !ok {
		t.Errorf("unknown arm value at depth 128: refused %+v, want read", r)
	}
	if ok, r := jsonrefRead(t, m, "Link", head+`{"u":{"zz":[[]]}}`+tail); ok {
		t.Errorf("unknown arm value reaching depth 129: read %+v, want refused", r)
	}
}

// jsonrefGraphSchema is a variable root with two pointer slots; zz is a key
// it does not know.
const jsonrefGraphSchema = `package probe
table Root
{
    a *PNode
    b *PNode
    p *PNode
}
table PNode
{
    x int32
}
`

// jsonrefLabelCase is one text and the verdict docs/SPEC-TABLES.md §16.2 and
// §16.7 give it; the generated Go leg's differential test
// (internal/codegen/gotable/jsonref_test.go) holds the same list.
type jsonrefLabelCase struct {
	Name   string
	Text   string
	Accept bool
}

// jsonrefLabelCases are the `&node` texts of TABLETEXT-2, -3 and -8.
var jsonrefLabelCases = []jsonrefLabelCase{
	// TABLETEXT-2: RFC 8259 needs the value-separator after the label.
	{"definition with a comma", `{"p": {"&node": 1, "x": 2}}`, true},
	{"definition with no comma after the label", `{"p": {"&node": 1 "x": 2}}`, false},
	// TABLETEXT-8: §16.2 "Trailing commas in objects and arrays are accepted
	// on read"; a reference is an object.
	{"reference", `{"a": {"&node": 1, "x": 5}, "b": {"&node": 1}}`, true},
	{"reference with a trailing comma", `{"a": {"&node": 1, "x": 5}, "b": {"&node": 1,}}`, true},
	{"undefined label alone with a trailing comma", `{"a": {"&node": 1,}}`, false},
	// TABLETEXT-3: §16.7 "The label grammar and the definition rules apply
	// EVERYWHERE, inside an unknown key's skipped value too".
	{"skipped reference before its definition", `{"zz": {"&node": 5}, "a": {"&node": 5, "x": 1}}`, false},
	{"skipped label after a field", `{"zz": {"x": 1, "&node": 2}}`, false},
	{"skipped label not an integer", `{"zz": {"&node": 1.5}}`, false},
	{"skipped reserved key of another spelling", `{"zz": {"&foo": 1}}`, false},
	{"skipped reference never defined", `{"zz": {"&node": 5}}`, false},
	{"label defined placed and again skipped", `{"a": {"&node":1,"x":1}, "zz": {"&node":1,"x":2}}`, false},
	{"skipped definition with no comma after the label", `{"zz": {"&node": 1 "x": 2}}`, false},
	{"skipped reference inside its own definition", `{"zz": {"&node": 3, "x": {"&node": 3}}}`, false},
	// the lawful twins on the same root
	{"skipped definition, placed reference", `{"zz": {"&node": 5, "x": 1}, "a": {"&node": 5}}`, true},
	{"placed definition, skipped reference", `{"a": {"&node": 5, "x": 1}, "zz": {"&node": 5}}`, true},
	{"placed definition, skipped reference with a trailing comma", `{"a": {"&node": 5, "x": 1}, "zz": {"&node": 5,}}`, true},
	{"skipped definition, skipped reference", `{"zz": {"&node": 5, "x": 1}, "yy": [{"&node": 5}]}`, true},
}

// TABLETEXT-2, -3, -8: the reference reader's verdict on every label text.
func TestJsonrefLabelRules(t *testing.T) {
	t.Parallel()
	m := jsonrefModel(t, jsonrefGraphSchema)
	for _, c := range jsonrefLabelCases {
		ok, r := jsonrefRead(t, m, "Root", c.Text)
		if ok != c.Accept {
			t.Errorf("%s: %s: read=%v (%+v), want %v", c.Name, c.Text, ok, r, c.Accept)
		}
	}
}

// TABLETEXT-4. docs/SPEC-TABLES.md §16.2: "The cap never reaches a map key or
// a string value — those are data a conforming writer emits"; a `string(N)`
// key's bound is N (§2.8). A 280-byte key of a `map[string(300)]` writes and
// reads back; 300 reads; 301 refuses.
func TestJsonrefStringMapKeyTakesItsDeclaredBound(t *testing.T) {
	t.Parallel()
	m := jsonrefModel(t, `package probe
table Item
{
    v int32
}
table E
{
    names map[string(300)]Item
}
`)
	for _, c := range []struct {
		n      int
		accept bool
	}{{255, true}, {256, true}, {280, true}, {300, true}, {301, false}} {
		text := `{"names": {"` + strings.Repeat("k", c.n) + `": {"v": 1}}}`
		ok, r := jsonrefRead(t, m, "E", text)
		if ok != c.accept {
			t.Errorf("key of %d bytes: read=%v (%+v), want %v", c.n, ok, r, c.accept)
		}
	}
	// read-then-write: the reference's own output reads back
	st := m.Lookup("E")
	inst := m.New(st)
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"names": {"`+strings.Repeat("k", 280)+`": {"v": 1}}}`), &r) {
		t.Fatalf("280-byte key refused: %+v", r)
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if ok, r := jsonrefRead(t, m, "E", string(out)); !ok {
		t.Fatalf("the reference refuses its own output: %+v\n%s", r, out)
	}
}

// TABLETEXT-3 in the fast lane, both readers: the committed generated Go
// reader over tables/examples (internal/codegen/gotable/fuzzunit) and the
// reference read the same skipped-value label texts at Patrol, a VARIABLE
// root, and must give the spec's verdict (docs/SPEC-TABLES.md §16.7, "The
// label grammar and the definition rules apply EVERYWHERE, inside an unknown
// key's skipped value too"). Every `&node` here sits under an unknown key, so
// the rules run on the skip path, which needs no pointer field. A fixed root
// (PackConfig) skips such a value whole, as the corpus row
// reserved-ampersand-in-skipped-value pins, and both readers still read it.
func TestJsonrefSkippedLabelsAgreeWithGeneratedGo(t *testing.T) {
	t.Parallel()
	m := tabletext.NewModel(unit(t))
	for _, c := range []struct {
		text   string
		accept bool
	}{
		{`{"zz": {"&node": 5}, "yy": {"&node": 5, "x": 1}}`, false},
		{`{"zz": {"x": 1, "&node": 2}}`, false},
		{`{"zz": {"&node": 1.5}}`, false},
		{`{"zz": {"&node": 01, "x": 1}}`, false},
		{`{"zz": {"&foo": 1}}`, false},
		{`{"zz": {"&node": 5}}`, false},
		{`{"zz": {"&node": 5,}}`, false},
		{`{"zz": {"&node":1,"x":1}, "yy": {"&node":1,"x":2}}`, false},
		{`{"zz": {"&node": 1 "x": 2}}`, false},
		{`{"zz": {"&node": 3, "x": {"&node": 3}}}`, false},
		{`{"zz": {"&node": 5, "x": 1}, "yy": [{"&node": 5}]}`, true},
		{`{"zz": {"&node": 5, "x": 1}, "yy": {"&node": 5,}}`, true},
		{`{"zz": {"&node": 5, "x": 1}}`, true},
		{`{"active": true, "zz": {"&node": 2, "x": {"y": [1, {"&node": 7, "z": 0}]}}, "yy": {"&node": 7}}`, true},
	} {
		inst := m.New(m.Lookup("Patrol"))
		var r tabletext.Report
		refOK := m.Read(inst, []byte(c.text), &r)
		var b tabledemo.PatrolBuilder
		if !b.Init() {
			t.Fatal("PatrolBuilder.Init")
		}
		var gr tabledemo.TableReport
		goOK := tabledemo.PatrolFromJSON(&b, []byte(c.text), &gr)
		b.Shutdown()
		if refOK != c.accept || goOK != c.accept {
			t.Errorf("Patrol %s: reference read=%v (%+v), generated Go read=%v (%v), spec %v", c.text, refOK, r, goOK, gr.Err(), c.accept)
		}
	}
	for _, text := range []string{`{"mystery": {"&node": 1}}`, `{"mystery": {"x": 1, "&node": 1.5}}`} {
		inst := m.New(m.Lookup("PackConfig"))
		var r tabletext.Report
		refOK := m.Read(inst, []byte(text), &r)
		var v tabledemo.PackConfig
		var gr tabledemo.TableReport
		goOK := tabledemo.PackConfigFromJSON(&v, []byte(text), &gr)
		if !refOK || !goOK {
			t.Errorf("fixed PackConfig %s: reference read=%v (%+v), generated Go read=%v (%v), want both read", text, refOK, r, goOK, gr.Err())
		}
	}
}
