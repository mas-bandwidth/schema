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
	var named []string
	for _, row := range rows {
		path := filepath.Join(root, row[0])
		named = append(named, path)
		t.Run(row[0], func(t *testing.T) {
			c := New()
			name := strings.TrimSuffix(row[0], ".schema") + ".go"
			unit, loadErr := c.Load([]string{path})
			switch {
			case row[1] != "accept" && row[1] != "refuse":
				t.Fatalf("unknown generation verdict %q", row[1])
			case loadErr != nil:
				// A FILE NAME SOME TARGET CANNOT BUILD IS REFUSED AT CHECK, for
				// every target (SPEC §4.6), before Go generation runs: the
				// refusal names the file and asks for a rename.
				if row[1] != "refuse" || !strings.Contains(loadErr.Error(), row[0]) || !strings.Contains(loadErr.Error(), "rename the file") {
					t.Errorf("check: want %s for %s, got the refusal %v", row[1], row[0], loadErr)
				}
			default:
				files, err := c.Generate(unit, "go", Options{})
				switch row[1] {
				case "accept":
					if err != nil || len(files[name]) == 0 {
						t.Errorf("Go generation: want %s, got error %v", name, err)
					}
				case "refuse":
					if err == nil || !strings.Contains(err.Error(), row[0]) || !strings.Contains(err.Error(), "build constraint") {
						t.Errorf("Go generation: want a build-constraint refusal naming %s, got %v", row[0], err)
					}
				}
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
	slices.Sort(named)
	onDisk, err := filepath.Glob(filepath.Join(root, "*.schema"))
	if err != nil || !slices.Equal(named, onDisk) {
		t.Fatalf("manifest inputs %v, on disk %v, error %v", named, onDisk, err)
	}
}
