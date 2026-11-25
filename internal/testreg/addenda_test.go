package testreg

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// TestTheSpecAddendaAreCurrent is the drift gate: the committed spec addenda
// are exactly what the registry generates. A hand edit to a page, a registry
// row added without regenerating, a page deleted, and a page left behind are
// each red, naming the page.
func TestTheSpecAddendaAreCurrent(t *testing.T) {
	t.Parallel()
	reg, err := Load("../../" + DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddendaDrift(reg, os.DirFS("../..")); err != nil {
		t.Fatal(err)
	}
}

// addendaFixture is the loader's fixture with an adapter contract on every
// type, which the addenda need.
func addendaFixture(t *testing.T, extra string) *Registry {
	t.Helper()
	reg := load(t, fixture+extra)
	for i := range reg.Types {
		reg.Types[i].Contract = "do the " + reg.Types[i].Name + " thing"
	}
	return reg
}

// pages is the generated pages as a file system rooted where the repository
// root would be, so the drift check reads them as it reads the real tree.
func pages(t *testing.T, reg *Registry) fstest.MapFS {
	t.Helper()
	files, err := Addenda(reg)
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{}
	for _, f := range files {
		fsys[f.Path] = &fstest.MapFile{Data: f.Data}
	}
	if err := AddendaDrift(reg, fsys); err != nil {
		t.Fatalf("the pages just generated are not current: %v", err)
	}
	return fsys
}

func page(t *testing.T, reg *Registry, path string) string {
	t.Helper()
	files, err := Addenda(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == path {
			return string(f.Data)
		}
	}
	t.Fatalf("no page %s", path)
	return ""
}

func wantDrift(t *testing.T, reg *Registry, fsys fstest.MapFS, words ...string) {
	t.Helper()
	err := AddendaDrift(reg, fsys)
	if err == nil {
		t.Fatalf("the drift check passed; want a failure saying %q", words)
	}
	for _, w := range words {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the drift failure does not say %q:\n%v", w, err)
		}
	}
}

// TestAHandEditToAnAddendumIsRed plants a hand edit in one generated page:
// the drift check names the page and the line.
func TestAHandEditToAnAddendumIsRed(t *testing.T) {
	t.Parallel()
	reg := addendaFixture(t, "")
	fsys := pages(t, reg)
	p := TierPage("unit")
	lines := strings.Split(string(fsys[p].Data), "\n")
	lines[3] += " (edited by hand)"
	fsys[p] = &fstest.MapFile{Data: []byte(strings.Join(lines, "\n"))}
	wantDrift(t, reg, fsys, p+":4 differs")
}

// TestARowAddedWithoutRegeneratingIsRed plants a registry row the pages
// were not regenerated for: the drift check names its tier's page.
func TestARowAddedWithoutRegeneratingIsRed(t *testing.T) {
	t.Parallel()
	fsys := pages(t, addendaFixture(t, ""))
	grown := addendaFixture(t, `{"rec":"test","key":"roundtrip.fam.three","type":"roundtrip","family":"fam","case":"three","tier":"functional","checks":"three","inputs":{"n":5},"expect":"same"}`+"\n")
	wantDrift(t, grown, fsys, TierPage("functional")+":", "differs", "go run ./tools/schema-test addenda")
}

// TestAMissingOrLeftoverAddendumIsRed: a generated page deleted is named
// missing, and a page in the addenda's place the registry no longer
// generates (a tier removed) is named too.
func TestAMissingOrLeftoverAddendumIsRed(t *testing.T) {
	t.Parallel()
	reg := addendaFixture(t, "")
	fsys := pages(t, reg)
	delete(fsys, AddendaContract)
	wantDrift(t, reg, fsys, AddendaContract+" is missing")

	fsys = pages(t, reg)
	fsys[AddendaPrefix+"OLD.md"] = &fstest.MapFile{Data: []byte("an old tier\n")}
	wantDrift(t, reg, fsys, AddendaPrefix+"OLD.md is in the addenda's place but the registry no longer generates it")
}

