package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func readOwnerPid(ownerFile, lockPath string) string {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		if ownerFile != "" {
			if data, err := os.ReadFile(ownerFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
				fields := strings.Fields(string(data))
				if len(fields) > 0 && isAlive(fields[0]) {
					return fields[0]
				}
			}
		}
		if lockPath != "" {
			if data, err := os.ReadFile(lockPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
				fields := strings.Fields(string(data))
				if len(fields) > 0 && isAlive(fields[0]) {
					return fields[0]
				}
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if ownerFile != "" {
		if data, err := os.ReadFile(ownerFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
			fields := strings.Fields(string(data))
			if len(fields) > 0 && fields[0] != "" {
				return fields[0]
			}
		}
	}
	if lockPath != "" {
		if data, err := os.ReadFile(lockPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
			fields := strings.Fields(string(data))
			if len(fields) > 0 && fields[0] != "" {
				return fields[0]
			}
		}
	}
	return "unknown"
}

func main() {
	var (
		lockPath  = flag.String("lock", "", "path to lock file")
		ownerFile = flag.String("owner-file", "", "path to owner file to inspect on contention")
		lockName  = flag.String("name", "", "name of lock to display in refusal messages")
	)
	flag.Parse()

	if *lockPath == "" {
		fmt.Fprintf(os.Stderr, "usage: treelock -lock <lockfile> [-owner-file <ownerfile>] [-name <name>] <command> [args...]\n")
		os.Exit(2)
	}

	cmdArgs := flag.Args()
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "treelock: no command specified\n")
		os.Exit(2)
	}

	name := *lockName
	if name == "" {
		name = *lockPath
	}

	lockDir := filepath.Dir(*lockPath)
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "treelock: mkdir %s: %v\n", lockDir, err)
		os.Exit(1)
	}

	f, err := os.OpenFile(*lockPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "treelock: open %s: %v\n", *lockPath, err)
		os.Exit(1)
	}
	defer f.Close()

	if err := acquireFlock(f.Fd()); err != nil {
		if isLockBlocked(err) {
			ownerPid := readOwnerPid(*ownerFile, *lockPath)
			fmt.Fprintf(os.Stderr, "REFUSED — another generated-tree verification (pid %s) holds %s; not touching either tree.\n", ownerPid, name)
			os.Exit(1)
		}
		if strings.HasPrefix(err.Error(), "treelock:") {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "treelock: flock %s: %v\n", *lockPath, err)
		}
		os.Exit(1)
	}

	// We now hold the exclusive kernel advisory lock.
	// Record our PID in the lock file immediately so any contender has an immediate owner pid.
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Sync()

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Pass the open lock file description to the child process so that the child
	// inherits the lock descriptor without O_CLOEXEC. Under Unix/Darwin/Linux semantics,
	// the flock remains held as long as any alive process references that open file description.
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = append(os.Environ(), "TREELOCK_HELD=1")

	sigChan := make(chan os.Signal, 8)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "treelock: start %s: %v\n", cmdArgs[0], err)
		os.Exit(1)
	}

	// Update lock file with child's PID as well.
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "%d\n", cmd.Process.Pid)
	_ = f.Sync()

	go func() {
		for sig := range sigChan {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()

	waitErr := cmd.Wait()
	signal.Stop(sigChan)
	close(sigChan)

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					os.Exit(128 + int(status.Signal()))
				}
				os.Exit(status.ExitStatus())
			}
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
	os.Exit(0)
}
