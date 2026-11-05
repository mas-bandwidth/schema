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

// refuses asserts the read refused the whole load (docs/SPEC-TABLES.md §6.5),
// with a diagnostic that starts with prefix.
func refuses(t testing.TB, r tabletext.Report, prefix string) {
	t.Helper()
	if !r.Refused || !strings.HasPrefix(r.Reason, prefix) {
		t.Fatalf("expected a refusal %q, got %+v", prefix, r)
	}
	if r.Unknown != 0 || r.Widened != 0 {
		t.Fatalf("a refusal carries no counter, got %+v", r)
	}
}

// accepts asserts the read returned the value with nothing counted, and that
// the value writes back out.
func accepts(t testing.TB, m *tabletext.Model, inst *tabletext.Instance, r tabletext.Report) {
	t.Helper()
	if !r.Silent() {
		t.Fatalf("expected an accepted read with nothing counted, got %+v", r)
	}
	if _, err := m.Write(inst); err != nil {
		t.Fatalf("an accepted value does not write: %v", err)
	}
}

// §16.2: unknown keys are skipped and counted; nothing else moves.
func TestUnknownKeyIsCounted(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 90, "mystery": [1,2,3] }`)
	if r.Unknown != 1 || r.Refused {
		t.Fatalf("expected one unknown, got %+v", r)
	}
	if field(t, inst, "tick_rate").Cell.U != 90 {
		t.Fatal("the known key beside it was not placed")
	}
}

// §16.2, §6.5: a repeated key is input no writer produces, and refuses the
// whole read. The negative control names the key once.
func TestDuplicateKeyRefuses(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 90, "tick_rate": 120 }`)
	refuses(t, r, "malformed")
	if field(t, inst, "tick_rate").Cell.U != 60 {
		t.Fatal("a refused read left a value placed")
	}
	m, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 120 }`)
	accepts(t, m, inst, r)
	if field(t, inst, "tick_rate").Cell.U != 120 {
		t.Fatal("the control did not place its value")
	}
}

// §16.2: a key present with the wrong JSON type refuses, NEVER coerced.
func TestWrongTypeRefuses(t *testing.T) {
	_, inst, r := read(t, "GlobalSettings", `{ "tick_rate": "120" }`)
	refuses(t, r, "kind_mismatch")
	if got := field(t, inst, "tick_rate").Cell.U; got != 60 {
		t.Fatalf("the refused instance should hold its declared defaults, got %d", got)
	}
	m, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 120 }`)
	accepts(t, m, inst, r)
}

