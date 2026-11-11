package rusttable

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE MESSAGE WRITER's STORAGE CHECK IS A DEBUG ASSERT (docs/SPEC.md §5.1).
// Rust is an assert leg: a counted array's count or a string(N)'s used length
// that would index past the storage it describes is the caller's bug, caught by
// `debug_assert!` in a debug build and gone in release. The release build
// carries no write-side check of any kind. tables-rust-message-storage-assert-
// negative-control takes the assert's condition out of the emitter and
// requires TestMessageStorageDebugAssertRust red.
const rustMessageStorageUnit = `package mstorage

type Item
{
    value int32 = 0
}

fixed table Root
{
    items [..8]Item
    wide  string(8)
}
`

func messageStorageSource(t *testing.T) string {
	t.Helper()
	files, err := Generate(unitFrom(t, rustMessageStorageUnit))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "_message.rs") {
			return string(data)
		}
	}
	t.Fatal("no _message.rs was generated")
	return ""
}

// TestMessageStorageCheckIsDebugOnlyRust reads the emitted writer: both
// storage checks are debug asserts, and no release-build refusal of either is
// left beside them.
func TestMessageStorageCheckIsDebugOnlyRust(t *testing.T) {
	t.Parallel()
	src := messageStorageSource(t)
	for _, want := range []string{
		"value.items_count >= 0 && value.items_count <= 8,",
		"value.wide_length >= 0 && value.wide_length <= 8,",
	} {
		i := strings.Index(src, want)
		if i < 0 || !strings.HasSuffix(strings.TrimRight(src[:i], " "), "debug_assert!(\n") {
			t.Errorf("the storage check %q is not a debug_assert!", want)
		}
	}
	for _, refusal := range []string{"value.items_count < 0 ||", "value.wide_length < 0 ||"} {
		if strings.Contains(src, refusal) {
			t.Errorf("a release-build storage refusal is still emitted: %q", refusal)
		}
	}
}

// TestMessageStorageDebugAssertRust builds the writer with debug assertions on:
// a count or length past its storage, or below zero, must hit the assert in
// both measure and save, and the lawful twin at the bound must be written.
func TestMessageStorageDebugAssertRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated message-writer probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal(err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	body := `
    std::panic::set_hook(Box::new(|_| {}));
    let mut failures = Vec::new();
    let cases: [(&str, i32, i32, bool); 6] = [
        ("count-at-bound", 8, 8, true),
        ("count-past-storage", 9, 0, false),
        ("count-negative", -1, 0, false),
        ("length-at-bound", 0, 8, true),
        ("length-past-storage", 0, 9, false),
        ("length-negative", 0, -1, false),
    ];
    for (name, count, length, lawful) in cases {
        let mut row = RootRow::default();
        row.items_count = count;
        row.wide_length = length;
        let measured = std::panic::catch_unwind(|| root_measure_messages(std::slice::from_ref(&row), &mut TableReport::default()));
        let saved = std::panic::catch_unwind(|| {
            let mut buffer = [0u8; 4096];
            root_save_messages(std::slice::from_ref(&row), &mut buffer, &mut TableReport::default())
        });
        if lawful {
            match (measured, saved) {
                (Ok(n), Ok(s)) if n > 0 && s == n => {}
                _ => failures.push(format!("{name}: the lawful twin must be written")),
            }
            continue;
        }
        // THE ASSERT, not a slice bound: only the debug_assert!'s own message
        // counts, so a writer that panics on the index instead is still red.
        for (call, got) in [("measure", measured), ("save", saved)] {
            let message = match &got {
                Ok(_) => String::new(),
                Err(p) => p.downcast_ref::<&str>().map(|s| s.to_string()).or_else(|| p.downcast_ref::<String>().cloned()).unwrap_or_default(),
            };
            if !message.contains("is the bound") {
                failures.push(format!("{name}: {call}: an over-long count or length must hit the debug assert, got {message:?}"));
            }
        }
    }
    if failures.is_empty() { println!("MESSAGE_STORAGE_ASSERT ok"); }
    else { for failure in failures { println!("MESSAGE_STORAGE_ASSERT BAD {failure}"); } std::process::exit(1); }
`
	out, err := runRustProbe(t, unitFrom(t, rustMessageStorageUnit), body, "-C", "debug-assertions=on")
	if err != nil || !strings.Contains(out, "MESSAGE_STORAGE_ASSERT ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
