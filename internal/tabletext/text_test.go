package tabletext_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

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

// §16.2: a repeated keyed SLOT key is last-wins and is NOT counted — the
// generated walk's behaviour, and the counter §16.2 raises for a TABLE
// object's keys. Two implementations reporting differently on one text is what
// the goldens exist to prevent, so the reference decides it.
func TestEnumKeyedDuplicateSlot(t *testing.T) {
	_, inst, r := read(t, "PackConfig", `{ "thresholds": { "Hard": 100, "Hard": 300 } }`)
	if !r.Silent() {
		t.Fatalf("a repeated slot key is silent here, got %+v", r)
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
// whatever the bytes, either the text is refused with malformed set, or it
// reads into an instance the writer can spell — and the text form reaches a
// FIXED POINT after one lap.
//
// One lap, not zero, and the reason is §16.3's: the read path is
// byte-transparent, so a raw ill-formed byte rides into storage as itself,
// and the writer replaces it with U+FFFD — three bytes where one stood. A
// string(N) can therefore come back one lap shorter, clamped at its own bound.
// That is the stated cost of emitting a text any conforming parser can read,
// and it is a property to pin rather than a bug to chase: after the first
// write every string is well-formed, so the second and third texts agree.
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
		first, err := m.Write(inst)
		if err != nil {
			t.Fatalf("a value this read accepted cannot be written: %v", err)
		}
		if !utf8.Valid(first) {
			t.Fatalf("the writer emitted a text that is not valid UTF-8:\n%q", first)
		}
		second := lap(t, m, st, first)
		third := lap(t, m, st, second)
		if !bytes.Equal(second, third) {
			t.Fatalf("the text form has no fixed point:\n%s\n%s", second, third)
		}
	})
}

// lap reads one text back and writes it out again.
func lap(t *testing.T, m *tabletext.Model, st *ir.Struct, text []byte) []byte {
	t.Helper()
	inst := m.New(st)
	var r tabletext.Report
	if !m.Read(inst, text, &r) {
		t.Fatalf("the writer's own text did not read back: %+v\n%s", r, text)
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatalf("the writer's own text produced a value it cannot write: %v", err)
	}
	return out
}

// numberCases is the number grammar and its semantics, row by row
// (SPEC-TABLES.md §16.2). Every row was measured against the generated C++
// reader before it was written down: the tokenizer is RFC 8259's, an integer
// field takes any token whose VALUE is integral however it was spelled, a
// genuinely fractional value is the wrong shape for it, a magnitude a float
// field cannot hold is the wrong shape too — so no path stores an infinity —
// and a token that is not a JSON number at all is a DIAGNOSTIC, never a
// quietly clamped value.
var numberCases = []struct {
	token string
	field string // "experience" uint32, "precision" float64, "timestamp" int64
	want  string // "value=N" | "kind_mismatch" | "clamped" | "malformed"
}{
	// integral in VALUE, however it is spelled
	{"1e3", "experience", "value=1000"},
	{"1e2", "experience", "value=100"},
	{"1E2", "experience", "value=100"},
	{"1e+2", "experience", "value=100"},
	{"2.0", "experience", "value=2"},
	{"1.0e1", "experience", "value=10"},
	{"0.0", "experience", "value=0"},
	{"0e0", "experience", "value=0"},
	{"0", "experience", "value=0"},
	{"-0", "experience", "value=0"},
	{"-0.0", "experience", "value=0"},
	{"2e1", "experience", "value=20"},
	{"7", "experience", "value=7"},
	// genuinely fractional in an integer field: the wrong shape, never rounded
	{"1.5", "experience", "kind_mismatch"},
	{"1e-2", "experience", "kind_mismatch"},
	{"0.5", "experience", "kind_mismatch"},
	// not a JSON number at all: a typo is a diagnostic
	{"1-2", "experience", "malformed"},
	{"+7", "experience", "malformed"},
	{"007", "experience", "malformed"},
	{"01", "experience", "malformed"},
	{".5", "experience", "malformed"},
	{"3.", "experience", "malformed"},
	{"1.2.3", "experience", "malformed"},
	{"--3", "experience", "malformed"},
	{"1e", "experience", "malformed"},
	{"1e+", "experience", "malformed"},
	{"1..2", "experience", "malformed"},
	{"1e2e3", "experience", "malformed"},
	{"5+", "experience", "malformed"},
	{"-", "experience", "malformed"},
	{"", "experience", "malformed"},
	// out of what the field can hold: clamped, and counted
	{"-1", "experience", "clamped"},
	{"99999999999999999999999999", "experience", "clamped"},
	// a magnitude no float of that width holds: the wrong shape, never an infinity
	{"1e400", "precision", "kind_mismatch"},
	{"1e1000000", "precision", "kind_mismatch"},
	{"9e99999", "precision", "kind_mismatch"},
	{"-1e400", "precision", "kind_mismatch"},
	{"1e308", "precision", "value=1e+308"},
	{"-9223372036854775808", "timestamp", "value=-9223372036854775808"},
}