// TestTheAddendaRefuseARowTheSpecCannotBeWrittenFrom: a test with no
// "checks" text, a test with no "expect" and a type with no adapter
// contract each fail generation, by name.
func TestTheAddendaRefuseARowTheSpecCannotBeWrittenFrom(t *testing.T) {
	t.Parallel()
	reg := addendaFixture(t, "")
	reg.Tests[1].Checks = "  "
	reg.Tests[0].Expect = ""
	reg.Types[0].Contract = ""
	_, err := Addenda(reg)
	if err == nil {
		t.Fatal("generated pages from a test with no checks, a test with no expect and a type with no contract")
	}
	for _, w := range []string{
		`test roundtrip.fam.two (registry line 10) says nothing in "checks"`,
		`test roundtrip.fam.one (registry line 9) has no "expect"`,
		`type roundtrip (registry line 6) has no adapter contract`,
	} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal does not say %q:\n%v", w, err)
		}
	}
	// the drift gate refuses the same registry rather than passing it
	if err := AddendaDrift(reg, fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "roundtrip.fam.two") {
		t.Errorf("the drift check did not refuse the registry by name: %v", err)
	}
}

// TestTheAddendaGiveARefusalItsVerdictOnly: a refusal's expected result is
// the verdict; reason text in the row is never written into the spec.
func TestTheAddendaGiveARefusalItsVerdictOnly(t *testing.T) {
	t.Parallel()
	reg := addendaFixture(t, "")
	for i := range reg.Tests {
		if reg.Tests[i].Type == "load-refusal" {
			reg.Tests[i].Expect = "refused with reason wire_damaged"
		}
	}
	p := page(t, reg, TierPage("unit"))
	if strings.Contains(p, "wire_damaged") {
		t.Errorf("the page copies a refusal's reason text:\n%s", p)
	}
	if !strings.Contains(p, "| `L.unit.load-refusal.fam.bad` | bad | `n: 3` | refused on load, in every build (verdict only) |") {
		t.Errorf("the page does not give the refusal its verdict:\n%s", p)
	}
}

// TestTheAddendaNameLargeInputsByWhereTheyLive: an input value too long to
// copy is named by the registry field that holds it.
func TestTheAddendaNameLargeInputsByWhereTheyLive(t *testing.T) {
	t.Parallel()
	reg := addendaFixture(t, "")
	big := strings.Repeat("ab", 100)
	in, err := json.Marshal(map[string]string{"hex": big, "file": "x.bin"})
	if err != nil {
		t.Fatal(err)
	}
	reg.Tests[0].Inputs = in
	p := page(t, reg, TierPage("functional"))
	if strings.Contains(p, big) {
		t.Error("the page copies a large input")
	}
	if !strings.Contains(p, "`file: x.bin`<br>`inputs.hex` (205 bytes, held in the registry row's `inputs.hex`)") {
		t.Errorf("the page does not name the large input by where it lives:\n%s", p)
	}
}

// TestTheAddendaListEveryTestOnItsTierPage: each test's full name is on its
// own tier's page and on no other, every language is on the contract page,
// and an unrepresentable pair is listed with its reason.
func TestTheAddendaListEveryTestOnItsTierPage(t *testing.T) {
	t.Parallel()
	reg := addendaFixture(t, "")
	files, err := Addenda(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(reg.Tiers)+1 {
		t.Fatalf("%d pages for %d tiers", len(files), len(reg.Tiers))
	}
	for _, tt := range reg.Tests {
		lang := "L"
		if !tt.PerLang() {
			lang = LangFree
		}
		name := "`" + lang + "." + tt.Tier + "." + tt.Key + "`"
		for _, f := range files {
			on := strings.Contains(string(f.Data), name)
			if want := f.Path == TierPage(tt.Tier); on != want {
				t.Errorf("%s on %s: %v, want %v", name, f.Path, on, want)
			}
		}
	}
	contract := page(t, reg, AddendaContract)
	for _, l := range reg.Langs {
		if !strings.Contains(contract, "| `"+l.Name+"` | `"+l.Write+"` |") {
			t.Errorf("the contract page does not list language %s", l.Name)
		}
	}
	if !strings.Contains(page(t, reg, TierPage("functional")), "| `roundtrip.fam.two` | `lua` | lua cannot hold it |") {
		t.Error("the unrepresentable pair is not listed on its tier's page")
	}
}

// TestLoadRefusesAContractOnATier: an adapter contract belongs to a type; a
// tier row carrying one is refused where it is written.
func TestLoadRefusesAContractOnATier(t *testing.T) {
	t.Parallel()
	refused(t, strings.Replace(fixture, `"what":"no toolchain"}`, `"what":"no toolchain","contract":"x"}`, 1), "fixture.jsonl:4", `tier unit: a tier has no "contract"`)
}
