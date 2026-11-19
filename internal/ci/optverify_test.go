package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var allOptVerifyLangs = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "goose", "java", "js",
	"lobster", "lua", "luau", "moo", "python", "ruby", "rust", "squirrel", "unity",
}

var compiledOptVerifyLangs = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "java", "rust", "unity",
}

var vmOptVerifyLangs = []string{
	"goose", "js", "lobster", "lua", "luau", "moo", "python", "ruby", "squirrel",
}

// TestOptVerifyCoversEveryPort verifies that the 9 compiled legs in
// make/verify-compiled.mk and the 9 VM legs in make/verify-vm.mk partition all
// 18 language ports in make/*.mk without overlap or omission, and define
// verify-<lang>, verify-<lang>-conformance, verify-<lang>-bench, and
// verify-<lang>-fuzz for every language.
func TestOptVerifyCoversEveryPort(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	// 1. Discover SCHEMA_PORTS from non-dashed make/<lang>.mk files.
	matches, err := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatalf("glob make/*.mk: %v", err)
	}
	var ports []string
	for _, m := range matches {
		stem := strings.TrimSuffix(filepath.Base(m), ".mk")
		if !strings.Contains(stem, "-") {
			ports = append(ports, stem)
		}
	}
	sort.Strings(ports)
	if !slices.Equal(ports, allOptVerifyLangs) {
		t.Fatalf("discovered SCHEMA_PORTS = %v; want %v", ports, allOptVerifyLangs)
	}

	// 2. Verify compiled + VM leg lists partition all 18 languages cleanly.
	compiledBytes, err := os.ReadFile(filepath.Join(root, "make", "verify-compiled.mk"))
	if err != nil {
		t.Fatalf("read make/verify-compiled.mk: %v", err)
	}
	vmBytes, err := os.ReadFile(filepath.Join(root, "make", "verify-vm.mk"))
	if err != nil {
		t.Fatalf("read make/verify-vm.mk: %v", err)
	}
	compiledText := string(compiledBytes)
	vmText := string(vmBytes)

	gotCompiled := parseMakeWordList(t, compiledText, "VERIFY_COMPILED_LEGS")
	gotVM := parseMakeWordList(t, vmText, "VERIFY_VM_LEGS")
	sort.Strings(gotCompiled)
	sort.Strings(gotVM)
	if !slices.Equal(gotCompiled, compiledOptVerifyLangs) {
		t.Errorf("VERIFY_COMPILED_LEGS = %v; want %v", gotCompiled, compiledOptVerifyLangs)
	}
	if !slices.Equal(gotVM, vmOptVerifyLangs) {
		t.Errorf("VERIFY_VM_LEGS = %v; want %v", gotVM, vmOptVerifyLangs)
	}

	// 3. Verify macro entrypoints and per-language sub-target definitions.
	for _, entry := range []string{
		"verify-$(1):",
		"verify-$(1)-conformance:",
		"verify-$(1)-bench:",
		"verify-$(1)-fuzz:",
		"_verify-$(1)-fuzz:",
	} {
		if !strings.Contains(compiledText, entry) {
			t.Errorf("make/verify-compiled.mk missing template rule %q", entry)
		}
	}
	for _, entry := range []string{
		"verify-$(1):",
		"verify-$(1)-conformance:",
		"verify-$(1)-bench:",
		"verify-$(1)-fuzz:",
	} {
		if !strings.Contains(vmText, entry) {
			t.Errorf("make/verify-vm.mk missing template rule %q", entry)
		}
	}

	for _, lang := range compiledOptVerifyLangs {
		for _, sub := range []string{"conformance", "bench"} {
			needle := fmt.Sprintf("_verify-%s-%s:", lang, sub)
			if !strings.Contains(compiledText, needle) {
				t.Errorf("make/verify-compiled.mk missing rule %q", needle)
			}
		}
	}
	for _, lang := range vmOptVerifyLangs {
		for _, sub := range []string{"conformance", "bench", "fuzz"} {
			needle := fmt.Sprintf("_verify-%s-%s:", lang, sub)
			if !strings.Contains(vmText, needle) {
				t.Errorf("make/verify-vm.mk missing rule %q", needle)
			}
		}
	}

	// 4. Verify sabotage negative control is wired and registered.
	if !strings.Contains(vmText, "verify-sabotage-negative-control:") {
		t.Errorf("make/verify-vm.mk missing verify-sabotage-negative-control target")
	}
	ncBytes, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.d", "verify-negative-controls.json"))
	if err != nil {
		t.Fatalf("read make/negative-controls.d/verify-negative-controls.json: %v", err)
	}
	if !strings.Contains(string(ncBytes), "verify-sabotage-negative-control") {
		t.Errorf("verify-negative-controls.json does not register verify-sabotage-negative-control")
	}
}

// TestOptVerifyFastWorkflowSelector checks that .github/workflows/ci-fast.yml
// maps every language in its `add_leg <lang>` path selector once all 18 legs
// are wired into ci-fast.yml.
func TestOptVerifyFastWorkflowSelector(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	wfBytes, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatalf("read ci-fast.yml: %v", err)
	}
	wf := string(wfBytes)
	if !strings.Contains(wf, "verify-") {
		t.Skip("ci-fast.yml does not reference verify-<lang> yet; enforcing selector once wired")
	}

	addLegRe := regexp.MustCompile(`\badd_leg\s+([a-z0-9]+)\b`)
	seen := map[string]bool{}
	for _, m := range addLegRe.FindAllStringSubmatch(wf, -1) {
		seen[m[1]] = true
	}
	for _, lang := range allOptVerifyLangs {
		if !seen[lang] {
			t.Errorf(".github/workflows/ci-fast.yml has no `add_leg %s` selector rule", lang)
		}
	}
}

func parseMakeWordList(t *testing.T, text, varName string) []string {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, varName) {
			if _, rhs, ok := strings.Cut(trimmed, ":="); ok {
				return strings.Fields(rhs)
			}
			if _, rhs, ok := strings.Cut(trimmed, "="); ok {
				return strings.Fields(rhs)
			}
		}
	}
	t.Fatalf("variable %s not found", varName)
	return nil
}
