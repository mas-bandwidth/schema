package testreg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/languages"
)

// fixture is a small registry: three languages (one with no adapter), two
// tiers, two types, three tests of one family, one unrepresentable pair and
// one language-free test.
const fixture = `{"rec":"lang","name":"go","write":"refuse","adapter":"fake"}
{"rec":"lang","name":"lua","write":"refuse","adapter":"fake"}
{"rec":"lang","name":"c","write":"assert","adapter":""}
{"rec":"tier","name":"unit","what":"no toolchain"}
{"rec":"tier","name":"functional","what":"a toolchain"}
{"rec":"type","name":"roundtrip","what":"same bytes back"}
{"rec":"type","name":"load-refusal","what":"refused"}
{"rec":"type","name":"tooling","what":"harness self-tests"}
{"rec":"test","key":"roundtrip.fam.one","type":"roundtrip","family":"fam","case":"one","tier":"functional","checks":"one","inputs":{"n":1},"expect":"same"}
{"rec":"test","key":"roundtrip.fam.two","type":"roundtrip","family":"fam","case":"two","tier":"functional","checks":"two","inputs":{"n":2},"expect":"same"}
{"rec":"test","key":"load-refusal.fam.bad","type":"load-refusal","family":"fam","case":"bad","tier":"unit","checks":"bad","inputs":{"n":3},"expect":"refused"}
{"rec":"test","key":"tooling.self.check","type":"tooling","family":"self","case":"check","tier":"unit","checks":"self","inputs":{"n":4},"expect":"passes","scope":"none","adapter":"fake"}
{"rec":"unrepresentable","test":"roundtrip.fam.two","lang":"lua","reason":"lua cannot hold it"}
{"rec":"group","name":"pr","what":"the ready languages","query":{"tiers":["unit","functional"],"ready":true}}
`

func load(t *testing.T, text string) *Registry {
	t.Helper()
	reg, err := Parse("fixture.jsonl", strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// refused parses text and requires a refusal that names the file, the line
// and the words.
func refused(t *testing.T, text, where string, words ...string) {
	t.Helper()
	_, err := Parse("fixture.jsonl", strings.NewReader(text))
	if err == nil {
		t.Fatalf("accepted; want a refusal at %s", where)
	}
	for _, w := range append([]string{where}, words...) {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("refusal does not say %q:\n%v", w, err)
		}
	}
}

func TestLoadAcceptsTheFixture(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	if len(reg.Langs) != 3 || len(reg.Tests) != 4 || len(reg.Groups) != 1 {
		t.Fatalf("%d languages, %d tests, %d groups", len(reg.Langs), len(reg.Tests), len(reg.Groups))
	}
	// 3 per-language tests x 3 languages, less one unrepresentable pair,
	// plus one language-free test
	if n := len(reg.Instances()); n != 3*3-1+1 {
		t.Fatalf("%d instances, want 9", n)
	}
}

func TestLoadRefusesADuplicateKey(t *testing.T) {
	t.Parallel()
	dup := `{"rec":"test","key":"roundtrip.fam.one","type":"roundtrip","family":"fam","case":"one","tier":"unit","checks":"again","inputs":{"n":9},"expect":"same"}` + "\n"
	refused(t, fixture+dup, "fixture.jsonl:15:", "roundtrip.fam.one is already declared on line 9")
}

func TestLoadRefusesAnUnknownType(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"test","key":"decode.fam.x","type":"decode","family":"fam","case":"x","tier":"unit","checks":"x","inputs":{"n":9},"expect":"x"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", `type "decode" is not declared`)
}

func TestLoadRefusesAnUnknownTier(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"test","key":"roundtrip.fam.x","type":"roundtrip","family":"fam","case":"x","tier":"weekly","checks":"x","inputs":{"n":9},"expect":"x"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", `tier "weekly" is not declared`)
}

func TestLoadRefusesUnrepresentableForATestThatDoesNotExist(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"unrepresentable","test":"roundtrip.fam.nine","lang":"go","reason":"r"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", `test "roundtrip.fam.nine" does not exist`)
}

func TestLoadRefusesUnrepresentableForALanguageThatDoesNotExist(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"unrepresentable","test":"roundtrip.fam.one","lang":"cobol","reason":"r"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", `language "cobol" does not exist`)
}

func TestLoadRefusesAGroupThatMatchesNothing(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"group","name":"none-such","what":"w","query":{"names":["c.unit.*"],"ready":true}}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "matches 0 tests")
}

func TestLoadRefusesAKeyThatIsNotTypeFamilyCase(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"test","key":"roundtrip.other.one","type":"roundtrip","family":"fam","case":"one","tier":"unit","checks":"x","inputs":{"n":9},"expect":"x"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "key is not type.family.case")
}

