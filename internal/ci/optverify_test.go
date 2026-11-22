package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
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
	matches, err := provesran.Glob(filepath.Join(root, "make", "*.mk"))
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
		cardsTarget := fmt.Sprintf("cards-%s-conformance", lang)
		if !strings.Contains(compiledText, cardsTarget) {
			t.Errorf("make/verify-compiled.mk does not wire %q for compiled leg %s", cardsTarget, lang)
		}
	}
	for _, required := range []string{
		"ludicrous",
		"packet-defaults-rust",
		"packet-end-rust",
		"packet-write-cfloat-rust",
		"packet-write-text-rust",
		"tables-rust-view",
		"tables-view",
		"tables-c-view",
		"tables-cs-view",
		"tables-go-view",
		"tables-java-view",
		"tables-dart-view",
		"tables-elixir-view",
		"TestUnityVTReflectionDocTagsAndUnitView",
	} {
		if !strings.Contains(compiledText, required) {
			t.Errorf("make/verify-compiled.mk missing widened compiled check %q", required)
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
	if !strings.Contains(string(ncBytes), "verify-json-fuzz-negative-control") {
		t.Errorf("verify-negative-controls.json does not register verify-json-fuzz-negative-control")
	}
	if !strings.Contains(compiledText, "json-fuzz --only") {
		t.Errorf("make/verify-compiled.mk missing json-fuzz wiring")
	}
	if !strings.Contains(vmText, "json-fuzz --only") {
		t.Errorf("make/verify-vm.mk missing json-fuzz wiring")
	}
}

// TestOptVerifyFastWorkflowSelector checks that .github/workflows/ci-fast.yml
// maps every language in its `add_leg <lang>` path selector and executes the
// bounded verify-leg matrix across all 18 ports.
func TestOptVerifyFastWorkflowSelector(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	wfBytes, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatalf("read ci-fast.yml: %v", err)
	}
	wf := string(wfBytes)
	if !strings.Contains(wf, "  verify-leg:\n") {
		t.Fatal("ci-fast.yml missing verify-leg job")
	}
	if !strings.Contains(wf, "gate: [conformance, bench, fuzz]") {
		t.Fatal("ci-fast.yml verify-leg missing gate: [conformance, bench, fuzz] matrix")
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

	_, tail, ok := strings.Cut(wf, "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          echo \"broad=$broad\"")
	if !ok {
		t.Fatal("fast workflow output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s' "$verify_legs_json"`)

	cases := []struct {
		name  string
		files string
		want  string
	}{
		{"c", "internal/codegen/ctable/emit.go", `["c"]`},
		{"cpp", "internal/codegen/cpptable/emit.go", `["cpp"]`},
		{"ccommon", "internal/codegen/ccommon/emit.go", `["c","cpp"]`},
		{"cs", "internal/codegen/cstable/emit.go", `["cs"]`},
		{"unity", "internal/codegen/unitytable/emit.go", `["unity"]`},
		{"go", "internal/codegen/gotable/wire.go", `["go"]`},
		{"rust", "internal/codegen/rusttable/emit.go", `["rust"]`},
		{"java", "internal/codegen/javatable/retain.go", `["java"]`},
		{"js", "internal/codegen/jstable/variableform.go", `["js"]`},
		{"dart", "internal/codegen/darttable/darttable.go", `["dart"]`},
		{"elixir", "internal/codegen/elixirtable/variableform.go", `["elixir"]`},
		{"goose", "internal/codegen/goosetable/fixedform.go", `["goose"]`},
		{"luau", "internal/codegen/luautable/fixedlineage.go", `["luau"]`},
		{"lua", "internal/codegen/luatable/runtime/wire.lua", `["lua"]`},
		{"squirrel", "internal/codegen/squirreltable/runtime/cook.nut", `["squirrel"]`},
		{"moo", "internal/codegen/mootable/runtime/json.moo", `["moo"]`},
		{"lobster", "internal/codegen/lobstertable/load.go", `["lobster"]`},
		{"python", "internal/codegen/pythontable/emit.go", `["python"]`},
		{"ruby", "internal/codegen/rubytable/emit.go", `["ruby"]`},
		// a shared edit gets every leg and a wide sweep every leg it
		// touches: no cap (2026-11-22), a skip is green only with no reader
		{"shared compiler", "compiler/compiler.go", `["c","cpp","cs","dart","elixir","go","goose","java","js","lobster","lua","luau","moo","python","ruby","rust","squirrel","unity"]`},
		{"shared Makefile", "Makefile", `["c","cpp","cs","dart","elixir","go","goose","java","js","lobster","lua","luau","moo","python","ruby","rust","squirrel","unity"]`},
		{"wide sweep uncapped", "generated/c/x\ngenerated/cs/x\ngenerated/go/x\ngenerated/js/x\ngenerated/lua/x", `["c","cs","go","js","lua"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // each case is its own bash over a read-only script
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
			cmd.Env = append(scriptEnv(os.Environ()), "files="+tc.files)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan script: %v: %s", err, out)
			}
			if got := selectorOutput(out); got != tc.want {
				t.Fatalf("files=%q selected verify_legs=%q; want %q", tc.files, got, tc.want)
			}
		})
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
