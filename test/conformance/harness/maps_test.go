package main

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE TOOL'S MAP HALF AGAINST THE REFERENCE (docs/SPEC-TABLES.md §2.8): the
// compiler's own engine reads the C++ reference's pinned map wires, writes them
// back byte for byte, renders the text and reads that text back to the same
// bytes. A map rides as kind 14 over element kind 13 in ASCENDING KEY ORDER
// with no key twice, and this is what says both implementations agree on the
// order as well as the framing — at three depths, a map of maps included.
func TestTheToolWritesTheReferencesMapBytes(t *testing.T) {
	const root = "../../../"
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{root + "tables/maps"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	for _, tc := range []struct{ name, root string }{{"map_full", "Fleet"}, {"map_empty", "Fleet"}, {"map_depth", "Depth"}} {
		name, rootName := tc.name, tc.root
		data, err := os.ReadFile(root + "testdata/wire/tables/" + name + ".bin")
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(rootName))
		var rep tabletext.Report
		ok, derr := tablewire.Decode(m, inst, data, &rep)
		if derr != nil || !ok {
			t.Errorf("%s: %v ok=%v", name, derr, ok)
			continue
		}
		if !rep.Silent() {
			t.Errorf("%s: the reference's own bytes did not read clean: %+v", name, rep)
		}
		again, eerr := tablewire.Encode(m, inst)
		if eerr != nil {
			t.Errorf("%s: %v", name, eerr)
			continue
		}
		if hex.EncodeToString(again) != hex.EncodeToString(data) {
			t.Errorf("%s: the engine re-encoded %d bytes, the reference wrote %d\n  got  %s\n  want %s",
				name, len(again), len(data), hex.EncodeToString(again), hex.EncodeToString(data))
			continue
		}
		text, terr := m.Write(inst)
		if terr != nil {
			t.Errorf("%s: text: %v", name, terr)
			continue
		}
		back := m.New(m.Lookup(rootName))
		var r2 tabletext.Report
		if !m.Read(back, text, &r2) || !r2.Silent() {
			t.Errorf("%s: the text did not read back clean: %+v\n%s", name, r2, text)
			continue
		}
		round, _ := tablewire.Encode(m, back)
		if hex.EncodeToString(round) != hex.EncodeToString(data) {
			t.Errorf("%s: text round trip moved bytes", name)
		}
	}
}

