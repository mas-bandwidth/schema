package goosetable

// THE JSON TEXT FORM's gates (docs/SPEC-TABLES.md §16; json_form.go and
// runtime/schema_json.goose), every expected answer from the tree:
//
//   - TestGooseJSONHostile: the hostile battery (§16.2, §16.3, §16.5), every
//     json-hostile row of the conformance manifest whose unit the goose
//     target generates and whose root carries the text form, against the
//     manifest's verdict (or the row's uncarried verdict, when goose is not in
//     its carried-by list).
//   - TestGooseJSONRoundTripsTheGoldens: every instance golden text (the
//     third implementation's, testdata/conformance/tables/json) reads CLEAN
//     through the strict FromJson and writes back byte for byte, with
//     ToJsonMeasure equal to the bytes written (§16.1, §16.5).
//   - TestGooseJSONWalkIsOneRuntime (PORTING M9): the walk is emitted byte for
//     byte into every unit that carries the form.
//   - TestGooseJSONWalkNegativeControl: the generated read accessor's field
//     dispatch sabotaged by one ordinal (the C# control's field index) turns
//     exactly the goldens that pass through a RootConfig red and leaves every other green.
//   - TestGooseTextDifferential and its control (PORTING I13): see
//     textdifferential_test.go.
//
// Each test generates into a t.TempDir() and runs the pinned compiler through
// gooserun: no real time, no socket, no network.

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsonManifest = "testdata/conformance/tables/MANIFEST.txt"

// jsonGooseUnits is the manifest's units the goose target generates today
// (`make build/conformance-goose` records every other as its refusal): the
// target's refusal chain lives behind the schema_leg_goose tag
// (compiler/target_goose.go), which this package's tests do not build with,
// so the set is named here and a unit the chain starts carrying joins it.
var jsonGooseUnits = map[string]bool{
	"tabledemo": true, "tblv1": true, "tblv2": true, "tblp1": true, "tblp2": true, "tblp3": true,
	"graphdemo": true, "blockdemo": true, "tblk1": true, "tblk2": true, "vocabdemo": true,
	"vocab9demo": true, "tblrt3": true,
}

// jsonRow is one manifest line a text test reads: a json-hostile row or an
// instance.
type jsonRow struct {
	name, unit, root, path, want string
}

type jsonManifestData struct {
	units     map[string][]string // key -> schema paths
	hostiles  []jsonRow
	instances []jsonRow // path is the golden text; want unused
}

