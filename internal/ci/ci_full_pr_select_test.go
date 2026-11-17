package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// prSelectScript returns ci-full.yml pr-select's selector, from its
// `# THE PR SELECTION.` marker to the output line, with a printf of the verdict.
func prSelectScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE PR SELECTION.")
	if !ok {
		t.Fatal("ci-full.yml pr-select selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          printf 'pins=%s")
	if !ok {
		t.Fatal("ci-full.yml pr-select output boundary not found")
	}
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s|%s|%s' "$broad" "$pins" "$cs"`)
	return script.String()
}

// TestCIFullPRSelect runs pr-select's own script (the owner, 2026-11-15: heavy
// suite failure on main is fine, like a hierarchical merge). An unlabelled
// pull request runs `pins` only on a diff that touches what the pins protect,
// and `cs-tables` only on the C# leg's paths or a shared root; a one-language
// card on another leg runs neither.
func TestCIFullPRSelect(t *testing.T) {
	t.Parallel()
	script := prSelectScript(t)
	for _, tc := range []struct{ name, files, want string }{
		{"luau generator", "internal/codegen/luautable/fixedlineage.go", "no|no|no"},
		{"luau golden", "testdata/golden/luau/Types.luau", "no|no|no"},
		{"goose runtime", "internal/codegen/goosetable/runtime/wire_retain.goose", "no|no|no"},
		{"ci tests", "internal/ci/ci_test.go", "no|no|no"},
		{"docs", "docs/CONTRIBUTING.md", "no|no|no"},
		{"c emitter", "internal/codegen/ctable/emit.go", "no|yes|no"},
		{"cs emitter", "internal/codegen/cstable/emit.go", "no|yes|yes"},
		{"cs leg test tree", "test/cs-tables/Program.cs", "no|no|yes"},
		{"cs make file", "make/cs.mk", "no|yes|yes"},
		{"cs card make file", "make/cs-packet-write-text.mk", "no|yes|yes"},
		{"wire pin", "testdata/wire/tables/examples.bin", "no|yes|no"},
		{"table golden", "testdata/golden/tables/maps/MapsTable.h", "no|yes|no"},
		{"conformance harness", "test/conformance/harness/main.go", "no|yes|no"},
		{"engine package", "internal/tablewire/decode.go", "no|yes|no"},
		{"compiler root", "compiler/compiler.go", "yes|yes|yes"},
		{"this workflow", ".github/workflows/ci-full.yml", "yes|yes|yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
			cmd.Env = append(scriptEnv(os.Environ()), "files="+tc.files)
			cmd.Dir = repoRoot(t)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("pr-select script: %v: %s", err, out)
			}
			if got := selectorOutput(out); got != tc.want {
				t.Fatalf("files=%q selected broad|pins|cs = %q; want %q", tc.files, got, tc.want)
			}
		})
	}
}

// TestCIFullPRSelectMatchesTheFastPlan holds pr-select's shared roots and C#
// paths to ci-fast.yml plan's, so the C# tables leg is selected on exactly
// the diffs that select the C# leg there.
func TestCIFullPRSelectMatchesTheFastPlan(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	fast, full := read("ci-fast.yml"), read("ci-full.yml")
	broadRE := regexp.MustCompile(`grep -q?E '\^\((ir/\|compiler/[^']*)\)'`)
	fb, ub := broadRE.FindStringSubmatch(fast), broadRE.FindStringSubmatch(full)
	if fb == nil || ub == nil {
		t.Fatal("the broad rule was not found in ci-fast.yml plan or ci-full.yml pr-select")
	}
	if want := strings.Replace(fb[1], `ci-fast\.yml`, `ci-full\.yml`, 1); ub[1] != want {
		t.Errorf("ci-full.yml pr-select's shared roots\n  %s\ndiffer from ci-fast.yml plan's (this file in place of that one)\n  %s", ub[1], want)
	}
	csCase := regexp.MustCompile(`(?m)^\s+(internal/codegen/cstable/[^)]*)\) add_leg cs ;;`).FindStringSubmatch(fast)
	if csCase == nil {
		t.Fatal("ci-fast.yml plan's cs row was not found")
	}
	var want []string
	for p := range strings.SplitSeq(csCase[1], "|") {
		p = strings.TrimSuffix(p, "*")
		p = strings.ReplaceAll(p, ".", `\.`)
		p = strings.ReplaceAll(p, "*", "[^/]*") // an inner glob star
		if !strings.HasSuffix(p, "/") {
			p += "$"
		}
		want = append(want, p)
	}
	if !strings.Contains(full, "'^("+strings.Join(want, "|")+")'") {
		t.Errorf("ci-full.yml pr-select's C# paths are not ci-fast.yml plan's cs row %q (want the alternation %q)", csCase[1], strings.Join(want, "|"))
	}
}
