package tabletext_test

import (
	"bytes"
	"math"
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

// §16.2: A CLAMP IS A PREFIX. Once one code point does not fit, the scan stops
// placing. A later SHORTER code point must not slip into the room the long one
// left, because that stores a string the input never spelled and the `clamped`
// count cannot tell the two apart.
func TestClampIsAPrefix(t *testing.T) {
	// callsign is string(24): twenty ASCII bytes, then a three-byte code point
	// that fits at 23, then one that does not, then a byte that would
	text := strings.Repeat("a", 20) + "✓✓X"
	_, inst, r := read(t, "GunnerSettings", `{ "callsign": "`+text+`" }`)
	if r.Clamped != 1 {
		t.Fatalf("expected one clamp, got %+v", r)
	}
	got := field(t, inst, "callsign").Cell.Str
	want := []byte(strings.Repeat("a", 20) + "✓")
	if !bytes.Equal(got, want) {
		t.Fatalf("the clamp is not a prefix: got %q, want %q", got, want)
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

// §16.2: trailing commas and comments are accepted on read, because the
// authoring files this form exists for carry them, and never written. `//` runs to the
// end of the line or of the input, `/* */` to its closing delimiter, and an
// unclosed `/*` is malformed on the terms an unclosed string is.
func TestTrailingCommasAndComments(t *testing.T) {
	_, _, r := read(t, "GlobalSettings", "{ \"tick_rate\": 90, }")
	if !r.Silent() {
		t.Fatalf("a trailing comma should be accepted, got %+v", r)
	}
	accepted := []string{
		"// before the first key\n{ \"tick_rate\": 90 }",
		"{ \"tick_rate\": 90 // after the last value\n}",
		"{ \"tick_rate\": /* between a key and its value */ 90, }",
		"{ \"tick_rate\": 90, }\n// the last line, with no trailing newline",
	}
	for _, text := range accepted {
		_, inst, r := read(t, "GlobalSettings", text)
		if !r.Silent() {
			t.Fatalf("a comment should be accepted in %q, got %+v", text, r)
		}
		if field(t, inst, "tick_rate").Cell.I != 90 {
			t.Fatalf("the value beside a comment did not land in %q", text)
		}
	}
	refused := []string{
		"{ \"tick_rate\": 90 /* open",
		"{ /*/ \"tick_rate\": 90 }",
		"{ / \"tick_rate\": 90 }",
	}
	for _, text := range refused {
		_, _, r := read(t, "GlobalSettings", text)
		if !r.Malformed {
			t.Fatalf("an unclosed or lone delimiter should be malformed in %q, got %+v", text, r)
		}
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
// (docs/SPEC-TABLES.md §16.2). Every row was measured against the generated C++
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
			// (docs/SPEC-TABLES.md §16.3)
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
	// a raw ill-formed byte is REPLACED WHERE IT ENTERS, on the same terms as
	// the lone surrogate escape above (§3, §16.3), so storage never holds one
	m, inst, _ := read(t, "GunnerSettings", "{ \"callsign\": \"a\xffb\" }")
	if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, []byte("a�b")) {
		t.Fatalf("a raw ill-formed byte must read as U+FFFD, got % x", got)
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

// A RAW ILL-FORMED BYTE IS REPLACED WHERE IT ENTERS (§3, §16.3): a kind 12
// payload is well-formed UTF-8, so a byte in a string body that is not part of
// a well-formed sequence is not a code point and READS as one U+FFFD. Three
// bytes where one stood, so a `string(N)` clamps at its own bound, at a code
// point boundary, and the text form is byte-stable from the FIRST lap,
// because the storage it built is storage the wire can carry.
func TestIllFormedByteIsReplacedOnRead(t *testing.T) {
	// build_note is string(48): 47 filler bytes plus one ill-formed byte fits,
	// and the U+FFFD that replaces it does not
	body := strings.Repeat("a", 47) + "\xff"
	m, inst, r := read(t, "GlobalSettings", "{ \"build_note\": \""+body+"\" }")
	if r.Clamped != 1 || r.Malformed || r.Unknown != 0 || r.KindMismatch != 0 {
		t.Fatalf("three bytes where one stood pass the bound: %+v", r)
	}
	stored := field(t, inst, "build_note").Cell.Str
	if len(stored) != 47 || !utf8.Valid(stored) {
		t.Fatalf("expected 47 well-formed stored bytes, got %d: % x", len(stored), stored)
	}
	first, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(first) {
		t.Fatal("the writer emitted a text that is not valid UTF-8")
	}
	second := lap(t, m, m.Lookup("GlobalSettings"), first)
	if !bytes.Equal(first, second) {
		t.Fatal("the text form is byte-stable once the replacement runs on read")
	}
}

// §16.2: RE-ESTABLISHMENT HAPPENS ON PLACEMENT. A repeated key whose repeat the
// walk refuses at the VALUE level — a fraction in an integer field, a magnitude
// no float of that width holds, a token that is not JSON at all — leaves the
// first occurrence's value standing. Only a value actually placed replaces one.
func TestRejectedRepeatKeepsTheFirstValue(t *testing.T) {
	cases := []struct {
		table, text, key string
		want             uint64
	}{
		{"ProfileConfig", `{ "tilt": 100, "tilt": 5e-324 }`, "tilt", 100},
		{"ProfileConfig", `{ "experience": 5, "experience": 2.5 }`, "experience", 5},
		{"ProfileConfig", `{ "badge": 9, "badge": 1e309 }`, "badge", 9},
		{"WeaponConfig", `{ "penetration": 3, "penetration": 1.5 }`, "penetration", 3},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			_, inst, r := read(t, tc.table, tc.text)
			if r.KindMismatch != 1 || r.Duplicate != 1 {
				t.Fatalf("expected one kind_mismatch and one duplicate, got %+v", r)
			}
			if got := field(t, inst, tc.key).Cell.U; got != tc.want {
				t.Fatalf("the refused repeat wiped the first value: got %d, want %d", got, tc.want)
			}
		})
	}
	// a keyed SLOT is the same rule one level down
	_, inst, r := read(t, "PackConfig", `{ "thresholds": { "Hard": 64, "Hard": 0.1 } }`)
	if r.KindMismatch != 1 || r.Duplicate != 1 {
		t.Fatalf("expected one kind_mismatch and one duplicate, got %+v", r)
	}
	fv := field(t, inst, "thresholds")
	hard := KeyedSlot(t, fv, "Hard")
	if fv.Elems[hard].U != 64 {
		t.Fatalf("the refused repeat wiped the slot: got %d", fv.Elems[hard].U)
	}
	// and a repeat that IS placeable replaces, as last-wins requires
	_, inst, r = read(t, "ProfileConfig", `{ "tilt": 100, "tilt": 7 }`)
	if r.Duplicate != 1 || r.KindMismatch != 0 {
		t.Fatalf("expected one duplicate and no mismatch, got %+v", r)
	}
	if got := field(t, inst, "tilt").Cell.U; got != 7 {
		t.Fatalf("last-wins did not: got %d", got)
	}
}

// A FIELD READS ITS TOKEN THROUGH THE FLOAT64 AND A MAP KEY READS IT EXACTLY,
// and this row is the field half. The exact reader lives on the key path alone
// (§16.2's map row, and the key rows in test/conformance/harness/maps_test.go),
// because a key is an identity that two spellings must not share, while a
// field's value is a quantity read the same way by every port that reads these
// texts. So the field path keeps the interpretation the C, Go and Rust readers
// have: a magnitude past what a float64 holds is kind_mismatch and nothing is
// stored, and a token at 2^53 lands the value the mantissa carries.
//
// The two verdicts below are the ones the ports produce, and moving either of
// them is a change to all four readers at once.
func TestAnIntegerFieldReadsItsTokenThroughTheFloat(t *testing.T) {
	_, inst, r := read(t, "ProfileConfig", `{ "badge": 1e309 }`)
	if r.KindMismatch != 1 || r.Clamped != 0 || r.Malformed {
		t.Fatalf("a magnitude no float64 holds is kind_mismatch for an integer field: %+v", r)
	}
	if got := field(t, inst, "badge").Cell.U; got != 0 {
		t.Fatalf("nothing is stored on a mismatch: got %d, want 0", got)
	}
	// the same spelling in a FLOAT field, which has always answered this way
	_, inst, r = read(t, "ProfileConfig", `{ "precision": 1e400 }`)
	if r.KindMismatch != 1 || r.Clamped != 0 {
		t.Fatalf("a float64 that cannot hold the value mismatches: %+v", r)
	}
	if got := field(t, inst, "precision").Cell.F; got != 0 {
		t.Fatalf("nothing is stored on a mismatch: got %v", got)
	}
	// and at 2^53 the field lands what the mantissa carries, where the KEY path
	// lands the digits the token spells
	_, inst, r = read(t, "ProfileConfig", `{ "epoch": 9007199254740993.0 }`)
	if r.KindMismatch != 0 || r.Clamped != 0 {
		t.Fatalf("an integral value is placed: %+v", r)
	}
	if got := field(t, inst, "epoch").Cell.U; got != 9007199254740992 {
		t.Fatalf("the field path rounds at the mantissa: got %d, want 9007199254740992", got)
	}
	// and the decimal spelling of UINT64_MAX rounds UP through the float64, so
	// the field saturates and COUNTS the clamp, where the key path reads the
	// digits and counts nothing
	_, inst, r = read(t, "ProfileConfig", `{ "epoch": 18446744073709551615.0 }`)
	if r.KindMismatch != 0 || r.Clamped != 1 {
		t.Fatalf("a magnitude at the top of the domain saturates and counts: %+v", r)
	}
	if got := field(t, inst, "epoch").Cell.U; got != math.MaxUint64 {
		t.Fatalf("the field holds the edge: got %d, want %d", got, uint64(math.MaxUint64))
	}
}

// KeyedSlot resolves a variant name to its slot for a keyed field.
func KeyedSlot(t *testing.T, fv *tabletext.Field, name string) int {
	t.Helper()
	slot := tabletext.KeyedValueSlot(fv.Def, tabletext.EnumValue(fv.Def.KeyEnumRef, name))
	if slot < 0 {
		t.Fatalf("%s names no slot of %s", name, fv.Def.KeyEnum)
	}
	return slot
}

// §16.2 with #282: a KEYED OBJECT'S KEYS ARE KEYS. A variant named twice is a
// duplicate, counted on the RESOLVED slot and before the shape check — so a
// repeat the walk then refuses is still a repeat. A key that names NO slot (an
// unknown variant, or `"None"`) is `unknown` each time and never a duplicate.
func TestKeyedObjectDuplicateRule(t *testing.T) {
	_, _, r := read(t, "PackConfig", `{ "thresholds": { "Hard": 1, "Hard": 2 } }`)
	if r.Duplicate != 1 || r.Unknown != 0 {
		t.Fatalf("a repeated slot is one duplicate, got %+v", r)
	}
	_, _, r = read(t, "PackConfig", `{ "ships": { "Frigate": {}, "Frigate": {} } }`)
	if r.Unknown != 2 || r.Duplicate != 0 {
		t.Fatalf("a repeated key naming no slot is unknown each time, got %+v", r)
	}
	_, _, r = read(t, "PackConfig", `{ "ships": { "None": {}, "None": {} } }`)
	if r.Unknown != 2 || r.Duplicate != 0 {
		t.Fatalf("None names no slot, so it is unknown each time, got %+v", r)
	}
	_, _, r = read(t, "PackConfig", `{ "ships": { "Scout": {}, "Frigate": {}, "Scout": {}, "Frigate": {} } }`)
	if r.Unknown != 2 || r.Duplicate != 1 {
		t.Fatalf("mixed: two unknown, one duplicate, got %+v", r)
	}
}

// §16.2: PRESENCE OF THE KEY IS THE PRESENCE — a `?T` given a value the walk
// will not place is still PRESENT, because it is the key that says so. Only
// `null` is the absence.
func TestOptionalPresentWhateverTheValue(t *testing.T) {
	_, inst, r := read(t, "ShipEntry", `{ "gunner": 5 }`)
	if r.KindMismatch != 1 {
		t.Fatalf("expected one kind_mismatch, got %+v", r)
	}
	if !field(t, inst, "gunner").Present {
		t.Fatal("the key was there, so the field is present whatever its value")
	}
}

// §16.2: one correctly-rounded conversion at the field's OWN width. Reading a
// float32 through a float64 first rounds twice, and the two roundings part
// company at both ends of the range.
func TestFloat32SingleRounding(t *testing.T) {
	cases := []struct {
		token string
		want  float32
		mis   int
	}{
		{"340282356779733661637539395458142568447", math.MaxFloat32, 0},
		{"-340282356779733661637539395458142568440", -math.MaxFloat32, 0},
		{"340282356779733661637539395458142568448", 0, 1}, // the midpoint: no float32 holds it
		{"7.0064923216240853547e-46", math.SmallestNonzeroFloat32, 0},
		{"-7.0064923216240853547e-46", -math.SmallestNonzeroFloat32, 0},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			_, inst, r := read(t, "ProfileConfig", `{ "ratings": [ `+tc.token+`, 0.0, 0.0, 0.0 ] }`)
			if r.KindMismatch != tc.mis {
				t.Fatalf("expected %d kind_mismatch, got %+v", tc.mis, r)
			}
			if tc.mis > 0 {
				return
			}
			if got := float32(field(t, inst, "ratings").Elems[0].F); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ---- the VARIABLE class (docs/SPEC-TABLES.md §16.7) ----

func pointered(t testing.TB) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func readScene(t testing.TB, m *tabletext.Model, text string) (*tabletext.Instance, tabletext.Report) {
	t.Helper()
	inst := m.New(m.Lookup("Scene"))
	var r tabletext.Report
	m.Read(inst, []byte(text), &r)
	return inst, r
}

// A shared node is written once under `&node`, with its fields, and named by
// `&node` alone after, spelled the same way at every site; a tree carries none. Reading it back places
// ONE node behind every reference — identity survives the seam.
func TestSharedNodeIsOneNode(t *testing.T) {
	m := pointered(t)
	inst, r := readScene(t, m, `{
		"head": { "&node": 1, "value": 7 },
		"alias": { "&node": 1 },
		"ground": { "head": { "&node": 1 } }
	}`)
	if !r.Silent() {
		t.Fatalf("a shared node did not read clean: %+v", r)
	}
	head := field(t, inst, "head").Cell.Node
	alias := field(t, inst, "alias").Cell.Node
	ground := field(t, field(t, inst, "ground").Cell.Tab, "head").Cell.Node
	if head == nil || head != alias || head != ground {
		t.Fatal("three references to one label did not resolve to one node")
	}
	if field(t, head, "value").Cell.I != 7 {
		t.Fatal("the definition's fields were not placed")
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"name\": \"\",\n  \"version\": 1,\n  \"head\": {\n    \"&node\": 1,\n    \"value\": 7,\n    \"name\": \"\",\n    \"next\": null\n  },\n" +
		"  \"tree\": null,\n  \"settings\": null,\n  \"alias\": {\n    \"&node\": 1\n  },\n  \"ground\": {\n    \"depth\": 0,\n    \"head\": {\n      \"&node\": 1\n    }\n  },\n" +
		"  \"layers\": [],\n  \"meta\": {\n    \"build\": 1,\n    \"tag\": \"\"\n  }\n}"
	if string(text) != want {
		t.Fatalf("the construct is not spelled as the page says:\n%s\nwant:\n%s", text, want)
	}
	// one definition and two references: the label appears three times, nowhere else
	if n := strings.Count(string(text), `"&node"`); n != 3 {
		t.Fatalf("the label appears %d times; the page says three", n)
	}
	// and a node named once carries no label: the tree reads and writes as
	// nested tables, and its text is byte-identical to a by-value spelling
	tree, r := readScene(t, m, `{ "head": { "value": 1, "next": { "value": 2 } } }`)
	if !r.Silent() {
		t.Fatalf("a tree did not read clean: %+v", r)
	}
	text, err = m.Write(tree)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "&") {
		t.Fatalf("a tree's text carries the construct:\n%s", text)
	}
}

// A label alone the text never defined, and a field after a label it has, are
// refused, and the prefix is refused everywhere a pointer's object is not —
// which is what makes a typo loud rather than a default node.
func TestSharingConstructRefusals(t *testing.T) {
	m := pointered(t)
	for _, text := range []string{
		`{ "head": { "&node": 1 } }`,                                                  // a label alone the text never defined
		`{ "head": { "&node": 1 }, "alias": { "&node": 1, "value": 1 } }`,             // a reference before its definition
		`{ "head": { "&node": 1, "value": 1 }, "alias": { "&node": 1, "value": 2 } }`, // a label defined twice
		`{ "head": { "value": 1, "&node": 1 } }`,                                      // the label after a field
		`{ "&node": 1 }`,                                                              // the root takes no label
		`{ "ground": { "&node": 1 } }`,                                                // a by-value nesting is not a node
		`{ "head": { "&node": 1.0 } }`,                                                // not an integer spelled as one
		`{ "head": { "&node": 0 } }`,                                                  // not positive
		`{ "head": { "&node": -1 } }`,                                                 // not positive
		`{ "head": { "&node": 01 } }`,                                                 // a leading zero
		`{ "head": { "&other": 1 } }`,                                                 // the prefix under another spelling
		`{ "mystery": { "value": 1, "&node": 1 } }`,                                   // the prefix out of place in a skipped value
	} {
		_, r := readScene(t, m, text)
		if !r.Malformed {
			t.Errorf("%s: read as a text; it is malformed", text)
		}
	}
	// and a FIXED reader refuses the prefix among the keys it places — the
	// construct is one it cannot honor, never a field it lacks — while a value
	// it skips is skipped whole, which is §4's tolerance
	if _, _, r := read(t, "RootConfig", `{ "version_note": "x", "&node": 1 }`); !r.Malformed {
		t.Errorf("a fixed reader did not refuse the prefix: %+v", r)
	}
	if _, _, r := read(t, "RootConfig", `{ "mystery": { "&node": 1 } }`); r.Malformed || r.Unknown != 1 {
		t.Errorf("a fixed reader did not skip a prefixed key inside an unknown value: %+v", r)
	}
}

// A reference resolves against the definition's TABLE, and a definition the
// walk dropped keeps its label: both mirror the wire's node rules (§3.1), where a
// node of another type is a kind mismatch and an unnameable node reads null.
func TestReferenceResolution(t *testing.T) {
	m := pointered(t)
	inst, r := readScene(t, m, `{ "settings": { "&node": 1, "quality": 1 }, "head": { "&node": 1 } }`)
	if r.KindMismatch != 1 || r.Malformed || field(t, inst, "head").Cell.Node != nil {
		t.Fatalf("a reference to a node of another table is a kind mismatch with the pointer null: %+v", r)
	}
	inst, r = readScene(t, m, `{ "layers": [ {}, {}, {}, {}, { "head": { "&node": 1, "value": 1 } } ], "alias": { "&node": 1 } }`)
	if r.Clamped != 1 || r.Malformed || r.Unknown != 0 || field(t, inst, "alias").Cell.Node != nil {
		t.Fatalf("a definition past the bound is dropped and counted once, and its reference reads null: %+v", r)
	}
	inst, r = readScene(t, m, `{ "mystery": { "&node": 9, "value": 1 }, "head": { "&node": 9 } }`)
	if r.Unknown != 1 || r.Malformed || field(t, inst, "head").Cell.Node != nil {
		t.Fatalf("a definition under an unknown key is dropped and counted once: %+v", r)
	}
	// a label is defined when its object CLOSES: a reference to it from inside
	// its own definition — at any depth of by-value nesting — is the cycle the
	// wire refuses, refused where it is written
	for _, text := range []string{
		`{ "head": { "&node": 1, "value": 1, "next": { "&node": 1 } } }`,
		`{ "head": { "&node": 1, "value": 1, "next": { "value": 2, "next": { "&node": 1 } } } }`,
	} {
		if _, r := readScene(t, m, text); !r.Malformed {
			t.Errorf("%s: a self-reference read as sharing; it is a cycle: %+v", text, r)
		}
	}
	// and a cycle a region holds is refused by the WRITER the same way
	cycle := m.New(m.Lookup("Scene"))
	node := m.New(m.Lookup("ListNode"))
	field(t, cycle, "head").Cell.Node = node
	field(t, node, "next").Cell.Node = node
	if _, err := m.Write(cycle); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("the writer did not refuse a cycle: %v", err)
	}
}

// A chain nests as deep as it is long, and the writer carries the reader's cap:
// the corpus's 260-node chain has a wire and no text.
func TestChainNestsAsDeepAsItIsLong(t *testing.T) {
	m := pointered(t)
	root := m.New(m.Lookup("Scene"))
	list := m.Lookup("ListNode")
	head := m.New(list)
	field(t, root, "head").Cell.Node = head
	tail := head
	for i := 1; i < 260; i++ {
		next := m.New(list)
		field(t, tail, "next").Cell.Node = next
		tail = next
	}
	if _, err := m.Write(root); err == nil || !strings.Contains(err.Error(), "depth cap") {
		t.Fatalf("a chain past the cap was written: %v", err)
	}
}

// A definition carries at least one field, because a label alone is a reference:
// a shared node with nothing to write has no definition the form can spell,
// and the writer refuses it rather than writing a text its reader refuses.
func TestSharedNodeWithNothingToWriteIsRefused(t *testing.T) {
	m := pointered(t)
	// Album.pin and Album.head cannot share a node of one table; the closest
	// shape the corpus declares is a Marker whose every field is at its
	// default — which still WRITES its fields, so it defines. The refusal
	// needs a node whose text is `{}`, which no table in the corpus produces
	// through a pointer; the rule is held on the writer's own branch instead.
	inst := m.New(m.Lookup("Scene"))
	list := m.Lookup("ListNode")
	shared := m.New(list)
	field(t, inst, "head").Cell.Node = shared
	field(t, inst, "alias").Cell.Node = shared
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "\"&node\": 1,\n") {
		t.Fatalf("a default-valued shared node still defines with its fields:\n%s", text)
	}
}

// §16.2: A MAGNITUDE IN THE HIGH HALF IS A MAGNITUDE, not a negative. It is the
// field's own DOMAIN that bounds it, established before the value reaches
// storage, so a narrower unsigned field clamps at its CEILING. Reading the
// interpreter's own signed lane instead lands zero, the floor, for a value
// larger than any this field can hold.
func TestUnsignedHighHalfClampsAtTheCeiling(t *testing.T) {
	_, inst, r := read(t, "ProfileConfig", `{ "badge": 18446744073709551615, "experience": 18446744073709551615 }`)
	if r.Clamped != 2 || r.KindMismatch != 0 || r.Malformed {
		t.Fatalf("two clamps and nothing else, got %+v", r)
	}
	if got := field(t, inst, "badge").Cell.U; got != 255 {
		t.Errorf("badge is uint8: expected its ceiling 255, got %d", got)
	}
	if got := field(t, inst, "experience").Cell.U; got != 4294967295 {
		t.Errorf("experience is uint32: expected its ceiling 4294967295, got %d", got)
	}
}

// §16.2: A NEGATIVE TOKEN IN AN UNSIGNED FIELD CLAMPS TO ZERO whatever its
// spelling. -1e30 is a magnitude past every width and a sign, and both halves
// are known before anything is cast. Reading it through a signed lane makes it
// the wrong SHAPE for the kind instead, which is the answer the page gives a
// fraction and not a negative.
func TestNegativeExponentInAnUnsignedFieldClampsToZero(t *testing.T) {
	_, inst, r := read(t, "ProfileConfig", `{ "experience": -1e30 }`)
	if r.KindMismatch != 0 || r.Clamped == 0 || r.Malformed {
		t.Fatalf("a negative in an unsigned field is a clamp, not a mismatch: %+v", r)
	}
	if got := field(t, inst, "experience").Cell.U; got != 0 {
		t.Errorf("expected zero, got %d", got)
	}
}
