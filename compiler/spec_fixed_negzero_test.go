// A COMPRESSED FLOAT'S `-0` IS `+0`, ON BOTH SIDES (docs/SPEC-TABLES.md §3.3
// and §3.4, docs/FIXED-FORM-ALGORITHM.md §3.1 and §4.6).
//
// The rule, dated 2026-11-10: a compressed float's negative zero reads as +0
// and is written as +0 (docs/rulings.json fixed-compressed-negzero). SPEC-TABLES
// §3.3 already states the read side; this pin holds the algorithm page to it
// and holds the FIXED row and the write side up beside it. A PLAIN float32 or
// float64's -0 is unchanged: it is carried bit-exact.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecFixedCompressedNegZero(t *testing.T) {
	t.Parallel()
	const algorithm = "../docs/FIXED-FORM-ALGORITHM.md"
	const tables = "../docs/SPEC-TABLES.md"

	write := specSection(t, algorithm, "### 3.1 The write", "## 4. The reader: ONE path")
	pass := specSection(t, algorithm, "### 4.6 The bounds pass", "## 5. Evolution")
	message := specSection(t, tables, "### 3.3 The message form", "### 3.4 The fixed form")

	var fixedRow string
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "| FIXED (§3.4) |") {
			fixedRow = line
			break
		}
	}

	// the pages wrap at a fixed width, so a sentence is matched with its line
	// breaks folded into single spaces
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	t.Run("the ruling is in docs/rulings.json", func(t *testing.T) {
		t.Parallel()
		r, ok := readRulings(t)["fixed-compressed-negzero"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling \"fixed-compressed-negzero\"")
		}
		if r.Date != "2026-11-10" {
			t.Errorf("the ruling fixed-compressed-negzero carries date %q, want 2026-11-10", r.Date)
		}
		const quote = "ruled 2026-11-10: a compressed float's negative zero reads as +0 and is written as +0"
		found := false
		for _, q := range r.Quotes {
			if q == quote {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the ruling fixed-compressed-negzero does not quote %q", quote)
		}
		if !strings.Contains(r.Source, "tables-finite-only") {
			t.Errorf("the ruling fixed-compressed-negzero does not say it narrows tables-finite-only: source %q", r.Source)
		}
	})

	t.Run("§3.1: the writer stores +0 for a compressed float's -0", func(t *testing.T) {
		t.Parallel()
		text := flat(write)
		for _, want := range []string{
			"the writer stores `+0` for a compressed float's `-0`",
			"keeps no sign bit and counts nothing",
			"carried bit-exact",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§3.1 no longer states %q", want)
			}
		}
	})

	t.Run("§4.6: the bounds pass lands a compressed float's -0 as +0", func(t *testing.T) {
		t.Parallel()
		text := flat(pass)
		for _, want := range []string{
			"lands it as exactly what `+0` lands as",
			"refuses nothing and counts nothing",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§4.6 no longer states %q", want)
			}
		}
	})

	t.Run("§3.3: the FIXED row and the write side state the rule", func(t *testing.T) {
		t.Parallel()
		if fixedRow == "" {
			t.Fatal("§3.3 has no FIXED (§3.4) row in the per-form table")
		}
		row := flat(fixedRow)
		for _, want := range []string{
			"a `-0` lands as `+0`, uncounted",
			"the WRITER stores `+0` for it",
		} {
			if !strings.Contains(row, want) {
				t.Errorf("§3.3's FIXED row no longer states %q", want)
			}
		}
		if !strings.Contains(flat(message), "the writer stores `+0` for a compressed float's `-0`") {
			t.Error("§3.3 no longer states the write side beside the read")
		}
	})
}
