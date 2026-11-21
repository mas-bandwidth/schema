//go:build unix

// THE REVIEWER'S FOUR, THROUGH THE BUILT BINARY UNDER A REAL LIMIT.
//
// Under a 24-byte file-size limit (RLIMIT_FSIZE, what `ulimit -f` sets),
// `schema fmt`, `schema fmt --migrate`, `schema tables-baseline --update` and
// `schema lock` each exited with "file too large" having cut the file they
// were rewriting down to 24 bytes, and the unrestricted retry of the baseline
// update then succeeded with the earlier history reason gone. Here each runs
// under that limit and must leave its file byte for byte as it was, with the
// same mode and nothing staged beside it; the retry without the limit
// succeeds, and the baseline's history keeps both reasons.
//
// The limit is the kernel's own, so this file is the functional half
// (SCHEMA_SLOW); atomicwrites_test.go fails the same commands through the
// publisher's seam in every run.
package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// sizeLimitEnv, set in the environment of this test binary, makes it lower
// its own file-size limit to that many bytes and become the command in its
// arguments: the way a test starts a child under a limit without a shell.
const sizeLimitEnv = "SCHEMA_TEST_FILE_SIZE_LIMIT"

func init() {
	v := os.Getenv(sizeLimitEnv)
	if v == "" {
		return
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || len(os.Args) < 2 {
		os.Exit(97)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: n, Max: n}); err != nil {
		os.Exit(98)
	}
	// a shell or a harness that ignores SIGXFSZ hands that on to its
	// children, and the write then fails with EFBIG ("file too large"):
	// the reviewer's run
	signal.Ignore(syscall.SIGXFSZ)
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, sizeLimitEnv+"=") {
			env = append(env, e)
		}
	}
	_ = syscall.Exec(os.Args[1], os.Args[1:], env)
	os.Exit(99)
}

// runLimited runs the CLI under a file-size limit of limit bytes and answers
// whether it succeeded and what it printed.
func runLimited(t *testing.T, limit int, bin string, args ...string) (ok bool, out string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, append([]string{bin}, args...)...)
	cmd.Env = append(os.Environ(), sizeLimitEnv+"="+strconv.Itoa(limit))
	data, err := cmd.CombinedOutput()
	if err != nil {
		if _, exited := errors.AsType[*exec.ExitError](err); !exited {
			t.Fatalf("schema %s under a %d-byte limit: %v\n%s", strings.Join(args, " "), limit, err, data)
		}
		return false, string(data)
	}
	return true, string(data)
}

func TestARealSizeLimitDamagesNoFile(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	const limit = 24

	cases := []struct {
		name string
		// setup builds the unit and answers the file the command rewrites
		setup func(t *testing.T, dir string) string
		args  []string
		check func(t *testing.T, file string)
	}{
		{
			name: "fmt",
			setup: func(t *testing.T, dir string) string {
				mustWrite(t, filepath.Join(dir, "Player.schema"), unformattedUnit)
				return filepath.Join(dir, "Player.schema")
			},
			args: []string{"fmt"},
		},
		{
			name: "fmt --migrate",
			setup: func(t *testing.T, dir string) string {
				mustWrite(t, filepath.Join(dir, "Player.schema"), atomicRetiredSpelling)
				return filepath.Join(dir, "Player.schema")
			},
			args: []string{"fmt", "--migrate"},
		},
		{
			name: "tables-baseline --update",
			setup: func(t *testing.T, dir string) string {
				mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Schema)
				run(t, bin, "tables-baseline", "--update", "--reason", "the first reason", dir)
				mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Widened)
				return filepath.Join(dir, baseline.FileName)
			},
			args: []string{"tables-baseline", "--update", "--reason", "the second reason"},
			check: func(t *testing.T, file string) {
				text := mustRead(t, file)
				for _, reason := range []string{"the first reason", "the second reason"} {
					if !strings.Contains(text, reason) {
						t.Errorf("after the limited run and its retry the baseline's history lost %q:\n%s", reason, text)
					}
				}
			},
		},
		{
			name: "lock",
			setup: func(t *testing.T, dir string) string {
				mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Schema)
				run(t, bin, "lock", dir)
				mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Widened)
				return filepath.Join(dir, lockfile.FileName)
			},
			args: []string{"lock"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// the control: with no limit the command succeeds and rewrites
			// the file, so the limited run below is refusing a real write
			control := realTempDir(t)
			file := tc.setup(t, control)
			was := mustRead(t, file)
			run(t, bin, append(tc.args, control)...)
			if mustRead(t, file) == was {
				t.Fatalf("the fixture is not one schema %s rewrites", strings.Join(tc.args, " "))
			}
			// and a limit the new content fits under refuses nothing
			roomy := realTempDir(t)
			tc.setup(t, roomy)
			if ok, out := runLimited(t, 1<<20, bin, append(tc.args, roomy)...); !ok {
				t.Fatalf("under a limit the file fits: %s", out)
			}

			dir := realTempDir(t)
			file = tc.setup(t, dir)
			if len(mustRead(t, file)) <= limit {
				t.Fatalf("the fixture is %d bytes: no longer than the limit", len(mustRead(t, file)))
			}
			ageTree(t, dir)
			before := snapshotTree(t, dir)
			ok, out := runLimited(t, limit, bin, append(tc.args, dir)...)
			if ok {
				t.Errorf("schema %s succeeded under a %d-byte limit: %s", strings.Join(tc.args, " "), limit, out)
			}
			if !strings.Contains(out, "file too large") {
				t.Errorf("the refusal does not say the file was too large: %q", out)
			}
			assertSameTree(t, "after a run under a 24-byte limit", before, snapshotTree(t, dir))

			// the retry without the limit
			run(t, bin, append(tc.args, dir)...)
			if mustRead(t, file) == before[filepath.Base(file)].data {
				t.Errorf("the retry rewrote nothing")
			}
			if tc.check != nil {
				tc.check(t, file)
			}
		})
	}
}

