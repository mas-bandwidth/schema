package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestGooseDriverWarmsOnlyTheUnitsItAnswers: the goose driver's batch cache
// (test/conformance/goose/driver, batch_dir and batch_warm) once warmed the
// batch of every unit its manifest listed before answering anything. A
// manifest scoped to one case (the corpus adapter's call, scope.go) still
// lists every unit's rows on the surfaces the run does not run, so one wire
// case paid for every unit's batch: about 30 s, twice over in the case's
// plant (plant.corpus-plants.goose-batch-wrong-byte-root-full). A call warms
// the units it answers, and only those, under the run's warm lock (the
// harness runs surfaces side by side, and their warms take turns, so no more
// than nproc compiles run at once); a second call that finds its units warm
// compiles nothing; a warm lock left by a call that ended is named and taken.
// The goose compiler is a stand-in script that logs each
// program it is asked to run, and whether the run's warm lock was held, and
// answers one wire case; the driver is the real one. Reads and writes only
// its temporary directory.
func TestGooseDriverWarmsOnlyTheUnitsItAnswers(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tmp := t.TempDir()
	write := func(path, body string, mode os.FileMode) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	log := filepath.Join(tmp, "goose.log")
	// the stand-in: `goose run -O0 <unit dir>/main.goose -- batch <manifest>
	// <unit> <surface>...` logs "batch <unit>" and answers case1 on each
	// surface it is asked; any other program is logged by its arguments
	goose := write(filepath.Join(tmp, "bin", "goose"), `#!/bin/sh
shift 3
[ "$1" = -- ] || { echo "stand-in: no -- in $*" >&2; exit 1; }
shift
if [ "$1" = batch ]; then
	echo "batch $3" >> "$GOOSE_LOG"
	ls -d "$GOOSE_UNITS".driver/batch/*/warm.lock >/dev/null 2>&1 || echo "no warm lock held for $3" >> "$GOOSE_LOG"
	shift 3
	for s in "$@"; do
		echo "#surface $s"
		echo "case1 00"
		echo "#done $s"
	done
	exit 0
fi
echo "single $*" >> "$GOOSE_LOG"
echo "case1 00"
`, 0o755)
	units := filepath.Join(tmp, "units")
	for _, u := range []string{"asked", "other"} {
		write(filepath.Join(units, u, "Unit"+u+"Table.goose"), "namespace "+u+";\n", 0o644)
	}
	// one wire case on unit asked; unit other has only report rows, which a
	// scoped manifest keeps when the run does not run the report surface
	manifest := write(filepath.Join(tmp, "manifest.txt"), strings.Join([]string{
		"unit asked",
		"unit other",
		"instance case1 asked Root wire.bin",
		"report report1 other Root report.bin",
		"report report2 other Root report2.bin",
		"",
	}, "\n"), 0o644)
	out := filepath.Join(tmp, "out")
	testinputs.Files(t, "test/conformance/goose", "test/conformance/answer-lines.pl")
	var text []byte
	// the same call twice in one run (this process is the harness): the
	// second finds unit asked warm
	for range 2 {
		cmd := exec.Command("sh", "test/conformance/goose/driver", manifest, "wire", out)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOSE="+goose, "GOOSE_LOG="+log, "GOOSE_UNITS="+units, "SCHEMA_GOOSE_GENERATED="+units)
		var err error
		text, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("the driver's wire call failed: %v\n%s", err, text)
		}
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the stand-in compiler ran no program: %v\ndriver output:\n%s", err, text)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if strings.Join(lines, "|") != "batch asked" {
		t.Fatalf("two wire calls over one unit's case ran %q; want exactly one batch, of unit asked, under the run's warm lock (unit other answers no wire case, so its batch is not this call's, and the second call finds asked warm)", lines)
	}
	// a call with nothing to warm starts no fill: an empty unit's fill
	// leaves .lock or .out behind
	strays, err := filepath.Glob(filepath.Join(units+".driver", "batch", "*", ".*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(strays) > 0 {
		t.Fatalf("a fill ran for no unit: %q", strays)
	}
	// a warm lock whose holder has ended (a call killed while the run goes
	// on) is named and taken, never waited on: the cache of unit asked is
	// made stale by its time alone (no clock is read), so a third call warms
	// it again under a lock a dead process left
	dirs, err := filepath.Glob(filepath.Join(units+".driver", "batch", "*-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("want the run's one batch directory, got %q (%v)", dirs, err)
	}
	ended := exec.Command("true")
	if err := ended.Run(); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(dirs[0], "warm.lock", "pid"), fmt.Sprintln(ended.Process.Pid), 0o644)
	long := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(filepath.Join(dirs[0], "asked.out"), long, long); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "test/conformance/goose/driver", manifest, "wire", out)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOSE="+goose, "GOOSE_LOG="+log, "GOOSE_UNITS="+units, "SCHEMA_GOOSE_GENERATED="+units)
	text, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the driver's wire call after a dead warm lock failed: %v\n%s", err, text)
	}
	if want := fmt.Sprintf("is held by %d, which has ended: taking it", ended.Process.Pid); !strings.Contains(string(text), want) {
		t.Errorf("the dead holder's warm lock was not named (want %q):\n%s", want, text)
	}
	if got, _ := os.ReadFile(log); strings.Join(strings.Fields(string(got)), " ") != "batch asked batch asked" {
		t.Errorf("after a dead warm lock the stand-in ran %q; want unit asked's batch warmed again", got)
	}
	answer, err := os.ReadFile(filepath.Join(out, "case1"))
	if err != nil {
		t.Fatalf("case1 was not answered: %v\ndriver output:\n%s", err, text)
	}
	if string(answer) != "\x00" {
		t.Fatalf("case1's answer is %x, want 00 (the batch's)", answer)
	}
}
