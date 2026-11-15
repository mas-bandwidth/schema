package mootable_test

// THE JSON WIDE-INTEGER VECTORS (docs/SPEC-TABLES.md §16.2, §16.14, the
// Numbers rule; docs/rulings.json json-wide-integers-are-strings and
// json-integer-token-only). One small unit holds an int32, an int64, a uint64,
// a ranged int128, a ranged int64, a wide fixed-point field and a map keyed by
// uint64. An integer wider than 32 bits is written as a STRING of decimal
// digits and reads as a string OR a number, the same digits; every integer
// takes an integer token only — digits with an optional minus, no fraction, no
// exponent, not -0 — and its range is tested on the exact value.

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsonWidePrelude is the moo every probe starts with: the inputs and hexs(s),
// which prints one string as one hex line so any byte survives.
const jsonWidePrelude = `--!strict
local inputs = require("./inputs")
local function hexs(s: string)
	local parts = table.create(#s)
	for i = 1, #s do
		parts[i] = string.format("%02x", string.byte(s, i) :: number)
	end
	print(table.concat(parts))
end
local _ = inputs
local _ = hexs
`

func jsonWideHexLines(t *testing.T, out string) []string {
	t.Helper()
	text := strings.TrimRight(out, "\n")
	if text == "" {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		b, err := hex.DecodeString(l)
		if err != nil {
			t.Fatalf("probe output line %q is not hex: %v\n%s", l, err, out)
		}
		lines = append(lines, string(b))
	}
	return lines
}

// jsonIntegersVectorSchema is the unit the vectors read (the one the engine's
// own vector test names).
const jsonIntegersVectorSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105727, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

// jsonWideVectorsRun reads every manifest row with the generated MOO walk and
// returns one answer per row: whether the read succeeded and, for a text it
// reads, the canonical text it writes back.
type jsonWideAnswer struct {
	name    string
	want    string
	verdict string
	text    string
}

func jsonWideVectorsRun(t *testing.T) ([]jsonWideAnswer, []byte) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		name, root, file, verdict := fields[0], fields[1], fields[2], fields[3]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, verdict)
		}
		if root != "Integers" {
			t.Fatalf("%s: root %q, want Integers", name, root)
		}
		body, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inputs[name] = body
		rows = append(rows, row{name: name, verdict: verdict})
	}
	if len(rows) == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}

	g := generateFiles(t, map[string]string{"Integers.schema": jsonIntegersVectorSchema})
	var b strings.Builder
	b.WriteString(jsonWidePrelude)
	b.WriteString("local I: any = require(\"./IntegersTable\")\n")
	b.WriteString(`local function run(name: string)
	local v = I.NewIntegers()
	local report: any = {}
	local ok = I.IntegersFromJson(v, inputs[name], report)
	if not ok then
		hexs("refused")
		hexs("refused")
		return
	end
	hexs("read")
	local size = I.IntegersToJsonMeasure(v)
	if size < 0 then
		hexs("refused")
		return
	end
	local out = buffer.create(size)
	if I.IntegersToJson(v, out) ~= size then
		hexs("measure and write disagree")
		return
	end
	hexs(buffer.tostring(out))
end
`)
	for _, r := range rows {
		b.WriteString("run(" + luaQuote(r.name) + ")\n")
	}
	out := g.run(t, b.String(), inputs)
	lines := jsonWideHexLines(t, out)
	if len(lines) != 2*len(rows) {
		t.Fatalf("the probe printed %d lines for %d cases:\n%s", len(lines), len(rows), strings.Join(lines, "\n"))
	}
	answers := make([]jsonWideAnswer, len(rows))
	for i, r := range rows {
		answers[i] = jsonWideAnswer{name: r.name, want: r.verdict, verdict: lines[2*i], text: lines[2*i+1]}
	}
	return answers, canonical
}

// luaQuote renders a Go string as a moo string literal.
func luaQuote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s) + "\""
}

// TestMooJsonWideIntegersAreStrings holds every vector of the unit: every wide
// field is written as a string of decimal digits and reads as a string or a
// number to the same value; every integer field refuses a fraction, an
// exponent, -0 and a value one past its range at 2^53 and above.
func TestMooJsonWideIntegersAreStrings(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form)")
	t.Parallel()
	answers, canonical := jsonWideVectorsRun(t)
	want := string(canonical)
	if !strings.HasSuffix(want, "\n") {
		// the committed canonical text is the body; the writer ends it with
		// exactly one line feed (docs/SPEC-TABLES.md §16.14, W2)
		want += "\n"
	}
	for _, a := range answers {
		switch a.want {
		case "refused":
			if a.verdict != "refused" {
				t.Errorf("%s: read %q, want refused", a.name, a.text)
			}
		case "read":
			if a.verdict != "read" {
				t.Errorf("%s: %s %q, want read", a.name, a.verdict, a.text)
				continue
			}
			if a.text != want {
				t.Errorf("%s: wrote\n%s\nwant the canonical text\n%s", a.name, a.text, want)
			}
		}
	}
}
