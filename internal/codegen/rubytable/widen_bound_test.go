package rubytable_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A fixed array whose bound grew since the writer (docs/SPEC-TABLES.md §21.1:
// "a grown bound") reads as the writer's elements followed by one fresh
// default element per new slot, the value the constructor is born with there.
// Every row below writes `[2]T` with the Go engine and reads it as `[4]T` in
// Ruby, in the variable, message and fixed forms the element kind is carried
// in, under both pinned interpreters.

// widenBoundKind is one element kind of the table. decls are the declarations
// the element needs, two is the JSON of the writer's two elements, and fixed
// says the kind is carried in a fixed table (so the fixed form and the
// message form at a fixed root are read too).
type widenBoundKind struct {
	name  string
	decls string
	elem  string
	attrs string
	two   string
	fixed bool
	// optional spells the field ?[N]T: absent at birth, so the probe holds
	// its new slots to the engine's value alone.
	optional bool
}

var widenBoundKinds = []widenBoundKind{
	{name: "bool", elem: "bool", two: `[true,true]`, fixed: true},
	{name: "int8", elem: "int8", two: `[-5,6]`, fixed: true},
	{name: "uint16", elem: "uint16", two: `[65535,6]`, fixed: true},
	{name: "int32", elem: "int32", two: `[-5,6]`, fixed: true},
	{name: "uint64", elem: "uint64", two: `["18446744073709551615","6"]`, fixed: true},
	{name: "int128", elem: "int128", attrs: " | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727", two: `["-170141183460469231731687303715884105728","6"]`, fixed: true},
	{name: "uint128", elem: "uint128", two: `["340282366920938463463374607431768211455","6"]`, fixed: true},
	{name: "fixedpoint", elem: "fixed(12, 4)", attrs: " | min = -8, max = 7", two: `[1.5,-2.25]`, fixed: true},
	{name: "float32", elem: "float32", two: `[1.5,-2.25]`, fixed: true},
	{name: "float64", elem: "float64", two: `[1.5,-2.25]`, fixed: true},
	{name: "enum", decls: "enum Mode {\n Idle\n Live\n Done\n}\n", elem: "Mode", two: `["Live","Done"]`, fixed: true},
	{name: "flags", decls: "flags Caps {\n Read\n Write\n}\n", elem: "Caps", two: `[["Read"],["Read","Write"]]`, fixed: true},
	{name: "record", decls: "fixed table Inner {\n v int32 = 7\n s string(8) = \"born\"\n}\n", elem: "Inner", two: `[{"v":1,"s":"a"},{"v":2,"s":"b"}]`, fixed: true},
	{name: "record-nested-array", decls: "fixed table Inner {\n xs [3]int16\n ys [..3]int8\n ms [2]Mode\n}\nenum Mode {\n Idle\n Live\n}\n", elem: "Inner", two: `[{"xs":[1,2,3],"ys":[4],"ms":["Live","Idle"]},{"xs":[5,6,7],"ys":[8,9],"ms":["Idle","Live"]}]`, fixed: true},
	{name: "record-nested-union", decls: "fixed table Inner {\n pick Choice\n picks [2]Choice\n}\nunion Choice {\n count int32\n note string(8)\n}\n", elem: "Inner", two: `[{"pick":{"count":5},"picks":[{"note":"n"},{"count":6}]},{"pick":{"note":"m"},"picks":[{"count":7},{"note":"o"}]}]`, fixed: true},
	{name: "union", decls: "fixed table Inner {\n v int32\n}\nunion Choice {\n count int32\n nested Inner\n}\n", elem: "Choice", two: `[{"count":5},{"nested":{"v":3}}]`, fixed: true},
	{name: "union-array-arm", decls: "union Choice {\n count int32\n run [3]int16\n}\n", elem: "Choice", two: `[{"count":5},{"run":[1,2,3]}]`, fixed: true},
	{name: "record-map", decls: "table Inner {\n m map[string(8)]int32\n v int32 = 7\n}\n", elem: "Inner", two: `[{"m":{"a":1},"v":1},{"m":{"b":2},"v":2}]`},
	{name: "record-list", decls: "table Inner {\n xs []int32\n names []string(8)\n}\n", elem: "Inner", two: `[{"xs":[1,2],"names":["a"]},{"xs":[3],"names":["b","c"]}]`},
	{name: "optional-int32", elem: "int32", two: `[-5,6]`, fixed: true, optional: true},
	{name: "optional-enum", decls: "enum Mode {\n Idle\n Live\n Done\n}\n", elem: "Mode", two: `["Live","Done"]`, fixed: true, optional: true},
	{name: "optional-record", decls: "fixed table Inner {\n v int32 = 7\n}\n", elem: "Inner", two: `[{"v":1},{"v":2}]`, fixed: true, optional: true},
	{name: "pointer", decls: "table Node {\n v int32\n}\n", elem: "*Node", two: `[{"v":1},{"v":2}]`},
}

func widenBoundSchema(k widenBoundKind, bound int) string {
	var b strings.Builder
	opt := ""
	if k.optional {
		opt = "?"
	}
	b.WriteString("package widenbound\n")
	b.WriteString(k.decls)
	if k.fixed {
		fmt.Fprintf(&b, "fixed table Root {\n items %s[%d]%s%s\n}\n", opt, bound, k.elem, k.attrs)
	}
	fmt.Fprintf(&b, "table Doc {\n items %s[%d]%s%s\n}\n", opt, bound, k.elem, k.attrs)
	return b.String()
}

// widenBoundPlace reads the writer's two elements into a root instance.
func widenBoundPlace(t *testing.T, m *tabletext.Model, root, two string) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"items":`+two+`}`), &rep) || rep.Refused {
		t.Fatalf("fixture %s %s: %+v", root, two, rep)
	}
	return inst
}

// widenBoundWant is the Go engine's own read of the writer's variable-form
// bytes under the reader's declaration, as the reader's text: the value every
// form must land.
func widenBoundWant(t *testing.T, writer, reader *tabletext.Model, root, two string) string {
	t.Helper()
	wire, err := tablewire.EncodeVariable(writer, widenBoundPlace(t, writer, root, two))
	if err != nil {
		t.Fatal(err)
	}
	inst := reader.New(reader.Lookup(root))
	var rep tabletext.Report
	if ok, err := tablewire.DecodeVariable(reader, inst, wire, &rep); !ok || err != nil || rep.Refused {
		t.Fatalf("the engine did not read %s across the grown bound: %v %+v", root, err, rep)
	}
	text, err := reader.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes.TrimSpace(text))
}

const widenBoundProbe = `require_relative "widenbound"