// mapsModel is the `tables/maps` corpus as the tool's model, for the rows below
// that read a text and look at what landed.
func mapsModel(t *testing.T) *tabletext.Model {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{"../../../tables/maps"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// A KEY THIS READER CANNOT HOLD WHOLE MUST NOT BECOME A DIFFERENT KEY
// (docs/SPEC-TABLES.md §2.8). The walker scans a key into a fixed buffer, so a
// key longer than it is truncated there, and two keys that share the prefix it
// keeps merge into ONE entry keyed by bytes the text never spelled, while
// `clamped`, which the scan raises either way, says nothing about it.
// `EdgeRow.names` is string(300), so the DECLARED bound holds both keys and
// only the reader's own does not.
//
// What the page states is the IDENTITY, so that is what this pins: every entry
// the map holds is a key the text spelled. Whether a key past the reader's
// buffer but inside the declared bound should be DROPPED or held WHOLE is the
// page's to say, and this row is green under either answer.
func TestAMapKeyIsNeverTruncatedIntoAnother(t *testing.T) {
	m := mapsModel(t)
	first := strings.Repeat("a", 255) + "b"
	second := strings.Repeat("a", 255) + "c"
	text := `{"names":{"` + first + `":{"count":1},"` + second + `":{"count":2}},"after":5}`
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) || r.Malformed {
		t.Fatalf("the text is well formed: %+v", r)
	}
	if r.Duplicate != 0 {
		t.Errorf("two distinct keys are not a duplicate: %+v", r)
	}
	fv, ok := inst.FieldByKey("names")
	if !ok {
		t.Fatal("no names field")
	}
	for _, cell := range fv.Entries {
		key := string(cell.Tab.Fields[0].Cell.Str)
		if key != first && key != second {
			t.Errorf("the map holds a key the text never spelled, %d bytes long", len(key))
		}
	}
	after, ok := inst.FieldByKey("after")
	if !ok || after.Cell.I != 5 {
		t.Error("the parent did not read on past the map")
	}
}

// AN INTEGER MAP KEY IS READ BY §16.2's INTEGER RULE AND BY NOTHING ELSE
// (docs/SPEC-TABLES.md §2.8), so "2.0" and "1e3" are the integers 2 and 1000 in
// a key exactly as in a field, at every integer key kind. The KEY's own policy
// over that value is REJECTION: a value outside the key kind's range drops the
// whole entry as kind_mismatch, and a key is never clamped.
func TestAnIntegerMapKeyTakesTheIntegerRule(t *testing.T) {
	m := mapsModel(t)
	for _, tc := range []struct {
		root, field, text string
		count             int
		mismatch          int
		keys              []uint64
	}{
		{"WideRow", "entries", `{"entries":{"2.0":{"count":1},"1e3":{"count":2},"99999999999":{"count":3}}}`,
			2, 1, []uint64{2, 1000}},
		{"EdgeRow", "ids", `{"ids":{"2.0":{"count":1},"1e19":{"count":2},"18446744073709551615":{"count":3},` +
			`"-1":{"count":4},"1e30":{"count":5}}}`,
			3, 2, []uint64{2, 10000000000000000000, 18446744073709551615}},
	} {
		t.Run(tc.root, func(t *testing.T) {
			inst := m.New(m.Lookup(tc.root))
			var r tabletext.Report
			if !m.Read(inst, []byte(tc.text), &r) || r.Malformed {
				t.Fatalf("the text is well formed: %+v", r)
			}
			if r.KindMismatch != tc.mismatch || r.Clamped != 0 {
				t.Fatalf("expected %d kind_mismatch and no clamp, got %+v", tc.mismatch, r)
			}
			fv, ok := inst.FieldByKey(tc.field)
			if !ok {
				t.Fatalf("no %s field", tc.field)
			}
			if len(fv.Entries) != tc.count {
				t.Fatalf("expected %d entries, got %d", tc.count, len(fv.Entries))
			}
			for i, want := range tc.keys {
				if got := fv.Entries[i].Tab.Fields[0].Cell.U; got != want {
					t.Errorf("entry %d: key %d, want %d", i, got, want)
				}
			}
		})
	}
}

// A KEY IS THE INTEGER'S SPELLING AND NOTHING AROUND IT (docs/SPEC-TABLES.md
// §16.2). A key is read by the integer rule and by nothing else, and that rule
// reads RFC 8259's number grammar, which has no room in it for whitespace: a
// padded spelling is not a JSON number at all, so it is `malformed` on the same
// terms as `1-2`, and the read stops there with the instance holding what was
// placed before the stop (§16.1).
//
// A reader that steps over the padding on its way in hands the digit path a
// byte that is not a digit, and what comes back is a key of its own making: the
// text below spells `2402` once and a padded `2` beside it, and the two must
// never be one entry.
func TestWhitespaceIsNeverPartOfAMapKey(t *testing.T) {
	m := mapsModel(t)
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	text := `{"ids":{"2402":{"count":7}," 2":{"count":1}}}`
	if m.Read(inst, []byte(text), &r) {
		t.Errorf("a padded key is not a JSON number: the read must stop, got %+v", r)
	}
	if !r.Malformed {
		t.Errorf("a padded key is malformed: %+v", r)
	}
	if r.Duplicate != 0 {
		t.Errorf("a padded 2 is not the key 2402, so nothing is a duplicate: %+v", r)
	}
	fv, ok := inst.FieldByKey("ids")
	if !ok {
		t.Fatal("no ids field")
	}
	for _, cell := range fv.Entries {
		if got := cell.Tab.Fields[0].Cell.U; got != 2402 {
			t.Errorf("the map holds the key %d, which the text never spelled", got)
		}
	}
}

// AN INTEGER KEY SPELLED WITH A DECIMAL POINT IS PARSED AS AN INTEGER, never
// through a double (docs/SPEC-TABLES.md §16.2). A double carries 53 bits of
// mantissa, so it cannot tell 9007199254740993 from its neighbour, and a key
// read through one lands on the neighbour's identity: two keys the text spells
// separately become one entry.
func TestADecimalMapKeyDoesNotRoundIntoItsNeighbour(t *testing.T) {
	m := mapsModel(t)
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	text := `{"ids":{"9007199254740992":{"count":1},"9007199254740993.0":{"count":2}}}`
	if !m.Read(inst, []byte(text), &r) || r.Malformed {
		t.Fatalf("the text is well formed: %+v", r)
	}
	if r.Duplicate != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
		t.Errorf("two keys 2^53 apart are two keys: %+v", r)
	}
	fv, ok := inst.FieldByKey("ids")
	if !ok {
		t.Fatal("no ids field")
	}
	want := []uint64{9007199254740992, 9007199254740993}
	if len(fv.Entries) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(fv.Entries))
	}
	for i, w := range want {
		if got := fv.Entries[i].Tab.Fields[0].Cell.U; got != w {
			t.Errorf("entry %d: key %d, want %d", i, got, w)
		}
	}
}

// THE SAME VALUE SPELLED TWO WAYS IS THE SAME KEY (docs/SPEC-TABLES.md §16.2):
// `2` and `2.0` are the integer 2 wherever an integer is read, and that holds at
// the top of the uint64 domain as well as at the bottom. Read through a double,
// 18446744073709551615.0 rounds UP to 2^64 and is dropped as outside the kind,
// where the digits alone are exactly the key the kind holds.
func TestADecimalMapKeyAtTheTopOfTheDomainIsTheSameKey(t *testing.T) {
	m := mapsModel(t)
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	text := `{"ids":{"18446744073709551615":{"count":1},"18446744073709551615.0":{"count":2}}}`
	if !m.Read(inst, []byte(text), &r) || r.Malformed {
		t.Fatalf("the text is well formed: %+v", r)
	}
	if r.KindMismatch != 0 || r.Clamped != 0 {
		t.Errorf("the decimal spelling of UINT64_MAX is a key the kind holds: %+v", r)
	}
	if r.Duplicate != 1 {
		t.Errorf("the two spellings are one key, so the second is a duplicate: %+v", r)
	}
	fv, ok := inst.FieldByKey("ids")
	if !ok {
		t.Fatal("no ids field")
	}
	if len(fv.Entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(fv.Entries))
	}
	if got := fv.Entries[0].Tab.Fields[0].Cell.U; got != 18446744073709551615 {
		t.Errorf("key %d, want 18446744073709551615", got)
	}
	item := fv.Entries[0].Tab.Fields[1].Cell.Tab
	if item == nil {
		t.Fatal("the entry carries no value")
	}
	count, ok := item.FieldByKey("count")
	if !ok {
		t.Fatal("no count field")
	}
	if count.Cell.I != 2 {
		t.Errorf("last wins: count %d, want 2", count.Cell.I)
	}
}
