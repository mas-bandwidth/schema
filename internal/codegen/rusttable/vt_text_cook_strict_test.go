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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The STRICT-100 cells vt/text-form/rust, vt/cook-write/rust and
// vt/widening/rust over one generated unit: every read is the one strict read
// (the whole value, or a refusal that leaves nothing), through the entry point
// each cell owns (docs/SPEC-TABLES.md §3.3, §3.4, §4, §7.6).
//
//   - text-form: a fixed root's JSON read is strict — a duplicate key, known,
//     unknown, or inside a skipped object, refused or not, refuses the whole
//     read — and the fixed root saves as form 3, the strict fixed reader
//     refusing a form-1 file as previous_form.
//   - cook-write: the cook of the value opens back to the same value, the two
//     byte orders differ, and a buffer short of the measure writes nothing.
//   - widening: a wire written by a schema whose field kind GREW past the
//     declaration is refused whole (kind_mismatch), exactly as the oracle
//     refuses it.

const vtTextCookWideningSchema = `package probe

fixed table Root
{
    n int32
    label string(8)
    values [..3]int32
}

table Sim
{
    angle int32
}
`

// wideningWriterSchema is the same Sim as an EARLIER writer carried, with the
// field one integer kind wider. The reader in vtTextCookWideningSchema must
// refuse that wire whole, and the oracle does.
const wideningWriterSchema = `package probe

table Sim
{
    angle int64
}
`

// runVtTextCookWidening generates the unit, applies edit to the generated
// files (the negative controls), lays the modules out, compiles the probe with
// rustc and runs it. The probe asserts every cell holds; a failure is a
// nonzero run or a panic.
func runVtTextCookWidening(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()

	modules, err := Generate(unitFrom(t, vtTextCookWideningSchema))
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(modules)
	}

	// THE WIDENING WIRE, from the earlier writer and read by the oracle: the
	// oracle must refuse it, and the probe reads the same bytes through the
	// generated reader and must refuse them too.
	writerU := unitFrom(t, wideningWriterSchema)
	wm := tabletext.NewModel(writerU)
	wv := wm.New(wm.Lookup("Sim"))
	f, _ := wv.FieldByKey("angle")
	f.Cell.I = 3
	wire, err := tablewire.Encode(wm, wv)
	if err != nil {
		t.Fatal(err)
	}
	rm := tabletext.NewModel(unitFrom(t, vtTextCookWideningSchema))
	rv := rm.New(rm.Lookup("Sim"))
	var oracleRep tabletext.Report
	if ok, err := tablewire.Decode(rm, rv, wire, &oracleRep); ok || err == nil {
		t.Fatalf("the oracle accepted the widened wire; the control proves nothing (ok=%v err=%v)", ok, err)
	}

	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "widen.bin"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	var mods strings.Builder
	for _, name := range names {
		if !strings.HasSuffix(name, ".rs") {
			continue
		}
		if err := os.WriteFile(filepath.Join(work, name), modules[name], 0o600); err != nil {
			t.Fatal(err)
		}
		mod := strings.TrimSuffix(name, ".rs")
		fmt.Fprintf(&mods, "pub mod %s; pub use %s::*;\n", mod, mod)
	}

	main := `#![allow(dead_code, unused_imports, unused_variables)]
` + mods.String() + `
fn main() {
    // ---- text-form: the strict JSON read ----
    {
        let mut report = TableReport::default();
        let v = root_from_json("{\"n\":7,\"label\":\"hi\",\"values\":[1,2]}", &mut report)
            .expect("text read refused");
        assert_eq!(v.n, 7, "text value n");
        assert_eq!(v.label, "hi", "text value label");
        assert_eq!(v.values, vec![1, 2], "text value values");
        let text = root_to_json(&v);
        assert!(text.contains("\"label\": \"hi\""), "text write: {}", text);
        // a duplicate key, known or unknown, plain or escaped, inside a skipped
        // object or not, refuses the whole read (docs/SPEC-TABLES.md §16.3)
        for dup in [
            "{\"n\":1,\"n\":2}",
            "{\"zz\":1,\"zz\":2}",
            "{\"zz\":{\"a\":1,\"a\":2}}",
            "{\"zz\":{\"a\":1,\"\\u0061\":2}}",
        ] {
            let mut r = TableReport::default();
            assert!(root_from_json(dup, &mut r).is_none(), "duplicate key accepted: {}", dup);
            assert!(r.damaged || r.refused, "duplicate key not refused: {}", dup);
        }
    }

    // ---- text-form: a fixed root saves as form 3, and the strict fixed
    // reader refuses a form-1 file as previous_form ----
    {
        let mut r = TableReport::default();
        let v = root_from_json("{\"n\":7,\"label\":\"hi\",\"values\":[1,2]}", &mut r)
            .expect("text read for the fixed save");
        let form1 = root_save(&v);
        assert_eq!(form1[0], TABLE_WIRE_FORM, "the variable save is not form 1");
        let mut row = RootRow::default();
        row.n = 7;
        row.label[..2].copy_from_slice(b"hi");
        row.label_length = 2;
        row.values[0] = 1;
        row.values[1] = 2;
        row.values_count = 2;
        let mut fb = vec![0u8; root_fixed_measure(1)];
        assert_eq!(root_fixed_save(&[row.clone()], &mut fb), Some(fb.len()), "fixed save");
        assert_eq!(fb[0], TABLE_FIXED_FORM, "fixed form byte");
        let mut out = vec![RootRow::default(); 1];
        let mut plan = vec![TableFixedEntry::default(); 64];
        let mut remap = vec![0u16; 64];
        let mut fr = TableFixedReport::default();
        assert_eq!(root_fixed_load(&mut out, &form1, &mut plan, &mut remap, &mut fr), None,
            "the strict fixed reader accepted a form-1 file");
        assert_eq!(fr.reason, TableFixedReason::PreviousForm, "form-1 refusal reason");
    }

    // ---- cook-write: the cooked value opens back, orders differ, and a
    // buffer short of the measure writes nothing ----
    {
        let mut row = RootRow::default();
        row.n = 7;
        row.label[..2].copy_from_slice(b"hi");
        row.label_length = 2;
        row.values[0] = 1;
        row.values[1] = 2;
        row.values_count = 2;
        let measure = root_cook_measure(&row);
        assert!(measure > 0, "cook measure");
        let mut buf = vec![0u8; measure as usize];
        assert!(root_cook_from(&row, &mut buf, 1), "cook little refused");
        let mut buf_be = vec![0u8; measure as usize];
        assert!(root_cook_from(&row, &mut buf_be, 2), "cook big refused");
        assert_ne!(buf, buf_be, "the two byte orders cooked to one file");
        let mut short = vec![0xa5u8; measure as usize];
        assert!(!root_cook_from(&row, &mut short[..(measure - 1) as usize], 1),
            "short cook wrote or was accepted");
        assert!(short.iter().all(|b| *b == 0xa5), "short cook touched the buffer");
        let cook = unsafe { RootCook::open(buf.as_ptr(), buf.len() as u64) }
            .expect("cook open refused");
        assert_eq!(unsafe { (*cook.root()).n }, 7, "cook open value n");
        assert_eq!(unsafe { (*cook.root()).values_count }, 2, "cook open values_count");
    }

    // ---- widening: a wire the earlier, wider writer wrote is refused whole ----
    {
        let wire = std::fs::read("widen.bin").unwrap();
        let mut r = TableReport::default();
        assert!(sim_load(&wire, &mut r).is_none(), "the widened wire was read");
        assert!(r.refused || r.damaged, "the widened wire was not refused");
    }

    println!("TEXTCOOKWIDENING DONE");
}
`
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("rustc", "--edition", "2024", "--cfg", `feature="cook"`, "-O", "-A", "warnings", "-o", "run", "main.rs")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		return out, err
	}
	cmd = exec.Command(filepath.Join(work, "run"))
	cmd.Dir = work
	return cmd.CombinedOutput()
}

func TestRustTextCookWideningStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated text/cook/widening probe)")
	keyedRequireRustc(t)
	out, err := runVtTextCookWidening(t, nil)
	if err != nil {
		t.Fatalf("the text/cook/widening cells do not hold: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "TEXTCOOKWIDENING DONE") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}

// sabotageRustGenerated applies each replacement to every generated Rust file
// and fails the test when none of them matched anything, so a renamed emitter
// line cannot leave a control vacuous.
func sabotageRustGenerated(t *testing.T, files map[string][]byte, what string, pairs ...[2]string) {
	t.Helper()
	patched := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".rs") {
			continue
		}
		s := string(data)
		for _, pair := range pairs {
			if next := strings.ReplaceAll(s, pair[0], pair[1]); next != s {
				patched++
				s = next
			}
		}
		files[name] = []byte(s)
	}
	if patched == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage patched nothing", what)
	}
}

// requireRustRed holds a control's verdict: the run went red, and on the line
// the gate owes that fault rather than on a compile error or another clause.
func requireRustRed(t *testing.T, what string, out []byte, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s left the gate GREEN\n%s", what, out)
	}
	if !bytes.Contains(out, []byte(want)) {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s went red, but not on %q\n%s", what, want, out)
	}
}

// TestRustTextCookWideningStrictNegativeControl plants each fault in a copy of
// the real generated artifact: the fixed writer's form byte is planted back to
// the variable form, and the cook's short-buffer guard is blinded. Both must
// turn the strict gate red on its own message.
func TestRustTextCookWideningStrictNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated text/cook/widening probe)")
	keyedRequireRustc(t)
	t.Run("form", func(t *testing.T) {
		t.Parallel()
		out, err := runVtTextCookWidening(t, func(files map[string][]byte) {
			sabotageRustGenerated(t, files, "fixed form byte", [2]string{"buffer[0] = TABLE_FIXED_FORM;", "buffer[0] = 1;"})
		})
		requireRustRed(t, "the fixed writer's form byte planted to the variable form", out, err, "fixed form byte")
	})
	t.Run("cook", func(t *testing.T) {
		t.Parallel()
		out, err := runVtTextCookWidening(t, func(files map[string][]byte) {
			sabotageRustGenerated(t, files, "cook length guard", [2]string{"if (buffer.len() as i64) < 120 {\n            return false;\n        }", "if false {\n            return false;\n        }"})
		})
		requireRustRed(t, "the cook short-buffer guard blinded", out, err, "short cook")
	})
}
