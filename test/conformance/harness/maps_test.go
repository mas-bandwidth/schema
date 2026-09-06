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
