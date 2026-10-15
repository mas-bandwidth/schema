package rusttable

import (
	"bytes"
	"encoding/binary"
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

// TestVariableClassBeyondCardConstructsRust pins the Rust leg's carry of the
// variable class beyond the card constructs (docs/SPEC-TABLES.md §2.5, §2.9,
// §3, §3.1, §16.7), every case held BYTE FOR BYTE against the Go oracle
// (internal/tablewire, internal/tabletext):
//
//   - the pointer corpus (tables/pointers): table pointers *T over the flat node
//     table, a shared node, a tree, a 260-node chain, a variable table nested
//     by value, a bounded array of variable tables and a fixed table nested by
//     value;
//   - the byte-buffer corpus (tables/blobs): *bytes and *string blobs, empty,
//     large and shared;
//   - the unbounded-array corpus (tables/lists): []T of fixed tables, []*T
//     pointer arrays with a shared node and a null slot, []int32 scalars, a
//     pointer array numbered before a pointer field, lists nested in lists, a
//     pointed-at holder of a list;
//   - the rest of the scalar kinds (uint8..uint64, int8, int16 with a range,
//     float32, float64, bits(N), bool), bytes(N), declared defaults, arrays of
//     enums, bools, floats and narrow ints, a bounded array of a fixed table,
//     and a shared node defined and named through §16.7's `&node`, in a probe
//     unit whose wires and texts the oracle writes here;
//   - blob identity (§2.5, §3.1): two *string and two *bytes slots holding
//     equal bytes, empty and not, are two records, as the oracle writes them,
//     while one blob two slots name (blob_shared) stays one record on the
//     wire and the text writer REFUSES it, as the oracle's does (§16.7);
//   - the escape kind 31 (§3): at a field the reader cannot name (unknown), at
//     a field it names and as an array's element kind (kind_mismatch each), the
//     report compared with the oracle's.
//
// Each wire is loaded and saved back (the bytes must not move), written as text
// (the text must be the oracle's), and read back from the text and saved (the
// bytes must be the wire again). A corpus wire is first held to the oracle
// itself: the oracle decodes it and re-encodes the same bytes, and writes the
// committed text.
func TestVariableClassBeyondCardConstructsRust(t *testing.T) {
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

	type vcCase struct {
		name, root string
		wire, text []byte // text nil: a wire-only instance (no-text)
		// refuseText: the oracle's writer REFUSES this value (a blob two
		// slots name has no text spelling, §16.7), and so must the leg's
		refuseText bool
	}
	type vcUnit struct {
		module string
		unit   *ir.Unit
		cases  []vcCase
	}
	corpus := func(name, root string, text bool) vcCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		c := vcCase{name: name, root: root, wire: wire}
		if text {
			c.text, err = os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	units := []vcUnit{
		{"graphdemo", vcLoadUnit(t, repo, "tables/pointers"), []vcCase{
			corpus("graph_tree", "Scene", true),
			corpus("graph_shared", "Scene", true),
			corpus("graph_empty", "Scene", true),
			corpus("graph_deep", "Scene", false),
		}},
		{"blobdemo", vcLoadUnit(t, repo, "tables/blobs"), []vcCase{
			corpus("blob_small", "Catalog", true),
			corpus("blob_empty", "Catalog", true),
			corpus("blob_large", "Catalog", true),
			func() vcCase { c := corpus("blob_shared", "Catalog", false); c.refuseText = true; return c }(),
			corpus("blob_str8", "Catalog", true),
			corpus("blob_str16", "Catalog", true),
		}},
		{"listdemo", vcLoadUnit(t, repo, "tables/lists"), []vcCase{
			corpus("list_tables", "Save", true),
			corpus("list_scalars", "Save", true),
			corpus("list_empty", "Save", true),
			corpus("list_erased", "Save", true),
			corpus("list_shared", "Album", true),
			corpus("list_before_pointer", "Album", true),
			corpus("list_nested", "Sheet", true),
			corpus("list_migrates", "Unbounded", true),
		}},
	}

	// THE CORPUS IS THE ORACLE'S: decode, re-encode the same bytes, and write
	// the committed text, so every byte the Rust leg is held to below is one the
	// Go oracle produces.
	for _, u := range units {
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
			if c.refuseText {
				if _, err := m.Write(inst); err == nil {
					t.Fatalf("%s: the oracle's writer did not refuse the value", c.name)
				}
			}
			if c.text != nil {
				// the canonical text ends with exactly one newline (§16.1)
				text, err := m.Write(inst)
				if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
					t.Fatalf("%s: the oracle does not write the committed text (%v)", c.name, err)
				}
			}
		}
	}

	// THE PROBE UNIT: what the corpus does not reach, its wires and texts made
	// by the oracle from the texts below.
	probe := unitFrom(t, variableClassProbeSrc)
	pm := tabletext.NewModel(probe)
	probeCases := []vcCase{}
	for _, pc := range variableClassProbeTexts {
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
		if strings.Contains(pc.name, "_strings") || strings.Contains(pc.name, "_bytes") {
			// the case is only worth its name if the oracle writes TWO blob
			// records for the two equal slots (node_count 2, §3.1)
			g, err := tablewire.Number(pm, inst)
			if err != nil || len(g.Records()) != 2 {
				t.Fatalf("%s: the oracle does not number two blob records for two equal slots (%v)", pc.name, err)
			}
		}
		probeCases = append(probeCases, vcCase{name: pc.name, root: "Probe", wire: wire, text: append(text, '\n')})
	}
	units = append(units, vcUnit{"classprobe", probe, probeCases})

	// THE ESCAPE KIND (§3): one wire, kind 31 at three positions, and the
	// oracle's report for it.
	escape := variableClassEscapeWire(t, probe)
	escInst := pm.New(pm.Lookup("Probe"))
	var escRep tabletext.Report
	if ok, err := tablewire.Decode(pm, escInst, escape, &escRep); !ok || err != nil {
		t.Fatalf("the oracle refused the escape wire: %v", err)
	}
	if escRep.Unknown != 1 || escRep.KindMismatch != 2 || escRep.Malformed {
		t.Fatalf("the oracle's escape report is not the spec's (one unknown, two kind mismatches, nothing malformed): %+v", escRep)
	}
	escSaved, err := tablewire.Encode(pm, escInst)
	if err != nil {
		t.Fatal(err)
	}
	escPath := filepath.Join(work, "escape.wire")
	escSavedPath := filepath.Join(work, "escape_saved.wire")
	if err := os.WriteFile(escPath, escape, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(escSavedPath, escSaved, 0o600); err != nil {
		t.Fatal(err)
	}

	// THE RUST PROBE: one binary over every unit's generated modules.
	var mods, main strings.Builder
	main.WriteString(variableClassProbeHead)
	var want []string
	for _, u := range units {
		modules, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("%s: the variable class was refused: %v", u.module, err)
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
		fmt.Fprintf(&mods, "#[allow(dead_code)]\nmod %s {\n", u.module)
		for _, mod := range names {
			fmt.Fprintf(&mods, "    #[path = %q]\n    pub mod %s;\n", filepath.Join(dir, mod+".rs"), mod)
		}
		mods.WriteString("}\n")
		for _, c := range u.cases {
			table := ""
			for _, f := range u.unit.Files {
				for _, st := range f.Tables {
					if st.Name == c.root {
						table = strings.ToLower(f.Base) + "_table"
					}
				}
			}
			if table == "" {
				t.Fatalf("%s: no file declares %s", c.name, c.root)
			}
			wirePath := filepath.Join(work, c.name+".wire")
			if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			jsonPath := "None"
			want = append(want, "WIRE "+c.name+" OK")
			if c.text != nil {
				p := filepath.Join(work, c.name+".json")
				if err := os.WriteFile(p, c.text, 0o600); err != nil {
					t.Fatal(err)
				}
				jsonPath = fmt.Sprintf("Some(%q)", p)
				want = append(want, "JSONW "+c.name+" OK", "JSONR "+c.name+" OK")
			}
			snake := ir.RustSnake(c.root)
			if c.refuseText {
				want = append(want, "JSONREFUSE "+c.name+" OK")
				fmt.Fprintf(&main, "    refuse_case!(%q, %q, crate::%s::%s, %s_load, %s_to_json_checked);\n",
					c.name, wirePath, u.module, table, snake, snake)
			}
			fmt.Fprintf(&main, "    run_case!(%q, %q, %s, crate::%s::%s, %s_load, %s_save, %s_to_json_checked, %s_from_json);\n",
				c.name, wirePath, jsonPath, u.module, table, snake, snake, snake, snake)
		}
	}
	fmt.Fprintf(&main, "    escape_case!(%q, %q, crate::classprobe::probe_table);\n", escPath, escSavedPath)
	want = append(want, fmt.Sprintf("ESCAPE %d,%d,%d,%d,%d,%v SAVED OK", escRep.Unknown, escRep.KindMismatch, escRep.Widened, escRep.Clamped, escRep.Duplicate, escRep.Malformed))
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(mods.String()+main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the variable class codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the variable class probe did not run: %v\n%s", err, out)
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

// vcLoadUnit parses one corpus unit directory through the parser and checker.
func vcLoadUnit(t *testing.T, repo, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repo, dir))
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		path := filepath.Join(repo, dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(e.Name(), data)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", path, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: path, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: data, AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}

// variableClassEscapeWire builds one Probe wire carrying the escape kind 31
// (docs/SPEC-TABLES.md §3) at three positions: a field this reader cannot name
// (unknown), the named int32 `tail` (kind_mismatch), and the element kind of
// the named []int8 `small` (kind_mismatch).
func variableClassEscapeWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	probe := u.Tables["Probe"]
	var tail, small *ir.Field
	for _, f := range probe.Fields {
		switch f.Name {
		case "tail":
			tail = f
		case "small":
			small = f
		}
	}
	if tail == nil || small == nil {
		t.Fatal("the probe declares no tail or small")
	}
	ids := []uint64{ir.TableWireId("from_a_later_major"), ir.TableFieldWireId(tail), ir.TableFieldWireId(small)}
	esc, arr := byte(ir.TableKindEscape), byte(ir.TableKindArray)
	w := []byte{byte(ir.TableWireForm)}
	w = append(w, 1, esc, 2, 0xAA, 0xBB) // unnameable: skipped by L, unknown
	w = append(w, 2, esc, 1, 0x00)       // named: kind_mismatch
	w = append(w, 3, arr, 3, esc, 1, 0)  // element kind 31: kind_mismatch
	w = append(w, 0)
	for _, id := range ids {
		w = binary.LittleEndian.AppendUint64(w, id)
	}
	return binary.LittleEndian.AppendUint64(w, uint64(len(ids)))
}

// variableClassProbeSrc is the variable class the corpus does not reach.
const variableClassProbeSrc = `package classprobe

enum Mood { calm, angry, sleepy }

fixed table Spot
{
    x   float32
    y   float32 = 1.5
    tag uint16
}

table Leaf
{
    id   uint32
    name string(8)
    next *Leaf
}

table Probe
{
    u8v    uint8 = 7
    u16v   uint16
    u32v   uint32
    u64v   uint64
    i8v    int8 = -3
    i16v   int16 | min = -100, max = 100
    f32v   float32 = 0.5
    f64v   float64
    flag   bool = true
    b10    bits(10)
    raw    bytes(6)
    blob   *bytes
    note   *string
    blob2  *bytes
    note2  *string
    mood   Mood = angry
    moods  []Mood
    flags  []bool
    nums   []float64
    small  []int8
    spots  [..3]Spot
    spot   Spot
    leaves []*Leaf
    head   *Leaf
    tail   int32
}
`

// variableClassProbeTexts are the probe instances, as the oracle reads them.
var variableClassProbeTexts = []struct{ name, text string }{
	{"probe_default", `{}`},
	{"probe_full", `{
  "u8v": 200, "u16v": 65535, "u32v": 4000000000, "u64v": 18446744073709551615,
  "i8v": -128, "i16v": -100, "f32v": 3.25, "f64v": -1e-300, "flag": false,
  "b10": 1023, "raw": "AQIDBAUG", "blob": "3q2+7w==", "note": "nöte",
  "mood": "sleepy", "moods": ["calm", "None", "angry"], "flags": [true, false, true],
  "nums": [0.1, -2.5, 1e300], "small": [-1, 0, 127],
  "spots": [{"x": 1, "y": 2, "tag": 3}, {"x": 0.5, "y": 1.5, "tag": 0}],
  "spot": {"x": -4, "y": 1.5, "tag": 9},
  "leaves": [{"&node": 1, "id": 1, "name": "one", "next": {"id": 2, "name": "two", "next": null}}, null, {"&node": 1}],
  "head": {"&node": 1},
  "tail": -7
}`},
	// A BLOB IS A NODE, NUMBERED BY OBJECT (docs/SPEC-TABLES.md §2.5, §3.1):
	// two slots holding EQUAL bytes in two blobs are two records, which the
	// oracle writes and a by-content numbering would merge into one
	{"probe_equal_strings", `{"note": "hi", "note2": "hi"}`},
	{"probe_empty_strings", `{"note": "", "note2": ""}`},
	{"probe_equal_bytes", `{"blob": "AQI=", "blob2": "AQI="}`},
	{"probe_empty_bytes", `{"blob": "", "blob2": ""}`},
	{"probe_edges", `{
  "u8v": 0, "f32v": 0, "flag": true, "b10": 0, "raw": "", "blob": "", "note": "",
  "mood": "None", "nums": [], "spots": [{"x": 0, "y": 1.5, "tag": 0}],
  "spot": {"x": 0, "y": 1.5, "tag": 0},
  "leaves": [null], "head": {"id": 0, "name": "", "next": null}
}`},
}

const variableClassProbeHead = `
macro_rules! run_case {
    ($case:expr, $wire:expr, $json:expr, $m:path, $load:ident, $save:ident, $to:ident, $from:ident) => {{
        use $m as table;
        let wire = std::fs::read($wire).unwrap();
        let mut r = Default::default();
        match table::$load(&wire, &mut r) {
            None => println!("WIRE {} REFUSED", $case),
            Some(v) => {
                let saved = table::$save(&v);
                let ok = r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed;
                if saved == wire && ok {
                    println!("WIRE {} OK", $case);
                } else {
                    println!("WIRE {} DIFF saved {} wire {} clean {}", $case, saved.len(), wire.len(), ok);
                }
                let json: Option<&str> = $json;
                if let Some(path) = json {
                    let text = std::fs::read_to_string(path).unwrap();
                    match table::$to(&v) {
                        Some(t) if t == text => println!("JSONW {} OK", $case),
                        Some(t) => println!("JSONW {} DIFF\n{}", $case, t),
                        None => println!("JSONW {} REFUSED", $case),
                    }
                    let mut r2 = Default::default();
                    match table::$from(&text, &mut r2) {
                        Some(v2) => {
                            let ok2 = r2.unknown == 0 && r2.kind_mismatch == 0 && r2.clamped == 0 && !r2.malformed;
                            if table::$save(&v2) == wire && ok2 { println!("JSONR {} OK", $case); }
                            else { println!("JSONR {} DIFF clean {}", $case, ok2); }
                        }
                        None => println!("JSONR {} REFUSED", $case),
                    }
                }
            }
        }
    }};
}

macro_rules! refuse_case {
    ($case:expr, $wire:expr, $m:path, $load:ident, $to:ident) => {{
        use $m as table;
        let wire = std::fs::read($wire).unwrap();
        let mut r = Default::default();
        match table::$load(&wire, &mut r) {
            None => println!("JSONREFUSE {} LOAD REFUSED", $case),
            Some(v) => match table::$to(&v) {
                None => println!("JSONREFUSE {} OK", $case),
                Some(t) => println!("JSONREFUSE {} WROTE {} bytes", $case, t.len()),
            },
        }
    }};
}

macro_rules! escape_case {
    ($wire:expr, $saved:expr, $m:path) => {{
        use $m as table;
        let wire = std::fs::read($wire).unwrap();
        let saved = std::fs::read($saved).unwrap();
        let mut r = Default::default();
        match table::probe_load(&wire, &mut r) {
            None => println!("ESCAPE REFUSED"),
            Some(v) => {
                let again = table::probe_save(&v);
                println!("ESCAPE {},{},{},{},{},{} SAVED {}", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.malformed, if again == saved { "OK" } else { "DIFF" });
            }
        }
    }};
}

fn main() {
`
