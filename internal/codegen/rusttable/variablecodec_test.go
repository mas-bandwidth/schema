package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cardsUnit loads the real cards corpus (tables/cards/Card.schema) through the
// parser and checker, so the round-trip test runs the card's own declaration
// and its committed wire, not a test-shaped stand-in.
func cardsUnit(t *testing.T) *ir.Unit {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, "tables", "cards", "Card.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse("Card.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Card.schema", Name: "Card.schema", Base: "Card", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestCardConstructsRoundTripTheVariableForm pins docs/SPEC-TABLES.md §3, §3.1:
// the emitted Rust save/load reads the committed card-populated.wire into the
// values card-populated.json names and re-saves it byte for byte, so a Rust
// program reads and writes a card exactly as the Go, C, C++ and C# backends do.
func TestCardConstructsRoundTripTheVariableForm(t *testing.T) {
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
	if _, ok := modules["card_table.rs"]; !ok {
		t.Fatalf("no card_table.rs: %v", keys(modules))
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
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(cardRoundTripProbe), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(root, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the card codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin, filepath.Join(repo, "tables", "cards", "fuzz", "card-populated.wire"))
	run.Dir = root
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the card codec did not run: %v\n%s", err, out)
	}
	for _, want := range []string{"BYTE IDENTICAL", "VALUES MATCH"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the round trip did not print %q; output:\n%s", want, out)
		}
	}
}

const cardRoundTripProbe = `
mod variable_runtime;
mod card_table;
mod card_variable;

use card_table::*;
use variable_runtime::*;

fn main() {
    let path = std::env::args().nth(1).expect("wire path");
    let wire = std::fs::read(&path).expect("read wire");
    let mut report = TableReport::default();
    let v = card_load(&wire, &mut report).expect("load failed");
    // the values the committed card-populated.json names (docs/SPEC-TABLES.md
    // §16): the loaded card must hold them, or the wire did not decode.
    assert_eq!(v.id, "populated");
    assert_eq!(v.tier, Tier::Flash);
    assert_eq!(v.priority, Priority::Critical);
    assert_eq!(v.paths.len(), 2);
    assert_eq!(v.needs.len(), 2);
    assert_eq!(v.deadline_minutes, 45);
    assert_eq!(v.source.as_deref(), Some("source résumé→世界"));
    assert_eq!(v.epoch, 7);
    assert_eq!(v.column, Column::Review);
    assert_eq!(v.attempt, 1);
    assert_eq!(v.body.as_deref(), Some("body with multibyte →世界 and \"escapes\"\nand tabs\t"));
    println!("VALUES MATCH");
    let saved = card_save(&v);
    if saved == wire {
        println!("BYTE IDENTICAL");
    } else {
        panic!("re-save diverged: {} bytes vs {}", saved.len(), wire.len());
    }
}
`
