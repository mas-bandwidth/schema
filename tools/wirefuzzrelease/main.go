// wirefuzzrelease scores the release-tier wire-fuzz gate
// (make/wire-fuzz-release.mk): one run into one row, and a leg's rows into its
// table and verdict. It is the gate's judgement, moved out of shell so that it
// has unit tests; make/checks/wire-fuzz-release-run.sh still runs the harness.
//
//	wirefuzzrelease row   -leg L -arm A -seed S -dir D (-no-driver | -status N -secs N -n N)
//	wirefuzzrelease table -leg L -dir D -arms "plain retain" -seeds "1 2 3" -n N
//
// `row` reads <dir>/<leg>.<arm>.<seed>.log and writes <dir>/<leg>.<arm>.<seed>.row:
// leg, arm, seed, verdict, tab-separated. A run is green only when the harness
// exited 0 AND its summary line proves the run: no divergence, at least one
// seed over at least one root, the random mutants the gate asked for (-n) and
// not merely some, and no seed absent. That is the rule for every leg, and
// there is no list that excuses an arm or a seed: a leg with no driver, an
// arm that runs red and a seed the leg has no codec for are each RED. Which
// legs and arms run is chosen at the root, never excused below it.
//
// `table` holds a leg to its ROSTER, which is derived (every arm at every
// seed), never read off the rows that happen to be on disk: each expected row
// is read by name, and a row that is missing, empty, or carries another run's
// coordinates is RED. It used to concatenate whatever row files it found, so
// a leg with five rows of six printed "every arm passes".
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "row":
		fs := flag.NewFlagSet("row", flag.ExitOnError)
		leg := fs.String("leg", "", "the leg")
		arm := fs.String("arm", "", "plain or retain")
		seed := fs.String("seed", "", "the run's seed")
		dir := fs.String("dir", "", "where the run's log is and its row goes")
		noDriver := fs.Bool("no-driver", false, "the leg has no wire-fuzz driver")
		status := fs.Int("status", -1, "the harness's exit status")
		secs := fs.Int("secs", 0, "how long the run took")
		asked := fs.Int("n", 0, "the random mutant count the gate asked the harness for")
		_ = fs.Parse(os.Args[2:])
		if *leg == "" || *arm == "" || *seed == "" || *dir == "" || (!*noDriver && (*status < 0 || *asked <= 0)) {
			usage()
		}
		if err := writeRow(*leg, *arm, *seed, *dir, *noDriver, *status, *secs, *asked); err != nil {
			fmt.Fprintln(os.Stderr, "wirefuzzrelease:", err)
			os.Exit(2)
		}
	case "table":
		fs := flag.NewFlagSet("table", flag.ExitOnError)
		leg := fs.String("leg", "", "the leg")
		dir := fs.String("dir", "", "where the rows are")
		arms := fs.String("arms", "", "the roster's arms")
		seeds := fs.String("seeds", "", "the roster's seeds")
		n := fs.String("n", "", "the mutant count asked for (printed)")
		_ = fs.Parse(os.Args[2:])
		if *leg == "" || *dir == "" {
			usage()
		}
		out, ok, err := table(*leg, *dir, strings.Fields(*arms), strings.Fields(*seeds), *n)
		fmt.Print(out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefuzzrelease:", err)
			os.Exit(2)
		}
		if !ok {
			os.Exit(1)
		}
	case "cases":
		dir := GateCasesDir
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		scratch, err := os.MkdirTemp("", "wire-fuzz-release-cases-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefuzzrelease:", err)
			os.Exit(2)
		}
		lines, err := runGateCases(dir, scratch)
		_ = os.RemoveAll(scratch) // errdiscard: best-effort cleanup of temp scratch dir after replay
		for _, line := range lines {
			fmt.Println(line)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "wirefuzzrelease:", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wirefuzzrelease row -leg L -arm A -seed S -dir D (-no-driver | -status N -secs N -n N)\n"+
		"       wirefuzzrelease table -leg L -dir D -arms \"plain retain\" -seeds \"1 2 3\" -n N\n"+
		"       wirefuzzrelease cases [dir]")
	os.Exit(2)
}

