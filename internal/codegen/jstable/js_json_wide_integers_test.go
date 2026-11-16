package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsJsonIntegersSchema is the unit testdata/wire/json-integers names: an int32,
// an int64, a uint64, a ranged int128, a ranged int64, a wide fixed-point field
// and a map keyed by uint64 (docs/SPEC-TABLES.md §16.2).
const jsJsonIntegersSchema = `package jsonintegers

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
`

// TestJsJsonWideIntegersAreStrings holds the JavaScript JSON walk to the two
// integer rules of docs/SPEC-TABLES.md §16.2 and docs/rulings.json
// json-wide-integers-are-strings, json-integer-token-only: a kind wider than 32
// bits is written and read as a string of decimal digits, and every integer field takes an integer token only — no fraction,
// no exponent, no negative zero — with the range tested on the exact value.
func TestJsJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it executes the generated JSON integer walk)")
	u := unitFrom(t, jsJsonIntegersSchema)
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	// ToJson answers a text with one trailing line feed (§16.1); the committed
	// vector is the engine's text for the same values without it.
	canonicalText := strings.TrimRight(string(canonical), "\r\n") + "\n"
	type row struct {
		Name        string `json:"name"`
		Text        string `json:"text"`
		Read        bool   `json:"read"`
		Canonical   string `json:"canonical"`
		NumberToken bool   `json:"numberToken"`
	}
	var rows []row
	for _, manifestName := range []string{"MANIFEST.tsv", "NUMBER-TOKEN.tsv"} {
		manifest, err := os.ReadFile(filepath.Join(dir, manifestName))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for i, line := range strings.Split(string(manifest), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) != 5 {
				t.Fatalf("MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", i+1, len(fields))
			}
			name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
			if root != "Integers" {
				t.Fatalf("%s: root %q, want Integers", name, root)
			}
			if verdict != "read" && verdict != "refused" {
				t.Fatalf("%s: verdict %q, want read or refused", name, verdict)
			}
			body, err := os.ReadFile(filepath.Join(dir, jsonFile))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			r := row{Name: name, Text: string(body), Read: verdict == "read", NumberToken: manifestName == "NUMBER-TOKEN.tsv"}
			if r.Read {
				r.Canonical = canonicalText
			}
			rows = append(rows, r)
			count++
		}
		if manifestName == "NUMBER-TOKEN.tsv" && count != 13 {
			t.Fatalf("NUMBER-TOKEN.tsv has %d rows, want 13", count)
		}
	}
	if len(rows) == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", jsJsonWideIntegersDriver, rows)
	for _, r := range rows {
		if !strings.Contains(out, r.Name+" OK\n") && !strings.Contains(out, r.Name+" REFUSED\n") {
			t.Errorf("missing %s\n%s", r.Name, out)
		}
	}
}

