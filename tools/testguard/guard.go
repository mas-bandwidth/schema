package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// config is the repository's checked-in config.
type config struct {
	dir      string // relative paths are read against it; "" is the working directory
	path     string
	suites   map[string]bool
	skips    map[[2]string]bool // suite, test
	tables   map[string]*table
	order    []string // table names in the order the config gives them
	baseline string
}

// table is one required coverage table: every row x column pair is covered by
// a test, or held as a gap with a reason.
type table struct {
	source map[string][2]string // axis (rows, columns, pairs) -> kind (file, run), argument
	gaps   map[[2]string]bool
}

var axes = []string{"rows", "columns", "pairs"}

// at is a path as the process opens it.
func (c *config) at(p string) string {
	if c.dir == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.dir, p)
}

func readConfig(dir, path string) (*config, error) {
	c := &config{dir: dir, path: path, suites: map[string]bool{}, skips: map[[2]string]bool{}, tables: map[string]*table{}}
	data, err := os.ReadFile(c.at(path))
	if err != nil {
		return nil, refuse("ls -l "+path, "reading the config: %v", err)
	}
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		f := strings.Split(line, "\t")
		at := fmt.Sprintf("sed -n %dp %s", n+1, path)
		bad := func(want string) error {
			return refuse(at, "%s:%d: %q wants %s, separated by tabs", path, n+1, f[0], want)
		}
		for _, field := range f {
			if field == "" || field != strings.TrimSpace(field) {
				return nil, refuse(at, "%s:%d: a field is empty or starts or ends with a blank: %q", path, n+1, field)
			}
		}
		switch f[0] {
		case "suite":
			if len(f) != 2 {
				return nil, bad("NAME")
			}
			c.suites[f[1]] = true
		case "skip":
			if len(f) != 4 {
				return nil, bad("SUITE, TEST and REASON")
			}
			c.skips[[2]string{f[1], f[2]}] = true
		case "cover":
			if len(f) != 5 || !slices.Contains(axes, f[2]) || (f[3] != "file" && f[3] != "run") {
				return nil, bad("TABLE, rows|columns|pairs, file|run and its PATH or COMMAND")
			}
			c.table(f[1]).source[f[2]] = [2]string{f[3], f[4]}
		case "gap":
			if len(f) != 5 {
				return nil, bad("TABLE, ROW, COLUMN and REASON")
			}
			c.table(f[1]).gaps[[2]string{f[2], f[3]}] = true
		case "baseline":
			if len(f) != 2 || c.baseline != "" {
				return nil, bad("DIR, once")
			}
			c.baseline = f[1]
		default:
			return nil, refuse(at, "%s:%d: %q is no entry; the entries are suite, skip, cover, gap and baseline", path, n+1, f[0])
		}
	}
	var missing []string
	if len(c.suites) == 0 {
		missing = append(missing, "suite line (a config that registers nothing checks nothing)")
	}
	if c.baseline == "" {
		missing = append(missing, "baseline line")
	}
	if len(missing) > 0 {
		return nil, refuse("go run ./tools/testguard help check", "%s has no %s", path, strings.Join(missing, " and no "))
	}
	for _, name := range c.order {
		for _, axis := range axes {
			if _, ok := c.tables[name].source[axis]; !ok {
				return nil, refuse("grep -n "+name+" "+path, "%s: table %s has no %s source; a table names rows, columns and pairs", path, name, axis)
			}
		}
	}
	return c, nil
}

func (c *config) table(name string) *table {
	t := c.tables[name]
	if t == nil {
		t = &table{source: map[string][2]string{}, gaps: map[[2]string]bool{}}
		c.tables[name] = t
		c.order = append(c.order, name)
	}
	return t
}

// report is the outcome of one check.
type report struct {
	findings []string
	notes    []string
	rules    map[string]int
	stats    string
}

func (r *report) add(rule, id, what, fix string) {
	r.findings = append(r.findings, fmt.Sprintf("RED %s %s: %s; fix: %s", rule, id, what, fix))
	r.rules[rule]++
}

