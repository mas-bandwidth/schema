package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the actual workflow shell, including its logging pipeline: a failed
// acceptance program or an empty successful command cannot leave a pass receipt.
func TestNightlyReadinessCannotPublishPassAfterFailure(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/nightly-conformance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, lang, dir string }{
		{"the C++ original-nine readiness gate", "cpp", "reference-report"},
		{"the original-nine runtime readiness gate", "dart", "report"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			text := string(raw)
			_, text, ok := strings.Cut(text, "      - name: "+tc.name+"\n")
			if !ok {
				t.Fatal("readiness step missing")
			}
			_, text, ok = strings.Cut(text, "        run: |\n")
			if !ok {
				t.Fatal("readiness command missing")
			}
			var lines []string
			for line := range strings.SplitSeq(text, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				if !strings.HasPrefix(line, "          ") {
					break
				}
				lines = append(lines, strings.TrimPrefix(line, "          "))
			}
			script := strings.Join(lines, "\n")
			script = strings.ReplaceAll(script, "${{ matrix.lang }}", tc.lang)
			script = strings.ReplaceAll(script, "${{ matrix.env }}", "")
			for _, run := range []struct {
				status, markers int
				pass            bool
			}{{0, 1, true}, {23, 1, false}, {0, 0, false}, {0, 2, false}} {
				dir := t.TempDir()
				entry := filepath.Join(dir, "test/conformance/nightly-readiness")
				if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
					t.Fatal(err)
				}
				body := fmt.Sprintf("import sys\nassert sys.argv[1] == %q\nprint('ACCEPTANCE_DIAGNOSTIC')\n", tc.lang)
				for range run.markers {
					body += fmt.Sprintf("print('NIGHTLY_READINESS_PASS language=%s phases=42')\n", tc.lang)
				}
				body += fmt.Sprintf("sys.exit(%d)\n", run.status)
				if err := os.WriteFile(entry, []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
				bin := filepath.Join(dir, "bin")
				if err := os.Mkdir(bin, 0700); err != nil {
					t.Fatal(err)
				}
				const revision = "0123456789012345678901234567890123456789"
				if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf '"+revision+"\\n'\n"), 0700); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("bash", "-c", script)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
				out, err := cmd.CombinedOutput()
				if (err == nil) != run.pass {
					t.Fatalf("run%+v: exit=%v output=%s", run, err, out)
				}
				prefix := filepath.Join(dir, "build/nightly", tc.dir, "readiness-"+tc.lang)
				record, err := os.ReadFile(prefix + ".txt")
				if err != nil {
					t.Fatal(err)
				}
				want := "fail\n"
				if run.pass {
					want = "pass\n"
				}
				if string(record) != want {
					t.Fatalf("run%+v published %q", run, record)
				}
				rev, err := os.ReadFile(prefix + ".revision")
				if err != nil || string(rev) != revision+"\n" {
					t.Fatalf("revision missing: %s %v", rev, err)
				}
				log, err := os.ReadFile(prefix + ".log")
				if err != nil || !strings.Contains(string(log), "ACCEPTANCE_DIAGNOSTIC") {
					t.Fatalf("diagnostic lost: %s %v", log, err)
				}
			}
		})
	}
}
