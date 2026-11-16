package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// jsonWideIntegersUnit is the small unit testdata/wire/json-integers reads, plus
// one table that carries a wide integer at every other text site: an array
// element, a map value, an optional and a union arm
// (docs/SPEC-TABLES.md §16.2, docs/rulings.json
// json-wide-integers-are-strings and json-integer-token-only).
const jsonWideIntegersUnit = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}

union Pick {
    count int64
    name string(8)
}

table Sites
{
    field  int64
    elems  []int64
    felems []fixed(48, 16) | min = -10, max = 10
    values map[uint64]int64
    maybe  ?int64
    pick   Pick
    keyed  [Key]int64
    u128   uint128
    b33    bits(33)
    f128   fixed(64, 64) | min = -10, max = 10
}

enum Key { A, B }

flags Mode { A, B, C }

table FlagSites
{
    f  Mode
    fs []Mode
}
`

// TestElixirJsonWideIntegersAreStrings holds the Elixir JSON writer and reader
// to docs/SPEC-TABLES.md §16.2's two integer rules: an integer wider than 32
// bits is written and read as a string of decimal digits, and every integer field takes an integer token only — no fraction,
// no exponent, no negative zero — with the value never passing through a
// double. Every manifest row of testdata/wire/json-integers is answered here,
// and the canonical read row is compared with the committed text byte for byte.
func TestElixirJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, jsonWideIntegersUnit)
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The vector file is the canonical text's body; the text writer ends the
	// document with one line feed (docs/SPEC-TABLES.md §16.14 W2).
	if len(canonical) == 0 || canonical[len(canonical)-1] != '\n' {
		canonical = append(canonical, '\n')
	}
	model := tabletext.NewModel(u)

	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    Process.put(:failed, true)
  end
end
`)
	rows := 0
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
		if verdict != "read" && verdict != "refused" {
			t.Errorf("%s: verdict %q, want read or refused", name, verdict)
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, jsonFile))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		textHex := hex.EncodeToString(body)
		switch verdict {
		case "read":
			wantHex := hex.EncodeToString(canonical)
			fmt.Fprintf(&probe, `
case Jsonintegers.Table.from_json(%q, Base.decode16!(%q, case: :mixed)) do
  {:ok, value, report} ->
    Probe.check(report.unknown == 0 and report.widened == 0, %q <> " read report: " <> inspect(report))
    case Jsonintegers.Table.to_json(%q, value) do
      {:ok, text} ->
        Probe.check(text == Base.decode16!(%q, case: :mixed), %q <> " canonical text: " <> inspect(text))
      other -> Probe.check(false, %q <> " to_json: " <> inspect(other))
    end
  other -> Probe.check(false, %q <> " refused: " <> inspect(other))
end
`, root, textHex, name, root, wantHex, name, name, name)
		case "refused":
			fmt.Fprintf(&probe, `
case Jsonintegers.Table.from_json(%q, Base.decode16!(%q, case: :mixed)) do
  {:ok, _value, _report} -> Probe.check(false, %q <> " accepted")
  {:error, _reason, _report} -> :ok
  other -> Probe.check(false, %q <> " unexpected: " <> inspect(other))
end
`, root, textHex, name, name)
		}
		rows++
	}
	if rows == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}

	// Each wide number-token fixture must refuse the complete read.
	tokenManifest, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	tokenRows := 0
	for _, line := range strings.Split(string(tokenManifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) != 5 || f[3] != "refused" {
			t.Fatalf("NUMBER-TOKEN.tsv: invalid row %q", line)
		}
		body, err := os.ReadFile(filepath.Join(dir, f[2]))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, `
case Jsonintegers.Table.from_json(%q, Base.decode16!(%q, case: :mixed)) do
  {:error, :kind_mismatch, %%{reason: "kind_mismatch"}} -> :ok
  other -> Probe.check(false, %q <> " expected kind_mismatch: " <> inspect(other))
end
`, f[1], hex.EncodeToString(body), f[0])
		tokenRows++
	}
	if tokenRows != 13 {
		t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", tokenRows)
	}

	// Every other integer site: a field, an array element, a map value, an
	// optional, union arm and enum-keyed slot. Wide values require strings.
	sites := []struct {
		name    string
		text    string
		refused bool
	}{
		{"sites-strings", `{
  "field": "9007199254740993",
  "elems": ["-42", "9007199254740993"],
  "felems": ["0.5", "-0.25"],
  "values": { "9007199254740993": "1234567890123456789" },
  "maybe": "7",
  "pick": { "count": "9007199254740993" },
  "keyed": { "A": "1" },
  "u128": "340282366920938463463374607431768211455",
  "b33": "8589934591",
  "f128": "0.5"
}`, false},
		{"sites-numbers", `{
  "field": 9007199254740993,
  "elems": [-42, 9007199254740993],
  "felems": [0.5, -0.25],
  "values": { "9007199254740993": 1234567890123456789 },
  "maybe": 7,
  "pick": { "count": 9007199254740993 }
}`, true},
		{"field-number", `{"field":1}`, true},
		{"element-number", `{"elems":[1]}`, true},
		{"fixed-element-number", `{"felems":[0.5]}`, true},
		{"map-number", `{"values":{"1":1}}`, true},
		{"optional-number", `{"maybe":1}`, true},
		{"arm-number", `{"pick":{"count":1}}`, true},
		{"keyed-number", `{"keyed":{"A":1}}`, true},
		{"u128-number", `{"u128":1}`, true},
		{"bits33-number", `{"b33":1}`, true},
		{"fixed128-number", `{"f128":0.5}`, true},
		{"sites-absent-optional", `{
  "field": "1",
  "elems": [],
  "felems": [],
  "values": {},
  "maybe": null,
  "pick": {}
}`, false},
		{"elems-fraction", `{"elems":[1.5]}`, true},
		{"elems-exponent", `{"elems":[1e3]}`, true},
		{"elems-neg-zero", `{"elems":[-0]}`, true},
		{"values-fraction", `{"values":{"1":1.5}}`, true},
		{"values-exponent", `{"values":{"1":1e3}}`, true},
		{"values-neg-zero", `{"values":{"1":-0}}`, true},
		{"optional-fraction", `{"maybe":1.5}`, true},
		{"optional-exponent", `{"maybe":1e3}`, true},
		{"optional-neg-zero", `{"maybe":-0}`, true},
		{"arm-fraction", `{"pick":{"count":1.5}}`, true},
		{"arm-exponent", `{"pick":{"count":1e3}}`, true},
		{"arm-neg-zero", `{"pick":{"count":-0}}`, true},
	}
	for _, tc := range sites {
		st := model.Lookup("Sites")
		inst := model.New(st)
		var report tabletext.Report
		ok := model.Read(inst, []byte(tc.text), &report)
		if tc.refused {
			if ok || !report.Refused {
				t.Fatalf("%s: the reference engine accepted %s", tc.name, tc.text)
			}
			fmt.Fprintf(&probe, `
case Jsonintegers.Table.from_json("Sites", Base.decode16!(%q, case: :mixed)) do
  {:error, :kind_mismatch, %%{reason: "kind_mismatch"}} -> :ok
  other -> Probe.check(false, %q <> " expected kind_mismatch: " <> inspect(other))
end
`, hex.EncodeToString([]byte(tc.text)), tc.name)
			continue
		}
		if !ok || report.Refused {
			t.Fatalf("%s: the reference engine refused %s: %+v", tc.name, tc.text, report)
		}
		canon, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		canon = append(canon, '\n')
		fmt.Fprintf(&probe, `
case Jsonintegers.Table.from_json("Sites", Base.decode16!(%q, case: :mixed)) do
  {:ok, value, report} ->
    Probe.check(report.unknown == 0 and report.widened == 0, %q <> " read report: " <> inspect(report))
    case Jsonintegers.Table.to_json("Sites", value) do
      {:ok, text} ->
        Probe.check(text == Base.decode16!(%q, case: :mixed), %q <> " canonical text: " <> inspect(text))
      other -> Probe.check(false, %q <> " to_json: " <> inspect(other))
    end
  other -> Probe.check(false, %q <> " refused: " <> inspect(other))
end
`, hex.EncodeToString([]byte(tc.text)), tc.name, hex.EncodeToString(canon), tc.name, tc.name, tc.name)
	}

	// A flags value is its list of set variant names, never an integer string:
	// a numeric string refuses at the field and at the array element rather
	// than reading as a raw mask (docs/SPEC-TABLES.md §16.2).
	flagSites := []struct {
		name string
		text string
	}{
		{"flags-field-string", `{"f":"3"}`},
		{"flags-elem-string", `{"fs":["3"]}`},
	}
	for _, tc := range flagSites {
		st := model.Lookup("FlagSites")
		inst := model.New(st)
		var report tabletext.Report
		if ok := model.Read(inst, []byte(tc.text), &report); ok || !report.Refused {
			t.Fatalf("%s: the reference engine accepted %s", tc.name, tc.text)
		}
		fmt.Fprintf(&probe, `
case Jsonintegers.Table.from_json("FlagSites", Base.decode16!(%q, case: :mixed)) do
  {:ok, _value, _report} -> Probe.check(false, %q <> " accepted")
  {:error, _reason, _report} -> :ok
  other -> Probe.check(false, %q <> " unexpected: " <> inspect(other))
end
`, hex.EncodeToString([]byte(tc.text)), tc.name, tc.name)
	}

	probe.WriteString("\nif Process.get(:failed), do: raise(\"wide integer assertions failed\")\nIO.puts(\"JSON_WIDE_INTEGERS_OK\")\n")
	out := runCompleteUnitProbe(t, u, probe.String())
	if !strings.Contains(out, "JSON_WIDE_INTEGERS_OK") {
		t.Fatal(out)
	}
}
