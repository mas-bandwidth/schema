package rusttable

// Five Rust table-codec verdicts, each held to the Go reference (the oracle,
// internal/tablewire and internal/tabletext) or to the Go leg's own rule:
//
//  1. EVERY TABLE CARRIES ITS FORM-1 CODEC AND ITS TEXT (docs/SPEC-TABLES.md
//     §3, §11): a table reaching a counted array with a declared minimum
//     [Min..N]T, or a table named after a standard name the modules spell
//     unqualified (`fixed table Vec`), gets its codec, byte for byte the
//     oracle's; a construct this leg still cannot carry (a `type` by a
//     standard name) refuses generation BY NAME and is never left off
//     silently.
//  2. THE JSON READ'S DEPTH CAP counts a placed array, a map, a union object,
//     an enum-keyed object and a flags array as a level, as the Go reader does
//     (docs/SPEC-TABLES.md §16.2, §16.3).
//  3. A `| deprecated` ENUM VARIANT READS AS THE FIELD'S DECLARED DEFAULT, an
//     element as None, on the wire and in the text, and is never unknown; a
//     writer debug-asserts it (docs/SPEC-TABLES.md §2.15).
//  4. THE UNCHECKED <T>_to_json never returns text for a value the writer
//     refused (a NaN): it debug-asserts, and returns no text, as
//     <T>_to_json_checked returns None (§16.3).
//  5. A REPORT THE CALLER PASSES IN IS ZEROED by each read, as Go's is.
//
// A FUNCTIONAL TEST for the Rust half: it compiles and runs Rust with rustc
// at opt-level 0 and no debug info, once with debug assertions and once
// without. No clock, no socket, no network.

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

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const codecVerdictsSchema = `package verdicts

enum Ship
{
    Fighter
    Bomber | deprecated
    Scout
}

fixed table Vec
{
    x int32 = 0
}

table Leaf
{
    a int32 = 0
}

table Node
{
    kids []*Node
}

table Root
{
    few    [2..4]int32
    leaves [1..3]Leaf
    ship   Ship = Scout
    fleet  []Ship
    v      Vec
    score  float32 = 0
}
`

// TestRustEveryTableCarriesItsCodec is verdict 1 at generation: the tables
// the old gate left off silently (a [Min..N]T, a table named Vec) now carry
// their codec, and what still cannot be carried refuses by name.
func TestRustEveryTableCarriesItsCodec(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, codecVerdictsSchema)
	out, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	codec := string(out["probe_table.rs"])
	for _, want := range []string{"pub fn root_load(", "pub fn root_save(", "pub fn root_to_json(", "pub fn root_from_json(", "pub struct VecTable ", "pub fn vec_table_load("} {
		if !strings.Contains(codec, want) {
			t.Errorf("the form-1 codec is missing %q", want)
		}
	}
	for name, data := range out {
		if strings.Contains(string(data), "pub struct Vec ") || strings.Contains(string(data), "pub struct Vec{") {
			t.Errorf("%s declares a struct named Vec, which shadows std's", name)
		}
	}

	// a `type` by a standard name (HashMap) stays off the variable wire, and
	// generation refuses it by name
	refused := unitFrom(t, "package p\n\ntype HashMap\n{\n    v uint8\n}\n\ntable Holder\n{\n    s HashMap\n}\n")
	if _, err := GenerateLineage(refused, nil); err == nil || !strings.Contains(err.Error(), "HashMap") || !strings.Contains(err.Error(), "rust table form") {
		t.Fatalf("a table reaching `type HashMap` generated without its codec, or refused without naming it: %v", err)
	}
}

// codecVerdictsOracle builds the oracle's wires and texts the probe holds the
// Rust leg to.
type codecVerdictsOracle struct {
	defaultWire, few3Wire, underWire, depWire, vecWire []byte
}

