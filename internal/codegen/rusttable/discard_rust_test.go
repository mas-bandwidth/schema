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

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rustDiscardUnit has a fixed root and a variable root, each carrying a nested
// body between two scalars, so damage inside the nested body leaves the walk
// able to read on past its L: the case where a reader that kept what it
// decoded would hand back a value. The C++ reference's gate
// (TestDamageDiscardsTheWholeLoadCPP) reads the same shape.
const rustDiscardUnit = `package discard

fixed table Inner
{
    ok bool = false
    n  int32 = 0
}

fixed table Root
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
}

table Graph
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

// rustDiscardWire encodes the text as the root and then damages the nested
// body: Inner's int32 field, kind 4 holding 3, gets the kind 37 no reader can
// skip. The engine is asked first, so the vector is damaged by the rule and not
// by assumption.
func rustDiscardWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return wire
}

// TestDamageDiscardsTheWholeLoadRust is the Rust leg's gate for DAMAGE
// DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): every public load entry
// point of the Rust leg, given damaged input, answers no usable value with
// `malformed` set. The variable wire's tolerant load of a fixed root and of a
// variable root answers None, the retaining load answers None with both of the
// retention store's stores empty, the fixed form's load answers None with
// every caller record back at its declared defaults, the message batch answers
// no bodies with every caller record back at its defaults, and the text read
// answers None. Red if any read hands back what it decoded.
func TestDamageDiscardsTheWholeLoadRust(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	u := unitFrom(t, rustDiscardUnit)
	fixed := rustDiscardWire(t, u, "Root", `{"a":5,"inner":{"ok":true,"n":3},"b":9}`)
	variable := rustDiscardWire(t, u, "Graph", `{"a":5,"inner":{"ok":true,"n":3},"b":9}`)
	announce := ir.TableAnnouncement(u)
	body := fmt.Sprintf(`
    const FIXED: [u8; %d] = [%s];
    const VARIABLE: [u8; %d] = [%s];
    const ANN: [u8; %d] = [%s];
    let mut bad = Vec::new();

    // the variable wire, a fixed root: the walk reads on past the damaged body
    let mut r = TableReport::default();
    match root_load(&FIXED, &mut r) {
        Some(v) => bad.push(format!("root: load handed back a value on damage: a={} b={}", v.a, v.b)),
        None if !r.malformed => bad.push("root: the report does not say malformed".to_string()),
        None => {}
    }

    // the variable wire, a variable root
    let mut r = TableReport::default();
    match graph_load(&VARIABLE, &mut r) {
        Some(v) => bad.push(format!("graph: load handed back a value on damage: a={} b={}", v.a, v.b)),
        None if !r.malformed => bad.push("graph: the report does not say malformed".to_string()),
        None => {}
    }

    // the retaining read: no value, and the store keeps nothing
    for (name, wire) in [("root", &FIXED[..]), ("graph", &VARIABLE[..])] {
        let mut store = TableRetain { bytes: Vec::with_capacity(1 << 12), ids: Vec::new() };
        let mut r = TableReport::default();
        let got = if name == "root" { root_load_retain(wire, &mut store, &mut r).is_some() } else { graph_load_retain(wire, &mut store, &mut r).is_some() };
        if got { bad.push(format!("{name} retain: load_retain handed back a value on damage")); }
        if !r.malformed { bad.push(format!("{name} retain: the report does not say malformed")); }
        if !store.bytes.is_empty() || !store.ids.is_empty() {
            bad.push(format!("{name} retain: the store kept {} bytes and {} ids", store.bytes.len(), store.ids.len()));
        }
    }

    // the fixed form: two whole records and a cut-short third
    let rows = [RootRow { a: 5, inner: InnerRow { ok: true, n: 3 }, b: 9 }, RootRow { a: 6, inner: InnerRow { ok: true, n: 4 }, b: 10 }];
    let mut file = vec![0u8; root_fixed_measure(2)];
    if root_fixed_save(&rows, &mut file).is_none() { bad.push("fixed: the save refused".to_string()); }
    file.extend_from_slice(&[1, 2, 3]);
    let mut values = vec![RootRow { a: 77, inner: InnerRow { ok: true, n: 77 }, b: 77 }; 4];
    let mut plan = [TableFixedEntry::default(); 64];
    let mut remap = [0u16; 64];
    let mut r = TableFixedReport::default();
    if root_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut r).is_some() { bad.push("fixed: fixed_load answered records on damage".to_string()); }
    if !r.malformed { bad.push("fixed: the report does not say malformed".to_string()); }
    if values.iter().any(|v| v.a != 0 || v.b != 0 || v.inner.n != 0 || v.inner.ok) {
        bad.push(format!("fixed: a damaged load left a caller record holding a={} b={} n={}", values[0].a, values[0].b, values[0].inner.n));
    }

    // the message batch: a reference above the entry count in the first body
    let mut buf = vec![0u8; 256];
    let mut sr = TableReport::default();
    let wrote = root_save_messages(&rows, &mut buf, &mut sr);
    if wrote < 3 { bad.push(format!("message: the save wrote {wrote}")); }
    let mut batch = buf[..wrote.max(3) as usize].to_vec();
    for b in batch.iter_mut().skip(2) { *b = 0xff; }
    let mut storage = vec![TableMessageEntry::default(); 4096];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut ar = TableReport::default();
    if !announce_read(&mut vocab, &ANN, &mut ar) { bad.push(format!("message: the announcement did not read: {ar:?}")); }
    let mut values = vec![RootRow { a: 77, inner: InnerRow { ok: true, n: 77 }, b: 77 }; TABLE_MESSAGE_BATCH_MAX];
    let mut r = TableReport::default();
    let (count, ok) = root_load_messages(&mut values, &vocab, &batch, &mut r);
    if ok || count != 0 { bad.push(format!("message: load_messages answered ok={ok} count={count} on damage")); }
    if !r.malformed { bad.push("message: the report does not say malformed".to_string()); }
    if values[..2].iter().any(|v| v.a != 0 || v.b != 0 || v.inner.n != 0) {
        bad.push(format!("message: a damaged batch left a body holding a={} b={}", values[0].a, values[0].b));
    }

    // the text read: a document cut short
    let mut r = TableReport::default();
    if graph_from_json("{\"a\":5,\"inner\":{\"n\":3},\"b\":", &mut r).is_some() { bad.push("text: from_json handed back a value on damage".to_string()); }
    if !r.malformed { bad.push("text: the report does not say malformed".to_string()); }

    if bad.is_empty() { println!("DISCARD ok"); } else { for b in &bad { println!("DISCARD BAD {b}"); } std::process::exit(1); }
`, len(fixed), rustBytes(fixed), len(variable), rustBytes(variable), len(announce), rustBytes(announce))
	out, err := runRustDiscardProbe(t, u, body)
	if err != nil || !strings.Contains(out, "DISCARD ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// runRustDiscardProbe lays the unit's wire modules out as one flat crate (the
// variable, fixed and message codecs and their runtimes, as every other Rust
// probe here does) beside a main that runs body.
func runRustDiscardProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
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
	build := exec.Command("rustc", "--edition", "2024", "-O", "-A", "warnings", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}