POINTERS = File.exist?("pointers")

def held(label, got, cls, want)
  born = cls.new
  abort("widen_bound_#{label}: #{got.items.length} slots, not 4") unless got.items.length == 4
  abort("widen_bound_#{label}: a new slot holds an Array or nil") if got.items[2..].any? { |v| v.instance_of?(Array) } || (got.items[2..].any?(&:nil?) && !POINTERS)
  unless born.items.nil?
    (2..3).each do |i|
      next if got.items[i].class == born.items[i].class

      abort("widen_bound_#{label}: slot #{i} holds a #{got.items[i].class}, the constructor a #{born.items[i].class}")
    end
    if got.items[2].equal?(got.items[3]) && !born.items[2].equal?(born.items[3])
      abort("widen_bound_#{label}: the new slots share one object")
    end
    abort("widen_bound_#{label}: the new slots are not the constructor's") unless got.items[2..] == born.items[2..]
  end
  text = got.to_json.strip
  abort("widen_bound_#{label}: landed #{text}, want #{want}") unless text == want
end

doc_want = File.read("doc.json")
held("variable", Widenbound::Doc.load(File.binread("doc.variable")), Widenbound::Doc, doc_want)
if File.exist?("root.json")
  vocab = Widenbound::MessageVocabulary.new
  Widenbound.announce_read(vocab, File.binread("peer"))
  root_want = File.read("root.json")
  held("message", Widenbound.load_messages(Widenbound::Root, vocab, File.binread("root.message"))[0], Widenbound::Root, root_want)
  rows = Widenbound::Root.fixed_load(File.binread("root.fixed"))
  abort("widen_bound_fixed: #{rows.length} records") unless rows.length == 1
  held("fixed", rows[0], Widenbound::Root, root_want)
