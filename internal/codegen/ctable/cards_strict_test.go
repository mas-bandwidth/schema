package ctable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCCardsStrictApi pins the C cards facade (tables/cards/c) to the
// strict read (docs/SPEC-TABLES.md §4, §6.5): one read returns the whole
// value or refuses the whole load. The hand-written C sources must call
// the generated strict entry points and must not carry any retired
// tolerant entry point or retired report counter (kind_mismatch, clamped,
// duplicate, or the malformed verdict field). The conformance report
// spelling (KindMismatch=0, Malformed=false, ...) is an output format for
// accepted reads, not a report field, and is not flagged.
//
// TestCCardsStrictApiNegativeControl plants each retired fault back into
// a copy of the real cards.c and requires the same check to go red, so
// the gate cannot pass vacuously.

var cCardsRetired = []string{
	"_tolerant", "Tolerant", "tolerant",
	"->kind_mismatch", ".kind_mismatch",
	"->clamped", ".clamped",
	"->duplicate", ".duplicate",
	"->malformed", ".malformed",
}

var cCardsStrictCalls = []string{
	"cards_card_load(",
	"cards_card_from_json(",
	"cards_card_load_builder(",
}

func cCardsCheck(files map[string]string) []string {
	var bad []string
	for name, src := range files {
		for _, retired := range cCardsRetired {
			if strings.Contains(src, retired) {
				bad = append(bad, name+": retired tolerant surface "+retired)
			}
		}
	}
	if src, ok := files["cards.c"]; ok {
		for _, call := range cCardsStrictCalls {
			if !strings.Contains(src, call) {
				bad = append(bad, "cards.c: missing strict call "+call)
			}
		}
	} else {
		bad = append(bad, "cards.c: missing")
	}
	return bad
}

func cCardsSources(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "tables", "cards", "c")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".c") && !strings.HasSuffix(e.Name(), ".h") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	if len(files) == 0 {
		t.Fatal("no C cards sources found")
	}
	return files
}

func TestCCardsStrictApi(t *testing.T) {
	t.Parallel()
	if bad := cCardsCheck(cCardsSources(t)); len(bad) != 0 {
		t.Fatalf("C cards sources are not on the strict read:\n%s", strings.Join(bad, "\n"))
	}
}

func TestCCardsStrictApiNegativeControl(t *testing.T) {
	t.Parallel()
	sabotages := []struct {
		name, from, to string
	}{
		{"region load", "cards_card_load(region", "cards_card_load_tolerant(region"},
		{"JSON read", "cards_card_from_json(&b", "cards_card_from_json_tolerant(&b"},
		{"builder load", "cards_card_load_builder(&b", "cards_card_load_builder_tolerant(&b"},
		{"retired report field", "rep->refused", "rep->malformed"},
	}
	for _, s := range sabotages {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			files := cCardsSources(t)
			src, ok := files["cards.c"]
			if !ok || !strings.Contains(src, s.from) {
				t.Fatalf("the %s sabotage plants nothing: %q not in the real cards.c", s.name, s.from)
			}
			files["cards.c"] = strings.Replace(src, s.from, s.to, 1)
			if bad := cCardsCheck(files); len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the strict check green", s.name)
			}
		})
	}
}
