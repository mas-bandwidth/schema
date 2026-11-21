package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGooseHostileMissingCodecIsAnEmptyAbsence: a schema the goose target
// does not generate is a MISSING CODEC, and make/goose-hostile.sh answers each
// of its rows with a bare `<name> absent`. It used to write `absent
// target-refuses-schema`, a reason, which reads as the port REFUSING the row
// (test/conformance/README.md: a missing codec is an empty absence, a refusal
// is a reasoned one). The schema compiler is a stand-in that refuses every
// generate; the goose compiler is never reached. Reads and writes only its
// temporary directory.
func TestGooseHostileMissingCodecIsAnEmptyAbsence(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tmp := t.TempDir()
	write := func(path, body string, mode os.FileMode) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	schema := write(filepath.Join(tmp, "bin", "schema"), "#!/bin/sh\necho 'goose: this schema is not supported' >&2\nexit 1\n", 0o755)
	goose := write(filepath.Join(tmp, "bin", "goose"), "#!/bin/sh\necho 'goose: never run for a schema that does not generate' >&2\nexit 9\n", 0o755)
	corpus := filepath.Join(tmp, "corpus")
	// two rows over one schema the target refuses: each answers its own
	// absence
	write(filepath.Join(corpus, "manifest.tsv"), strings.Join([]string{
		"# name\tcarrier\tschema\ttable\tinput\tnote\tverdict\treason\tdigest",
		"nogen-read\twire\tnosuch/Gone.schema\tGone\tinputs/a.bin\t-\taccept\t-\t-",
		"nogen-refuse\twire\tnosuch/Gone.schema\tGone\tinputs/b.bin\t-\trefuse\twire_damaged\t-",
	}, "\n")+"\n", 0o644)
	known := write(filepath.Join(tmp, "known.tsv"), "# none\n", 0o644)
	work := filepath.Join(tmp, "work")
	cmd := exec.Command("sh", "make/goose-hostile.sh", goose, schema, corpus, work, known)
	cmd.Dir = root
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "2 absent") {
		t.Fatalf("the run did not count both rows absent:\n%s", out)
	}
	lines, err := os.ReadFile(filepath.Join(work, "lines"))
	if err != nil {
		t.Fatal(err)
	}
	want := "nogen-read absent\nnogen-refuse absent\n"
	if string(lines) != want {
		t.Fatalf("a schema the target does not generate answered\n%s\nwant an EMPTY absence per row (no reason):\n%s", lines, want)
	}
}
