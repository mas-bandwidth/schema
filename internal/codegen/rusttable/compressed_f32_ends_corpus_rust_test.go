package rusttable

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3),
// through the GENERATED Rust: the cross-port corpus
// testdata/conformance/feature-witnesses/compressed-f32-ends, every cases.tsv
// row compiled into one native probe and run. The value at each float32 end
// (0.1f, -0.3f) reads, saves and reads back to the same bytes in the variable,
// fixed and JSON forms; one float32 step past either end is refused by every
// reader and every writer. This leg's write checks are debug asserts, so the
// probe is built with debug assertions on and a writer's refusal is its
// assert firing. Beside the rows, each write row also asks the JSON writer for
// the same verdict, and the cook writer for acceptance at the ends.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const rustF32EndsCorpus = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// rustF32EndsHelpers are the probe's readers and writers, one per root and
// form; None is the refusal, and a writer's debug assert firing is one too.
const rustF32EndsHelpers = `
std::panic::set_hook(Box::new(|_| {}));
fn quiet<T>(f: impl FnOnce() -> Option<T>) -> Option<T> { std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)).ok().flatten() }
fn json_v(s: &str) -> Option<V> { let mut r = TableReport::default(); let v = v_from_json(s, &mut r)?; if r.damaged || r.refused { None } else { Some(v) } }
fn json_f(s: &str) -> Option<F> { let mut r = TableReport::default(); let v = f_from_json(s, &mut r)?; if r.damaged || r.refused { None } else { Some(v) } }
fn wire_v(b: &[u8]) -> Option<V> { let mut r = TableReport::default(); let v = v_load(b, &mut r)?; if r.damaged || r.refused { None } else { Some(v) } }
fn wire_f(b: &[u8]) -> Option<F> {
    let mut rows = [FRow::default()];
    let mut plan = vec![TableFixedEntry::default(); 64];
    let mut remap = vec![0u16; 64];
    let mut r = TableFixedReport::default();
    if f_fixed_load(&mut rows, b, &mut plan, &mut remap, &mut r)? != 1 { return None; }
    F::try_from(&rows[0]).ok()
}
fn save_v(v: &V) -> Option<Vec<u8>> { quiet(|| v_save_checked(v).ok()) }
fn save_f(v: &F) -> Option<Vec<u8>> { quiet(|| { let mut buf = vec![0u8; f_fixed_measure(1)]; let n = f_fixed_save(&[<FRow as From<&F>>::from(v)], &mut buf)?; buf.truncate(n); Some(buf) }) }
fn text_v(v: &V) -> bool { quiet(|| v_to_json_checked(v)).is_some() }
fn text_f(v: &F) -> bool { quiet(|| f_to_json_checked(v)).is_some() }
fn cook_v(v: &V) -> bool { quiet(|| v_cook_write(v, 1).ok()).is_some() }
fn cook_f(v: &F) -> bool { quiet(|| f_cook_write(v, 1).ok()).is_some() }
fn row(name: &str, why: Option<String>) { match why { None => println!("ROW {name} PASS"), Some(w) => println!("ROW {name} FAIL {w}") } }
`

