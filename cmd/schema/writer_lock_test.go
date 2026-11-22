package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/lockfile"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestLockWriterHistoricalImportCLI(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	current, old := t.TempDir(), t.TempDir()
	before := "package probe\nfixed table Row { n int32 | min = 0, max = 10 }\n"
	after := "package probe\nfixed table Row { n int32 | min = 0, max = 20 }\n"
	for _, dir := range []string{current, old} {
		if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(before), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run(t, bin, "lock", current)
	if err := os.WriteFile(filepath.Join(current, "Probe.schema"), []byte(after), 0600); err != nil {
		t.Fatal(err)
	}
	run(t, bin, "lock", current)
	path := filepath.Join(current, compiler.SchemaLockFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lockfile.Parse(path, data)
	if err != nil {
		t.Fatal(err)
	}
	// Missing metadata also remains explicit for a prospective entry.
	lk.Version = lockfile.Version
	for i := range lk.Tables {
		for j := range lk.Tables[i].Lineage {
			lk.Tables[i].Lineage[j].Writer = nil
		}
	}
	target := fmt.Sprintf("Row@0x%016x", lk.Table("Row").Lineage[0].Wire)
	if err := os.WriteFile(path, []byte(lk.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	out := run(t, bin, "lock", "--writer", target, "--from", old, "--verbose", current)
	if !strings.Contains(out, "captured writer metadata") {
		t.Fatal(out)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := lockfile.Parse(path, data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != lockfile.Version || got.Table("Row").Lineage[0].Writer == nil || got.Table("Row").Lineage[1].Writer != nil {
		t.Fatal("CLI did not enrich exactly the selected legacy entry")
	}
	out = run(t, bin, "lock", "--writer", target, "--from", old, "--verbose", current)
	if !strings.Contains(out, "already carries") {
		t.Fatal(out)
	}
	// Only the named opt-in can attach constrained source to a layout-only key.
	legacy := got.Table("Row").Lineage[0]
	legacy.Digest, legacy.Writer = nil, nil
	legacy.Wire = ir.TableFixedLayoutHashRecorded(legacy.Layout, nil)
	got.Table("Row").Lineage = append([]lockfile.LineageEntry{legacy}, got.Table("Row").Lineage...)
	if err := os.WriteFile(path, []byte(got.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	layoutTarget := fmt.Sprintf("Row@0x%016x", legacy.Wire)
	if output, err := exec.Command(bin, "lock", "--writer", layoutTarget, "--from", old, current).CombinedOutput(); err == nil || !strings.Contains(string(output), "does not match") {
		t.Fatalf("strict CLI accepted missing digest facts: %v %s", err, output)
	}
	run(t, bin, "lock", "--writer", layoutTarget, "--from", old, "--legacy-layout-only", current)
	enriched, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := lockfile.Parse(path, enriched)
	if err != nil {
		t.Fatal(err)
	}
	if !trusted.Table("Row").Lineage[0].LegacyLayoutOnly || trusted.Table("Row").Lineage[0].Writer.Fields[0].IntMax.Int64() != 10 {
		t.Fatal("CLI lost trusted historical writer bounds")
	}
	for _, args := range [][]string{{"--legacy-layout-only"}, {"--writer", target}, {"--from", old}, {"--writer", target, "--from", old, "--print"}, {"--writer", target, "--from", old, "--retire", target, "--reason", "old"}} {
		argv := append([]string{"lock"}, args...)
		argv = append(argv, current)
		out, err := exec.Command(bin, argv...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "--writer and --from") {
			t.Fatalf("invalid combination accepted: %v: %v %s", args, err, out)
		}
	}
}
