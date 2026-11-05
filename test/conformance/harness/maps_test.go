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
	for _, tc := range []struct{ name, root string }{
		{"map_full", "Fleet"}, {"map_empty", "Fleet"}, {"map_depth", "Depth"}, {"map_text", "Text"},
		// one row per value kind that carries an extent or names a buffer node
		// (#628): a fixed array, a counted array, an enum-keyed array, an
		// unbounded array, a text buffer and a byte buffer, each under a map key
		{"map_cells", "Cells"}, {"map_runs", "Runs"}, {"map_slots", "Slots"},
		{"map_spans", "Spans"}, {"map_docs", "Docs"}, {"map_chunks", "Chunks"},
		// one row per value kind whose ELEMENT is a pointer (#666): a fixed
		// array, a counted array and an unbounded array of node references,
		// each carrying a shared node and a slot that names none
		{"map_pairs", "Pairs"}, {"map_crews", "Crews"}, {"map_trails", "Trails"},
	} {
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
// (docs/SPEC-TABLES.md §2.8). Two keys that share a long prefix are two keys:
// a read that truncated them into one buffer would merge them into ONE entry
// keyed by bytes the text never spelled. `EdgeRow.names` is string(300), so
// the declared bound holds both keys.
//
// What the page states is the IDENTITY, so that is what this pins: the read
// returns the whole value or refuses (§6.5), and every entry a read holds is
// a key the text spelled.
func TestAMapKeyIsNeverTruncatedIntoAnother(t *testing.T) {
	m := mapsModel(t)
	first := strings.Repeat("a", 255) + "b"
	second := strings.Repeat("a", 255) + "c"
	text := `{"names":{"` + first + `":{"count":1},"` + second + `":{"count":2}},"past":5}`
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		if !r.Refused {
			t.Fatalf("a read that returns false refuses: %+v", r)
		}
		return
	}
	fv, ok := inst.FieldByKey("names")
	if !ok {
		t.Fatal("no names field")
	}
	if len(fv.Entries) != 2 {
		t.Errorf("two distinct keys are two entries, got %d", len(fv.Entries))
	}
	for _, cell := range fv.Entries {
		key := string(cell.Tab.Fields[0].Cell.Str)
		if key != first && key != second {
			t.Errorf("the map holds a key the text never spelled, %d bytes long", len(key))
		}
	}
	after, ok := inst.FieldByKey("past")
	if !ok || after.Cell.I != 5 {
		t.Error("the parent did not read on past the map")
	}
}

// AN INTEGER MAP KEY IS READ BY §16.2's INTEGER RULE AND BY NOTHING ELSE
// (docs/SPEC-TABLES.md §2.8), so "2.0" and "1e3" are the integers 2 and 1000 in
// a key exactly as in a field, at every integer key kind. A value outside the
// key kind's range refuses the whole read (§6.5), and a key is never clamped.
func TestAnIntegerMapKeyTakesTheIntegerRule(t *testing.T) {
	m := mapsModel(t)
	for _, tc := range []struct {
		root, field, entries string
		keys                 []uint64
		outside              []string
	}{
		{"WideRow", "entries", `"2.0":{"count":1},"1e3":{"count":2}`,
			[]uint64{2, 1000}, []string{"99999999999"}},
		{"EdgeRow", "ids", `"2.0":{"count":1},"1e19":{"count":2},"18446744073709551615":{"count":3}`,
			[]uint64{2, 10000000000000000000, 18446744073709551615}, []string{"-1", "1e30"}},
	} {
		t.Run(tc.root, func(t *testing.T) {
			inst := m.New(m.Lookup(tc.root))
			var r tabletext.Report
			if !m.Read(inst, []byte(`{"`+tc.field+`":{`+tc.entries+`}}`), &r) {
				t.Fatalf("every key is in the kind's range: %+v", r)
			}
			fv, ok := inst.FieldByKey(tc.field)
			if !ok {
				t.Fatalf("no %s field", tc.field)
			}
			if len(fv.Entries) != len(tc.keys) {
				t.Fatalf("expected %d entries, got %d", len(tc.keys), len(fv.Entries))
			}
			for i, want := range tc.keys {
				if got := fv.Entries[i].Tab.Fields[0].Cell.U; got != want {
					t.Errorf("entry %d: key %d, want %d", i, got, want)
				}
			}
			for _, key := range tc.outside {
				inst := m.New(m.Lookup(tc.root))
				var r tabletext.Report
				text := `{"` + tc.field + `":{` + tc.entries + `,"` + key + `":{"count":9}}}`
				if m.Read(inst, []byte(text), &r) || !r.Refused {
					t.Errorf("the key %s is outside the kind's range and the read returns: %+v", key, r)
				}
			}
		})
	}
}

