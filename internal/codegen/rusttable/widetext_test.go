package rusttable

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestWideTextOnTheTableWireRust pins the Rust leg's carry of `wstring(N)` on
// the variable wire (docs/SPEC-TABLES.md §2, §3 kind 33, §4, §16.2; SPEC.md
// §4.12), every case held BYTE FOR BYTE against the Go oracle
// (internal/tablewire, internal/tabletext):
//
//   - every widedemo instance testdata/conformance/tables/MANIFEST.txt names
//     (wide_empty, wide_title, wide_sites, wide_narrow_arm, wide_stamp), read
//     TWICE: through examples-wide/Caption.schema itself, whose `fixed table`
//     roots ride the variable form (§2.2, §3), and through a VARIABLE MIRROR
//     of it (the same names, so the same ids, with Line a table rather than a
//     `type`) that the probe and damage rows below are authored against; the
//     oracle first proves both read and write the goldens byte for byte;
//   - map_text (tables/maps Text): wstring(6) as a MAP VALUE;
//   - wstring(N) as a direct field, as a union arm (and its narrow sibling),
//     inside a table reached by value and by element, and as a map value;
//   - the READ RULES: a clamp at N code units that never splits a pair, an
//     odd L, an unpaired surrogate and a zero unit each counted as the oracle
//     counts them, and a respelling between kind 12 and kind 33 a kind
//     mismatch, the saved bytes the oracle's own re-encode;
//   - the WRITE RULE: a value over N units is refused by name by
//     <root>_save_checked (Err), <root>_save and <root>_save_retain (panic),
//     with no successful truncated wire from any public entry point.
func TestWideTextOnTheTableWireRust(t *testing.T) {
	t.Parallel()

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	// THE CLASS: before the wire, the leg must carry every wide site by name.
	mirror := unitFrom(t, wideMirrorSrc)
	vc := newVariableClass(mirror)
	for _, root := range []string{"Caption", "Stamp", "Notes"} {
		if !vc.carried(root) {
			t.Fatalf("wstring(N) on the table wire: %s is refused: %s", root, vc.reason[root])
		}
	}
	// the committed unit itself: its fixed roots are on the variable form
	captionSrc, err := os.ReadFile(filepath.Join(repo, "examples-wide", "Caption.schema"))
	if err != nil {
		t.Fatal(err)
	}
	widedemo := unitFrom(t, string(captionSrc))
	wvc := newVariableClass(widedemo)
	for _, root := range []string{"Caption", "Stamp"} {
		if !wvc.carried(root) || !wvc.emitted[root] {
			t.Fatalf("wstring(N) on the table wire: examples-wide %s is not on the variable form: %q", root, wvc.reason[root])
		}
	}
	mapsUnit := vcLoadUnit(t, repo, "tables/maps")
	if why := newVariableClass(mapsUnit).reason["Text"]; why != "" {
		t.Fatalf("wstring(N) as a map value: Text is refused: %s", why)
	}
	// an array of wstring(N) and ?wstring(N) never reach this leg: the
	// checker refuses both by name ("wrap the element in a type", §15)

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	work := t.TempDir()

	type wCase struct {
		name, root string
		wire, text []byte
	}
	type wUnit struct {
		module string
		unit   *ir.Unit
		cases  []wCase
	}

	// the widedemo rows, read from the manifest rather than listed here
	var wide []wCase
	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 || f[0] != "instance" || f[2] != "widedemo" {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(repo, f[4]))
		if err != nil {
			t.Fatal(err)
		}
		c := wCase{name: f[1], root: f[3], wire: wire}
		if c.text, err = os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", f[1]+".json")); err != nil {
			t.Fatal(err)
		}
		wide = append(wide, c)
	}
	if len(wide) < 5 {
		t.Fatalf("the manifest names %d widedemo instances, want at least 5", len(wide))
	}
	mapText := func() wCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "map_text.bin"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", "map_text.json"))
		if err != nil {
			t.Fatal(err)
		}
		return wCase{name: "map_text", root: "Text", wire: wire, text: text}
	}()

	// the probe rows: authored as text, encoded by the oracle
	mm := tabletext.NewModel(mirror)
	var probes []wCase
	for _, pc := range wideProbeTexts {
		inst := mm.New(mm.Lookup(pc.root))
		var rep tabletext.Report
		if !mm.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(mm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		text, err := mm.Write(inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not write: %v", pc.name, err)
		}
		probes = append(probes, wCase{name: pc.name, root: pc.root, wire: wire, text: append(text, '\n')})
	}

	// the same goldens through the committed unit, named apart so each row
	// is its own line in the probe's output
	var fixed []wCase
	for _, c := range wide {
		c.name += "_widedemo"
		fixed = append(fixed, c)
	}
	units := []wUnit{
		{"widedemo", widedemo, fixed},
		{"widemirror", mirror, append(wide, probes...)},
		{"mapdemo", mapsUnit, []wCase{mapText}},
	}
	for _, u := range units {
		m := tabletext.NewModel(u.unit)
		for _, c := range u.cases {
			inst := m.New(m.Lookup(c.root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil || rep != (tabletext.Report{}) {
				t.Fatalf("%s: the oracle did not decode the wire cleanly through %s: %v %+v", c.name, u.module, err, rep)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire through %s (%v)", c.name, u.module, err)
			}
			text, err := m.Write(inst)
			if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
				t.Fatalf("%s: the oracle does not write the committed text through %s (%v)", c.name, u.module, err)
			}
		}
	}

	// THE READ RULES: a writer with wider bounds and the other text kind
	// makes the wires, a byte patch makes the damage, and the oracle's own
	// report and re-encode are the expectation.
	writer := unitFrom(t, wideWriterSrc)
	wm := tabletext.NewModel(writer)
	type dCase struct {
		name, root string
		wire       []byte
		want       string
	}
	var damage []dCase
	for _, dc := range wideDamage {
		inst := wm.New(wm.Lookup(dc.root))
		var rep tabletext.Report
		if !wm.Read(inst, []byte(dc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the writer text cleanly: %+v", dc.name, rep)
		}
		wire, err := tablewire.Encode(wm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", dc.name, err)
		}
		if dc.find != "" {
			at := bytes.Index(wire, []byte(dc.find))
			if at < 0 || bytes.Count(wire, []byte(dc.find)) != 1 {
				t.Fatalf("%s: the patch site % x is not in the wire once", dc.name, dc.find)
			}
			wire = append(append(append([]byte(nil), wire[:at]...), dc.with...), wire[at+len(dc.find):]...)
		}
		// a respelled writer's root is read as the mirror's Caption: ids are
		// names (ir.TableFieldWireId), so the title rides at Caption.title
		readRoot := dc.root
		if readRoot == "Respell" {
			readRoot = "Caption"
		}
		rinst := mm.New(mm.Lookup(readRoot))
		var rrep tabletext.Report
		if _, err := tablewire.Decode(mm, rinst, wire, &rrep); err != nil {
			t.Fatalf("%s: the oracle refused the damaged wire: %v", dc.name, err)
		}
		if rrep == (tabletext.Report{}) {
			t.Fatalf("%s: the oracle reads the damage clean, so the row pins nothing", dc.name)
		}
		saved, err := tablewire.Encode(mm, rinst)
		if err != nil {
			t.Fatalf("%s: the oracle did not re-encode what it read: %v", dc.name, err)
		}
		savedPath := filepath.Join(work, dc.name+".saved")
		if err := os.WriteFile(savedPath, saved, 0o600); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("REPORT %s %d,%d,%d,%d,%d,%v SAVED OK", dc.name, rrep.Unknown, rrep.KindMismatch, rrep.Widened, rrep.Clamped, rrep.Duplicate, rrep.Malformed)
		if rrep.Malformed {
			// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the leg
			// owes the refusal, and no counter or saved byte is compared
			want = "DISCARD " + dc.name + " true"
		}
		damage = append(damage, dCase{name: dc.name, root: readRoot, wire: wire, want: want})
	}

	var mods, main strings.Builder
	main.WriteString(variableClassProbeHead)
	var want []string
	for _, u := range units {
		modules, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("%s: wide text was refused: %v", u.module, err)
		}
		dir := filepath.Join(work, u.module)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var names []string
		for name, data := range modules {
			if name == VariableRuntimeModule+".rs" || name == TableEnumsModule+".rs" || strings.HasSuffix(name, "_table.rs") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
				names = append(names, strings.TrimSuffix(name, ".rs"))
			}
		}
		sort.Strings(names)
		fmt.Fprintf(&mods, "#[allow(dead_code)]\nmod %s {\n", u.module)
		for _, mod := range names {
			fmt.Fprintf(&mods, "    #[path = %q]\n    pub mod %s;\n", filepath.Join(dir, mod+".rs"), mod)
		}
		mods.WriteString("}\n")
		for _, c := range u.cases {
			table := wideTableModule(t, u.unit, c.root)
			wirePath := filepath.Join(work, c.name+".wire")
			if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(work, c.name+".json")
			if err := os.WriteFile(p, c.text, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "WIRE "+c.name+" OK", "JSONW "+c.name+" OK", "JSONR "+c.name+" OK")
			snake := ir.RustSnake(c.root)
			fmt.Fprintf(&main, "    run_case!(%q, %q, Some(%q), crate::%s::%s, %s_load, %s_save, %s_to_json_checked, %s_from_json);\n",
				c.name, wirePath, p, u.module, table, snake, snake, snake, snake)
		}
	}
	mirrorTable := wideTableModule(t, mirror, "Caption")
	for _, d := range damage {
		wirePath := filepath.Join(work, d.name+".wire")
		if err := os.WriteFile(wirePath, d.wire, 0o600); err != nil {
			t.Fatal(err)
		}
		snake := ir.RustSnake(d.root)
		fmt.Fprintf(&main, "    wide_report!(%q, %q, %q, crate::widemirror::%s, %s_load, %s_save);\n",
			d.name, wirePath, filepath.Join(work, d.name+".saved"), mirrorTable, snake, snake)
		want = append(want, d.want)
	}
	fmt.Fprintf(&main, wideRefusalRust, mirrorTable)
	want = append(want, "REFUSE direct OK", "REFUSE arm OK", "REFUSE map OK", "REFUSE pair OK", "REFUSE fits OK",
		"REFUSE direct save OK", "REFUSE direct retain OK", "REFUSE arm save OK", "REFUSE arm retain OK", "REFUSE map save OK", "REFUSE map retain OK", "REFUSE fits retain OK")
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(wideReportMacro+mods.String()+main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the wide-text codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the wide-text probe did not run: %v\n%s", err, out)
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
	t.Logf("probe output (%d expected lines):\n%s", len(want), out)
}

// wideTableModule is the Rust module a root's file emits.
func wideTableModule(t *testing.T, u *ir.Unit, root string) string {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == root {
				return strings.ToLower(f.Base) + "_table"
			}
		}
	}
	t.Fatalf("no file declares %s", root)
	return ""
}

