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

// TestFixedLockRefusesARecordPastItsSizeField: a lock records every fixed
// table's lineage, including one past the record bound, and a layout entry
// carries its size in 32 bits. A record past 2^32-1 bytes is refused by name
// when the lock is written, rather than locked with its size modulo 2^32; a
// record past the record bound but under 2^32 is locked as before.
func TestFixedLockRefusesARecordPastItsSizeField(t *testing.T) {
	t.Parallel()
	wide := fixedBoundUnit(t, readWideSchema(t))
	errs := ir.TableFixedLockRefusals(wide)
	var refusal *ir.TableFixedLayoutRefusal
	if len(errs) != 1 || !errors.As(errs[0], &refusal) || refusal.Table != "Huge" || !refusal.Lock {
		t.Fatalf("Wide.schema lock refusals: %v, want layout_record_too_large naming Huge alone (Part is 2^31 bytes)", errs)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(readWideSchema(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(wide, []string{path}); !errors.As(err, &refusal) {
		t.Fatalf("schema lock on Wide.schema: %v, want layout_record_too_large", err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockfile.FileName)); !os.IsNotExist(err) {
		t.Fatalf("a refused lock was written: %v", err)
	}
	// past the record bound, under 2^32: no fixed form, and the lock holds it
	big := fixedBoundUnit(t, "package bound\nfixed table Frame { a [1048576]int64 }\n")
	if errs := ir.TableFixedLockRefusals(big); len(errs) != 0 {
		t.Fatalf("an 8 MiB record is lockable: %v", errs)
	}
}
