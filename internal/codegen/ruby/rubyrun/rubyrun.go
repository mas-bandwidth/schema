// Package rubyrun runs generated Ruby under the pinned interpreters for the
// Ruby leg's Go tests (docs/PORT-RUBY.md, THE HARNESS). It is a TEST HELPER:
// it writes a probe's inputs as files into the test's own directory, runs one
// interpreter process there and hands back its stdout. No network, no clock,
// no service: the interpreter reads files in dir and writes to stdout.
//
// The interpreters are $RUBY and $RUBY_FLOOR when set (make/ruby-toolchain.mk
// exports the pins), else the repository's dist/ builds (make dist-ruby). A
// test that runs one is behind the slow gate (internal/slowtest); with the
// gate on, a pin that does not resolve FAILS the test and names make
// dist-ruby. It never skips there, because a leg that skips is a gate with no
// blade (issue #599).
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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The pins make/ruby-toolchain.mk builds (docs/PORT-RUBY.md, THE PIN).
const (
	CurrentVersion = "4.0.9"
	FloorVersion   = "3.4.12"
)

// isolated is the environment variables an interpreter the leg starts never
// sees.
var isolated = map[string]bool{"RUBYOPT": true, "RUBYLIB": true, "GEM_HOME": true, "GEM_PATH": true}

// Interpreter returns the pinned current interpreter, or fails t.
func Interpreter(t testing.TB) string {
	t.Helper()
	return pin(t, "RUBY", CurrentVersion)
}

// Floor returns the pinned floor interpreter, or fails t.
func Floor(t testing.TB) string {
	t.Helper()
	return pin(t, "RUBY_FLOOR", FloorVersion)
}

func pin(t testing.TB, env, version string) string {
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
	p := filepath.Join(root, "dist", "ruby-"+version, "bin", "ruby")
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

// Run writes each input as the file dir/<name>, runs dir/main under the
// current pin with dir as the working directory, and returns stdout. A
// nonzero exit, or anything on stderr (a warning under -w), fails t with both
// streams.
func Run(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, Interpreter(t), dir, main, inputs)
}

// RunFloor is Run under the floor pin.
func RunFloor(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	return run(t, Floor(t), dir, main, inputs)
}

func run(t testing.TB, ruby, dir, main string, inputs map[string][]byte) []byte {
	t.Helper()
	stdout, stderr, err := Try(t, ruby, dir, main, inputs)
	if err != nil || stderr != "" {
		t.Fatalf("%s %s: %v\nstderr:\n%s\nstdout:\n%s", ruby, main, err, stderr, stdout)
	}
	return []byte(stdout)
}

// Try is Run for a probe that is EXPECTED to fail, under the interpreter ruby
// names: a negative control sabotages what the probe reads and requires it to
// go red, so the failure is handed back with both streams.
func Try(t testing.TB, ruby, dir, main string, inputs map[string][]byte) (stdout, stderr string, err error) {
	t.Helper()
	for name, data := range inputs {
		if werr := os.WriteFile(filepath.Join(dir, name), data, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	cmd := exec.Command(ruby, "--disable-gems", "-w", main)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !isolated[k] {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
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
