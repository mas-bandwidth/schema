package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Run the workflow's actual packaging step with the two already approved
// positive pins. Different source permissions identify which copy was packed;
// no table parser or native reader is invoked here.
func TestCppPositiveFeatureCorpusPack(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("nightly corpus packaging runs in Bash on Linux")
	}
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/nightly-conformance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, step, ok := strings.Cut(string(raw), "      - name: pack the C++ corpus\n")
	if !ok {
		t.Fatal("pack step is missing")
	}
	_, step, ok = strings.Cut(step, "        run: |\n")
	if !ok {
		t.Fatal("pack script is missing")
	}
	var lines []string
	for line := range strings.SplitSeq(step, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "          "))
	}
	if len(lines) == 0 {
		t.Fatal("pack script is empty")
	}
	dir := t.TempDir()
	pinDir := filepath.Join(dir, "testdata/conformance/feature-witnesses/full")
	outDir := filepath.Join(dir, "build/feature-witness-cpp/outputs")
	for _, d := range []string{pinDir, outDir, filepath.Join(dir, "testdata/wire")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	originals := make(map[string][]byte)
	for _, name := range []string{"Root.bin", "ArrayRoot.bin"} {
		data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/feature-witnesses/full", name))
		if err != nil {
			t.Fatal(err)
		}
		originals[name] = data
		for _, item := range []struct {
			path string
			mode os.FileMode
		}{{filepath.Join(pinDir, name), 0o644}, {filepath.Join(outDir, name), 0o600}} {
			if err := os.WriteFile(item.path, data, item.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(item.path, item.mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("bash", "-e", "-c", strings.Join(lines, "\n"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FEATURE_OUTCOME=success")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pack step: %v\n%s", err, out)
	}
	for _, name := range []string{"Root.bin", "ArrayRoot.bin"} {
		original, err := os.ReadFile(filepath.Join(root, "testdata/conformance/feature-witnesses/full", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(original, originals[name]) {
			t.Fatalf("committed pin %s changed", name)
		}
		for _, path := range []string{filepath.Join(pinDir, name), filepath.Join(outDir, name), filepath.Join(dir, "build/nightly/cpp-corpus/testdata/conformance/feature-witnesses/full", name)} {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("%s: contents changed or missing: %v", path, err)
			}
		}
		packed := filepath.Join(dir, "build/nightly/cpp-corpus/testdata/conformance/feature-witnesses/full", name)
		info, err := os.Stat(packed)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: artifact did not take output file mode: %v", packed, err)
		}
	}
}
