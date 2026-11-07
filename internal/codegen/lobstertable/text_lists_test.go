package lobstertable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const lobsterTextListsSchema = `package textlists

table Total
{
    names [..8]string(8) | total = 12
    maybe [..8]?string(8) | total = 12
    repos []string(8) | total = 12
    bases []?string(8) | total = 12
}

table Wide
{
    names [..8]string(8)
    maybe [..8]?string(8)
    repos []string(8)
    bases []?string(8)
}

table Loose
{
    receipts []string
    running []?string
    recent [..4]string
    pending [..4]?string
    rows map[string]FriendRow
}

table FriendRow
{
    name string(8)
}
`

// textListsUnit writes the card's schema and generates its Lobster surfaces.
func textListsUnit(t *testing.T) *loadUnit {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "TextLists.schema")
	if err := os.WriteFile(src, []byte(lobsterTextListsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the lobster table surface refuses the text-list unit: %v", err)
	}
	return g
}

func textField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("%s has no field %s", inst.Def.Name, name)
	return nil
}

// setTextList places a text list's elements: present says which slots of the
// ? spelling ride, and every slot named present carries the string.
func setTextList(t *testing.T, m *tabletext.Model, inst *tabletext.Instance, name string, values []string, present []bool) {
	t.Helper()
	fv := textField(t, inst, name)
	f := fv.Def
	elem := f.Type.Ref.(*ir.Struct)
	fv.Count = len(values)
	slots := make([]tabletext.Cell, len(values))
	for i, s := range values {
		rec := m.New(elem)
		value := &rec.Fields[0]
		value.Cell.Str = []byte(s)
		value.Count = len(s)
		if value.Def.Type.Optional {
			value.Present = present == nil || present[i]
		}
		slots[i] = tabletext.Cell{Tab: rec}
	}
	if f.Array == ir.ArrayList {
		fv.Elems = slots
		return
	}
	if len(slots) > len(fv.Elems) {
		t.Fatalf("%s: %d elements past the bound %d", name, len(slots), len(fv.Elems))
	}
	copy(fv.Elems, slots)
}

func setMap(t *testing.T, m *tabletext.Model, inst *tabletext.Instance, name string, keys []string, names []string) {
	t.Helper()
	fv := textField(t, inst, name)
	f := fv.Def
	for i, key := range keys {
		entry := m.NewMapEntry(f)
		entry.Fields[0].Cell.Str = []byte(key)
		entry.Fields[0].Count = len(key)
		value := entry.Fields[1].Cell.Tab
		value.Fields[0].Cell.Str = []byte(names[i])
		value.Fields[0].Count = len(names[i])
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: entry})
	}
}

func totalInstance(t *testing.T, m *tabletext.Model) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup("Total"))
	setTextList(t, m, inst, "names", []string{"ab", "cd"}, nil)
	setTextList(t, m, inst, "maybe", []string{"ef", ""}, []bool{true, false})
	setTextList(t, m, inst, "repos", []string{"gh"}, nil)
	setTextList(t, m, inst, "bases", []string{"ij", ""}, []bool{true, false})
	return inst
}

func looseInstance(t *testing.T, m *tabletext.Model) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup("Loose"))
	setTextList(t, m, inst, "receipts", []string{"a much longer receipt line", "second"}, nil)
	setTextList(t, m, inst, "running", []string{"x", ""}, []bool{true, false})
	setTextList(t, m, inst, "recent", []string{"an unbounded recent value"}, nil)
	setTextList(t, m, inst, "pending", []string{"", "kept"}, []bool{false, true})
	setMap(t, m, inst, "rows", []string{"a", "a much longer friend key"}, []string{"one", "two"})
	return inst
}

// overTotalInstance is a Wide value (no total) whose names sum to 16, past
// Total.names' 12: the Total reader keeps its longest prefix within 12.
func overTotalInstance(t *testing.T, m *tabletext.Model) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup("Wide"))
	setTextList(t, m, inst, "names", []string{"abcdefgh", "abcdefgh"}, nil)
	return inst
}

