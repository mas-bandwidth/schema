package ci

// make/checks/c-clean-format.sh, the C format gate's engine, reads a file's
// verdict from a store on the runner host when C_FORMAT_CACHE is set (the
// per-commit lane, ci-fast.yml lang-clean, c row). The gate must judge every
// file's present bytes, so the store may answer only for the same bytes, the
// same formatter and the same style, and never from a formatter that failed.
// This drives the script with a counting stand-in for clang-format inside the
// test's own directory: no clang-format, no network, no clock.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// The stand-in formatter: --version prints the first line of version.txt;
// otherwise it logs the call and writes the file with every "DRIFT" replaced
// (so a file holding that word is not what the formatter writes). With a
// file named broken beside it, it writes the file unchanged and exits 1.
const fakeClangFormat = `#!/bin/sh
here=$(dirname "$0")
if [ "$1" = "--version" ]; then cat "$here/version.txt"; exit 0; fi
for a in "$@"; do f=$a; done
echo "$f" >> "$here/calls.log"
sed 's/DRIFT/fixed/g' "$f"
if [ -f "$here/broken" ]; then exit 1; fi
`

type cFormatRig struct {
	t      *testing.T
	script string
	dir    string
}

func newCFormatRig(t *testing.T) *cFormatRig {
	t.Helper()
	slowtest.Gate(t, "shell (the C format gate's verdict store, with a stand-in formatter)")
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is not on this machine: the script formats every file here, as it says")
	}
	r := &cFormatRig{t: t, script: filepath.Join(repoRoot(t), "make", "checks", "c-clean-format.sh"), dir: t.TempDir()}
	r.write("clang-format", fakeClangFormat)
	if err := os.Chmod(filepath.Join(r.dir, "clang-format"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write("version.txt", "clang-format version 23.1.3\n")
	r.write(".clang-format", "BasedOnStyle: LLVM\n")
	r.write("allow.txt", "# ratchet\ntree/listed.h\n")
	if err := os.MkdirAll(filepath.Join(r.dir, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write("tree/a.h", "int a;\n")
	r.write("tree/b.c", "int b;\n")
	r.write("tree/listed.h", "int DRIFT;\n")
	return r
}

func (r *cFormatRig) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// run runs the gate over tree/ with the store on, and hands back its output,
// its error and how many files the formatter was started on.
func (r *cFormatRig) run() (string, int, error) {
	r.t.Helper()
	calls := filepath.Join(r.dir, "calls.log")
	if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
		r.t.Fatal(err)
	}
	cmd := testproc.Command(r.t, "sh", r.script, "allow.txt", "tree")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"CLANG_FORMAT="+filepath.Join(r.dir, "clang-format"),
		"OUT="+filepath.Join(r.dir, "out"),
		"C_FORMAT_CACHE="+filepath.Join(r.dir, "store"))
	out, err := cmd.CombinedOutput()
	b, readErr := os.ReadFile(calls)
	if readErr != nil && !os.IsNotExist(readErr) {
		r.t.Fatal(readErr)
	}
	return string(out), strings.Count(string(b), "\n"), err
}

func (r *cFormatRig) want(what string, wantGreen bool, wantCalls int, say ...string) {
	r.t.Helper()
	out, calls, err := r.run()
	if (err == nil) != wantGreen || calls != wantCalls {
		r.t.Fatalf("%s: green=%v with %d formatter runs, want green=%v with %d\n%s", what, err == nil, calls, wantGreen, wantCalls, out)
	}
	for _, s := range say {
		if !strings.Contains(out, s) {
			r.t.Errorf("%s: the gate does not say %q:\n%s", what, s, out)
		}
	}
}

// Held verdicts answer only for the same bytes, formatter and style, and the
// gate's verdict from the store is the one it gave from the formatter: green
// stays green, a drift off the ratchet stays red by name, and a listed file
// that became clean is still told to leave the list.
func TestCFormatStoreAnswersOnlyForTheSameBytes(t *testing.T) {
	r := newCFormatRig(t)
	r.want("the first run", true, 3, "0 of 3 verdicts", "1 table-emitter files still on the ratchet")
	r.want("the same tree again", true, 0, "3 of 3 verdicts", "1 table-emitter files still on the ratchet")

	r.write("tree/b.c", "int DRIFT_b;\n")
	r.want("a file's bytes changed to a drift", false, 1, "clang-format drift in tree/b.c")
	r.want("that drift, from the store", false, 0, "clang-format drift in tree/b.c")

	r.write("tree/b.c", "int b;\n")
	r.write("tree/listed.h", "int listed;\n")
	r.want("a listed file became clean", false, 1, "tree/listed.h is clean now")
	r.want("that clean listed file, from the store", false, 0, "tree/listed.h is clean now")
	r.write("tree/listed.h", "int DRIFT;\n")
	r.want("back to the first tree", true, 0, "3 of 3 verdicts")

	r.write(".clang-format", "BasedOnStyle: Google\n")
	r.want("the style file changed", true, 3, "0 of 3 verdicts")
	r.write("version.txt", "clang-format version 23.1.4\n")
	r.want("the formatter changed", true, 3, "0 of 3 verdicts")
}

// THE REVERSED WITNESS: a formatter that fails gives no verdict to keep. The
// file reads as a drift, as it did before the store, and the next run starts
// the formatter on it again instead of answering from the failure.
func TestCFormatStoreNeverKeepsAFailedFormatter(t *testing.T) {
	r := newCFormatRig(t)
	r.write("broken", "")
	r.want("the formatter exits 1", false, 3, "clang-format drift in tree/a.h")
	r.want("the formatter exits 1 again", false, 3, "0 of 3 verdicts")
	if err := os.Remove(filepath.Join(r.dir, "broken")); err != nil {
		t.Fatal(err)
	}
	r.want("the formatter works", true, 3, "0 of 3 verdicts")
	r.want("and its verdicts are held", true, 0, "3 of 3 verdicts")
}
