package rusttable

// The Rust leg's answers to the shared hostile corpus
// (internal/ci/hostilecorpus), each pinned by a focused test beside the
// corpus row:
//
//   - a [Min..N]T array rides the variable form: born at Min, its birth
//     elided on save, a count below Min refused on the wire and text reads;
//   - the &node label rules hold inside a value the text read skips, under a
//     variable root, and a reference may carry a trailing comma;
//   - a union's arm and a map's value sit one level past their object, as the
//     engine counts depth, so the depth cap refuses where the engine's does;
//   - a map's text read indexes the keys it placed by hash, never a linear
//     find, and a repeated key still refuses;
//   - a bool written from a dereferenced union arm carries no parentheses.
//
// The engine (internal/tabletext, internal/tablewire) is the oracle for every
// verdict. A FUNCTIONAL TEST where it runs rustc: no clock, no socket, no
// network.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const hostileMinSrc = `package hostilemin

fixed table Rec
{
    a int32
}

table R
{
    xs   [2..4]uint8
    recs [2..3]Rec
    tail int32
}
`

// hostileMinWriterSrc is the record at another build with no minimum, so it
// can send fewer elements than the reader's Min.
const hostileMinWriterSrc = `package hostilemin

fixed table Rec
{
    a int32
}

table R
{
    xs   [..4]uint8
    recs [..3]Rec
    tail int32
}
`

func engineAccepts(t *testing.T, m *tabletext.Model, table, text string) bool {
	t.Helper()
	v := m.New(m.Lookup(table))
	var rep tabletext.Report
	return m.Read(v, []byte(text), &rep)
}

func engineEncode(t *testing.T, m *tabletext.Model, table, text string) []byte {
	t.Helper()
	v := m.New(m.Lookup(table))
	var rep tabletext.Report
	if !m.Read(v, []byte(text), &rep) {
		t.Fatalf("the engine did not read %s: %+v", text, rep)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("the engine did not encode %s: %v", text, err)
	}
	return wire
}

func rustRaw(s string) string {
	if strings.Contains(s, `"##`) {
		panic("text holds the raw-string terminator")
	}
	return `r##"` + s + `"##`
}

func TestRustVariableCountedMinBornElidedAndHeld(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	u := unitFrom(t, hostileMinSrc)
	m := tabletext.NewModel(u)
	w := tabletext.NewModel(unitFrom(t, hostileMinWriterSrc))
	fresh, err := tablewire.Encode(m, m.New(m.Lookup("R")))
	if err != nil {
		t.Fatal(err)
	}
	set := engineEncode(t, m, "R", `{"xs":[1,2,3],"recs":[{"a":4},{}],"tail":5}`)
	short := engineEncode(t, w, "R", `{"xs":[7],"recs":[{"a":1}]}`)
	oneRec := engineEncode(t, w, "R", `{"xs":[1,2],"recs":[{"a":1}]}`)
	for _, c := range []struct {
		text string
		want bool
	}{{`{"xs":[1]}`, false}, {`{"recs":[{"a":1}]}`, false}, {`{"xs":[1,2]}`, true}, {`{"tail":3}`, true}} {
		if got := engineAccepts(t, m, "R", c.text); got != c.want {
			t.Fatalf("the oracle reads %s as %v, the test expects %v", c.text, got, c.want)
		}
	}
	body := fmt.Sprintf(`
    let born = R::default();
    println!("MIN born {}", born.xs == vec![0u8, 0] && born.recs.len() == 2 && born.recs[0].a == 0);
    println!("MIN birth_elides {}", r_save_checked(&born).map(|b| b == vec!%s).unwrap_or(false));
    let mut v = R::default();
    v.xs = vec![1, 2, 3];
    v.recs[0].a = 4;
    v.tail = 5;
    println!("MIN set_matches {}", r_save_checked(&v).map(|b| b == vec!%s).unwrap_or(false));
    let mut rep = TableReport::default();
    let back = r_load(&vec!%s, &mut rep);
    println!("MIN absent_reads_born {}", back.map(|b| b.xs == vec![0u8, 0] && b.recs.len() == 2).unwrap_or(false) && !rep.damaged);
    let mut rep = TableReport::default();
    println!("MIN wire_below_min {}", r_load(&vec!%s, &mut rep).is_none());
    let mut rep = TableReport::default();
    println!("MIN wire_table_below_min {}", r_load(&vec!%s, &mut rep).is_none());
    let mut rep = TableReport::default();
    println!("MIN text_below_min {}", r_from_json("{\"xs\":[1]}", &mut rep).is_none());
    let mut rep = TableReport::default();
    println!("MIN text_table_below_min {}", r_from_json("{\"recs\":[{\"a\":1}]}", &mut rep).is_none());
    let mut rep = TableReport::default();
    println!("MIN text_at_min {}", r_from_json("{\"xs\":[1,2]}", &mut rep).map(|b| b.xs == vec![1u8, 2]).unwrap_or(false));
    let mut rep = TableReport::default();
    println!("MIN text_absent_born {}", r_from_json("{\"tail\":3}", &mut rep).map(|b| b.xs.len() == 2 && b.recs.len() == 2).unwrap_or(false));
`, rustByteVector(fresh), rustByteVector(set), rustByteVector(fresh), rustByteVector(short), rustByteVector(oneRec))
	out, err := rustConstructProbe(t, u, body)
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, id := range []string{"born", "birth_elides", "set_matches", "absent_reads_born", "wire_below_min", "wire_table_below_min", "text_below_min", "text_table_below_min", "text_at_min", "text_absent_born"} {
		if !strings.Contains(out, "MIN "+id+" true") {
			t.Errorf("%s: the Rust variable form does not hold [Min..N]T:\n%s", id, out)
		}
	}
}

