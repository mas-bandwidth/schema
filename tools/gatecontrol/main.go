// Command gatecontrol runs one shared gate on one leg with its oracle planted
// wrong, and passes only when the leg's gate goes red on the plant: the
// negative control of that gate on that leg (make/widen-18-gate-controls.mk).
//
//	go run ./tools/gatecontrol -leg go -plant tablegen -target verify-tablegen-go \
//	    -want 'FAILED: tg[0-9]+:' [-refuse 'TABLEGEN_OK go '] [-log FILE]
//
// It runs `make <target>` with SCHEMA_GATE_PLANT=<plant> in the environment.
// The harnesses read that variable and make their oracle wrong (bitphase,
// canonical, tablegen, and the conformance harness's message-fuzz, fixed,
// variable and retain plants), printing "PLANTED <plant>" when they do. The
// control is green only when ALL of these hold, and names the one that does
// not:
//
//   - make exited non-zero (a gate that stays green on a wrong oracle
//     compares nothing for this leg);
//   - the log says PLANTED <plant> (a red that came before the plant was
//     applied proves nothing about it);
//   - -want, which is required, matches the log: the red is the planted
//     case's own, named by the leg, so a red for any other reason (the
//     interpreter missing, a build broken after the plant) fails the control;
//   - -refuse, when given, does not match (the leg did not report a pass).
//
// The whole log is written to -log (default build/gate-controls/<plant>-<leg>.log)
// and printed on a failure.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// verdict is the control's reading of one planted run: "" when it is red the
// way the plant demands, else what is wrong.
func verdict(plant string, exitErr error, log []byte, want, refuse *regexp.Regexp) string {
	switch {
	case exitErr == nil:
		return "the gate stayed green with its oracle planted wrong: it compares nothing for this leg"
	case !bytes.Contains(log, []byte("PLANTED "+plant)):
		return "the gate went red before the " + plant + " plant was applied (no PLANTED " + plant + " line): that red proves nothing about the plant"
	case want != nil && !want.Match(log):
		return fmt.Sprintf("the gate went red, but not on the planted case (no line matches %q)", want)
	case refuse != nil && refuse.Match(log):
		return fmt.Sprintf("the leg reported a pass (%q) under the plant", refuse)
	}
	return ""
}

func main() {
	leg := flag.String("leg", "", "the leg")
	plant := flag.String("plant", "", "the SCHEMA_GATE_PLANT class")
	target := flag.String("target", "", "the make target that runs the gate on the leg")
	wantFlag := flag.String("want", "", "a regular expression naming the planted case, which the red log must match (required)")
	refuseFlag := flag.String("refuse", "", "a regular expression the red log must not match")
	logFlag := flag.String("log", "", "where the run's log is written")
	flag.Parse()
	if *leg == "" || *plant == "" || *target == "" || *wantFlag == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gatecontrol: -leg, -plant, -target and -want are required, and nothing else: without -want a red for any other reason (a missing interpreter, a broken build) would pass the control")
		os.Exit(2)
	}
	var want, refuse *regexp.Regexp
	var err error
	if *wantFlag != "" {
		if want, err = regexp.Compile(*wantFlag); err != nil {
			fmt.Fprintf(os.Stderr, "gatecontrol: -want: %v\n", err)
			os.Exit(2)
		}
	}
	if *refuseFlag != "" {
		if refuse, err = regexp.Compile(*refuseFlag); err != nil {
			fmt.Fprintf(os.Stderr, "gatecontrol: -refuse: %v\n", err)
			os.Exit(2)
		}
	}
	logPath := *logFlag
	if logPath == "" {
		logPath = filepath.Join("build", "gate-controls", *plant+"-"+*leg+".log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "gatecontrol: %v\n", err)
		os.Exit(1)
	}
	var out bytes.Buffer
	cmd := exec.Command("make", "--no-print-directory", *target)
	cmd.Env = append(os.Environ(), "SCHEMA_GATE_PLANT="+*plant)
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if err := os.WriteFile(logPath, out.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gatecontrol: %v\n", err)
		os.Exit(1)
	}
	if why := verdict(*plant, runErr, out.Bytes(), want, refuse); why != "" {
		if _, werr := os.Stderr.Write(out.Bytes()); werr != nil {
			fmt.Fprintf(os.Stderr, "gatecontrol: the leg's output could not be written to stderr: %v\n", werr)
		}
		fmt.Fprintf(os.Stderr, "NEGATIVE CONTROL FAILED: %s on %s (make %s; log %s): %s\n", *plant, *leg, *target, logPath, why)
		os.Exit(1)
	}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if want.MatchString(line) {
			fmt.Println("  " + strings.TrimSpace(line))
			break
		}
	}
	fmt.Printf("%s negative control on %s: the planted oracle turned make %s red\n", *plant, *leg, *target)
}
