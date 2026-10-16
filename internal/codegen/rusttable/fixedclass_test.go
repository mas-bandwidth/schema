package rusttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedClassOnTableWireRust is the card's carry: a FIXED TABLE rides the
// VARIABLE FORM too (docs/SPEC-TABLES.md §2.2, §3, §3.1, §4.2), so a closure
// that holds a `fixed table` gets the form-1 codec — load, save and the text
// verbs — BYTE FOR BYTE against the Go oracle (internal/tablewire and
// internal/tabletext; internal/codegen/gotable/wire.go is the Go leg's emitter
// of the same bytes). Form 3 (§3.4) still stands beside it.
//
// The cases are the conformance corpus's three evolution pairs, each a
// closure holding a fixed table:
//
//   - tblv1/tblv2 (test/tables/V1.schema, V2.schema): a fixed root with a
//     nested `type`, fixed-length, bounded and enum-keyed arrays, a union whose
//     arms are `type`s, and optionals of a scalar, an enum and a `type`;
//   - tblw1/tblw2 (W1.schema, W2.schema): string, bytes and FLAGS defaults on a
//     fixed table reached by pointer and by value, and the table renamed
//     Vessel -> Ship under `was` (§5), so every node record keeps its type id;
//   - tblr1/tblr2 (R1.schema, R2.schema): the vocabulary renames — an enum
//     variant, two union arms (one payload-free) and a `type`'s field — each
//     under `was`, so the cross reads are silent (§5);
//
// and a probe unit of `fixed table` roots carrying a string, a bytes and a
// flags default, elided and written, so a dropped default is a red subtest.
//
// Every instance is loaded and saved back (bytes unmoved, report clean),
// written as text (the oracle's text), and read back from text and saved (the
// wire again). Every `report` row over the three pairs prints the six counters
// and the verdict the Go oracle's decode gives (MANIFEST.txt's report rows).
func TestFixedClassOnTableWireRust(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type fcCase struct {
		name, root string
		wire, text []byte
	}
	type fcReport struct {
		name, root string
		wire       []byte
		want       string
	}
	type fcUnit struct {
		module  string
		unit    *ir.Unit
		cases   []fcCase
		reports []fcReport
	}
	corpus := func(name, root string) fcCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return fcCase{name: name, root: root, wire: wire, text: text}
	}
	report := func(name, root, file string) fcReport {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", file+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return fcReport{name: name, root: root, wire: wire}
	}
	units := []*fcUnit{
		{module: "tblv1", unit: fcLoadFile(t, repo, "test/tables/V1.schema"),
			cases:   []fcCase{corpus("v1_cfg", "Cfg"), corpus("v1_seams", "Cfg")},
			reports: []fcReport{report("v2_cfg_as_v1", "Cfg", "v2_cfg"), report("v2_seams_as_v1", "Cfg", "v2_seams")}},
		{module: "tblv2", unit: fcLoadFile(t, repo, "test/tables/V2.schema"),
			cases:   []fcCase{corpus("v2_cfg", "Cfg"), corpus("v2_seams", "Cfg")},
			reports: []fcReport{report("v1_cfg_as_v2", "Cfg", "v1_cfg"), report("v1_seams_as_v2", "Cfg", "v1_seams")}},
		{module: "tblw1", unit: fcLoadFile(t, repo, "test/tables/W1.schema"),
			cases:   []fcCase{corpus("w1_fleet", "Fleet"), corpus("w1_fleet_default", "Fleet")},
			reports: []fcReport{report("w2_fleet_as_w1", "Fleet", "w2_fleet")}},
		{module: "tblw2", unit: fcLoadFile(t, repo, "test/tables/W2.schema"),
			cases:   []fcCase{corpus("w2_fleet", "Fleet")},
			reports: []fcReport{report("w1_fleet_as_w2", "Fleet", "w1_fleet"), report("w1_fleet_default_as_w2", "Fleet", "w1_fleet_default")}},
		{module: "tblr1", unit: fcLoadFile(t, repo, "test/tables/R1.schema"),
			cases:   []fcCase{corpus("r1_cfg", "Cfg"), corpus("r1_ping", "Cfg")},
			reports: []fcReport{report("r2_cfg_as_r1", "Cfg", "r2_cfg")}},
		{module: "tblr2", unit: fcLoadFile(t, repo, "test/tables/R2.schema"),
			cases:   []fcCase{corpus("r2_cfg", "Cfg")},
			reports: []fcReport{report("r1_cfg_as_r2", "Cfg", "r1_cfg"), report("r1_ping_as_r2", "Cfg", "r1_ping")}},
	}

	// THE PROBE UNIT: fixed table roots whose string, bytes and flags defaults
	// the storage carries and the writer elides against (§3, §4.2).
	probe := unitFrom(t, fixedClassProbeSrc)
	pm := tabletext.NewModel(probe)
	pu := &fcUnit{module: "fixedclassprobe", unit: probe}
	for _, pc := range fixedClassProbeTexts {
		inst := pm.New(pm.Lookup(pc.root))
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
		pu.cases = append(pu.cases, fcCase{name: pc.name, root: pc.root, wire: wire, text: append(text, '\n')})
	}
	units = append(units, pu)

	// THE PREMISE AND THE ORACLE FIRST: every unit's closure holds a declared
	// `fixed table`, every committed wire decodes, re-encodes to the same bytes
	// and writes the committed text, and every report row's expected answer is
	// the oracle's own decode, so a difference below is this leg's.
	for _, u := range units {
		if len(ir.TableFixedRoots(u.unit)) == 0 {
			t.Fatalf("%s: the closure holds no fixed table, so this test proves nothing", u.module)
		}
		m := tabletext.NewModel(u.unit)
		for _, c := range u.cases {
			inst := m.New(m.Lookup(c.root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
				t.Fatalf("%s: the oracle did not decode its own wire: %v", c.name, err)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire (%v)", c.name, err)
			}
			text, err := m.Write(inst)
			if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
				t.Fatalf("%s: the oracle does not write the committed text (%v)", c.name, err)
			}
		}
		for i := range u.reports {
			r := &u.reports[i]
			inst := m.New(m.Lookup(r.root))
			var rep tabletext.Report
			ok, _ := tablewire.Decode(m, inst, r.wire, &rep)
			malformed := rep.Malformed || (!ok && !rep.Refused)
			verdict := "read"
			if rep.Refused {
				verdict = "refused"
			}
			r.want = fmt.Sprintf("%d,%d,%d,%d,%d,%t,%s", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, malformed, verdict)
		}
	}

	var mods, main strings.Builder
	// variableClassProbeHead's macros, without the `fn main() {` it opens:
	// this test drives its own main.
	head := strings.TrimSuffix(variableClassProbeHead, "fn main() {\n")
	if head == variableClassProbeHead {
		t.Fatal("variableClassProbeHead no longer ends by opening main")
	}
	main.WriteString(head)
	main.WriteString(fixedClassReportMacro)
	main.WriteString("fn fixed_class_cases() {\n")
	var want []string
	for _, u := range units {
		vc := newVariableClass(u.unit)
		for _, st := range ir.TableFixedRoots(u.unit) {
			if !vc.carried(st.Name) {
				t.Errorf("%s: this leg refuses the fixed table %s on the variable wire: %s", u.module, st.Name, vc.reason[st.Name])
			}
		}
		modules, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("%s: generation refused a closure holding a fixed table: %v", u.module, err)
		}
		dir := filepath.Join(work, u.module)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var names []string
		for name, data := range modules {
			if name == VariableRuntimeModule+".rs" || strings.HasSuffix(name, "_table.rs") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
				names = append(names, strings.TrimSuffix(name, ".rs"))
			}
		}
		sort.Strings(names)
		// THE FORM-1 SURFACE IS PRESENT for every fixed root: the surface
		// vt/fixed-class/rust said was absent.
		for _, st := range ir.TableFixedRoots(u.unit) {
			table := fcTableModule(u.unit, st.Name)
			snake := ir.RustSnake(st.Name)
			for _, verb := range []string{"_load(", "_save(", "_to_json_checked(", "_from_json("} {
				if !strings.Contains(string(modules[table+".rs"]), "pub fn "+snake+verb) {
					t.Errorf("%s: no form-1 %s for the fixed table %s in %s.rs", u.module, snake+strings.TrimSuffix(verb, "("), st.Name, table)
				}
			}
		}
		if t.Failed() {
			t.FailNow()
		}
		fmt.Fprintf(&mods, "#[allow(dead_code)]\nmod %s {\n", u.module)
		for _, mod := range names {
			fmt.Fprintf(&mods, "    #[path = %q]\n    pub mod %s;\n", filepath.Join(dir, mod+".rs"), mod)
		}
		mods.WriteString("}\n")
		for _, c := range u.cases {
			wirePath := filepath.Join(work, c.name+".wire")
			if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			jsonPath := filepath.Join(work, c.name+".json")
			if err := os.WriteFile(jsonPath, c.text, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "WIRE "+c.name+" OK", "JSONW "+c.name+" OK", "JSONR "+c.name+" OK")
			snake := ir.RustSnake(c.root)
			fmt.Fprintf(&main, "    run_case!(%q, %q, Some(%q), crate::%s::%s, %s_load, %s_save, %s_to_json_checked, %s_from_json);\n",
				c.name, wirePath, jsonPath, u.module, fcTableModule(u.unit, c.root), snake, snake, snake, snake)
		}
		for _, r := range u.reports {
			wirePath := filepath.Join(work, r.name+".report.wire")
			if err := os.WriteFile(wirePath, r.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "REPORT "+r.name+" "+r.want)
			snake := ir.RustSnake(r.root)
			fmt.Fprintf(&main, "    report_case!(%q, %q, crate::%s::%s, %s_load);\n",
				r.name, wirePath, u.module, fcTableModule(u.unit, r.root), snake)
		}
	}
	main.WriteString("}\n\nfn main() { fixed_class_cases(); }\n")
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(mods.String()+main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the fixed-class codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the fixed-class probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		got[strings.TrimSpace(line)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("the probe did not print %q", w)
		}
	}
	t.Logf("probe output (%d expected lines):\n%s", len(want), out)
}

