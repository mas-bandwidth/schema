package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableClassBeyondCardConstructsRust pins the variable wire's carry of
// the constructs the card record does not use but the class does — the rest of
// the scalar kinds (uint*, float, bits(N)), bytes(N) and the *bytes blob — by
// round-tripping a table of exactly those constructs through the emitted Rust
// codec (docs/SPEC-TABLES.md §2.5, §3, §3.1): a save and a load agree, and the
// re-save is byte-identical to the save.
func TestVariableClassBeyondCardConstructsRust(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	modules, err := Generate(unitFrom(t, scalarClassSrc))
	if err != nil {
		t.Fatalf("the scalar class constructs were refused: %v", err)
	}
	for _, name := range []string{"variable_runtime.rs", "probe_table.rs", "probe_variable.rs"} {
		if _, ok := modules[name]; !ok {
			t.Fatalf("missing module %s; got %v", name, keys(modules))
		}
	}

	root := t.TempDir()
	for _, name := range []string{"variable_runtime.rs", "probe_table.rs", "probe_variable.rs"} {
		if err := os.WriteFile(filepath.Join(root, name), modules[name], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(scalarClassProbe), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(root, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the scalar class codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = root
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the scalar class codec did not run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ROUND TRIP OK") {
		t.Errorf("the round trip did not print %q; output:\n%s", "ROUND TRIP OK", out)
	}
}

const scalarClassSrc = `package probe

table Scalar
{
    u    uint16
    w    uint64
    f    float32
    d    float64
    b    bits(10)
    raw  bytes(8)
    blob *bytes
}
`

const scalarClassProbe = `
mod variable_runtime;
mod probe_table;
mod probe_variable;

use probe_table::*;
use variable_runtime::*;

fn main() {
    let mut v = Scalar::default();
    v.u = 65535;
    v.w = 12345678901234;
    v.f = 3.5;
    v.d = -2.25;
    v.b = 1023;
    v.raw = vec![1, 2, 3, 4, 255];
    v.blob = Some(vec![0, 1, 2, 3, 4, 5, 255]);
    let wire = scalar_save(&v);
    let mut report = TableReport::default();
    let w = scalar_load(&wire, &mut report).expect("load failed");
    assert_eq!(w.u, v.u);
    assert_eq!(w.w, v.w);
    assert_eq!(w.f, v.f);
    assert_eq!(w.d, v.d);
    assert_eq!(w.b, v.b);
    assert_eq!(w.raw, v.raw);
    assert_eq!(w.blob, v.blob);
    let saved = scalar_save(&w);
    assert_eq!(saved, wire);
    println!("ROUND TRIP OK");
}
`