// A KEY IS THE INTEGER'S SPELLING AND NOTHING AROUND IT (docs/SPEC-TABLES.md
// §16.2). A key is read by the integer rule and by nothing else, and that rule
// reads RFC 8259's number grammar, which has no room in it for whitespace: a
// padded spelling is not a JSON number at all, so the read refuses on the same
// terms as `1-2`, and the instance holds nothing it placed (§6.5).
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
		t.Errorf("a padded key is not a JSON number: the read must refuse, got %+v", r)
	}
	if !r.Refused {
		t.Errorf("a padded key refuses: %+v", r)
	}
	fv, ok := inst.FieldByKey("ids")
	if !ok {
		t.Fatal("no ids field")
	}
	if len(fv.Entries) != 0 {
		t.Errorf("a refused read holds no entry, got %d", len(fv.Entries))
	}
}

// AN INTEGER KEY SPELLED WITH A DECIMAL POINT IS PARSED AS AN INTEGER, never
// through a double (docs/SPEC-TABLES.md §16.2). A double carries 53 bits of
// mantissa, so it cannot tell 9007199254740993 from its neighbor, and a key
// read through one lands on the neighbor's identity: two keys the text spells
// separately become one, and the read refuses a repeat the text never wrote.
func TestADecimalMapKeyDoesNotRoundIntoItsNeighbour(t *testing.T) {
	m := mapsModel(t)
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	text := `{"ids":{"9007199254740992":{"count":1},"9007199254740993.0":{"count":2}}}`
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("two keys 2^53 apart are two keys: %+v", r)
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
// 18446744073709551615.0 rounds UP to 2^64 and falls outside the kind, where
// the digits alone are exactly the key the kind holds. The two spellings are
// one key written twice, and a repeated key refuses (§6.5).
func TestADecimalMapKeyAtTheTopOfTheDomainIsTheSameKey(t *testing.T) {
	m := mapsModel(t)
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"ids":{"18446744073709551615.0":{"count":2}}}`), &r) {
		t.Fatalf("the decimal spelling of UINT64_MAX is a key the kind holds: %+v", r)
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
	inst = m.New(m.Lookup("EdgeRow"))
	r = tabletext.Report{}
	text := `{"ids":{"18446744073709551615":{"count":1},"18446744073709551615.0":{"count":2}}}`
	if m.Read(inst, []byte(text), &r) || !r.Refused {
		t.Errorf("the two spellings are one key repeated, which refuses: %+v", r)
	}
}

// AN INTEGER KEY PAST THE SCAN'S BUFFER is never read as its truncated prefix
// (#609). The bytes a scan keeps are a PREFIX, and a prefix is a different
// token. The key below is 256 bytes and spells the integer 1, a value the
// kind holds, beside the key 1 itself: the text holds one key twice or a key
// this reader cannot scan, and either refuses (§6.5).
func TestAnIntegerMapKeyPastTheBufferDrops(t *testing.T) {
	m := mapsModel(t)
	inst := m.New(m.Lookup("EdgeRow"))
	var r tabletext.Report
	long := "1e" + strings.Repeat("0", 254)
	text := `{"ids":{"1":{"count":7},"` + long + `":{"count":9}}}`
	if m.Read(inst, []byte(text), &r) || !r.Refused {
		t.Errorf("an over-long key refuses the read: %+v", r)
	}
	fv, ok := inst.FieldByKey("ids")
	if !ok {
		t.Fatal("no ids field")
	}
	if len(fv.Entries) != 0 {
		t.Errorf("a refused read holds no entry, got %d", len(fv.Entries))
	}
}
