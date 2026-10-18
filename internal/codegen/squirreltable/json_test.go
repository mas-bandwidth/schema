package squirreltable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// jsonPrelude drives both JSON surfaces of the conformance harness
// (test/conformance/README.md) and the hostile battery:
//
//	instance <name> <json-write hex> <measure> <json-read hex> <report>
//	    json-write: Load the wire, ToJson (the text, as hex), ToJsonMeasure;
//	    json-read: FromJson the pinned text, Save (the wire, as hex), and the
//	    read's §4 report
//	hostile <case> <report or refused>
//	    FromJson <tree>/<root>.json, the report as u,k,w,c,d,m,verdict, or
//	    `refused` where the read was malformed
//
// The Go side compares every byte (docs/SPEC-TABLES.md §16.5).
const jsonPrelude = `local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local join = function(a) {
  if (a.len() == 0) return "";
  while (a.len() > 1) {
    local b = array((a.len() + 1) / 2);
    for (local i = 0; i < b.len(); i++) b[i] = (2 * i + 1 < a.len()) ? a[2 * i] + a[2 * i + 1] : a[2 * i];
    a = b;
  }
  return a[0];
};
local hexOf = function(b) {
  if (b == null) return "null";
  if (b.len() == 0) return "-";
  local parts = array(b.len());
  for (local i = 0; i < b.len(); i++) parts[i] = HEXB[b[i] & 0xff];
  return join(parts);
};
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
};
local reportLine = function(r) {
  if (r.malformed) return "refused";
  return r.unknown + "," + r.kindMismatch + "," + r.widened + "," + r.clamped + "," +
    r.duplicate + ",false,read";
};
local instance = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local J = schema.require("schema_table_json");
  local v = L.load(T, root, inputs[key + ".wire"], fresh());
  local w = J.ToJson(T, root, v);
  local m = J.ToJsonMeasure(T, root, v);
  local r = fresh();
  local x = J.FromJson(T, root, inputs[key + ".json"], r);
  local b = r.malformed ? null : S.save(T, root, x);
  print("instance " + key + " " + hexOf(w) + " " + m + " " + hexOf(b) + " " + reportLine(r) + "\n");
};
local hostile = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local r = fresh();
  J.FromJson(T, root, inputs[key + ".hostile"], r);
  print("hostile " + key + " " + reportLine(r) + "\n");
};
`

// jsonRows is the manifest's JSON half: every instance row with whether it
// carries a text (a `no-text` row is wire-only by the form's own rule,
// docs/SPEC-TABLES.md §16.7), and every json-hostile row with its verdict.
type jsonRows struct {
	instances []jsonInstance
	hostile   []jsonHostile
}

type jsonInstance struct {
	instanceRow
	noText bool
}

type jsonHostile struct {
	name, unit, root, tree, verdict string
}

