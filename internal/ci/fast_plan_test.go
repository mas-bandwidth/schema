package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// selectorOutput returns the selector's own verdict from the combined output.
// The script ends with a newline-free `printf`, so its verdict is the text
// after the last newline; a bench whose bash sources a startup file that reads
// an unset PS1 under the selector's `set -u` prefixes the selection with a
// diagnostic line that no child environment can suppress, and only that line
// is dropped. A clean runner has no newline, so the output is unchanged.
func selectorOutput(out []byte) string {
	if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
		return string(out[i+1:])
	}
	return string(out)
}

// scriptEnv returns environ without the startup files a non-interactive bash
// sources before the -c script: BASH_ENV, or ENV for a POSIX shell. A bench
// whose BASH_ENV reads an unset variable under the selector's `set -u` writes
// that diagnostic to stderr, and CombinedOutput would capture it into the
// selection. The workflow runs where neither name is set.
func scriptEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "BASH_ENV=") || strings.HasPrefix(kv, "ENV=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Exercise the workflow's actual selector: a green plan that selects no
// harness must not silently skip a changed C++ fixed-table regression test.
func TestFastPlanSelectsTableFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          # THE PACKAGES.")
	if !ok {
		t.Fatal("fast workflow package boundary not found")
	}
	// Remove the remainder of the first comment line and YAML indentation.
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s|%s' "$broad" "$legs"`)
	for _, tc := range []struct{ name, files, want string }{
		{"cpp witness", "test/tables/versioning_numbers.cpp", "no| cpp"},
		{"cpp header", "test/tables/floatnan.h", "no| cpp"},
		{"shared schema", "test/tables/VNEW_floor.schema", "yes|cpp go c rust js cs java dart elixir"},
		{"documentation", "README.md", "no|"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
			cmd.Env = append(scriptEnv(os.Environ()), "files="+tc.files)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("selector: %v: %s", err, out)
			}
			got := selectorOutput(out)
			if got != tc.want {
				t.Fatalf("%s selected %q; want %q", tc.files, got, tc.want)
			}
		})
	}
}
