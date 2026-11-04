package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Each report reader changes only package identity and declared table class.
// In particular, bounds, defaults, aliases, names and nested declarations must
// remain identical to the historical fixture's schema.
func TestReportReadersPreserveFieldDeclarations(t *testing.T) {
	root := conformanceRoot(t)
	comments := regexp.MustCompile(`(?m)//[^\n]*`)
	pkg := regexp.MustCompile(`(?m)^package \w+`)
	normalize := func(data []byte) string {
		s := comments.ReplaceAllString(string(data), "")
		s = pkg.ReplaceAllString(s, "package report")
		return strings.Join(strings.Fields(strings.ReplaceAll(s, "fixed table ", "table ")), " ")
	}
	for _, tc := range []struct{ unit, source string }{
		{"reportmessages", "tables/messages/Messages.schema"},
		{"reportarms", "test/tables/A2.schema"},
		{"reportscalars", "test/tables/Scalars2.schema"},
		{"reportkeyed1", "test/tables/K1.schema"},
		{"reportkeyed2", "test/tables/K2.schema"},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			path := filepath.Join(root, "test/conformance/report-schemas", tc.unit, filepath.Base(tc.source))
			original, err := os.ReadFile(filepath.Join(root, tc.source))
			if err != nil {
				t.Fatal(err)
			}
			reader, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if normalize(original) != normalize(reader) {
				t.Fatal("report reader changed field declarations")
			}
			u, err := compiler.New().Load([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			for name, def := range u.Tables {
				if def.FixedDeclared {
					t.Errorf("%s is still declared fixed", name)
				}
			}
		})
	}
}

// A legal historical body must reach and preserve its fields. Invalid bodies
// refuse at those fields; a positive case beside each refusal prevents an
// accidental fixed-root form rejection from satisfying the entire battery.
func TestReportReadersExerciseHistoricalBodies(t *testing.T) {
	root := conformanceRoot(t)
	for _, tc := range []struct{ unit, file, table, goodWire, text, badWire string }{
		{"reportmessages", "Messages.schema", "ToolMessage", "batch_unknown_arm", `{"history":[{}]}`, "batch_elem_kind"},
		{"reportarms", "A2.schema", "Root", "a1_arm_moved_width", `{"id":11,"value":{"a":7},"tail":22}`, "a1_arm_as_string"},
		{"reportscalars", "Scalars2.schema", "SimState", "field_scalar_widen", `{"angle":9}`, "scalars_full"},
		{"reportkeyed1", "K1.schema", "Root", "", `{"grade":"Gold","raw":3}`, "k2_as_k1"},
		{"reportkeyed2", "K2.schema", "Root", "", `{"grade":3,"raw":"Gold"}`, "k1_as_k2"},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			u, err := compiler.New().Load([]string{filepath.Join(root, "test/conformance/report-schemas", tc.unit, tc.file)})
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(u)
			want := model.New(model.Lookup(tc.table))
			if !model.Read(want, []byte(tc.text), &tabletext.Report{}) {
				t.Fatal("valid fixture JSON refused")
			}
			wantWire, err := tablewire.Encode(model, want)
			if err != nil {
				t.Fatal(err)
			}
			wire := wantWire
			if tc.goodWire != "" {
				wire, err = os.ReadFile(filepath.Join(root, "testdata/wire/tables", tc.goodWire+".bin"))
				if err != nil {
					t.Fatal(err)
				}
			}
			got := model.New(model.Lookup(tc.table))
			if ok, err := tablewire.Decode(model, got, wire, &tabletext.Report{}); !ok || err != nil {
				t.Fatalf("valid body refused: %v", err)
			}
			gotWire, err := tablewire.Encode(model, got)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotWire, wantWire) {
				t.Fatalf("accepted body lost values: got %x, want %x", gotWire, wantWire)
			}
			bad, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables", tc.badWire+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := tablewire.Decode(model, got, bad, &tabletext.Report{}); ok || (err != nil && !tablewire.ReadRefusal(err)) {
				t.Fatalf("invalid body: accepted=%t err=%v", ok, err)
			}
		})
	}
}
