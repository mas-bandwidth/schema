// THE LEDGER: a hostile-corpus runner proves it ran.
//
// Every port's runner loops over the manifest, and a loop can stop early, drop
// a carrier, or run zero times and still print a summary whose total is the
// manifest's length. The ledger is the count that is not the loop's own: what
// the corpus FOLDER registers (the manifest's rows, checked against the files
// in inputs/), against the cases the port's reader actually gave a verdict
// for, carrier by carrier. A case is either RAN or held by NAME with a reason;
// anything else is red, and so is a run of nothing.
//
// A Go runner keeps a Ledger and calls Check. A runner in another language
// prints one block of lines when it is done,
//
//	hostile-ledger ran <carrier> <cases the reader answered>
//	hostile-ledger end
//
// and tools/hostileledger holds that block to the folder (CheckLog).
package hostilecorpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Registered reads what a corpus folder registers: the manifest's cases, held
// to the folder itself. A manifest with no case is refused, and so is an
// inputs/ file no row names: either is the registry having lost rows, which
// no runner that reads only the manifest can see.
func Registered(dir string) ([]Case, error) {
	_, cases, err := LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s registers no case: a runner over it compares nothing", filepath.Join(dir, ManifestName))
	}
	cited := map[string]bool{}
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			return nil, fmt.Errorf("%s names the case %s twice", filepath.Join(dir, ManifestName), c.Name)
		}
		names[c.Name] = true
		cited[strings.TrimPrefix(c.Input, "inputs/")] = true
	}
	// A folder may carry a second manifest in the same format beside the one
	// the runners read (corpus/number-token.tsv, which the ports move to one
	// leg at a time): its rows name inputs too, and are not cases of this one.
	others, err := filepath.Glob(filepath.Join(dir, "*.tsv"))
	if err != nil {
		return nil, err
	}
	for _, path := range others {
		if filepath.Base(path) == ManifestName {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rows, err := Parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, c := range rows {
			cited[strings.TrimPrefix(c.Input, "inputs/")] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "inputs"))
	if err != nil {
		return nil, err
	}
	var orphans []string
	for _, e := range entries {
		if !cited[e.Name()] {
			orphans = append(orphans, e.Name())
		}
	}
	if len(orphans) > 0 {
		return nil, fmt.Errorf("%s: %d file(s) under inputs/ that no manifest row names (%s): the manifest lost rows, or the files are stale",
			dir, len(orphans), strings.Join(orphans, ", "))
	}
	return cases, nil
}

// Held is one named exemption: a case, or a whole carrier, a port does not
// run, and one line saying why.
type Held map[string]string

// LoadHeld reads a held-by-name list: `name<TAB>reason` lines, `#` comments
// and blank lines ignored. An empty path is an empty list.
func LoadHeld(path string) (Held, error) {
	out := Held{}
	if path == "" {
		return out, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, why, ok := strings.Cut(line, "\t")
		if !ok || name == "" || strings.TrimSpace(why) == "" {
			return nil, fmt.Errorf("%s:%d: a held line is a name and one line saying why, tab-separated", path, n+1)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%s:%d: %s is held twice", path, n+1, name)
		}
		out[name] = why
	}
	return out, nil
}

// Ledger counts the cases one run gave a verdict for, by name.
type Ledger struct {
	cases   []Case
	carrier map[string]string
	ran     map[string]int
	skipped map[string]string
	unknown []string
}

// NewLedger starts a ledger over the registered cases.
func NewLedger(cases []Case) *Ledger {
	l := &Ledger{cases: cases, carrier: map[string]string{}, ran: map[string]int{}, skipped: map[string]string{}}
	for _, c := range cases {
		l.carrier[c.Name] = c.Carrier
	}
	return l
}

// Ran records that the port's reader gave this case a verdict, agreeing or
// not. A crash, a timeout and a wrong answer all count: the case was run.
func (l *Ledger) Ran(name string) {
	if _, ok := l.carrier[name]; !ok {
		l.unknown = append(l.unknown, name)
		return
	}
	l.ran[name]++
}