end
puts "ok"
`

func TestRubyWidenedFixedArrayBoundPadsOneDefaultPerSlot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	for _, k := range widenBoundKinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			older := fixedproof.ParseCheckUnit(t, widenBoundSchema(k, 2))
			newer := fixedproof.ParseCheckUnit(t, widenBoundSchema(k, 4))
			writer, reader := tabletext.NewModel(older), tabletext.NewModel(newer)

			inputs := map[string][]byte{
				"peer":     tablewire.Announce(older),
				"doc.json": []byte(widenBoundWant(t, writer, reader, "Doc", k.two)),
			}
			if strings.HasPrefix(k.elem, "*") {
				inputs["pointers"] = nil
			}
			doc := widenBoundPlace(t, writer, "Doc", k.two)
			var err error
			if inputs["doc.variable"], err = tablewire.EncodeVariable(writer, doc); err != nil {
				t.Fatal(err)
			}
			var lineage map[string][]rubytable.FixedLineageEntry
			if k.fixed {
				root := widenBoundPlace(t, writer, "Root", k.two)
				inputs["root.json"] = []byte(widenBoundWant(t, writer, reader, "Root", k.two))
				if inputs["root.message"], err = tablewire.EncodeMessages(writer, []*tabletext.Instance{root}); err != nil {
					t.Fatal(err)
				}
				if inputs["root.fixed"], err = tablewire.EncodeFixed(writer, root); err != nil {
					t.Fatal(err)
				}
				entry, ok := fixedproof.LineageEntryOf(older, "Root")
				if !ok {
					t.Fatal("the writer's fixed layout is missing")
				}
				lineage = map[string][]rubytable.FixedLineageEntry{"Root": {{
					Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record,
					Digest: ir.TableFixedDefinitionsDigest(entry.Writer), Writer: entry.Writer,
				}}}
			}
			files, err := rubytable.GenerateLineage(newer, lineage)
			if err != nil {
				t.Fatal(err)
			}
			if out, _ := rubyFixedRun(t, files, widenBoundProbe, inputs); strings.TrimSpace(out) != "ok" {
				t.Fatalf("widened bound: %s", out)
			}
		})
	}
}

// The same grown bound where the array is not a root field: inside a nested
// record, inside a union arm, and inside an optional record. Every array in
// the declaration grows from 2 to 4 at once, and the value every form lands
// is the engine's. No array in the language holds an array, so an Array
// found inside an Array is a slot padded with a whole field's default.
func TestRubyWidenedFixedArrayBoundInNestedPlaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	schema := func(n int) string {
		body := fmt.Sprintf(" inner Inner\n outer Outer\n second Outer\n maybe ?Inner\n")
		return fmt.Sprintf("package widenbound\nunion Choice {\n count int32\n note string(8)\n}\nfixed table Leaf {\n v int32 = 7\n}\nfixed table Inner {\n picks [%[1]d]Choice\n leaves [%[1]d]Leaf\n nums [%[1]d]int16\n}\nunion Outer {\n one int32\n many [%[1]d]Choice\n nums [%[1]d]int16\n}\nfixed table Root {\n%[2]s}\ntable Doc {\n%[2]s}\n", n, body)
	}
	const doc = `{"inner":{"picks":[{"count":1},{"note":"a"}],"leaves":[{"v":1},{"v":2}],"nums":[1,2]},"outer":{"many":[{"note":"b"},{"count":2}]},"second":{"nums":[3,4]},"maybe":{"picks":[{"count":3},{"count":4}],"leaves":[{"v":5},{"v":6}],"nums":[3,4]}}`
	older := fixedproof.ParseCheckUnit(t, schema(2))
	newer := fixedproof.ParseCheckUnit(t, schema(4))
	writer, reader := tabletext.NewModel(older), tabletext.NewModel(newer)
	place := func(root string) *tabletext.Instance {
		inst := writer.New(writer.Lookup(root))
		var rep tabletext.Report
		if !writer.Read(inst, []byte(doc), &rep) || rep.Refused {
			t.Fatalf("fixture %s: %+v", root, rep)
		}
		return inst
	}
	want := func(root string) []byte {
		wire, err := tablewire.EncodeVariable(writer, place(root))
		if err != nil {
			t.Fatal(err)
		}
		inst := reader.New(reader.Lookup(root))
		var rep tabletext.Report
		if ok, err := tablewire.DecodeVariable(reader, inst, wire, &rep); !ok || err != nil || rep.Refused {
			t.Fatalf("the engine did not read %s across the grown bounds: %v %+v", root, err, rep)
		}
		text, err := reader.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSpace(text)
	}
	inputs := map[string][]byte{"peer": tablewire.Announce(older), "doc.json": want("Doc"), "root.json": want("Root")}
	var err error
	if inputs["doc.variable"], err = tablewire.EncodeVariable(writer, place("Doc")); err != nil {
		t.Fatal(err)
	}
	if inputs["root.message"], err = tablewire.EncodeMessages(writer, []*tabletext.Instance{place("Root")}); err != nil {
		t.Fatal(err)
	}
	if inputs["root.fixed"], err = tablewire.EncodeFixed(writer, place("Root")); err != nil {
		t.Fatal(err)
	}
	entry, ok := fixedproof.LineageEntryOf(older, "Root")
	if !ok {
		t.Fatal("the writer's fixed layout is missing")
	}
	files, err := rubytable.GenerateLineage(newer, map[string][]rubytable.FixedLineageEntry{"Root": {{
		Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record,
		Digest: ir.TableFixedDefinitionsDigest(entry.Writer), Writer: entry.Writer,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	probe := `require_relative "widenbound"

