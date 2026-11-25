// Command hostilecorpusexec runs the shared hostile-input corpus
// (docs/HOSTILE-CORPUS.md) against a port whose readers are native code: C and
// C++. The port supplies a CASE BINARY that answers one manifest line per
// process; this driver owns everything else, so one crash, sanitizer report or
// hang costs only its own case:
//
//	hostilecorpusexec run -port c -exec build/c-hostile/case \
//	    -corpus internal/ci/hostilecorpus/corpus
//	hostilecorpusexec probe-schema <out>   # write the corpus's probe schema
//
// The case binary is invoked as `<exec> <carrier> <schema> <table> <input>` and
// answers on stdout (test/c-hostile/hostile.h names the shape):
//
//	accept text\n<bytes>    the port's text writer's output; the digest is its
//	                        SHA-256 with one trailing newline removed (the
//	                        engine's canonical text carries none)
//	accept bytes\n<bytes>   a packet re-written by the port's own writer
//	accept -\n              an accepted announcement: no digest
//	refuse <reason>\n       a refusal; the reason is the port's own diagnostic
//	                        and is never compared (refusal-verdict-only)
//	skip <why>\n            a carrier the port has no API for
//	unbound <what>\n        the runner names no binding: the runner's defect
//	error <what>\n          the port could not answer
//
// A non-zero exit, a signal, a sanitizer report on stderr, or a case past its
// budget (5 s, HOSTILE_CASE_BUDGET_MS overrides) is a FAIL for that case, and
// so is every disagreement with the manifest: there is no list that carries a
// finding (docs/rulings.json no-skips), so each one reads red by its case's
// name. A skip is a failure too, so a carrier the port has no API for, or a
// binding dropped from the case binary, reads red by name. Which cases run is chosen at the root. Reads files only: no network.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "probe-schema" {
		if len(os.Args) != 3 {
			fatal(errors.New("usage: hostilecorpusexec probe-schema <out>"))
		}
		if err := os.MkdirAll(filepath.Dir(os.Args[2]), 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(os.Args[2], []byte(hostilecorpus.ProbeSchema), 0o644); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fatal(errors.New("usage: hostilecorpusexec run -port NAME -exec BINARY -corpus DIR [-jobs N] [-case NAME]"))
	}
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	port := flags.String("port", "", "the port's name, for the report")
	binary := flags.String("exec", "", "the port's case binary")
	corpus := flags.String("corpus", "internal/ci/hostilecorpus/corpus", "corpus directory")
	jobs := flags.Int("jobs", defaultJobs(), "cases run at once")
	only := flags.String("case", "", "run one case and print its raw answer")
	if err := flags.Parse(os.Args[2:]); err != nil {
		fatal(err)
	}
	if *port == "" || *binary == "" {
		fatal(errors.New("run needs -port and -exec"))
	}
	budget := 5 * time.Second
	if v := os.Getenv("HOSTILE_CASE_BUDGET_MS"); v != "" {
		ms, err := strconv.Atoi(v)
		if err != nil || ms <= 0 {
			fatal(fmt.Errorf("HOSTILE_CASE_BUDGET_MS=%q is not a positive integer", v))
		}
		budget = time.Duration(ms) * time.Millisecond
	}
	ok, err := run(*port, *binary, *corpus, *only, *jobs, budget)
	if err != nil {
		fatal(err)
	}
	if !ok {
		os.Exit(1)
	}
}

// defaultJobs leaves half the machine to everything else: each case is a
// sanitizer build's whole process.
func defaultJobs() int {
	return max(1, runtime.NumCPU()/2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "hostilecorpusexec:", err)
	os.Exit(2)
}

// answer is one case's outcome: kind is accept, refuse, skip or fail, and
// detail is the digest, the reason, the skip's why, or what went wrong.
type answer struct {
	kind   string
	detail string
}

func (a answer) String() string { return a.kind + " " + a.detail }

