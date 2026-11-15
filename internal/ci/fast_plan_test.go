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
		{"shared schema", "test/tables/VNEW_floor.schema", "yes|cpp go c rust js cs java dart elixir goose lobster luau squirrel"},
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

// TestFastPlanCoversStreamToDevAndSelectsRelevantWork verifies that ci-fast.yml's
// plan job diffs against the PR's base branch on pull_request (so stream-to-dev
// PRs diff against origin/dev and main promotion PRs diff against origin/main),
// defaults to dev on workflow_dispatch, and selects only relevant packages and
// legs without broadening a single-leg change into the full suite.
func TestFastPlanCoversStreamToDevAndSelectsRelevantWork(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)

	const wantBaseLine = "BASE: ${{ github.event_name == 'pull_request' && github.event.pull_request.base.ref || 'dev' }}"
	if !strings.Contains(wf, wantBaseLine) {
		t.Fatalf("ci-fast.yml plan does not set %q", wantBaseLine)
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
	script.WriteString(`printf '%s|%s|%s' "$broad" "$packages" "$json"`)

	for _, tc := range []struct {
		name  string
		files string
		want  string
	}{
		{
			name:  "elixir leg only",
			files: "internal/codegen/elixirtable/wire.go",
			want:  `no|./internal/codegen/elixirtable ./internal/goldens|["elixir"]`,
		},
		{
			name:  "luau leg only",
			files: "internal/codegen/luautable/fixedlineage.go",
			want:  `no|./internal/codegen/luautable ./internal/goldens|["luau"]`,
		},
		{
			name:  "goose leg only",
			files: "internal/codegen/goosetable/fixedform.go",
			want:  `no|./internal/codegen/goosetable ./internal/goldens|["goose"]`,
		},
		{
			// the Squirrel leg row runs make targets; its two unit packages ride the make-file rule
			name:  "squirrel card leg only",
			files: "make/squirrel-cards.mk",
			want:  `no|./internal/codegen/squirrel ./internal/codegen/squirreltable|["squirrel"]`,
		},
		{
			name:  "dart leg only",
			files: "internal/codegen/darttable/darttable.go",
			want:  `no|./internal/codegen/darttable ./internal/goldens|["dart"]`,
		},
		{
			name:  "ci package only",
			files: "internal/ci/ci_test.go",
			want:  `no|./internal/ci|[]`,
		},
		{
			name:  "docs only",
			files: "docs/CONTRIBUTING.md",
			want:  `no||[]`,
		},
		{
			name:  "broad compiler change",
			files: "compiler/compiler.go",
			want:  `yes|./compiler|["c","cpp","cs","dart","elixir","go","goose","java","js","lobster","luau","rust","squirrel"]`,
		},
		{
			name:  "lobster leg only",
			files: "internal/codegen/lobstertable/load.go",
			want:  `no|./internal/codegen/lobstertable ./internal/codegen/lobster ./internal/goldens|["lobster"]`,
		},
		{
			// a golden alone selects no leg, and still re-checks the goldens
			name:  "golden only",
			files: "testdata/golden/java/Types.java",
			want:  `no|./internal/goldens|[]`,
		},
		{
			// an emitter alone re-checks the goldens it writes
			name:  "java emitter only",
			files: "internal/codegen/java/canonical_test.go",
			want:  `no|./internal/codegen/java ./internal/codegen/javatable ./internal/goldens|["java"]`,
		},
		{
			name:  "usage page",
			files: "docs/USAGE.md",
			want:  `no|./internal/goldens|[]`,
		},
		{
			// a card's targets live in their own make/<lang>-<topic>.mk,
			// which selects its leg as make/<lang>.mk does
			name:  "elixir card make file",
			files: "make/elixir-packet-write-text.mk",
			want:  `no|./internal/codegen/elixirtable|["elixir"]`,
		},
		{
			name:  "c card make file",
			files: "make/c-packet-write-text.mk",
			want:  `no|./internal/codegen/ctable|["c"]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
			cmd.Env = append(scriptEnv(os.Environ()), "files="+tc.files)
			cmd.Dir = repoRoot(t)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan script: %v: %s", err, out)
			}
			got := selectorOutput(out)
			if got != tc.want {
				t.Fatalf("files=%q produced %q; want %q", tc.files, got, tc.want)
			}
		})
	}
}

// TestFastPlanSelectsLanguageLints runs the plan script and holds the
// per-language lint selection (the owner, 2026-11-15: "run only the fastest unit
// tests as verification, on a per-language basis"): the lens a touched
// language owns, and only that one, rides the pull request as a lang-lint row,
// and a broad diff selects all three.
func TestFastPlanSelectsLanguageLints(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
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
	script.WriteString(`printf '%s|%s' "$lints_json" "$packages"`)
	for _, tc := range []struct{ name, files, want string }{
		{"luau only", "internal/codegen/luautable/fixedlineage.go", `[]|./internal/codegen/luautable ./internal/goldens`},
		{"dart leg", "internal/codegen/darttable/darttable.go", `["dart"]|./internal/codegen/darttable ./internal/goldens`},
		{"go leg", "internal/codegen/gotable/wire.go", `["go"]|./internal/codegen/gotable ./internal/goldens`},
		{"lua runtime, no .go file", "internal/codegen/luatable/runtime/wire.lua", `["lua"]|./internal/codegen/lua ./internal/codegen/luatable ./internal/goldens`},
		{"lua make file", "make/lua.mk", `["lua"]|./internal/codegen/lua ./internal/codegen/luatable`},
		{"dart lens make file", "make/schema-dart-generated-strict-analyze-gate.mk", `["dart"]|`},
		{"docs only", "docs/CONTRIBUTING.md", `[]|`},
		{"broad", "compiler/compiler.go", `["dart","go","lua"]|./compiler`},
		// the shared packet corpora are every lens's input
		{"packet examples", "examples/Types.schema", `["dart","go","lua"]|./internal/goldens`},
		{"128-bit examples", "examples128/Ludicrous.schema", `["dart","go","lua"]|./internal/goldens`},
		{"bench corpus", "bench/corpus/Bench.schema", `["dart","go","lua"]|`},
		{"guard unit", "test/guard/second.schema", `["dart","go","lua"]|`},
		{"guard non-schema", "test/guard/README.md", `[]|`},
		// a runtime-only (non-Go) diff runs its language's two unit packages
		{"c runtime", "internal/codegen/ctable/testdata/runtime.h", `[]|./internal/codegen/c ./internal/codegen/ctable ./internal/goldens`},
		{"cs runtime", "internal/codegen/cstable/testdata/Runtime.cs", `[]|./internal/codegen/csharp ./internal/codegen/cstable ./internal/goldens`},
		{"go runtime", "internal/codegen/gotable/testdata/runtime.go.txt", `["go"]|./internal/codegen/golang ./internal/codegen/gotable ./internal/goldens`},
		{"rust runtime", "internal/codegen/rusttable/testdata/runtime.rs", `[]|./internal/codegen/rust ./internal/codegen/rusttable ./internal/goldens`},
		{"js runtime", "internal/codegen/js/testdata/runtime.js", `[]|./internal/codegen/js ./internal/codegen/jstable ./internal/goldens`},
		{"java runtime", "internal/codegen/javatable/testdata/Runtime.java", `[]|./internal/codegen/java ./internal/codegen/javatable ./internal/goldens`},
		{"dart runtime", "internal/codegen/darttable/testdata/runtime.dart", `["dart"]|./internal/codegen/dart ./internal/codegen/darttable ./internal/goldens`},
		{"elixir runtime", "internal/codegen/elixirtable/testdata/runtime.ex", `[]|./internal/codegen/elixir ./internal/codegen/elixirtable ./internal/goldens`},
		{"luau runtime", "internal/codegen/luau/runtime/serialize_bytes.luau", `[]|./internal/codegen/luau ./internal/codegen/luautable ./internal/goldens`},
		{"lobster runtime", "internal/codegen/lobster/runtime/serialize_compressed_float.lobster", `[]|./internal/codegen/lobster ./internal/codegen/lobstertable ./internal/goldens`},
		{"goose runtime", "internal/codegen/goosetable/runtime/schema_json.goose", `[]|./internal/codegen/goose ./internal/codegen/goosetable ./internal/goldens`},
		{"squirrel runtime", "internal/codegen/squirrel/runtime/module.nut", `[]|./internal/codegen/squirrel ./internal/codegen/squirreltable ./internal/goldens`},
		{"squirrel table runtime", "internal/codegen/squirreltable/runtime/cook.nut", `[]|./internal/codegen/squirrel ./internal/codegen/squirreltable ./internal/goldens`},
		{"lua type runtime", "internal/codegen/lua/runtime/serialize_int_relative.lua", `["lua"]|./internal/codegen/lua ./internal/codegen/luatable ./internal/goldens`},
		{"moo runtime", "internal/codegen/moo/runtime/serialize_float.moo", `[]|./internal/codegen/moo ./internal/codegen/mootable ./internal/goldens`},
		{"moo table runtime", "internal/codegen/mootable/runtime/json.moo", `[]|./internal/codegen/moo ./internal/codegen/mootable ./internal/goldens`},
		{"moo make file", "make/moo.mk", `[]|./internal/codegen/moo ./internal/codegen/mootable`},
		{"squirrel make file", "make/squirrel.mk", `[]|./internal/codegen/squirrel ./internal/codegen/squirreltable`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
			cmd.Env = append(scriptEnv(os.Environ()), "files="+tc.files)
			cmd.Dir = repoRoot(t)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan script: %v: %s", err, out)
			}
			if got := selectorOutput(out); got != tc.want {
				t.Fatalf("files=%q produced %q; want %q", tc.files, got, tc.want)
			}
		})
	}
}
