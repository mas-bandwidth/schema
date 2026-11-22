// Command mustrun runs one test harness and refuses a run that did not prove
// it ran.
//
//	mustrun -name NAME -count N -- command [args...]
//
// WHY. A harness in any of the 18 targets can exit 0 having checked nothing:
// a glob that matched no test file, a runner that caught the error and
// printed it, an assert compiled out, a filter that selected zero tests. The
// exit status alone cannot tell that run from a real pass.
//
// THE CONTRACT. The harness prints, as a line of its own on stdout,
//
//	ran N, failed M
//
// (text before it on the line is allowed when it ends in ": "). N counts the
// checks or cases the harness executed, M the ones that failed. mustrun copies
// the harness's output through unchanged and then exits non-zero when ANY of
// these holds, naming which:
//
//   - the command exited non-zero (mustrun exits with the same status; a
//     command killed by a signal gives 128+n);
//   - no "ran N, failed M" line was printed, or more than one was;
//   - N is zero;
//   - M is not zero, even though the command exited zero;
//   - N differs from -count, the count registered in the recipe. A harness
//     that gains or loses a check changes -count in the same commit, so a
//     test that silently stops running is red.
//
// -count is required and must be at least 1: there is no way to ask mustrun
// to accept whatever ran.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

var ranLine = regexp.MustCompile(`(?m)^(?:.*: )?ran ([0-9]+), failed ([0-9]+)\r?$`)

// verdict decides a finished run. It is a pure function of the command's exit
// status, its stdout and the registered count.
func verdict(name string, exit int, stdout []byte, want int) (int, string) {
	if exit != 0 {
		return exit, fmt.Sprintf("%s: the command exited %d", name, exit)
	}
	lines := ranLine.FindAllSubmatch(stdout, -1)
	if len(lines) == 0 {
		return 1, fmt.Sprintf("%s: the command exited 0 and printed no \"ran N, failed M\" line: nothing shows a test ran", name)
	}
	if len(lines) > 1 {
		return 1, fmt.Sprintf("%s: the command printed %d \"ran N, failed M\" lines; exactly one is the run's count", name, len(lines))
	}
	ran, err1 := strconv.Atoi(string(lines[0][1]))
	failed, err2 := strconv.Atoi(string(lines[0][2]))
	if err1 != nil || err2 != nil {
		return 1, fmt.Sprintf("%s: the count line does not hold two numbers: %q", name, lines[0][0])
	}
	if failed != 0 {
		return 1, fmt.Sprintf("%s: the command exited 0 and reported %d failed of %d", name, failed, ran)
	}
	if ran == 0 {
		return 1, fmt.Sprintf("%s: ran 0: the harness found nothing to run", name)
	}
	if ran != want {
		return 1, fmt.Sprintf("%s: ran %d, and the recipe registers %d: a check was added or stopped running; change -count in the same commit as the harness", name, ran, want)
	}
	return 0, ""
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mustrun", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "what this run is called in messages (required)")
	count := fs.Int("count", 0, "the registered number of checks the harness runs (required, at least 1)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: mustrun -name NAME -count N -- command [args...]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" || *count < 1 || fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	cmd := exec.Command(fs.Arg(0), fs.Args()[1:]...)
	var seen bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(stdout, &seen)
	cmd.Stderr = stderr
	exit := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			_, _ = fmt.Fprintf(stderr, "mustrun: %s: %v\n", *name, err)
			return 127
		}
		exit = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			exit = 128 + int(ws.Signal())
		}
		if exit <= 0 {
			exit = 1
		}
	}
	code, msg := verdict(*name, exit, seen.Bytes(), *count)
	if code != 0 {
		_, _ = fmt.Fprintf(stderr, "mustrun: RED: %s\n", msg)
	}
	return code
}
