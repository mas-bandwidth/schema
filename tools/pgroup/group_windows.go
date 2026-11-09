//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"time"
)

// run is the Windows form: there are no process groups to kill, so the
// command runs as a plain child and every kill the policy asks for is a kill
// of that child.
func run(argv []string, wall, grace time.Duration, wallLabel string, ciPolicy bool) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	if err := cmd.Start(); err != nil {
		return 127, err
	}
	exited := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		var ee *exec.ExitError
		switch {
		case err == nil:
			exited <- 0
		case errors.As(err, &ee):
			exited <- ee.ExitCode()
		default:
			exited <- 1
		}
	}()

	var wallC <-chan time.Time
	if wall > 0 {
		t := time.NewTimer(wall)
		defer t.Stop()
		wallC = t.C
	}
	var graceC <-chan time.Time

	s := state{}
	command := 0
	for s.phase != phaseDone {
		var e event
		select {
		case command = <-exited:
			e = evExit
		case <-sigs:
			e = evStop
		case <-wallC:
			e = evWall
		case <-graceC:
			e = evGrace
		}
		var acts []action
		s, acts = step(s, e)
		for _, a := range acts {
			switch a {
			case actForward, actTerm, actKillGroup:
				_ = cmd.Process.Kill()
			case actStartGrace:
				graceC = time.After(grace)
			case actReportWall:
				fmt.Fprintf(os.Stderr, "pgroup: %s ran past its %s wall clock; stopping it\n", argv[0], wall)
				if ciPolicy {
					fmt.Fprintln(os.Stderr, ciMessage(stepName(argv), wallLabel, wall))
				}
			}
		}
	}
	return exitCode(s, command), nil
}
