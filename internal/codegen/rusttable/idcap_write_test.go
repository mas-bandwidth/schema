package rusttable

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), on the Rust leg. Every reader answers wire_damaged for an id
// table of more than 4096 entries, so root_save_checked refuses a value that
// needs 4097 and writes the one that needs exactly 4096, which the read
// accepts. The unit is tablewire.IdCapSchema: 4095, 4096 and 4097 ids.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc, behind slowtest.Gate
// (SCHEMA_SLOW=1 runs it).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const idCapRustProbe = `
fn value(x: u8, y: u8) -> Root {
    Root { keys: vec![1u8; 4094], x, y }
}

fn main() {
    for (x, y, ids) in [(0u8, 0u8, 4095u64), (1, 0, 4096)] {
        let wire = match root_save_checked(&value(x, y)) {
            Ok(w) => w,
            Err(why) => { println!("IDCAP BAD {ids} ids refused: {why}"); continue; }
        };
        let n = u64::from_le_bytes(wire[wire.len() - 8..].try_into().unwrap());
        let mut report = TableReport::default();
        let back = root_load(&wire, &mut report);
        if n == ids && back.is_some() && !report.damaged && !report.refused {
            println!("IDCAP {ids} ok");
        } else {
            println!("IDCAP BAD {ids} count {n} read {}", back.is_some());
        }
    }
    match root_save_checked(&value(1, 1)) {
        Err(why) => println!("IDCAP 4097 ok {why}"),
        Ok(w) => println!("IDCAP BAD 4097 wrote {} bytes", w.len()),
    }
}
`

func runIdCapRust(t *testing.T, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	files, err := Generate(unitFrom(t, tablewire.IdCapSchema()))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		mod := strings.TrimSuffix(name, ".rs")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".rs") || mod == "lib" || mod == "probe" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(idCapRustProbe)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "idcap")
	build := testproc.Command(t, "rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := testproc.Command(t, bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func TestRustWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	out, err := runIdCapRust(t, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for _, want := range []string{"IDCAP 4095 ok", "IDCAP 4096 ok", "IDCAP 4097 ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BAD") {
		t.Fatalf("probe reported BAD:\n%s", out)
	}
}
