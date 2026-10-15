package darttable

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCookWriteAnswersEveryCookWriteRowDart pins §7.6's runtime writer to
// schema cook's committed bytes in both byte orders, one manifest row at a
// time. The driver loads each row's instance wire before measuring and writing.
func TestCookWriteAnswersEveryCookWriteRowDart(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, little, big string }
	var rows []row
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "cook-write" {
			continue
		}
		if len(fields) != 4 || seen[fields[1]] {
			t.Fatalf("bad or duplicate cook-write row: %q", line)
		}
		seen[fields[1]] = true
		rows = append(rows, row{fields[1], fields[2], fields[3]})
	}
	if len(rows) != 46 {
		t.Fatalf("cook-write manifest has %d rows, want 46", len(rows))
	}
	outDir := t.TempDir()
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "test/conformance/dart/main.dart", manifest, "cook-write", outDir)
	cmd.Dir = repo
	output, runErr := cmd.CombinedOutput()
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range []struct{ suffix, golden string }{{"", r.little}, {"-be", r.big}} {
				got, err := os.ReadFile(filepath.Join(outDir, r.name+order.suffix))
				if err != nil {
					t.Fatalf("%s runtime cook output missing (driver: %v): %v\n%s", order.suffix, runErr, err, output)
				}
				want, err := os.ReadFile(filepath.Join(repo, order.golden))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s runtime cook differs from schema cook: got %d bytes, want %d", order.suffix, len(got), len(want))
				}
			}
		})
	}
}
