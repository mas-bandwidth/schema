package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPythonPinsAreRegisteredAndNeverFromPath(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mkPath := filepath.Join(root, "make", "python-toolchain.mk")
	mkBytes, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("read make/python-toolchain.mk: %v", err)
	}
	mk := string(mkBytes)

	if !regexp.MustCompile(`(?m)^TOOLCHAIN_LEGS\s*\+=\s*python\s*$`).MatchString(mk) {
		t.Error("make/python-toolchain.mk does not append python to TOOLCHAIN_LEGS")
	}

	pinsMatch := regexp.MustCompile(`(?m)^TOOLCHAIN_PINS_python\s*:=\s*(.+)$`).FindStringSubmatch(mk)
	if pinsMatch == nil {
		t.Fatal("make/python-toolchain.mk does not define TOOLCHAIN_PINS_python")
	}
	pinsFields := strings.Fields(pinsMatch[1])
	wantPins := []string{"PYTHON", "PYTHON_FLOOR", "MYPY"}
	if strings.Join(pinsFields, " ") != strings.Join(wantPins, " ") {
		t.Errorf("TOOLCHAIN_PINS_python = %v, want %v", pinsFields, wantPins)
	}
	for _, pin := range wantPins {
		probe := `$(call toolchain_probe,python,` + pin + `,`
		if !strings.Contains(mk, probe) {
			t.Errorf("make/python-toolchain.mk does not probe %s via %s", pin, probe)
		}
	}

	shaRE := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, verVar := range []string{"PYTHON_FLOOR_VERSION", "PYTHON_CURRENT_VERSION"} {
		vm := regexp.MustCompile(`(?m)^` + verVar + `\s*:=\s*(\S+)\s*$`).FindStringSubmatch(mk)
		if vm == nil {
			t.Fatalf("make/python-toolchain.mk does not define %s", verVar)
		}
		ver := vm[1]
		sm := regexp.MustCompile(`(?m)^PYTHON_SHA256_` + regexp.QuoteMeta(ver) + `\s*:=\s*(\S+)\s*$`).FindStringSubmatch(mk)
		if sm == nil {
			t.Errorf("make/python-toolchain.mk is missing PYTHON_SHA256_%s", ver)
			continue
		}
		if !shaRE.MatchString(sm[1]) {
			t.Errorf("PYTHON_SHA256_%s = %q is not a 64-hex sha256 digest", ver, sm[1])
		}
	}

	if !strings.Contains(mk, "--require-hashes") || !strings.Contains(mk, "--no-index") {
		t.Error("make/python-toolchain.mk must install mypy offline with --no-index and --require-hashes")
	}

	lockPath := filepath.Join(root, "make", "python-mypy.lock")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read make/python-mypy.lock: %v", err)
	}
	lockLines := strings.Split(strings.TrimSpace(string(lockBytes)), "\n")
	reqCount := 0
	hashLineRE := regexp.MustCompile(`^[A-Za-z0-9_.-]+==[0-9A-Za-z_.-]+\s+--hash=sha256:[0-9a-f]{64}$`)
	for i, line := range lockLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		reqCount++
		if !hashLineRE.MatchString(trimmed) {
			t.Errorf("make/python-mypy.lock:%d %q is not pinned with ==version and --hash=sha256:<64hex>", i+1, trimmed)
		}
	}
	if reqCount == 0 {
		t.Error("make/python-mypy.lock contains no hash-locked requirement lines")
	}

	for _, rel := range []string{
		".github/actions/python/action.yml",
		".github/workflows/python-leg.yml",
		"make/python-toolchain-check.sh",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	var legFiles []string
	for _, pattern := range []string{
		"make/python*.mk",
		"make/python*.sh",
		"internal/codegen/python/*.go",
		"internal/codegen/pythontable/*.go",
		"test/conformance/python/*",
		"compiler/*python*.go",
	} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		legFiles = append(legFiles, matches...)
	}
	if len(legFiles) == 0 {
		t.Fatal("found no make/python* or Python leg files to check")
	}

	barePython3RE := regexp.MustCompile(`(^|[^A-Za-z0-9_./$-])python3([^A-Za-z0-9_.-]|$)`)
	envPythonRE := regexp.MustCompile(`/usr/bin/env\s+python`)
	execPythonRE := regexp.MustCompile(`(Command|LookPath)\("python`)

	for _, f := range legFiles {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.ToSlash(rel) == "make/python-toolchain-check.sh" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if barePython3RE.MatchString(line) || envPythonRE.MatchString(line) || execPythonRE.MatchString(line) {
				t.Errorf("%s:%d runs an interpreter from PATH: %s", rel, i+1, trimmed)
			}
		}
	}
}