func TestLoadRefusesNegativeSeconds(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"test","key":"roundtrip.fam.x","type":"roundtrip","family":"fam","case":"x","tier":"unit","checks":"x","inputs":{"n":9},"expect":"x","seconds":-1}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "seconds -1 is negative")
}

// TestWriteSecondsKeepsEveryOtherByte: measured seconds land on their test's
// line, replacing an earlier measure and rounding up to the hundredth; the
// rest of the file is as it was; a key with no test line writes nothing.
func TestWriteSecondsKeepsEveryOtherByte(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "r.jsonl")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range []float64{3.001, 0.5} {
		if n, err := WriteSeconds(path, map[string]float64{"roundtrip.fam.two": s}, nil); err != nil || n != 1 {
			t.Fatalf("WriteSeconds: %d %v", n, err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(fixture, `"expect":"same"}`+"\n"+`{"rec":"test","key":"load-refusal`, `"expect":"same","seconds":0.5}`+"\n"+`{"rec":"test","key":"load-refusal`, 1)
	if string(got) != want {
		t.Fatalf("after WriteSeconds:\n%s\nwant\n%s", got, want)
	}
	reg := load(t, string(got))
	if tt, _ := reg.Test("roundtrip.fam.two"); tt.Seconds != 0.5 {
		t.Fatalf("seconds read back as %v", tt.Seconds)
	}
	if _, err := WriteSeconds(path, map[string]float64{"roundtrip.fam.nine": 1}, nil); err == nil || !strings.Contains(err.Error(), "no test line for roundtrip.fam.nine") {
		t.Fatalf("a key with no line: %v", err)
	}
}

// TestWriteSecondsTouchesOnlyTheLastField: a "seconds" key inside a row's
// inputs is the row's data, not its measure: WriteSeconds sets and replaces
// only the line's last field. The plant is a match anywhere on the line,
// which takes the inputs' key away.
func TestWriteSecondsTouchesOnlyTheLastField(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "r.jsonl")
	const line = `{"rec":"test","key":"tooling.a.b","inputs":{"x":1,"seconds":7},"expect":"e"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range []float64{2, 3} {
		if n, err := WriteSeconds(path, map[string]float64{"tooling.a.b": s}, nil); err != nil || n != 1 {
			t.Fatalf("WriteSeconds: %d %v", n, err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"rec":"test","key":"tooling.a.b","inputs":{"x":1,"seconds":7},"expect":"e","seconds":3}` + "\n"; string(got) != want {
		t.Fatalf("after WriteSeconds:\n%s\nwant\n%s", got, want)
	}
}

// TestTheRegistryLoads holds the committed registry to Load, its language
// rows to the generators this build registers (internal/languages: a
// generator with no row, or a row with no generator, is red), and each
// stored query to a non-empty selection.
func TestTheRegistryLoads(t *testing.T) {
	t.Parallel()
	reg, err := Load("../../" + DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, l := range languages.All() {
		want = append(want, l.Target)
	}
	got := reg.LangNames()
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("the registry's languages are %v; the generators are %v", got, want)
	}
	for _, g := range reg.Groups {
		if _, err := reg.Select(g.Query()); err != nil {
			t.Errorf("group %s: %v", g.Name, err)
		}
	}
	for _, name := range []string{"pr", "nightly"} {
		if _, ok := reg.Group(name); !ok {
			t.Errorf("the registry has no group %q, which the workflows run", name)
		}
	}
}

// TestLoadRefusesABadNeed: a need is a lowercase machine property named
// once; a row needing an empty, upper-case or repeated property is refused
// by line, and a row needing casefold loads with the need on it.
func TestLoadRefusesABadNeed(t *testing.T) {
	t.Parallel()
	row := func(needs string) string {
		return `{"rec":"test","key":"roundtrip.fam.x","type":"roundtrip","family":"fam","case":"x","tier":"unit","checks":"x","inputs":{"n":9},"expect":"x","needs":` + needs + "}\n"
	}
	for _, bad := range []string{`[""]`, `["CaseFold"]`, `["casefold","casefold"]`, `["case fold"]`} {
		refused(t, fixture+row(bad), "fixture.jsonl:15:", "each need is a lowercase machine property, named once")
	}
	reg := load(t, fixture+row(`["casefold"]`))
	if got, ok := reg.Test("roundtrip.fam.x"); !ok || !slices.Equal(got.Needs, []string{"casefold"}) {
		t.Fatalf("a row needing casefold: %+v", got)
	}
}
