//go:build !windows

package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestFormatGatesHoldTheCPULimit drives both format engines,
// make/checks/c-clean-format.sh (tables-c-clean) and cpp-clean-format.sh
// (tables-cpp-format), with a stand-in formatter, and holds them to the
// per-file limit: clang-format runs under `ulimit -t $FILE_SECONDS`
// (CLANG_FORMAT_FILE_SECONDS, make/c.mk), and a file it cannot finish inside
// that is red by name whatever its drift.
//
// One generated header took clang-format 41-50 s, and the lint gates that
// format it could not fit their minute; nothing said which file until a
// bisect found it. No test here spends real time: the stand-in reads the
// limit it was given back out of `ulimit -t`, and stands for a file over the
// limit by sending itself the signal the kernel sends at it (SIGXCPU).
func TestFormatGatesHoldTheCPULimit(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	type engine struct {
		name    string
		script  string
		args    []string // between the ratchet and the tree
		files   map[string]string
		receipt string
	}
	for _, e := range []engine{
		{"c", "c-clean-format.sh", nil, map[string]string{"gen/a.h": "int a;\n", "gen/b.c": "int b;\n"}, "C: clang-format clean over"},
		{"cpp", "cpp-clean-format.sh", []string{"check"}, map[string]string{"gen/a.h": "int a;\n", "gen/b.cpp": "int b;\n"}, "C++: clang-format clean over"},
		{"cpp census", "cpp-clean-format.sh", []string{"census"}, map[string]string{"gen/a.h": "int a;\n", "gen/b.cpp": "int b;\n"}, ""},
	} {
		script := filepath.Join(root, "make", "checks", e.script)
		testinputs.Files(t, script, filepath.Join(root, "make", "checks", "shard-filter.sh"))
		run := func(t *testing.T, formatter, seconds string) (string, error) {
			t.Helper()
			dir := t.TempDir()
			checks := filepath.Join(dir, "make", "checks")
			if err := os.MkdirAll(filepath.Join(dir, "gen"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(checks, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{e.script, "shard-filter.sh"} {
				b, err := os.ReadFile(filepath.Join(root, "make", "checks", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(checks, name), b, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				".clang-format": "BasedOnStyle: LLVM\n",
				"allow.txt":     "",
				// stands in for clang-format: `format --style=… <file>`
				"format": "#!/bin/sh\n" + formatter + "\ncat < \"$2\"\n",
			}
			for rel, text := range e.files {
				files[rel] = text
			}
			for rel, text := range files {
				if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(text), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string{filepath.Join("make", "checks", e.script), "allow.txt"}, e.args...)
			cmd := exec.Command("sh", append(args, "gen")...)
			cmd.Dir = dir
			env := []string{"CLANG_FORMAT=" + filepath.Join(dir, "format"), "OUT=" + filepath.Join(dir, "out"), "LC_ALL=C"}
			if seconds != "-" {
				env = append(env, "FILE_SECONDS="+seconds)
			}
			for _, kv := range os.Environ() {
				if !strings.HasPrefix(kv, "FILE_SECONDS=") {
					env = append(env, kv)
				}
			}
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name      string
				formatter string
				seconds   string // "-": not set at all
				want      string // "" for green, else the line the red run must print
			}{
				// the stand-in exits 3 unless the limit it runs under is the
				// one the gate was given, so a gate that drops the ulimit is red
				{"the formatter runs under the limit it was given", `[ "$(ulimit -t)" = 7 ] || exit 3`, "7", ""},
				{"a file over the limit is red by name", `case $2 in *a.h) kill -s XCPU $$ ;; esac`, "7", "clang-format took more than 7 s of CPU on gen/a.h"},
				{"a formatter that fails is red by name", `case $2 in *a.h) exit 3 ;; esac`, "7", "clang-format failed on gen/a.h"},
				{"no limit is red", "", "-", "FILE_SECONDS must name the per-file CPU limit"},
				{"a limit of zero is red", "", "0", "FILE_SECONDS must name the per-file CPU limit"},
				{"a limit that is not whole seconds is red", "", "2.5", "FILE_SECONDS must name the per-file CPU limit"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					out, err := run(t, tc.formatter, tc.seconds)
					if tc.want == "" {
						if err != nil {
							t.Fatalf("the gate is red on a tree it should pass: %v\n%s", err, out)
						}
						if e.receipt != "" && !strings.Contains(out, e.receipt) {
							t.Fatalf("the green run did not print its receipt:\n%s", out)
						}
						return
					}
					if err == nil {
						t.Fatalf("the gate is green, want it red printing %q:\n%s", tc.want, out)
					}
					if !strings.Contains(out, tc.want) {
						t.Fatalf("the gate is red, but not for the planted reason %q:\n%s", tc.want, out)
					}
				})
			}
		})
	}
}