const hostileLabelSrc = `package hostilelabel

table Node
{
    value int32
    next  *Node
}

table Scene
{
    head  *Node
    alias *Node
}

fixed table Flat
{
    a int32
}
`

func TestRustTextLabelRulesHoldInSkippedValues(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	u := unitFrom(t, hostileLabelSrc)
	m := tabletext.NewModel(u)
	cases := []struct {
		name, table, text string
		want              bool
	}{
		{"reference_trailing_comma", "Scene", `{"head": {"&node": 1, "value": 5}, "alias": {"&node": 1,}}`, true},
		{"skipped_second_definition", "Scene", `{"head": {"&node": 1, "value": 1}, "zz": {"&node": 1, "value": 2}}`, false},
		{"skipped_label_not_an_integer", "Scene", `{"zz": {"&node": 1.5}}`, false},
		{"skipped_reserved_key_misspelled", "Scene", `{"zz": {"&foo": 1}}`, false},
		{"skipped_label_not_first", "Scene", `{"zz": {"value": 1, "&node": 2}}`, false},
		{"skipped_reference_before_definition", "Scene", `{"zz": {"&node": 5}, "head": {"&node": 5, "value": 1}}`, false},
		{"skipped_reference_undefined", "Scene", `{"zz": {"&node": 5}}`, false},
		{"skipped_definition_then_reference", "Scene", `{"zz": {"&node": 9, "value": 1}, "head": {"&node": 9}}`, true},
		{"skipped_plain_object", "Scene", `{"zz": {"value": 1}, "head": {"&node": 5, "value": 1}}`, true},
		{"fixed_root_skips_whole", "Flat", `{"zz": {"&node": 1}}`, true},
	}
	var body strings.Builder
	for _, c := range cases {
		if got := engineAccepts(t, m, c.table, c.text); got != c.want {
			t.Fatalf("%s: the oracle reads it as %v, the test expects %v", c.name, got, c.want)
		}
		fn := "scene_from_json"
		if c.table == "Flat" {
			fn = "flat_from_json"
		}
		fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); println!(\"LABEL %s {}\", %s(%s, &mut rep).is_some()); }\n", c.name, fn, rustRaw(c.text))
	}
	body.WriteString("    { let mut rep = TableReport::default(); let s = scene_from_json(" + rustRaw(cases[0].text) + ", &mut rep);\n")
	body.WriteString("      println!(\"LABEL alias_is_head {}\", s.map(|s| matches!((&s.head, &s.alias), (Some(a), Some(b)) if Rc::ptr_eq(a, b))).unwrap_or(false)); }\n")
	out, err := rustConstructProbe(t, u, "    use std::rc::Rc;\n"+body.String())
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, c := range cases {
		if !strings.Contains(out, fmt.Sprintf("LABEL %s %v", c.name, c.want)) {
			t.Errorf("%s: the Rust text read does not answer %v as the engine does:\n%s", c.name, c.want, out)
		}
	}
	if !strings.Contains(out, "LABEL alias_is_head true") {
		t.Errorf("a reference with a trailing comma does not resolve to its definition:\n%s", out)
	}
}

const hostileDepthSrc = `package hostiledepth

table Item
{
    count int32
}

table Squad
{
    roster map[uint8]Item
}

table Chunk
{
    data int32
    next *Chunk
}

union Force
{
    squad Squad
    chunk Chunk
    plain int32
}

table Root
{
    one Squad
    arm Force
}
`

