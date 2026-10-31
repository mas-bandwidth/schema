package tabletext_test

import (
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUncoveredPaths drives the text engine over the corpora the package's
// other tests leave partly unread (docs/SPEC-TABLES.md §2.8, §16.2) and pins
// the rules whose functions no existing case reached: map key order and its
// integer spelling, the wide kinds' clamp and raw value, both packer entry
// points, and a union arm that names no declaration.
func TestUncoveredPaths(t *testing.T) {
	t.Parallel()

	t.Run("existing suite", func(t *testing.T) {
		for _, row := range []struct {
			name string
			fn   func(*testing.T)
		}{
			{"unknown key", TestUnknownKeyIsCounted},
			{"repeated key", TestRepeatedKeyIsDamage},
			{"wrong type", TestWrongTypeIsSkippedNotCoerced},
			{"fraction", TestFractionWhereIntegerDeclared},
			{"out of range", TestOutOfRangeIsClamped},
			{"string clamp", TestStringClampsAtCodePointBoundary},
			{"clamp prefix", TestClampIsAPrefix},
			{"escapes", TestEscapesAndSurrogates},
			{"enum", TestEnumByNameAndUnknownVariant},
			{"optional", TestOptionalPresenceIsTheKey},
			{"enum keyed", TestEnumKeyedObject},
			{"json key", TestJsonKeyIsHonoured},
			{"commas", TestTrailingCommasAndComments},
			{"union", TestUnionShape},
			{"flags", TestFlagsByName},
			{"base64", TestBytesBase64},
			{"guards", TestGuardsInferNothing},
			{"array bounds", TestArrayBounds},
			{"round trip", TestWriteReadWriteIsStable},
			{"numbers", TestNumberGrammarAndSemantics},
			{"null", TestNullPerKind},
			{"utf8 writer", TestWriterEmitsValidUTF8},
			{"bits", TestBitsImpliedBound},
			{"ill formed", TestIllFormedByteIsReplacedOnRead},
			{"integer token", TestAnIntegerFieldReadsItsTokenThroughTheFloat},
			{"optional present", TestOptionalPresentWhateverTheValue},
			{"float32", TestFloat32SingleRounding},
			{"shared node", TestSharedNodeIsOneNode},
			{"sharing refusal", TestSharingConstructRefusals},
			{"reference", TestReferenceResolution},
			{"chain", TestChainNestsAsDeepAsItIsLong},
			{"empty shared", TestSharedNodeWithNothingToWriteIsRefused},
			{"unsigned ceiling", TestUnsignedHighHalfClampsAtTheCeiling},
			{"negative exponent", TestNegativeExponentInAnUnsignedFieldClampsToZero},
			{"timestamp", TestEngineTimestampIsRFC3339Text},
			{"string utf8", TestEngineStringRefusesInvalidUTF8},
			{"bom entry", TestJSONByteOrderMarkAtTheEntry},
			{"bom packer", TestJSONByteOrderMarkAtThePackerEntries},
			{"zero text", TestPackTextReaderRefusesZeroInText},
			{"zero note", TestPackTextReaderRefusesZeroInBuildNote},
		} {
			t.Run(row.name, row.fn)
		}
	})

	t.Run("map keys", func(t *testing.T) {
		m, _ := load(t, "../../tables/maps")
		st := m.Lookup("Text")
		if st == nil {
			t.Fatal("no Text table")
		}
		inst := m.New(st)
		var r tabletext.Report
		text := `{
			"names": { "zz": "last", "a": "first", "ab": "middle" },
			"wide": { "10": "ten", "2": "two" },
			"blobs": { "9007199254740993": "AA==", "1e3": "AQ==", "2.0": "Ag==",
			           "-0": "Aw==", "2.5": "BA==", "999999999999999999999999999999": "BQ==" },
			"past": 7
		}`
		if !m.Read(inst, []byte(text), &r) {
			t.Fatalf("map text refused: %+v", r)
		}
		names := field(t, inst, "names")
		if len(names.Entries) != 3 {
			t.Fatalf("names holds %d entries, want 3", len(names.Entries))
		}
		if got := string(names.Entries[0].Tab.Fields[0].Cell.Str); got != "a" {
			t.Fatalf("first string key is %q, want a", got)
		}
		if got := string(names.Entries[2].Tab.Fields[0].Cell.Str); got != "zz" {
			t.Fatalf("last string key is %q, want zz", got)
		}
		blobs := field(t, inst, "blobs")
		if len(blobs.Entries) != 3 {
			t.Fatalf("blobs holds %d entries, want 3 (fractional and saturated keys drop)", len(blobs.Entries))
		}
		want := []int64{0, 2, 1000} // -0, 2.0 and 1e3, sorted
		for i, w := range want {
			if got := blobs.Entries[i].Tab.Fields[0].Cell.I; got != w {
				t.Fatalf("blob key %d is %d, want %d", i, got, w)
			}
		}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("write: %v", err)
		}

		edge := m.New(m.Lookup("EdgeRow"))
		var er tabletext.Report
		edgeText := `{ "ids": {
			"9007199254740993": { "count": 1 },
			"18446744073709551615": { "count": 2 },
			"1e3": { "count": 3 },
			"-1": { "count": 4 },
			"2.5": { "count": 5 },
			"18446744073709551616": { "count": 6 }
		} }`
		if !m.Read(edge, []byte(edgeText), &er) || er.Malformed {
			t.Fatalf("uint64 keys refused: %+v", er)
		}
		ids := field(t, edge, "ids")
		wantKeys := []uint64{1000, 9007199254740993, 18446744073709551615}
		if len(ids.Entries) != len(wantKeys) {
			t.Fatalf("uint64 keys hold %d entries, want %d", len(ids.Entries), len(wantKeys))
		}
		for i, want := range wantKeys {
			if got := ids.Entries[i].Tab.Fields[0].Cell.U; got != want {
				t.Fatalf("uint64 key %d is %d, want %d", i, got, want)
			}
		}
		edgeOut, err := m.Write(edge)
		if err != nil {
			t.Fatalf("uint64 key write: %v", err)
		}
		if !bytes.Contains(edgeOut, []byte(`"9007199254740993"`)) {
			t.Fatalf("the exact key did not ride the text form:\n%s", edgeOut)
		}
		back := m.New(st)
		var rr tabletext.Report
		if !m.Read(back, out, &rr) || !rr.Silent() {
			t.Fatalf("map round trip: %+v\n%s", rr, out)
		}
	})

	t.Run("nested map", func(t *testing.T) {
		m, _ := load(t, "../../tables/maps")
		st := m.Lookup("Fleet")
		if st == nil {
			t.Fatal("no Fleet table")
		}
		inst := m.New(st)
		var r tabletext.Report
		text := `{
			"ships": { "a": { "name": "one", "health": 1 } },
			"by_id": { "3": { "name": "two", "health": 2 } },
			"flagship": { "name": "flag", "health": 3 },
			"loadouts": { "x": { "1": { "count": 4 } } },
			"tiers": { "-3": { "count": 5 }, "2": { "count": 6 } }
		}`
		if !m.Read(inst, []byte(text), &r) || r.Malformed {
			t.Fatalf("nested map text refused: %+v", r)
		}
		tiers := field(t, inst, "tiers")
		if len(tiers.Entries) != 2 || tiers.Entries[0].Tab.Fields[0].Cell.I != -3 {
			t.Fatalf("signed keys did not sort numerically: %+v", tiers.Entries)
		}
	})

	t.Run("wide kinds", func(t *testing.T) {
		m, u := load(t, "../../tables/scalars")
		st := m.Lookup("SimState")
		if st == nil {
			t.Fatal("no SimState table")
		}
		inst := m.New(st)
		var r tabletext.Report
		text := `{
			"tilt": 1.5, "angle": -12.25, "position": 3.5, "reach": -100.25,
			"ticks": 42, "ratio": 1.5, "speed": 2.5, "span": 3.5, "mass": 4.5,
			"frames": 5, "flux": 9007199254740993, "energy": -250,
			"entity_id": 340282366920938463463374607431768211455, "scale": 1.0,
			"samples": [1.5, 2.5, 3.5], "weights": [1.5, 2.5],
			"axes": { "X": 1.0, "Y": 2.0, "Z": 3.0 }, "seeds": [1, 2],
			"pose": { "x": 1.5, "y": 2.5, "heading": 3.5 },
			"spawn": { "x": 1.0, "y": 1.0, "heading": 1.0 }
		}`
		if !m.Read(inst, []byte(text), &r) || r.Malformed {
			t.Fatalf("wide text refused: %+v", r)
		}
		if got := tabletext.WideValue(&field(t, inst, "flux").Cell); got.String() != "9007199254740993" {
			t.Fatalf("int128 key lost precision: %s", got)
		}
		if zero := tabletext.WideValue(&tabletext.Cell{}); zero.Sign() != 0 {
			t.Fatalf("an unset wide cell is %s, want zero", zero)
		}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("wide write: %v", err)
		}
		if !bytes.Contains(out, []byte("9007199254740993")) {
			t.Fatalf("the wide value did not write exactly:\n%s", out)
		}

		// the declared range clamps, and a finer fraction is a kind mismatch.
		clampInst := m.New(st)
		var cr tabletext.Report
		if !m.Read(clampInst, []byte(`{ "angle": 1e9, "position": 1e9 }`), &cr) {
			t.Fatalf("clamping text refused: %+v", cr)
		}
		if cr.Clamped == 0 {
			t.Fatalf("expected the wide range to clamp, got %+v", cr)
		}
		fine := m.New(st)
		var fr tabletext.Report
		if !m.Read(fine, []byte(`{ "angle": 1e-9 }`), &fr) || fr.KindMismatch == 0 {
			t.Fatalf("a finer fraction must be a kind mismatch: %+v", fr)
		}

		angle := field(t, inst, "angle").Def
		if got, moved := tabletext.WideClamp(big.NewInt(-1<<40), angle); !moved || got.Int64() != -180*(1<<16) {
			t.Fatalf("WideClamp low: got %s moved=%v", got, moved)
		}
		if got, moved := tabletext.WideClamp(big.NewInt(1<<40), angle); !moved || got.Int64() != 180*(1<<16) {
			t.Fatalf("WideClamp high: got %s moved=%v", got, moved)
		}
		if got, moved := tabletext.WideClamp(big.NewInt(0), angle); moved || got.Sign() != 0 {
			t.Fatalf("WideClamp in range moved: %s %v", got, moved)
		}
		if got, moved := tabletext.WideClamp(big.NewInt(-7), u.Tables["SimState"].Fields[0]); moved || got.Int64() != -7 {
			t.Fatalf("WideClamp without a range moved: %s %v", got, moved)
		}

		for _, row := range []struct {
			raw  int64
			kind int
		}{
			{-32768, ir.TableKindFixed32},
			{65536, ir.TableKindUFixed32},
			{-1, ir.TableKindI128},
			{1 << 40, ir.TableKindU128},
			{0, ir.TableKindFixed64},
		} {
			raw := big.NewInt(row.raw)
			bytes := tabletext.WideBytes(raw, row.kind)
			if len(bytes) != ir.TableKindWidth(row.kind) {
				t.Fatalf("WideBytes(%d, %d) is %d bytes", row.raw, row.kind, len(bytes))
			}
			if back := tabletext.WideFromBytes(bytes, row.kind); back.Cmp(raw) != 0 {
				t.Fatalf("WideBytes/WideFromBytes(%d, %d) = %s", row.raw, row.kind, back)
			}
		}
	})

	t.Run("packer entries", func(t *testing.T) {
		m, _ := load(t, "../../tables/scalars")
		inst := m.New(m.Lookup("SimState"))
		samples := field(t, inst, "samples")
		text, err := m.WriteElement(samples, 1)
		if err != nil {
			t.Fatalf("WriteElement: %v", err)
		}
		if string(bytes.TrimSpace(text)) != "0.0" {
			t.Fatalf("element text is %q, want 0.0", text)
		}
		samples.Elems[1] = m.ElementZero(samples.Def)
		var r tabletext.Report
		if !m.ReadElement(samples, 1, text, &r) || !r.Silent() {
			t.Fatalf("ReadElement: %+v", r)
		}
		value, err := m.WriteValue(field(t, inst, "seeds"))
		if err != nil {
			t.Fatalf("WriteValue: %v", err)
		}
		if len(value) == 0 {
			t.Fatal("WriteValue produced nothing")
		}
	})

	t.Run("union arm", func(t *testing.T) {
		m, _ := load(t, "../../tables/maps")
		st := m.Lookup("Depth")
		if st == nil {
			t.Fatal("no Depth table")
		}
		inst := m.New(st)
		var r tabletext.Report
		text := `{
			"one": { "roster": { "5": { "count": 1 } } },
			"many": [ { "roster": {} } ],
			"keyed": { "Alpha": { "roster": {} }, "Beta": { "roster": { "2": { "count": 2 } } } },
			"arm": { "plain": 42 },
			"past": 9
		}`
		if !m.Read(inst, []byte(text), &r) || r.Malformed {
			t.Fatalf("union arm text refused: %+v", r)
		}
		arm := field(t, inst, "arm")
		if arm.Cell.U != 2 || arm.Cell.Arm == nil || arm.Cell.Arm.Cell.I != 42 {
			t.Fatalf("the field arm did not read: tag=%d arm=%+v", arm.Cell.U, arm.Cell.Arm)
		}
		un, _ := field(t, inst, "arm").Def.Type.Ref.(*ir.Union)
		if un == nil {
			t.Fatal("arm field is not a union")
		}
		for _, v := range un.Variants {
			fv := m.NewArm(v)
			if v.Void() != (fv == nil) {
				t.Fatalf("NewArm(%s) nil=%v, void=%v", v.Name, fv == nil, v.Void())
			}
		}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("union write: %v", err)
		}
		if !bytes.Contains(out, []byte(`"plain"`)) {
			t.Fatalf("the field arm did not write:\n%s", out)
		}
	})

	t.Run("map key order", func(t *testing.T) {
		str := &ir.Field{MapEntry: &ir.Struct{Fields: []*ir.Field{{Name: "key", Type: ir.FieldType{Kind: ir.TString, Size: 8}}}}}
		signed := &ir.Field{MapEntry: &ir.Struct{Fields: []*ir.Field{{Name: "key", Type: ir.FieldType{Kind: ir.TInt, Signed: true}}}}}
		unsigned := &ir.Field{MapEntry: &ir.Struct{Fields: []*ir.Field{{Name: "key", Type: ir.FieldType{Kind: ir.TInt}}}}}

		if got := tabletext.MapKeyOrder(str, tabletext.MapKey{Str: []byte("a")}, tabletext.MapKey{Str: []byte("ab")}); got != -1 {
			t.Fatalf("string prefix order = %d, want -1", got)
		}
		if got := tabletext.MapKeyOrder(str, tabletext.MapKey{Str: []byte("b")}, tabletext.MapKey{Str: []byte("a")}); got != 1 {
			t.Fatalf("string order = %d, want 1", got)
		}
		if got := tabletext.MapKeyOrder(str, tabletext.MapKey{Str: []byte("a")}, tabletext.MapKey{Str: []byte("a")}); got != 0 {
			t.Fatalf("equal strings order = %d, want 0", got)
		}
		if got := tabletext.MapKeyOrder(signed, tabletext.MapKey{I: -4}, tabletext.MapKey{I: 2}); got != -1 {
			t.Fatalf("signed order = %d, want -1", got)
		}
		if got := tabletext.MapKeyOrder(signed, tabletext.MapKey{I: 4}, tabletext.MapKey{I: 2}); got != 1 {
			t.Fatalf("signed order = %d, want 1", got)
		}
		if got := tabletext.MapKeyOrder(signed, tabletext.MapKey{I: 2}, tabletext.MapKey{I: 2}); got != 0 {
			t.Fatalf("equal signed order = %d, want 0", got)
		}
		if got := tabletext.MapKeyOrder(unsigned, tabletext.MapKey{U: 1}, tabletext.MapKey{U: 2}); got != -1 {
			t.Fatalf("unsigned order = %d, want -1", got)
		}
		if got := tabletext.MapKeyOrder(unsigned, tabletext.MapKey{U: 3}, tabletext.MapKey{U: 2}); got != 1 {
			t.Fatalf("unsigned order = %d, want 1", got)
		}
		if got := tabletext.MapKeyOrder(unsigned, tabletext.MapKey{U: 2}, tabletext.MapKey{U: 2}); got != 0 {
			t.Fatalf("equal unsigned order = %d, want 0", got)
		}
		if !tabletext.MapKeyFits(unsigned, tabletext.MapKey{U: 1 << 40}) {
			t.Fatal("an integer key is always within its kind")
		}
		if tabletext.MapKeyFits(str, tabletext.MapKey{Str: []byte("123456789")}) {
			t.Fatal("a string key past string(8) must not fit")
		}
	})

	t.Run("writer strings", func(t *testing.T) {
		m, _ := load(t, "../../tables/examples")
		st := m.Lookup("GunnerSettings")
		if st == nil {
			t.Fatal("no GunnerSettings table")
		}
		inst := m.New(st)
		// one well-formed sequence of each width, then every shape utf8Sequence
		// refuses: a stray continuation, an overlong form, a surrogate half, a
		// point past U+10FFFF, and a truncated tail.
		field(t, inst, "callsign").Cell.Str = []byte{
			'a', 0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x98, 0x80,
			0x80, 0xc0, 0xaf, 0xed, 0xa0, 0x80, 0xf4, 0x90, 0x80, 0x80, 0xe2,
		}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if !utf8.Valid(out) {
			t.Fatalf("the writer emitted invalid UTF-8: %q", out)
		}
		if n := bytes.Count(out, []byte("\ufffd")); n != 11 {
			t.Fatalf("the writer emitted %d replacement points, want 11:\n%q", n, out)
		}
	})

	t.Run("float exponents", func(t *testing.T) {
		m, _ := load(t, "../../tables/examples")
		st := m.Lookup("ProfileConfig")
		if st == nil {
			t.Fatal("no ProfileConfig table")
		}
		inst := m.New(st)
		fv := field(t, inst, "precision")
		for _, row := range []struct {
			value float64
			want  string
		}{
			{1e-9, "1e-09"},
			{1e-20, "1e-20"},
			{1e20, "1e+20"},
			{1.5, "1.5"},
		} {
			fv.Cell.F = row.value
			text, err := m.WriteValue(fv)
			if err != nil {
				t.Fatalf("write %v: %v", row.value, err)
			}
			if string(bytes.TrimSpace(text)) != row.want {
				t.Fatalf("float %v wrote %q, want %q", row.value, text, row.want)
			}
		}
	})

	t.Run("wide string units", func(t *testing.T) {
		dir := t.TempDir()
		source := `package wstr

table Words
{
    w wstring(16)
}
`
		if err := os.WriteFile(filepath.Join(dir, "Words.schema"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		m, _ := load(t, dir)
		st := m.Lookup("Words")
		if st == nil {
			t.Fatal("no Words table")
		}
		inst := m.New(st)
		// a surrogate PAIR is one code point; an unpaired half and a zero unit
		// each have their own spelling (§16.2, §16.3).
		field(t, inst, "w").Cell.Units = []uint16{'a', 0xd83d, 0xde00, 0xd800, 0x0000}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("wstring write: %v", err)
		}
		if !utf8.Valid(out) {
			t.Fatalf("wstring writer emitted invalid UTF-8: %q", out)
		}
		back := m.New(st)
		var r tabletext.Report
		// A ZERO UNIT IS REFUSED BY NAME ON READ (§16.2): the escaped form has
		// no storage value, so the text is named rather than quietly kept.
		if m.Read(back, out, &r) || !r.Malformed || r.Reason != "text_holds_zero: Words.w" {
			t.Fatalf("a zero unit must refuse by name: %+v\n%s", r, out)
		}
		// without it the same text round-trips, pair and replacement intact.
		field(t, inst, "w").Cell.Units = []uint16{'a', 0xd83d, 0xde00, 0xd800}
		out, err = m.Write(inst)
		if err != nil {
			t.Fatalf("wstring write: %v", err)
		}
		back = m.New(st)
		var rr tabletext.Report
		if !m.Read(back, out, &rr) || !rr.Silent() {
			t.Fatalf("wstring round trip: %+v\n%s", rr, out)
		}
		units := field(t, back, "w").Cell.Units
		if len(units) != 4 || units[1] != 0xd83d || units[2] != 0xde00 || units[3] != 0xfffd {
			t.Fatalf("wstring units came back %#v", units)
		}
	})

	t.Run("optional map value", func(t *testing.T) {
		dir := t.TempDir()
		source := `package optmap

type Item
{
    n int32
}

table Holder
{
    m map[uint8]?Item
}
`
		if err := os.WriteFile(filepath.Join(dir, "Opt.schema"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		m, _ := load(t, dir)
		st := m.Lookup("Holder")
		if st == nil {
			t.Fatal("no Holder table")
		}
		inst := m.New(st)
		var r tabletext.Report
		text := `{ "m": { "1": null, "2": { "n": 7 } } }`
		if !m.Read(inst, []byte(text), &r) || r.Malformed {
			t.Fatalf("optional map value refused: %+v", r)
		}
		entries := field(t, inst, "m").Entries
		if len(entries) != 2 {
			t.Fatalf("optional map holds %d entries, want 2", len(entries))
		}
		if entries[0].Tab.Fields[1].Present || !entries[1].Tab.Fields[1].Present {
			t.Fatalf("optional presence is wrong: %+v", entries)
		}
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("optional map write: %v", err)
		}
		if !bytes.Contains(out, []byte("null")) {
			t.Fatalf("an absent optional map value did not write null:\n%s", out)
		}
	})

	t.Run("corpora round trip", func(t *testing.T) {
		for _, dir := range []string{
			"../../tables/maps", "../../tables/scalars", "../../tables/lists",
			"../../tables/arms", "../../tables/pointers", "../../tables/messages",
			"../../tables/stream", "../../tables/blobs",
		} {
			m, _ := load(t, dir)
			for _, name := range m.Roots() {
				st := m.Lookup(name)
				if st == nil {
					continue
				}
				inst := m.New(st)
				first, err := m.Write(inst)
				if err != nil {
					continue // a root whose defaults cannot be spelled is not this card's
				}
				back := m.New(st)
				var r tabletext.Report
				if !m.Read(back, first, &r) {
					t.Fatalf("%s/%s: wrote a text it cannot read: %+v\n%s", dir, name, r, first)
				}
			}
		}
	})
}

func load(t testing.TB, dir string) (*tabletext.Model, *ir.Unit) {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u), u
}