// TestARealSizeLimitFormatsADirectoryAllOrNothing is F-220's short write: fmt
// and fmt --migrate over a directory of two sources, under a limit A's
// canonical form fits and B's does not. A directory is one request
// (docs/CLI.md), so the run is refused with A as it was too; it used to
// publish A and then fail on B. The sources are the committed corpus in
// testdata/fmt_directory.
func TestARealSizeLimitFormatsADirectoryAllOrNothing(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	const limit = 70
	corpus := filepath.Join("testdata", "fmt_directory")
	for _, tc := range []struct {
		args []string
		a, b string
	}{
		{[]string{"fmt"}, "A.in", "B.long"},
		{[]string{"fmt", "--migrate"}, "A.retired", "B.retired"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Parallel()
			setup := func(t *testing.T) (dir, a, b string) {
				dir = realTempDir(t)
				a, b = filepath.Join(dir, "A.schema"), filepath.Join(dir, "B.schema")
				mustWrite(t, a, mustRead(t, filepath.Join(corpus, tc.a)))
				mustWrite(t, b, mustRead(t, filepath.Join(corpus, tc.b)))
				return dir, a, b
			}
			// the control: unrestricted, both change; A's canonical form
			// fits under the limit and B's does not, the shape of the finding
			control, ca, cb := setup(t)
			wasA, wasB := mustRead(t, ca), mustRead(t, cb)
			run(t, bin, append(tc.args, control)...)
			canonA, canonB := mustRead(t, ca), mustRead(t, cb)
			if canonA == wasA || canonB == wasB {
				t.Fatalf("the corpus is not two sources schema %s rewrites", strings.Join(tc.args, " "))
			}
			if len(canonA) > limit || len(canonB) <= limit {
				t.Fatalf("canonical A is %d bytes and B %d: the case needs A within the %d-byte limit and B beyond it", len(canonA), len(canonB), limit)
			}

			dir, a, b := setup(t)
			ageTree(t, dir)
			before := snapshotTree(t, dir)
			ok, out := runLimited(t, limit, bin, append(tc.args, dir)...)
			if ok {
				t.Errorf("schema %s succeeded under a %d-byte limit: %s", strings.Join(tc.args, " "), limit, out)
			}
			if !strings.Contains(out, "file too large") {
				t.Errorf("the refusal does not say the file was too large: %q", out)
			}
			assertSameTree(t, "after a directory run under a 70-byte limit", before, snapshotTree(t, dir))

			// the retry without the limit lands both
			run(t, bin, append(tc.args, dir)...)
			if mustRead(t, a) != canonA || mustRead(t, b) != canonB {
				t.Errorf("the retry is not the unrestricted run's output")
			}
		})
	}
}

// TestGenerateOverAReadOnlyFileThroughTheCLI is the reviewer's second run: one
// file of an earlier generation is read-only, the unit has changed, and
// `schema generate` exits 1 with permission denied. Every generated file must
// be the one the earlier run wrote (it left five new and one old); with the
// file writable again the retry writes them all.
func TestGenerateOverAReadOnlyFileThroughTheCLI(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	bin := buildCLI(t)
	unit, out := realTempDir(t), realTempDir(t)
	mustWrite(t, filepath.Join(unit, "Config.schema"), sharedS3Schema)
	run(t, bin, "generate", "--lang", "go", "--out", out, unit)
	mustWrite(t, filepath.Join(unit, "Config.schema"), sharedS3Widened)

	// which files the widened unit changes, from a twin directory
	twin := realTempDir(t)
	run(t, bin, "generate", "--lang", "go", "--out", twin, unit)
	first, want := snapshotTree(t, out), snapshotTree(t, twin)
	var changed []string
	for _, name := range treeNamesOf(want) {
		if first[name].data != want[name].data {
			changed = append(changed, name)
		}
	}
	if len(changed) < 2 {
		t.Fatalf("the widened unit changes %v: the fixture needs at least two changed files", changed)
	}
	locked := filepath.Join(out, changed[len(changed)-1])
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	ageTree(t, out)
	before := snapshotTree(t, out)

	code, text := runExit(t, bin, "generate", "--lang", "go", "--out", out, unit)
	if code != 1 || !strings.Contains(text, "permission denied") {
		t.Errorf("generate over a read-only file: exit %d, %q; want exit 1 and permission denied", code, text)
	}
	assertSameTree(t, "after generate was refused by a read-only file", before, snapshotTree(t, out))

	if err := os.Chmod(locked, 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, bin, "generate", "--lang", "go", "--out", out, unit)
	after := snapshotTree(t, out)
	for _, name := range treeNamesOf(want) {
		if after[name].data != want[name].data {
			t.Errorf("after the retry %s is not the file the unit generates", name)
		}
	}
}
