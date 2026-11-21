package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A retirement takes effect for the reads of the unit already loaded: once
// RetireSchemaLock marks an older layout, ReadReport on the same *ir.Unit
// refuses a file of that layout as layout_unsupported, as a fresh Load does
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.4; docs/SPEC-TABLES.md §3.4).
func TestRetireTakesEffectForTheLoadedUnit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Config.schema")
	const first = "package retireread\n\nfixed table Config\n{\n    version int32\n}\n"
	const widened = "package retireread\n\nfixed table Config\n{\n    version int32\n    flags   uint32\n}\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Config"]
	olderHash := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "Config.json"), []byte(`{"version": 3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old, _, _, err := c.Pack(u, "Config", tree)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(u, paths); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if err := os.WriteFile(path, []byte(widened), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(w, paths); err != nil {
		t.Fatalf("second lock: %v", err)
	}
	report, err := c.ReadReport(w, "Config", old)
	if err != nil || report.Refused {
		t.Fatalf("the locked older layout must read before retirement: report=%+v err=%v", report, err)
	}
	if _, _, err := RetireSchemaLock(w, paths, fmt.Sprintf("Config@0x%016x", olderHash), "the v1 writer is gone"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	for name, unit := range map[string]*ir.Unit{"the loaded unit": w, "a fresh load": mustLoad(t, c, paths)} {
		report, err := c.ReadReport(unit, "Config", old)
		got := report.Reason
		if err != nil {
			got += " " + err.Error()
		}
		if !report.Refused && err == nil || !strings.Contains(got, "layout_unsupported") {
			t.Fatalf("%s: a retired layout must refuse as layout_unsupported: report=%+v err=%v", name, report, err)
		}
	}
}

func mustLoad(t *testing.T, c *Compiler, paths []string) *ir.Unit {
	t.Helper()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