func run(port, binary, corpus, only string, jobs int, budget time.Duration) (bool, error) {
	cases, err := hostilecorpus.Registered(corpus)
	if err != nil {
		return false, err
	}
	if only != "" {
		for _, c := range cases {
			if c.Name == only {
				a, raw := runCase(binary, corpus, c, budget)
				fmt.Printf("%s\n--- raw stdout ---\n%s\n", a, raw)
				return true, nil
			}
		}
		return false, fmt.Errorf("no case %s", only)
	}
	if jobs < 1 {
		jobs = 1
	}
	answers := make([]answer, len(cases))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < jobs; w++ {
		wg.Go(func() {
			for i := range next {
				answers[i], _ = runCase(binary, corpus, cases[i], budget)
			}
		})
	}
	for i := range cases {
		next <- i
	}
	close(next)
	wg.Wait()

	var pass, skipped, unexplained int
	// THE LEDGER (internal/ci/hostilecorpus/ledger.go): cases the binary
	// answered against cases the folder registers, per carrier. answers is
	// filled by the workers; a slot none of them reached has no kind.
	ledger := hostilecorpus.NewLedger(cases)
	for i, c := range cases {
		got := answers[i]
		if got.kind != "skip" && got.kind != "" {
			ledger.Ran(c.Name)
		}
		want := expected(c)
		switch {
		case got.kind == "skip":
			// a skip is a failure: a case that did not run is never a pass
			fmt.Printf("FAIL %s carrier=%s: skipped, and a skip is a failure: %s\n", c.Name, c.Carrier, got.detail)
			skipped++
			unexplained++
		case agrees(got, want):
			pass++
		default:
			fmt.Printf("FAIL %s carrier=%s got=%s want=%s\n", c.Name, c.Carrier, got, want)
			unexplained++
		}
	}
	// the per-case block (internal/ci/hostilecorpus/verdicts.go): a ran
	// line per case the binary answered, then the ledger, then the end, so
	// a ledger that does not close is inside the block the registry reads
	for i, c := range cases {
		if k := answers[i].kind; k != "skip" && k != "" {
			fmt.Printf("hostile-ledger ran %s\n", c.Name)
		}
	}
	ledgerErr := ledger.Check()
	if ledgerErr != nil {
		fmt.Printf("FAIL %s %v\n", port, ledgerErr)
	}
	fmt.Println("hostile-ledger end")
	fmt.Printf("%s: %d cases: %d pass, %d skipped, %d unexplained\n",
		port, len(cases), pass, skipped, unexplained)
	if ledgerErr != nil {
		return false, nil
	}
	fmt.Printf("%s hostile-corpus ledger: %s\n", port, ledger.Summary())
	return unexplained == 0, nil
}

func expected(c hostilecorpus.Case) answer {
	if c.Verdict == hostilecorpus.Accept {
		return answer{"accept", c.Digest}
	}
	return answer{"refuse", c.Reason}
}

// agrees reports whether a port's answer conforms to the manifest's. A refusal
// conforms on its verdict alone (docs/rulings.json refusal-verdict-only,
// SPEC-TABLES §6.5 and §13.10): the reason a port names is its own diagnostic
// detail, printed on a FAIL line but never compared. An accept conforms on the
// digest of its decoded value.
func agrees(got, want answer) bool {
	if got.kind == "refuse" && want.kind == "refuse" {
		return true
	}
	return got == want
}

// sanitizerMarks are the lines a sanitizer prints when it reports; one on
// stderr fails the case whatever the exit status.
var sanitizerMarks = []string{"ERROR: AddressSanitizer", "ERROR: LeakSanitizer", "runtime error:", "SUMMARY: UndefinedBehaviorSanitizer", "WARNING: ThreadSanitizer"}

// startBudget starts a case's budget: the channel fires when it runs out, and
// stop releases it. The runner reads no clock but this one, so a test can
// hand it a budget that is already spent, or one that never runs out.
var startBudget = func(budget time.Duration) (fire <-chan time.Time, stop func() bool) {
	t := time.NewTimer(budget)
	return t.C, t.Stop
}

func runCase(binary, corpus string, c hostilecorpus.Case, budget time.Duration) (answer, string) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	fire, stop := startBudget(budget)
	defer stop()
	go func() {
		select {
		case <-fire:
			cancel(errOverBudget)
		case <-ctx.Done():
		}
	}()
	cmd := exec.CommandContext(ctx, binary, c.Carrier, c.Schema, c.Table, filepath.Join(corpus, c.Input))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// a killed case's descendants may hold its pipes open: stop waiting on them
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	raw := stdout.String()
	if errors.Is(context.Cause(ctx), errOverBudget) {
		return answer{"fail", fmt.Sprintf("hang: over the %v budget", budget)}, raw
	}
	for _, mark := range sanitizerMarks {
		if i := strings.Index(stderr.String(), mark); i >= 0 {
			return answer{"fail", "sanitizer: " + firstLine(stderr.String()[i:])}, raw
		}
	}
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return answer{"fail", fmt.Sprintf("crash: %s %s", exit.ProcessState, firstLine(stderr.String()))}, raw
		}
		return answer{"fail", "cannot run the case binary: " + err.Error()}, raw
	}
	head, body, _ := strings.Cut(raw, "\n")
	kind, detail, _ := strings.Cut(head, " ")
	switch kind {
	case "accept":
		switch detail {
		case "-":
			return answer{"accept", "-"}, raw
		case "text":
			body = strings.TrimSuffix(body, "\n")
		case "bytes":
		default:
			return answer{"fail", "unreadable answer: " + head}, raw
		}
		sum := sha256.Sum256([]byte(body))
		return answer{"accept", hex.EncodeToString(sum[:])}, raw
	case "refuse", "skip":
		return answer{kind, detail}, raw
	case "unbound":
		return answer{"fail", "unbound: " + detail}, raw
	case "error":
		return answer{"fail", "error: " + detail}, raw
	}
	return answer{"fail", "unreadable answer: " + firstLine(raw)}, raw
}

// errOverBudget is the cause a case is killed with when its budget runs out.
var errOverBudget = errors.New("over the case budget")

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
