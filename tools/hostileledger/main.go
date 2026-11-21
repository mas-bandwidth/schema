// hostileledger holds a hostile-corpus runner's count to the corpus folder
// (internal/ci/hostilecorpus/ledger.go; docs/HOSTILE-CORPUS.md, "The ledger").
//
// A runner in any language prints one line per row its reader gave a verdict
// for, by the case's name, and closes the block when it is done:
//
//	hostile-ledger ran <case name>
//	hostile-ledger end
//
// This tool reads the runner's log and the corpus folder and compares the two
// SETS of names: every registered case ran (every row of it, each by its own
// id, where a carrier is read as several), no name ran that the folder does
// not register, and a case or a carrier a
// list holds by name did not run. A count is not enough: a run of the right
// number of the wrong cases is red by name. A log with no ledger, a run of
// nothing and a stale held name are red too. The names come from the folder
// on every run, never from the runner's own enumerator, and nothing here
// carries a number.
//
// After a green check it runs its own negative controls on the same log: the
// log with one case's lines taken out must be red, and so must the log with
// one case run under a name the folder does not register.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

func main() {
	port := flag.String("port", "", "the port the log is from (printed)")
	corpus := flag.String("corpus", "internal/ci/hostilecorpus/corpus", "the corpus folder")
	logPath := flag.String("log", "", "the runner's captured output")
	runs := flag.Int("runs", 1, "how many times the runner goes over the corpus")
	skips := flag.String("skips", "", "cases held by name: name<TAB>reason lines")
	carriers := flag.String("absent-carriers", "", "carriers held by name: carrier<TAB>reason lines")
	rows := flag.String("rows", "", "the rows a case of a carrier owes, each by its id suffix: <carrier>=<row>,<row>,... (a row prints as <case>@<row>)")
	ledgerCorpus := flag.String("ledger-corpus", "", "replay this committed corpus of ledger cases and exit (testdata/ledger-corpus)")
	flag.Parse()
	if *ledgerCorpus != "" {
		scratch, err := os.MkdirTemp("", "ledger-corpus-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		lines, err := runLedgerCorpus(*ledgerCorpus, scratch)
		_ = os.RemoveAll(scratch)
		for _, line := range lines {
			fmt.Println(line)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "hostileledger:", err)
			os.Exit(1)
		}
		return
	}
	if *port == "" || *logPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: hostileledger -port <port> -log <runner output> [-corpus dir] [-runs n] [-skips tsv] [-absent-carriers tsv] [-rows carrier=row,row]")
		os.Exit(2)
	}
	out, err := check(*port, *corpus, *logPath, *runs, *skips, *carriers, *rows)
	fmt.Print(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", *port, err)
		os.Exit(1)
	}
}

func check(port, corpus, logPath string, runs int, skipsPath, carriersPath, rowsSpec string) (string, error) {
	rows := map[string][]string{}
	for _, spec := range strings.Fields(rowsSpec) {
		carrier, list, ok := strings.Cut(spec, "=")
		names := strings.Split(list, ",")
		if !ok || carrier == "" || slices.Contains(names, "") || len(names) != len(slices.Compact(slices.Sorted(slices.Values(names)))) {
			return "", fmt.Errorf("-rows %q: want <carrier>=<row>,<row>,... with every row named once", spec)
		}
		rows[carrier] = names
	}
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
	summary, err := hostilecorpus.CheckLog(cases, string(log), runs, heldCases, heldCarriers, rows)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("%s hostile-corpus ledger: %s, %d run(s)\n", port, summary, runs)

	// THE NEGATIVE CONTROLS, on this log. One case's lines taken out is a
	// runner that dropped it; the same lines under another name is a runner
	// that ran the right NUMBER of cases and the wrong ones. Both must be red.
	// the controls edit a copy with LF line endings: a runner on a CRLF
	// platform prints the same records, and the parser already reads them
	text := strings.ReplaceAll(string(log), "\r\n", "\n")
	blocks, err := hostilecorpus.LedgerBlocks(text)
	if err != nil {
		return out, err
	}
	var victim string
	for _, c := range cases {
		if id := hostilecorpus.RowIDs(c, rows)[0]; blocks[0][id] > 0 {
			victim = id
			break
		}
	}
	line := "hostile-ledger ran " + victim + "\n"
	dropped := strings.ReplaceAll(text, line, "")
	if dropped == text {
		return out, fmt.Errorf("NEGATIVE CONTROL FAILED: the control found no line of %q to take out", victim)
	}
	if _, err := hostilecorpus.CheckLog(cases, dropped, runs, heldCases, heldCarriers, rows); err == nil || !strings.Contains(err.Error(), strings.SplitN(victim, "@", 2)[0]) {
		return out, fmt.Errorf("NEGATIVE CONTROL FAILED: the log without %s passed the ledger, or went red without naming it: %v", victim, err)
	}
	renamed := strings.ReplaceAll(text, line, "hostile-ledger ran "+victim+"-not-registered\n")
	if _, err := hostilecorpus.CheckLog(cases, renamed, runs, heldCases, heldCarriers, rows); err == nil || !strings.Contains(err.Error(), victim+"-not-registered ran and the corpus does not register it") {
		return out, fmt.Errorf("NEGATIVE CONTROL FAILED: the log with %s run under another name passed the ledger: %v", victim, err)
	}
	out += fmt.Sprintf("%s hostile-corpus ledger negative controls: %s dropped is red by name; %s run under an unregistered name (the same count) is red by name\n", port, victim, victim)
	return out, nil
}