func TestRustTextDepthCountsUnionArmsAndMapValues(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	u := unitFrom(t, hostileDepthSrc)
	m := tabletext.NewModel(u)
	brackets := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	chain := func(n int) string {
		s := `{"data": 1}`
		for range n {
			s = `{"data": 1, "next": ` + s + `}`
		}
		return s
	}
	shapes := []struct {
		name string
		text func(n int) string
	}{
		{"union_arm", func(n int) string { return `{"arm": {"squad": {"zz": ` + brackets(n) + `}}}` }},
		{"map_value", func(n int) string { return `{"one": {"roster": {"1": {"count": 1, "zz": ` + brackets(n) + `}}}}` }},
		{"arm_pointer", func(n int) string { return `{"arm": {"chunk": ` + chain(n) + `}}` }},
	}
	var body strings.Builder
	for _, s := range shapes {
		// the deepest text the engine accepts, and one level past it
		at := -1
		for n := 1; n < 300; n++ {
			if !engineAccepts(t, m, "Root", s.text(n)) {
				at = n - 1
				break
			}
		}
		if at < 1 {
			t.Fatalf("%s: the oracle has no depth boundary under 300", s.name)
		}
		fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); println!(\"DEPTH %s_at_cap {}\", root_from_json(%s, &mut rep).is_some()); }\n", s.name, rustRaw(s.text(at)))
		fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); println!(\"DEPTH %s_past_cap {}\", root_from_json(%s, &mut rep).is_none()); }\n", s.name, rustRaw(s.text(at+1)))
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, s := range shapes {
		for _, id := range []string{s.name + "_at_cap", s.name + "_past_cap"} {
			if !strings.Contains(out, "DEPTH "+id+" true") {
				t.Errorf("%s: the Rust text read does not hold the engine's depth cap:\n%s", id, out)
			}
		}
	}
}

const hostileMapKeysSrc = `package hostilemapkeys

table Item
{
    count int32
}

table Book
{
    names map[string(16)]Item
    ids   map[uint32]Item
}
`

func TestRustTextMapKeysIndexedByHash(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, hostileMapKeysSrc)
	modules, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range modules {
		// a placed entry is never searched for linearly by its key: that
		// read is O(n^2) in the keys a text holds
		if strings.Contains(string(src), ".iter().position(|x| x.key ==") {
			t.Errorf("%s: the map text read finds a repeated key by a linear search", name)
		}
	}
	keyedRequireRustc(t)
	m := tabletext.NewModel(u)
	cases := []struct {
		name, text string
		want       bool
	}{
		{"distinct_text_keys", `{"names": {"b": {"count": 1}, "a": {"count": 2}}}`, true},
		{"repeated_text_key", `{"names": {"a": {"count": 1}, "a": {"count": 2}}}`, false},
		{"distinct_integer_keys", `{"ids": {"9": {"count": 1}, "3": {"count": 2}}}`, true},
		{"repeated_integer_key", `{"ids": {"3": {"count": 1}, "3": {"count": 2}}}`, false},
	}
	var body strings.Builder
	for _, c := range cases {
		if got := engineAccepts(t, m, "Book", c.text); got != c.want {
			t.Fatalf("%s: the oracle reads it as %v, the test expects %v", c.name, got, c.want)
		}
		fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); println!(\"KEYS %s {}\", book_from_json(%s, &mut rep).is_some()); }\n", c.name, rustRaw(c.text))
	}
	body.WriteString("    { let mut rep = TableReport::default(); let b = book_from_json(" + rustRaw(cases[0].text) + ", &mut rep);\n")
	body.WriteString("      println!(\"KEYS ascending {}\", b.map(|b| b.names.len() == 2 && b.names[0].key == \"a\" && b.names[1].key == \"b\").unwrap_or(false)); }\n")
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, c := range cases {
		if !strings.Contains(out, fmt.Sprintf("KEYS %s %v", c.name, c.want)) {
			t.Errorf("%s: the Rust map read does not answer %v as the engine does:\n%s", c.name, c.want, out)
		}
	}
	if !strings.Contains(out, "KEYS ascending true") {
		t.Errorf("the map read does not hold its entries in ascending key order:\n%s", out)
	}
}

const hostileBoolArmSrc = `package hostilebool

union Pick
{
    small int16
    on    bool
}

table R
{
    pick Pick
}
`

func TestRustBoolArmWritesWithoutParentheses(t *testing.T) {
	t.Parallel()
	modules, err := Generate(unitFrom(t, hostileBoolArmSrc))
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range modules {
		// rustc's unused_parens: `if (*arm) {` is a warning in every build
		if strings.Contains(string(src), "if (*") {
			t.Errorf("%s: a bool is tested through parentheses rustc warns on", name)
		}
	}
}