// wideMirrorSrc is examples-wide/Caption.schema with its fixed roots declared
// variable and its `type Line` a table, so the names (and with them every id
// on the wire) are the committed goldens' own; Notes adds the map value and an
// array of the union.
const wideMirrorSrc = `package wide

table Line
{
    text wstring(4)
}

union Body
{
    wide   wstring(4)
    narrow string(4)
    tally  int32      | was = "count"
}

table Caption
{
    title wstring(7)
    line  Line
    lines [..3]Line
    body  Body
}

table Stamp
{
    label wstring(4)
    seq   uint32
}

table Notes
{
    by_id   map[uint8]wstring(3)
    bodies  []Body
    caption Caption
    trailer int32
}
`

// wideWriterSrc is the same shape as an OLDER or a WIDER writer would declare
// it: every bound larger, and Stamp's label and Notes' trailer respelled to the
// other text kind, so a reader of wideMirrorSrc meets every rule of §3 and §4.
const wideWriterSrc = `package wide

table Line
{
    text wstring(8)
}

union Body
{
    wide   wstring(8)
    narrow string(8)
    tally  int32      | was = "count"
}

table Caption
{
    title wstring(12)
    line  Line
    lines [..3]Line
    body  Body
}

table Stamp
{
    label wstring(8)
    seq   uint32
}

table Respell
{
    title string(12)
}

table Notes
{
    by_id   map[uint8]wstring(8)
    bodies  []Body
    caption Caption
    trailer int32
}
`