func readJSONManifest(t *testing.T) jsonManifestData {
	t.Helper()
	f, err := os.Open(filepath.Join(fixedRoot, jsonManifest))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := jsonManifestData{units: map[string][]string{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) == 0 {
			continue
		}
		switch w[0] {
		case "unit":
			m.units[w[1]] = w[2:]
		case "json-hostile":
			want := w[5]
			carried := false
			uncarried := ""
			for _, mark := range w[6:] {
				if list, ok := strings.CutPrefix(mark, "carried-by="); ok {
					carried = slices.Contains(strings.Split(list, ","), "goose")
				} else if u, ok := strings.CutPrefix(mark, "uncarried="); ok {
					uncarried = u
				}
			}
			if !carried && uncarried != "" {
				want = uncarried
			}
			m.hostiles = append(m.hostiles, jsonRow{name: w[1], unit: w[2], root: w[3], path: w[4] + "/" + w[3] + ".json", want: want})
		case "instance":
			if slices.Contains(w[5:], "no-text") {
				continue
			}
			m.instances = append(m.instances, jsonRow{name: w[1], unit: w[2], root: w[3], path: "testdata/conformance/tables/json/" + w[1] + ".json"})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

// jsonUnitFiles generates a unit's type and table surfaces, or answers why the
// goose target refuses it.
func jsonUnitFiles(t *testing.T, key string, paths []string) (*ir.Unit, map[string][]byte, error) {
	t.Helper()
	if !jsonGooseUnits[key] {
		return nil, nil, fmt.Errorf("the goose target refuses unit %s", key)
	}
	var abs []string
	for _, p := range paths {
		path := filepath.Join(fixedRoot, filepath.FromSlash(p))
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			found, err := filepath.Glob(filepath.Join(path, "*.schema"))
			if err != nil {
				t.Fatal(err)
			}
			abs = append(abs, found...)
			continue
		}
		abs = append(abs, path)
	}
	u, err := compiler.New().Load(abs)
	if err != nil {
		t.Fatalf("unit %s: load: %v", key, err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: goose type surface: %v", key, err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: goose table surface: %v", key, err)
	}
	maps.Copy(files, tables)
	return u, files, nil
}

// jsonProbe writes the unit and a main.goose into dir. For each (root, file)
// it prints one line: the tolerant read's verdict as the conformance driver
// spells it (`u,k,w,c,d,false,read` or `refused`); with roundTrip, the strict
// read's verdict, the measure, then the text ToJson wrote as hex.
func jsonProbe(t *testing.T, u *ir.Unit, files map[string][]byte, dir string, rows []jsonRow, roundTrip bool) string {
	t.Helper()
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString(`fn verdict(ok: bool, r: const schema_rt::JsonReport&) {
    if ok && !r.malformed { print(r.unknown, ",", r.kind_mismatch, ",", r.widened, ",", r.clamped, ",", r.duplicate, ",false,read"); } else { print("refused"); }
}

`)
	roots := map[string]bool{}
	for _, r := range rows {
		roots[r.root] = true
	}
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		fmt.Fprintf(&b, "fn probe_%s(name: const u8[:]) {\n", root)
		b.WriteString("    var text: u8[>..] = [];\n    if !read_file(name, text) { print(\"read-failed\"); return; }\n")
		fmt.Fprintf(&b, "    var v = %s::New%s();\n    var report = schema_rt::json_report();\n", ns, root)
		if roundTrip {
			fmt.Fprintf(&b, "    let ok = %s::%sFromJson(v, text, report);\n", ns, root)
			b.WriteString("    print(ok);\n")
			fmt.Fprintf(&b, "    print(%s::%sToJsonMeasure(v));\n", ns, root)
			fmt.Fprintf(&b, "    var out: u8[>..] = [];\n    let n = %s::%sToJson(v, out);\n    if n != out.len { print(\"short\"); }\n    hex(out);\n", ns, root)
		} else {
			fmt.Fprintf(&b, "    let ok = %s::%sFromJsonTolerant(v, text, report);\n", ns, root)
			b.WriteString("    verdict(ok, report);\n")
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("fn main() {\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "    probe_%s(\"in%d.json\");\n", r.root, i)
	}
	b.WriteString("}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return "main.goose"
}

func jsonInputs(t *testing.T, rows []jsonRow) map[string][]byte {
	t.Helper()
	in := map[string][]byte{}
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(fixedRoot, filepath.FromSlash(r.path)))
		if err != nil {
			t.Fatal(err)
		}
		in[fmt.Sprintf("in%d.json", i)] = data
	}
	return in
}

// jsonByUnit groups rows by unit and drops the rows whose unit the target
// refuses or whose root carries no text form, saying so: those cases are the
// conformance leg's `absent`.
func jsonByUnit(t *testing.T, m jsonManifestData, rows []jsonRow) (map[string][]jsonRow, map[string]*ir.Unit, map[string]map[string][]byte, int) {
	t.Helper()
	by := map[string][]jsonRow{}
	units := map[string]*ir.Unit{}
	files := map[string]map[string][]byte{}
	absent := 0
	refused := map[string]bool{}
	for _, r := range rows {
		if refused[r.unit] {
			absent++
			continue
		}
		if units[r.unit] == nil {
			u, f, err := jsonUnitFiles(t, r.unit, m.units[r.unit])
			if err != nil {
				t.Logf("absent: unit %s: %v", r.unit, firstLine(err.Error()))
				refused[r.unit] = true
				absent++
				continue
			}
			units[r.unit], files[r.unit] = u, f
		}
		tables, _, _ := jsonCarried(units[r.unit])
		if !tables[r.root] {
			absent++
			continue
		}
		by[r.unit] = append(by[r.unit], r)
	}
	return by, units, files, absent
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 160 {
		line = line[:160] + "..."
	}
	return line
}

func TestGooseJSONHostile(t *testing.T) {
	m := readJSONManifest(t)
	by, units, files, absent := jsonByUnit(t, m, m.hostiles)
	passed := 0
	for _, unit := range slices.Sorted(maps.Keys(by)) {
		rows := by[unit]
		dir := t.TempDir()
		main := jsonProbe(t, units[unit], files[unit], dir, rows, false)
		out := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, main, jsonInputs(t, rows))), "\n"), "\n")
		if len(out) != len(rows) {
			t.Fatalf("%s: %d answers for %d rows:\n%s", unit, len(out), len(rows), strings.Join(out, "\n"))
		}
		for i, r := range rows {
			if out[i] != r.want {
				t.Errorf("json-hostile %s (%s %s): got %s, want %s", r.name, unit, r.root, out[i], r.want)
				continue
			}
			passed++
		}
	}
	if passed == 0 {
		t.Fatal("no json-hostile row was answered: the gate would pass vacuously")
	}
	t.Logf("json-hostile: %d of %d rows pass, %d absent (unit refused or root uncarried)", passed, len(m.hostiles), absent)
}

