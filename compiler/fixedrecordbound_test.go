package compiler_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedBoundUnit loads one schema source as a unit.
func fixedBoundUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Bound.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestFixedLayoutRefusedPastTheRecordBound: the shared layout encoder refuses
// a fixed table whose record is past the 65536-byte fixed-form record bound
// (docs/SPEC-TABLES.md §3.4) by name, layout_record_too_large, rather than
// write its size modulo 2^32. A record of exactly 65536 bytes is laid out; one
// byte past it is refused, and so is Wide.schema's 6 GiB record, whose size a
// u32 would carry as 0x80000000.
func TestFixedLayoutRefusedPastTheRecordBound(t *testing.T) {
	t.Parallel()
	u := fixedBoundUnit(t, "package bound\nfixed table At { a [8192]int64 }\nfixed table Over { a [8192]int64\n b uint8 }\n")
	at := u.Tables["At"]
	if ir.TableFixedTypeBytes(at) != ir.TableFixedRecordMaxBytes {
		t.Fatalf("At is %d bytes; the boundary case needs exactly %d", ir.TableFixedTypeBytes(at), ir.TableFixedRecordMaxBytes)
	}
	if layout, err := ir.TableFixedLayoutOf(at); err != nil || len(layout) == 0 {
		t.Fatalf("a record at the bound is laid out: %v", err)
	}
	over := u.Tables["Over"]
	var refusal *ir.TableFixedLayoutRefusal
	if _, err := ir.TableFixedLayoutOf(over); !errors.As(err, &refusal) || refusal.Table != "Over" || refusal.Size != ir.TableFixedTypeBytes(over) {
		t.Fatalf("a record one byte past the bound: %v, want layout_record_too_large naming Over", err)
	}
	wide := fixedBoundUnit(t, readWideSchema(t))
	if _, err := ir.TableFixedLayoutOf(wide.Tables["Huge"]); !errors.As(err, &refusal) || refusal.Size <= 1<<32 {
		t.Fatalf("Huge: %v, want layout_record_too_large with its full size", err)
	}
	if err := (&ir.TableFixedLayoutRefusal{Table: "Over", Size: 65537}).Error(); !bytes.HasPrefix([]byte(err), []byte(ir.TableFixedLayoutRecordTooLarge+": ")) {
		t.Fatalf("the refusal is not named: %q", err)
	}
	// the encoder itself refuses, so no caller can write a truncated size
	panicked := func() (err any) {
		defer func() { err = recover() }()
		ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(over))
		return nil
	}()
	if r, ok := panicked.(*ir.TableFixedLayoutRefusal); !ok || r.Table != "Over" {
		t.Fatalf("TableFixedLayoutBytes wrote a layout past the bound: %v", panicked)
	}
}

func readWideSchema(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../testdata/wire/tables/storage-cap/wide/Wide.schema")
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// TestEveryTargetDeclinesTheFixedFormPastTheRecordBound: every target
// generates the storage cap units, whose fixed tables (2^27 bytes; 2 and 6
// GiB) are past the fixed-form record bound, without asking the encoder for
// their layout: each keeps form 1 (docs/SPEC-TABLES.md §3.4). A target that
// asked would reach the encoder's layout_record_too_large refusal.
func TestEveryTargetDeclinesTheFixedFormPastTheRecordBound(t *testing.T) {
	t.Parallel()
	cap, err := os.ReadFile("../testdata/wire/tables/storage-cap/Cap.schema")
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]string{"Cap": string(cap), "Wide": readWideSchema(t)}
	c := compiler.New()
	for name, src := range units {
		u := fixedBoundUnit(t, src)
		for _, target := range c.Targets() {
			t.Run(name+"/"+target, func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s asked for a layout past the record bound: %v", target, r)
					}
				}()
				if _, err := c.Generate(u, target, compiler.Options{}); err != nil {
					var refusal *ir.TableFixedLayoutRefusal
					if errors.As(err, &refusal) {
						t.Fatalf("%s: %v", target, err)
					}
				}
			})
		}
	}
}

// lockedLineage writes a lock for one unit directory and answers, per fixed
// table, the lineage entries the written file holds.
func lockedLineage(t *testing.T, src string) map[string][]lockfile.LineageEntry {
	t.Helper()
	u := fixedBoundUnit(t, src)
	dir := t.TempDir()
	path := filepath.Join(dir, "Bound.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, _, err := lockfile.Update(u, []string{path})
	if err != nil {
		t.Fatalf("schema lock: %v", err)
	}
	data, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Parse(lock, data)
	if err != nil {
		t.Fatalf("the written lock does not parse: %v", err)
	}
	if errs := lockfile.Check(u, []string{path}); len(errs) != 0 {
		t.Fatalf("the written lock does not check: %v", errs)
	}
	out := map[string][]lockfile.LineageEntry{}
	for _, tb := range locked.Tables {
		if tb.Decl == lockfile.DeclFixedTable {
			out[tb.Name] = tb.Lineage
		}
	}
	return out
}

// TestFixedLockRecordsNoLayoutPastTheRecordBound: a fixed table past the
// fixed-form record bound has no fixed form (docs/SPEC-TABLES.md §3.4), so its
// lock records no layout for it: a 6 GiB record and an 8 MiB record each lock,
// with no lineage entry, in every language, and no size is stored modulo 2^32.
// A record of exactly 65536 bytes keeps its layout entry.
func TestFixedLockRecordsNoLayoutPastTheRecordBound(t *testing.T) {
	t.Parallel()
	wide := lockedLineage(t, readWideSchema(t))
	for _, name := range []string{"Huge", "Part"} {
		entries, ok := wide[name]
		if !ok {
			t.Fatalf("Wide.schema: the lock does not hold fixed table %s", name)
		}
		if len(entries) != 0 {
			t.Fatalf("Wide.schema: %s is past the record bound and the lock records %d layout entries for it", name, len(entries))
		}
	}
	if entries := lockedLineage(t, "package bound\nfixed table Frame { a [1048576]int64 }\n")["Frame"]; len(entries) != 0 {
		t.Fatalf("an 8 MiB record: the lock records %d layout entries", len(entries))
	}
	at := lockedLineage(t, "package bound\nfixed table At { a [8192]int64 }\n")["At"]
	if len(at) != 1 || at[0].Record != ir.TableFixedRecordMaxBytes || len(at[0].Layout) == 0 {
		t.Fatalf("a record of exactly %d bytes keeps its one layout entry: %+v", ir.TableFixedRecordMaxBytes, at)
	}
}

// TestFixedLockKeepsHistoryPastTheRecordBound: a committed lock that already
// holds lineage for a table past the record bound (tables/block's 7.5 MB
// RenderFrame) still checks, and writing the lock again keeps every entry it
// holds: they are history, and the lock is append-only.
func TestFixedLockKeepsHistoryPastTheRecordBound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "../tables/block"
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if filepath.Ext(e.Name()) == ".schema" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	before, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.Parse(lockfile.FileName, before)
	if err != nil {
		t.Fatal(err)
	}
	if frame := held.Table("RenderFrame"); frame == nil || len(frame.Lineage) == 0 {
		t.Fatal("tables/block's lock no longer holds RenderFrame's historical lineage; this test needs it")
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(u, paths); len(errs) != 0 {
		t.Fatalf("the committed lock does not check: %v", errs)
	}
	if _, rewrote, err := lockfile.Update(u, paths); err != nil || rewrote {
		t.Fatalf("schema lock over the committed lock: rewrote %v, err %v", rewrote, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("schema lock changed the committed lock: its historical entries must stand")
	}
}
