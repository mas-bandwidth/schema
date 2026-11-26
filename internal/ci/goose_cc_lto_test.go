package ci

// THE GOOSE LTO COMPILER FAILS LOUDLY. make/goose-cc-lto.sh is the C
// compiler `goose build --cc` runs for a per-commit fuzz leg
// (make/goose-wirefuzz-build.sh under GOOSE_LEG_LTO). It ran no set -e when it
// landed (recipescan's TestCIAreaFailsLoudly named it); it now runs set -eu,
// and its exec hands goose the compiler's own exit status. A compile that
// fails is then the leg build's failure: goose-wirefuzz-build.sh exits
// non-zero, which make/goose-fuzz.mk records in build-failed.txt and fails
// the make on, by unit. This test runs the script, and the leg build through
// it, with a stand-in compiler and goose written to its TempDir: no
// toolchain, no network, nothing outside its TempDir written.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

const gooseLTOScript = "make/goose-cc-lto.sh"

// writeExec writes an executable shell script.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// exitCode is a finished command's exit status, -1 when it did not run.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// runLTO runs the script text as the leg build runs it, with GOOSE_LEG_CC
// naming cc, and returns its exit status and output.
func runLTO(t *testing.T, script, cc string, args ...string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "goose-cc-lto.sh")
	writeExec(t, path, script)
	// testinputs:none — runs a copy of the script in its own directory, with a stand-in compiler
	cmd := exec.Command("sh", append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOSE_LEG_CC="+cc)
	out, err := cmd.CombinedOutput()
	if c := exitCode(err); c >= 0 {
		return c, string(out)
	}
	t.Fatalf("sh did not run: %v", err)
	return 0, ""
}

// ltoVerdict is what a script does with a compiler that passes, one that
// fails, and one that is not there: "" when each is right, else what is not.
func ltoVerdict(t *testing.T, script string) []string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	good := filepath.Join(dir, "cc-good")
	writeExec(t, good, `printf '%s\n' "$@" > "`+argsFile+`"`+"\n")
	bad := filepath.Join(dir, "cc-bad")
	writeExec(t, bad, "echo 'cc: unit.c:1: error' >&2\nexit 3\n")
	var wrong []string
	if c, out := runLTO(t, script, good, "-O2", "-c", "unit.c"); c != 0 {
		wrong = append(wrong, "a compile that passes exits "+strconv.Itoa(c)+": "+out)
	} else if b, err := os.ReadFile(argsFile); err != nil || strings.Join(strings.Fields(string(b)), " ") != "-O2 -c unit.c -flto=auto" {
		wrong = append(wrong, "the compiler is not run with goose's flags and -flto=auto: "+string(b))
	}
	if c, _ := runLTO(t, script, bad, "-O2", "-c", "unit.c"); c != 3 {
		wrong = append(wrong, "a compile that fails (cc exit 3) exits "+strconv.Itoa(c)+", not the compiler's 3")
	}
	if c, _ := runLTO(t, script, filepath.Join(dir, "no-such-cc"), "-c", "unit.c"); c == 0 {
		wrong = append(wrong, "a compiler that is not there exits 0")
	}
	return wrong
}

func TestTheGooseLTOCompilerFailsLoudly(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), gooseLTOScript))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "\nset -eu\n") {
		t.Errorf("%s does not run set -eu", gooseLTOScript)
	}
	for _, w := range ltoVerdict(t, src) {
		t.Errorf("%s: %s", gooseLTOScript, w)
	}
	// the plants: the compiler's failure swallowed; and, with set -eu gone,
	// a line after the compile whose status the script ends with. Each is red.
	// (With set -eu the second is caught: the script stops at the compile.)
	const execLine = `exec "${GOOSE_LEG_CC:-cc}" "$@" -flto=auto`
	if !strings.Contains(src, execLine) {
		t.Fatalf("%s no longer runs %s: the plants have nothing to edit", gooseLTOScript, execLine)
	}
	unstrict := strings.Replace(src, "\nset -eu\n", "\n", 1)
	for name, plant := range map[string]string{
		"swallowed":               strings.Replace(src, execLine, `"${GOOSE_LEG_CC:-cc}" "$@" -flto=auto || true`, 1),
		"no set -eu, status lost": strings.Replace(unstrict, execLine, `"${GOOSE_LEG_CC:-cc}" "$@" -flto=auto; echo done`, 1),
	} {
		if plant == src {
			t.Fatalf("planted %s: the plant changed nothing", name)
		}
		if len(ltoVerdict(t, plant)) == 0 {
			t.Errorf("planted %s: the test is not red", name)
		}
	}
	if len(ltoVerdict(t, strings.Replace(src, execLine, `"${GOOSE_LEG_CC:-cc}" "$@" -flto=auto; echo done`, 1))) != 0 {
		t.Errorf("with set -eu, a line after the compile hides its failure: set -e is not holding")
	}
}

