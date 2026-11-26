package compiler

import (
	"encoding/csv"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Each .schema file is a separate, explicitly named CLI input. The two
// build contexts distinguish an ordinary source file from a test-only,
// platform-specific or hidden file without requiring either target host.
func TestGoBasenameCorpus(t *testing.T) {
	t.Parallel()
	const root = "testdata/go-basenames"
	t.Run("MANIFEST.tsv", func(t *testing.T) {
		rows := readGoBasenameManifest(t, root)
		var named []string
		for _, row := range rows {
			named = append(named, filepath.Join(root, row[0]))
		}
		slices.Sort(named)
		onDisk, err := filepath.Glob(filepath.Join(root, "*.schema"))
		if err != nil || !slices.Equal(named, onDisk) {
			t.Fatalf("manifest inputs %v, on disk %v, error %v", named, onDisk, err)
		}
	})
	for _, row := range readGoBasenameManifest(t, root) {
		path := filepath.Join(root, row[0])
		t.Run(row[0], func(t *testing.T) {
			c := New()
			name := strings.TrimSuffix(row[0], ".schema") + ".go"
			unit, err := c.Load([]string{path})
			switch row[1] {
			case "accept":
				if err != nil {
					t.Fatal(err)
				}
				files, err := c.Generate(unit, "go", Options{})
				if err != nil || len(files[name]) == 0 {
					t.Errorf("Go generation: want %s, got error %v", name, err)
				}
			case "refuse":
				if err == nil || !strings.Contains(err.Error(), row[0]) || !strings.Contains(err.Error(), "rename the file") {
					t.Errorf("check: want a refusal naming %s and asking for a rename, got %v", row[0], err)
				}
			default:
				t.Fatalf("unknown generation verdict %q", row[1])
			}

			// Always give Go the candidate filename, including ones Schema
			// correctly refused to write. The anchor keeps the package valid
			// when Go excludes the candidate from its ordinary source files.
			dir := t.TempDir()
			for _, file := range []string{"anchor.go", name} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte("package p\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for i, platform := range [][2]string{{"linux", "arm64"}, {"windows", "amd64"}} {
				ctx := build.Default
				ctx.GOOS, ctx.GOARCH = platform[0], platform[1]
				pkg, err := ctx.ImportDir(dir, 0)
				if err != nil {
					t.Fatal(err)
				}
				class := "ignored"
				if slices.Contains(pkg.GoFiles, name) {
					class = "go"
				} else if slices.Contains(pkg.TestGoFiles, name) {
					class = "test"
				}
				if want := row[2+i]; class != want {
					t.Errorf("%s/%s classifies %s as %s, manifest wants %s", ctx.GOOS, ctx.GOARCH, name, class, want)
				}
			}
		})
	}
}

// readGoBasenameManifest is the manifest read. TestGoBasenameCorpus runs it
// inside t.Run("MANIFEST.tsv"), and again to name the per-file subtests.
func readGoBasenameManifest(t *testing.T, root string) [][]string {
	t.Helper()
	manifest, err := os.Open(filepath.Join(root, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	reader := csv.NewReader(manifest)
	reader.Comma = '\t'
	reader.Comment = '#'
	reader.FieldsPerRecord = 4
	rows, err := reader.ReadAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("basename manifest: %d rows, error %v", len(rows), err)
	}
	return rows
}
