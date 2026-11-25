package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
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
			testinputs.Files(t, ".github/workflows/ci-full.yml")
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
