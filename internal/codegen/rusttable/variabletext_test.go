package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCardTextFormMatchesGo pins docs/SPEC-TABLES.md §16, §16.2, §16.7: the
// emitted Rust text form writes card-populated.wire as the committed
// card-populated.json byte for byte, reads that text back to the same wire byte
// for byte, and refuses an unknown property, an out-of-range enum and an
// over-bound string — never truncating one silently.
func TestCardTextFormMatchesGo(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	modules, err := Generate(cardsUnit(t))
	if err != nil {
		t.Fatalf("the card constructs were refused: %v", err)
	}

	root := t.TempDir()
	for _, name := range []string{"variable_runtime.rs", "card_table.rs", "card_variable.rs"} {
		data, ok := modules[name]
		if !ok {
			t.Fatalf("missing module %s; got %v", name, keys(modules))
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(cardTextProbe), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(root, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the card text form did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin,
		filepath.Join(repo, "tables", "cards", "fuzz", "card-populated.wire"),
		filepath.Join(repo, "tables", "cards", "fuzz", "card-populated.json"))
	run.Dir = root
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the card text form did not run: %v\n%s", err, out)
	}
	for _, want := range []string{"JSON BYTE IDENTICAL", "WIRE ROUND TRIP", "REFUSALS OK"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the text form did not print %q; output:\n%s", want, out)
		}
	}
}

const cardTextProbe = `
mod variable_runtime;
mod card_table;
mod card_variable;

use card_table::*;
use variable_runtime::*;

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let wire = std::fs::read(&args[1]).expect("read wire");
    let json = std::fs::read(&args[2]).expect("read json");
    let json_str = String::from_utf8(json.clone()).expect("json utf8");

    let mut report = TableReport::default();
    let v = card_load(&wire, &mut report).expect("load");

    // §16.5: ToJson of the known instance equals the committed text byte for
    // byte (docs/SPEC-TABLES.md §16.1, the canonical trailing newline).
    let text = card_to_json(&v);
    if text.as_bytes() != json {
        panic!("to_json diverged from card-populated.json:\n{}", text);
    }
    println!("JSON BYTE IDENTICAL");

    // §16.5: that text read back saves to the wire it came from byte for byte.
    let mut r2 = TableReport::default();
    let v2 = card_from_json(&json_str, &mut r2).expect("from_json");
    let saved = card_save(&v2);
    if saved != wire {
        panic!("from_json re-save diverged: {} vs {}", saved.len(), wire.len());
    }
    println!("WIRE ROUND TRIP");

    // §16.2: an unknown property is skipped and counted, never corrupting the
    // named fields.
    let mut r3 = TableReport::default();
    let _ = card_from_json("{\"id\": \"x\", \"no_such_field\": 7}\n", &mut r3);
    if r3.unknown < 1 {
        panic!("unknown property was not counted: {}", r3.unknown);
    }

    // §16.2: an enum value no variant names reads None and is counted unknown.
    let mut r4 = TableReport::default();
    let v4 = card_from_json("{\"tier\": \"notavariant\"}\n", &mut r4).expect("enum");
    assert_eq!(v4.tier, Tier::None);
    if r4.unknown < 1 {
        panic!("out-of-range enum was not counted: {}", r4.unknown);
    }

    // §16.2: a string longer than N is clamped to N and counted, never cut
    // through a character.
    let mut r5 = TableReport::default();
    let long_id = "x".repeat(100);
    let v5 = card_from_json(&format!("{{\"id\": {:?}}}\n", long_id), &mut r5).expect("clamp");
    assert_eq!(v5.id.len(), 64);
    if r5.clamped < 1 {
        panic!("over-bound string was not clamped: {}", r5.clamped);
    }

    println!("REFUSALS OK");
}
`