// TestAFailedLTOCompileFailsTheLegBuild: make/goose-wirefuzz-build.sh under
// GOOSE_LEG_LTO, with a goose that runs the --cc it is handed (as goose build
// does) and exits with its status: a compile that fails fails the leg build,
// non-zero with the build log on stderr, and one that passes builds. The
// plant, the build's status swallowed in a copy of the script, is red.
func TestAFailedLTOCompileFailsTheLegBuild(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	// what the leg build reads from the checkout: the compiler script it hands
	// goose, and the leg's sources it copies beside the unit
	testinputs.Files(t, gooseLTOScript, "test/conformance/goose/wirefuzz.goose",
		"test/conformance/goose/wirefuzz.build", "test/conformance/goose/wirefuzz_io.c")
	build, err := os.ReadFile(filepath.Join(root, "make", "goose-wirefuzz-build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, script, cc string) (int, string) {
		t.Helper()
		dir := t.TempDir()
		src := filepath.Join(dir, "unit")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "XTable.goose"), []byte("namespace x;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		goose := filepath.Join(dir, "goose")
		// goose build: find --cc, compile one C file with it, keep its status
		writeExec(t, goose, `cc=
while [ $# -gt 0 ]; do
	case "$1" in --cc) cc=$2; shift ;; esac
	shift
done
[ -n "$cc" ] || { echo "goose: no --cc" >&2; exit 9; }
"$cc" -O2 -c unit.c -o unit.o || exit $?
: > wirefuzz
`)
		path := filepath.Join(dir, "goose-wirefuzz-build.sh")
		writeExec(t, path, script)
		cmd := exec.Command("sh", path, goose, src, filepath.Join(dir, "out"))
		cmd.Dir = root // the script copies the leg's sources from the checkout, and writes only under out
		cmd.Env = append(os.Environ(), "GOOSE_LEG_LTO=1", "GOOSE_LEG_CC="+cc)
		out, err := cmd.CombinedOutput()
		c := exitCode(err)
		if c < 0 {
			t.Fatalf("sh did not run: %v", err)
		}
		return c, string(out)
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "cc-good")
	writeExec(t, good, "exit 0\n")
	bad := filepath.Join(dir, "cc-bad")
	writeExec(t, bad, "echo 'cc: unit.c:1: error: planted' >&2\nexit 1\n")
	if c, out := run(t, string(build), good); c != 0 {
		t.Fatalf("a leg whose compile passes does not build (exit %d): %s", c, out)
	}
	if c, out := run(t, string(build), bad); c == 0 || !strings.Contains(out, "planted") {
		t.Errorf("a leg whose LTO compile fails: exit %d, want non-zero with the compiler's words: %s", c, out)
	}
	// the plant: the build's status swallowed
	const buildLine = `(cd "$out" && "$goose" build -O2 "$@" wirefuzz.goose -o wirefuzz) >"$out/build.log" 2>&1 || {`
	if !strings.Contains(string(build), buildLine) {
		t.Fatalf("goose-wirefuzz-build.sh no longer runs %s: the plant has nothing to edit", buildLine)
	}
	planted := strings.Replace(string(build), buildLine, `(cd "$out" && "$goose" build -O2 "$@" wirefuzz.goose -o wirefuzz) >"$out/build.log" 2>&1 || true || {`, 1)
	if c, _ := run(t, planted, bad); c != 0 {
		t.Errorf("planted (the build's status swallowed): exit %d, so the test is not red on it", c)
	}
}