// jsonRoundTrip runs the golden round trip and answers the failing instance
// names (the strict read not clean, the text not the golden, or the measure
// not the length), in manifest order.
func jsonRoundTrip(t *testing.T, edit func(unit string, files map[string][]byte)) (fails []string, passed, absent int) {
	t.Helper()
	m := readJSONManifest(t)
	by, units, files, absent := jsonByUnit(t, m, m.instances)
	for _, unit := range slices.Sorted(maps.Keys(by)) {
		rows := by[unit]
		f := maps.Clone(files[unit])
		if edit != nil {
			edit(unit, f)
		}
		dir := t.TempDir()
		main := jsonProbe(t, units[unit], f, dir, rows, true)
		inputs := jsonInputs(t, rows)
		out := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, main, inputs)), "\n"), "\n")
		if len(out) != 3*len(rows) {
			t.Fatalf("%s: %d lines for %d instances:\n%s", unit, len(out), len(rows), strings.Join(out, "\n"))
		}
		for i, r := range rows {
			golden := inputs[fmt.Sprintf("in%d.json", i)]
			clean, measure, text := out[3*i], out[3*i+1], out[3*i+2]
			got := gooserun.HexLines(t, []byte(text))[0]
			switch {
			case clean != "true":
				fails = append(fails, r.name+": the strict read is not clean")
			case string(got) != string(golden):
				fails = append(fails, fmt.Sprintf("%s: ToJson is not the golden:\n%s", r.name, firstDiff(got, golden)))
			case measure != fmt.Sprint(len(got)):
				fails = append(fails, fmt.Sprintf("%s: ToJsonMeasure %s, ToJson wrote %d", r.name, measure, len(got)))
			default:
				passed++
			}
		}
	}
	return fails, passed, absent
}

func firstDiff(got, want []byte) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	lo := max(i-60, 0)
	return fmt.Sprintf("at byte %d\n got: %q\nwant: %q", i, got[lo:min(i+60, len(got))], want[lo:min(i+60, len(want))])
}

func TestGooseJSONRoundTripsTheGoldens(t *testing.T) {
	fails, passed, absent := jsonRoundTrip(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if passed == 0 {
		t.Fatal("no golden text was round-tripped: the gate would pass vacuously")
	}
	t.Logf("golden texts: %d round-trip byte for byte through the strict read, %d absent", passed, absent)
}

// jsonWalkSabotage swaps the first two field arms of RootConfig's generated
// read accessor, so `version_note` reads through `weapons`' code and the
// reverse: the reader's defect, in GENERATED code.
func jsonWalkSabotage(t *testing.T, unit string, files map[string][]byte) {
	t.Helper()
	if unit != "tabledemo" {
		return
	}
	src := string(files["TablesTable.goose"])
	head := "fn _RootConfigJsonRead("
	at := strings.Index(src, head)
	if at < 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: no %s in TablesTable.goose", head)
	}
	body := src[at:]
	end := strings.Index(body, "\n}\n")
	fn := body[:end]
	swapped := strings.Replace(strings.Replace(strings.Replace(fn, "if o == 0 {", "if o == @ {", 1), "if o == 1 {", "if o == 0 {", 1), "if o == @ {", "if o == 1 {", 1)
	if swapped == fn {
		t.Fatal("NEGATIVE CONTROL FAILED: the sabotage changed nothing")
	}
	files["TablesTable.goose"] = []byte(src[:at] + swapped + body[end:])
}

func TestGooseJSONWalkNegativeControl(t *testing.T) {
	fails, _, _ := jsonRoundTrip(t, func(unit string, files map[string][]byte) { jsonWalkSabotage(t, unit, files) })
	var red []string
	for _, f := range fails {
		name, _, _ := strings.Cut(f, ":")
		red = append(red, name)
	}
	// every golden whose text passes through a RootConfig: its own two and
	// ArchiveConfig's, which nests one (tables/examples/Nested.schema)
	want := []string{"root_full", "root_default", "archive"}
	if !slices.Equal(red, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: a read accessor with two field arms swapped turned %v red, want exactly %v", red, want)
	}
	t.Logf("json walk negative control: red on exactly %v", red)
}

func TestGooseJSONWalkIsOneRuntime(t *testing.T) {
	m := readJSONManifest(t)
	var first []byte
	firstUnit := ""
	n := 0
	for _, key := range slices.Sorted(maps.Keys(m.units)) {
		u, files, err := jsonUnitFiles(t, key, m.units[key])
		if err != nil {
			continue
		}
		if tables, _, _ := jsonCarried(u); len(tables) == 0 {
			continue
		}
		walk := files["schema_json.goose"]
		if len(walk) == 0 {
			t.Fatalf("unit %s carries the text form but emits no schema_json.goose", key)
		}
		if !strings.Contains(string(walk), "// ---- json walk: begin ----") || !strings.Contains(string(walk), "// ---- json walk: end ----") {
			t.Fatalf("unit %s: the walk's markers are missing", key)
		}
		if first == nil {
			first, firstUnit = walk, key
		} else if string(walk) != string(first) {
			t.Errorf("unit %s's walk differs from unit %s's: the walk is one runtime, byte for byte (PORTING M9)", key, firstUnit)
		}
		n++
	}
	if n < 2 {
		t.Fatalf("only %d unit carries the text form: one walk across units is not shown", n)
	}
	t.Logf("json walk: byte-identical across %d units", n)
}