// §16.2: an integer field given a fraction is the wrong SHAPE for the kind,
// refused, never rounded into place; the integral spelling of it reads.
func TestFractionWhereIntegerDeclared(t *testing.T) {
	_, _, r := read(t, "GlobalSettings", `{ "tick_rate": 90.5 }`)
	refuses(t, r, "kind_mismatch")
	m, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 90.0 }`)
	accepts(t, m, inst, r)
	if field(t, inst, "tick_rate").Cell.U != 90 {
		t.Fatal("an integral spelling did not place")
	}
}

// §16.2: a number outside the declared range refuses; the declared maximum
// itself reads.
func TestOutOfRangeRefuses(t *testing.T) {
	_, _, r := read(t, "GlobalSettings", `{ "tick_rate": 100000 }`)
	refuses(t, r, "out_of_range")
	m, inst, r := read(t, "GlobalSettings", `{ "tick_rate": 240 }`)
	accepts(t, m, inst, r)
	if got := field(t, inst, "tick_rate").Cell.U; got != 240 {
		t.Fatalf("expected the declared max, got %d", got)
	}
}

// §16.2: a string longer than the field refuses, whatever code point crosses
// the bound; a string that fills the field exactly reads.
func TestStringPastItsBoundRefuses(t *testing.T) {
	// callsign is string(24)
	for _, long := range []string{
		strings.Repeat("é", 13),         // two bytes each: 26 bytes
		strings.Repeat("a", 20) + "✓✓X", // the second three-byte code point crosses
		strings.Repeat("a", 25),         // one byte past
	} {
		_, _, r := read(t, "GunnerSettings", `{ "callsign": "`+long+`" }`)
		refuses(t, r, "length_over_bound")
	}
	for _, exact := range []string{strings.Repeat("é", 12), strings.Repeat("a", 20) + "✓"} {
		m, inst, r := read(t, "GunnerSettings", `{ "callsign": "`+exact+`" }`)
		accepts(t, m, inst, r)
		if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, []byte(exact)) {
			t.Fatalf("got %q, want %q", got, exact)
		}
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
		if !r.Refused {
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
	if !r.Refused {
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

// §16.2: `bytes(N)` rides as base64; a body that is not base64 refuses.
func TestBytesBase64(t *testing.T) {
	m, inst, r := read(t, "ProfileConfig", `{ "icon": "AQIDBA==" }`)
	accepts(t, m, inst, r)
	if !bytes.Equal(field(t, inst, "icon").Cell.Str, []byte{1, 2, 3, 4}) {
		t.Fatal("base64 did not decode")
	}
	_, _, r = read(t, "ProfileConfig", `{ "icon": "not base64!" }`)
	if !r.Refused {
		t.Fatalf("a body that is not base64 refuses: %+v", r)
	}
}

// §16.2: a guarded group's guard is an ordinary bool in the text, and the walk
// infers NOTHING from the presence of the group.
func TestGuardsInferNothing(t *testing.T) {
	// Guarded.schema's Patrol, not Tables.schema's ProfileConfig: a guarded
	// branch disqualifies a `fixed table` (docs/SPEC-TABLES.md §2.2, §3.4), so
	// every guard in this corpus lives on a variable table.
	m, inst, r := read(t, "Patrol", `{ "speed": 2.5 }`)
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	if field(t, inst, "active").Cell.B {
		t.Fatal("the walk inferred a guard from the presence of the group")
	}
	// and the writer drops the group while the guard reads false
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(text, []byte(`"speed"`)) {
		t.Fatalf("a guarded-out group should not be written:\n%s", text)
	}
}

// §16.2: an array past the reader's bound refuses; one at the bound reads.
func TestArrayBounds(t *testing.T) {
	_, _, r := read(t, "ShipEntry", `{ "hardpoints": [1,2,3,4,5,6] }`)
	refuses(t, r, "count_over_bound")
	m, inst, r := read(t, "ShipEntry", `{ "hardpoints": [1,2,3,4] }`)
	accepts(t, m, inst, r)
	if got := field(t, inst, "hardpoints").Count; got != 4 {
		t.Fatalf("expected four elements, got %d", got)
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
// whatever the bytes, either the text is refused, or it reads into an instance
// the writer can spell, and the text form reaches a FIXED POINT after one lap
// (the first write normalizes spelling: whitespace, number forms, key order).
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
			if !r.Refused {
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
// and a token that is not a JSON number at all refuses as malformed. Every
// row that is not a value refuses the whole read (§6.5); the reason is
// diagnostic and checked here only as this engine's spelling.
var numberCases = []struct {
	token string
	field string // "experience" uint32, "precision" float64, "timestamp" int64
	want  string // "value=N" | "kind_mismatch" | "out_of_range" | "malformed"
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
	// out of what the field can hold
	{"-1", "experience", "out_of_range"},
	{"99999999999999999999999999", "experience", "out_of_range"},
	// a magnitude no float of that width holds: refused, never an infinity
	{"1e400", "precision", "out_of_range"},
	{"1e1000000", "precision", "out_of_range"},
	{"9e99999", "precision", "out_of_range"},
	{"-1e400", "precision", "out_of_range"},
	{"1e308", "precision", "value=1e+308"},
	{"-9223372036854775808", "timestamp", "value=-9223372036854775808"},
}

func TestNumberGrammarAndSemantics(t *testing.T) {
	for _, tc := range numberCases {
		t.Run(tc.field+"="+tc.token, func(t *testing.T) {
			m, inst, r := read(t, "ProfileConfig", `{ "`+tc.field+`": `+tc.token+` }`)
			switch tc.want {
			case "malformed":
				if !r.Refused {
					t.Fatalf("%q is not a JSON number and must be malformed, got %+v", tc.token, r)
				}
				return
			case "kind_mismatch", "out_of_range":
				refuses(t, r, tc.want)
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

// §16.2: `null` refuses as a kind mismatch for every kind except the two where
// absence is a value — a `?T` reads it as ABSENT, and a pointer reads it as
// null.
func TestNullPerKind(t *testing.T) {
	m, inst, r := read(t, "ShipEntry", `{ "name": "X", "gunner": null }`)
	accepts(t, m, inst, r)
	if field(t, inst, "gunner").Present {
		t.Fatal("null on a ?T left the field present")
	}
	// a repeated key refuses whatever the repeat holds
	_, _, r = read(t, "ShipEntry", `{ "gunner": { "tracking": true }, "gunner": null }`)
	refuses(t, r, "malformed")
	for _, key := range []string{"name", "health", "hardpoints"} {
		_, _, r := read(t, "ShipEntry", `{ "`+key+`": null }`)
		refuses(t, r, "kind_mismatch")
	}
}

// §16.2, §16.3: a lone or out-of-order surrogate escape refuses as malformed
// rather than manufacturing CESU-8 or a U+FFFD the text never spelled, and a
// raw ill-formed byte refuses by name. A surrogate PAIR reads.
func TestIllFormedTextRefusesOnRead(t *testing.T) {
	for _, text := range []string{`"\uD800x"`, `"\uDC00x"`, `"\uDC00\uD800"`} {
		_, _, r := read(t, "GunnerSettings", `{ "callsign": `+text+` }`)
		refuses(t, r, "malformed: string_lone_surrogate")
	}
	m, inst, r := read(t, "GunnerSettings", `{ "callsign": "😀x" }`)
	accepts(t, m, inst, r)
	if got := field(t, inst, "callsign").Cell.Str; !bytes.Equal(got, []byte("\U0001F600x")) {
		t.Fatalf("a surrogate pair must read as its code point, got % x", got)
	}
	_, _, r = read(t, "GunnerSettings", "{ \"callsign\": \"a\xffb\" }")
	refuses(t, r, "string_not_utf8")
}

// §16.2: a `bits(N)` value over its implied [0, 2^N - 1] refuses — the bound
// the declaration implies rather than one it states.
func TestBitsImpliedBound(t *testing.T) {
	m, inst, r := read(t, "WeaponConfig", `{ "channel": 63 }`)
	accepts(t, m, inst, r)
	if field(t, inst, "channel").Cell.U != 63 {
		t.Fatalf("63 fits in bits(6): %+v", r)
	}
	_, _, r = read(t, "WeaponConfig", `{ "channel": 64 }`)
	refuses(t, r, "out_of_range")
}

// A RAW ILL-FORMED BYTE IS REFUSED BY NAME ON READ (§16.3): RFC 8259
// requires a JSON text to be valid UTF-8, so a byte in a string body that is
// not part of a well-formed sequence refuses the read with Reason
// `string_not_utf8`, rather than being silently replaced with U+FFFD.
func TestIllFormedByteRefusesOnRead(t *testing.T) {
	_, _, r := read(t, "GlobalSettings", "{ \"build_note\": \"a\xffb\" }")
	if !r.Refused || !strings.HasPrefix(r.Reason, "string_not_utf8") {
		t.Fatalf("a raw ill-formed byte must refuse the read: %+v", r)
	}
}

// §16.2, §6.5: A REPEATED KEY REFUSES WHATEVER THE REPEAT HOLDS — a value the
// walk would place, or one it would refuse on its own — on a field and on a
// keyed slot alike. The controls name each key once.
func TestRepeatedKeyRefusesWhateverItHolds(t *testing.T) {
	for _, tc := range []struct{ table, repeat, control string }{
		{"ProfileConfig", `{ "tilt": 100, "tilt": 5e-324 }`, `{ "tilt": 100 }`},
		{"ProfileConfig", `{ "experience": 5, "experience": 2.5 }`, `{ "experience": 5 }`},
		{"ProfileConfig", `{ "badge": 9, "badge": 1e309 }`, `{ "badge": 9 }`},
		{"WeaponConfig", `{ "penetration": 3, "penetration": 1.5 }`, `{ "penetration": 3 }`},
		{"ProfileConfig", `{ "tilt": 100, "tilt": 7 }`, `{ "tilt": 7 }`},
		{"PackConfig", `{ "thresholds": { "Hard": 64, "Hard": 0.1 } }`, `{ "thresholds": { "Hard": 64 } }`},
		{"PackConfig", `{ "thresholds": { "Hard": 64, "Hard": 2 } }`, `{ "thresholds": { "Hard": 2 } }`},
	} {
		t.Run(tc.repeat, func(t *testing.T) {
			_, _, r := read(t, tc.table, tc.repeat)
			if !r.Refused {
				t.Fatalf("a repeated key read: %+v", r)
			}
			m, inst, r := read(t, tc.table, tc.control)
			accepts(t, m, inst, r)
		})
	}
}

// A FIELD READS ITS TOKEN THROUGH THE FLOAT64 AND A MAP KEY READS IT EXACTLY,
// and this row is the field half. The exact reader lives on the key path alone
// (§16.2's map row, and the key rows in test/conformance/harness/maps_test.go),
// because a key is an identity that two spellings must not share, while a
// field's value is a quantity read the same way by every port that reads these
// texts. So the field path keeps the interpretation the C, Go and Rust readers
// have: a magnitude past what a float64 holds refuses, and a token at 2^53
// lands the value the mantissa carries.
func TestAnIntegerFieldReadsItsTokenThroughTheFloat(t *testing.T) {
	_, _, r := read(t, "ProfileConfig", `{ "badge": 1e309 }`)
	if !r.Refused {
		t.Fatalf("a magnitude no float64 holds refuses for an integer field: %+v", r)
	}
	// the same spelling in a FLOAT field
	_, _, r = read(t, "ProfileConfig", `{ "precision": 1e400 }`)
	refuses(t, r, "out_of_range")
	// and at 2^53 the field lands what the mantissa carries, where the KEY path
	// lands the digits the token spells
	m, inst, r := read(t, "ProfileConfig", `{ "epoch": 9007199254740993.0 }`)
	accepts(t, m, inst, r)
	if got := field(t, inst, "epoch").Cell.U; got != 9007199254740992 {
		t.Fatalf("the field path rounds at the mantissa: got %d, want 9007199254740992", got)
	}
	// and the decimal spelling of UINT64_MAX rounds UP through the float64,
	// past the domain, so the field refuses where the key path reads the digits
	_, _, r = read(t, "ProfileConfig", `{ "epoch": 18446744073709551615.0 }`)
	refuses(t, r, "out_of_range")
	// the largest float64 below 2^64 is in the domain and reads
	m, inst, r = read(t, "ProfileConfig", `{ "epoch": 18446744073709549568 }`)
	accepts(t, m, inst, r)
	if got := field(t, inst, "epoch").Cell.U; got != 18446744073709549568 {
		t.Fatalf("got %d", got)
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

// §16.2 with #282: a KEYED OBJECT'S KEYS ARE KEYS. A variant named twice
// refuses the read. A key that names NO slot (an unknown variant, or `"None"`)
// is evolution, `unknown` each time, and a repeat of it refuses as any
// repeated key does.
func TestKeyedObjectRepeatRule(t *testing.T) {
	_, _, r := read(t, "PackConfig", `{ "thresholds": { "Hard": 1, "Hard": 2 } }`)
	refuses(t, r, "malformed")
	m, inst, r := read(t, "PackConfig", `{ "ships": { "Frigate": {}, "None": {} } }`)
	if r.Unknown != 2 || r.Refused {
		t.Fatalf("a key naming no slot is unknown, got %+v", r)
	}
	if _, err := m.Write(inst); err != nil {
		t.Fatal(err)
	}
	_, _, r = read(t, "PackConfig", `{ "ships": { "Scout": {}, "Frigate": {}, "Scout": {} } }`)
	refuses(t, r, "malformed")
	m, inst, r = read(t, "PackConfig", `{ "ships": { "Scout": {} } }`)
	accepts(t, m, inst, r)
}

// §16.2: PRESENCE OF THE KEY IS THE PRESENCE, but a `?T` given a value of the
// wrong shape refuses the read; `null` is the absence and `{}` is a present
// value at its defaults.
func TestOptionalWrongShapeRefuses(t *testing.T) {
	_, _, r := read(t, "ShipEntry", `{ "gunner": 5 }`)
	refuses(t, r, "kind_mismatch")
	m, inst, r := read(t, "ShipEntry", `{ "gunner": {} }`)
	accepts(t, m, inst, r)
	if !field(t, inst, "gunner").Present {
		t.Fatal("the key was there, so the field is present")
	}
}

// §16.2: one correctly-rounded conversion at the field's OWN width. Reading a
// float32 through a float64 first rounds twice, and the two roundings part
// company at both ends of the range.
func TestFloat32SingleRounding(t *testing.T) {
	cases := []struct {
		token   string
		want    float32
		refused bool
	}{
		{"340282356779733661637539395458142568447", math.MaxFloat32, false},
		{"-340282356779733661637539395458142568440", -math.MaxFloat32, false},
		{"340282356779733661637539395458142568448", 0, true}, // the midpoint: no float32 holds it
		{"7.0064923216240853547e-46", math.SmallestNonzeroFloat32, false},
		{"-7.0064923216240853547e-46", -math.SmallestNonzeroFloat32, false},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			m, inst, r := read(t, "ProfileConfig", `{ "ratings": [ `+tc.token+`, 0.0, 0.0, 0.0 ] }`)
			if tc.refused {
				refuses(t, r, "out_of_range")
				return
			}
			accepts(t, m, inst, r)
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
		if !r.Refused {
			t.Errorf("%s: read as a text; it is malformed", text)
		}
	}
	// and a FIXED reader refuses the prefix among the keys it places — the
	// construct is one it cannot honor, never a field it lacks — while a value
	// it skips is skipped whole, which is §4's tolerance
	if _, _, r := read(t, "RootConfig", `{ "version_note": "x", "&node": 1 }`); !r.Refused {
		t.Errorf("a fixed reader did not refuse the prefix: %+v", r)
	}
	if _, _, r := read(t, "RootConfig", `{ "mystery": { "&node": 1 } }`); r.Refused || r.Unknown != 1 {
		t.Errorf("a fixed reader did not skip a prefixed key inside an unknown value: %+v", r)
	}
}

// A reference resolves against the definition's TABLE, and a definition the
// walk skipped keeps its label: both mirror the wire's node rules (§3.1), where
// a node of another type refuses and an unnameable node reads null.
func TestReferenceResolution(t *testing.T) {
	m := pointered(t)
	_, r := readScene(t, m, `{ "settings": { "&node": 1, "quality": 1 }, "head": { "&node": 1 } }`)
	refuses(t, r, "kind_mismatch")
	_, r = readScene(t, m, `{ "layers": [ {}, {}, {}, {}, { "head": { "&node": 1, "value": 1 } } ], "alias": { "&node": 1 } }`)
	refuses(t, r, "count_over_bound")
	inst, r := readScene(t, m, `{ "layers": [ { "head": { "&node": 1, "value": 1 } } ], "alias": { "&node": 1 } }`)
	if !r.Silent() || field(t, inst, "alias").Cell.Node == nil {
		t.Fatalf("a definition inside the bound is named by its reference: %+v", r)
	}
	inst, r = readScene(t, m, `{ "mystery": { "&node": 9, "value": 1 }, "head": { "&node": 9 } }`)
	if r.Unknown != 1 || r.Refused || field(t, inst, "head").Cell.Node != nil {
		t.Fatalf("a definition under an unknown key is dropped and counted once: %+v", r)
	}
	// a label is defined when its object CLOSES: a reference to it from inside
	// its own definition — at any depth of by-value nesting — is the cycle the
	// wire refuses, refused where it is written
	for _, text := range []string{
		`{ "head": { "&node": 1, "value": 1, "next": { "&node": 1 } } }`,
		`{ "head": { "&node": 1, "value": 1, "next": { "value": 2, "next": { "&node": 1 } } } }`,
	} {
		if _, r := readScene(t, m, text); !r.Refused {
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
// field's own DOMAIN that bounds it, so a narrower unsigned field refuses it as
// out of range; the field's own ceiling reads.
func TestUnsignedHighHalfIsOutOfRange(t *testing.T) {
	for _, key := range []string{"badge", "experience"} {
		_, _, r := read(t, "ProfileConfig", `{ "`+key+`": 18446744073709551615 }`)
		refuses(t, r, "out_of_range")
	}
	m, inst, r := read(t, "ProfileConfig", `{ "badge": 255, "experience": 4294967295 }`)
	accepts(t, m, inst, r)
	if field(t, inst, "badge").Cell.U != 255 || field(t, inst, "experience").Cell.U != 4294967295 {
		t.Fatal("the ceilings did not place")
	}
}

// §16.2: A NEGATIVE TOKEN IN AN UNSIGNED FIELD IS OUT OF RANGE whatever its
// spelling, never the wrong SHAPE: -1e30 is a magnitude past every width and a
// sign, and both halves are known before anything is cast.
func TestNegativeExponentInAnUnsignedFieldIsOutOfRange(t *testing.T) {
	_, _, r := read(t, "ProfileConfig", `{ "experience": -1e30 }`)
	refuses(t, r, "out_of_range")
	m, inst, r := read(t, "ProfileConfig", `{ "experience": -0e30 }`)
	accepts(t, m, inst, r)
}
