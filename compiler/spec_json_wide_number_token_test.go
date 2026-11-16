// TestSpecJsonWideIntegerRefusesNumberToken pins docs/SPEC-TABLES.md §16.2's
// string-only read for wide integers and docs/rulings.json
// `json-wide-integers-refuse-number-token`: an integer field whose kind is
// wider than 32 bits takes the STRING form only; a bare number token there
// refuses `kind_mismatch`. It also pins the refused number-token vectors,
// testdata/wire/json-integers/NUMBER-TOKEN.tsv.
package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// numberTokenRow is one row of testdata/wire/json-integers/NUMBER-TOKEN.tsv:
// a refused vector that writes the named wide fields as bare number tokens,
// and its string twin in MANIFEST.tsv, which holds the same values as strings.
type numberTokenRow struct {
	name   string
	fields []string
	twin   string
}

// numberTokenRows is every row NUMBER-TOKEN.tsv must name: one per wide field
// of the Integers unit that a bare number token can reach, and the `numbers`
// row (every wide field at once) moved out of MANIFEST.tsv.
var numberTokenRows = []numberTokenRow{
	{"wide-number", []string{"wide"}, "canonical.read.json"},
	{"uwide-number", []string{"uwide"}, "canonical.read.json"},
	{"wide128-number", []string{"wide128"}, "canonical.read.json"},
	{"ranged-number", []string{"ranged"}, "canonical.read.json"},
	{"frac-number", []string{"frac"}, "canonical.read.json"},
	{"numbers", []string{"wide", "uwide", "wide128", "ranged", "frac"}, "strings.read.json"},
	// the unlawful values a bare number token spelled, moved out of
	// MANIFEST.tsv: each twin holds the same digits in a string and refuses
	// for them
	{"wide-fraction", []string{"wide"}, "wide-string-fraction.refused.json"},
	{"wide-exponent", []string{"wide"}, "wide-string-exponent.refused.json"},
	{"wide-neg-zero", []string{"wide"}, "wide-string-neg-zero.refused.json"},
	{"ranged-one-past", []string{"ranged"}, "ranged-one-past-string.refused.json"},
	{"ranged-below-min", []string{"ranged"}, "ranged-below-min-string.refused.json"},
	{"uwide-one-past", []string{"uwide"}, "uwide-one-past-string.refused.json"},
	{"wide128-one-past", []string{"wide128"}, "wide128-one-past-string.refused.json"},
}

// decodeVector reads a JSON vector keeping every number token as its text.
func decodeVector(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the vector %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("the vector %s is not a JSON object: %v", path, err)
	}
	return v
}

func TestSpecJsonWideIntegerRefusesNumberToken(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 states the string-only read", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 ", "### 16.3 ")

		mustState(t, "§16.2 the wide integer reads as the string only",
			section, "reads as that STRING only; a bare number token there refuses `kind_mismatch`")
		mustState(t, "§16.2 the int128 row reads as the string only",
			section, "Reads as that STRING only")
		mustState(t, "§16.2 the number read is scoped to a fixed kind of 32 bits and under",
			section, "A fixed kind of 32 bits and under is written as the shortest EXACT decimal with at least one fractional digit and read from any number token")
		mustNotState(t, "§16.2 no longer reads a wide integer as a number",
			section, "reads as a string or a number")
	})

	t.Run("docs/rulings.json carries the ruling", func(t *testing.T) {
		t.Parallel()
		rulings := readRulings(t)

		r, ok := rulings["json-wide-integers-refuse-number-token"]
		if !ok {
			t.Fatal(`docs/rulings.json carries no ruling "json-wide-integers-refuse-number-token"`)
		}
		if r.Date != "2026-11-10" {
			t.Errorf("json-wide-integers-refuse-number-token date is %q, want 2026-11-10", r.Date)
		}
		const rulingText = "ruled 2026-11-10: a JSON integer field whose kind is wider than 32 bits takes the STRING form only; a bare number token there refuses kind_mismatch"
		if len(r.Quotes) != 1 || r.Quotes[0] != rulingText {
			t.Errorf("json-wide-integers-refuse-number-token quotes are %q, want exactly the ruling text %q", r.Quotes, rulingText)
		}
		if !strings.Contains(r.Source, "strict wins") {
			t.Errorf("json-wide-integers-refuse-number-token source is %q", r.Source)
		}
	})

	t.Run("NUMBER-TOKEN.tsv names each refused number row with its string twin", func(t *testing.T) {
		t.Parallel()
		const dir = "../testdata/wire/json-integers"
		data, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
		if err != nil {
			t.Fatalf("the number-token manifest: %v", err)
		}
		type row struct {
			root    string
			json    string
			verdict string
			what    string
		}
		named := make(map[string]row)
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) != 5 || fields[0] == "" || fields[1] == "" || fields[2] == "" || fields[3] == "" {
				t.Fatalf("%s/NUMBER-TOKEN.tsv:%d: a vector line is <name> TAB <root> TAB <json> TAB <verdict> TAB <what it holds>, got %q", dir, i+1, line)
			}
			if _, dup := named[fields[0]]; dup {
				t.Errorf("%s/NUMBER-TOKEN.tsv:%d: %q is named twice", dir, i+1, fields[0])
			}
			named[fields[0]] = row{root: fields[1], json: fields[2], verdict: fields[3], what: fields[4]}
		}
		if len(named) != len(numberTokenRows) {
			t.Errorf("%s/NUMBER-TOKEN.tsv names %d vectors, want %d", dir, len(named), len(numberTokenRows))
		}

		for _, want := range numberTokenRows {
			r, ok := named[want.name]
			if !ok {
				t.Errorf("%s/NUMBER-TOKEN.tsv names no vector %q", dir, want.name)
				continue
			}
			if r.root != "Integers" {
				t.Errorf("%s: root %q, want Integers", want.name, r.root)
			}
			if r.verdict != "refused" {
				t.Errorf("%s: verdict %q, want refused", want.name, r.verdict)
			}
			wantFile := want.name + ".refused.json"
			if r.json != wantFile {
				t.Errorf("%s: json file %q, want %q", want.name, r.json, wantFile)
				continue
			}
			if !strings.Contains(r.what, want.twin) {
				t.Errorf("%s: the row does not name its string twin %q: %q", want.name, want.twin, r.what)
			}

			refused := decodeVector(t, filepath.Join(dir, wantFile))
			twin := decodeVector(t, filepath.Join(dir, want.twin))
			wide := make(map[string]bool, len(want.fields))
			for _, field := range want.fields {
				wide[field] = true
				n, isNumber := refused[field].(json.Number)
				if !isNumber {
					t.Errorf("%s: the field %q is %#v, want a bare number token", want.name, field, refused[field])
					continue
				}
				s, isString := twin[field].(string)
				if !isString {
					t.Errorf("%s: the twin %s's field %q is %#v, want a string", want.name, want.twin, field, twin[field])
					continue
				}
				if string(n) != s {
					t.Errorf("%s: the field %q is the number %s, its twin the string %q: want the same digits", want.name, field, n, s)
				}
			}
			if len(refused) != len(twin) {
				t.Errorf("%s: %d fields, its twin %s %d", want.name, len(refused), want.twin, len(twin))
			}
			for key, v := range twin {
				if wide[key] {
					continue
				}
				if !reflect.DeepEqual(refused[key], v) {
					t.Errorf("%s: the field %q is %#v, want its twin's %#v", want.name, key, refused[key], v)
				}
			}
		}
	})
}
