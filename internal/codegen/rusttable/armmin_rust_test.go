package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// R10 (docs/SPEC-TABLES.md section 3.3): a counted [A..B] holds A wherever
// the field sits, a union arm and a plain field alike. The Rust leg carries
// the construct (it once refused it, so the conformance roots MinArmFix and
// MinPlainFix did not build), a plain field is born at A default elements and
// elides there, and A holds on every form the shared rows cannot reach: the
// variable wire read, and the write side, which in Rust is a debug assert
// (variable save, fixed save, message save) or the cook writer's refusal. The
// probe is built with debug assertions on so the asserts fire. No real time,
// no sockets: the generated unit is built in a temporary directory.
func TestRustCarriesAndHoldsACountedArmMinimum(t *testing.T) {
	slowtest.Gate(t, "rustc (the generated counted-minimum probe)")
	u := unitFrom(t, `package armmin

union Pick
{
    few   [2..4]uint32
    other int32
}

fixed table ArmFix { pick Pick }

fixed table PlainFix { few [2..4]uint32 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" || name == TableEnumsModule+".rs" || name == "cook_runtime.rs" ||
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
	main.WriteString(`
fn panics(f: impl FnOnce() + std::panic::UnwindSafe) -> bool {
    std::panic::catch_unwind(f).is_err()
}

// the array's payload as the variable writer lays it: L, the element kind,
// the count, the elements, after the array kind (an arm's, or a field's)
fn patch_count(wire: &[u8], keep: usize) -> Vec<u8> {
    let arr: [u8; 11] = [10, 8, 2, 7, 0, 0, 0, 9, 0, 0, 0];
    let at = wire.windows(arr.len()).position(|w| w == arr).expect("the [7,9] payload");
    assert_eq!(wire[at - 1], 14, "the array kind");
    let mut out = wire[..at].to_vec();
    out.push((2 + 4 * keep) as u8);
    out.push(8);
    out.push(keep as u8);
    out.extend_from_slice(&arr[3..3 + 4 * keep]);
    out.extend_from_slice(&wire[at + arr.len()..]);
    out
}

fn main() {
    let mut failures = 0;
    macro_rules! check {
        ($ok:expr, $name:expr) => { if !$ok { eprintln!("FAIL: {}", $name); failures += 1; } };
    }
    // the variable wire: a count below A refuses, A itself reads
    let arm_good = arm_fix_save(&ArmFix { pick: Pick::Few(vec![7, 9]) });
    let plain_good = plain_fix_save(&PlainFix { few: vec![7, 9] });
    let mut report = TableReport::default();
    check!(matches!(arm_fix_load(&arm_good, &mut report), Some(ArmFix { pick: Pick::Few(ref x) }) if *x == vec![7, 9]), "arm wire count 2 reads");
    let mut report = TableReport::default();
    check!(matches!(plain_fix_load(&plain_good, &mut report), Some(ref v) if v.few == vec![7, 9]), "plain wire count 2 reads");
    for keep in [0usize, 1] {
        let mut report = TableReport::default();
        check!(arm_fix_load(&patch_count(&arm_good, keep), &mut report).is_none(), format!("arm wire count {} refuses", keep));
        let mut report = TableReport::default();
        check!(plain_fix_load(&patch_count(&plain_good, keep), &mut report).is_none(), format!("plain wire count {} refuses", keep));
    }
    // a plain field is born at A default elements, elides there, and an
    // absent field reads it
    check!(PlainFix::default().few == vec![0, 0], "plain born at A");
    let born = plain_fix_save(&PlainFix::default());
    check!(born.windows(2).all(|w| w != [8, 2]), "plain birth value elides");
    let mut report = TableReport::default();
    check!(matches!(plain_fix_load(&born, &mut report), Some(ref v) if v.few == vec![0, 0]), "plain absent reads A defaults");
    let mut report = TableReport::default();
    check!(matches!(plain_fix_from_json_bytes(b"{}", &mut report), Some(ref v) if v.few == vec![0, 0]), "plain json absent reads A defaults");
    // the text form
    for (few, ok) in [("[7]", false), ("[]", false), ("[7,9]", true), ("[1,2,3,4]", true)] {
        let mut report = TableReport::default();
        let text = format!("{{\"pick\":{{\"few\":{}}}}}", few);
        check!(arm_fix_from_json_bytes(text.as_bytes(), &mut report).is_some() == ok, format!("arm json {}", text));
        let mut report = TableReport::default();
        let text = format!("{{\"few\":{}}}", few);
        check!(plain_fix_from_json_bytes(text.as_bytes(), &mut report).is_some() == ok, format!("plain json {}", text));
    }
    // the write side: a debug assert in every form, the cook writer refuses;
    // the expected panics print nothing
    std::panic::set_hook(Box::new(|_| {}));
    for (n, below) in [(0usize, true), (1, true), (2, false), (4, false)] {
        let elems: Vec<u32> = (1..=n as u32).collect();
        let v = ArmFix { pick: Pick::Few(elems.clone()) };
        let w = v.clone();
        check!(panics(move || { let _ = arm_fix_save(&w); }) == below, format!("arm variable save count {}", n));
        check!(arm_fix_cook_write(&v, 1).is_err() == below, format!("arm cook write count {}", n));
        let mut row = ArmFixRow::default();
        let mut arm = PickRowArmFew::default();
        arm.value_count = n as i32;
        row.pick.set_few(arm);
        let r = row.clone();
        check!(panics(move || { let mut b = vec![0u8; arm_fix_fixed_measure(1)]; let _ = arm_fix_fixed_save(&[r], &mut b); }) == below, format!("arm fixed save count {}", n));
        let r = row.clone();
        check!(panics(move || { let mut b = vec![0u8; 4096]; let mut rep = TableReport::default(); let _ = arm_fix_save_messages(&[r], &mut b, &mut rep); }) == below, format!("arm message save count {}", n));

        let p = PlainFix { few: elems.clone() };
        let q = p.clone();
        check!(panics(move || { let _ = plain_fix_save(&q); }) == below, format!("plain variable save count {}", n));
        check!(plain_fix_cook_write(&p, 1).is_err() == below, format!("plain cook write count {}", n));
        let mut row = PlainFixRow::default();
        row.few_count = n as i32;
        let r = row.clone();
        check!(panics(move || { let mut b = vec![0u8; plain_fix_fixed_measure(1)]; let _ = plain_fix_fixed_save(&[r], &mut b); }) == below, format!("plain fixed save count {}", n));
        let r = row.clone();
        check!(panics(move || { let mut b = vec![0u8; 4096]; let mut rep = TableReport::default(); let _ = plain_fix_save_messages(&[r], &mut b, &mut rep); }) == below, format!("plain message save count {}", n));
    }
    if failures != 0 { std::process::exit(1); }
}
`)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	build := testproc.Command(t, "rustc", "--edition", "2024", "-O", "-C", "debug-assertions=on", "--cfg", `feature="cook"`, "-A", "warnings", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	if out, err := testproc.Command(t, bin).CombinedOutput(); err != nil {
		t.Fatalf("arm-minimum probe: %v\n%s", err, out)
	}
}