var wideProbeTexts = []struct{ root, name, text string }{
	{"Notes", "wide_notes_empty", `{}`},
	{"Notes", "wide_notes_map", `{"by_id": {"9": "z", "1": "Aé中", "4": "😀"}, "trailer": 3}`},
	{"Notes", "wide_notes_arms", `{"bodies": [{"wide": "AB"}, {}, {"narrow": "ok"}, {"tally": -4}, {"wide": "😀😀"}]}`},
	{"Notes", "wide_notes_all", `{"by_id": {"2": "x"}, "bodies": [{"wide": "q"}], "caption": {"title": "Zé😀中", "line": {"text": "ab"}, "lines": [{"text": "1"}, {}, {"text": "😀"}], "body": {"wide": "w"}}, "trailer": -1}`},
	{"Caption", "wide_caption_full", `{"title": "1234567", "line": {"text": "abcd"}, "lines": [{"text": "a"}, {"text": "bb"}, {"text": "ccc"}], "body": {"tally": 7}}`},
	{"Stamp", "wide_stamp_pair", `{"label": "A😀B", "seq": 4294967295}`},
	{"Stamp", "wide_stamp_escapes", "{\"label\": \"\\\"\\\\\\n\\u0001\"}"},
}

// wideDamage is the read side's rows. `find`/`with` patch the writer's wire:
// "AB" is 41 00 42 00 under L = 4.
var wideDamage = []struct {
	root, name, text string
	find, with       string
}{
	{"Caption", "wide_clamp_title", `{"title": "ABCDEFGHI"}`, "", ""},
	{"Stamp", "wide_clamp_pair", `{"label": "ABC😀", "seq": 1}`, "", ""},
	{"Caption", "wide_clamp_arm", `{"body": {"wide": "ABCDEF"}}`, "", ""},
	{"Caption", "wide_clamp_line", `{"line": {"text": "ABCD😀"}, "lines": [{"text": "ABCDEFG"}]}`, "", ""},
	{"Notes", "wide_clamp_map", `{"by_id": {"1": "ABCD", "2": "AB"}, "trailer": 5}`, "", ""},
	{"Stamp", "wide_unpaired", `{"label": "AB", "seq": 2}`, "\x41\x00\x42\x00", "\x41\x00\x00\xd8"},
	{"Stamp", "wide_low_first", `{"label": "AB", "seq": 2}`, "\x41\x00\x42\x00", "\x00\xdc\x42\x00"},
	{"Stamp", "wide_zero_unit", `{"label": "AB", "seq": 2}`, "\x41\x00\x42\x00", "\x41\x00\x00\x00"},
	{"Stamp", "wide_odd_length", `{"label": "AB", "seq": 2}`, "\x21\x04\x41\x00\x42\x00", "\x21\x05\x41\x00\x42\x00\x43"},
	{"Caption", "wide_arm_unpaired", `{"body": {"wide": "AB"}}`, "\x41\x00\x42\x00", "\x41\x00\x00\xd8"},
	{"Caption", "wide_arm_odd", `{"body": {"wide": "AB"}}`, "\x21\x04\x41\x00\x42\x00", "\x21\x05\x41\x00\x42\x00\x43"},
	{"Notes", "wide_map_unpaired", `{"by_id": {"1": "AB"}}`, "\x41\x00\x42\x00", "\x41\x00\x00\xd8"},
	{"Respell", "wide_respell_title", `{"title": "narrow"}`, "", ""},
}

