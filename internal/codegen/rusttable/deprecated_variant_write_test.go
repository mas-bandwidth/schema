package rusttable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func TestRustDeclaredEnumWriteWitness(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	fixture := filepath.Join("..", "..", "..", "testdata", "conformance", "feature-witnesses", "declared-deprecated-enum")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(fixture, "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := generate(t, string(schema))
	layoutRustUnit(t, dir, "depr", files)
	for _, module := range []string{"probe_cook.rs", "cook_runtime.rs"} {
		source := bytes.ReplaceAll(files[module], []byte("use crate::*;"), []byte("use super::*;"))
		if err := os.WriteFile(filepath.Join(dir, "depr", module), source, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(deprecatedEnumWitness), 0o600); err != nil {
		t.Fatal(err)
	}
	compile := testproc.Command(t, "rustc", "--edition", "2024", "--cfg", `feature="cook"`, "-A", "warnings", "-o", "run", "main.rs")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("native Rust compile: %v\n%s", err, out)
	}
	run := testproc.Command(t, filepath.Join(dir, "run"))
	if out, err := run.Output(); err != nil || string(out) != string(want) {
		t.Fatalf("Rust deprecated enum writer probe: %v\ngot:\n%swant:\n%s", err, out, want)
	}
}

const deprecatedEnumWitness = `
mod depr;
use depr::*;
#[path = "depr/cook_runtime.rs"]
mod cook_runtime;
use cook_runtime::*;
#[path = "depr/probe_cook.rs"]
mod probe_cook;
use probe_cook::*;

fn emit(id: &str, bytes: &[u8]) {
    print!("{id} ");
    for b in bytes {
        print!("{b:02x}");
    }
    println!();
}

fn check_refusal(why: &str, write: impl FnOnce()) {
    let err = std::panic::catch_unwind(std::panic::AssertUnwindSafe(write))
        .expect_err("deprecated variant must refuse");
    let message = err.downcast_ref::<String>().map(String::as_str)
        .or_else(|| err.downcast_ref::<&str>().copied());
    assert_eq!(message, Some(why));
}

fn refuse(id: &str, why: &str, write: impl FnOnce()) {
    check_refusal(why, write);
    println!("{id} refused");
}

fn main() {
    let mut variable = VarRoot::default();
    variable.phase = Phase::Next;
    emit("sf26_form1_next", &var_root_save(&variable));
    variable.phase = Phase::Old;
    refuse("sf26_form1_old", "invalid_value: Phase.Old is deprecated", || {
        var_root_save(&variable);
    });

    let mut row = FixedRootRow { phase: 3, ..FixedRootRow::default() };
    let size = fixed_root_fixed_measure(1);
    let mut out = vec![0; size];
    assert_eq!(fixed_root_fixed_save(std::slice::from_ref(&row), &mut out), Some(size));
    emit("sf26_form3_next", &out);
    row.phase = 2;
    refuse("sf26_form3_old", "invalid_value: phase holds a deprecated variant", || {
        fixed_root_fixed_save(std::slice::from_ref(&row), &mut out);
    });

    let mut fixed = FixedRoot::default();
    fixed.phase = Phase::Next;
    row.phase = 3;
    let size = fixed_root_cook_measure(&row);
    assert!(matches!(fixed_root_cook_measure_reason(&row), (n, TableCookRefuse::Ok) if n == size));
    println!("sf26_cook_measure_next {size}");
    let size = size as usize;
    for (id, order) in [("sf26_cook_next_le", 1), ("sf26_cook_next_be", 2)] {
        let bytes = fixed_root_cook_write(&fixed, order).unwrap();
        assert_eq!(size, bytes.len());
        let mut raw = vec![0; size];
        assert!(fixed_root_cook_from(&row, &mut raw, order));
        assert_eq!(raw, bytes);
        emit(id, &bytes);
    }
    fixed.phase = Phase::Old;
    row.phase = 2;
    let old_size = fixed_root_cook_measure(&row);
    assert!(matches!(fixed_root_cook_measure_reason(&row), (n, TableCookRefuse::Ok) if n == old_size));
    println!("sf26_cook_measure_old {old_size}");
    let why = "invalid_value: FixedRoot.phase holds deprecated variant Phase.Old";
    for (id, order) in [("sf26_cook_old_le", 1), ("sf26_cook_old_be", 2)] {
        let mut raw = vec![0x5a; size];
        check_refusal(why, || {
            fixed_root_cook_from(&row, &mut raw, order);
        });
        assert_eq!(raw, vec![0x5a; size]);
        refuse(id, "invalid_value: Phase.Old is deprecated", || {
            fixed_root_cook_write(&fixed, order).ok();
        });
    }
}
`
