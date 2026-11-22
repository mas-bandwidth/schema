package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Check describes a file without a unit. Give every case a neutral basename:
// neither the extension nor a directory named "cook" should supply the answer.
func TestCheckClassifiesPublishedArtifacts(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	repo := filepath.Join("..", "..")
	var cases []struct{ Role, File string }
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join("testdata", "form_classification", "artifacts.json"))), &cases); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{
		"cook-le": "cook", "cook-be": "cook", "block": "block",
		"variable": "form 1", "message": "form 2", "fixed": "form 3",
	}
	for _, c := range cases {
		want, ok := labels[c.Role]
		if !ok {
			t.Fatalf("duplicate or unknown artifact role %q", c.Role)
		}
		delete(labels, c.Role)
		t.Run(c.Role, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "artifact.bin")
			writeTestFile(t, file, mustRead(t, filepath.Join(repo, filepath.FromSlash(c.File))))
			switch c.Role {
			case "cook-le", "cook-be":
				order := "little-endian"
				if c.Role == "cook-be" {
					order = "big-endian"
				}
				opened := run(t, bin, "cook-check", "--root", "Catalog", "--verbose", file, filepath.Join(repo, "tables", "blobs"))
				if !strings.Contains(opened, order) || !strings.Contains(opened, "2 nodes") {
					t.Fatalf("unexpected native cook validation: %s", opened)
				}
			case "block":
				// TestRowR22 in test/conformance/go/rows opens these same
				// committed bytes with the generated RenderFrameBlock reader.
			case "variable":
				run(t, bin, "unpack", "--in", file, filepath.Join(repo, "tables", "backend"))
			case "message":
				run(t, bin, "unpack", "--announce", filepath.Join(repo, "testdata/wire/tables/backend_conn.bin"),
					"--in", file, "--root", "LoginRequest", "--one-file", t.TempDir(), filepath.Join(repo, "tables", "backend"))
			case "fixed":
				run(t, bin, "unpack", "--in", file, "--root", "ArchiveConfig", "--one-file", t.TempDir(), filepath.Join(repo, "tables", "examples"))
			}
			out := run(t, bin, "check", file)
			line, _, _ := strings.Cut(out, "\n")
			description, named := strings.CutPrefix(line, file+": ")
			description = strings.ToLower(description)
			if !named || !strings.Contains(description, want) || strings.Contains(description, "assigned by no form") || strings.Contains(description, "planned") {
				t.Errorf("wanted the supported %s artifact named in the form line, got %q", c.Role, line)
			}
		})
	}
	if len(labels) != 0 {
		t.Fatalf("corpus is missing artifact roles %v", labels)
	}
}
