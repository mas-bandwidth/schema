package tabletext_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unit(t testing.TB) *ir.Unit {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// read places one text into one instance of the named table and hands back
// the report, so a case is one line.
func read(t testing.TB, table, text string) (*tabletext.Model, *tabletext.Instance, tabletext.Report) {
	t.Helper()
	m := tabletext.NewModel(unit(t))
	st := m.Lookup(table)
	if st == nil {
		t.Fatalf("no table %s", table)
	}
	inst := m.New(st)
	var report tabletext.Report
	m.Read(inst, []byte(text), &report)
	return m, inst, report
}

func field(t testing.TB, inst *tabletext.Instance, key string) *tabletext.Field {
	t.Helper()
	fv, ok := inst.FieldByKey(key)
	if !ok {
		t.Fatalf("no field keyed %q on %s", key, inst.Def.Name)
	}
	return fv
}

// §16.2: unknown keys are skipped and counted; nothing else moves.
func TestUnknownKeyIsCounted(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 90, "mystery": [1,2,3] }`)
	if r.Unknown != 1 || r.Malformed {
		t.Fatalf("expected one unknown, got %+v", r)
	}
	if field(t, inst, "tick_rate").Cell.U != 90 {
		t.Fatal("the known key beside it was not placed")
	}
}

// §16.2: a duplicate key is last-wins, counted.
func TestDuplicateKeyIsLastWins(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 90, "tick_rate": 120 }`)
	if r.Duplicate != 1 {
		t.Fatalf("expected one duplicate, got %+v", r)
	}
	if field(t, inst, "tick_rate").Cell.U != 120 {
		t.Fatal("the last occurrence did not win")
	}
}

// §16.2: a key present with the wrong JSON type is skipped, NEVER coerced.
func TestWrongTypeIsSkippedNotCoerced(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": "120" }`)
	if r.KindMismatch != 1 || r.Malformed {
		t.Fatalf("expected one kind_mismatch, got %+v", r)
	}
	if got := field(t, inst, "tick_rate").Cell.U; got != 60 {
		t.Fatalf("the field should hold its declared default, got %d", got)
	}
}

// §16.2: an integer field given a fraction is the wrong SHAPE for the kind,
// counted, never rounded into place.
func TestFractionWhereIntegerDeclared(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 90.5 }`)
	if r.KindMismatch != 1 {
		t.Fatalf("expected one kind_mismatch, got %+v", r)
	}
	if field(t, inst, "tick_rate").Cell.U != 60 {
		t.Fatal("a fraction was rounded into an integer field")
	}
}

// §16.2: a number outside the declared range is CLAMPED and counted, never
// refused — the wire's rule, so a text and a wire land the same instance.
func TestOutOfRangeIsClamped(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 100000 }`)
	if r.Clamped == 0 {
		t.Fatalf("expected a clamp, got %+v", r)
	}
	if got := field(t, inst, "tick_rate").Cell.U; got != 240 {
		t.Fatalf("expected the declared max, got %d", got)
	}
}

// §16.2: a string longer than the field is clamped AT A CODE POINT BOUNDARY,
// counted — never cut through a multi-byte character.
func TestStringClampsAtCodePointBoundary(t *testing.T) {
	// callsign is string(24); twelve three-byte code points is 36 bytes, so the
	// clamp lands after the eighth, at byte 24
	long := strings.Repeat("é", 20) // two bytes each: 40 bytes
	_, inst, r := read(t, "GunnerSettings", `{ "callsign": "`+long+`" }`)
	if r.Clamped != 1 {
		t.Fatalf("expected one clamp, got %+v", r)
	}
	got := field(t, inst, "callsign").Cell.Str
	if len(got) != 24 {
		t.Fatalf("expected 24 bytes kept, got %d", len(got))
	}
	if !bytes.Equal(got, []byte(strings.Repeat("é", 12))) {
		t.Fatal("the clamp cut through a code point")
	}
}

// §16.2: escapes, including a surrogate pair, decode to their UTF-8 bytes.
func TestEscapesAndSurrogates(t *testing.T) {
	_, inst, r := read(t, "GunnerSettings", `{ "callsign": "a\tbé😀" }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	want := "a\tbé\U0001F600"
	if got := string(field(t, inst, "callsign").Cell.Str); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// §16.2: an enum reads by variant NAME; a name this build cannot name reads as
// None and counts as unknown.
func TestEnumByNameAndUnknownVariant(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "difficulty": "Easy" }`)
	if !r.Silent() || field(t, inst, "difficulty").Cell.U != 1 {
		t.Fatalf("Easy did not land: %+v", r)
	}
	_, inst, r = read(t, "GlobalSettings", `{ "difficulty": "Brutal" }`)
	if r.Unknown != 1 || field(t, inst, "difficulty").Cell.U != 0 {
		t.Fatalf("an unknown variant should read as None and count: %+v", r)
	}
}

