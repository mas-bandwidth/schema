//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// run starts argv as the leader of a new process group and drives step until
// the policy reaches phaseDone.
func run(argv []string, wall, grace time.Duration) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	// *os.File streams, so the child writes straight to pgroup's own
	// descriptors and no copying goroutine waits on a pipe an orphan holds.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	parent := os.Getppid()
	if err := cmd.Start(); err != nil {
		return 127, err
	}
	pgid := cmd.Process.Pid

	exited := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		exited <- status(cmd, err)
	}()

	var wallC <-chan time.Time
	if wall > 0 {
		t := time.NewTimer(wall)
		defer t.Stop()
		wallC = t.C
	}
	var graceC <-chan time.Time
	// the parent watch: a runner that SIGKILLs the step's process cannot be
	// caught, but the change of parent can
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	stopSig := syscall.Signal(syscall.SIGTERM)
	s := state{}
	command := 0
	for s.phase != phaseDone {
		var e event
		select {
		case command = <-exited:
			e = evExit
		case sig := <-sigs:
			stopSig = sig.(syscall.Signal)
			e = evStop
		case <-tick.C:
			if os.Getppid() == parent {
				continue
			}
			stopSig = syscall.SIGTERM
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
			case actForward:
				_ = syscall.Kill(-pgid, stopSig)
			case actTerm:
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
			case actStartGrace:
				graceC = time.After(grace)
			case actKillGroup:
				// ESRCH is the common case: nothing outlived the leader
				if err := syscall.Kill(-pgid, syscall.SIGKILL); err == nil && e == evExit {
					fmt.Fprintf(os.Stderr, "pgroup: killed what %s left running in its process group\n", argv[0])
				}
			case actReportWall:
				fmt.Fprintf(os.Stderr, "pgroup: %s ran past its %s wall clock; stopping its process group\n", argv[0], wall)
			}
		}
	}
	return exitCode(s, command), nil
}

// status is the command's exit status as a shell reports it.
func status(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return 1
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return cmd.ProcessState.ExitCode()
}