func writeRow(leg, arm, seed, dir string, noDriver bool, status, secs, asked int) error {
	base := filepath.Join(dir, leg+"."+arm+"."+seed)
	var verdict string
	if noDriver {
		verdict = "RED: the leg has no wire-fuzz driver"
	} else {
		log, err := os.ReadFile(base + ".log")
		if err != nil {
			return err
		}
		verdict = score(arm, seed, string(log), status, secs, asked)
	}
	verdict = strings.NewReplacer("\t", " ", "\n", " ").Replace(verdict)
	if len(verdict) > 400 {
		verdict = verdict[:400]
	}
	return os.WriteFile(base+".row", []byte(leg+"\t"+arm+"\t"+seed+"\t"+verdict+"\n"), 0o644)
}

var (
	seedRE     = regexp.MustCompile(`: seed (\d+),`)
	summaryRE  = regexp.MustCompile(`(\d+) seeds over (\d+) roots`)
	absentRE   = regexp.MustCompile(`(\d+) seeds absent`)
	mutantsRE  = regexp.MustCompile(`(\d+) enumerated \+ (\d+) random = (\d+) mutants`)
	zeroDivRE  = regexp.MustCompile(`(^|[^0-9])0 divergences`)
	failedRE   = regexp.MustCompile(`FAILED after (\d+) mutants: (.*)`)
	whereRE    = regexp.MustCompile(`(?m)^ *corpus seed [^ ]+ \(([^)]*)\), pass ([^,]+),`)
	errorishRE = regexp.MustCompile(`FAILED|harness:|wire-fuzz:|panic|error`)
)

// score is one run's verdict, from the harness's exit status and its log.
func score(arm, seed, log string, status, secs, asked int) string {
	red := runRed(arm, seed, log, status, asked)
	if red == "" {
		mutants := mutantsRE.FindAllStringSubmatch(log, -1)
		m := mutants[len(mutants)-1][3]
		return fmt.Sprintf("pass (%s mutants, %d s)", m, secs)
	}
	return red
}

// runRed says why a run is not green, "" when it is. A green run exited 0 and
// its summary line proves what it ran: the random pass is exactly the count
// the gate asked for (the harness draws --n random mutants after the
// enumerated ones), so a run of one mutant under a budget of 100,000 is red.
func runRed(arm, seed, log string, status, asked int) string {
	if status != 0 {
		if f := failedRE.FindStringSubmatch(log); f != nil {
			// THE FIRST DIVERGENCE: the root, the mutant after the ones that
			// passed, and what differs
			where := ""
			if w := whereRE.FindStringSubmatch(log); w != nil {
				where = w[1] + ", pass " + w[2]
			}
			passed, err := strconv.Atoi(f[1])
			if err != nil {
				return fmt.Sprintf("RED: %s, mutant after %s: %s", where, f[1], f[2])
			}
			return fmt.Sprintf("RED: %s, mutant %d: %s", where, passed+1, f[2])
		}
		return fmt.Sprintf("RED: exit %d: %s", status, firstErrorLine(log))
	}
	summary := ""
	for line := range strings.SplitSeq(log, "\n") {
		if summaryRE.MatchString(line) {
			summary = line
		}
	}
	if summary == "" {
		return "RED: exit 0 and no summary line: the run does not say what it ran: " + firstErrorLine(log)
	}
	if !zeroDivRE.MatchString(summary) {
		return "RED: exit 0 and the summary does not say 0 divergences: " + summary
	}
	// THE RUN'S COORDINATES: the summary is this arm's at this seed, not a
	// line another run left in the log's place
	label, _, _ := strings.Cut(summary, ":")
	if strings.Contains(label, "retain") != (arm == "retain") {
		return fmt.Sprintf("RED: the summary is not the %s arm's: %s", arm, summary)
	}
	if c := seedRE.FindStringSubmatch(summary); c == nil || c[1] != seed {
		return fmt.Sprintf("RED: the summary is not seed %s's: %s", seed, summary)
	}
	s := summaryRE.FindStringSubmatch(summary)
	seeds, err := strconv.Atoi(s[1])
	if err != nil {
		return "RED: malformed wire-fuzz seed count: " + summary
	}
	roots, err := strconv.Atoi(s[2])
	if err != nil {
		return "RED: malformed wire-fuzz root count: " + summary
	}
	if seeds == 0 || roots == 0 {
		return fmt.Sprintf("RED: empty wire-fuzz roster: %d seeds over %d roots", seeds, roots)
	}
	m := mutantsRE.FindStringSubmatch(summary)
	if m == nil {
		return "RED: missing wire-fuzz mutation count: " + summary
	}
	enumerated, err := strconv.Atoi(m[1])
	if err != nil {
		return "RED: malformed wire-fuzz enumerated count: " + m[0]
	}
	random, err := strconv.Atoi(m[2])
	if err != nil {
		return "RED: malformed wire-fuzz random count: " + m[0]
	}
	total, err := strconv.Atoi(m[3])
	if err != nil {
		return "RED: malformed wire-fuzz total count: " + m[0]
	}
	switch {
	case total == 0:
		return "RED: empty wire-fuzz mutation count: 0 mutants over " + s[0]
	case enumerated+random != total:
		return fmt.Sprintf("RED: the summary's mutation counts do not add up: %s", m[0])
	case random != asked:
		return fmt.Sprintf("RED: short wire-fuzz run: %d random mutants ran and the gate asked for %d (%s)", random, asked, m[0])
	case enumerated == 0:
		return fmt.Sprintf("RED: no enumerated mutant ran: %s", m[0])
	}
	absent := 0
	if a := absentRE.FindStringSubmatch(summary); a != nil {
		absent, err = strconv.Atoi(a[1])
		if err != nil {
			return "RED: malformed wire-fuzz absent count: " + summary
		}
	}
	if absent > 0 {
		return fmt.Sprintf("RED: incomplete wire-fuzz roster: %d seeds absent (roots the leg has no codec for): every seed runs", absent)
	}
	return ""
}