// §16.2: `?T` presence is the presence of the KEY, whatever the value.
func TestOptionalPresenceIsTheKey(t *testing.T) {
	_, inst, _ := read(t, "ShipEntry", `{ "name": "X" }`)
	if field(t, inst, "gunner").Present {
		t.Fatal("an absent key left the optional present")
	}
	// present, and holding nothing but defaults: presence, not content
	_, inst, _ = read(t, "ShipEntry", `{ "gunner": {} }`)
	if !field(t, inst, "gunner").Present {
		t.Fatal("a present key did not set presence")
	}
}

// §16.2, §2.4: an enum-keyed array is an object keyed by VARIANT NAME; an
// absent key keeps that slot's defaults, an unknown key is counted, and None
// keys no record.
func TestEnumKeyedObject(t *testing.T) {
	m, inst, r := read(t, "PackConfig", `{ "thresholds": { "Hard": 500, "Nowhere": 1, "None": 2 } }`)
	if r.Unknown != 2 {
		t.Fatalf("expected two unknown keys (Nowhere and None), got %+v", r)
	}
	fv := field(t, inst, "thresholds")
	hard := tabletext.EnumValue(fv.Def.KeyEnumRef, "Hard")
	if got := fv.Elems[tabletext.KeyedValueSlot(fv.Def, hard)].U; got != 500 {
		t.Fatalf("the Hard slot holds %d", got)
	}
	easy := tabletext.EnumValue(fv.Def.KeyEnumRef, "Easy")
	if got := fv.Elems[tabletext.KeyedValueSlot(fv.Def, easy)].U; got != 0 {
		t.Fatalf("an absent key should keep the slot's default, got %d", got)
	}
	_ = m
}

// §16.2: a duplicate keyed slot is last-wins, counted.
func TestEnumKeyedDuplicateSlot(t *testing.T) {
	_, inst, r := read(t, "PackConfig", `{ "thresholds": { "Hard": 100, "Hard": 300 } }`)
	if r.Duplicate != 1 {
		t.Fatalf("expected one duplicate, got %+v", r)
	}
	fv := field(t, inst, "thresholds")
	hard := tabletext.KeyedValueSlot(fv.Def, tabletext.EnumValue(fv.Def.KeyEnumRef, "Hard"))
	if fv.Elems[hard].U != 300 {
		t.Fatal("the last occurrence did not win")
	}
}

