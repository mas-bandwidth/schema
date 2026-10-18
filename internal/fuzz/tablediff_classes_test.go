package fuzz_test

// tablediff_classes_test.go holds the random-schema differential's own
// classifier to account (card schema-go-fuzz-random-table-differential): an
// expected class may file a divergence line only when that line is exactly the
// owed difference and nothing else, every class names a card that exists, and
// the campaign runs only where make fuzz-tables-diff asks for it. These are
// pure unit tests: no toolchain, no gate.

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// verifiedClassCards is every card id an expected class may name, each looked
// up with `nova-sprint card <id>` on 2026-10-18. A class naming anything else
// is owed by nobody.
var verifiedClassCards = map[string]string{
	"schema-tabletext-timestamp-text":      "stream schema-go, landed (attempt 1, head 95e77db21168)",
	"schema-go-plain-float-special-values": "stream schema-go, merging (attempt 7, head 16b49922e447)",
}

func classUnit() *genUnit {
	return &genUnit{Root: &genTable{Name: "Root", Fields: []*genField{
		{Name: "t", Kind: gTimestamp},
		{Name: "b", Kind: gBool},
		{Name: "i", Kind: gInt, Bits: 64},
		{Name: "f", Kind: gFloat64},
		{Name: "s", Kind: gString, StrN: 40},
		{Name: "m", Kind: gMap, MapKey: &genField{Kind: gString, StrN: 40}, Elem: &genField{Name: "v", Kind: gInt, Bits: 32}},
	}}}
}

// maskedBy returns the id of the first expected class that files line, or "".
func maskedBy(u *genUnit, line string) string {
	for _, c := range expectedClasses {
		if c.match(u, line) {
			return c.id
		}
	}
	return ""
}

func pairLine(kind, a, b string) string {
	return kind + " " + base64.StdEncoding.EncodeToString([]byte(a)) + " " + base64.StdEncoding.EncodeToString([]byte(b))
}

func TestTimestampClassMasksOnlyTimestampSpellings(t *testing.T) {
	t.Parallel()
	const ts = "2026-10-15T12:34:05.123456789Z"
	tm, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatal(err)
	}
	n := strconv.FormatInt(tm.UnixNano(), 10)
	n1 := strconv.FormatInt(tm.UnixNano()+1, 10)
	u := classUnit()
	cases := []struct {
		name, leg, eng string
		masked         bool
	}{
		{"timestamp crossing alone", `{"t":"` + ts + `","b":true,"i":5}`, `{"t":` + n + `,"b":true,"i":5}`, true},
		{"timestamp crossing plus a wrong bool", `{"t":"` + ts + `","b":true,"i":5}`, `{"t":` + n + `,"b":false,"i":5}`, false},
		{"timestamp crossing plus a wrong int", `{"t":"` + ts + `","b":true,"i":5}`, `{"t":` + n + `,"b":true,"i":6}`, false},
		{"timestamp crossing plus a missing key", `{"t":"` + ts + `","b":true,"i":5}`, `{"t":` + n + `,"b":true,"x":5}`, false},
		{"a count of another instant", `{"t":"` + ts + `"}`, `{"t":` + n1 + `}`, false},
		{"a string that is no timestamp against a number", `{"t":"abc"}`, `{"t":` + n + `}`, false},
		{"a wrong int alone", `{"i":5}`, `{"i":6}`, false},
		{"identical texts", `{"t":"` + ts + `"}`, `{"t":"` + ts + `"}`, false},
		{"timestamp crossing plus an escape against the literal", `{"t":"` + ts + `","s":"\u00e9"}`, `{"t":` + n + `,"s":"é"}`, false},
		{"timestamp crossing plus reordered keys", `{"t":"` + ts + `","a":1,"b":2}`, `{"b":2,"t":` + n + `,"a":1}`, false},
		{"timestamp crossing plus extra whitespace", `{"t":"` + ts + `","a":1}`, `{"t":` + n + ` , "a":1}`, false},
		{"timestamp crossing plus a collapsed duplicate key", `{"t":"` + ts + `","a":1,"a":2}`, `{"t":` + n + `,"a":2}`, false},
		{"timestamp crossing plus a trailing space", `{"t":"` + ts + `"}`, `{"t":` + n + `} `, false},
		{"the same instant in two offsets", `{"t":"2026-10-15T12:34:05Z"}`, `{"t":"2026-10-15T08:34:05-04:00"}`, true},
		{"a changed timestamp-looking object key", `{"m":{"2026-10-15T12:34:05Z":1}}`, `{"m":{"2026-10-15T08:34:05-04:00":1}}`, false},
		{"a changed timestamp-looking key at the top", `{"2026-10-15T12:34:05Z":1}`, `{"2026-10-15T08:34:05-04:00":1}`, false},
		{"an ordinary string holding a timestamp spelling", `{"s":"2026-10-15T12:34:05Z"}`, `{"s":"2026-10-15T08:34:05-04:00"}`, false},
		{"an int field holding a count against timestamp text", `{"i":` + n + `}`, `{"i":"` + ts + `"}`, false},
	}
	for _, c := range cases {
		got := maskedBy(u, pairLine("engine", c.leg, c.eng))
		if c.masked && got == "" {
			t.Errorf("%s: not masked, want it filed under the timestamp class", c.name)
		}
		if !c.masked && got != "" {
			t.Errorf("%s: masked by %s, want an unexpected divergence", c.name, got)
		}
	}
}

