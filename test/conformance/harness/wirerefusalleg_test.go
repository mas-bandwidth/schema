package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWireRefusalIsPerLeg holds the `<lang>-refusal=` markers to the leg they
// name: the C writer refuses a shared blob by name, so the C wire answer is
// that name, while a leg the row does not name still owes bytes.
func TestWireRefusalIsPerLeg(t *testing.T) {
	t.Parallel()

	readRow := func(t *testing.T, markers string) (*Manifest, error) {
		t.Helper()
		dir := t.TempDir()
		manifest := filepath.Join(dir, "MANIFEST.txt")
		text := "unit mapdemo testdata/conformance/tables/maps.schema\n" +
			"instance blob_shared_string mapdemo Docs " + filepath.Join(dir, "w.bin") + " no-text " + markers + "\n"
		if err := os.WriteFile(manifest, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return ReadManifest(manifest, filepath.Join(dir, "json"))
	}

	t.Run("both markers parse, each to its leg", func(t *testing.T) {
		t.Parallel()
		m, err := readRow(t, "go-refusal=blob_shared c-refusal=blob_shared")
		if err != nil {
			t.Fatal(err)
		}
		inst := m.Instances[0]
		if inst.GoWireRefusal != "blob_shared" || inst.WireRefusals["go"] != "blob_shared" || inst.WireRefusals["c"] != "blob_shared" {
			t.Fatalf("refusals = go %q, per leg %v", inst.GoWireRefusal, inst.WireRefusals)
		}
		if inst.WireRefusals["lua"] != "" {
			t.Fatalf("lua was given a refusal no marker names: %v", inst.WireRefusals)
		}
	})

	t.Run("a duplicate C marker is refused", func(t *testing.T) {
		t.Parallel()
		_, err := readRow(t, "c-refusal=blob_shared c-refusal=blob_shared")
		if err == nil || !strings.Contains(err.Error(), "duplicate C wire refusal marker") {
			t.Fatalf("duplicate c-refusal: err %v", err)
		}
	})

	refusals := map[string]string{"go": "blob_shared", "c": "blob_shared"}
	pinned := map[string]bool{"blob_shared": true}
	want := expectation{"blob_shared_string", []byte("golden wire")}

	t.Run("the C leg's named absence answers its refusal", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared_string.absent"), []byte("blob_shared"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreLeg("c", "wire", out, want, refusals, pinned, r)
		if r.missing != 1 || len(r.failures) != 0 {
			t.Fatalf("c wire = absent %d, failures %v; want one named absence", r.missing, r.failures)
		}
	})

	for _, answer := range []struct{ name, file, data string }{
		{"bytes", "blob_shared_string", "golden wire"},
		{"an unnamed absence", "blob_shared_string.absent", ""},
		{"a different name", "blob_shared_string.absent", "table_refused"},
	} {
		t.Run(answer.name+" does not answer the C refusal", func(t *testing.T) {
			t.Parallel()
			out := t.TempDir()
			if err := os.WriteFile(filepath.Join(out, answer.file), []byte(answer.data), 0o644); err != nil {
				t.Fatal(err)
			}
			r := &result{total: 1}
			scoreLeg("c", "wire", out, want, refusals, pinned, r)
			if len(r.failures) != 1 || r.missing != 0 || r.pass != 0 {
				t.Fatalf("c wire = pass %d absent %d failures %v; want red", r.pass, r.missing, r.failures)
			}
		})
	}

	t.Run("a leg no marker names keeps the bytes expectation", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared_string"), want.want, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreLeg("lua", "wire", out, want, refusals, pinned, r)
		if r.pass != 1 || len(r.failures) != 0 {
			t.Fatalf("lua wire = pass %d failures %v; want the bytes", r.pass, r.failures)
		}
	})

	t.Run("a named refusal on a row that names none for the leg is red", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared_string.absent"), []byte("blob_shared"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreLeg("cpp", "wire", out, want, refusals, pinned, r)
		if len(r.failures) != 1 || r.missing != 0 {
			t.Fatalf("cpp wire = absent %d failures %v; an unpinned named refusal must be red", r.missing, r.failures)
		}
		r = &result{total: 1}
		scoreLeg("c", "wire", out, want, nil, pinned, r)
		if len(r.failures) != 1 || r.missing != 0 {
			t.Fatalf("unmarked c wire = absent %d failures %v; an unpinned named refusal must be red", r.missing, r.failures)
		}
	})

	for _, note := range []string{"", "unit not generated for lua: docs/SPEC-TABLES.md §15\n"} {
		t.Run(fmt.Sprintf("an absence carrying %q on an unmarked row is still a missing feature", note), func(t *testing.T) {
			t.Parallel()
			out := t.TempDir()
			if err := os.WriteFile(filepath.Join(out, "blob_shared_string.absent"), []byte(note), 0o644); err != nil {
				t.Fatal(err)
			}
			r := &result{total: 1}
			scoreLeg("cpp", "wire", out, want, refusals, pinned, r)
			if r.missing != 1 || len(r.failures) != 0 {
				t.Fatalf("cpp wire = absent %d failures %v; want one missing", r.missing, r.failures)
			}
		})
	}

	t.Run("the C refusal reaches the wire surface alone", func(t *testing.T) {
		t.Parallel()
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "blob_shared_string"), want.want, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreLeg("c", "report", out, want, refusals, pinned, r)
		if r.pass != 1 || len(r.failures) != 0 {
			t.Fatalf("c report = pass %d failures %v; want the bytes", r.pass, r.failures)
		}
	})
}
