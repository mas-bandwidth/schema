package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCFormatGateProvesItRan drives make/checks/c-clean-format.sh, the engine
// of tables-c-clean, over scratch trees with a stand-in formatter, and holds
// it to the rule that a gate which looked at nothing is red.
//
// Before, the engine printed "clang-format clean" and exited 0 in three cases
// it had judged nothing in: a tree that does not exist (find's error was lost
// in a pipe), trees that hold no C file, and a ratchet line naming a file that
// is on disk but under none of the scanned trees (neither CLEAN nor DRIFT, so
// neither rule fired). Its C++ twin already refused the second.
//
// The stand-in formatter is `cat` (every file is "clean") or `tr a-z A-Z`
// (every file "drifts"): the gate's bookkeeping is what is under test, not
// clang-format.
func TestCFormatGateProvesItRan(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the engine is a POSIX shell script; the C lane that runs it is Linux and macOS")
	}
	root := repoRoot(t)
	script := filepath.Join(root, "make", "checks", "c-clean-format.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}

	type tree map[string]string
	run := func(t *testing.T, files tree, formatter, allow string, trees ...string) (string, error) {
		t.Helper()
		dir := t.TempDir()
		files[".clang-format"] = "BasedOnStyle: LLVM\n"
		files["allow.txt"] = allow
		files["format"] = "#!/bin/sh\n# stands in for clang-format: `format --style=… <file>`\n" + formatter + " < \"$2\"\n"
		for rel, text := range files {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("sh", append([]string{script, "allow.txt"}, trees...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CLANG_FORMAT="+filepath.Join(dir, "format"), "OUT="+filepath.Join(dir, "out"))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	for _, tc := range []struct {
		name      string
		files     tree
		formatter string
		allow     string
		trees     []string
		want      string // "" for green, else the line the red run must print
	}{
		{"a clean tree is green",
			tree{"gen/a.h": "int a;\n", "gen/sub/b.c": "int b;\n"}, "cat", "", []string{"gen"}, ""},
		{"a drifting file on the ratchet is green",
			tree{"gen/a.h": "int a;\n"}, "tr a-z A-Z", "# the ratchet\ngen/a.h\n", []string{"gen"}, ""},
		{"a drifting file off the ratchet is red",
			tree{"gen/a.h": "int a;\n"}, "tr a-z A-Z", "", []string{"gen"}, "clang-format drift in gen/a.h"},
		{"a tree that does not exist is red",
			tree{"gen/a.h": "int a;\n"}, "cat", "", []string{"gen", "moved"}, "moved is not a directory"},
		{"trees that hold no C are red",
			tree{"gen/readme.txt": "nothing generated here\n"}, "cat", "", []string{"gen"}, "no generated C under gen"},
		{"a ratchet line outside every scanned tree is red",
			tree{"gen/a.h": "int a;\n", "other/b.h": "int b;\n"}, "cat", "other/b.h\n", []string{"gen"}, "other/b.h is listed in allow.txt but is under none of the trees this gate scans"},
		{"a ratchet line for a file that is gone is red",
			tree{"gen/a.h": "int a;\n"}, "cat", "gen/gone.h\n", []string{"gen"}, "gen/gone.h is listed in allow.txt but no longer generated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, tc.files, tc.formatter, tc.allow, tc.trees...)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("the gate is red on a tree it should pass: %v\n%s", err, out)
				}
				if !strings.Contains(out, "C: clang-format clean over") {
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
}