func (r *report) byRule() string {
	var parts []string
	for rule, n := range r.rules {
		parts = append(parts, fmt.Sprintf("%s %d", rule, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// id is how a finding names a test: SUITE<TAB>TEST, as a skip line holds it.
func id(suite, test string) string { return suite + "\t" + test }

// guard reads the config and the results and runs every check. With write
// set, a run whose only findings are tests the baseline does not list yet
// adds those tests to the baseline; any other finding writes nothing.
func guard(dir, configPath string, args []string, write bool, sh shellFunc) (*report, error) {
	kinds, paths := make([]string, len(args)), make([]string, len(args))
	for i, a := range args {
		f, path, err := format(a)
		if err != nil {
			return nil, err
		}
		kinds[i], paths[i] = f, path
	}
	c, err := readConfig(dir, configPath)
	if err != nil {
		return nil, err
	}
	res := results{}
	for i, f := range kinds {
		path := paths[i]
		data, err := os.ReadFile(c.at(path))
		if err != nil {
			return nil, refuse("ls -l "+path, "reading a result file: %v", err)
		}
		if err := formats[f](res, path, data); err != nil {
			return nil, err
		}
	}
	tests, skips := 0, 0
	for _, s := range res {
		tests += s.tests()
		skips += len(s.skipped)
	}
	if tests == 0 {
		return nil, refuse("ls -l "+strings.Join(args, " "), "the result files hold 0 tests that ran; a run of nothing is never a pass")
	}
	rep := &report{rules: map[string]int{}}

	// Failures and incomplete runs.
	for _, name := range sortedKeys(res) {
		s := res[name]
		for _, t := range sortedKeys(s.failed) {
			rep.add("failed", id(name, t), "failed, or started and never ended", "make it pass")
		}
		if s.countFailed > 0 {
			rep.add("failed", name, fmt.Sprintf("reports %d failed", s.countFailed), "make them pass")
		}
		for _, p := range s.problems {
			rep.add("incomplete", name, p, "make the run complete")
		}
	}

	// 1. Skips: the ledger is the exact set of skips.
	skipped := map[[2]string]bool{}
	for name, s := range res {
		for t := range s.skipped {
			skipped[[2]string{name, t}] = true
		}
	}
	for _, k := range sortedPairs(skipped) {
		if !c.skips[k] {
			rep.add("skip", id(k[0], k[1]), "skipped, and the ledger does not hold it", "make it run, or add the line skip<TAB>SUITE<TAB>TEST<TAB>REASON to "+configPath)
		}
	}
	for _, k := range sortedPairs(c.skips) {
		if !skipped[k] {
			rep.add("stale-skip", id(k[0], k[1]), "ledgered as a skip, and it did not skip here (it ran, or it is gone)", "delete its skip line from "+configPath)
		}
	}

	// 2. Suites: every registered suite ran a test, and every suite that ran a test is registered.
	for _, name := range sortedKeys(c.suites) {
		s := res[name]
		switch {
		case s == nil:
			rep.add("missing-suite", name, "registered, and absent from every result file", "run the suite and pass its results, or delete its suite line from "+configPath)
		case s.tests() == 0:
			rep.add("empty-suite", name, "registered, and ran 0 tests", "make its tests run (a build tag, a filter or a missing file can leave none)")
		}
	}
	for _, name := range sortedKeys(res) {
		if !c.suites[name] && res[name].tests() > 0 {
			rep.add("unregistered-suite", name, fmt.Sprintf("ran %d tests and is not registered, so its disappearance would go unseen", res[name].tests()), "add a suite line for it to "+configPath)
		}
	}

	// 3. Coverage tables.
	pairs := 0
	for _, name := range c.order {
		n, err := c.cover(name, rep, sh)
		if err != nil {
			return nil, err
		}
		pairs += n
	}

	// 4. The baseline. A bootstrap makes its directory: git holds no empty one.
	if write {
		if err := os.MkdirAll(c.at(c.baseline), 0o755); err != nil {
			return nil, refuse("ls -ld "+c.baseline, "making the baseline: %v", err)
		}
	}
	additions, err := c.checkBaseline(res, rep)
	if err != nil {
		return nil, err
	}
	// A write is never a pass: it ends red either way, so a workflow that
	// passes the flag can never absorb new tests into a green run.
	if write {
		if len(rep.findings) > rep.rules["unlisted"] {
			rep.notes = append(rep.notes, "nothing written: --write-baseline adds entries only to a run whose only findings are unlisted")
		} else {
			for _, a := range additions {
				p := c.at(a.path)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return nil, refuse("ls -ld "+c.baseline, "writing the baseline: %v", err)
				}
				if err := os.WriteFile(p, []byte(a.body), 0o644); err != nil {
					return nil, refuse("ls -ld "+filepath.Dir(p), "writing the baseline: %v", err)
				}
			}
			rep.findings, rep.rules = nil, map[string]int{}
			rep.notes = append(rep.notes, fmt.Sprintf("WROTE: %d baseline entries under %s; commit them, then check without --write-baseline (a write is never a pass)", len(additions), c.baseline))
		}
	}
	rep.stats = fmt.Sprintf("read %d result files: %d suites, %d tests run, %d skipped; %d coverage tables, %d pairs",
		len(args), len(res), tests, skips, len(c.order), pairs)
	return rep, nil
}

// lines reads one axis of a table: a file, or a command's stdout. Blank lines
// and '#' comments are left out.
func (c *config) lines(name, axis string, sh shellFunc) ([]string, error) {
	src := c.tables[name].source[axis]
	var data []byte
	var err error
	if src[0] == "file" {
		if data, err = os.ReadFile(c.at(src[1])); err != nil {
			return nil, refuse("ls -l "+src[1], "table %s %s: %v", name, axis, err)
		}
	} else if data, err = sh(c.at("."), src[1]); err != nil {
		return nil, refuse(src[1], "table %s %s: the command %q failed: %v", name, axis, src[1], err)
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out, nil
}

// cover checks one table and returns its number of required pairs.
func (c *config) cover(name string, rep *report, sh shellFunc) (int, error) {
	got := map[string][]string{}
	for _, axis := range axes {
		l, err := c.lines(name, axis, sh)
		if err != nil {
			return 0, err
		}
		got[axis] = l
	}
	rows, cols := set(got["rows"]), set(got["columns"])
	for _, axis := range []string{"rows", "columns"} {
		if len(got[axis]) == 0 {
			rep.add("uncovered", name, "its "+axis+" list is empty, so the table vouches for nothing", "fix the "+axis+" source of table "+name)
		}
	}
	covered := map[[2]string]bool{}
	for _, l := range got["pairs"] {
		f := strings.Split(l, "\t")
		if len(f) != 2 {
			return 0, refuse(c.tables[name].source["pairs"][1], "table %s pairs: %q is not ROW<TAB>COLUMN", name, l)
		}
		if !rows[f[0]] || !cols[f[1]] {
			rep.add("uncovered", name+" "+f[0]+" x "+f[1], "a covered pair names a row or column the table does not list", "add it to the rows or columns source of table "+name)
			continue
		}
		covered[[2]string{f[0], f[1]}] = true
	}
	t := c.tables[name]
	for _, r := range sortedKeys(rows) {
		for _, col := range sortedKeys(cols) {
			p := [2]string{r, col}
			if !covered[p] && !t.gaps[p] {
				rep.add("uncovered", name+" "+r+" x "+col, "no test covers this pair, and no gap holds it", "cover it, or add a gap line with the reason to "+c.path)
			}
		}
	}
	for _, p := range sortedPairs(t.gaps) {
		gid := name + " " + p[0] + " x " + p[1]
		switch {
		case !rows[p[0]] || !cols[p[1]]:
			rep.add("stale-gap", gid, "a gap names a row or column the table does not list", "delete its gap line from "+c.path)
		case covered[p]:
			rep.add("stale-gap", gid, "a gap holds a pair that is covered", "delete its gap line from "+c.path)
		}
	}
	return len(rows) * len(cols), nil
}

// maxEntry is the longest entry name written: well under the 255 bytes most
// filesystems allow, since escaping can triple a name.
const maxEntry = 200

// Names Windows will not create, with or without an extension.
var reserved = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(\.|$)`)

// escape makes a suite or test name one path segment that every filesystem git
// checks out on can hold: letters, digits, _ and - stay, a dot stays unless it
// starts or ends the name, and every other byte is %XX (so / \ : * ? " < > |
// and blanks never reach a file name). A name Windows reserves (CON, NUL, ...)
// has its first letter escaped. A name longer than maxEntry is cut, at a whole
// %XX, and ends in ~ and 16 hex digits of its SHA-256; ~ is escaped everywhere
// else, so a hashed name is never another name's escape.
func escape(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		plain := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '-' ||
			c == '.' && i > 0 && i < len(name)-1
		if plain {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	e := b.String()
	if reserved.MatchString(e) {
		e = fmt.Sprintf("%%%02X", e[0]) + e[1:]
	}
	if len(e) > maxEntry {
		cut := maxEntry - 17
		if p := strings.LastIndexByte(e[:cut], '%'); p > cut-3 {
			cut = p
		}
		sum := sha256.Sum256([]byte(name))
		e = e[:cut] + "~" + hex.EncodeToString(sum[:8])
	}
	return e
}

// display is how a finding names an entry: its name, or the entry itself when
// it is hashed.
func display(entry string) string {
	if strings.Contains(entry, "~") {
		return entry
	}
	s, err := url.PathUnescape(entry)
	if err != nil {
		return entry
	}
	return s
}

var entryName = regexp.MustCompile(`^(?:[A-Za-z0-9_.-]|%[0-9A-F]{2})+(?:~[0-9a-f]{16})?$`)

// validEntry says whether a file name is one escape could have written.
func validEntry(f string) bool {
	if !entryName.MatchString(f) {
		return false
	}
	if strings.Contains(f, "~") {
		return len(f) <= maxEntry
	}
	return escape(display(f)) == f
}

// keep is a file git needs to hold an empty directory; it is never an entry,
// and no escaped name starts with a dot.
const keep = ".gitkeep"

// countEntry holds a count suite's floor: the fewest tests its count line may
// report. Escaping writes # as %23, so it is never a test's entry.
const countEntry = "#count"

// entry is a test's file in the baseline: DIR/SUITE/TEST, each escaped.
func (c *config) entry(suite, test string) string {
	return filepath.Join(c.baseline, escape(suite), escape(test))
}

// addition is one baseline file --write-baseline adds.
type addition struct{ path, body string }

// checkBaseline holds the run to the baseline: one empty file per test that
// runs, under a directory per suite, and for a suite read from "ran N, failed
// M" lines, which name no tests, a file #count holding the fewest it may run.
// A listed test that did not run vanished, and so did a count below its
// floor; a test that ran with no entry, or a count suite with no floor, is
// unlisted. It returns what --write-baseline would add.
func (c *config) checkBaseline(res results, rep *report) ([]addition, error) {
	dirs, err := os.ReadDir(c.at(c.baseline))
	if err != nil {
		return nil, refuse("ls -ld "+c.baseline, "the baseline %s cannot be read: %v", c.baseline, err)
	}
	suiteOf := map[string]string{}
	for s := range c.suites {
		suiteOf[escape(s)] = s
	}
	listed := map[string]map[string]bool{} // suite -> entry names
	floors := map[string]int{}
	for _, d := range dirs {
		if d.Name() == keep && !d.IsDir() {
			continue
		}
		p := filepath.Join(c.baseline, d.Name())
		suite, ok := suiteOf[d.Name()]
		if !d.IsDir() || !ok {
			rep.add("baseline", p, "is not the directory of a registered suite", "delete it, or register its suite in "+c.path)
			continue
		}
		files, err := os.ReadDir(c.at(p))
		if err != nil {
			return nil, refuse("ls -ld "+p, "the baseline %s cannot be read: %v", p, err)
		}
		listed[suite] = map[string]bool{}
		for _, f := range files {
			fp := filepath.Join(p, f.Name())
			switch {
			case f.Name() == keep && !f.IsDir():
			case f.Name() == countEntry && !f.IsDir():
				data, err := os.ReadFile(c.at(fp))
				if err != nil {
					return nil, refuse("ls -l "+fp, "the baseline %s cannot be read: %v", fp, err)
				}
				n, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil || n < 1 {
					rep.add("baseline", fp, "does not hold a count of 1 or more", "write the fewest tests the suite's count line may report into it")
					continue
				}
				floors[suite] = n
			case f.IsDir() || !validEntry(f.Name()):
				rep.add("baseline", fp, "is not a test entry (a file named for an escaped test name)", "delete it")
			default:
				listed[suite][f.Name()] = true
			}
		}
	}
	var writes []addition
	for _, name := range sortedKeys(c.suites) {
		s := res[name]
		if s == nil || s.tests() == 0 {
			continue // missing-suite or empty-suite names it once
		}
		floor, hasFloor := floors[name]
		switch {
		case s.countOnly && !hasFloor:
			p := filepath.Join(c.baseline, escape(name), countEntry)
			writes = append(writes, addition{p, strconv.Itoa(s.count) + "\n"})
			rep.add("unlisted", name, fmt.Sprintf("ran %d tests by its count line, and the baseline holds no floor for it", s.count), fmt.Sprintf("write %d into %s in this change", s.count, p))
		case s.countOnly && s.count < floor:
			rep.add("vanished", name, fmt.Sprintf("ran %d tests by its count line, and its floor is %d", s.count, floor), "make them run, or lower the count in "+filepath.Join(c.baseline, escape(name), countEntry)+" in this change")
		case !s.countOnly && hasFloor:
			rep.add("baseline", filepath.Join(c.baseline, escape(name), countEntry), "is a count floor, and the suite reported no count line", "delete it")
		}
		ran := map[string]string{}
		for t := range s.ran {
			ran[escape(t)] = t
		}
		for _, e := range sortedKeys(listed[name]) {
			if _, ok := ran[e]; !ok {
				rep.add("vanished", id(name, display(e)), "ran on the baseline and did not run here", "make it run, or delete "+filepath.Join(c.baseline, escape(name), e)+" in this change")
			}
		}
		for _, e := range sortedKeys(ran) {
			if !listed[name][e] {
				p := c.entry(name, ran[e])
				writes = append(writes, addition{p, ""})
				rep.add("unlisted", id(name, ran[e]), "ran, and the baseline does not list it", "add the empty file "+p+" in this change")
			}
		}
	}
	return writes, nil
}

func set(list []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range list {
		m[s] = true
	}
	return m
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPairs(m map[[2]string]bool) [][2]string {
	keys := make([][2]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}