// §16.3: `json = "key"` is honoured on the way in and on the way out, and the
// field's own name is then NOT a key.
func TestJsonKeyIsHonoured(t *testing.T) {
	m, inst, r := read(t, "ShipEntry", `{ "name": "Vulture" }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	if got := string(field(t, inst, "name").Cell.Str); got != "Vulture" {
		t.Fatalf("got %q", got)
	}
	_, _, r = read(t, "ShipEntry", `{ "display_name": "Vulture" }`)
	if r.Unknown != 1 {
		t.Fatalf("the declaration's own name is not the key once json names one: %+v", r)
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(text, []byte(`"name"`)) || bytes.Contains(text, []byte(`"display_name"`)) {
		t.Fatalf("the writer did not use the json key:\n%s", text)
	}
}

// §16.2: trailing commas are accepted on read — the authoring files this form
// exists for carry them — and comments are not JSON and are refused.
func TestTrailingCommasAndComments(t *testing.T) {
	_, _, r := read(t, "GlobalSettings", "{ \"tick_rate\": 90, }")
	if !r.Silent() {
		t.Fatalf("a trailing comma should be accepted, got %+v", r)
	}
	_, _, r = read(t, "GlobalSettings", "{ // a comment\n \"tick_rate\": 90 }")
	if !r.Malformed {
		t.Fatal("a comment should be refused")
	}
}

// §16.2: a union is an object with ONE key; {} is None, and two keys is a text
// the walk will not guess at.
func TestUnionShape(t *testing.T) {
	_, inst, r := read(t, "WeaponConfig", `{ "effect": { "buff": { "multiplier": 3.0 } } }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	if field(t, inst, "effect").Cell.U != 1 {
		t.Fatal("the buff arm did not land")
	}
	_, inst, r = read(t, "WeaponConfig", `{ "effect": {} }`)
	if !r.Silent() || field(t, inst, "effect").Cell.U != 0 {
		t.Fatalf("{} is None: %+v", r)
	}
	_, _, r = read(t, "WeaponConfig", `{ "effect": { "buff": {}, "debuff": {} } }`)
	if !r.Malformed {
		t.Fatal("a union with two keys should be malformed")
	}
}

// §16.2: flags read as an array of variant NAMES; an unknown name is skipped
// and counted.
func TestFlagsByName(t *testing.T) {
	_, inst, r := read(t, "LoadoutConfig", `{ "perks": [ "Shielded", "Turbo", "Nope" ] }`)
	if r.Unknown != 1 {
		t.Fatalf("expected one unknown bit name, got %+v", r)
	}
	if got := field(t, inst, "perks").Cell.U; got != 0b101 {
		t.Fatalf("expected bits 0 and 2, got %b", got)
	}
}

// §16.2: `bytes(N)` rides as base64, clamped past the bound and counted; a
// body that is not base64 is the wrong shape for the kind.
func TestBytesBase64(t *testing.T) {
	_, inst, r := read(t, "ProfileConfig", `{ "icon": "AQIDBA==" }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	if !bytes.Equal(field(t, inst, "icon").Cell.Str, []byte{1, 2, 3, 4}) {
		t.Fatal("base64 did not decode")
	}
	_, _, r = read(t, "ProfileConfig", `{ "icon": "not base64!" }`)
	if r.KindMismatch != 1 {
		t.Fatalf("a body that is not base64 is the wrong shape: %+v", r)
	}
}

// §16.2: a guarded group's guard is an ordinary bool in the text, and the walk
// infers NOTHING from the presence of the group.
func TestGuardsInferNothing(t *testing.T) {
	m, inst, r := read(t, "ProfileConfig", `{ "loadout": { "grade": "Gold" } }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	if field(t, inst, "has_loadout").Cell.B {
		t.Fatal("the walk inferred a guard from the presence of the group")
	}
	// and the writer drops the group while the guard reads false
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(text, []byte(`"loadout"`)) {
		t.Fatalf("a guarded-out group should not be written:\n%s", text)
	}
}

// §16.2: an array past the reader's bound keeps the bounded prefix and counts;
// a fixed array's tail keeps its defaults.
func TestArrayBounds(t *testing.T) {
	_, inst, r := read(t, "ShipEntry", `{ "hardpoints": [1,2,3,4,5,6] }`)
	if r.Clamped != 2 {
		t.Fatalf("expected two dropped elements, got %+v", r)
	}
	if got := field(t, inst, "hardpoints").Count; got != 4 {
		t.Fatalf("expected the bounded prefix, got %d", got)
	}
}

// A text this walk wrote is a text this walk reads back into the same value:
// ToJson -> FromJson -> ToJson is byte-stable for every table in the corpus.
func TestWriteReadWriteIsStable(t *testing.T) {
	m := tabletext.NewModel(unit(t))
	for _, name := range m.Roots() {
		st := m.Lookup(name)
		inst := m.New(st)
		first, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back := m.New(st)
		var r tabletext.Report
		if !m.Read(back, first, &r) {
			t.Fatalf("%s: the writer's own text did not read back: %+v\n%s", name, r, first)
		}
		if !r.Silent() {
			t.Fatalf("%s: the writer's own text reported %+v", name, r)
		}
		second, err := m.Write(back)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: the round trip moved bytes\n%s\n%s", name, first, second)
		}
	}
}

// The tokenizer never panics and never leaves a half-placed instance behind:
// whatever the bytes, either the text reads and the instance re-writes to a
// text that reads back the same, or the report says malformed.
func FuzzReadText(f *testing.F) {
	seeds := []string{
		`{}`,
		`{ "tick_rate": 120, "difficulty": "Hard" }`,
		`{ "tick_rate": 120, }`,
		`{ "build_note": "aé😀b" }`,
		`{ "spawn_delays": [ 0.5, 1e30, -0 ] }`,
		`{ "difficulty": "Nope", "mystery": { "a": [1, {"b": null}] } }`,
		`{ "tick_rate": 99999999999999999999999999 }`,
		`{ "tick_rate": -1 }`,
		`{ "tick_rate": 1.5 }`,
		"{ \"build_note\": \"\xff\xfe\" }",
		`{ "build_note": "\uD800" }`,
		`{`,
		`[]`,
		`{ "a": }`,
		`{ "tick_rate": 1 } trailing`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	u := unit(f)
	m := tabletext.NewModel(u)
	st := m.Lookup("GlobalSettings")
	f.Fuzz(func(t *testing.T, data []byte) {
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, data, &r) {
			if !r.Malformed {
				t.Fatal("a refused text left malformed unset")
			}
			return
		}
		text, err := m.Write(inst)
		if err != nil {
			return // a value with no text spelling is refused, not a crash
		}
		again := m.New(st)
		var r2 tabletext.Report
		if !m.Read(again, text, &r2) {
			t.Fatalf("the writer's own text did not read back:\n%s", text)
		}
		second, err := m.Write(again)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(text, second) {
			t.Fatalf("the round trip moved bytes:\n%s\n%s", text, second)
		}
	})
}