// Skipped records a case the run did not put to the reader, held by name.
func (l *Ledger) Skipped(name, why string) {
	if _, ok := l.carrier[name]; !ok {
		l.unknown = append(l.unknown, name)
		return
	}
	l.skipped[name] = why
}

// Counts is one run's tally per carrier.
type Counts map[string]int

// counts tallies the ledger by carrier.
func (l *Ledger) counts() (registered, ran, skipped Counts) {
	registered, ran, skipped = Counts{}, Counts{}, Counts{}
	for _, c := range l.cases {
		registered[c.Carrier]++
		if l.ran[c.Name] > 0 {
			ran[c.Carrier]++
		} else if _, ok := l.skipped[c.Name]; ok {
			skipped[c.Carrier]++
		}
	}
	return registered, ran, skipped
}

// Check is the assertion: every registered case ran exactly once or is
// skipped by name, no case ran that the folder does not register, and the run
// was not a run of nothing.
func (l *Ledger) Check() error {
	var bad []string
	if len(l.cases) == 0 {
		bad = append(bad, "the corpus registers no case")
	}
	total := 0
	for _, c := range l.cases {
		n := l.ran[c.Name]
		_, skip := l.skipped[c.Name]
		total += n
		switch {
		case n > 1:
			bad = append(bad, fmt.Sprintf("%s (%s) ran %d times", c.Name, c.Carrier, n))
		case n == 1 && skip:
			bad = append(bad, fmt.Sprintf("%s (%s) ran and is also skipped", c.Name, c.Carrier))
		case n == 0 && !skip:
			bad = append(bad, fmt.Sprintf("%s (%s) is registered and was not run", c.Name, c.Carrier))
		}
	}
	for _, name := range l.unknown {
		bad = append(bad, fmt.Sprintf("%s was run and the corpus does not register it", name))
	}
	if len(l.cases) > 0 && total == 0 {
		bad = append(bad, "no case ran: the run compared nothing")
	}
	if len(bad) > 0 {
		return fmt.Errorf("hostile-corpus ledger: %s\n  %s", l.Summary(), strings.Join(bad, "\n  "))
	}
	return nil
}

// Summary is the one line a green run prints: cases run of cases registered,
// per carrier.
func (l *Ledger) Summary() string {
	registered, ran, skipped := l.counts()
	return summary(registered, ran, skipped)
}

func summary(registered, ran, skipped Counts) string {
	var carriers []string
	nr, nran, nskip := 0, 0, 0
	for c, n := range registered {
		carriers = append(carriers, c)
		nr += n
		nran += ran[c]
		nskip += skipped[c]
	}
	for c := range ran {
		if _, ok := registered[c]; !ok {
			carriers = append(carriers, c)
			nran += ran[c]
		}
	}
	sort.Strings(carriers)
	parts := make([]string, 0, len(carriers))
	for _, c := range carriers {
		parts = append(parts, fmt.Sprintf("%s %d/%d", c, ran[c], registered[c]))
	}
	return fmt.Sprintf("ran %d of %d registered, %d held by name (%s)", nran, nr, nskip, strings.Join(parts, ", "))
}

// LedgerBlocks reads every `hostile-ledger` block a runner printed: the cases
// it ran per carrier, one block per run of the corpus.
func LedgerBlocks(log string) ([]Counts, error) {
	var out []Counts
	var cur Counts
	for n, line := range strings.Split(log, "\n") {
		line = strings.TrimRight(line, "\r")
		rest, ok := strings.CutPrefix(line, "hostile-ledger ")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		switch {
		case len(f) == 1 && f[0] == "end":
			if cur == nil {
				cur = Counts{}
			}
			out = append(out, cur)
			cur = nil
		case len(f) == 3 && f[0] == "ran":
			v, err := strconv.Atoi(f[2])
			if err != nil || v < 0 {
				return nil, fmt.Errorf("log line %d: %q is not a count", n+1, line)
			}
			if cur == nil {
				cur = Counts{}
			}
			if _, dup := cur[f[1]]; dup {
				return nil, fmt.Errorf("log line %d: the carrier %s is counted twice in one block", n+1, f[1])
			}
			cur[f[1]] = v
		default:
			return nil, fmt.Errorf("log line %d: %q is not a ledger line", n+1, line)
		}
	}
	if cur != nil {
		return nil, fmt.Errorf("the log's last hostile-ledger block has no `hostile-ledger end`: the runner stopped before it finished")
	}
	return out, nil
}

