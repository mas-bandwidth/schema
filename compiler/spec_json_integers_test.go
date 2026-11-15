// TestSpecJsonIntegers pins docs/SPEC-TABLES.md §16.2 and §16.14's two JSON
// integer rules (docs/rulings.json `json-wide-integers-are-strings` and
// `json-integer-token-only`): an integer whose storage is wider than 32 bits is
// written as a JSON string of decimal digits and reads as a string or a number;
// every integer field takes an integer token only — digits with an optional
// minus, no fraction, no exponent, no negative zero. It also pins the vector
// corpus under testdata/wire/json-integers/.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecJsonIntegers(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 pins the wide-integer kind list", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 ", "### 16.3 ")

		for _, want := range []string{
			// the exact kind list, pinned (§16.2)
			"`int64`, `uint64`, `bits(33..64)` — is written as a JSON string of decimal digits",
			"`int8` … `int32`, `uint8` … `uint32`, `bits(1..32)` — is a number",
			// the wide integer reads as a string or a number
			"reads as a string or a number, the same digits",
			// a wide fixed-point kind is a string, a narrow one a number
			"one whose storage is wider than 32 bits — 64- or 128-bit — is written as a string of digits",
			// the map key is the same digits
			"an integer key is its decimal spelling, quoted",
		} {
			mustState(t, "§16.2 the wide-integer row", section, want)
		}

		// the stale sentences are gone
		mustNotState(t, "§16.2 no longer reads 2.0 and 1e3 as integers",
			section, "`2`, `2.0` and `1e3` are the integers")
		mustNotState(t, "§16.2 no longer reads -0 as zero",
			section, "and `-0` is zero")
		mustNotState(t, "§16.2 no longer reads an integer through a double",
			section, "round-trips numbers through a double")
		mustState(t, "§16.2 no integer's path is a double",
			section, "The value never passes")
	})

	t.Run("§16.14 spells the writer as strings", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.14 ", "## 17. ")

		mustState(t, "§16.14 W10 writes a wide integer as a string",
			section, "is written as a STRING (W8)")
		mustState(t, "§16.14 W10 writes a narrow integer as a bare number",
			section, "a kind of 32 bits and under is written as a bare number")
		mustState(t, "§16.14 W11 writes a wide fixed as a string",
			section, "whose storage is wider than 32 bits is written as a STRING (W8)")
	})

	t.Run("docs/rulings.json carries both rulings", func(t *testing.T) {
		t.Parallel()
		rulings := readRulings(t)

		wide, ok := rulings["json-wide-integers-are-strings"]
		if !ok {
			t.Fatal(`docs/rulings.json carries no ruling "json-wide-integers-are-strings"`)
		}
		if wide.Date != "2026-11-10" {
			t.Errorf("json-wide-integers-are-strings date is %q, want 2026-11-10", wide.Date)
		}
		if len(wide.Quotes) != 1 || wide.Quotes[0] != "strings, because numbers can be wrong" {
			t.Errorf("json-wide-integers-are-strings quotes are %q, want exactly %q", wide.Quotes, "strings, because numbers can be wrong")
		}
		if !strings.Contains(wide.Source, "relayed by the coordinator from the live session") {
			t.Errorf("json-wide-integers-are-strings source is %q", wide.Source)
		}

		token, ok := rulings["json-integer-token-only"]
		if !ok {
			t.Fatal(`docs/rulings.json carries no ruling "json-integer-token-only"`)
		}
		if token.Date != "2026-11-10" {
			t.Errorf("json-integer-token-only date is %q, want 2026-11-10", token.Date)
		}
		const rulingText = "ruled 2026-11-10: a JSON integer field takes an integer token only: no fraction, no exponent, no negative zero"
		if len(token.Quotes) != 1 || token.Quotes[0] != rulingText {
			t.Errorf("json-integer-token-only quotes are %q, want exactly the ruling text %q", token.Quotes, rulingText)
		}
		if !strings.Contains(token.Source, "strict wins") {
			t.Errorf("json-integer-token-only source is %q", token.Source)
		}
	})

	t.Run("the vectors exist and the manifest names each with its verdict", func(t *testing.T) {
		t.Parallel()
		const dir = "../testdata/wire/json-integers"
		want := []struct {
			name    string
			root    string
			verdict string
		}{
			{"canonical", "Integers", "read"},
			{"strings", "Integers", "read"},
			{"numbers", "Integers", "read"},
			{"small-fraction", "Integers", "refused"},
			{"small-exponent", "Integers", "refused"},
			{"small-neg-zero", "Integers", "refused"},
			{"wide-fraction", "Integers", "refused"},
			{"wide-exponent", "Integers", "refused"},
			{"wide-neg-zero", "Integers", "refused"},
			{"wide-string-fraction", "Integers", "refused"},
			{"wide-string-exponent", "Integers", "refused"},
			{"wide-string-neg-zero", "Integers", "refused"},
			{"ranged-one-past", "Integers", "refused"},
			{"ranged-below-min", "Integers", "refused"},
			{"uwide-one-past", "Integers", "refused"},
			{"wide128-one-past", "Integers", "refused"},
			{"map-key-fraction", "Integers", "refused"},
			{"map-key-exponent", "Integers", "refused"},
			{"map-key-neg-zero", "Integers", "refused"},
		}

		data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatalf("the vectors' manifest: %v", err)
		}
		type row struct {
			root    string
			json    string
			verdict string
		}
		named := make(map[string]row)
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) != 5 || fields[0] == "" || fields[1] == "" || fields[2] == "" || fields[3] == "" {
				t.Fatalf("%s/MANIFEST.tsv:%d: a vector line is <name> TAB <root> TAB <json> TAB <verdict> TAB <what it holds>, got %q", dir, i+1, line)
			}
			name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
			if verdict != "read" && verdict != "refused" {
				t.Errorf("%s/MANIFEST.tsv:%d: %q has verdict %q, want read or refused", dir, i+1, name, verdict)
			}
			if _, dup := named[name]; dup {
				t.Errorf("%s/MANIFEST.tsv:%d: %q is named twice", dir, i+1, name)
			}
			named[name] = row{root: root, json: jsonFile, verdict: verdict}
		}

		if len(named) != len(want) {
			t.Errorf("%s/MANIFEST.tsv names %d vectors, want %d", dir, len(named), len(want))
		}
		for _, v := range want {
			r, ok := named[v.name]
			if !ok {
				t.Errorf("%s/MANIFEST.tsv names no vector %q", dir, v.name)
				continue
			}
			if r.root != v.root {
				t.Errorf("%s: root %q, want %q", v.name, r.root, v.root)
			}
			wantFile := v.name + "." + v.verdict + ".json"
			if r.json != wantFile {
				t.Errorf("%s: json file %q, want %q", v.name, r.json, wantFile)
			}
			if r.verdict != v.verdict {
				t.Errorf("%s: verdict %q, want %q", v.name, r.verdict, v.verdict)
			}
			if info, err := os.Stat(filepath.Join(dir, wantFile)); err != nil {
				t.Errorf("the vector %q: %v", wantFile, err)
			} else if info.IsDir() || info.Size() == 0 {
				t.Errorf("the vector %q is not a non-empty file", wantFile)
			}
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("the vectors' directory: %v", err)
		}
		files := make(map[string]bool, len(named))
		for _, r := range named {
			files[r.json] = true
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "MANIFEST.tsv" {
				continue
			}
			if !files[e.Name()] {
				t.Errorf("the vector %q is not named in MANIFEST.tsv", e.Name())
			}
		}
	})
}
