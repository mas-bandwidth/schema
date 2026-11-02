package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// The shared entry fixture is separate from the global corpus: no verdict is
// assigned to an expansion port. The nightly reference job runs this oracle
// once, then each original-nine driver reads the same four files once.
func TestBOMEntryCorpusOracle(t *testing.T) {
	_, _, units := corpus(t)
	m, err := ReadManifest("testdata/conformance/tables/bom-entry/MANIFEST.txt", defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Hostiles) != 4 {
		t.Fatalf("BOM entry fixture has %d rows, want 4", len(m.Hostiles))
	}
	unit, err := units.get("tabledemo")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	typ := model.Lookup("RootConfig")
	plain := model.New(typ)
	var plainReport tabletext.Report
	if !model.Read(plain, []byte("{}\n"), &plainReport) || plainReport != (tabletext.Report{}) {
		t.Fatalf("plain control refused or reported events: %+v", plainReport)
	}
	canonical, err := model.Write(plain)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := map[string]string{
		"leading-bom": "\xef\xbb\xbf{}\n",
		"double-bom":  "\xef\xbb\xbf\xef\xbb\xbf{}\n",
		"space-bom":   " \xef\xbb\xbf{}\n",
		"cut-bom":     "\xef\xbb{}\n",
	}
	for _, row := range m.Hostiles {
		t.Run(row.Name, func(t *testing.T) {
			want, known := wantBytes[row.Name]
			if !known {
				t.Fatalf("unexpected BOM entry case %q", row.Name)
			}
			delete(wantBytes, row.Name)
			text, err := os.ReadFile(filepath.Join(row.Tree, row.Root+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(text) != want || row.Unit != "tabledemo" || row.Root != "RootConfig" || len(row.CarriedBy) != 0 || row.Uncarried != "" {
				t.Fatalf("BOM entry fixture drift: row=%+v bytes=%x", row, text)
			}
			value := model.New(typ)
			var report tabletext.Report
			accepted := model.Read(value, text, &report)
			wantAccepted := row.Name == "leading-bom"
			t.Logf("bytes=%x accepted=%t report=%+v", text, accepted, report)
			if accepted != wantAccepted || report.Malformed == wantAccepted || row.Refused == wantAccepted {
				t.Fatalf("accepted=%t report=%+v row refusal=%t", accepted, report, row.Refused)
			}
			if accepted {
				if report != (tabletext.Report{}) || row.Verdict() != "0,0,0,0,0,false,read" {
					t.Fatalf("accepted BOM changed the report: %+v / %s", report, row.Verdict())
				}
				encoded, err := model.Write(value)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(encoded, canonical) {
					t.Fatal("one leading BOM changes the canonical value")
				}
			}
		})
	}
}
