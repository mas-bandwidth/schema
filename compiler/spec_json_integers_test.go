// TestSpecJsonIntegers pins the two JSON-integer rules on the page and the
// corpus (docs/SPEC-TABLES.md §16.2 and §16.14, docs/rulings.json
// `json-wide-integers-are-strings` and `json-integer-token-only`): a kind whose
// storage is wider than 32 bits is written as a JSON string of decimal digits
// and read from either the string or a number, and an integer field takes an
// integer token only — digits with an optional minus, no fraction, no exponent,
// not `-0`. The retired sentences are "`2.0` and `1e3` are the integers" and
// "`-0` is zero".
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecJsonIntegers(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 states the kind list and retires the lenient integer read", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 The mapping, field kind by field kind", "### 16.3 ")
		flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
		text := flat(section)

		for _, want := range []string{
			"A KIND WHOSE STORAGE IS WIDER THAN 32 BITS IS A STRING OF DECIMAL DIGITS",
			"`int64`, `uint64`",
			"`int128`, `uint128`",
			"whose `I + F` is 64 or 128",
			"Kinds of 32 bits and under stay JSON numbers",
			"takes the string and KEEPS taking the number token an earlier build wrote",
			"a string or a number",
			"takes an INTEGER TOKEN and nothing else",
			"no fraction, no exponent, and not `-0`",
			"the value never passes through a double",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§16.2 no longer states %q", want)
			}
		}
		for _, stale := range []string{
			"`2.0` and `1e3` are the integers",
			"`-0` is zero",
		} {
			if strings.Contains(text, stale) {
				t.Errorf("§16.2 still states the retired lenient read %q", stale)
			}
		}
	})

	t.Run("§16.14 W10 and W11 spell the wide kinds as strings", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		w10 := specTablesSection(t, page, "**W10. An integer**", "**W11. A fixed-point value**")
		w11 := specTablesSection(t, page, "**W11. A fixed-point value**", "**W12. A float**")
		flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }

		for _, want := range []string{
			"WRAPPED IN QUOTES, a JSON string",
			"a kind of 32 bits and under stays a number",
		} {
			if !strings.Contains(flat(w10), want) {
				t.Errorf("W10 no longer states %q", want)
			}
		}
		if !strings.Contains(flat(w11), "WRAPPED IN QUOTES, a JSON string") {
			t.Error("W11 no longer states the wide fixed string")
		}
	})

	t.Run("docs/rulings.json carries both entries", func(t *testing.T) {
		t.Parallel()
		rulings := readRulings(t)

		wide, ok := rulings["json-wide-integers-are-strings"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling json-wide-integers-are-strings")
		}
		if wide.Date != "2026-11-10" {
			t.Errorf("json-wide-integers-are-strings date %q, want 2026-11-10", wide.Date)
		}
		wideQuotes := strings.Join(wide.Quotes, "\n")
		if !strings.Contains(wideQuotes, "strings, because numbers can be wrong") {
			t.Errorf("json-wide-integers-are-strings quotes do not carry the owner's words: %q", wide.Quotes)
		}
		if !strings.Contains(wideQuotes, "ruled 2026-11-10: in the JSON form, integers wider than 32 bits are written as strings of decimal digits") {
			t.Errorf("json-wide-integers-are-strings quotes do not carry the ruling text: %q", wide.Quotes)
		}
		if !strings.Contains(wide.Source, "coordinator") || !strings.Contains(wide.Source, "live session") {
			t.Errorf("json-wide-integers-are-strings source %q, want relayed by the coordinator from the live session", wide.Source)
		}

		token, ok := rulings["json-integer-token-only"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling json-integer-token-only")
		}
		if token.Date != "2026-11-10" {
			t.Errorf("json-integer-token-only date %q, want 2026-11-10", token.Date)
		}
		if !strings.Contains(strings.Join(token.Quotes, "\n"), "ruled 2026-11-10: a JSON integer field takes an integer token only: no fraction, no exponent, no negative zero") {
			t.Errorf("json-integer-token-only quotes do not carry the ruling text: %q", token.Quotes)
		}
		if !strings.Contains(token.Source, "strict wins") || !strings.Contains(token.Source, "coordinator") {
			t.Errorf("json-integer-token-only source %q, want the standing strict-wins rule applied by the coordinator", token.Source)
		}
	})

	t.Run("the vectors exist and the manifest names each with its verdict", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "testdata", "wire", "json-integers")
		data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatalf("vectors manifest: %v", err)
		}
		want := map[string]struct {
			json    string
			verdict string
			reason  string
		}{
			"canonical":                 {"canonical.json", "read", "-"},
			"wide-as-numbers":           {"wide-as-numbers.json", "read", "-"},
			"int32-fraction":            {"int32-fraction.json", "refused", "kind_mismatch"},
			"int32-exponent":            {"int32-exponent.json", "refused", "kind_mismatch"},
			"int32-negative-zero":       {"int32-negative-zero.json", "refused", "kind_mismatch"},
			"int64-fraction":            {"int64-fraction.json", "refused", "kind_mismatch"},
			"int64-string-fraction":     {"int64-string-fraction.json", "refused", "kind_mismatch"},
			"int64-string-exponent":     {"int64-string-exponent.json", "refused", "kind_mismatch"},
			"int64-string-negative-zero": {"int64-string-negative-zero.json", "refused", "kind_mismatch"},
			"int64-one-past":            {"int64-one-past.json", "refused", "out_of_range"},
			"uint64-one-past":           {"uint64-one-past.json", "refused", "out_of_range"},
			"int128-one-past":           {"int128-one-past.json", "refused", "out_of_range"},
			"ranged-one-past":           {"ranged-one-past.json", "refused", "out_of_range"},
		}
		seen := make(map[string]bool)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) < 6 {
				t.Errorf("manifest row %q has %d columns, want name, schema, root, json, verdict, reason", line, len(cols))
				continue
			}
			name, schema, root, jsonFile, verdict, reason := cols[0], cols[1], cols[2], cols[3], cols[4], cols[5]
			expected, ok := want[name]
			if !ok {
				t.Errorf("manifest names an unexpected vector %q", name)
				continue
			}
			seen[name] = true
			if schema != "testdata/wire/json-integers" {
				t.Errorf("%s: schema %q, want testdata/wire/json-integers", name, schema)
			}
			if root != "Counts" {
				t.Errorf("%s: root %q, want Counts", name, root)
			}
			if jsonFile != expected.json {
				t.Errorf("%s: json file %q, want %q", name, jsonFile, expected.json)
			}
			if verdict != expected.verdict {
				t.Errorf("%s: verdict %q, want %q", name, verdict, expected.verdict)
			}
			if expected.reason != "-" && reason != expected.reason {
				t.Errorf("%s: reason %q, want %q", name, reason, expected.reason)
			}
			if _, err := os.Stat(filepath.Join(dir, jsonFile)); err != nil {
				t.Errorf("%s: json file: %v", name, err)
			}
		}
		for name := range want {
			if !seen[name] {
				t.Errorf("manifest omits vector %q", name)
			}
		}

		// Every *.json file beside the manifest is named by it.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if !seen[strings.TrimSuffix(e.Name(), ".json")] {
				t.Errorf("%s is not named by MANIFEST.tsv", e.Name())
			}
		}

		// The schema the rows name exists and compiles.
		if _, err := os.Stat(filepath.Join(dir, "JsonIntegers.schema")); err != nil {
			t.Errorf("the unit's schema: %v", err)
		}
	})

	t.Run("the corpus carries one wide kind of each shape", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "testdata", "wire", "json-integers")
		page, err := os.ReadFile(filepath.Join(dir, "JsonIntegers.schema"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"int32", "int64", "uint64", "int128", "fixed", "map[uint64]",
		} {
			if !strings.Contains(string(page), want) {
				t.Errorf("the unit does not declare %s", want)
			}
		}
	})
}
