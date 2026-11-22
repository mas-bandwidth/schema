package baseline_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestCountAndMapSavedFileCorpus(t *testing.T) {
	for _, shape := range []string{"field", "arm", "map", "sort"} {
		t.Run(shape, func(t *testing.T) {
			read := func(name string) []byte {
				t.Helper()
				body, err := os.ReadFile(filepath.Join("testdata", "count-and-map", shape, name))
				if err != nil {
					t.Fatal(err)
				}
				return body
			}
			var expected struct {
				Verdict  string
				Contains string
				Saves    []struct {
					Name            string
					Input, Old, New json.RawMessage
					Refusal         string
				}
			}
			if err := json.Unmarshal(read("expected.json"), &expected); err != nil {
				t.Fatal(err)
			}
			before := unit(t, string(read("before.schema")))
			after := unit(t, string(read("after.schema")))
			oldModel := tabletext.NewModel(before)
			newModel := tabletext.NewModel(after)

			// Use this compiler's own rendering so an older compiler fails on
			// the missing or incorrect diagnostic, not a newer file format.
			_, paths := writeUnit(t, string(read("before.schema")))
			if _, _, err := baseline.Update(before, paths, "before migration"); err != nil {
				t.Fatal(err)
			}
			if warnings, refusals := baseline.Check(before, paths); len(warnings)+len(refusals) != 0 {
				t.Errorf("unchanged baseline: warnings=%v, refusals=%v", warnings, refusals)
			}
			warnings, refusals := baseline.Check(after, paths)
			verdict := "pass"
			if len(refusals) > 0 {
				verdict = "refuse"
			} else if len(warnings) > 0 {
				verdict = "warn"
			}
			if verdict != expected.Verdict || !strings.Contains(fmt.Sprint(warnings, refusals), expected.Contains) {
				t.Errorf("expected %s naming %s, got warnings=%v refusals=%v", expected.Verdict, expected.Contains, warnings, refusals)
			}

			for _, tc := range expected.Saves {
				t.Run(tc.Name, func(t *testing.T) {
					saved := read(tc.Name + ".save")
					value := oldModel.New(oldModel.Lookup("Root"))
					var placed tabletext.Report
					if !oldModel.Read(value, tc.Input, &placed) {
						t.Fatalf("old input: %+v", placed)
					}
					encoded, err := tablewire.Encode(oldModel, value)
					if err != nil || !bytes.Equal(encoded, saved) {
						t.Fatalf("old writer produced %x, want %x; err=%v", encoded, saved, err)
					}
					for _, reader := range []struct {
						name    string
						model   *tabletext.Model
						value   json.RawMessage
						refusal string
					}{{"old", oldModel, tc.Old, ""}, {"new", newModel, tc.New, tc.Refusal}} {
						v := reader.model.New(reader.model.Lookup("Root"))
						var report tabletext.Report
						ok, err := tablewire.Decode(reader.model, v, saved, &report)
						if reader.refusal != "" {
							if ok || err == nil || tablewire.RefusalReason(err) != reader.refusal {
								t.Errorf("%s reader: want %s, got ok=%v, err=%v", reader.name, reader.refusal, ok, err)
							}
							continue
						}
						if !ok || err != nil {
							t.Fatalf("%s reader: %+v, %v", reader.name, report, err)
						}
						got, err := reader.model.Write(v)
						if err != nil {
							t.Fatal(err)
						}
						var actual, want any
						if err := json.Unmarshal(got, &actual); err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal(reader.value, &want); err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(actual, want) {
							t.Errorf("%s reader: got %s, want %s", reader.name, got, reader.value)
						}
					}
				})
			}
		})
	}
}