func TestNegativeZeroClassMasksOnlyMinusZeroPairs(t *testing.T) {
	t.Parallel()
	u := classUnit()
	cases := []struct {
		name, a, b string
		masked     bool
	}{
		{"-0 against 0", `{"f":-0,"i":5}`, `{"f":0,"i":5}`, true},
		{"-0 against 0 in a list", `{"f":[1.5,-0,2]}`, `{"f":[1.5,0,2]}`, true},
		{"64-bit int off by one", `{"i":9223372036854775807}`, `{"i":9223372036854775806}`, false},
		{"2^53+1 against 2^53", `{"i":9007199254740993}`, `{"i":9007199254740992}`, false},
		{"reordered keys", `{"f":0,"i":5}`, `{"i":5,"f":0}`, false},
		{"1e2 against 100", `{"f":1e2}`, `{"f":100}`, false},
		{"an escape against the literal character", "{\"s\":\"\\u00e9\"}", `{"s":"é"}`, false},
		{"identical texts", `{"f":-0}`, `{"f":-0}`, false},
		{"a trailing space", `{"f":1}`, `{"f":1} `, false},
		{"-0 pair plus a trailing space", `{"f":-0}`, `{"f":0} `, false},
		{"sign loss on a nonzero float", `{"f":-0.5}`, `{"f":0.5}`, false},
		{"-0 pair plus a wrong int", `{"f":-0,"i":5}`, `{"f":0,"i":6}`, false},
		{"\"-0\" inside a string", `{"s":"-0"}`, `{"s":"0"}`, false},
		{"a -0 lost on one side and invented on the other", `[[-0],[0]]`, `[[0],[-0]]`, false},
		{"two top-level values", `-0 1`, `0 1`, false},
	}
	for _, c := range cases {
		for _, kind := range []string{"roundtrip", "engine"} {
			got := maskedBy(u, pairLine(kind, c.a, c.b))
			if c.masked && got == "" {
				t.Errorf("%s (%s line): not masked, want it filed under the -0 class", c.name, kind)
			}
			if !c.masked && got != "" {
				t.Errorf("%s (%s line): masked by %s, want an unexpected divergence", c.name, kind, got)
			}
		}
	}
}

// TestClassesSeeKindsInInnerTablesAndUnions: a float or timestamp held only
// inside an inner table, behind a pointer or in a union arm still makes the
// unit one that holds it (seed 184 at 0..200: -0 in an inner table's field).
func TestClassesSeeKindsInInnerTablesAndUnions(t *testing.T) {
	t.Parallel()
	inner := &genTable{Name: "Inner", Fields: []*genField{{Name: "g0", Kind: gFloat64}}}
	stamp := &genTable{Name: "Stamp", Fields: []*genField{{Name: "g0", Kind: gTimestamp}}}
	un := &genUnion{Name: "U", Arms: []*genField{{Name: "a", Kind: gPointer, NestedName: "Stamp"}}}
	nested := &genUnit{Inner: []*genTable{inner, stamp}, Unions: []*genUnion{un}, Root: &genTable{Name: "Root", Fields: []*genField{
		{Name: "f0", Kind: gNested, NestedName: "Inner"},
		{Name: "u1", Kind: gUnion, UnionName: "U"},
	}}}
	if !nested.hasFloat() {
		t.Errorf("a float64 inside an inner table is not seen by hasFloat")
	}
	if !nested.hasTimestamp() {
		t.Errorf("a timestamp inside a union arm's table is not seen by hasTimestamp")
	}
	if got := maskedBy(nested, pairLine("roundtrip", `{"f0":{"g0":-0}}`, `{"f0":{"g0":0}}`)); got == "" {
		t.Errorf("-0 against 0 in an inner table's float is not masked")
	}
	plain := &genUnit{Root: &genTable{Name: "Root", Fields: []*genField{{Name: "i", Kind: gInt, Bits: 32}}}}
	if plain.hasFloat() || plain.hasTimestamp() {
		t.Errorf("a unit with one int field holds a float or a timestamp")
	}
}