func TestNumberGrammarAndSemantics(t *testing.T) {
	for _, tc := range numberCases {
		t.Run(tc.field+"="+tc.token, func(t *testing.T) {
			m, inst, r := read(t, "ProfileConfig", `{ "`+tc.field+`": `+tc.token+` }`)
			switch tc.want {
			case "malformed":
				if !r.Malformed {
					t.Fatalf("%q is not a JSON number and must be malformed, got %+v", tc.token, r)
				}
				return
			case "kind_mismatch":
				if r.KindMismatch != 1 || r.Malformed {
					t.Fatalf("expected one kind_mismatch, got %+v", r)
				}
			case "clamped":
				if r.Clamped == 0 || r.Malformed {
					t.Fatalf("expected a clamp, got %+v", r)
				}
			default:
				if !r.Silent() {
					t.Fatalf("expected silence, got %+v", r)
				}
				text, err := m.WriteValue(field(t, inst, tc.field))
				if err != nil {
					t.Fatal(err)
				}
				if got := "value=" + string(text); got != tc.want {
					t.Fatalf("got %s, want %s", got, tc.want)
				}
			}
			// whatever was placed, the instance must be WRITABLE: a read this
			// walk calls clean can never produce a value the writer refuses
			// (SPEC-TABLES.md §16.3)
			if _, err := m.Write(inst); err != nil {
				t.Fatalf("a value this read accepted cannot be written: %v", err)
			}
		})
	}
}

// §16.2: `null` is kind_mismatch for every kind except the two where absence is
// a value — a `?T` reads it as ABSENT, and a pointer reads it as null.
func TestNullPerKind(t *testing.T) {
	_, inst, r := read(t, "ShipEntry", `{ "name": "X", "gunner": null }`)
	if !r.Silent() {
		t.Fatalf("null on a ?T is the absence, not a mismatch: %+v", r)
	}
	if field(t, inst, "gunner").Present {
		t.Fatal("null on a ?T left the field present")
	}
	// and it beats an earlier occurrence, because last wins as a whole value
	_, inst, r = read(t, "ShipEntry", `{ "gunner": { "tracking": true }, "gunner": null }`)
	if r.Duplicate != 1 || field(t, inst, "gunner").Present {
		t.Fatalf("a repeated key ending in null must leave the field absent: %+v", r)
	}
	for _, key := range []string{"name", "health", "hardpoints"} {
		_, _, r := read(t, "ShipEntry", `{ "`+key+`": null }`)
		if r.KindMismatch != 1 {
			t.Fatalf("null on %s should be a kind mismatch, got %+v", key, r)
		}
	}
}

// §16.3: the writer always emits valid UTF-8. A stored byte sequence that is
// not well-formed — which the storage permits and a lone surrogate escape can
// introduce — is written as U+FFFD, one per bad byte, never raw.
func TestWriterEmitsValidUTF8(t *testing.T) {
	// a lone surrogate escape READS as U+FFFD rather than manufacturing CESU-8
	_, inst, r := read(t, "GunnerSettings", `{ "callsign": "\uD800x" }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, []byte("�x")) {
		t.Fatalf("a lone lead surrogate must read as U+FFFD, got % x", got)
	}
	_, inst, _ = read(t, "GunnerSettings", `{ "callsign": "\uDC00x" }`)
	if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, []byte("�x")) {
		t.Fatalf("a lone trail surrogate must read as U+FFFD, got % x", got)
	}
	// a raw ill-formed byte rides to the wire as itself (§3 imposes no
	// encoding) and is REPLACED on the way out, because a JSON text must be
	// valid UTF-8
	m, inst, _ := read(t, "GunnerSettings", "{ \"callsign\": \"a\xffb\" }")
	if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, []byte{'a', 0xff, 'b'}) {
		t.Fatalf("the read path is byte-transparent, got % x", got)
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(text, []byte("a�b")) {
		t.Fatalf("the writer passed an ill-formed byte through:\n%q", text)
	}
	if !utf8.Valid(text) {
		t.Fatal("the writer emitted a text that is not valid UTF-8")
	}
}

// §16.2: a `bits(N)` value over its implied [0, 2^N - 1] clamps and counts —
// the bound the declaration implies rather than one it states, and the one the
// generated backend clamps against on the way back in.
func TestBitsImpliedBound(t *testing.T) {
	_, inst, r := read(t, "WeaponConfig", `{ "channel": 63 }`)
	if !r.Silent() || field(t, inst, "channel").Cell.U != 63 {
		t.Fatalf("63 fits in bits(6): %+v", r)
	}
	_, inst, r = read(t, "WeaponConfig", `{ "channel": 64 }`)
	if r.Clamped != 1 {
		t.Fatalf("expected one clamp, got %+v", r)
	}
	if got := field(t, inst, "channel").Cell.U; got != 63 {
		t.Fatalf("expected the implied max, got %d", got)
	}
}

// The one lap the text form is NOT byte-stable over, pinned rather than left
// to a fuzz find: a raw ill-formed byte rides into storage as itself (§3
// imposes no encoding), the writer replaces it with U+FFFD — three bytes where
// one stood — and a `string(N)` therefore comes back clamped at its own bound.
// It is the stated cost of emitting a text any conforming parser can read.
func TestIllFormedByteCostsOneLap(t *testing.T) {
	// build_note is string(48): 46 filler bytes plus one ill-formed byte fits,
	// and the U+FFFD that replaces it does not
	body := strings.Repeat("a", 47) + "\xff"
	m, inst, r := read(t, "GlobalSettings", "{ \"build_note\": \""+body+"\" }")
	if !r.Silent() {
		t.Fatalf("the read path is byte-transparent: %+v", r)
	}
	if got := len(field(t, inst, "build_note").Cell.Str); got != 48 {
		t.Fatalf("expected 48 stored bytes, got %d", got)
	}
	first, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(first) {
		t.Fatal("the writer emitted a text that is not valid UTF-8")
	}
	second := lap(t, m, m.Lookup("GlobalSettings"), first)
	if bytes.Equal(first, second) {
		t.Fatal("this case exists to pin the lap that is NOT byte-stable")
	}
	if third := lap(t, m, m.Lookup("GlobalSettings"), second); !bytes.Equal(second, third) {
		t.Fatal("the text form did not settle after one lap")
	}
}
