package lockfile_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The saved schema.lock was produced by pre-v2 metadata compiler948ec717c6
// on Linux. Its bytes and key are historical fixtures, never regeneration targets.
func TestLockWriterV2PreservesV1HistoryAndWideWidening(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("testdata/writer-v1/wide/Writer.schema")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile("testdata/writer-v1/wide/schema.lock")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := lockfile.Parse("saved-v8", saved)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Version != 8 || len(legacy.Table("Row").Lineage) != 1 {
		t.Fatal("historical fixture moved")
	}
	old := legacy.Table("Row").Lineage[0]
	if version, err := ir.TableFixedDefinitionsDigestVersion(old.Digest); err != nil || version != 1 {
		t.Fatalf("historical scheme %d: %v", version, err)
	}
	if legacy.Text() != string(saved) {
		t.Fatal("parsing changed real-v1 fixture bytes")
	}
	dir, paths := fixture(t, string(source))
	path := filepath.Join(dir, lockfile.FileName)
	// A genuine old retirement remains at exactly its original lineage index.
	legacy.Table("Row").Lineage[0].Retired = true
	legacy.Table("Row").Lineage[0].Reason = "old deployment retired"
	if err := os.WriteFile(path, []byte(legacy.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	u := load(t, paths)
	if _, _, err := lockfile.Update(u, paths); err != nil {
		t.Fatal(err)
	}
	got := readLock(t, path)
	es := got.Table("Row").Lineage
	if got.Version != 9 || len(es) != 2 || es[0].Wire != old.Wire || !bytes.Equal(es[0].Layout, old.Layout) || !bytes.Equal(es[0].Digest, old.Digest) || es[0].Record != old.Record || !es[0].Retired || es[0].Reason != "old deployment retired" || es[0].Writer == nil || es[1].Writer == nil || lockfile.Floor(got, "Row") != 1 {
		t.Fatal("v9 migration changed historical facts or failed to append new current identity")
	}
	if es[0].Wire == es[1].Wire || !bytes.Equal(es[0].Layout, es[1].Layout) {
		t.Fatal("identity correction must change key, not layout")
	}
	target := fmt.Sprintf("Row@0x%016x", old.Wire)
	if _, rewrote, err := lockfile.ImportWriter(u, paths, target, u); err != nil || rewrote {
		t.Fatalf("idempotent recorded-v1 import: %v %v", rewrote, err)
	}
	wider := strings.Replace(string(source), "1208925819614629174706176", "2417851639229258349412352", 1)
	if err := os.WriteFile(paths[0], []byte(wider), 0600); err != nil {
		t.Fatal(err)
	}
	current := load(t, paths)
	if _, _, err := lockfile.Update(current, paths); err != nil {
		t.Fatalf("lawful full-width widening: %v", err)
	}
	after := readLock(t, path)
	entries := after.Table("Row").Lineage
	if len(entries) != 3 || entries[0].Wire != old.Wire || entries[1].Wire != es[1].Wire || entries[2].Wire == es[1].Wire || entries[2].Writer.Fields[0].IntMax.String() != "2417851639229258349412352" || lockfile.Floor(after, "Row") != 1 {
		t.Fatal("wide widening lost identities, writer bounds or floor")
	}
	if errs := lockfile.Check(current, paths); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestLockWriterDigestSchemeRefusals(t *testing.T) {
	t.Parallel()
	_, paths := fixture(t, rowTable("a int32 | min = 0, max = 10"))
	lk := lockfile.Render(load(t, paths))
	lk.Version = 8
	if _, err := lockfile.Parse("wrong-rendering", []byte(lk.Text())); err == nil || !strings.Contains(err.Error(), "rendering 9") {
		t.Fatalf("v2 under v8: %v", err)
	}
	// An empty legacy digest is a real claim, not permission to erase source facts.
	entry := lk.Table("Row").Lineage[0]
	source := entry.Writer
	entry.Digest = nil
	entry.Wire = ir.TableFixedLayoutHashRecorded(entry.Layout, nil)
	lk.Version = 9
	lk.Table("Row").Lineage[0] = entry
	if _, err := lockfile.Parse("missing-facts", []byte(lk.Text())); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("legacy empty digest erased constraints: %v", err)
	}
	entry.Writer = nil
	entry.Digest = []byte("\x00schema-fixed-definitions\x03")
	entry.Wire = ir.TableFixedLayoutHashRecorded(entry.Layout, entry.Digest)
	lk.Table("Row").Lineage[0] = entry
	if _, err := lockfile.Parse("future", []byte(lk.Text())); err == nil || !strings.Contains(err.Error(), "unknown fixed definitions digest scheme") {
		t.Fatalf("unknown future epoch: %v", err)
	}
	if source == nil {
		t.Fatal("fixture lost checked source")
	}
}

func TestLockWriterExplicitTrustedLayoutOnlyImport(t *testing.T) {
	t.Parallel()
	dir, paths := fixture(t, rowTable("a int32 | min = 0, max = 10\nb int32"))
	writer := load(t, paths)
	if _, _, err := lockfile.Update(writer, paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	lk := readLock(t, path)
	legacy := lk.Table("Row").Lineage[0]
	legacy.Digest = nil
	legacy.Wire = ir.TableFixedLayoutHashRecorded(legacy.Layout, nil)
	legacy.Writer = nil
	lk.Table("Row").Lineage = append([]lockfile.LineageEntry{legacy}, lk.Table("Row").Lineage...)
	if err := os.WriteFile(path, []byte(lk.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("Row@0x%016x", legacy.Wire)
	before, _ := os.ReadFile(path)
	if _, _, err := lockfile.ImportWriter(writer, paths, target, writer); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("strict default erased missing digest facts: %v", err)
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(before, unchanged) {
		t.Fatal("strict refusal modified history")
	}
	options := lockfile.WriterImportOptions{LegacyLayoutOnly: true}
	if _, rewrote, err := lockfile.ImportWriterWithOptions(writer, paths, target, writer, options); err != nil || !rewrote {
		t.Fatalf("explicit layout-only import: %v %v", rewrote, err)
	}
	got := readLock(t, path)
	imported := got.Table("Row").Lineage[0]
	if !imported.LegacyLayoutOnly || imported.Writer == nil || imported.Writer.Fields[0].IntMax.Int64() != 10 || imported.Wire != legacy.Wire || len(imported.Digest) != 0 {
		t.Fatal("trusted import lost old checks or rewrote key")
	}
	if _, rewrote, err := lockfile.ImportWriterWithOptions(writer, paths, target, writer, options); err != nil || rewrote {
		t.Fatalf("explicit import not idempotent: %v %v", rewrote, err)
	}
	if _, _, err := lockfile.ImportWriter(writer, paths, target, writer); err == nil {
		t.Fatal("strict API silently reused explicit layout-only trust")
	}
	// A different bound attribution can share identical old file bytes. Trusted
	// metadata records one provenance; it cannot make the other files distinct.
	_, otherPaths := fixture(t, rowTable("a int32\nb int32 | min = 0, max = 10"))
	if _, _, err := lockfile.ImportWriterWithOptions(writer, paths, target, load(t, otherPaths), options); err == nil || !strings.Contains(err.Error(), "distinct captured writer constraints") {
		t.Fatalf("conflicting trusted import: %v", err)
	}
	prospective := fmt.Sprintf("Row@0x%016x", got.Table("Row").Lineage[1].Wire)
	if _, _, err := lockfile.ImportWriterWithOptions(writer, paths, prospective, writer, options); err == nil {
		t.Fatal("layout-only trust erased prospective digest")
	}
	// The marker itself is covered by the roll-up even if its removal leaves a
	// declaration whose legacy digest would otherwise be empty and valid.
	originalHash := lockfile.LineageHash(got.Table("Row").Lineage)
	got.Table("Row").Lineage[0].LegacyLayoutOnly = false
	if lockfile.LineageHash(got.Table("Row").Lineage) == originalHash {
		t.Fatal("trust marker is outside lineage rollup")
	}
}

func TestLockV8RollupIntegritySurvivesRenderingUpgrade(t *testing.T) {
	t.Parallel()
	saved, err := os.ReadFile("testdata/writer-v1/attribution/schema.lock")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/writer-v1/attribution/Writer.schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"snapshot", "retirement"} {
		t.Run(change, func(t *testing.T) {
			dir, paths := fixture(t, string(source))
			lk, err := lockfile.Parse("v8", saved)
			if err != nil {
				t.Fatal(err)
			}
			e := &lk.Table("Row").Lineage[0]
			if change == "retirement" {
				e.Retired = true
			} else {
				e.Writer.Fields[0].HasDefault = true
				e.Writer.Fields[0].DefInt = e.Writer.Fields[0].IntMax
			}
			oldLines, newLines := strings.Split(string(saved), "\n"), strings.Split(lk.Text(), "\n")
			for i := range oldLines {
				if strings.Contains(oldLines[i], "lineage wire=") {
					oldLines[i] = newLines[i]
				}
			}
			path := filepath.Join(dir, lockfile.FileName)
			tampered := []byte(strings.Join(oldLines, "\n"))
			if err := os.WriteFile(path, tampered, 0600); err != nil {
				t.Fatal(err)
			}
			errors := lockfile.Check(load(t, paths), paths)
			if len(errors) == 0 || !strings.Contains(errors[0].Error(), "roll-up") {
				t.Fatalf("v8 %s escaped rollup validation: %v", change, errors)
			}
			if _, _, err := lockfile.Update(load(t, paths), paths); err == nil {
				t.Fatal("relock erased old integrity failure")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(tampered, after) {
				t.Fatal("rejected tamper changed history")
			}
		})
	}
}

func TestLockWriterGenuineV6SalvageAndChangedSourceRefusal(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("testdata/writer-v6/Writer.schema")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile("testdata/writer-v6/schema.lock")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := os.ReadFile("testdata/writer-v6/identity.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"unchanged", "range", "width"} {
		t.Run(change, func(t *testing.T) {
			dir, paths := fixture(t, string(source))
			path := filepath.Join(dir, lockfile.FileName)
			if err := os.WriteFile(path, saved, 0600); err != nil {
				t.Fatal(err)
			}
			changed := string(source)
			if change == "range" {
				changed = strings.Replace(changed, "max = 10", "max = 20", 1)
			}
			if change == "width" {
				changed = strings.Replace(changed, "int32", "int64", 1)
			}
			if err := os.WriteFile(paths[0], []byte(changed), 0600); err != nil {
				t.Fatal(err)
			}
			u := load(t, paths)
			_, rewrote, err := lockfile.Update(u, paths)
			if change != "unchanged" {
				if err == nil || !strings.Contains(err.Error(), "restore the exact historical source, migrate the lock, then evolve") {
					t.Fatalf("changed v6 source lacks safe recovery: %v", err)
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(saved, after) {
					t.Fatal("failed v6 salvage modified history")
				}
				return
			}
			if err != nil || !rewrote {
				t.Fatalf("genuine v6 salvage: %v %v", rewrote, err)
			}
			got := readLock(t, path)
			entries := got.Table("Row").Lineage
			if len(entries) != 2 || entries[0].Writer != nil || len(entries[0].Digest) != 0 || entries[1].Writer == nil {
				t.Fatal("salvage invented historical source or dropped current writer")
			}
			actual := fmt.Sprintf("wire=0x%016x\nrecord=%d\nlayout=%x\n", entries[0].Wire, entries[0].Record, entries[0].Layout)
			if actual != string(identity) {
				t.Fatalf("genuine v6 identity changed:\n%s\nwant\n%s", actual, identity)
			}
			target := fmt.Sprintf("Row@0x%016x", entries[0].Wire)
			if _, _, err := lockfile.ImportWriter(u, paths, target, u); err == nil {
				t.Fatal("ordinary import guessed source provenance for a constrained layout-only writer")
			}
			if _, _, err := lockfile.ImportWriterWithOptions(u, paths, target, u, lockfile.WriterImportOptions{LegacyLayoutOnly: true}); err != nil {
				t.Fatalf("explicit historical source: %v", err)
			}
			if errs := lockfile.Check(u, paths); len(errs) != 0 {
				t.Fatal(errs)
			}
		})
	}
}
