package ruby

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
)

// TestRubyIdentifierCorpus holds the Ruby name check to the committed corpus
// testdata/identifier-rules (docs/PORT-RUBY.md, IDENTIFIERS): a field spelled
// like to_h or public_send, a union arm spelled public_send or whose tag
// constant spells NAMES, and a variable or fixed table field spelled like a
// table-class member (measure, measure_retain, save_retain, save_text,
// to_json, cook, cook_measure) is refused by name: by the Ruby check, or, for
// the members whose Dart spellings are Dart table verbs, already by the shared
// checker for every target. The controls beside them (a packet type's fields
// may spell table members, names beside the members, an arm whose constant is
// Names) pass. Each schema goes through the parser and the shared checker
// first, so every case is a schema a user could write.
// A pure Go test: no interpreter, no clock.
func TestRubyIdentifierCorpus(t *testing.T) {
	t.Parallel()
	const dir = "testdata/identifier-rules"
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	verdicts := map[string]int{}
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("cases.tsv line %d: %d fields, want 3", n+1, len(f))
		}
		id, verdict, says := f[0], f[1], f[2]
		verdicts[verdict]++
		src, err := os.ReadFile(filepath.Join(dir, id+".schema"))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		ast, perrs := parser.Parse("Game.schema", src)
		if len(perrs) != 0 {
			t.Fatalf("%s: parse: %v", id, perrs[0])
		}
		u, cerrs := check.Unit([]check.SourceFile{{Path: "Game.schema", Name: "Game.schema", Base: "Game", Bytes: src, AST: ast}})
		if verdict == "checker" {
			if len(cerrs) == 0 || !strings.Contains(cerrs[0].Error(), says) {
				t.Errorf("%s: the shared checker answered %v, want the refusal %q", id, cerrs, says)
			}
			continue
		}
		if len(cerrs) != 0 {
			t.Fatalf("%s: the shared checker refused it before the Ruby check: %v", id, cerrs[0])
		}
		got := CheckNames(u)
		switch verdict {
		case "ruby":
			if got == nil {
				t.Errorf("%s: accepted; the Ruby check owes the refusal %q", id, says)
			} else if !strings.Contains(got.Error(), says) {
				t.Errorf("%s: refused %q, want it to say %q", id, got, says)
			}
		case "accepted":
			if got != nil {
				t.Errorf("%s: refused: %v", id, got)
			}
		default:
			t.Fatalf("cases.tsv line %d: verdict %q", n+1, verdict)
		}
	}
	if verdicts["ruby"] == 0 || verdicts["checker"] == 0 || verdicts["accepted"] == 0 {
		t.Fatalf("cases.tsv: verdicts %v; the corpus owes refusals and controls", verdicts)
	}
}