def flat(label, value, inside)
  if value.instance_of?(Array)
    abort("widen_bound_nested_#{label}: an array holds an array") if inside
    value.each { |v| flat(label, v, true) }
  else
    abort("widen_bound_nested_#{label}: an array slot is nil") if inside && value.nil?
    value.instance_variables.each { |iv| flat(label, value.instance_variable_get(iv), false) }
  end
end

def held(label, got, want)
  flat(label, got, false)
  abort("widen_bound_nested_#{label}: inner counts") unless [got.inner.picks, got.inner.leaves, got.inner.nums, got.maybe.picks].all? { |a| a.length == 4 }
  abort("widen_bound_nested_#{label}: arm counts") unless got.outer.many.length == 4 && got.second.nums.length == 4
  abort("widen_bound_nested_#{label}: a new record slot is not born at its defaults") unless got.inner.leaves[3].v == 7 && got.maybe.leaves[2].v == 7
  text = got.to_json.strip
  abort("widen_bound_nested_#{label}: landed #{text}, want #{want}") unless text == want
end

held("variable", Widenbound::Doc.load(File.binread("doc.variable")), File.read("doc.json"))
vocab = Widenbound::MessageVocabulary.new
Widenbound.announce_read(vocab, File.binread("peer"))
held("message", Widenbound.load_messages(Widenbound::Root, vocab, File.binread("root.message"))[0], File.read("root.json"))
held("fixed", Widenbound::Root.fixed_load(File.binread("root.fixed"))[0], File.read("root.json"))
puts "ok"
`
	if out, _ := rubyFixedRun(t, files, probe, inputs); strings.TrimSpace(out) != "ok" {
		t.Fatalf("widened bounds in nested places: %s", out)
	}
}

// F-092, the reviewer's case as filed: a legal Go message of
// `fixed table Root { items [2]Choice }` selecting arms 1 and 2, read by a
// Ruby whose only change is the bound `[4]Choice`. Her controls are kept: the
// first two Choices land, and a fresh constructor holds four separate Choices.
func TestRubyMessageFixedUnionArrayWidenedBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	schema := func(n string) string {
		return "package msgunionpad\nfixed table Inner { v int32 }\nunion Choice { count int32\n nested Inner }\nfixed table Root { items [" + n + "]Choice }\n"
	}
	older := fixedproof.ParseCheckUnit(t, schema("2"))
	writer := tabletext.NewModel(older)
	root := writer.New(writer.Lookup("Root"))
	var rep tabletext.Report
	if !writer.Read(root, []byte(`{"items":[{"count":5},{"nested":{"v":3}}]}`), &rep) || rep.Refused {
		t.Fatalf("fixture: %+v", rep)
	}
	wire, err := tablewire.EncodeMessages(writer, []*tabletext.Instance{root})
	if err != nil {
		t.Fatal(err)
	}
	files, err := rubytable.Generate(fixedproof.ParseCheckUnit(t, schema("4")))
	if err != nil {
		t.Fatal(err)
	}
	probe := `require_relative "msgunionpad"
vocab = Msgunionpad::MessageVocabulary.new
Msgunionpad.announce_read(vocab, File.binread("peer"))
items = Msgunionpad.load_messages(Msgunionpad::Root, vocab, File.binread("wire"))[0].items
born = Msgunionpad::Root.new.items
abort("union_pad_control_born") unless born.length == 4 && born.all? { |c| c.instance_of?(Msgunionpad::Choice) && c.type.zero? }
abort("union_pad_count") unless items.length == 4
abort("union_pad_landed") unless items[0].type == 1 && items[0].count == 5 && items[1].type == 2 && items[1].nested.v == 3
(2..3).each do |i|
  abort("union_pad_slot_#{i}: #{items[i].class}") unless items[i].instance_of?(Msgunionpad::Choice) && items[i].type.zero?
end
abort("union_pad_distinct") if items[2].equal?(items[3])
abort("union_pad_matches_birth") unless items[2..] == born[2..]
puts "ok"
`
	out, _ := rubyFixedRun(t, files, probe, map[string][]byte{"peer": tablewire.Announce(older), "wire": wire})
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("union padding: %s", out)
	}
}
