package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// jsonIntegersSchemaPath is the small unit the json-integers vectors read,
// committed beside their manifests: an int32, an int64, a uint64, a ranged
// int128, a ranged int64, a wide fixed-point field and a map keyed by uint64 —
// every integer storage width the two JSON integer rules name
// (docs/SPEC-TABLES.md §16.2).
const jsonIntegersSchemaPath = "../../testdata/wire/json-integers/Integers.schema"

func jsonIntegersModel(t *testing.T) *tabletext.Model {
	t.Helper()
	jsonIntegersSchema, err := os.ReadFile(jsonIntegersSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse("Integers.schema", jsonIntegersSchema)
	if len(perrs) > 0 {
		t.Fatal(perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Integers.schema", Name: "Integers.schema", Base: "Integers", Bytes: jsonIntegersSchema, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatal(cerrs)
	}
	return tabletext.NewModel(u)
}

// TestTextIntegersWideAreStringsAndTokensAreIntegers holds every manifest row
// of testdata/wire/json-integers/ through the engine's writer and reader: a
// wide integer is written as a STRING of decimal digits and reads as that
// STRING only — its bare number token refuses kind_mismatch, the rows of
// NUMBER-TOKEN.tsv that TestTextWideIntegerRefusesNumberToken holds; an
// integer field takes an integer token only — no fraction, no exponent, no
// negative zero — the value never passes through a double, and its range is
// tested on the exact value.
func TestTextIntegersWideAreStringsAndTokensAreIntegers(t *testing.T) {
	t.Parallel()
	m := jsonIntegersModel(t)
	dir := filepath.Join("..", "..", "testdata", "wire", "json-integers")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
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
		st := m.Lookup(root)
		if st == nil {
			t.Errorf("%s: no root %q in the unit", name, root)
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, jsonFile))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		inst := m.New(st)
		var report tabletext.Report
		ok := m.Read(inst, body, &report)
		switch verdict {
		case "read":
			if !ok || report.Refused {
				t.Errorf("%s: read refused %q: %+v", name, body, report)
				continue
			}
			// a value the read calls clean must write back and read again:
			// the writer spells every wide integer as a string of digits, and
			// its own output is a fixed point of the reader
			out, err := m.Write(inst)
			if err != nil {
				t.Errorf("%s: the writer refused a value the read accepted: %v", name, err)
				continue
			}
			back := m.New(st)
			var again tabletext.Report
			if !m.Read(back, out, &again) || again.Refused {
				t.Errorf("%s: the writer's own text did not read back: %+v\n%s", name, again, out)
				continue
			}
			if name == "canonical" && string(out) != string(body) {
				t.Errorf("canonical: read then write moved the text\nwrote:\n%s\nwant:\n%s", out, body)
			}
		case "refused":
			if ok || !report.Refused {
				t.Errorf("%s: read accepted %q", name, body)
			}
		}
		rows++
	}
	if rows == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}
}
