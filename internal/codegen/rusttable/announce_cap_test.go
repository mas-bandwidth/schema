package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up), on the Rust leg: announce and
// announce_measure refuse a unit with 4097 vocabulary entries and write the one
// with exactly 4096, which announce_read accepts at the default maxima. Red
// when the refusal is removed.
func TestRustAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH: %v", err)
	}
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		files, err := Generate(unitFrom(t, strings.Replace(tablewire.AnnounceSchema(row.entries), "table Root", "fixed table Root", 1)))
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for _, module := range []string{BuildVersionModule + ".rs", MessageRuntimeModule + ".rs", VariableRuntimeModule + ".rs"} {
			if data, ok := files[module]; ok {
				if err := os.WriteFile(filepath.Join(dir, module), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		main := "mod build_version;\nmod message_runtime;\npub use build_version::*;\npub use message_runtime::*;\n"
		if _, ok := files[VariableRuntimeModule+".rs"]; ok {
			main += "mod variable_runtime;\npub use variable_runtime::*;\n"
		}
		main += fmt.Sprintf(`
fn main() {
    let refused = %t;
    let n = announce_measure();
    let mut buf = vec![0u8; 1 << 20];
    let w = announce(&mut buf);
    if refused {
        if n >= 0 || w >= 0 { println!("BAD measure {n} announce {w}, want both refused"); std::process::exit(1); }
        println!("ANNOUNCE ok");
        return;
    }
    if n <= 0 || w != n { println!("BAD measure {n} announce {w}, want both to write"); std::process::exit(1); }
    let mut storage = vec![TableMessageEntry::default(); 4096];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut report = TableReport::default();
    if !announce_read(&mut vocab, &buf[..w as usize], &mut report) { println!("BAD the reader refused the written announcement"); std::process::exit(1); }
    println!("ANNOUNCE ok");
}
`, row.refused)
		if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "annprobe")
		build := testproc.Command(t, "rustc", "--edition", "2024", "-o", bin, "main.rs")
		build.Dir = dir
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("%d entries: the probe did not build: %v\n%s", row.entries, err, out)
		}
		run := testproc.Command(t, bin)
		run.Dir = dir
		out, err := run.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "ANNOUNCE ok") {
			t.Fatalf("%d entries: %v\n%s", row.entries, err, out)
		}
	}
}
