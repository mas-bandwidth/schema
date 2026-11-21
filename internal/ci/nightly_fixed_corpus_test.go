package ci

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These are synthetic file metadata, never packet/native inputs or producers.
func TestNightlyFixedCorpusMetadata(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"unchanged", "identical rewrite", "non-inputs", "changed", "missing", "extra", "reference changed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			reference, consumer := filepath.Join(dir, "reference"), filepath.Join(dir, "consumer")
			receipt := filepath.Join(dir, "report", "identity.json")
			files := map[string]string{
				"fixedform-corpus/manifest.txt":                  "synthetic metadata rows\n",
				"fixedform-corpus/a.bin":                         "ordinary file A\n",
				"fixedform-corpus/b.bin":                         "ordinary file B\n",
				"fixedform-bench-corpus/bench_fixed.bin":         "ordinary bench file\n",
				"fixedform-bench-corpus/bench_fixed.oracle.json": "ordinary semantic metadata\n",
			}
			write := func(root, name, content string) {
				t.Helper()
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range files {
				write(reference, name, content)
				write(consumer, name, content)
			}
			run := func(args ...string) ([]byte, error) {
				command := exec.Command("python3", append([]string{filepath.Join(repoRoot(t), "test", "conformance", "fixed-corpus-identity.py")}, args...)...)
				return command.CombinedOutput()
			}
			if out, err := run("snapshot", reference, receipt); err != nil {
				t.Fatalf("snapshot: %v\n%s", err, out)
			}
			switch kind {
			case "identical rewrite":
				write(consumer, "fixedform-corpus/a.bin", files["fixedform-corpus/a.bin"])
			case "non-inputs":
				for _, name := range []string{"fixedform-corpus/.stamp", "fixedform-bench-corpus/.stamp", "fixedform-bench-corpus/corpus"} {
					write(reference, name, "producer metadata\n")
					write(consumer, name, "rebuilt metadata\n")
				}
			case "changed":
				write(consumer, "fixedform-corpus/a.bin", "different metadata\n")
			case "missing":
				if err := os.Remove(filepath.Join(consumer, "fixedform-corpus", "a.bin")); err != nil {
					t.Fatal(err)
				}
			case "extra":
				write(consumer, "fixedform-corpus/extra.txt", "extra metadata\n")
			case "reference changed":
				write(reference, "fixedform-corpus/a.bin", "different metadata\n")
			}
			out, err := run("verify", reference, receipt, "--consumer", consumer, "--phase", "metadata")
			wantPass := kind == "unchanged" || kind == "identical rewrite" || kind == "non-inputs"
			if (err == nil) != wantPass {
				t.Fatalf("pass=%v, want %v: %v\n%s", err == nil, wantPass, err, out)
			}
			var record struct {
				Files  map[string]any
				Status string
			}
			data, err := os.ReadFile(receipt)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			if len(record.Files) != len(files) || (record.Status == "pass") != wantPass {
				t.Fatalf("wrong durable receipt: %s", data)
			}
			if kind == "changed" {
				write(consumer, "fixedform-corpus/a.bin", files["fixedform-corpus/a.bin"])
				if out, err := run("verify", reference, receipt, "--consumer", consumer); err == nil {
					t.Fatalf("earlier failure was erased: %s", out)
				}
			}
		})
	}
}

func TestNightlyFixedCorpusWiring(t *testing.T) {
	t.Parallel()
	wf := workflows(t, repoRoot(t))["nightly-conformance.yml"]
	if errs := nightlyOnlyErrors(map[string]string{"nightly-conformance.yml": wf}); len(errs) != 0 {
		t.Fatal(strings.Join(errs, "\n"))
	}
	if strings.Contains(wf, "A PULL REQUEST THAT CHANGES THIS FILE RUNS IT") {
		t.Fatal("stale pull-request trigger claim")
	}
	port := yamlJobs(wf)["port"]
	for _, token := range []string{
		"SHARED_FIXED_REFERENCE: build/nightly/cpp-corpus/build",
		"SHARED_FIXED_RECEIPT: build/nightly/report/fixedform-corpus-identity.json",
		"cp -R build/nightly/report/. build/nightly/port-writes/nightly-report/",
	} {
		if !strings.Contains(port, token) {
			t.Fatalf("missing shared artifact/report wiring: %s", token)
		}
	}
	step := func(name string) string {
		t.Helper()
		_, rest, ok := strings.Cut(port, "      - name: "+name+"\n")
		if !ok {
			t.Fatalf("missing step %q", name)
		}
		body, _, _ := strings.Cut(rest, "\n      - ")
		return body
	}
	overlay := step("read the C++ corpus")
	snapshot, copyAt, check := strings.Index(overlay, " snapshot "), strings.Index(overlay, "cp -R build/nightly/cpp-corpus/build/. build/"), strings.Index(overlay, "--phase overlay")
	if snapshot < 0 || copyAt <= snapshot || check <= copyAt {
		t.Fatal("reference snapshot must precede overlay, followed by identity verification")
	}
	for _, phase := range []struct{ step, name, command string }{
		{"build the ${{ matrix.lang }} driver and the nightly harness", "driver", "make ${{ matrix.targets }} ${{ matrix.env }} $extra"},
		{"the ${{ matrix.lang }} leg reads the C++ corpus (claims enforced)", "wire", "env ${{ matrix.env }} ./build/nightly-harness run"},
		{"the ${{ matrix.lang }} leg checks the four BOM entry cases", "bom", "env ${{ matrix.env }} make conformance-bom-entry"},
		{"the ${{ matrix.lang }} leg reads the C++ fixed form", "fixed", "if $gate; then"},
		{"the ${{ matrix.lang }} runtime readiness gate", "readiness", "if env ${{ matrix.env }} python3 test/conformance/nightly-readiness"},
	} {
		body := step(phase.step)
		before := strings.Index(body, "--phase "+phase.name+"-before")
		command := strings.Index(body, phase.command)
		after := strings.Index(body, "--phase "+phase.name+"-after")
		if before < 0 || command <= before || after <= command {
			t.Errorf("%s must retain its native command between identity checks", phase.name)
		}
		if phase.name == "fixed" || phase.name == "readiness" {
			if pass := strings.Index(body, "echo pass > \"$record\""); pass <= after {
				t.Errorf("%s records pass before artifact identity is established", phase.name)
			}
		}
	}
}
