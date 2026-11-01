// Command hostilecorpusexec runs the shared hostile-input corpus
// (docs/HOSTILE-CORPUS.md) against a port whose readers are native code: C and
// C++. The port supplies a CASE BINARY that answers one manifest line per
// process; this driver owns everything else, so one crash, sanitizer report or
// hang costs only its own case:
//
//	hostilecorpusexec run -port c -exec build/c-hostile/case \
//	    -corpus internal/ci/hostilecorpus/corpus -findings test/c-hostile/known-findings.tsv
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
//	refuse <reason>\n       a named refusal, or damaged
//	skip <why>\n            a carrier the port has no API for
//	unbound <what>\n        the runner names no binding: the runner's defect
//	error <what>\n          the port could not answer
//
// A non-zero exit, a signal, a sanitizer report on stderr, or a case past its
// budget (5 s, HOSTILE_CASE_BUDGET_MS overrides) is a FAIL for that case. A
// disagreement the port owes is carried in known-findings.tsv (name, slug,
// input, one line) and printed as a FIX-CARD; a disagreement no FIX-CARD names
// fails the run, and so does a FIX-CARD whose case agrees, is skipped, or is
// gone from the manifest. Reads files only: no network.
package main

import (
	"bufio"
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
		fatal(errors.New("usage: hostilecorpusexec run -port NAME -exec BINARY -corpus DIR -findings TSV [-jobs N] [-case NAME]"))
	}
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	port := flags.String("port", "", "the port's name, for the report")
	binary := flags.String("exec", "", "the port's case binary")
	corpus := flags.String("corpus", "internal/ci/hostilecorpus/corpus", "corpus directory")
	findings := flags.String("findings", "", "the port's known-findings.tsv")
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
	ok, err := run(*port, *binary, *corpus, *findings, *only, *jobs, budget)
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
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
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

// finding is one carried line of known-findings.tsv.
type finding struct {
	name, slug, input, line string
}

func loadFindings(path string) (map[string]finding, []string, error) {
	out := map[string]finding{}
	var order []string
	if path == "" {
		return out, order, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, order, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for n := 1; s.Scan(); n++ {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 || cols[3] == "" {
			return nil, nil, fmt.Errorf("%s:%d: a finding is name, slug, input and one line, tab-separated", path, n)
		}
		if _, dup := out[cols[0]]; dup {
			return nil, nil, fmt.Errorf("%s:%d: %s is carried twice", path, n, cols[0])
		}
		out[cols[0]] = finding{cols[0], cols[1], cols[2], cols[3]}
		order = append(order, cols[0])
	}
	return out, order, s.Err()
}

func run(port, binary, corpus, findingsPath, only string, jobs int, budget time.Duration) (bool, error) {
	_, cases, err := hostilecorpus.LoadManifest(corpus)
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
	findings, order, err := loadFindings(findingsPath)
	if err != nil {
		return false, err
	}
	if jobs < 1 {
		jobs = 1
	}
	answers := make([]answer, len(cases))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				answers[i], _ = runCase(binary, corpus, cases[i], budget)
			}
		}()
	}
	for i := range cases {
		next <- i
	}
	close(next)
	wg.Wait()

	var pass, carried, skipped, unexplained, stale int
	seen := map[string]bool{}
	for i, c := range cases {
		seen[c.Name] = true
		got := answers[i]
		want := expected(c)
		f, has := findings[c.Name]
		switch {
		case got.kind == "skip":
			skipped++
			fmt.Printf("SKIP %s carrier=%s: %s\n", c.Name, c.Carrier, got.detail)
			if has {
				fmt.Printf("STALE %s: FIX-CARD %s names a case this port skips\n", c.Name, f.slug)
				stale++
			}
		case got == want:
			if has {
				fmt.Printf("STALE %s: FIX-CARD %s no longer reproduces\n", c.Name, f.slug)
				stale++
			} else {
				pass++
			}
		case has && !strings.HasPrefix(got.detail, "unbound"):
			fmt.Printf("FIX-CARD: %s: %s %s (got %s, want %s)\n", f.slug, f.input, f.line, got, want)
			carried++
		default:
			fmt.Printf("FAIL %s carrier=%s got=%s want=%s\n", c.Name, c.Carrier, got, want)
			unexplained++
		}
	}
	for _, name := range order {
		if !seen[name] {
			fmt.Printf("STALE %s: FIX-CARD %s names no case the manifest carries\n", name, findings[name].slug)
			stale++
		}
	}
	fmt.Printf("%s: %d cases: %d pass, %d carried as FIX-CARD, %d skipped, %d unexplained, %d stale\n",
		port, len(cases), pass, carried, skipped, unexplained, stale)
	return unexplained == 0 && stale == 0, nil
}

func expected(c hostilecorpus.Case) answer {
	if c.Verdict == hostilecorpus.Accept {
		return answer{"accept", c.Digest}
	}
	return answer{"refuse", c.Reason}
}

// sanitizerMarks are the lines a sanitizer prints when it reports; one on
// stderr fails the case whatever the exit status.
var sanitizerMarks = []string{"ERROR: AddressSanitizer", "ERROR: LeakSanitizer", "runtime error:", "SUMMARY: UndefinedBehaviorSanitizer", "WARNING: ThreadSanitizer"}

func runCase(binary, corpus string, c hostilecorpus.Case, budget time.Duration) (answer, string) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, c.Carrier, c.Schema, c.Table, filepath.Join(corpus, c.Input))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// a killed case's descendants may hold its pipes open: stop waiting on them
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	raw := stdout.String()
	if ctx.Err() != nil {
		return answer{"fail", fmt.Sprintf("hang: over the %v budget", budget)}, raw
	}
	for _, mark := range sanitizerMarks {
		if i := strings.Index(stderr.String(), mark); i >= 0 {
			return answer{"fail", "sanitizer: " + firstLine(stderr.String()[i:])}, raw
		}
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
