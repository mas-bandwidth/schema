package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func fixedBoundUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Bound.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func readWideSchema(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../testdata/wire/tables/storage-cap/wide/Wide.schema")
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

var fixedBoundTableMark = regexp.MustCompile(`(?i)(?:\b|_)(probe_fixed|ProbeFixed|probeFixed|PROBE_FIXED)[A-Za-z_]*\b`)

func fixedBoundMarks(t *testing.T, lang string, u *ir.Unit) []string {
	t.Helper()
	files, err := New().Generate(u, lang, Options{})
	if err != nil {
		t.Fatalf("%s: %v", lang, err)
	}
	seen := map[string]bool{}
	for _, content := range files {
		for _, m := range fixedBoundTableMark.FindAllString(string(content), -1) {
			seen[strings.TrimPrefix(m, "_")] = true
		}
	}
	marks := make([]string, 0, len(seen))
	for m := range seen {
		marks = append(marks, m)
	}
	return marks
}

// TestFixedRecordBoundAtTheCeilingEmitsOnAllNineLegs: a fixed table whose
// record is exactly 65536 bytes (docs/SPEC-TABLES.md §3.4, fixed_record_bound =
// 65536) is at the wire ceiling and emits the fixed form on all nine legs; one
// byte past the ceiling emits no fixed form on any leg.
func TestFixedRecordBoundAtTheCeilingEmitsOnAllNineLegs(t *testing.T) {
	t.Parallel()
	atUnit := fixedBoundUnit(t, "package probe\nfixed table Probe { payload bytes(65532) }\n")
	at := atUnit.Tables["Probe"]
	if ir.TableFixedTypeBytes(at) != ir.TableFixedRecordMaxBytes {
		t.Fatalf("Probe at bound is %d bytes; want %d", ir.TableFixedTypeBytes(at), ir.TableFixedRecordMaxBytes)
	}
	if !ir.TableFixedEmitted(atUnit, at) {
		t.Fatal("a 65536-byte fixed table must emit the fixed form")
	}
	for _, lang := range fixedDepthLegs {
		if got := fixedBoundMarks(t, lang, atUnit); len(got) == 0 {
			t.Errorf("%s emitted no fixed-form symbols for a 65536-byte table at the record bound", lang)
		}
	}

	overUnit := fixedBoundUnit(t, "package probe\nfixed table Probe { payload bytes(65533) }\n")
	over := overUnit.Tables["Probe"]
	if ir.TableFixedTypeBytes(over) <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("Probe past bound is %d bytes; want > %d", ir.TableFixedTypeBytes(over), ir.TableFixedRecordMaxBytes)
	}
	if ir.TableFixedEmitted(overUnit, over) {
		t.Fatal("a fixed table past 65536 bytes must not emit the fixed form")
	}
	for _, lang := range fixedDepthLegs {
		if got := fixedBoundMarks(t, lang, overUnit); len(got) != 0 {
			t.Errorf("%s emitted fixed-form symbols %v for a %d-byte table past the %d-byte record bound",
				lang, got, ir.TableFixedTypeBytes(over), ir.TableFixedRecordMaxBytes)
		}
	}
}

// TestFixedRecordBoundStorageCapUnitsDeclineFixedForm: the storage-cap units
// (Cap.schema at 2^27 bytes; Wide.schema at 2 GiB and 6 GiB) are past the
// 65536-byte fixed-form record bound and keep form 1 without emitting form 3.
func TestFixedRecordBoundStorageCapUnitsDeclineFixedForm(t *testing.T) {
	t.Parallel()
	capSrc, err := os.ReadFile("../testdata/wire/tables/storage-cap/Cap.schema")
	if err != nil {
		t.Fatal(err)
	}
	capUnit := fixedBoundUnit(t, string(capSrc))
	big := capUnit.Tables["Big"]
	if big == nil || ir.TableFixedEmitted(capUnit, big) {
		t.Fatalf("Cap.Big must decline the fixed form (size=%d)", ir.TableFixedTypeBytes(big))
	}
	wideUnit := fixedBoundUnit(t, readWideSchema(t))
	for _, name := range []string{"Part", "Huge"} {
		st := wideUnit.Tables[name]
		if st == nil || ir.TableFixedEmitted(wideUnit, st) {
			t.Fatalf("Wide.%s must decline the fixed form", name)
		}
	}
	if ir.TableFixedTypeBytes(wideUnit.Tables["Huge"]) <= 1<<32 {
		t.Fatalf("Wide.Huge size=%d, want > 2^32", ir.TableFixedTypeBytes(wideUnit.Tables["Huge"]))
	}
	c := New()
	for _, lang := range fixedDepthLegs {
		if _, err := c.Generate(capUnit, lang, Options{}); err != nil {
			t.Fatalf("Cap/%s: %v", lang, err)
		}
	}
}

// TestFixedRecordBoundLockKeepsHistoryPastTheCeiling: a committed lock that
// already holds lineage for a table past the record bound (tables/block's
// 7.5 MB RenderFrame) still checks, and writing the lock again keeps every
// entry it holds; growing a locked table from at the 65536-byte bound to past
// it is refused as a change of form.
func TestFixedRecordBoundLockKeepsHistoryPastTheCeiling(t *testing.T) {
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
	u, err := New().Load(paths)
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

	// Crossing from 65536 (at the ceiling) to 65544 (past the ceiling) is refused by lockfile.Check.
	crossDir := t.TempDir()
	schemaPath := filepath.Join(crossDir, "Bound.schema")
	atSrc := "package probe\nfixed table Probe { payload bytes(65532) }\n"
	if err := os.WriteFile(schemaPath, []byte(atSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	atU, err := New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(atU, []string{schemaPath}); err != nil {
		t.Fatal(err)
	}
	overSrc := "package probe\nfixed table Probe { payload bytes(65533) }\n"
	if err := os.WriteFile(schemaPath, []byte(overSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	overU, err := New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(overU, []string{schemaPath}); len(errs) == 0 {
		t.Fatal("growing a locked fixed table across the 65536-byte ceiling was not refused")
	}
}
