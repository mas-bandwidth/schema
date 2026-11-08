// Command pgroup runs a command in a process group of its own and kills the
// whole group when the command ends, however it ends.
//
//	pgroup [-wall 15m] [-grace 10s] -- command [args...]
//
// WHY. A test that starts an interpreter (sq, lua, luau, moo, lobster,
// elixir) and then dies — go test's -timeout panic, a t.Fatal before the
// cleanup ran — leaves that interpreter running, reparented to init and still
// holding the step's stdout. go test itself gives up on the pipe after its
// WaitDelay, but the orphan keeps the runner's step open, and the runner
// takes no other work while it is.
//
// Every self-hosted job in .github/workflows runs its `run:` steps through
// this command (the job's defaults.run.shell), so the step's shell, make,
// go test, the test binary and everything they start share one group, and
// the group dies with the step.
//
// WHAT IT DOES.
//   - The command exits: SIGKILL to the group, then exit with the command's
//     status (128+n for a command killed by signal n).
//   - SIGINT, SIGTERM or SIGHUP arrives, or pgroup's parent goes away: the
//     signal (SIGTERM for a lost parent) goes to the whole group, and after
//     -grace the group gets SIGKILL.
//   - -wall elapses: the budget is named on stderr, the group gets SIGTERM,
//     after -grace SIGKILL, and pgroup exits 124.
//
// A process that leaves the group on purpose (setsid, Setpgid) is outside
// this; nothing the repository runs under CI does.
//
// The policy is a pure function (supervise.go), modeled in tla/PGroup.tla.
// On Windows there are no process groups: the command runs as a plain child
// and a stop or the wall kills it.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	fs := flag.NewFlagSet("pgroup", flag.ExitOnError)
	wall := fs.Duration("wall", 0, "kill the group after this long (0: no budget)")
	grace := fs.Duration("grace", 10*time.Second, "how long a stopped group gets before SIGKILL")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: pgroup [-wall DURATION] [-grace DURATION] -- command [args...]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(2)
	}
	code, err := run(fs.Args(), *wall, *grace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pgroup: %v\n", err)
	}
	os.Exit(code)
}
