package jstable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedPointAndWideTextOnTheTableWireJavaScript is the card's carry
// (docs/SPEC-TABLES.md §2, §3, §4, §15, §16.2; SPEC.md §4.6, §4.12): the
// JavaScript variable form carries fixed/ufixed scalars (the raw integer at
// the declared width, BigInt at 64 and 128 bits) and wstring(N) (a length in
// BYTES, twice the UTF-16 code units, then the units little-endian), as direct
// fields, as union arms, as map values and inside a nested `type`, BYTE FOR
// BYTE against the Go oracle (internal/tablewire, internal/tabletext):
//
//   - the corpus of testdata/conformance/tables/MANIFEST.txt: scalars_full,
//     scalars_default and scalars_edges (tables/scalars SimState), map_text
//     (tables/maps Text, a wstring(6) map value), the widedemo instances
//     (examples-wide Caption and Stamp) and the messagedemo instances
//     (tables/messages ToolMessage), each loaded and saved back, measured,
//     written as text and read back from the text;
//   - a probe unit whose wires and texts the oracle writes here, covering a
//     fixed/ufixed and a wstring as an arm and a map value, a fixed as an
//     optional (the front end refuses ?wstring(N), §15), a
//     surrogate pair at the bound and a nested `type` holding wide text;
//   - the READ REPORT over damaged and over-long payloads (§3, §4): an
//     over-long wstring clamps at a code-point boundary, an unpaired surrogate
//     or a zero unit is malformed and reads the default, and an odd L is
//     framing damage — each counted as the Go leg counts it, and the value the
//     load leaves saved back to the bytes the Go leg saves;
//   - the WRITE refusal (SPEC.md §4.12): a value longer than N code units is
//     refused (Save null, Measure -1) as a field, an arm and a map value.
func TestFixedPointAndWideTextOnTheTableWireJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type unit struct {
		name  string
		unit  *ir.Unit
		cases []*vcCase
	}
	corpus := func(name, root string) *vcCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return &vcCase{Name: name, Root: root, wire: wire, text: text}
	}
	scalars := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "scalars")})
	maps := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "maps")})
	wide := loadUnitPaths(t, []string{filepath.Join(repo, "examples-wide")})
	messages := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "messages")})
	units := []*unit{
		{"scalars", scalars, []*vcCase{
			corpus("scalars_full", "SimState"),
			corpus("scalars_default", "SimState"),
			corpus("scalars_edges", "SimState"),
		}},
		{"mapdemo", maps, []*vcCase{corpus("map_text", "Text")}},
		{"widedemo", wide, []*vcCase{
			corpus("wide_empty", "Caption"),
			corpus("wide_title", "Caption"),
			corpus("wide_sites", "Caption"),
			corpus("wide_narrow_arm", "Caption"),
			corpus("wide_stamp", "Stamp"),
		}},
		{"messagedemo", messages, []*vcCase{
			corpus("message_open", "ToolMessage"),
			corpus("message_transaction", "ToolMessage"),
			corpus("message_ping", "ToolMessage"),
			corpus("message_save", "ToolMessage"),
			corpus("message_empty", "ToolMessage"),
			corpus("message_batch", "ToolMessage"),
			corpus("message_trace", "ToolMessage"),
			corpus("message_arms", "ToolMessage"),
			corpus("message_arm_edits", "ToolMessage"),
			corpus("message_ack", "ToolMessage"),
		}},
	}

	// THE CORPUS IS THE ORACLE'S: decode, re-encode the same bytes, and write
	// the committed text; and every root is carried, none refused by name.
	for _, u := range units {
		vc := newVariableClass(u.unit)
		m := tabletext.NewModel(u.unit)
		for _, c := range u.cases {
			if !vc.carried(c.Root) {
				t.Fatalf("%s: this leg refuses %s: %s", c.Name, c.Root, vc.reason[c.Root])
			}
			inst := m.New(m.Lookup(c.Root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
				t.Fatalf("%s: the oracle did not decode its own wire: %v", c.Name, err)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire (%v)", c.Name, err)
			}
			text, err := m.Write(inst)
			if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
				t.Fatalf("%s: the oracle does not write the committed text (%v)", c.Name, err)
			}
		}
	}

	// THE PROBE UNIT: its wires and texts made by the oracle from the texts.
	probe := unitFrom(t, wideProbeSrc)
	if why := newVariableClass(probe).reason["Probe"]; why != "" {
		t.Fatalf("the probe root is refused: %s", why)
	}
	pm := tabletext.NewModel(probe)
	var probeCases []*vcCase
	for _, pc := range wideProbeTexts {
		inst := pm.New(pm.Lookup("Probe"))
		var rep tabletext.Report
		if !pm.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(pm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		text, err := pm.Write(inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not write: %v", pc.name, err)
		}
		probeCases = append(probeCases, &vcCase{Name: pc.name, Root: "Probe", wire: wire, text: append(text, '\n')})
	}
	units = append(units, &unit{"wideprobe", probe, probeCases})

	// THE DAMAGE CASES: a wire the WIDE writer made, read by the NARROW reader
	// (same table and field names, so the same ids, §5), and forged payloads.
	writer := unitFrom(t, wideDamageWriterSrc)
	reader := unitFrom(t, wideDamageReaderSrc)
	wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
	encodeTag := func(text string) []byte {
		t.Helper()
		inst := wm.New(wm.Lookup("Tag"))
		var rep tabletext.Report
		if !wm.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("the oracle did not read %s cleanly: %+v", text, rep)
		}
		wire, err := tablewire.Encode(wm, inst)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	// patch replaces the ONE occurrence of old in wire, so a forgery names the
	// bytes it changes
	patch := func(wire []byte, old, repl string) []byte {
		t.Helper()
		o, _ := hex.DecodeString(old)
		r, _ := hex.DecodeString(repl)
		if bytes.Count(wire, o) != 1 {
			t.Fatalf("patch %s: %d occurrences in %x", old, bytes.Count(wire, o), wire)
		}
		return bytes.Replace(wire, o, r, 1)
	}
	type damage struct {
		Name   string `json:"name"`
		Module string `json:"module"`
		Root   string `json:"root"`
		Wire   string `json:"wire"`
		Want   string `json:"want"` // the Go leg's report, then its save, hex
		// Damaged is the oracle's malformed: DAMAGE DISCARDS THE WHOLE LOAD
		// (docs/SPEC-TABLES.md §4), so the leg owes the refusal and Want is
		// not compared
		Damaged bool `json:"damaged"`
		bytes   []byte
	}
	damages := []*damage{
		// over-long: clamped at N units, a pair never split (§3)
		{Name: "clamp_plain", bytes: encodeTag(`{"label": "ABCD"}`)},
		{Name: "clamp_pair", bytes: encodeTag(`{"label": "A💩B"}`)},
		{Name: "clamp_arm", bytes: encodeTag(`{"pick": {"wide": "ABCDE"}}`)},
		{Name: "clamp_map_value", bytes: encodeTag(`{"names": {"1": "ABC"}}`)},
		{Name: "clamp_nested", bytes: encodeTag(`{"inner": {"w": "xyzw"}}`)},
		// unpaired surrogates and a zero unit: damage, the load refused (§3, §4)
		{Name: "lone_high", bytes: patch(encodeTag(`{"label": "AB", "seq": 7}`), "41004200", "410000d8")},
		{Name: "lone_low", bytes: patch(encodeTag(`{"label": "AB", "seq": 7}`), "41004200", "00dc4200")},
		{Name: "zero_unit", bytes: patch(encodeTag(`{"label": "AB", "seq": 7}`), "41004200", "41000000")},
		{Name: "lone_arm", bytes: patch(encodeTag(`{"pick": {"wide": "AB"}, "seq": 7}`), "41004200", "410000d8")},
		{Name: "lone_map_value", bytes: patch(encodeTag(`{"names": {"1": "AB"}, "seq": 7}`), "41004200", "410000d8")},
		// an odd L is framing damage on the body that carries it (§3)
		{Name: "odd_nested", bytes: patch(encodeTag(`{"inner": {"w": "ABC"}, "seq": 7}`), "2106410042004300", "2105410042004300")},
	}
	for _, d := range damages {
		inst := rm.New(rm.Lookup("Tag"))
		var rep tabletext.Report
		if _, err := tablewire.Decode(rm, inst, d.bytes, &rep); err != nil {
			t.Fatalf("%s: the oracle refused the forged wire: %v", d.Name, err)
		}
		d.Damaged = rep.Malformed
		save, err := tablewire.Encode(rm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not re-encode: %v", d.Name, err)
		}
		d.Root = "Tag"
		d.Want = strings.Join([]string{
			itoa(int64(rep.Unknown)), itoa(int64(rep.KindMismatch)), itoa(int64(rep.Widened)),
			itoa(int64(rep.Clamped)), itoa(int64(rep.Duplicate)), map[bool]string{true: "true", false: "false"}[rep.Malformed],
		}, ",") + " " + hex.EncodeToString(save)
	}
	// every damage case moves the report: a forgery the reader takes cleanly
	// is a forgery that tests nothing
	for _, d := range damages {
		if strings.HasPrefix(d.Want, "0,0,0,0,0,false ") {
			t.Fatalf("%s: the forgery moved no counter in the oracle: %s", d.Name, d.Want)
		}
	}

	var all []*vcCase
	var want []string
	modules := map[string]string{}
	for _, u := range append(units, &unit{"widereader", reader, nil}) {
		files, err := generateVariableFiles(u.unit, nil)
		if err != nil {
			t.Fatalf("%s: the variable form was refused: %v", u.name, err)
		}
		home := runtimeHome(u.unit) + "Variable.js"
		data, ok := files[home]
		if !ok {
			t.Fatalf("%s: no variable module was generated", u.name)
		}
		module := filepath.Join(work, u.name, home)
		if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(module, data, 0o600); err != nil {
			t.Fatal(err)
		}
		modules[u.name] = module
		for _, c := range u.cases {
			c.Module = module
			c.Wire = filepath.Join(work, c.Name+".wire")
			c.Text = filepath.Join(work, c.Name+".json")
			if err := os.WriteFile(c.Wire, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(c.Text, c.text, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "WIRE "+c.Name+" OK", "MEASURE "+c.Name+" OK", "JSONW "+c.Name+" OK", "JSONR "+c.Name+" OK")
			all = append(all, c)
		}
	}
	for _, d := range damages {
		d.Module = modules["widereader"]
		d.Wire = filepath.Join(work, d.Name+".wire")
		if err := os.WriteFile(d.Wire, d.bytes, 0o600); err != nil {
			t.Fatal(err)
		}
		want = append(want, "DAMAGE "+d.Name+" OK")
	}
	// THE WRITE REFUSAL: past N code units is refused, never truncated
	refusals := []map[string]string{
		{"name": "refuse_field", "set": `v.title = "ABCDEFG";`},
		{"name": "refuse_arm", "set": `v.pick = { arm: "wide", value: "ABCDE" };`},
		{"name": "refuse_map_value", "set": `v.w_map.set(1, "ABCDE");`},
		{"name": "refuse_nested", "set": `v.inner.w = "ABCD";`},
		{"name": "fits_pair", "set": `v.title = "ab\u{1F4A9}cd";`, "fits": "1"},
	}
	for _, r := range refusals {
		r["module"] = modules["wideprobe"]
		want = append(want, "WRITE "+r["name"]+" OK")
	}
	spec, err := json.Marshal(map[string]any{"cases": all, "damages": damages, "refusals": refusals})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(work, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(mapsArmsDriver+wideTextDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, specPath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the wide probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		got[strings.TrimSpace(line)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("the probe did not print %q", w)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	} else {
		t.Logf("probe output (%d expected lines, all present):\n%s", len(want), out)
	}
}

// wideProbeSrc is the probe unit: fixed/ufixed and wstring(N) in every place a
// field line rides on the variable wire.
const wideProbeSrc = `package wideprobe

type Inner
{
    w wstring(3)
}

// a map value cannot carry a range (internal/check refuses one on a map), and
// a fixed(I, F) must declare one (SPEC.md §4.3), so a fixed map value rides in
// a type
type Gauge
{
    level fixed(16, 16)   | min = -100, max = 100
    mass  ufixed(112, 16) | min = 0, max = 1000000
}

union Pick
{
    fx     fixed(16, 16)  | min = -100, max = 100
    ufx    ufixed(48, 16) | min = 0, max = 1000000
    big    fixed(112, 16) | min = -1000000, max = 1000000
    wide   wstring(4)
    narrow string(4)
    off
}

table Probe
{
    title   wstring(6)
    pick    Pick
    picks   [..3]Pick
    fx_map  map[uint8]Gauge
    ufx_map map[string(4)]Gauge
    w_map   map[int16]wstring(4)
    o_fx    ?fixed(32, 32)                | min = -1000, max = 1000
    inner   Inner
    inners  [..2]Inner
    past    int32
}
`

// wideProbeTexts are the probe's instances, as the oracle reads them.
var wideProbeTexts = []struct{ name, text string }{
	{"wide_probe_empty", `{}`},
	{"wide_probe_title", `{"title": "мир", "past": 1}`},
	{"wide_probe_title_pair_at_bound", `{"title": "ab💩cd"}`},
	{"wide_probe_title_escapes", `{"title": "\"\\\n\u0001é"}`},
	{"wide_probe_arm_fx", `{"pick": {"fx": -1.5}}`},
	{"wide_probe_arm_fx_zero", `{"pick": {"fx": 0}}`},
	{"wide_probe_arm_ufx", `{"pick": {"ufx": 12345.25}}`},
	{"wide_probe_arm_big", `{"pick": {"big": -99999.0625}}`},
	{"wide_probe_arm_wide", `{"pick": {"wide": "😀a"}}`},
	{"wide_probe_arm_wide_empty", `{"pick": {"wide": ""}}`},
	{"wide_probe_arm_narrow", `{"pick": {"narrow": "ok"}}`},
	{"wide_probe_arm_array", `{"picks": [{"wide": "中"}, {"fx": 2.25}, {"off": null}]}`},
	{"wide_probe_maps", `{"fx_map": {"2": {"level": -3.25}, "1": {"level": 0.5, "mass": 7.75}}, "ufx_map": {"a": {"mass": 1000.5}, "b": {}}, "w_map": {"-1": "中😀", "3": "", "7": "wxyz"}}`},
	{"wide_probe_optionals_present", `{"o_fx": 0}`},
	{"wide_probe_optionals_values", `{"o_fx": -42.125}`},
	{"wide_probe_nested", `{"inner": {"w": "xyz"}, "inners": [{"w": "a"}, {}]}`},
}

// the damage pair: the WRITER's bounds are wider than the READER's, so a
// payload the writer carries whole is over-long for the reader (§3).
const wideDamageWriterSrc = `package widedamage

type Inner
{
    w wstring(8)
}

union Pick
{
    wide wstring(8)
    off
}

table Tag
{
    label wstring(8)
    pick  Pick
    names map[uint8]wstring(8)
    inner Inner
    seq   uint32
}
`

const wideDamageReaderSrc = `package widedamage

type Inner
{
    w wstring(3)
}

// a map value cannot carry a range (internal/check refuses one on a map), and
// a fixed(I, F) must declare one (SPEC.md §4.3), so a fixed map value rides in
// a type
type Gauge
{
    level fixed(16, 16)   | min = -100, max = 100
    mass  ufixed(112, 16) | min = 0, max = 1000000
}

union Pick
{
    wide wstring(2)
    off
}

table Tag
{
    label wstring(2)
    pick  Pick
    names map[uint8]wstring(2)
    inner Inner
    seq   uint32
}
`

// wideTextDriver runs after mapsArmsDriver over the same spec: the damage
// reports and the write refusals.
const wideTextDriver = `
const hexOf = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
for (const d of spec.damages ?? []) {
  const m = await import(pathToFileURL(d.module).href);
  const rep = new m.TableVariableReport();
  const v = m[d.root + "Load"](new Uint8Array(readFileSync(d.wire)), rep);
  if (d.damaged) { console.log("DAMAGE " + d.name + (rep.malformed ? " OK" : " DIFF the damaged load was not refused " + JSON.stringify(rep))); continue; }
  if (v === null) { console.log("DAMAGE " + d.name + " REFUSED " + JSON.stringify(rep)); continue; }
  const s = m[d.root + "Save"](v);
  const got = [rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, rep.malformed].join(",") + " " + (s === null ? "null" : hexOf(s));
  console.log("DAMAGE " + d.name + (got === d.want ? " OK" : " DIFF got " + got + " want " + d.want));
}
for (const r of spec.refusals ?? []) {
  const m = await import(pathToFileURL(r.module).href);
  const v = new m.Probe();
  new Function("v", r.set)(v);
  const s = m.ProbeSave(v), n = m.ProbeMeasure(v);
  const ok = r.fits ? (s !== null && n === s.length) : (s === null && n === -1);
  console.log("WRITE " + r.name + (ok ? " OK" : " DIFF save " + (s === null ? "null" : hexOf(s)) + " measure " + n));
}
`

// itoa spells a report counter for the probe's expected line (main's lint
// retired the production copy in fixedform.go, which only this test read).
func itoa(v int64) string { return strconv.FormatInt(v, 10) }
