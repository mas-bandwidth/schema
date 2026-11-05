package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateFixedRetiresTheCookWriteOfANoFileFormInstance holds the
// migration to a manifest the next command can read: an instance at a fixed
// root past the form-3 ceiling has no file form, so the cook-write row that
// reaches it is retired with it, its cooked files go, and generate completes
// over the migrated manifest.
func TestMigrateFixedRetiresTheCookWriteOfANoFileFormInstance(t *testing.T) {
	t.Parallel()
	repo := conformanceRoot(t)
	dir := t.TempDir()
	old, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/wide_blob.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(old) == 0 || old[0] == 3 {
		t.Fatalf("the historical wide_blob input is not a form-1 file: % x", old[:min(len(old), 4)])
	}
	wire := filepath.Join(dir, "wide_blob.bin")
	little := filepath.Join(dir, "cook-write", "wide_blob.cook")
	big := filepath.Join(dir, "cook-write", "wide_blob-be.cook")
	if err := os.MkdirAll(filepath.Dir(little), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{wire: old, little: {0}, big: {0}} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(dir, "MANIFEST.txt")
	jsonDir := filepath.Join(dir, "json")
	body := strings.Join([]string{
		"unit tabledemo " + filepath.Join(repo, "tables/examples"),
		"instance wide_blob tabledemo WideBlob " + wire,
		"cook-write wide_blob " + little + " " + big,
		"",
	}, "\n")
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(manifest, jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateFixed(m, manifest, jsonDir); err != nil {
		t.Fatal(err)
	}
	migrated, err := ReadManifest(manifest, jsonDir)
	if err != nil {
		t.Fatalf("the migrated manifest does not read: %v", err)
	}
	if len(migrated.Instances) != 0 || len(migrated.CookWrites) != 0 {
		t.Fatalf("instance and cook-write rows survive: %d instances, %d cook-writes", len(migrated.Instances), len(migrated.CookWrites))
	}
	if len(migrated.Reports) != 1 || migrated.Reports[0].Name != "previous_form_wide_blob" || migrated.Reports[0].Wire != wire {
		t.Fatalf("the historical bytes are not kept as the refusal row: %+v", migrated.Reports)
	}
	for _, path := range []string{little, big} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: a cooked file no row names survives: %v", path, err)
		}
	}
	if err := generate(migrated, jsonDir, filepath.Join(dir, "reports.txt")); err != nil {
		t.Fatalf("generate over the migrated manifest: %v", err)
	}
}