func firstErrorLine(log string) string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	for _, line := range lines {
		if errorishRE.MatchString(line) {
			return line
		}
	}
	return lines[len(lines)-1]
}

// table reads a leg's roster of rows by name, writes <dir>/<leg>.tsv, and
// answers the printed table and whether the leg is green.
func table(leg, dir string, arms, seeds []string, n string) (string, bool, error) {
	if len(arms) == 0 || len(seeds) == 0 {
		return "", false, fmt.Errorf("the roster is empty: arms %q, seeds %q: a gate over no run passes nothing", arms, seeds)
	}
	var out, tsv strings.Builder
	red, passed := 0, 0
	for _, arm := range arms {
		for _, seed := range seeds {
			name := leg + "." + arm + "." + seed + ".row"
			verdict := ""
			data, err := os.ReadFile(filepath.Join(dir, name))
			switch f := strings.Split(strings.TrimRight(string(data), "\n"), "\t"); {
			case err != nil:
				verdict = "RED: the run left no row (" + name + "): it did not finish, or its row was removed"
			case len(f) != 4 || f[0] != leg || f[1] != arm || f[2] != seed || strings.Contains(string(data[:max(len(data)-1, 0)]), "\n"):
				verdict = "RED: " + name + " is not this run's row: " + strings.Join(strings.Fields(string(data)), " ")
			default:
				verdict = f[3]
			}
			switch {
			case strings.HasPrefix(verdict, "pass ("):
				passed++
			case strings.HasPrefix(verdict, "RED"):
				red++
			default:
				verdict = "RED: not a verdict: " + verdict
				red++
			}
			fmt.Fprintf(&tsv, "%s\t%s\t%s\t%s\n", leg, arm, seed, verdict)
			fmt.Fprintf(&out, "%s | %s | %s | %s\n", leg, arm, seed, verdict)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return out.String(), false, err
	}
	if err := os.WriteFile(filepath.Join(dir, leg+".tsv"), []byte(tsv.String()), 0o644); err != nil {
		return out.String(), false, err
	}
	want := len(arms) * len(seeds)
	if red > 0 {
		fmt.Fprintf(&out, "tables-wire-fuzz-release-%s: RED on %d of %d rows (logs and mutants under %s/%s.*)\n", leg, red, want, dir, leg)
		return out.String(), false, nil
	}
	fmt.Fprintf(&out, "tables-wire-fuzz-release-%s: all %d rows of the roster are here and pass (%d), arms %s, seeds %s at N=%s\n",
		leg, want, passed, strings.Join(arms, " "), strings.Join(seeds, " "), n)
	return out.String(), true, nil
}
