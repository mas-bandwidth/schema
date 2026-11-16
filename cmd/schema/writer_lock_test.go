package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
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
	lk.Version = 7
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
	for _, args := range [][]string{{"--writer", target}, {"--from", old}, {"--writer", target, "--from", old, "--print"}, {"--writer", target, "--from", old, "--retire", target, "--reason", "old"}} {
		argv := append([]string{"lock"}, args...)
		argv = append(argv, current)
		out, err := exec.Command(bin, argv...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "--writer and --from") {
			t.Fatalf("invalid combination accepted: %v: %v %s", args, err, out)
		}
	}
}
