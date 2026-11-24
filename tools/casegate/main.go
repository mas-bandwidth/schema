// Command casegate runs one leg's driver of a shared-corpus family and
// refuses a run in which any case of the family's one case file went
// unanswered.
//
//	casegate -name NAME -leg LEG -cases FILE -owed FILE -- command [args...]
//
// WHY (2026-11-24). Every test is a shared corpus: the cases are defined once,
// every one of the 18 legs' drivers answers every case, and the harness
// refuses a missing answer. A driver that counts its own checks (mustrun
// -count) still decides which cases it runs; a case added to the shared file
// and never taught to one driver would leave that leg green. casegate takes
// the case list from the case file, never from the driver.
//
// THE CONTRACT. The driver reads the case file and prints, for every case, a
// line of its own on stdout:
//
//	case <name> ok
//	case <name> FAILED: <why>
//
// casegate copies the driver's output through and exits non-zero, naming the
// leg and the case, when any of these holds:
//
//   - the command exited non-zero;
//   - a case has no answer, or more than one;
//   - a case answered FAILED and the owed ledger has no line for it;
//   - the owed ledger has a line for a case the leg answers ok (the ledger is
//     an exact set: a line that is no longer owed is red until it is removed);
//   - the driver answered a case the case file does not name;
//   - the case file names no case.
//
// THE OWED LEDGER. One line per case a leg does not answer yet:
//
//	<leg> <case> owed: <cause>
//
// and `<leg> all owed: <cause>` for a leg with no driver at all
// (internal/inputfamilies.Check refuses a leg with neither a target nor that
// line). A ledger line naming a case the case file lacks is red.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/mas-bandwidth/schema/v2/internal/casegate"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("casegate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "what this run is called in messages (required)")
	leg := fs.String("leg", "", "the leg the driver is (required)")
	casesPath := fs.String("cases", "", "the family's case file (required)")
	owedPath := fs.String("owed", "", "the family's owed ledger (required; it may hold no line)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: casegate -name NAME -leg LEG -cases FILE -owed FILE -- command [args...]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" || *leg == "" || *casesPath == "" || *owedPath == "" || fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(stderr, "casegate: "+format+"\n", a...) }
	caseText, err := os.ReadFile(*casesPath)
	if err != nil {
		say("%s: %v", *name, err)
		return 2
	}
	cases, err := casegate.ReadCases(string(caseText))
	if err != nil {
		say("%s: %s: %v", *name, *casesPath, err)
		return 2
	}
	owedText, err := os.ReadFile(*owedPath)
	if err != nil {
		say("%s: %v", *name, err)
		return 2
	}
	owed, err := casegate.ReadOwed(string(owedText), *leg)
	if err != nil {
		say("%s: %s: %v", *name, *owedPath, err)
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
			say("%s: %v", *name, err)
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
	red, summary := casegate.Verdict(*name, *leg, exit, seen.Bytes(), cases, owed)
	w := bufio.NewWriter(stderr)
	for _, r := range red {
		_, _ = fmt.Fprintf(w, "casegate: RED: %s\n", r)
	}
	_ = w.Flush()
	if len(red) > 0 {
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "casegate: %s\n", summary)
	return 0
}