// TestTimestampClassFollowsTypedPaths: the timestamp class masks a leaf only
// at a path the schema types as timestamp, through inner tables, optionals,
// pointers, arrays, map values and union arms.
func TestTimestampClassFollowsTypedPaths(t *testing.T) {
	t.Parallel()
	const a, b = "2026-10-15T12:34:05Z", "2026-10-15T08:34:05-04:00"
	inner := &genTable{Name: "Inner", Fields: []*genField{{Name: "t", Kind: gTimestamp}, {Name: "s", Kind: gString, StrN: 40}}}
	un := &genUnion{Name: "U", Arms: []*genField{{Name: "p", Kind: gPointer, Elem: &genField{Kind: gNested, NestedName: "Inner"}}, {Name: "n", Kind: gInt, Bits: 32}}}
	u := &genUnit{Inner: []*genTable{inner}, Unions: []*genUnion{un}, Root: &genTable{Name: "Root", Fields: []*genField{
		{Name: "x", Kind: gNested, NestedName: "Inner"},
		{Name: "o", Kind: gOptScalar, Elem: &genField{Kind: gNested, NestedName: "Inner"}},
		{Name: "l", Kind: gOpenArray, Elem: &genField{Kind: gTimestamp}},
		{Name: "m", Kind: gMap, MapKey: &genField{Kind: gString, StrN: 40}, Elem: &genField{Kind: gNested, NestedName: "Inner"}},
		{Name: "u", Kind: gUnion, UnionName: "U"},
	}}}
	cases := []struct {
		name, leg, eng string
		masked         bool
	}{
		{"inner table timestamp", `{"x":{"t":"` + a + `"}}`, `{"x":{"t":"` + b + `"}}`, true},
		{"inner table string", `{"x":{"s":"` + a + `"}}`, `{"x":{"s":"` + b + `"}}`, false},
		{"optional inner table timestamp", `{"o":{"t":"` + a + `"}}`, `{"o":{"t":"` + b + `"}}`, true},
		{"list of timestamps", `{"l":["` + a + `","` + a + `"]}`, `{"l":["` + a + `","` + b + `"]}`, true},
		{"map value table timestamp", `{"m":{"k":{"t":"` + a + `"}}}`, `{"m":{"k":{"t":"` + b + `"}}}`, true},
		{"map key changed", `{"m":{"` + a + `":{"t":"` + a + `"}}}`, `{"m":{"` + b + `":{"t":"` + a + `"}}}`, false},
		{"union arm through a pointer", `{"u":{"p":{"t":"` + a + `"}}}`, `{"u":{"p":{"t":"` + b + `"}}}`, true},
		{"union int arm", `{"u":{"n":"` + a + `"}}`, `{"u":{"n":"` + b + `"}}`, false},
		{"a field the schema does not have", `{"zz":"` + a + `"}`, `{"zz":"` + b + `"}`, false},
	}
	for _, c := range cases {
		got := maskedBy(u, pairLine("engine", c.leg, c.eng))
		if c.masked && got == "" {
			t.Errorf("%s: not masked, want it filed under the timestamp class", c.name)
		}
		if !c.masked && got != "" {
			t.Errorf("%s: masked by %s, want an unexpected divergence", c.name, got)
		}
	}
}

func TestExpectedClassesNameRealCards(t *testing.T) {
	t.Parallel()
	for _, c := range expectedClasses {
		if _, ok := verifiedClassCards[c.id]; !ok {
			t.Errorf("expected class %q names no card found by nova-sprint card", c.id)
		}
	}
	// A class may match only a line kind the probe records: the probe never
	// writes "wrongvalue", so a class matching it is dead.
	if strings.Contains(tableDifferentialProbe, `"wrongvalue`) {
		t.Fatalf("the probe records wrongvalue lines now; update this test")
	}
	u := classUnit()
	if id := maskedBy(u, "wrongvalue 3"); id != "" {
		t.Errorf("class %s matches a wrongvalue line the probe never records", id)
	}
}