const jsJsonWideIntegersDriver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const rows = JSON.parse(readFileSync(process.argv[2], "utf8"));
const m = await import(pathToFileURL(process.argv[3]).href);
const check = (p, s) => { if (!p) { throw new Error(s); } };
for (const row of rows) {
  const report = new m.TableVariableReport();
  const v = m.IntegersFromJson(row.text, report);
  if (!row.read) {
    check(v === null, row.name + ": FromJson accepted a refused row");
    if (row.numberToken) check(report.err() === "kind_mismatch", row.name + ": wanted kind_mismatch, got " + report.err());
    console.log(row.name + " REFUSED");
    continue;
  }
  check(v !== null, row.name + ": FromJson refused an accepted row " + report.reason);
  const text = m.IntegersToJson(v);
  check(text === row.canonical, row.name + ": ToJson moved the canonical text\nwrote\n" + text + "want\n" + row.canonical);
  check(v.small === 7, row.name + ": small");
  check(v.wide === -42n, row.name + ": wide");
  check(v.uwide === 18446744073709551615n, row.name + ": uwide");
  check(v.wide128 === 170141183460469231731687303715884105727n, row.name + ": wide128");
  check(v.ranged === 9007199254740993n, row.name + ": ranged");
  check(v.frac === 32768n, row.name + ": frac");
  check(v.counts.size === 2 && v.counts.get(0n) === 1 && v.counts.get(9007199254740993n) === 2, row.name + ": counts");
  console.log(row.name + " OK");
}
// THE WRITER ALONE: the canonical instance built field by field, with no read
// in the path, spells the committed canonical text byte for byte.
const c = new m.Integers();
c.small = 7;
c.wide = -42n;
c.uwide = 18446744073709551615n;
c.wide128 = 170141183460469231731687303715884105727n;
c.ranged = 9007199254740993n;
c.frac = 32768n;
c.counts = new Map([[0n, 1], [9007199254740993n, 2]]);
const canonical = rows.find((r) => r.name === "canonical").canonical;
const written = m.IntegersToJson(c);
check(written === canonical, "direct write moved the canonical text\nwrote\n" + written + "want\n" + canonical);
console.log("write OK");
`

// TestJsJsonWideIntegerSites covers each position that delegates to the
// scalar JSON reader. Each number token has a string twin; narrow numbers stay
// accepted. The corpus above separately tests exact limits and spelling.
func TestJsJsonWideIntegerSites(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it executes the generated JSON reader)")
	const schema = `package sites
 enum Key { A, B }
 union Pick { value int64 }
 table Sites {
   plain int64
   arr [..2]int64
   opt ?uint64
   pick Pick
   entries map[int32]int64
   keyed [Key]int64
   narrow int32
   mask bits(40)
   amount fixed(48,16) | min = -10, max = 10
   large uint128
   fixed128 fixed(112,16) | min = -10, max = 10
   smallfixed fixed(16,16) | min = -10, max = 10
 }
 `
	rows := []struct {
		Name, Text string
		Read       bool
	}{
		{"plain-number", `{"plain":7}`, false}, {"plain-string", `{"plain":"7"}`, true},
		{"array-number", `{"arr":[7]}`, false}, {"array-string", `{"arr":["7"]}`, true},
		{"optional-number", `{"opt":7}`, false}, {"optional-string", `{"opt":"7"}`, true},
		{"arm-number", `{"pick":{"value":7}}`, false}, {"arm-string", `{"pick":{"value":"7"}}`, true},
		{"map-number", `{"entries":{"1":7}}`, false}, {"map-string", `{"entries":{"1":"7"}}`, true},
		{"keyed-number", `{"keyed":{"A":7}}`, false}, {"keyed-string", `{"keyed":{"A":"7"}}`, true},
		{"bits-number", `{"mask":7}`, false}, {"bits-string", `{"mask":"7"}`, true},
		{"fixed-number", `{"amount":1}`, false}, {"fixed-string", `{"amount":"1"}`, true},
		{"uint128-number", `{"large":7}`, false}, {"uint128-string", `{"large":"7"}`, true},
		{"fixed128-number", `{"fixed128":1}`, false}, {"fixed128-string", `{"fixed128":"1"}`, true},
		{"smallfixed-number", `{"smallfixed":1.5}`, true},
		{"narrow-number", `{"narrow":7}`, true},
	}
	u := unitFrom(t, schema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	const driver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const rows = JSON.parse(readFileSync(process.argv[2], "utf8"));
const m = await import(pathToFileURL(process.argv[3]).href);
for (const row of rows) {
 const r = new m.TableVariableReport();
 const value = m.SitesFromJson(row.Text, r);
 if ((value !== null) !== row.Read) throw new Error(row.Name + ": unexpected verdict " + r.err());
 if (!row.Read && r.err() !== "kind_mismatch") throw new Error(row.Name + ": wanted kind_mismatch, got " + r.err());
 if (row.Name === "narrow-number" && value.narrow !== 7) throw new Error("narrow value moved");
 console.log(row.Name + " OK");
}
`
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", driver, rows)
	for _, row := range rows {
		if !strings.Contains(out, row.Name+" OK\n") {
			t.Errorf("missing %s", row.Name)
		}
	}
}
