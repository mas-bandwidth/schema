// Shared S4 gate: one subtest per clause of the title.  Each clause drives the
// production entry point (Compiler.Generate) and inspects the emitted source.
// TestFixedLineageIsShippedByEveryTargetThatTakesIt already asserts clause 1;
// the other three are asserted here.
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestSharedS4Gate(t *testing.T) {
	t.Run("COMPILE_reads_lockfile_Lineage", func(t *testing.T) {
		// TestFixedLineageIsShippedByEveryTargetThatTakesIt already asserts this
		// for every target.  This subtest re-asserts the observable outcome
		// through the production entry point for a target (go) whose backend
		// reads fixedlineage.Entries(), not the lock directly.
		dir := t.TempDir()
		schema := "package s4lineage\n\nfixed table Row\n{\n    a int32\n}\n"
		path := filepath.Join(dir, "Lineage.schema")
		if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
			t.Fatal(err)
		}
		paths, err := GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		c := New()
		first, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := UpdateSchemaLock(first, paths); err != nil {
			t.Fatalf("first lock: %v", err)
		}
		firstHash := tableHash(t, first, "Row")

		if err := os.WriteFile(path, []byte("package s4lineage\n\nfixed table Row\n{\n    a int32\n    b int32\n}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		widened, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := UpdateSchemaLock(widened, paths); err != nil {
			t.Fatalf("second lock: %v", err)
		}
		secondHash := tableHash(t, widened, "Row")
		if firstHash == secondHash {
			t.Fatal("the widening must change the layout hash")
		}

		u, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		files, err := c.Generate(u, "go", nil)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		body := joined(files)
		if !strings.Contains(body, fmt.Sprintf("0x%016x", firstHash)) {
			t.Fatalf("COMPILE reads lockfile.Lineage: the older layout hash 0x%016x is not in the emitted source — the lock's lineage did not reach the output", firstHash)
		}
		if !strings.Contains(body, fmt.Sprintf("0x%016x", secondHash)) {
			t.Fatalf("COMPILE reads lockfile.Lineage: the current layout hash 0x%016x is not in the emitted source", secondHash)
		}
	})

	t.Run("COMPILE_reads_lockfile_Floor", func(t *testing.T) {
		// Retire the OLDER layout so lockfile.Floor returns 1.  The emitted
		// RowFixedFloor must be 1, proving the floor is read from the lock.
		dir := t.TempDir()
		schema := "package s4floor\n\nfixed table Row\n{\n    a int32\n}\n"
		path := filepath.Join(dir, "Lineage.schema")
		if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
			t.Fatal(err)
		}
		paths, err := GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		c := New()
		first, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := UpdateSchemaLock(first, paths); err != nil {
			t.Fatalf("first lock: %v", err)
		}
		firstHash := tableHash(t, first, "Row")

		if err := os.WriteFile(path, []byte("package s4floor\n\nfixed table Row\n{\n    a int32\n    b int32\n}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		widened, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := UpdateSchemaLock(widened, paths); err != nil {
			t.Fatalf("second lock: %v", err)
		}

		u, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := RetireSchemaLock(u, paths, fmt.Sprintf("Row@0x%016x", firstHash), "test retirement for S4 gate"); err != nil {
			t.Fatalf("retire: %v", err)
		}

		lock, ok, err := lockfile.Open(paths)
		if err != nil || !ok {
			t.Fatalf("lockfile.Open: ok=%v err=%v", ok, err)
		}
		if f := lockfile.Floor(lock, "Row"); f != 1 {
			t.Fatalf("the lock's floor is %d, want 1 after retiring the first entry", f)
		}

		u2, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		files, err := c.Generate(u2, "cpp", nil)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		body := joined(files)
		m := regexp.MustCompile(`RowFixedFloor\s*=\s*(\d+)`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("COMPILE reads lockfile.Floor: no RowFixedFloor found in the emitted source")
		}
		if m[1] != "1" {
			t.Fatalf("COMPILE reads lockfile.Floor: the emitted floor is %s, want 1 — the lock's floor is derived from its retired marks, not from a hard-coded value", m[1])
		}
	})

	t.Run("COMPILE_not_filename_convention", func(t *testing.T) {
		// Create a VOLD_ sibling the filename convention would pick up if the
		// lock were absent.  The VNEW_ unit is LOCKED, so the convention must
		// never fire: the VOLD_ layout hash must not appear in the output.
		dir := t.TempDir()
		voldContent := "package s4conv\n\nfixed table Peer\n{\n    a int32\n}\n"
		voldPath := filepath.Join(dir, "VOLD_Peer.schema")
		if err := os.WriteFile(voldPath, []byte(voldContent), 0o600); err != nil {
			t.Fatal(err)
		}
		voldHash := hashFromSchemaFile(t, voldPath, "Peer")

		vnewContent := "package s4conv\n\nfixed table Peer\n{\n    a int32\n    b int32\n}\n"
		vnewPath := filepath.Join(dir, "VNEW_Peer.schema")
		if err := os.WriteFile(vnewPath, []byte(vnewContent), 0o600); err != nil {
			t.Fatal(err)
		}

		paths, err := GatherPaths([]string{vnewPath})
		if err != nil {
			t.Fatal(err)
		}
		c := New()
		u, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := UpdateSchemaLock(u, paths); err != nil {
			t.Fatalf("lock: %v", err)
		}

		u2, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		files, err := c.Generate(u2, "cpp", nil)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		body := joined(files)
		if strings.Contains(body, fmt.Sprintf("0x%016x", voldHash)) {
			t.Fatalf("COMPILE does not use the filename convention: the VOLD_ sibling's hash 0x%016x appears in the emitted source — the filename convention fired when it should not have. The lock's lineage is the only source.", voldHash)
		}
	})

	t.Run("COMPILE_not_hardcoded_floor", func(t *testing.T) {
		// fixtureFloor maps "vnew_floor" → 1.  When a lock exists, the floor
		// must come from lockfile.Floor (0, because nothing is retired), not
		// from the hard-coded map.
		dir := t.TempDir()
		schema := "package vnew_floor\n\nfixed table Row\n{\n    a int32\n}\n"
		path := filepath.Join(dir, "Lineage.schema")
		if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
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
		if _, _, err := UpdateSchemaLock(u, paths); err != nil {
			t.Fatalf("lock: %v", err)
		}

		u2, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		files, err := c.Generate(u2, "cpp", nil)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		body := joined(files)
		m := regexp.MustCompile(`RowFixedFloor\s*=\s*(\d+)`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("COMPILE does not use a hard-coded floor: no RowFixedFloor found in the emitted source")
		}
		if m[1] != "0" {
			t.Fatalf("COMPILE does not use a hard-coded floor: the emitted floor is %s, want 0 — the hard-coded fixtureFloor[vnew_floor] is 1, and a lock with no retired entries has floor 0. When a lockfile exists the floor comes from lockfile.Floor, not from fixtureFloor.", m[1])
		}
	})
}

func tableHash(t *testing.T, u *ir.Unit, name string) uint64 {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("table %s not found", name)
	}
	return ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
}

func hashFromSchemaFile(t *testing.T, schemaPath, tableName string) uint64 {
	t.Helper()
	paths, err := GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables[tableName]
	if st == nil {
		t.Fatalf("table %s not found in %s", tableName, schemaPath)
	}
	return ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
}

func joined(files map[string][]byte) string {
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}
