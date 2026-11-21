// Command releasetargets is certify.yml's release-gates planner: it prints, as
// a JSON array, every release target the job matrix runs, read from GNU Make's
// own database of the rules it defined (`make -pq`) and never from the make
// source text.
//
// F-066 (the reviewer, 2026-11-21): the planner it replaces grepped the source
// for '^tables-[a-z0-9]+-release:' and read WIRE_FUZZ_RELEASE_LEGS's first `:=`
// line with sed. A space before the colon of tables-go-release left the rule
// intact and removed its CI job; splitting the leg list into `:=` and a `+=`
// continuation kept all 36 rules and silently lost 17 jobs. Both planners
// exited 0. Make's database holds the expanded rules and the expanded list
// whatever the source's spelling, so this reads that.
//
// The plan is
//
//   - every rule make defines named tables-<lang>-release,
//   - tables-wire-fuzz-release-<leg> for every leg of WIRE_FUZZ_RELEASE_LEGS
//     (as make expanded it), and every such rule make defines,
//   - every -also target,
//
// and it REFUSES, naming each, a leg or -also target with no rule (a job would
// run a target make cannot build), a leg list that is missing or still holds an
// unexpanded reference, no tables-<lang>-release rule at all, and a make that
// failed to read its makefiles. A plan is never empty.
//
//	go run ./tools/releasetargets -also tables-cs-wire-fuzz,tables-dart-wire-fuzz
//	go run ./tools/releasetargets -db make-database.txt -also ...
//	go run ./tools/releasetargets -fixtures tools/releasetargets/testdata/f066
//
// -fixtures runs the real make on each committed fixture makefile in the
// directory and holds the plan to the fixture's own `# want:` line (a list of
// targets, or `refuse <target>`): certify.yml runs it before it plans, so the
// corpus of make spellings that once fooled the planner is replayed by the
// make that plans the release.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// legsVariable names the wire-fuzz leg list make/wire-fuzz-release.mk holds.
const legsVariable = "WIRE_FUZZ_RELEASE_LEGS"

// wireFuzzPrefix is the per-leg release wire-fuzz target's prefix.
const wireFuzzPrefix = "tables-wire-fuzz-release-"

var (
	// a port's release gate: one word between tables- and -release
	releaseRule = regexp.MustCompile(`^tables-[a-z0-9]+-release$`)
	// a leg's release wire-fuzz gate: one word after the prefix (so the
	// tables-wire-fuzz-release-negative-control rule is not a leg)
	wireFuzzRule = regexp.MustCompile(`^` + wireFuzzPrefix + `[a-z0-9]+$`)
	// a variable line in the database: NAME := value, NAME = value, ...
	variableLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*) (:::=|::=|:=|\?=|=) ?(.*)$`)
)

// database is what the planner reads from make's printed database.
type database struct {
	// targets make defined a rule for (a name printed under "# Not a
	// target:" is a prerequisite or a file make only looked at, not a rule)
	rules map[string]bool
	// variables as make printed them, by name: the flavour and the value
	variables map[string]variable
}

type variable struct {
	flavour string
	value   string
}

// parseDatabase reads the output of `make -p`.
func parseDatabase(r io.Reader) (database, error) {
	db := database{rules: map[string]bool{}, variables: map[string]variable{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	notATarget := false
	inDefine := false
	for sc.Scan() {
		line := sc.Text()
		if inDefine {
			if line == "endef" {
				inDefine = false
			}
			continue
		}
		if strings.HasPrefix(line, "define ") {
			inDefine = true
			continue
		}
		if line == "# Not a target:" {
			notATarget = true
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "\t") {
			continue
		}
		skip := notATarget
		notATarget = false
		if m := variableLine.FindStringSubmatch(line); m != nil {
			db.variables[m[1]] = variable{flavour: m[2], value: m[3]}
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok || skip {
			continue
		}
		rest = strings.TrimPrefix(rest, ":")
		// a target-specific variable prints as `target: VAR := value`
		if strings.Contains(rest, "=") {
			continue
		}
		if name == "" || strings.ContainsAny(name, " %$") {
			continue
		}
		db.rules[name] = true
	}
	if err := sc.Err(); err != nil {
		return db, err
	}
	return db, nil
}

// plan computes the targets, or every refusal by name.
func plan(db database, also []string) ([]string, error) {
	var errs []string
	set := map[string]bool{}
	releases := 0
	for name := range db.rules {
		if releaseRule.MatchString(name) {
			set[name] = true
			releases++
		}
		if wireFuzzRule.MatchString(name) {
			set[name] = true
		}
	}
	if releases == 0 {
		errs = append(errs, "make defines no tables-<lang>-release rule: a plan of release gates that names none plans nothing")
	}
	legs, ok := db.variables[legsVariable]
	switch {
	case !ok:
		errs = append(errs, legsVariable+" is not defined in make's database: the wire-fuzz release legs cannot be planned")
	case strings.Contains(legs.value, "$"):
		errs = append(errs, fmt.Sprintf("%s %s %q holds an unexpanded reference: plan from a `:=` list", legsVariable, legs.flavour, legs.value))
	case len(strings.Fields(legs.value)) == 0:
		errs = append(errs, legsVariable+" is empty: the wire-fuzz release legs plan nothing")
	default:
		for _, leg := range strings.Fields(legs.value) {
			name := wireFuzzPrefix + leg
			if !db.rules[name] {
				errs = append(errs, fmt.Sprintf("%s names the leg %q, and make defines no rule %s", legsVariable, leg, name))
				continue
			}
			set[name] = true
		}
	}
	for _, name := range also {
		if !db.rules[name] {
			errs = append(errs, fmt.Sprintf("the workflow plans %s, and make defines no rule by that name", name))
			continue
		}
		set[name] = true
	}
	if len(errs) > 0 {
		slices.Sort(errs)
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	slices.Sort(out)
	return out, nil
}

// makeDatabase runs `make -pq` in dir (on file, when it is not empty). -q
// runs no recipe: make exits 0 when the default goal is up to date, 1 when it
// is not, and 2 when it could not read its makefiles, which refuses.
func makeDatabase(dir, file string) ([]byte, error) {
	args := []string{"-pq", "--no-print-directory"}
	if file != "" {
		args = append(args, "-f", file)
	}
	cmd := exec.Command("make", args...)
	cmd.Dir = dir
	// the planner's make reads the makefiles as a bare `make` would
	cmd.Env = append(os.Environ(), "MAKEFLAGS=", "MAKELEVEL=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("make -pq in %s: %w\n%s", dir, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// fixtureWant reads a fixture's `# want:` line and its `#` continuation lines:
// the planned targets, or refuse and the one target the refusal names.
func fixtureWant(src []byte) (targets []string, refuse string, err error) {
	var words []string
	in := false
	for line := range strings.SplitSeq(string(src), "\n") {
		if rest, ok := strings.CutPrefix(line, "# want:"); ok {
			in = true
			words = append(words, strings.Fields(rest)...)
			continue
		}
		if in && strings.HasPrefix(line, "#  ") {
			words = append(words, strings.Fields(strings.TrimPrefix(line, "#"))...)
			continue
		}
		in = false
	}
	if len(words) == 0 {
		return nil, "", errors.New("no `# want:` line")
	}
	if words[0] == "refuse" {
		if len(words) != 2 {
			return nil, "", fmt.Errorf("`# want: refuse` names %d targets, want one", len(words)-1)
		}
		return nil, words[1], nil
	}
	slices.Sort(words)
	return words, "", nil
}

// checkFixture plans one fixture's database and holds it to its want.
func checkFixture(db []byte, src []byte, also []string) error {
	want, refuse, err := fixtureWant(src)
	if err != nil {
		return err
	}
	parsed, err := parseDatabase(bytes.NewReader(db))
	if err != nil {
		return err
	}
	got, err := plan(parsed, also)
	if refuse != "" {
		if err == nil {
			return fmt.Errorf("planned %v, want a refusal naming %s", got, refuse)
		}
		if !strings.Contains(err.Error(), refuse) {
			return fmt.Errorf("refused without naming %s:\n%v", refuse, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("refused, want %v:\n%v", want, err)
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("planned %v, want %v", got, want)
	}
	return nil
}

// fixtureAlso is the -also list every fixture is planned with.
var fixtureAlso = []string{"tables-cs-wire-fuzz"}

func runFixtures(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.mk"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no fixture makefiles (*.mk) in %s: a replay of nothing proves nothing", dir)
	}
	var errs []string
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		db, err := makeDatabase(filepath.Dir(f), filepath.Base(f))
		if err == nil {
			err = checkFixture(db, src, fixtureAlso)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		fmt.Fprintf(os.Stderr, "releasetargets: fixture %s planned as its want says\n", filepath.Base(f))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	fmt.Fprintf(os.Stderr, "releasetargets: %d fixtures replayed\n", len(files))
	return nil
}

func main() {
	dbFile := flag.String("db", "", "read make's database from this file instead of running `make -pq` here")
	alsoList := flag.String("also", "", "comma-separated targets the workflow always plans; each must be a rule make defines")
	fixtures := flag.String("fixtures", "", "replay every *.mk fixture in this directory through the real make and hold each to its `# want:` line")
	flag.Parse()
	if *fixtures != "" {
		if err := runFixtures(*fixtures); err != nil {
			fmt.Fprintf(os.Stderr, "releasetargets: REFUSES:\n%v\n", err)
			os.Exit(1)
		}
		return
	}
	var also []string
	for name := range strings.SplitSeq(*alsoList, ",") {
		if name = strings.TrimSpace(name); name != "" {
			also = append(also, name)
		}
	}
	var raw []byte
	var err error
	if *dbFile != "" {
		raw, err = os.ReadFile(*dbFile)
	} else {
		raw, err = makeDatabase(".", "")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasetargets: REFUSES: %v\n", err)
		os.Exit(1)
	}
	db, err := parseDatabase(bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasetargets: REFUSES: reading make's database: %v\n", err)
		os.Exit(1)
	}
	targets, err := plan(db, also)
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasetargets: REFUSES:\n%v\n", err)
		os.Exit(1)
	}
	out, err := json.Marshal(targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasetargets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "releasetargets: %d release targets planned\n", len(targets))
	fmt.Println(string(out))
}
