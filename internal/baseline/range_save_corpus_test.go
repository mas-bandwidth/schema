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

func TestRangeTighteningSaveCorpus(t *testing.T) {
	for _, name := range []string{"enum-keyed-maximum", "scalar-maximum"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", "range-tightening", name)
			read := func(t *testing.T, file string) []byte {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			var want struct {
				Field   string
				Warning []string
				Saves   []struct {
					File     string
					Value    json.RawMessage
					NewReads bool
				}
			}
			if err := json.Unmarshal(read(t, "expected.json"), &want); err != nil {
				t.Fatal(err)
			}
			before := string(read(t, "before/Save.schema"))
			old := unit(t, before)
			live := unit(t, string(read(t, "after/Save.schema")))
			t.Run("baseline", func(t *testing.T) {
				// Replay creation of the old baseline in the running compiler's format.
				got := baseline.Diff(committed(t, before), baseline.Render(live), baseline.DefaultTokenPolicy)
				if len(got) != 1 || got[0].Verdict != baseline.Warn || got[0].Where != want.Field {
					t.Fatalf("expected one warning for %s; got:%s", want.Field, summary(got))
				}
				for _, part := range want.Warning {
					if !strings.Contains(got[0].What, part) {
						t.Errorf("expected warning to contain %q; got %q", part, got[0].What)
					}
				}
			})
			om, lm := tabletext.NewModel(old), tabletext.NewModel(live)
			for _, s := range want.Saves {
				t.Run(s.File, func(t *testing.T) {
					saved := read(t, s.File)
					oldValue := om.New(old.Tables["Save"])
					var report tabletext.Report
					if !om.Read(oldValue, s.Value, &report) {
						t.Fatalf("old schema refused fixture value %s: %+v", s.Value, report)
					}
					encoded, err := tablewire.Encode(om, oldValue)
					if err != nil || !bytes.Equal(encoded, saved) {
						t.Fatalf("old writer emitted %x (%v); saved file contains %x", encoded, err, saved)
					}
					v := lm.New(live.Tables["Save"])
					report = tabletext.Report{}
					ok, err := tablewire.Decode(lm, v, saved, &report)
					if ok != s.NewReads || (err == nil) != s.NewReads {
						t.Fatalf("new reader returned ok=%t, err=%v; expected read=%t", ok, err, s.NewReads)
					}
					if ok {
						back, err := tablewire.Encode(lm, v)
						if err != nil || !bytes.Equal(back, saved) {
							t.Errorf("valid save changed on round trip: %x (%v); expected %x", back, err, saved)
						}
					}
				})
			}
		})
	}
}
