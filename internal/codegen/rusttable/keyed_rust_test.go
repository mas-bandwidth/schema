package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE KEYED ARRAY INDEXES BY KEY, REFUSED AT BOTH ENDS (docs/PORTING.md M5,
// docs/SPEC-TABLES.md §2.3). tables/examples' HullConfig carries
// `turrets [Weapon]TurretConfig`; the probe below is compiled against the
// crate this generator emits, OPTIMIZED (rustc -O, no debug assertions), and
// run. Four clauses, each printed by name:
//
//	NONE   key 0 (Weapon::None, or the raw 0) is refused by a panic that names
//	       the key, on the variable struct's Vec and on the fixed row's array
//	MAX    a key past E.Max is refused the same way, and E.Max itself is not
//	SHIFT  key k lives in slot k-1, through the raw and the typed accessor
//	ITER   iteration yields each key beside the slot it owns, in key order
//
// A refusal that is the slice's own bounds check names an index and not a
// key, so it does not pass: the panic message is part of the clause.
const keyedProbe = `#![allow(dead_code)]
mod tabledemo;
use tabledemo::*;
use std::panic;

fn refused<F: FnOnce() + panic::UnwindSafe>(key: usize, f: F) -> bool {
    match panic::catch_unwind(f) {
        Ok(()) => false,
        Err(payload) => {
            let text = payload.downcast_ref::<String>().cloned()
                .or_else(|| payload.downcast_ref::<&str>().map(|s| s.to_string()))
                .unwrap_or_default();
            text.contains(&format!("keyed array: key {key} names no slot"))
        }
    }
}

fn main() {
    panic::set_hook(Box::new(|_| {}));
    let hull = HullConfig::default();
    let row = HullConfigRow::default();
    let mut bad = Vec::new();

    // NONE
    let none = refused(0, || { let _ = Weapon::None.keyed(&hull.turrets); })
        && refused(0, || { let _ = table_keyed(&hull.turrets, 0); })
        && refused(0, || { let _ = table_keyed(&row.turrets, 0); })
        && refused(0, || { let mut r = HullConfigRow::default(); let _ = Weapon::None.keyed_mut(&mut r.turrets); });
    if !none { bad.push("NONE"); }

    // MAX
    let n = hull.turrets.len();
    let max = n == 3
        && refused(n + 1, || { let _ = table_keyed(&hull.turrets, n + 1); })
        && refused(n + 1, || { let _ = table_keyed(&row.turrets, n + 1); })
        && refused(usize::MAX, || { let _ = table_keyed(&row.turrets, usize::MAX); })
        && panic::catch_unwind(|| { let _ = table_keyed(&row.turrets, n); }).is_ok()
        && panic::catch_unwind(|| { let _ = Weapon::Mine.keyed(&hull.turrets); }).is_ok();
    if !max { bad.push("MAX"); }

    // SHIFT
    let mut v = HullConfig::default();
    let mut r = HullConfigRow::default();
    for key in 1..=3usize {
        table_keyed_mut(&mut v.turrets, key).damage = key as f32;
    }
    Weapon::Cannon.keyed_mut(&mut r.turrets).damage = 1.0;
    Weapon::Missile.keyed_mut(&mut r.turrets).damage = 2.0;
    Weapon::Mine.keyed_mut(&mut r.turrets).damage = 3.0;
    let shift = (0..3).all(|i| v.turrets[i].damage == (i + 1) as f32 && r.turrets[i].damage == (i + 1) as f32)
        && Weapon::Missile.keyed(&v.turrets).damage == 2.0
        && table_keyed(&r.turrets, 3).damage == 3.0;
    if !shift { bad.push("SHIFT"); }

    // ITER, over slots filled by position, so it stands apart from SHIFT
    let mut v = HullConfig::default();
    let mut r = HullConfigRow::default();
    for i in 0..3 {
        v.turrets[i].damage = (i + 1) as f32;
        r.turrets[i].damage = (i + 1) as f32;
    }
    let typed: Vec<(Weapon, f32)> = Weapon::keyed_iter(&v.turrets).map(|(k, t)| (k, t.damage)).collect();
    let raw: Vec<(usize, f32)> = table_keyed_iter(&r.turrets).map(|(k, t)| (k, t.damage)).collect();
    let iter = typed == vec![(Weapon::Cannon, 1.0), (Weapon::Missile, 2.0), (Weapon::Mine, 3.0)]
        && raw == vec![(1, 1.0), (2, 2.0), (3, 3.0)];
    if !iter { bad.push("ITER"); }

    if bad.is_empty() {
        println!("keyed accessor: NONE MAX SHIFT ITER hold");
    } else {
        for clause in &bad { println!("RED {clause}"); }
        std::process::exit(1);
    }
}
`

// keyedRun generates tables/examples, applies edit to the emitted modules,
// compiles the probe and runs it: the clauses that went red, or nil.
func keyedRun(t *testing.T, edit func(map[string][]byte)) []string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	modules, err := Generate(generateUnit(t, repo, []string{"tables/examples"}))
	if err != nil {
		t.Fatalf("tables/examples was refused: %v", err)
	}
	if edit != nil {
		edit(modules)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "tabledemo", modules)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(keyedProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)
	out, err := exec.Command(bin).CombinedOutput()
	var red []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if clause, ok := strings.CutPrefix(line, "RED "); ok {
			red = append(red, clause)
		}
	}
	if err != nil && len(red) == 0 {
		t.Fatalf("the keyed probe failed without naming a clause: %v\n%s", err, out)
	}
	return red
}

func keyedRequireRustc(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "rustc (it compiles and runs the keyed accessor probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
}

func TestKeyedAccessorRust(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	if red := keyedRun(t, nil); len(red) != 0 {
		t.Fatalf("the keyed accessor is red on %v", red)
	}
}

// TestKeyedAccessorRustNegativeControl is the reversed witness: three
// sabotages of the emitted table_enums.rs, each required to red exactly its
// own clause and nothing else.
func TestKeyedAccessorRustNegativeControl(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	guard := "    let slot = key.wrapping_sub(1);\n    if slot >= slots {\n"
	cases := []struct {
		name, from, to string
		want           string
	}{
		// the None half dropped: key 0 reaches the slice's own bounds check,
		// which names an index
		{"none", guard, "    let slot = key.wrapping_sub(1);\n    if key > slots {\n", "NONE"},
		// the E.Max half dropped: a key past E.Max reaches the slice's check
		{"max", guard, "    let slot = key.wrapping_sub(1);\n    if key == 0 {\n", "MAX"},
		// the shift reversed with both refusals intact: key k lands in slot
		// slots - k
		{"shift", "        table_key_refused(key, slots);\n    }\n    slot\n}", "        table_key_refused(key, slots);\n    }\n    slots - 1 - slot\n}", "SHIFT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			red := keyedRun(t, func(m map[string][]byte) {
				src := string(m[TableEnumsModule+".rs"])
				if strings.Count(src, c.from) != 1 {
					t.Fatalf("the %s sabotage does not apply once to table_enums.rs", c.name)
				}
				m[TableEnumsModule+".rs"] = []byte(strings.Replace(src, c.from, c.to, 1))
			})
			if len(red) != 1 || red[0] != c.want {
				t.Fatalf("sabotage %s: red on %v, want exactly [%s]", c.name, red, c.want)
			}
		})
	}
}