// TestRustCompressedF32EndsCorpus runs every cases.tsv row through the
// generated Rust and reads one verdict line per row back.
func TestRustCompressedF32EndsCorpus(t *testing.T) {
	slowtest.Gate(t, "rustc (it compiles and runs the compressed-f32-ends corpus probe)")
	keyedRequireRustc(t)
	file := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(rustF32EndsCorpus, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	u := unitFrom(t, string(file("Probe.schema")))
	var b strings.Builder
	b.WriteString(rustF32EndsHelpers)
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	var names []string
	for line := range strings.SplitSeq(string(file("cases.tsv")), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv: %q has %d fields, want 8", line, len(f))
		}
		name, op, form, root, input, path, bits, expect := f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]
		own := formOf[root]
		if own == "" || (op != "read" || form != "json") && form != own {
			t.Fatalf("%s: form %s under root %s", name, form, root)
		}
		names = append(names, name)
		r := strings.ToLower(root)
		var want string
		if expect != "refused" {
			want = rustByteVector(file(expect + "." + own + ".bin"))
		}
		switch op {
		case "read":
			load := fmt.Sprintf("json_%s(%q)", r, file(input+".json"))
			if form != "json" {
				load = fmt.Sprintf("wire_%s(&%s)", r, rustByteVector(file(input+"."+form+".bin")))
			}
			if expect == "refused" {
				fmt.Fprintf(&b, "row(%q, if %s.is_none() { None } else { Some(String::from(\"the %s read was not refused\")) });\n", name, load, form)
				continue
			}
			fmt.Fprintf(&b, "row(%q, match %s { None => Some(String::from(\"the %s read refused the value at the float32 end\")), Some(x) => match save_%s(&x) { None => Some(String::from(\"saving again what was just read was refused\")), Some(got) => if got == vec!%s { None } else { Some(format!(\"the %s save of what was read: have {got:?}\")) } } });\n", name, load, form, r, want, own)
		case "write":
			n, err := strconv.ParseUint(bits, 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			lhs := map[string]string{"r": "x.r", "xs[0]": "x.xs[0]"}[path]
			if lhs == "" {
				t.Fatalf("%s: path %q", name, path)
			}
			fmt.Fprintf(&b, "{ let mut x = json_%s(%q).expect(\"%s.json\"); %s = f32::from_bits(%#x); let got = save_%s(&x); let text = text_%s(&x); let cook = cook_%s(&x); println!(\"INFO %s cook {cook}\"); ", r, file(input+".json"), input, lhs, n, r, r, r, name)
			if expect == "refused" {
				fmt.Fprintf(&b, "row(%q, if got.is_some() { Some(format!(\"the %s save was not refused: {got:?}\")) } else if text { Some(String::from(\"past the end: the JSON writer accepted it\")) } else { None }); }\n", name, form)
				continue
			}
			fmt.Fprintf(&b, "row(%q, match got { None => Some(String::from(\"the %s save was refused\")), Some(_) if !text || !cook => Some(format!(\"at the end: the JSON writer accepted {text}, the cook writer accepted {cook}; both owe acceptance\")), Some(got) => if got == vec!%s { None } else { Some(format!(\"the %s save: have {got:?}\")) } }); }\n", name, form, want, form)
		default:
			t.Fatalf("%s: op %q", name, op)
		}
	}
	if len(names) != 18 {
		t.Fatalf("cases.tsv: %d rows, want 18", len(names))
	}
	modules, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", modules)
	// The fixed record's conversions to and from the text/cook value ride in
	// <base>_fixed_values.rs, which the shared layout leaves out.
	for file, body := range modules {
		if !strings.HasSuffix(file, "_fixed_values.rs") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "probe", file), body, 0o600); err != nil {
			t.Fatal(err)
		}
		mod, err := os.OpenFile(filepath.Join(dir, "probe", "mod.rs"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = fmt.Fprintf(mod, "#[allow(dead_code, unused_imports)]\nmod %s;\n", strings.TrimSuffix(file, ".rs"))
		if cerr := mod.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	main := "#![allow(dead_code, unused_imports)]\nmod probe;\nuse probe::*;\nfn main() {\n" + b.String() + "\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, "rustc", "--edition", "2024", "--cfg", `feature="cook"`, "-O", "-C", "debug-assertions=on", "-A", "warnings", "-o", "run", "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Rust compile: %v\n%s", err, out)
	}
	cmd = testproc.Command(t, filepath.Join(dir, "run"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	t.Logf("%s", out)
	for _, name := range names {
		if !strings.Contains(string(out), "ROW "+name+" PASS\n") {
			t.Errorf("%s: not PASS", name)
		}
	}
}