// fcTableModule is the codec module that declares a table: its file's base,
// lowered, with _table.
func fcTableModule(u *ir.Unit, name string) string {
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return strings.ToLower(f.Base) + "_table"
			}
		}
	}
	return ""
}

// fcLoadFile parses one schema file of the corpus as a unit of its own.
func fcLoadFile(t *testing.T, repo, rel string) *ir.Unit {
	t.Helper()
	path := filepath.Join(repo, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	f, perrs := parser.Parse(name, data)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", path, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: data, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", path, cerrs[0])
	}
	return u
}

// fixedClassReportMacro prints a load's report the way the conformance
// driver's `report` surface spells it (test/conformance/rust/src/main.rs).
const fixedClassReportMacro = `
macro_rules! report_case {
    ($case:expr, $wire:expr, $m:path, $load:ident) => {{
        use $m as table;
        let wire = std::fs::read($wire).unwrap();
        let mut r = Default::default();
        let ok = table::$load(&wire, &mut r).is_some();
        let malformed = r.malformed || (!ok && !r.refused);
        let verdict = if r.refused { "refused" } else { "read" };
        println!("REPORT {} {},{},{},{},{},{},{}", $case, r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, malformed, verdict);
    }};
}
`

// fixedClassProbeSrc is the probe unit: `fixed table` roots whose fields carry
// a string, a bytes and a flags default beside the scalar ones
// (docs/SPEC-TABLES.md §4.2), and an elided instance beside a written one, so
// a dropped default shows as a wire difference and not as a silent zero.
const fixedClassProbeSrc = `package fixedclassprobe

enum Grade { Bronze, Silver, Gold }

flags Perks { Shielded, Cloaked, Boosted }

type Badge
{
    label string(8) = "new"
    perks Perks = { Cloaked }
}

fixed table Leaf
{
    id    int32      = 7
    name  string(8)  = "leaf"
    raw   bytes(4)   = "ab"
    perks Perks      = { Shielded, Boosted }
    on    bool       = true
    ratio float64    = 1.5
    grade Grade      = Silver
}

fixed table Config
{
    level  int32      = 3
    name   string(16) = "hi"
    leaf   Leaf
    badge  Badge
    many   [..4]int32
    podium [3]Grade
}
`

