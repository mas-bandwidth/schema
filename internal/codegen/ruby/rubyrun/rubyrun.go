// Package rubyrun runs the Ruby leg's pinned interpreters and analyzers
// (docs/PORT-RUBY.md, THE PIN and THE HARNESS).
//
// It never looks on PATH. A pin resolves from its environment variable ($RUBY,
// $RUBY_FLOOR, $RUBOCOP, $STEEP, $RBS; make/ruby-toolchain.mk exports the
// pins), else the repository's dist/ builds (make dist-ruby). A test that runs
// one is behind the slow gate (internal/slowtest); with the gate on, a pin
// that does not resolve FAILS the test and names make dist-ruby. It never
// skips there, because a leg that skips is a gate with no blade (issue #599).
//
// Every interpreter runs isolated: --disable-gems, and RUBYOPT, RUBYLIB,
// GEM_HOME and GEM_PATH removed from its environment, so no user gem,
// environment option or library path changes what runs. It runs with -w, and
// a warning on stderr fails Run as an error would: the leg's generated code
// and runtime have no warning under ruby -w.
package rubyrun

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// The pins make/ruby-toolchain.mk builds (docs/PORT-RUBY.md, THE PIN).
const (
	CurrentVersion = "4.0.9"
	FloorVersion   = "3.4.12"
)

// isolated is the environment variables an interpreter or analyzer the leg
// starts never sees.
var isolated = map[string]bool{
	"RUBYOPT":        true,
	"RUBYLIB":        true,
	"GEM_HOME":       true,
	"GEM_PATH":       true,
	"BUNDLE_GEMFILE": true,
}

// Interpreter returns the pinned current interpreter, or fails t.
func Interpreter(t testing.TB) string {
	t.Helper()
	return pinBin(t, "RUBY", CurrentVersion, "ruby")
}

// Floor returns the pinned floor interpreter, or fails t.
func Floor(t testing.TB) string {
	t.Helper()
	return pinBin(t, "RUBY_FLOOR", FloorVersion, "ruby")
}

// RuboCop returns the pinned RuboCop analyzer, or fails t.
func RuboCop(t testing.TB) string {
	t.Helper()
	return pinBin(t, "RUBOCOP", CurrentVersion, "rubocop")
}

// Steep returns the pinned Steep type checker, or fails t.
func Steep(t testing.TB) string {
	t.Helper()
	return pinBin(t, "STEEP", CurrentVersion, "steep")
}

// RBS returns the pinned RBS CLI, or fails t.
func RBS(t testing.TB) string {
	t.Helper()
	return pinBin(t, "RBS", CurrentVersion, "rbs")
}

func pinBin(t testing.TB, env, version, bin string) string {
	t.Helper()
	if !slowtest.Enabled() {
		t.Skipf("SCHEMA_SLOW: this test shells out to the pinned %s; set SCHEMA_SLOW=1 to run it (make test-ruby does)", env)
	}
	if p := os.Getenv(env); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s=%s does not resolve (%v): run make dist-ruby (docs/PORT-RUBY.md, THE PIN)", env, p, err)
		}
		return p
	}
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the pinned %s does not resolve: %v; run make dist-ruby (docs/PORT-RUBY.md, THE PIN)", env, err)
	}
	p := filepath.Join(root, "dist", "ruby-"+version, "bin", bin)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the pinned %s does not resolve: %s is absent and %s is unset; run make dist-ruby (docs/PORT-RUBY.md, THE PIN)", env, p, env)
	}
	return p
}

// RepoRoot walks up from the working directory to the directory holding the
// module's go.mod.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above the working directory")
		}
		dir = parent
	}
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !isolated[k] {
			env = append(env, kv)
		}
	}
	return env
}

// Run writes each input as the file dir/<name>, runs dir/main under the
// current pin with dir as the working directory, and returns stdout. A
// nonzero exit, or anything on stderr (a warning under -w), fails t with both
// streams.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, Interpreter(t), dir, main, nil, inputs)
}

// RunFloor is Run under the floor pin.
func RunFloor(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, Floor(t), dir, main, nil, inputs)
}

// RunArgs runs dir/main with extra arguments under the given pinned interpreter.
func RunArgs(t testing.TB, interp, dir, main string, args []string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, interp, dir, main, args, inputs)
}