func encodeInstance(t *testing.T, m *tabletext.Model, inst *tabletext.Instance) []byte {
	t.Helper()
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// TestLobsterTextLists holds the four bounded spellings, their total and the
// unbounded spellings to the Go reference through runtime/load.lobster and
// runtime/schema_save.lobster (docs/SPEC-TABLES.md §2.13, §2.13.1;
// docs/PORT-LOBSTER.md, THE LOAD): load, save and re-load must agree byte for
// byte and report for report, each construct named with its count.
func TestLobsterTextLists(t *testing.T) {
	t.Parallel()
	g := textListsUnit(t)

	lists := ir.TableTextLists(g.u)
	totals := ir.TableTextTotals(g.u)
	unbounded := ir.TableUnboundedTexts(g.u)
	t.Logf("text-list: %d fields %v", len(lists), lists)
	t.Logf("text-list-total: %d fields %v", len(totals), totals)
	t.Logf("unbounded-text: %d fields %v", len(unbounded), unbounded)
	if len(lists) != 12 || len(totals) != 4 || len(unbounded) != 5 {
		t.Fatalf("construct counts: text-list %d want 12, total %d want 4, unbounded-text %d want 5", len(lists), len(totals), len(unbounded))
	}

	m := tabletext.NewModel(g.u)
	totalValue := totalInstance(t, m)
	looseValue := looseInstance(t, m)
	within := []loadRow{
		{kind: "instance", name: "total_within", unit: "textlists", root: "Total", data: encodeInstance(t, m, totalValue)},
		{kind: "instance", name: "unbounded_loose", unit: "textlists", root: "Loose", data: encodeInstance(t, m, looseValue)},
	}
	loaded := runUnit(t, g, within)
	// Total is bounded: its report and value are the tablewire reference's.
	if loaded.lob[0] != loaded.goLine[0] {
		t.Errorf("total_within: the report is not the reference's:\nlobster: %s\ngo:      %s", loaded.lob[0], loaded.goLine[0])
	}
	if loaded.lob[1] != loaded.goDump[0] {
		t.Errorf("total_within: the loaded value is not the reference's at byte %s", divergence(loaded.lob[1], loaded.goDump[0]))
	}
	// Unbounded text has no length bound, so tablewire.Decode's string(N)
	// clamp is not its answer; the Go MODEL of the same instance is. The read
	// reports nothing, as §2.13.1 states.
	if got, want := loaded.lob[2], "0,0,0,0,0,false,read"; got != want {
		t.Errorf("unbounded_loose: report %s, want %s", got, want)
	}
	looseDump := &goDumper{labels: map[*tabletext.Instance]int{looseValue: 0}, next: 1}
	if got, want := loaded.lob[3], looseDump.rec(looseValue); got != want {
		t.Errorf("unbounded_loose: the loaded value is not the Go model's at byte %s", divergence(got, want))
	}

	saved := runSaveRows(t, g, within)
	totalWant, err := goSave(g, "Total", within[0].data)
	if err != nil {
		t.Fatalf("total_within: the reference writer refuses: %v", err)
	}
	if saved.saved[0] != hex.EncodeToString(totalWant) {
		t.Errorf("total_within: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", saved.saved[0], hex.EncodeToString(totalWant))
	}
	// The unbounded writer's bytes are the reference WRITER's on the same
	// value (tablewire.Encode, which writes the bytes the storage holds).
	looseWant := encodeInstance(t, m, looseValue)
	if saved.saved[1] != hex.EncodeToString(looseWant) {
		t.Errorf("unbounded_loose: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", saved.saved[1], hex.EncodeToString(looseWant))
	}
	if saved.dump[0] != loaded.goDump[0] {
		t.Errorf("total_within: the re-loaded value is not the reference's at byte %s", divergence(saved.dump[0], loaded.goDump[0]))
	}
	if saved.dump[1] != looseDump.rec(looseValue) {
		t.Errorf("unbounded_loose: the re-loaded value is not the Go model's at byte %s", divergence(saved.dump[1], looseDump.rec(looseValue)))
	}

	// THE TOTAL on the read side: a wire whose names sum to 16 is read by
	// Total with the first element kept and the second dropped, clamped once
	// and the field named (docs/SPEC-TABLES.md §2.13).
	over := loadRow{kind: "instance", name: "total_prefix", unit: "textlists", root: "Total", data: encodeInstance(t, m, overTotalInstance(t, m))}
	res := runUnit(t, g, []loadRow{over})
	if got, want := res.lob[0], "0,0,0,1,0,false,read"; got != want {
		t.Errorf("total_prefix: report %s, want %s", got, want)
	}
	keep := m.New(m.Lookup("Total"))
	setTextList(t, m, keep, "names", []string{"abcdefgh"}, nil)
	d := &goDumper{labels: map[*tabletext.Instance]int{keep: 0}, next: 1}
	if got, want := res.lob[1], d.rec(keep); got != want {
		t.Errorf("total_prefix: value is not the prefix Go holds at byte %s", divergence(got, want))
	}
	if !strings.Contains(res.lob[1], "names=#1") {
		t.Errorf("total_prefix: the kept prefix is not one element: %s", res.lob[1])
	}

	// THE UNBOUNDED element and key past every string(N) bound: the same
	// value the reference holds (its text is 27 bytes, longer than any
	// string(8) field here).
	if !strings.Contains(loaded.lob[3], hex.EncodeToString([]byte("a much longer receipt line"))) {
		t.Fatalf("the unbounded fixture does not carry its long element: %s", loaded.lob[3])
	}
}

// TestLobsterTextListsNegativeControl breaks the unbounded element's read —
// the line that spares an `s: "text"` value the string(N) clamp — and holds
// the comparison red, with the unpatched control green.
func TestLobsterTextListsNegativeControl(t *testing.T) {
	t.Parallel()
	g := textListsUnit(t)
	m := tabletext.NewModel(g.u)
	row := loadRow{kind: "instance", name: "unbounded_loose", unit: "textlists", root: "Loose", data: encodeInstance(t, m, looseInstance(t, m))}
	want := func() string {
		v := looseInstance(t, tabletext.NewModel(g.u))
		d := &goDumper{labels: map[*tabletext.Instance]int{v: 0}, next: 1}
		return d.rec(v)
	}()
	control := runUnit(t, g, []loadRow{row})
	if control.lob[1] != want {
		t.Fatalf("the control, unpatched: the value is not the Go model's at byte %s", divergence(control.lob[1], want))
	}
	path := filepath.Join(g.dir, "text_lists.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "        if vd.s != \"text\" and keep > vd.bound:\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the unbounded-element anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "        if keep > vd.bound:\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runUnit(t, g, []loadRow{row})
	if got.lob[1] == want {
		t.Fatalf("the patched runtime still matches the Go model: %s", got.lob[1])
	}
	t.Logf("the value comparison went red under the patch: text-list unbounded element was clamped to the string bound")
}
