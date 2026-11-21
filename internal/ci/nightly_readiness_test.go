package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every target owes parity: the BOM entry and runtime readiness steps run for
// every port leg, never for a listed subset, and the coverage gate requires all
// 18 targets from a harness built with every leg tag.
func TestNightlyAcceptanceCoversEveryTarget(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/nightly-conformance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(raw)
	for _, stale := range []string{"original-nine", "original nine", "require-original-nine"} {
		if strings.Contains(strings.ToLower(wf), stale) {
			t.Errorf("workflow still scopes acceptance by %q", stale)
		}
	}
	port := yamlJobs(wf)["port"]
	for _, name := range []string{
		"the ${{ matrix.lang }} leg checks the four BOM entry cases",
		"the ${{ matrix.lang }} runtime readiness gate",
	} {
		_, rest, ok := strings.Cut(port, "      - name: "+name+"\n")
		if !ok {
			t.Fatalf("missing step %q", name)
		}
		body, _, _ := strings.Cut(rest, "\n      - ")
		if strings.Contains(body, "fromJSON(") || strings.Contains(body, "matrix.lang ==") {
			t.Errorf("step %q is limited to a subset of the legs:\n%s", name, body)
		}
	}
	coverage := yamlJobs(wf)["coverage"]
	if !strings.Contains(coverage, `go run ./test/conformance/harness coverage --in build/nightly/rows --require-every-target`) {
		t.Errorf("coverage step does not require every target from the default-build harness:\n%s", coverage)
	}
	if strings.Contains(coverage, "-tags") {
		t.Errorf("coverage step builds the harness with tags; every target is in the default build:\n%s", coverage)
	}
}

// Execute the actual workflow shell, including its logging pipeline: a failed
// acceptance program or an empty successful command cannot leave a pass receipt.
func TestNightlyReadinessCannotPublishPassAfterFailure(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/nightly-conformance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, lang, dir string }{
		{"the C++ runtime readiness gate", "cpp", "reference-report"},
		{"the ${{ matrix.lang }} runtime readiness gate", "dart", "report"},
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
				var identityEnv []string
				if tc.lang != "cpp" {
					// Stage the real identity helper with ordinary file metadata;
					// the readiness program stays synthetic and keeps every outcome.
					helper := filepath.Join(dir, "test/conformance/fixed-corpus-identity.py")
					data, err := os.ReadFile(filepath.Join(repoRoot(t), "test/conformance/fixed-corpus-identity.py"))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(helper, data, 0600); err != nil {
						t.Fatal(err)
					}
					reference, consumer := filepath.Join(dir, "shared"), filepath.Join(dir, "build")
					for _, root := range []string{reference, consumer} {
						for _, name := range []string{"fixedform-corpus/manifest.txt", "fixedform-corpus/ordinary.bin", "fixedform-bench-corpus/bench_fixed.bin", "fixedform-bench-corpus/bench_fixed.oracle.json"} {
							path := filepath.Join(root, name)
							if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(path, []byte("ordinary synthetic metadata\n"), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
					receipt := filepath.Join(dir, "identity.json")
					if out, err := exec.Command("python3", helper, "snapshot", reference, receipt).CombinedOutput(); err != nil {
						t.Fatalf("identity snapshot: %v\n%s", err, out)
					}
					identityEnv = []string{"SHARED_FIXED_REFERENCE=" + reference, "SHARED_FIXED_RECEIPT=" + receipt}
				}
				cmd := exec.Command("bash", "-c", script)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
				cmd.Env = append(cmd.Env, identityEnv...)
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
