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

// wideSitesSchema reaches every site the JSON reader places a wide integer
// at — a plain field, an array element, an optional, a union arm, a map value
// and an enum-keyed slot — and the wide kinds the shared Integers unit does
// not hold: uint128, bits(33..64) and a 128-bit fixed kind. Beside them sit
// the 32-bit kinds, which stay JSON numbers (docs/SPEC-TABLES.md §16.2).
const wideSitesSchema = `package widesites

enum Slot { Alpha, Beta }

union Arm
{
    wide   int64
    narrow int32
}

table Sites
{
    plain  int64
    arr    [..3]int64
    opt    ?uint64
    arm    Arm
    vals   map[uint8]uint64
    keyed  [Slot]int64
    u128   uint128
    b33    bits(33)
    b64    bits(64)
    f128   fixed(64, 64) | min = -10, max = 10
    narrow int32
    nbits  bits(32)
    nfixed fixed(16, 16) | min = -10, max = 10
    nu32   uint32
}
`

func wideSitesModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("Sites.schema", []byte(wideSitesSchema))
	if len(perrs) > 0 {
		t.Fatal(perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Sites.schema", Name: "Sites.schema", Base: "Sites", Bytes: []byte(wideSitesSchema), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatal(cerrs)
	}
	return tabletext.NewModel(u)
}

// TestTextWideIntegerRefusesNumberToken holds the ruling
// json-wide-integers-refuse-number-token (docs/rulings.json, 2026-11-10;
// docs/SPEC-TABLES.md §16.2): a JSON integer field whose kind is wider than 32
// bits takes the STRING form only, and a bare number token there refuses
// kind_mismatch. It reads every row of
// testdata/wire/json-integers/NUMBER-TOKEN.tsv, refused as kind_mismatch with
// its string twin read, then reaches each site and each wide kind the manifest
// cannot, and holds the 32-bit kinds to their number token.
func TestTextWideIntegerRefusesNumberToken(t *testing.T) {
	t.Parallel()

	t.Run("NUMBER-TOKEN.tsv", func(t *testing.T) {
		t.Parallel()
		m := jsonIntegersModel(t)
		dir := filepath.Join("..", "..", "testdata", "wire", "json-integers")
		data, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
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
				t.Fatalf("%s/NUMBER-TOKEN.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
			}
			name, root, jsonFile, verdict, what := fields[0], fields[1], fields[2], fields[3], fields[4]
			if verdict != "refused" {
				t.Errorf("%s: verdict %q; every NUMBER-TOKEN.tsv row is refused", name, verdict)
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
			var report tabletext.Report
			if m.Read(m.New(st), body, &report) || !report.Refused || !strings.HasPrefix(report.Reason, "kind_mismatch") {
				t.Errorf("%s: read = %+v, want a kind_mismatch refusal of a bare number in a wide field", name, report)
			}
			// the string twin the row names holds the same values as strings
			// and reads to them
			_, twin, found := strings.Cut(what, "string twin ")
			if !found {
				t.Errorf("%s: the description names no string twin: %q", name, what)
				continue
			}
			twinBody, err := os.ReadFile(filepath.Join(dir, strings.TrimSpace(twin)))
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			var twinReport tabletext.Report
			if !m.Read(m.New(st), twinBody, &twinReport) || twinReport.Refused {
				t.Errorf("%s: string twin %s refused: %+v", name, twin, twinReport)
			}
			rows++
		}
		if rows == 0 {
			t.Fatal("no NUMBER-TOKEN.tsv rows read; the gate would pass over an empty set")
		}
	})

	t.Run("sites and kinds", func(t *testing.T) {
		t.Parallel()
		m := wideSitesModel(t)
		st := m.Lookup("Sites")
		if st == nil {
			t.Fatal("no Sites table")
		}
		for _, tc := range []struct {
			name string
			text string
			read bool
		}{
			// every site: the bare number refused, its string read
			{"plain number", `{"plain": 5}`, false},
			{"plain string", `{"plain": "5"}`, true},
			{"array element number", `{"arr": ["1", 2]}`, false},
			{"array element string", `{"arr": ["1", "2"]}`, true},
			{"optional number", `{"opt": 7}`, false},
			{"optional string", `{"opt": "7"}`, true},
			{"optional null", `{"opt": null}`, true},
			{"union arm number", `{"arm": {"wide": 9}}`, false},
			{"union arm string", `{"arm": {"wide": "9"}}`, true},
			{"map value number", `{"vals": {"1": 18446744073709551615}}`, false},
			{"map value string", `{"vals": {"1": "18446744073709551615"}}`, true},
			{"enum-keyed slot number", `{"keyed": {"Alpha": "1", "Beta": -2}}`, false},
			{"enum-keyed slot string", `{"keyed": {"Alpha": "1", "Beta": "-2"}}`, true},
			// the kinds the shared Integers unit cannot reach
			{"uint128 number", `{"u128": 340282366920938463463374607431768211455}`, false},
			{"uint128 string", `{"u128": "340282366920938463463374607431768211455"}`, true},
			{"bits(33) number", `{"b33": 8589934591}`, false},
			{"bits(33) string", `{"b33": "8589934591"}`, true},
			{"bits(64) number", `{"b64": 1}`, false},
			{"bits(64) string", `{"b64": "18446744073709551615"}`, true},
			{"fixed128 number", `{"f128": 0.5}`, false},
			{"fixed128 string", `{"f128": "0.5"}`, true},
			// the 32-bit kinds stay JSON numbers
			{"int32 number", `{"narrow": -7}`, true},
			{"union narrow arm number", `{"arm": {"narrow": 3}}`, true},
			{"bits(32) number", `{"nbits": 4294967295}`, true},
			{"fixed32 number", `{"nfixed": 1.5}`, true},
			{"fixed32 integer token", `{"nfixed": 2}`, true},
			{"uint32 number", `{"nu32": 4294967295}`, true},
		} {
			var report tabletext.Report
			ok := m.Read(m.New(st), []byte(tc.text), &report)
			switch {
			case tc.read && (!ok || report.Refused):
				t.Errorf("%s: %s refused: %+v", tc.name, tc.text, report)
			case !tc.read && (ok || !report.Refused || !strings.HasPrefix(report.Reason, "kind_mismatch")):
				t.Errorf("%s: %s = %+v, want a kind_mismatch refusal", tc.name, tc.text, report)
			}
		}
	})
}