func run(t testing.TB, interp, dir, main string, args []string, inputs map[string][]byte) []byte {
	t.Helper()
	stdout, stderr, err := TryArgs(t, interp, dir, main, args, inputs, nil)
	if err != nil || stderr != "" {
		t.Fatalf("%s %s: %v\nstderr:\n%s\nstdout:\n%s", interp, main, err, stderr, stdout)
	}
	return []byte(stdout)
}

// Try is Run for a probe that is EXPECTED to fail, under the interpreter interp
// names: a negative control sabotages what the probe reads and requires it to
// go red, so the failure is handed back with both streams.
func Try(t testing.TB, interp, dir, main string, inputs map[string][]byte) (stdout, stderr string, err error) {
	t.Helper()
	return TryArgs(t, interp, dir, main, nil, inputs, nil)
}

// TryArgs runs interp on main with args and optional stdin bytes.
func TryArgs(t testing.TB, interp, dir, main string, args []string, inputs map[string][]byte, stdin []byte) (stdout, stderr string, err error) {
	t.Helper()
	for name, data := range inputs {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if werr := os.WriteFile(p, data, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	cmdArgs := append([]string{"--disable-gems", "-w", main}, args...)
	cmd := testproc.Command(t, interp, cmdArgs...)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// Analyze runs RuboCop, RBS validation, and Steep in strict mode over the
// generated unit pkg inside dir. Any finding or warning fails t.
func Analyze(t testing.TB, dir string, pkgs ...string) {
	t.Helper()
	step, out, err := TryAnalyze(t, dir, pkgs...)
	if err != nil {
		t.Fatalf("analyzer %s failed (%v):\n%s", step, err, out)
	}
}

// TryAnalyze runs RuboCop, RBS validation, and Steep strict mode over pkgs in
// dir, returning the first failing step and its combined output.
func TryAnalyze(t testing.TB, dir string, pkgs ...string) (step, output string, err error) {
	t.Helper()
	rubocop := RuboCop(t)
	rbs := RBS(t)
	steep := Steep(t)
	root, rerr := RepoRoot()
	if rerr != nil {
		t.Fatal(rerr)
	}
	cfg := filepath.Join(root, "internal", "codegen", "ruby", "rubocop.yml")

	rcArgs := []string{"--config", cfg}
	var steepBody strings.Builder
	steepBody.WriteString("D = Steep::Diagnostic\n\ntarget :unit do\n  signature \".\"\n")
	for _, pkg := range pkgs {
		rcArgs = append(rcArgs, pkg+".rb", pkg)
		fmt.Fprintf(&steepBody, "  check %q\n  check %q\n", pkg+".rb", pkg)
		fmt.Fprintf(&steepBody, "  ignore %q\n  ignore %q\n", pkg+"/serialize.rb", pkg+"/serialize")
	}
	steepBody.WriteString("  configure_code_diagnostics(D::Ruby.strict)\nend\n")

	if out, cerr := runTool(t, dir, rubocop, rcArgs...); cerr != nil {
		return "rubocop", out, cerr
	}
	if out, cerr := runTool(t, dir, rbs, "-I", ".", "validate"); cerr != nil {
		return "rbs", out, cerr
	}
	if werr := os.WriteFile(filepath.Join(dir, "Steepfile"), []byte(steepBody.String()), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if out, cerr := runTool(t, dir, steep, "check", "--jobs=1"); cerr != nil {
		return "steep", out, cerr
	}
	return "", "", nil
}

func runTool(t testing.TB, dir, bin string, args ...string) (string, error) {
	cmd := testproc.Command(t, bin, args...)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// HexLines decodes stdout as one lowercase hex string per line.
func HexLines(t testing.TB, stdout []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		b, err := hex.DecodeString(line)
		if err != nil {
			t.Fatalf("probe line %q is not hex: %v", line, err)
		}
		out = append(out, b)
	}
	return out
}

// RuboCopAll runs the pinned RuboCop, with the generator's rubocop.yml, once
// over every path in dir: the strict gate's pull-request form lints all its
// units in one process instead of one process per unit.
func RuboCopAll(t testing.TB, dir string, paths ...string) (string, error) {
	t.Helper()
	root, rerr := RepoRoot()
	if rerr != nil {
		t.Fatal(rerr)
	}
	args := append([]string{"--config", filepath.Join(root, "internal", "codegen", "ruby", "rubocop.yml")}, paths...)
	return runTool(t, dir, RuboCop(t), args...)
}
