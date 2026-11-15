package rusttable

// THE MESSAGE WRITER'S STORAGE CHECK IS A DEBUG ASSERT (docs/SPEC.md §5.1).
// Rust is an assert leg: a string/bytes length or a counted-array count past
// the storage it indexes is a write check like any other, and every write
// check is a debug_assert! and nothing else. A debug build panics on the
// assert before the first byte; a release build carries no write-side check,
// so a string whose used length rides its N + 1 buffer writes rather than
// refuses. tables-rust-message-storage-assert-negative-control strips the
// generated debug_assert! and requires this test red.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc in both profiles.
// It skips by name when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset,
// and fails by name when SCHEMA_REQUIRE_CORPUS=1.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const messageStorageAssertSchema = `package storagerelease

type Item
{
    value int32 = 0
}

fixed table Root
{
    items [..8]Item
    name  string(16)
    wide  string(8)
}
`

// messageStorageAssertProbe is the crate body: one verdict line per clause.
// The debug half catches the assert; the release half proves the writer does
// not refuse (the string's buffer is N + 1, so a length of 9 rides it).
const messageStorageAssertProbe = `
fn measure_asserts(v: &RootRow) -> bool {
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let _ = root_measure_messages(std::slice::from_ref(v), &mut TableReport::default());
    })).is_err()
}

fn save_asserts(v: &RootRow) -> bool {
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let mut buffer = [0u8; 4096];
        let _ = root_save_messages(std::slice::from_ref(v), &mut buffer, &mut TableReport::default());
    })).is_err()
}

fn write_ok(v: &RootRow) -> bool {
    let n = root_measure_messages(std::slice::from_ref(v), &mut TableReport::default());
    if n < 0 {
        return false;
    }
    let mut buffer = [0u8; 4096];
    root_save_messages(std::slice::from_ref(v), &mut buffer, &mut TableReport::default()) >= 0
}

fn main() {
    let mut count = RootRow::default();
    count.items_count = 9; // the declared bound is 8
    let mut length = RootRow::default();
    length.wide_length = 9; // the declared bound is 8
    length.wide[8] = 0x41;

    if cfg!(debug_assertions) {
        println!("ASSERT count {}", if measure_asserts(&count) && save_asserts(&count) { "ok" } else { "BAD" });
        println!("ASSERT length {}", if measure_asserts(&length) && save_asserts(&length) { "ok" } else { "BAD" });
    } else {
        println!("WRITE length {}", if write_ok(&length) { "ok" } else { "BAD" });
    }
}
`

func TestMessageStorageDebugAssertRust(t *testing.T) {
	slowtest.Gate(t, "rustc (it builds and runs the message storage-assert probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the message storage assert cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	u := unitFrom(t, messageStorageAssertSchema)

	debugOut, err := runMessageStorageAssertProbe(t, u, messageStorageAssertProbe, nil)
	if err != nil {
		t.Fatalf("debug profile: the probe did not finish: %v\n%s", err, debugOut)
	}
	for _, id := range []string{"count", "length"} {
		if line := storageAssertLine(debugOut, "ASSERT "+id); line != "ASSERT "+id+" ok" {
			t.Errorf("debug: the %s storage assert did not fire: %s", id, line)
		}
	}

	releaseOut, err := runMessageStorageAssertProbe(t, u, messageStorageAssertProbe, []string{"-O"})
	if err != nil {
		t.Fatalf("release profile: the probe did not finish: %v\n%s", err, releaseOut)
	}
	if line := storageAssertLine(releaseOut, "WRITE length"); line != "WRITE length ok" {
		t.Errorf("release: a length past its bound was refused instead of written: %s", line)
	}
}

func storageAssertLine(out, id string) string {
	prefix := id + " "
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return fmt.Sprintf("[no %s line in the probe's output: %s]", id, strings.TrimSpace(out))
}

// runMessageStorageAssertProbe writes the generated wire modules beside a
// crate root, builds them with rustc in the profile's flags, and runs the
// probe.
func runMessageStorageAssertProbe(t *testing.T, u *ir.Unit, body string, flags []string) (string, error) {
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
	main.WriteString(body)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	linkFlags := append([]string{}, flags...)
	if linker, err := exec.LookPath("cc"); err == nil {
		linkFlags = append(linkFlags, "-C", "linker="+linker)
	}
	build := exec.Command("rustc", append(linkFlags, "--edition", "2024", "-A", "warnings", "-o", bin, "main.rs")...)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}