// CheckLog holds a runner's printed ledger to the folder. runs is how many
// times the runner went over the corpus (the Rust runner runs it once per
// build profile). heldCases names cases the port does not run, heldCarriers
// whole carriers it has no reader for; a held name that is run, or that the
// corpus no longer registers, is stale and red. It returns the green summary.
func CheckLog(cases []Case, log string, runs int, heldCases, heldCarriers Held) (string, error) {
	blocks, err := LedgerBlocks(log)
	if err != nil {
		return "", err
	}
	if len(blocks) != runs {
		return "", fmt.Errorf("hostile-corpus ledger: the runner printed %d ledger block(s) and owes %d: a run that prints none did not finish", len(blocks), runs)
	}
	registered, owed, held := Counts{}, Counts{}, Counts{}
	names := map[string]bool{}
	for _, c := range cases {
		names[c.Name] = true
		registered[c.Carrier]++
		_, heldCase := heldCases[c.Name]
		_, heldCarrier := heldCarriers[c.Carrier]
		if heldCase && heldCarrier {
			return "", fmt.Errorf("hostile-corpus ledger: %s is held by name and its carrier %s is held too: drop one", c.Name, c.Carrier)
		}
		if heldCase || heldCarrier {
			held[c.Carrier]++
		} else {
			owed[c.Carrier]++
		}
	}
	if len(cases) == 0 {
		return "", fmt.Errorf("hostile-corpus ledger: the corpus registers no case")
	}
	var bad []string
	for name := range heldCases {
		if !names[name] {
			bad = append(bad, fmt.Sprintf("the held case %s is not in the corpus: delete its line", name))
		}
	}
	for carrier := range heldCarriers {
		if registered[carrier] == 0 {
			bad = append(bad, fmt.Sprintf("the held carrier %s has no case in the corpus: delete its line", carrier))
		}
	}
	var last string
	for i, ran := range blocks {
		total := 0
		for carrier, n := range ran {
			total += n
			if registered[carrier] == 0 {
				bad = append(bad, fmt.Sprintf("run %d: %d case(s) ran on the carrier %s, which the corpus does not register", i+1, n, carrier))
			}
		}
		for carrier, n := range registered {
			if ran[carrier] != owed[carrier] {
				bad = append(bad, fmt.Sprintf("run %d: carrier %s ran %d case(s) and owes %d (%d registered, %d held by name)",
					i+1, carrier, ran[carrier], owed[carrier], n, held[carrier]))
			}
		}
		if total == 0 {
			bad = append(bad, fmt.Sprintf("run %d: no case ran: the run compared nothing", i+1))
		}
		last = summary(registered, ran, held)
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		return "", fmt.Errorf("hostile-corpus ledger:\n  %s", strings.Join(bad, "\n  "))
	}
	return last, nil
}

// RanEvery is the ledger for a Go runner that collects its answers by name:
// it reads what the folder registers, counts the names the port's reader
// answered, and fails unless every registered case ran once or is held by
// name. It returns the green summary line.
func RanEvery(dir string, ran []string, held Held) (string, error) {
	cases, err := Registered(dir)
	if err != nil {
		return "", err
	}
	l := NewLedger(cases)
	for _, name := range ran {
		l.Ran(name)
	}
	for name, why := range held {
		l.Skipped(name, why)
	}
	if err := l.Check(); err != nil {
		return "", err
	}
	return l.Summary(), nil
}
