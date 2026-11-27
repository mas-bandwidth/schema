package rusttable

// THE MESSAGE WRITER NEVER WRITES A DEPRECATED VARIANT (docs/SPEC-TABLES.md
// §2.15 WRITE). Rust is an assert leg (docs/SPEC.md §5.1): the refusal is a
// debug_assert! and nothing else. A fixed table's Row holds an enum ordinal as
// plain u8 storage, so the message batch (<root>_measure_messages and
// <root>_save_messages, one body walk for both) can be handed ordinal 2, Old;
// before the assert it wrote Old's name slot. The probe drives the message
// batch in a DEBUG build over an array element and a scalar: Old must hit the
// assert in measure and save, Calm and Fierce must write and read back. A
// variable `table` has no message form on this leg (rusttable.go emits
// <base>_message.rs for fixed roots only), and the variable save is held by
// enumRef's own assert.
//
// TestRustDeprecatedVariantMessageNegativeControl removes the generated assert
// from the crate (element and scalar in turn) and requires exactly that
// refusal red and nothing else, so the probe can see a write of Old.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1 (make tables-rust-deprecated-message-write).

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

const deprecatedMessageSchema = `package depmsg

enum Mood
{
    Calm
    Old | deprecated
    Fierce
}

fixed table Moods
{
    row  [..4]Mood
    mood Mood = Fierce
}
`

// deprecatedMessageProbe prints one verdict line per check. Ordinals: None 0,
// Calm 1, Old 2, Fierce 3.
const deprecatedMessageProbe = `
fn asserts(v: &MoodsRow) -> bool {
    let measured = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let _ = moods_measure_messages(std::slice::from_ref(v), &mut TableReport::default());
    })).is_err();
    let saved = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let mut buffer = [0u8; 4096];
        let _ = moods_save_messages(std::slice::from_ref(v), &mut buffer, &mut TableReport::default());
    })).is_err();
    measured && saved
}

fn round_trips(v: &MoodsRow) -> bool {
    let n = moods_measure_messages(std::slice::from_ref(v), &mut TableReport::default());
    if n < 0 {
        return false;
    }
    let mut buffer = [0u8; 4096];
    let w = moods_save_messages(std::slice::from_ref(v), &mut buffer, &mut TableReport::default());
    if w != n {
        return false;
    }
    let mut storage = vec![TableMessageEntry::default(); 4096];
    let mut vocab = TableVocabulary::new(&mut storage);
    if !announce_read(&mut vocab, &TABLE_ANNOUNCEMENT, &mut TableReport::default()) {
        return false;
    }
    let mut back = [MoodsRow::default()];
    let (count, ok) = moods_load_messages(&mut back, &vocab, &buffer[..w as usize], &mut TableReport::default());
    if !ok || count != 1 {
        return false;
    }
    back[0].mood == v.mood && back[0].row_count == v.row_count && back[0].row[..v.row_count as usize] == v.row[..v.row_count as usize]
}

fn line(id: &str, ok: bool) {
    println!("{} {}", id, if ok { "ok" } else { "BAD" });
}

fn main() {
    if !cfg!(debug_assertions) {
        println!("PROFILE not a debug build");
        return;
    }
    let mut element = MoodsRow::default();
    element.row_count = 2;
    element.row[0] = 1;
    element.row[1] = 2; // Old
    let mut scalar = MoodsRow::default();
    scalar.mood = 2; // Old
    let mut live = MoodsRow::default();
    live.row_count = 3;
    live.row[0] = 1; // Calm
    live.row[1] = 3; // Fierce
    live.row[2] = 1;
    live.mood = 1; // Calm, off the Fierce default so it rides
    let mut fierce = MoodsRow::default();
    fierce.row_count = 1;
    fierce.row[0] = 3;

    line("REFUSE element", asserts(&element));
    line("REFUSE scalar", asserts(&scalar));
    line("WRITE live", !asserts(&live) && round_trips(&live));
    line("WRITE fierce", !asserts(&fierce) && round_trips(&fierce));
}
`

var deprecatedMessageLines = []string{"REFUSE element", "REFUSE scalar", "WRITE live", "WRITE fierce"}

// the generated asserts the negative control removes, one per refusal line.
var deprecatedMessageAsserts = map[string]string{
	"REFUSE element": "debug_assert!(value.row[i] != 2, ",
	"REFUSE scalar":  "debug_assert!(value.mood != 2, ",
}

func deprecatedMessageGate(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "rustc (it builds and runs the message deprecated-variant probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the message writer's deprecated-variant refusal cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
}

// runDeprecatedMessageProbe builds the probe in its debug profile, with the
// named generated asserts removed, and returns its verdict per line.
func runDeprecatedMessageProbe(t *testing.T, strip []string) map[string]string {
	t.Helper()
	u := unitFrom(t, deprecatedMessageSchema)
	out, err := runMessageProbeEdited(t, u, deprecatedMessageProbe, nil, func(name string, data []byte) []byte {
		if !strings.HasSuffix(name, "_message.rs") {
			return data
		}
		s := string(data)
		for _, a := range strip {
			if !strings.Contains(s, a) {
				t.Fatalf("the generated message writer carries no %q to remove", a)
			}
			s = strings.ReplaceAll(s, a, "debug_assert!(true, ")
		}
		return []byte(s)
	})
	if err != nil {
		t.Fatalf("the probe did not finish: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, id := range deprecatedMessageLines {
		got[id] = storageAssertLine(out, id)
	}
	return got
}

func TestRustDeprecatedVariantMessageNeverWritten(t *testing.T) {
	deprecatedMessageGate(t)
	got := runDeprecatedMessageProbe(t, nil)
	for _, id := range deprecatedMessageLines {
		if got[id] != id+" ok" {
			t.Errorf("debug: the message writer (moods_measure_messages / moods_save_messages) %s: %s", id, got[id])
		}
	}
}

func TestRustDeprecatedVariantMessageNegativeControl(t *testing.T) {
	deprecatedMessageGate(t)
	for _, target := range []string{"REFUSE element", "REFUSE scalar"} {
		t.Run(strings.TrimPrefix(target, "REFUSE "), func(t *testing.T) {
			got := runDeprecatedMessageProbe(t, []string{deprecatedMessageAsserts[target]})
			for _, id := range deprecatedMessageLines {
				red := got[id] != id+" ok"
				switch {
				case id == target && !red:
					t.Errorf("NEGATIVE CONTROL FAILED: with the message writer's %s assert gone, %s stayed green", target, id)
				case id != target && red:
					t.Errorf("NEGATIVE CONTROL FAILED: removing the %s assert also redded %s", target, got[id])
				}
			}
			t.Logf("negative control: with the message writer's %s assert gone, the message batch writes Old: %s", strings.TrimPrefix(target, "REFUSE "), got[target])
		})
	}
}
