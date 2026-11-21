package lockfile_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/lockfile"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestLockWriterSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	const src = `package lockdemo
 enum Key { Red, Blue }
 flags Mask { First, Second }
 type Child {
  big int128 | min = 0, max = 1208925819614629174706176
  ratio float32 | min = -1, max = 1, resolution = 0.1
  scaled fixed(64,64) | min = -2, max = 3
  mask Mask
 }
 union Pick { child Child
 ping }
 fixed table Row {
  maybe ?Child
  many [2..4]Child
  fixed_items [2]Child
  keyed [Key]Child
  pick Pick
 }
 `
	dir, paths := fixture(t, src)
	u := load(t, paths)
	if _, _, err := lockfile.Update(u, paths); err != nil {
		t.Fatal(err)
	}
	lk := readLock(t, filepath.Join(dir, lockfile.FileName))
	got := lk.Table("Row").Lineage[0]
	if got.Writer == nil {
		t.Fatal("new entries must capture a complete writer")
	}
	want := u.Tables["Row"]
	if !bytes.Equal(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(got.Writer)), got.Layout) || !bytes.Equal(ir.TableFixedDefinitionsDigest(got.Writer), got.Digest) {
		t.Fatal("snapshot does not reconstruct layout and digest")
	}
	if got.Wire != ir.TableFixedLayoutHash(got.Layout, want) {
		t.Fatal("capturing metadata moved the wire hash")
	}
	child := got.Writer.Fields[0].Type.Ref.(*ir.Struct)
	if child.Fields[0].IntMax.String() != "1208925819614629174706176" || got.Writer.Fields[1].ArrayMin != 2 {
		t.Fatal("wide integer/count bounds did not survive")
	}
	if child.Fields[1].FMin != -1 || child.Fields[1].FMax != 1 || child.Fields[1].Round != "nearest" || child.Fields[1].Steps != want.Fields[0].Type.Ref.(*ir.Struct).Fields[1].Steps || child.Fields[2].Type.FracBits != 64 {
		t.Fatal("float/fixed constraints did not survive")
	}
	if got.Writer.Fields[3].KeyEnumRef.Max != 2 || got.Writer.Fields[4].Type.Ref.(*ir.Union).Variants[0].F.Type.Ref.(*ir.Struct) != child {
		t.Fatal("closed named references were not restored")
	}
	data, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != lk.Text() {
		t.Fatal("writer snapshot rendering is not canonical")
	}
	if _, rewrote, err := lockfile.Update(u, paths); err != nil || rewrote {
		t.Fatalf("update not idempotent: %v %v", rewrote, err)
	}
}

func TestLockWriterLegacySalvageAndTrustedImport(t *testing.T) {
	t.Parallel()
	old := strings.Replace(rowTable("a int32 | min = 0, max = 10\nb int32"), "package lockrows", "package lockdemo", 1)
	next := strings.Replace(rowTable("a int32\nb int32"), "package lockrows", "package lockdemo", 1)
	dir, paths := fixture(t, old)
	writer := load(t, paths)
	// This is an actual rendering8 lock captured before the identity correction,
	// not a lock recomputed by the new compiler with a relabelled header.
	data, err := os.ReadFile("testdata/writer-v1/attribution/schema.lock")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := lockfile.Parse("historical", data)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Version = 7
	legacy.Table("Row").Lineage[0].Writer = nil
	path := filepath.Join(dir, lockfile.FileName)
	if err := os.WriteFile(path, []byte(legacy.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	prior := legacy.Table("Row").Lineage[0]
	if err := os.WriteFile(paths[0], []byte(next), 0600); err != nil {
		t.Fatal(err)
	}
	current := load(t, paths)
	if _, _, err := lockfile.Update(current, paths); err != nil {
		t.Fatal(err)
	}
	migrated := readLock(t, path)
	entries := migrated.Table("Row").Lineage
	if len(entries) != 2 || entries[0].Writer != nil || entries[1].Writer == nil || entries[0].Wire != prior.Wire || entries[0].Record != prior.Record || !bytes.Equal(entries[0].Layout, prior.Layout) || !bytes.Equal(entries[0].Digest, prior.Digest) {
		t.Fatal("salvage guessed historical constraints, rewrote an old identity or lost the new writer")
	}
	if errs := lockfile.Check(current, paths); len(errs) != 0 {
		t.Fatal(errs)
	}
	target := fmt.Sprintf("Row@0x%016x", prior.Wire)
	if _, rewrote, err := lockfile.ImportWriter(current, paths, target, writer); err != nil || !rewrote {
		t.Fatalf("legacy scheme import: %v %v", rewrote, err)
	}
	after := readLock(t, path)
	known := after.Table("Row").Lineage
	if known[0].Writer == nil || known[0].Wire != prior.Wire || lockfile.Floor(after, "Row") != lockfile.Floor(legacy, "Row") {
		t.Fatal("import changed entry identity or floor")
	}
	if _, rewrote, err := lockfile.ImportWriter(current, paths, target, writer); err != nil || rewrote {
		t.Fatalf("import not idempotent: %v %v", rewrote, err)
	}
	// Legacy anonymous R rows cannot authenticate which field owned the bound.
	// Explicit source provenance is required, and conflicting captured facts refuse.
	_, otherPaths := fixture(t, strings.Replace(rowTable("a int32\nb int32 | min = 0, max = 10"), "package lockrows", "package lockdemo", 1))
	other := load(t, otherPaths)
	before, _ := os.ReadFile(path)
	if _, _, err := lockfile.ImportWriter(current, paths, target, other); err == nil || !strings.Contains(err.Error(), "distinct captured writer constraints") {
		t.Fatalf("ambiguous import: %v", err)
	}
	now, _ := os.ReadFile(path)
	if !bytes.Equal(before, now) {
		t.Fatal("refused import changed lock")
	}
	if _, _, err := lockfile.ImportWriter(current, paths, "Row@0x0000000000000001", writer); err == nil {
		t.Fatal("unknown hash accepted")
	}
	if _, _, err := lockfile.ImportWriter(current, paths, fmt.Sprintf("Row@0x%016x", entries[1].Wire), writer); err == nil {
		t.Fatal("mismatched source accepted")
	}
}

func TestLockWriterMinimumRemainsInvariant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, old, next, want string }{
		{"count-minimum", "a [1..3]int16", "a [0..3]int16", "default changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, paths := fixture(t, rowTable(tc.old))
			if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, lockfile.FileName)
			before, _ := os.ReadFile(path)
			if err := os.WriteFile(paths[0], []byte(rowTable(tc.next)), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := lockfile.Update(load(t, paths), paths); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s: %v", tc.want, err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("refused transition changed lock")
			}
		})
	}
}

func TestLockWriterMetadataIsBound(t *testing.T) {
	t.Parallel()
	dir, paths := fixture(t, rowTable("a int32 | min = 0, max = 10"))
	u := load(t, paths)
	if _, _, err := lockfile.Update(u, paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	lk := readLock(t, path)
	// The default does not enter the legacy wire digest. Its snapshot still
	// enters the lineage roll-up, so editing it alone is caught by Check.
	lk.Table("Row").Lineage[0].Writer.Fields[0].HasDefault = true
	lk.Table("Row").Lineage[0].Writer.Fields[0].DefInt = u.Tables["Row"].Fields[0].IntMax
	changed := lk.Text()
	original, _ := os.ReadFile(path)
	// Keep the old roll-up while replacing only the writer token.
	oldLines, newLines := strings.Split(string(original), "\n"), strings.Split(changed, "\n")
	for i := range oldLines {
		if strings.Contains(oldLines[i], "lineage wire=") {
			oldLines[i] = newLines[i]
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(oldLines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(u, paths); len(errs) == 0 {
		t.Fatal("snapshot-only edit escaped lineage binding")
	}
}

func TestLockWriterSnapshotWholeOptionalCollections(t *testing.T) {
	t.Parallel()
	const src = `package lockdemo
 fixed table Row {
 a ?[2]int32
 b ?[1..3]int16
 }
 `
	dir, paths := fixture(t, src)
	u := load(t, paths)
	if _, _, err := lockfile.Update(u, paths); err != nil {
		t.Fatal(err)
	}
	got := readLock(t, filepath.Join(dir, lockfile.FileName)).Table("Row").Lineage[0].Writer
	if got == nil {
		t.Fatal("lawful optional collections lost their snapshot")
	}
	for i, f := range got.Fields {
		if !f.Type.Optional {
			t.Fatalf("field %d lost whole optional framing", i)
		}
	}
	if got.Fields[0].Array != ir.ArrayFixed || got.Fields[0].ArrayBound != 2 || got.Fields[1].Array != ir.ArrayCounted || got.Fields[1].ArrayMin != 1 || got.Fields[1].ArrayBound != 3 {
		t.Fatal("optional collection shape/bounds changed")
	}
}