type fixedClassProbeText struct {
	name string
	root string
	text string
}

// fixedClassProbeTexts are the probe instances: every field at its declared
// default (so the wire elides it and a dropped default reads back wrong), and
// every field carrying a value, the empty string, bytes and mask included.
var fixedClassProbeTexts = []fixedClassProbeText{
	{"fc_leaf_default", "Leaf", `{}`},
	{"fc_leaf_values", "Leaf", `{"id": 41, "name": "x", "raw": "AAECAw==", "perks": ["Cloaked"], "on": false, "ratio": 2.25, "grade": "Gold"}`},
	{"fc_leaf_empty", "Leaf", `{"name": "", "raw": "", "perks": []}`},
	{"fc_config_default", "Config", `{}`},
	{"fc_config_values", "Config", `{"level": 9, "name": "there", "leaf": {"id": 1, "name": "l", "perks": [], "on": false, "grade": "Bronze"}, "badge": {"label": "b", "perks": ["Shielded", "Boosted"]}, "many": [1, 2, 3], "podium": ["Gold", "None", "Silver"]}`},
}

// TestFixedClassClampElidesImpossibleEnds pins §4's read clamp on the form-1
// codec a fixed table now gets (docs/SPEC-TABLES.md §4): an end of a declared
// range that sits ON the decode local's own limit (0 and u64::MAX for an
// unsigned field, i64::MIN and i64::MAX for a signed one) is not written,
// because that comparison cannot fire and is a deny-by-default clippy error
// (absurd_extreme_comparisons) that `make tables-rust-clippy` reds on
// tables/examples/Ranges.schema; an end that can fire is kept and counted.
func TestFixedClassClampElidesImpossibleEnds(t *testing.T) {
	t.Parallel()

	out := generate(t, `package probe

fixed table Ranged
{
    u8_span  uint8  | min = 0, max = 255
    u64_span uint64 | min = 0, max = 18446744073709551615
    u64_low  uint64 = 1 | min = 1, max = 18446744073709551615
    i64_span int64  | min = -9223372036854775808, max = 9223372036854775807
    i64_high int64  | min = -9223372036854775808, max = 9
    i8_span  int8   | min = -128, max = 127
}
`)
	codec := string(out["probe_table.rs"])
	if codec == "" {
		t.Fatal("the fixed table Ranged emitted no form-1 codec probe_table.rs")
	}
	for _, never := range []string{
		"x < 0u64", "x > 18446744073709551615u64",
		"x < -9223372036854775808i64", "x > 9223372036854775807i64",
	} {
		if strings.Contains(codec, never) {
			t.Errorf("the form-1 read writes a clamp that cannot fire: %q", never)
		}
	}
	for _, want := range []string{
		"if x > 255u64 { x = 255u64; report.clamped += 1; }",
		"if x < 1u64 { x = 1u64; report.clamped += 1; }",
		"if x > 9i64 { x = 9i64; report.clamped += 1; }",
		"if x < -128i64 { x = -128i64; report.clamped += 1; } else if x > 127i64 { x = 127i64; report.clamped += 1; }",
	} {
		if !strings.Contains(codec, want) {
			t.Errorf("the form-1 read lost a clamp that can fire: %q", want)
		}
	}
}