// wideReportMacro reads one damaged wire and prints the report and whether the
// save is the oracle's own re-encode.
const wideReportMacro = `
macro_rules! wide_report {
    ($case:expr, $wire:expr, $saved:expr, $m:path, $load:ident, $save:ident) => {{
        use $m as table;
        let wire = std::fs::read($wire).unwrap();
        let saved = std::fs::read($saved).unwrap();
        let mut r = Default::default();
        let loaded = table::$load(&wire, &mut r);
        println!("DISCARD {} {}", $case, r.malformed);
        match loaded {
            None => println!("REPORT {} REFUSED", $case),
            Some(v) => {
                let again = table::$save(&v);
                println!("REPORT {} {},{},{},{},{},{} SAVED {}", $case, r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.malformed, if again == saved { "OK" } else { "DIFF" });
            }
        }
    }};
}
`

// wideRefusalRust is the write rule: over N code units is refused by name, at
// a direct field, an arm and a map value, and a pair counts two units.
const wideRefusalRust = `    {
        use crate::widemirror::%[1]s as table;
        let units = |s: &str| -> Vec<u16> { s.encode_utf16().collect() };
        fn check_panic<F: FnOnce() -> Vec<u8>>(what: &str, write: F, names: &[&str]) {
            match std::panic::catch_unwind(std::panic::AssertUnwindSafe(write)) {
                Ok(_) => println!("REFUSE {} WROTE", what),
                Err(p) => {
                    let why = p.downcast_ref::<String>().map(|s| s.as_str())
                        .or_else(|| p.downcast_ref::<&str>().copied()).unwrap_or("");
                    if names.iter().all(|n| why.contains(n)) { println!("REFUSE {} OK", what); }
                    else { println!("REFUSE {} WRONG {}", what, why); }
                }
            }
        }
        let check = |what: &str, r: Result<Vec<u8>, String>, names: &[&str]| match r {
            Err(e) if names.iter().all(|n| e.contains(n)) => println!("REFUSE {} OK", what),
            Err(e) => println!("REFUSE {} WRONG {}", what, e),
            Ok(_) => println!("REFUSE {} WROTE", what),
        };
        let mut s = table::Stamp::default();
        s.label = units("ABCDE");
        check("direct", table::stamp_save_checked(&s), &["Stamp.label", "wstring(4)", "5"]);
        check_panic("direct save", || table::stamp_save(&s), &["Stamp.label", "wstring(4)"]);
        let mut retain = crate::widemirror::variable_runtime::TableRetain::default();
        let mut report = crate::widemirror::variable_runtime::TableReport::default();
        check_panic("direct retain", || table::stamp_save_retain(&s, &mut retain, &mut report), &["Stamp.label", "wstring(4)"]);
        let mut c = table::Caption::default();
        c.body = table::Body::Wide(units("ABCDE"));
        check("arm", table::caption_save_checked(&c), &["Body.wide", "wstring(4)"]);
        check_panic("arm save", || table::caption_save(&c), &["Body.wide", "wstring(4)"]);
        let mut retain = crate::widemirror::variable_runtime::TableRetain::default();
        let mut report = crate::widemirror::variable_runtime::TableReport::default();
        check_panic("arm retain", || table::caption_save_retain(&c, &mut retain, &mut report), &["Body.wide", "wstring(4)"]);
        let mut n = table::Notes::default();
        let mut e = table::NotesByIdEntry::default();
        e.key = 1;
        e.value = units("ABCD");
        n.by_id.push(e);
        check("map", table::notes_save_checked(&n), &["NotesByIdEntry.value", "wstring(3)"]);
        check_panic("map save", || table::notes_save(&n), &["NotesByIdEntry.value", "wstring(3)"]);
        let mut retain = crate::widemirror::variable_runtime::TableRetain::default();
        let mut report = crate::widemirror::variable_runtime::TableReport::default();
        check_panic("map retain", || table::notes_save_retain(&n, &mut retain, &mut report), &["NotesByIdEntry.value", "wstring(3)"]);
        s.label = units("AB😀C");
        check("pair", table::stamp_save_checked(&s), &["Stamp.label", "wstring(4)"]);
        s.label = units("A😀B");
        match table::stamp_save_checked(&s) {
            Ok(w) if w == table::stamp_save(&s) => println!("REFUSE fits OK"),
            other => println!("REFUSE fits WRONG {:?}", other),
        }
        let mut retain = crate::widemirror::variable_runtime::TableRetain::default();
        let mut report = crate::widemirror::variable_runtime::TableReport::default();
        if table::stamp_save_retain(&s, &mut retain, &mut report) == table::stamp_save(&s) {
            println!("REFUSE fits retain OK");
        } else {
            println!("REFUSE fits retain WRONG");
        }
    }
`