func TestEngineCheckMaskedTextStillPacks(t *testing.T) {
	t.Parallel()
	const ts = "2026-10-15T12:34:05.123456789Z"
	tm, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatal(err)
	}
	leg := []byte(`{"t":"` + ts + `"}`)
	eng := []byte(`{"t":` + strconv.FormatInt(tm.UnixNano(), 10) + `}`)
	wire := []byte{1, 2, 3}
	packed := false
	pack := func(legJSON []byte) ([]byte, tabletext.Report, error) {
		packed = true
		return []byte{9, 9, 9}, tabletext.Report{}, nil
	}
	unexpected, expected := engineCheck(classUnit(), 0, 0, wire, leg, eng, pack)
	if len(expected) != 1 {
		t.Errorf("the timestamp text difference was not reported as owed: %v", expected)
	}
	if !packed {
		t.Fatalf("an owed text difference skipped the Pack direction")
	}
	if !strings.Contains(strings.Join(unexpected, "\n"), "engine wire differs from leg wire") {
		t.Errorf("a wrong Pack wire behind an owed text difference was not failed: %v", unexpected)
	}
}

func TestTableDiffRunsOnlyWhenOptedIn(t *testing.T) {
	t.Parallel()
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	if tableDiffOptedIn(env("SCHEMA_SLOW", "1")) {
		t.Errorf("SCHEMA_SLOW=1 alone (ci-full) runs the table differential")
	}
	if tableDiffOptedIn(env("SCHEMA_REQUIRE_CORPUS", "1")) {
		t.Errorf("SCHEMA_REQUIRE_CORPUS alone runs the table differential")
	}
	if !tableDiffOptedIn(env("SCHEMA_SLOW", "1", "SCHEMA_TABLE_DIFF", "1")) {
		t.Errorf("SCHEMA_TABLE_DIFF=1 does not run the table differential")
	}
}

// TestCiFullKeepsFuzzAndOnlyMakeOptsIn reads the workflow and make files: ci-full
// runs ./internal/fuzz (crasher corpus, property and oracle tests), and the one
// line that sets SCHEMA_TABLE_DIFF=1 anywhere is the fuzz-tables-diff recipe.
func TestCiFullKeepsFuzzAndOnlyMakeOptsIn(t *testing.T) {
	t.Parallel()
	ci, err := os.ReadFile("../../.github/workflows/ci-full.yml")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(ci), "\n") {
		if strings.Contains(line, "run: go test $(go list ./...") && strings.Contains(line, "grep -v '/internal/codegen/'") {
			found = true
			if strings.Contains(line, "/internal/fuzz") {
				t.Errorf("ci-full's unit-rest go test drops ./internal/fuzz: %s", strings.TrimSpace(line))
			}
		}
	}
	if !found {
		t.Errorf("ci-full.yml has no unit-rest go test over the non-emitter packages")
	}
	files := []string{"../../.github/workflows/ci-full.yml", "../../.github/workflows/ci-fast.yml", "../../.github/workflows/certify.yml", "../../Makefile"}
	mk, err := os.ReadDir("../../make")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range mk {
		if strings.HasSuffix(e.Name(), ".mk") {
			files = append(files, "../../make/"+e.Name())
		}
	}
	setters := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		inTarget := false
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "\t") {
				inTarget = strings.HasPrefix(line, "fuzz-tables-diff:")
			}
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue // a comment sets nothing
			}
			if strings.Contains(line, "SCHEMA_TABLE_DIFF:") || strings.Contains(line, "SCHEMA_TABLE_DIFF=1") {
				if !(f == "../../make/go.mk" && inTarget) {
					t.Errorf("%s sets SCHEMA_TABLE_DIFF outside make fuzz-tables-diff: %s", f, strings.TrimSpace(line))
					continue
				}
				setters++
			}
		}
	}
	if setters != 2 {
		t.Errorf("make fuzz-tables-diff sets SCHEMA_TABLE_DIFF=1 on %d recipe lines, want 2 (the seeds and the negative control)", setters)
	}
}