// TestPointerToDefaultedTableKeepsANullSlot pins the cooked record a
// variable table holding a pointer to a defaulted fixed table gets (W1's Fleet
// over Vessel; docs/SPEC-TABLES.md §6.3, §7.2): the pointer's slot is an i64
// self-relative delta whose zero is null, so <Name>Row::default() never
// assigns the target's Row into it, scalar or bounded array, while a by-value
// field of the same table still takes the target's defaults.
func TestPointerToDefaultedTableKeepsANullSlot(t *testing.T) {
	t.Parallel()

	out := generate(t, `package probe

fixed table Vessel
{
    name string(8) = "boat"
}

table Fleet
{
    flagship *Vessel
    escorts  [..4]*Vessel
    home     Vessel
}
`)
	def := rustDefaultImpl(string(out["probe_records.rs"]), "FleetRow")
	if def == "" {
		t.Fatal("impl Default for FleetRow was not emitted")
	}
	for _, never := range []string{"value.flagship = VesselRow::default()", "value.escorts[i] = VesselRow::default()"} {
		if strings.Contains(def, never) {
			t.Errorf("a pointer slot takes the target's record: %q\n%s", never, def)
		}
	}
	if !strings.Contains(def, "value.home = VesselRow::default()") {
		t.Errorf("the by-value field lost the target's defaults:\n%s", def)
	}
}