func codecVerdictsWires(t *testing.T, u *ir.Unit) codecVerdictsOracle {
	t.Helper()
	m := fcVariableModel(u)
	encode := func(text string) []byte {
		inst := m.New(m.Lookup("Root"))
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) {
			t.Fatalf("the oracle did not place %s: %+v", text, r)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("the oracle did not encode %s: %v", text, err)
		}
		return wire
	}
	var o codecVerdictsOracle
	o.defaultWire = encode(`{}`)
	o.few3Wire = encode(`{"few": [1, 2, 3]}`)
	o.vecWire = encode(`{"v": {"x": 9}}`)

	// UNDER THE MINIMUM: the oracle's own [5, 6], its count cut to 1
	two := encode(`{"few": [5, 6]}`)
	ek := byte(ir.TableKindI32)
	o.underWire = bytes.Replace(two, []byte{byte(ir.TableKindArray), 10, ek, 2, 5, 0, 0, 0, 6, 0, 0, 0}, []byte{byte(ir.TableKindArray), 6, ek, 1, 5, 0, 0, 0}, 1)
	if bytes.Equal(o.underWire, two) {
		t.Fatal("the [5, 6] array is not where this test expects it")
	}
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), o.underWire, &r); ok || tablewire.RefusalReason(err) != tablewire.ReasonCountOverBound {
		t.Fatalf("the oracle does not refuse a count under the minimum: ok=%v err=%v", ok, err)
	}

	// A DEPRECATED VARIANT ON THE WIRE: the oracle writes Fighter (it refuses
	// to write Bomber), and the id table's Fighter becomes Bomber's id
	dep := encode(`{"ship": "Fighter", "fleet": ["Fighter"]}`)
	e := u.Enums["Ship"]
	from := binary.LittleEndian.AppendUint64(nil, ir.TableWireId(e.VariantWireName(0)))
	to := binary.LittleEndian.AppendUint64(nil, ir.TableWireId(e.VariantWireName(1)))
	o.depWire = bytes.Replace(dep, from, to, 1)
	if bytes.Equal(o.depWire, dep) {
		t.Fatal("Fighter's id is not in the id table")
	}
	inst := m.New(m.Lookup("Root"))
	r = tabletext.Report{}
	if ok, err := tablewire.Decode(m, inst, o.depWire, &r); !ok || err != nil || r.Unknown != 0 {
		t.Fatalf("the oracle does not read a deprecated variant: ok=%v err=%v %+v", ok, err, r)
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), `"ship": "Scout"`) || !strings.Contains(string(text), `"None"`) {
		t.Fatalf("the oracle does not read Bomber as the declared default and the element's None:\n%s", text)
	}
	return o
}

// codecVerdictsDeep is a text nesting `levels` Node tables through placed
// arrays of pointers: the Go reader counts each table and each array.
func codecVerdictsDeep(levels int) string {
	return strings.Repeat(`{"kids": [`, levels) + `{}` + strings.Repeat(`]}`, levels)
}