func readJsonRows(t *testing.T, path string) jsonRows {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows jsonRows
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		switch {
		case len(fields) >= 5 && fields[0] == "instance":
			in := jsonInstance{instanceRow: instanceRow{name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]}}
			in.noText = len(fields) >= 6 && fields[5] == "no-text"
			rows.instances = append(rows.instances, in)
		case len(fields) >= 6 && fields[0] == "json-hostile":
			rows.hostile = append(rows.hostile, jsonHostile{name: fields[1], unit: fields[2], root: fields[3], tree: fields[4], verdict: fields[5]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// jsonProbe is one run of the probe over the whole corpus: what it printed,
// and what it was asked, so a test can compare and count.
type jsonProbe struct {
	root      string
	rows      jsonRows
	refused   map[string]string // unit -> why the table surface refused it
	instances []jsonInstance    // the rows the probe was asked, with a text
	hostile   []jsonHostile
	got       map[string][]string
}

// runJsonProbe generates every manifest unit an instance or hostile row
// names, writes the probe, and runs it under the pinned interpreter.
func runJsonProbe(t *testing.T) *jsonProbe {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	pr := &jsonProbe{root: root, rows: readJsonRows(t, manifest), refused: map[string]string{}, got: map[string][]string{}}
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, manifest) {
		paths[u.key] = u.paths
	}
	dir := t.TempDir()
	mods := map[string]string{}
	unitFor := func(key string) bool {
		if _, done := mods[key]; done {
			return true
		}
		if _, no := pr.refused[key]; no {
			return false
		}
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("a row names unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			pr.refused[key] = err.Error()
			return false
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mods[key] = squirreltable.ModuleName(u)
		return true
	}
	var p strings.Builder
	p.WriteString(jsonPrelude)
	inputs := map[string][]byte{}
	read := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, in := range pr.rows.instances {
		if in.noText || !unitFor(in.unit) {
			continue
		}
		inputs[in.name+".wire"] = read(in.wire)
		inputs[in.name+".json"] = read("testdata/conformance/tables/json/" + in.name + ".json")
		fmt.Fprintf(&p, "instance(%q, %q, %q, %q);\n", in.unit, mods[in.unit], in.root, in.name)
		pr.instances = append(pr.instances, in)
	}
	for _, h := range pr.rows.hostile {
		if !unitFor(h.unit) {
			continue
		}
		inputs[h.name+".hostile"] = read(h.tree + "/" + h.root + ".json")
		fmt.Fprintf(&p, "hostile(%q, %q, %q, %q);\n", h.unit, mods[h.unit], h.root, h.name)
		pr.hostile = append(pr.hostile, h)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	for _, line := range strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			t.Fatalf("probe line %q is not a row", line)
		}
		pr.got[f[0]+" "+f[1]] = f[2:]
	}
	return pr
}

func unhex(s string) ([]byte, bool) {
	switch s {
	case "null":
		return nil, false
	case "-":
		return []byte{}, true
	}
	b, err := hex.DecodeString(s)
	return b, err == nil
}

// jsonWriteRed names the instances whose ToJson does not equal the pinned
// text byte for byte, or whose measure is not the text's length.
func (pr *jsonProbe) jsonWriteRed(t *testing.T) map[string]string {
	t.Helper()
	red := map[string]string{}
	for _, in := range pr.instances {
		g, ok := pr.got["instance "+in.name]
		if !ok || len(g) != 4 {
			red[in.name] = "the probe printed no row"
			continue
		}
		want, err := os.ReadFile(filepath.Join(pr.root, "testdata", "conformance", "tables", "json", in.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		have, ok := unhex(g[0])
		switch {
		case !ok:
			red[in.name] = "ToJson refused"
		case string(have) != string(want):
			at := 0
			for at < len(have) && at < len(want) && have[at] == want[at] {
				at++
			}
			lo := max(at-40, 0)
			red[in.name] = fmt.Sprintf("ToJson differs at byte %d of %d (want %d)\n got …%q\nwant …%q", at, len(have), len(want),
				string(have[lo:min(at+40, len(have))]), string(want[lo:min(at+40, len(want))]))
		case g[1] != fmt.Sprint(len(have)):
			red[in.name] = fmt.Sprintf("ToJsonMeasure %s, the text is %d bytes", g[1], len(have))
		}
	}
	return red
}

// TestSquirrelJsonAgreesWithTheCorpus is the text form against the corpus
// both ways (docs/SPEC-TABLES.md §16.5): for every instance row the target
// generates and the corpus carries a text for, Load the wire then ToJson
// equals the pinned text byte for byte and ToJsonMeasure is its length
// (json-write), and FromJson the text then Save equals the pinned wire byte
// for byte with a clean report (json-read); and every json-hostile row's
// FromJson gives the manifest's verdict, counter for counter. A unit the
// table surface refuses is named and its rows are not compared; every other
// row is.
func TestSquirrelJsonAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	pr := runJsonProbe(t)
	if len(pr.rows.instances) != 96 {
		t.Fatalf("the manifest names %d instance rows, want 96", len(pr.rows.instances))
	}
	red := pr.jsonWriteRed(t)
	for name, why := range red {
		t.Errorf("json-write %s: %s", name, why)
	}
	writes, reads := 0, 0
	for _, in := range pr.instances {
		if _, bad := red[in.name]; !bad {
			writes++
		}
		g := pr.got["instance "+in.name]
		if len(g) != 4 {
			continue
		}
		want, err := os.ReadFile(filepath.Join(pr.root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		have, ok := unhex(g[2])
		switch {
		case g[3] != "0,0,0,0,0,false,read":
			t.Errorf("json-read %s: FromJson of the pinned text reported %s, want a clean read", in.name, g[3])
		case !ok:
			t.Errorf("json-read %s: Save after FromJson refused", in.name)
		case string(have) != string(want):
			t.Errorf("json-read %s: Save after FromJson wrote %d bytes, the wire holds %d\n got %x\nwant %x", in.name, len(have), len(want), have, want)
		default:
			reads++
		}
	}
	hostile := 0
	for _, h := range pr.hostile {
		g, ok := pr.got["hostile "+h.name]
		switch {
		case !ok:
			t.Errorf("json-hostile %s: the probe printed no row", h.name)
		case g[0] != h.verdict:
			t.Errorf("json-hostile %s: FromJson gave %s, the manifest's verdict is %s", h.name, g[0], h.verdict)
		default:
			hostile++
		}
	}
	noText, refusedRows := 0, 0
	for _, in := range pr.rows.instances {
		if _, no := pr.refused[in.unit]; no {
			refusedRows++
		} else if in.noText {
			noText++
		}
	}
	for unit, why := range pr.refused {
		t.Logf("refused %s: %s", unit, why)
	}
	if len(pr.instances) == 0 || len(pr.hostile) == 0 {
		t.Fatalf("compared %d instances and %d json-hostile rows; the corpus is not reached", len(pr.instances), len(pr.hostile))
	}
	if len(pr.instances)+noText+refusedRows != len(pr.rows.instances) {
		t.Errorf("%d instance rows asked, %d no-text, %d in refused units: %d rows unaccounted for",
			len(pr.instances), noText, refusedRows, len(pr.rows.instances)-len(pr.instances)-noText-refusedRows)
	}
	t.Logf("compared %d instances both ways: json-write %d green, json-read %d green; %d no-text rows, %d rows in %d refused units",
		len(pr.instances), writes, reads, noText, refusedRows, len(pr.refused))
	t.Logf("compared %d of %d json-hostile rows: %d gave the manifest's verdict", len(pr.hostile), len(pr.rows.hostile), hostile)
}

// controlPrelude is the negative control's one probe: keyed_config loaded
// from its wire, one float32 (the first hull's health) set to the float32
// nearest 1/3, which needs eight significant digits to read back, and the
// ToJson text printed as hex.
const controlPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("tabledemo/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local J = schema.require("schema_table_json");
local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
  malformed = false, refused = false };
local v = L.load(T, "KeyedConfig", inputs["keyed_config"], report);
local c = T.Descriptors.KeyedConfig;
local h = T.Descriptors.HullConfig;
v[c.byKey.hulls.member][0][h.byKey.health.member] = 0.3333333432674408;
local w = J.ToJson(T, "KeyedConfig", v);
local out = "";
if (w == null) out = "null";
else for (local i = 0; i < w.len(); i++) out += format("%%02x", w[i] & 0xff);
print(out + "\n");
`

// TestSquirrelJsonNegativeControl patches the emitted json runtime in memory,
// one anchor matched exactly once, so a float32 renders at six significant
// digits whatever its value needs, and requires a json-write comparison to go
// red: ToJson of keyed_config with one float32 set to the float32 nearest 1/3
// against the pinned text with that one value respelled as Go spells it,
// green unpatched and red patched. (No float32 in the corpus's generated
// instances needs more than six digits, so the corpus rows alone cannot see
// this sabotage; the probe value is the one that can.)
func TestSquirrelJsonNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	var paths []string
	for _, u := range manifestUnits(t, manifest) {
		if u.key == "tabledemo" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no tabledemo unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tabledemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(fmt.Sprintf(controlPrelude, squirreltable.ModuleName(unit))))
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "keyed_config.bin"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", "keyed_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	// the pinned text with the one value respelled: Go's shortest float32
	// spelling of the same value, which is C's %.8g here
	third := float32(1.0 / 3.0)
	const was = `"health": 100,`
	if n := strings.Count(string(golden), was); n < 1 {
		t.Fatalf("keyed_config.json carries no %s line to respell", was)
	}
	want := strings.Replace(string(golden), was, `"health": `+strconv.FormatFloat(float64(third), 'g', -1, 32)+`,`, 1)
	inputs := map[string][]byte{"keyed_config": wire}
	text := func() string {
		got := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs)))
		b, ok := unhex(got)
		if !ok {
			t.Fatalf("ToJson refused the probe value: %q", got)
		}
		return string(b)
	}
	if got := text(); got != want {
		t.Fatalf("unpatched ToJson does not equal the respelled pinned text:\n got %q\nwant %q", got, want)
	}

	const anchor = "local low = single ? 6 : 15, high = single ? 9 : 17;"
	runtime := string(squirreltable.Runtime()["schema_table_json.nut"])
	if n := strings.Count(runtime, anchor); n != 1 {
		t.Fatalf("the negative control's anchor appears %d times in the emitted runtime, want exactly once", n)
	}
	patched := strings.Replace(runtime, anchor, "local low = single ? 6 : 15, high = single ? 6 : 17;", 1)
	writeFile(t, filepath.Join(out, "schema_table_json.nut"), []byte(patched))
	got := text()
	if got == want {
		t.Fatalf("a float32 at six significant digits still equals the pinned text; the json-write comparison did not go red")
	}
	at := 0
	for at < len(got) && at < len(want) && got[at] == want[at] {
		at++
	}
	t.Logf("unpatched ToJson equals the pinned text (%d bytes); patched, it differs at byte %d: the json-write comparison goes red", len(want), at)
}
