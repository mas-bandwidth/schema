// hostileledger holds a hostile-corpus runner's count to the corpus folder
// (internal/ci/hostilecorpus/ledger.go; docs/HOSTILE-CORPUS.md, "The ledger").
//
// A runner in any language prints, when it is done, the cases its reader gave
// a verdict for, per carrier:
//
//	hostile-ledger ran <carrier> <n>
//	hostile-ledger end
//
// This tool reads the runner's log and the corpus folder and fails unless
// cases run equals cases registered on every carrier, less the cases and
// carriers a list holds by name. A log with no ledger, a run of nothing, a
// short count and a stale held name are all red. The registered count is read
// from the folder on every run; nothing here carries a number.
//
// After a green check it runs its own negative control on the same log: one
// more registered case than the runner ran must be red.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

func main() {
	port := flag.String("port", "", "the port the log is from (printed)")
	corpus := flag.String("corpus", "internal/ci/hostilecorpus/corpus", "the corpus folder")
	logPath := flag.String("log", "", "the runner's captured output")
	runs := flag.Int("runs", 1, "how many times the runner goes over the corpus")
	skips := flag.String("skips", "", "cases held by name: name<TAB>reason lines")
	carriers := flag.String("absent-carriers", "", "carriers held by name: carrier<TAB>reason lines")
	flag.Parse()
	if *port == "" || *logPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: hostileledger -port <port> -log <runner output> [-corpus dir] [-runs n] [-skips tsv] [-absent-carriers tsv]")
		os.Exit(2)
	}
	out, err := check(*port, *corpus, *logPath, *runs, *skips, *carriers)
	fmt.Print(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", *port, err)
		os.Exit(1)
	}
}

func check(port, corpus, logPath string, runs int, skipsPath, carriersPath string) (string, error) {
	cases, err := hostilecorpus.Registered(corpus)
	if err != nil {
		return "", err
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		return "", err
	}
	heldCases, err := hostilecorpus.LoadHeld(skipsPath)
	if err != nil {
		return "", err
	}
	heldCarriers, err := hostilecorpus.LoadHeld(carriersPath)
	if err != nil {
		return "", err
	}
	summary, err := hostilecorpus.CheckLog(cases, string(log), runs, heldCases, heldCarriers)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("%s hostile-corpus ledger: %s, %d run(s)\n", port, summary, runs)

	// THE NEGATIVE CONTROL, on this log: a corpus with one more case than the
	// runner ran is a runner one case short, and must be red.
	var owed string
	for _, c := range cases {
		_, a := heldCases[c.Name]
		_, b := heldCarriers[c.Carrier]
		if !a && !b {
			owed = c.Carrier
			break
		}
	}
	short := append(append([]hostilecorpus.Case{}, cases...), hostilecorpus.Case{Name: "\x00one-more", Carrier: owed})
	if _, err := hostilecorpus.CheckLog(short, string(log), runs, heldCases, heldCarriers); err == nil {
		return out, fmt.Errorf("NEGATIVE CONTROL FAILED: a runner one %s case short of the registry passed the ledger", owed)
	}
	out += fmt.Sprintf("%s hostile-corpus ledger negative control: one %s case short of the registry is red\n", port, owed)
	return out, nil
}
