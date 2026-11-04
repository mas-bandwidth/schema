package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Repeating an explicit migration must preserve the original old-to-new
// evidence, including the variable WideExtent witness which remains form 1.
func TestMigrationReplayPreservesEvidence(t *testing.T) {
	for _, kind := range []string{"fixed", "verdicts"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name string, data []byte) string {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				return path
			}
			schema := write("Probe.schema", []byte("package probe\nfixed table Fixed { n int32 }\ntable WideExtent { n int32 }\n"))
			m := &Manifest{Units: []Unit{{Key: "probe", Paths: []string{schema}}}}
			unit, err := newUnits(m).get("probe")
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			value := model.New(model.Lookup("WideExtent"))
			value.Fields[0].Cell.I = 7
			canonical, err := model.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			oldWire := write("historical.bin", wire)
			jsonDir := filepath.Join(dir, "json")
			write("json/fixed.json", canonical)
			write("json/wide_blob.json", canonical)
			write("hostile/WideExtent.json", canonical)
			body := fmt.Sprintf("unit probe %s\n", schema)
			migrate := migrateFixed
			if kind == "fixed" {
				body += fmt.Sprintf("instance fixed probe Fixed %s\ninstance wide_blob probe WideExtent %s\n", oldWire, oldWire)
			} else {
				body += fmt.Sprintf("json-hostile accepted probe WideExtent %s 0,0,0,0,0,false,read\n", filepath.Join(dir, "hostile"))
				migrate = migrateVerdicts
			}
			manifest := write("MANIFEST.txt", []byte(body))
			for pass := 0; pass < 2; pass++ {
				m, err = ReadManifest(manifest, jsonDir)
				if err != nil {
					t.Fatal(err)
				}
				before, err := treeFingerprint(dir)
				if err != nil {
					t.Fatal(err)
				}
				if err := migrate(m, manifest, jsonDir); err != nil {
					t.Fatal(err)
				}
				after, err := treeFingerprint(dir)
				if err != nil {
					t.Fatal(err)
				}
				moved := treeMoved(before, after)
				if pass == 0 && !strings.Contains(strings.Join(moved, "\n"), "MIGRATION.md") {
					t.Fatal("first migration did not record its evidence")
				}
				if pass == 1 && len(moved) != 0 {
					t.Fatalf("replay changed migration evidence: %v", moved)
				}
			}
		})
	}
}
