package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/testinputs"
)

// TestCppFormatGateProvesItRan drives make/checks/cpp-clean-format.sh, the
// engine of tables-cpp-format, over scratch trees with a stand-in formatter,
// and holds it to the rule its C twin (TestCFormatGateProvesItRan) already
// keeps: a gate that judged nothing, or judged with a formatter that failed,
// is red by name.
//
// Before, the engine was green in three cases it had not judged: a named tree
// that holds no C++ beside one that does (only the union was checked for
// emptiness), a formatter that printed the file's own bytes and exited
// non-zero (only its output was compared), and a ratchet line for a file on
// disk under none of the scanned trees (called "clean now", which is red but
// names the wrong fault). Found in the format-gate audit (F-130 to F-132,
// F-213, F-214) on rowan/v3.0.2-parity (#4450), which never reached main.
//
// The stand-in formatter is `cat` (every file is "clean"), `tr a-z A-Z`
// (every file "drifts"), or `cat; exit 9` (right bytes, failed run).
func TestCppFormatGateProvesItRan(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the engine is a POSIX shell script; the C++ lane that runs it is Linux and macOS")
	}
	root := repoRoot(t)
	script := filepath.Join(root, "make", "checks", "cpp-clean-format.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}

	type tree map[string]string
	run := func(t *testing.T, files tree, formatter, allow, mode string, trees ...string) (string, error) {
		t.Helper()
		dir := t.TempDir()
		files[".clang-format"] = "BasedOnStyle: LLVM\n"
		if allow != "<none>" {
			files["allow.txt"] = allow
		}
		files["format"] = "#!/bin/sh\n# stands in for clang-format: `format --style=… <file>`\n" + formatter + " < \"$2\"\n"
		if formatter == "cat; exit 9" {
			files["format"] = "#!/bin/sh\ncat < \"$2\"\nexit 9\n"
		}
		for rel, text := range files {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		testinputs.Files(t, script)
		cmd := exec.Command("sh", append([]string{script, "allow.txt", mode}, trees...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CLANG_FORMAT="+filepath.Join(dir, "format"), "OUT="+filepath.Join(dir, "out"), "JOBS=2")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	for _, tc := range []struct {
		name      string
		files     tree
		formatter string
		allow     string
		mode      string
		trees     []string
		want      string // "" for green, else the line the red run must print
	}{
		{"a clean tree is green",
			tree{"gen/a.h": "int a;\n", "gen/sub/b.cpp": "int b;\n"}, "cat", "", "check", []string{"gen"}, ""},
		{"a drifting file on the ratchet is green",
			tree{"gen/a.h": "int a;\n"}, "tr a-z A-Z", "# the ratchet\ngen/a.h\n", "check", []string{"gen"}, ""},
		{"census lists the drifting file",
			tree{"gen/a.h": "int a;\n"}, "tr a-z A-Z", "", "census", []string{"gen"}, "<census>gen/a.h"},
		{"a drifting file off the ratchet is red",
			tree{"gen/a.h": "int a;\n"}, "tr a-z A-Z", "", "check", []string{"gen"}, "clang-format drift in gen/a.h"},
		{"a tree that does not exist is red",
			tree{"gen/a.h": "int a;\n"}, "cat", "", "check", []string{"gen", "moved"}, "moved is not a directory"},
		{"a tree with no C++ beside one with C++ is red",
			tree{"gen/a.h": "int a;\n", "empty/readme.txt": "nothing generated here\n"}, "cat", "", "check", []string{"gen", "empty"}, "no generated C++ under empty"},
		{"a formatter that fails is red even when its bytes match",
			tree{"gen/a.h": "int a;\n"}, "cat; exit 9", "", "check", []string{"gen"}, "exited non-zero on gen/a.h"},
		{"a formatter that fails is red in census too",
			tree{"gen/a.h": "int a;\n"}, "cat; exit 9", "", "census", []string{"gen"}, "exited non-zero on gen/a.h"},
		{"a missing ratchet is red",
			tree{"gen/a.h": "int a;\n"}, "cat", "<none>", "check", []string{"gen"}, "the ratchet allow.txt is missing or unreadable"},
		{"a ratchet line outside every scanned tree is red",
			tree{"gen/a.h": "int a;\n", "other/b.h": "int b;\n"}, "cat", "other/b.h\n", "check", []string{"gen"}, "other/b.h is listed in allow.txt but is under none of the trees this gate scans"},
		{"a ratchet line for a clean file is red",
			tree{"gen/a.h": "int a;\n"}, "cat", "gen/a.h\n", "check", []string{"gen"}, "gen/a.h is clean now"},
		{"a ratchet line for a file that is gone is red",
			tree{"gen/a.h": "int a;\n"}, "cat", "gen/gone.h\n", "check", []string{"gen"}, "gen/gone.h is listed in allow.txt but no longer generated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := run(t, tc.files, tc.formatter, tc.allow, tc.mode, tc.trees...)
			if census, ok := strings.CutPrefix(tc.want, "<census>"); ok {
				if err != nil || strings.TrimSpace(out) != census {
					t.Fatalf("census: want exactly %q and green, got %v:\n%s", census, err, out)
				}
				return
			}
			if tc.want == "" {
				if err != nil {
					t.Fatalf("the gate is red on a tree it should pass: %v\n%s", err, out)
				}
				if !strings.Contains(out, "C++: clang-format clean over") {
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
