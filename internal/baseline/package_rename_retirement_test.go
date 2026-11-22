package baseline_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPackageRenameKeepsRetirementLedger(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		body, err := os.ReadFile(filepath.Join("testdata", "package-rename-retirement", name))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	before := read("before.schema")
	removed := read("removed.schema")
	old := tabletext.NewModel(unit(t, string(before)))
	value := old.New(old.Lookup("First"))
	var placed tabletext.Report
	if !old.Read(value, read("old.json"), &placed) {
		t.Fatalf("old writer: %+v", placed)
	}
	saved := read("old.save")
	if rebuilt, err := tablewire.Encode(old, value); err != nil || !bytes.Equal(rebuilt, saved) {
		t.Fatalf("old writer made %x, want %x; err=%v", rebuilt, saved, err)
	}

	for _, tc := range []struct{ name, regenerated, restored string }{
		{"same-package", "removed.schema", "before.schema"},
		{"renamed-package", "renamed.schema", "restored.schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, paths := writeUnit(t, string(before))
			update := func(src []byte, reason string) {
				t.Helper()
				if err := os.WriteFile(paths[0], src, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, _, err := baseline.Update(unit(t, string(src)), paths, reason); err != nil {
					t.Fatal(err)
				}
			}
			ledger := func() *baseline.Unit {
				t.Helper()
				path := filepath.Join(dir, baseline.FileName)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				u, err := baseline.Parse(path, data)
				if err != nil {
					t.Fatal(err)
				}
				return u
			}
			update(before, "original schema")
			update(removed, "retire First.value")
			prior := ledger()
			if len(prior.Retired) != 1 || prior.Retired[0].Name != "First.value" {
				t.Fatalf("expected one active retirement, got %+v", prior.Retired)
			}
			update(read(tc.regenerated), "regenerate from current declarations")
			now := ledger()
			if len(now.Retired) != 1 || now.Retired[0] != prior.Retired[0] {
				t.Errorf("regeneration lost the machine-readable retirement: before=%+v after=%+v", prior.Retired, now.Retired)
			}
			if !strings.Contains(strings.Join(now.History, "\n"), "retire First.value") {
				t.Error("regeneration lost retirement history")
			}
			restored := unit(t, string(read(tc.restored)))
			if _, errs := baseline.Check(restored, paths); len(errs) == 0 {
				t.Error("a genuine reintroduction must still require acknowledgment")
			}

			// The original value really does populate the reintroduced field.
			model := tabletext.NewModel(restored)
			dst := model.New(model.Lookup("First"))
			var report tabletext.Report
			if ok, err := tablewire.Decode(model, dst, saved, &report); !ok || err != nil {
				t.Fatalf("old file: %v, %+v", err, report)
			}
			out, err := model.Write(dst)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]int
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got["value"] != 7 || got["keep"] != 8 {
				t.Fatalf("old save recovered as %s", out)
			}
		})
	}
}