func TestRustCodecVerdicts(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skipf("rustc is not on PATH: %v", err)
	}
	u := unitFrom(t, codecVerdictsSchema)
	o := codecVerdictsWires(t, u)

	// THE DEPTH CAP, held to the oracle: the first nesting it refuses, and
	// the one inside it, which it reads
	m := fcVariableModel(u)
	deep := 0
	for levels := 1; levels <= 200 && deep == 0; levels++ {
		var r tabletext.Report
		if !m.Read(m.New(m.Lookup("Node")), []byte(codecVerdictsDeep(levels)), &r) {
			deep = levels
		}
	}
	if deep < 2 {
		t.Fatalf("the oracle refuses no nesting up to 200 levels, or every one: %d", deep)
	}

	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	head := fmt.Sprintf(`
    std::panic::set_hook(Box::new(|_| {}));
    fn hex(b: &[u8]) -> String { b.iter().map(|x| format!("{:02x}", x)).collect() }
    const WIRE_DEFAULT: [u8; %d] = [%s];
    const WIRE_UNDER: [u8; %d] = [%s];
    const WIRE_DEP: [u8; %d] = [%s];
    const JSON_DEEP_OK: &str = %q;
    const JSON_DEEP_BAD: &str = %q;
`, len(o.defaultWire), rustBytes(o.defaultWire), len(o.underWire), rustBytes(o.underWire), len(o.depWire), rustBytes(o.depWire), codecVerdictsDeep(deep-1), codecVerdictsDeep(deep))
	body := head + `
    let d = Root::default();
    println!("born {} {}", d.few.len(), d.leaves.len());
    println!("save_default {}", hex(&root_save(&d)));
    let mut v = Root::default();
    v.few = vec![1, 2, 3];
    println!("save_few3 {}", hex(&root_save(&v)));
    let mut v = Root::default();
    v.v.x = 9;
    println!("save_vec {}", hex(&root_save(&v)));
    let mut rep = TableReport::default();
    println!("load_under {}", root_load(&WIRE_UNDER, &mut rep).is_some());
    let mut rep = TableReport::default();
    match root_load(&WIRE_DEP, &mut rep) {
        Some(x) => println!("dep_wire {:?} {:?} {}", x.ship, x.fleet, rep.unknown),
        None => println!("dep_wire refused {:?}", rep),
    }
    let mut rep = TableReport::default();
    match root_from_json("{\"ship\": \"Bomber\", \"fleet\": [\"Bomber\"]}", &mut rep) {
        Some(x) => println!("dep_json {:?} {:?} {}", x.ship, x.fleet, rep.unknown),
        None => println!("dep_json refused {:?}", rep),
    }
    let mut rep = TableReport::default();
    println!("deep_ok {}", node_from_json(JSON_DEEP_OK, &mut rep).is_some());
    let mut rep = TableReport::default();
    println!("deep_bad {}", node_from_json(JSON_DEEP_BAD, &mut rep).is_some());
    let mut rep = TableReport::default();
    rep.unknown = 5;
    rep.damaged = true;
    let ok = root_load(&WIRE_DEFAULT, &mut rep).is_some();
    println!("report_wire {} {} {}", ok, rep.unknown, rep.damaged);
    let mut rep = TableReport::default();
    rep.unknown = 5;
    rep.damaged = true;
    let ok = root_from_json("{}", &mut rep).is_some();
    println!("report_json {} {} {}", ok, rep.unknown, rep.damaged);
    let mut n = Root::default();
    n.score = f32::NAN;
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| root_to_json(&n))) {
        Ok(s) => println!("nan_text {:?}", s),
        Err(_) => println!("nan_text panicked"),
    }
    let mut b = Root::default();
    b.ship = Ship::Bomber;
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| root_save(&b))) {
        Ok(_) => println!("dep_save wrote"),
        Err(_) => println!("dep_save panicked"),
    }
`
	common := []string{
		"born 2 1",
		"save_default " + fmt.Sprintf("%x", o.defaultWire),
		"save_few3 " + fmt.Sprintf("%x", o.few3Wire),
		"save_vec " + fmt.Sprintf("%x", o.vecWire),
		"load_under false",
		"dep_wire Scout [None] 0",
		"dep_json Scout [None] 0",
		"deep_ok true",
		"deep_bad false",
		"report_wire true 0 false",
		"report_json true 0 false",
	}
	for _, mode := range []struct {
		name    string
		asserts bool
		want    []string
	}{
		{"debug-asserts", true, append(append([]string{}, common...), "nan_text panicked", "dep_save panicked")},
		{"no-debug-asserts", false, append(append([]string{}, common...), `nan_text ""`, "dep_save wrote")},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			out, err := runCodecVerdictsProbe(t, files, body, mode.asserts)
			if err != nil {
				t.Fatalf("the probe failed: %v\n%s", err, out)
			}
			got := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				k, _, _ := strings.Cut(line, " ")
				got[k] = line
			}
			for _, w := range mode.want {
				k, _, _ := strings.Cut(w, " ")
				if got[k] != w {
					t.Errorf("got %q, want %q", got[k], w)
				}
			}
		})
	}
}

// runCodecVerdictsProbe compiles the generated modules beside a main at
// opt-level 0 with no debug info (the Rust legs' test profile), with or
// without debug assertions, and runs it.
func runCodecVerdictsProbe(t *testing.T, files map[string][]byte, body string, asserts bool) (string, error) {
	t.Helper()
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" || name == TableEnumsModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs") ||
			strings.HasSuffix(name, "_table.rs") || strings.HasSuffix(name, "_variable.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables, unused_mut)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString("\nfn main() {\n")
	main.WriteString(body)
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	da := "off"
	if asserts {
		da = "on"
	}
	build := testproc.Command(t, "rustc", "--edition", "2024", "-C", "opt-level=0", "-C", "debuginfo=0", "-C", "debug-assertions="+da, "-A", "warnings", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return string(out), err
	}
	out, err := testproc.Command(t, bin).CombinedOutput()
	return string(out), err
}
