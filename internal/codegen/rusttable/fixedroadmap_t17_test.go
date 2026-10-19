package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// rust/W6: THE PLAN IS PARTITIONED UNGUARDED-FIRST (docs/FIXED-FORM-ALGORITHM.md
// §4.1, §4.4). table_fixed_compile states the split: every entry before it is
// unguarded, every entry at or after it is guarded, and no coalesced run
// crosses it. The probe compiles the fixture's own layout against itself, so
// the walk pushes the unguarded tail past the union behind guarded arm entries
// and only the partition repairs that order. Compiling and running generated
// Rust is a functional test behind slowtest.Gate.
func TestFixedRoadmapRustT17PlanPartition(t *testing.T) {
	t.Parallel()
	t.Run("rust/W6", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "rustc (it compiles and runs the generated unit)")
		if _, err := exec.LookPath("rustc"); err != nil {
			t.Fatalf("rustc is not on PATH: %v", err)
		}
		const schema = `package probe
type ArmA { n int32 }
type ArmB {
    s string(8)
    m int32
}
union Pick
{
    a ArmA
    b ArmB
}
fixed table Root
{
    head int32
    pick Pick
    tail int32
}
`
		u := unitFrom(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		root := t.TempDir()
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		probe := `mod build_version;
mod fixed_runtime;
mod probe_fixed;
mod probe_records;

pub use build_version::*;
pub use fixed_runtime::*;
pub use probe_fixed::*;
pub use probe_records::*;

fn main() {
    let mine = TableFixedBlock::parse(&ROOT_FIXED_BLOCK).expect("this build's own block parses");
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut guards = vec![TableFixedGuard::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = table_fixed_compile(&mine, &mine, &ROOT_FIXED_COUNTED, &mut plan, &mut guards, &mut remap, &mut report)
        .expect("the own-against-own plan compiles");
    if n == 0 {
        println!("W6 BAD empty plan");
        std::process::exit(1);
    }
    // The split is the first guarded index (FIXED-FORM-ALGORITHM.md §4.1).
    let mut split = n;
    for i in 0..n {
        if plan[i].guard_count != 0 { split = i; break; }
    }
    if split == 0 || split >= n {
        println!("W6 BAD split={} of {} is vacuous", split, n);
        std::process::exit(1);
    }
    for i in 0..n {
        let guarded = plan[i].guard_count != 0;
        if i < split && guarded {
            println!("W6 BAD entry {} below the split carries a guard", i);
            std::process::exit(1);
        }
        if i >= split && !guarded {
            println!("W6 BAD entry {} at or above the split carries no guard", i);
            std::process::exit(1);
        }
    }
    println!("W6 ok split={} of {}", split, n);
}
`
		if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(root, "t17probe")
		cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("W6: the probe did not build: %v\n%s", err, out)
		}
		run := exec.Command(bin)
		run.Dir = root
		out, err := run.CombinedOutput()
		if err != nil {
			t.Fatalf("W6: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "W6 ok") {
			t.Fatalf("W6: %s", strings.TrimSpace(string(out)))
		}
		t.Log(strings.TrimSpace(string(out)))
	})
}
