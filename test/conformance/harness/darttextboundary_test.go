package main

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// Use the same table and exact BOM+{} document as the native adapter control.
// The engine accepts one leading mark, not a mark after whitespace or another
// mark. This is a byte-boundary rule, not permission to replace invalid UTF-8.
func TestDartTextBoundaryBOMOracle(t *testing.T) {
	_, _, units := corpus(t)
	unit, err := units.get("tabledemo")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	var canonical []byte
	for _, tc := range []struct {
		name, text string
		ok         bool
	}{
		{"plain", "{}\n", true},
		{"leading-bom", "\xef\xbb\xbf{}\n", true},
		{"double-bom", "\xef\xbb\xbf\xef\xbb\xbf{}\n", false},
		{"space-bom", " \xef\xbb\xbf{}\n", false},
		{"cut-bom", "\xef\xbb{}\n", false},
	} {
		value := model.New(model.Lookup("RootConfig"))
		var report tabletext.Report
		ok := model.Read(value, []byte(tc.text), &report)
		t.Logf("%s bytes=%x ok=%t report=%+v", tc.name, []byte(tc.text), ok, report)
		if ok != tc.ok {
			t.Fatalf("%s: ok=%t report=%+v, want accepted=%t", tc.name, ok, report, tc.ok)
		}
		if ok {
			got, err := model.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "plain" {
				canonical = got
			} else if !bytes.Equal(got, canonical) {
				t.Fatalf("%s changes the canonical value", tc.name)
			}
		}
	}
}

func TestDartIllFormedBoundaryClaim(t *testing.T) {
	t.Parallel()
	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	row := findHostile(t, m, "str-illformed-at-bound")
	if got := row.VerdictFor("dart"); got != "refused" {
		t.Fatalf("Dart invalid UTF-8 claim=%q, want refused", got)
	}
	wants, exemptions := hostileExpectations(m, "dart")
	for _, exemption := range exemptions {
		if bytes.Contains([]byte(exemption), []byte(row.Name)) {
			t.Fatalf("Dart still exempted: %s", exemption)
		}
	}
	for _, want := range wants {
		if want.name == row.Name && string(want.want) == "refused\n" {
			return
		}
	}
	t.Fatal("Dart invalid UTF-8 claim did not reach the scored expectation")
}
