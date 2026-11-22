// Package nightlyrows reads .github/nightly-fast-rows.txt, the pull-request
// forms of gates whose full form runs only nightly, and picks the rows a diff
// needs. ci-fast.yml runs the picked rows as its nightly-fast job; the plan
// job calls tools/nightlyrows to pick them. docs/CI-TIERS.md says which
// nightly gate each row stands for, and internal/ci TestEveryNightlyOnlyGate
// HasAPRTierForm holds every nightly-only workflow to a row or to a named
// pull-request job that already catches what it catches.
package nightlyrows

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// RegistryPath is where the rows live. A diff that touches it selects every
// row: an edited row is checked by running it.
const RegistryPath = ".github/nightly-fast-rows.txt"

// Row is one pull-request form: Name is the matrix row, Leg the toolchain the
// job puts on the runner for it (and the leg whose diff selects it; "-" for
// none), Paths the files besides the leg that select it, Run the shell line.
type Row struct {
	Name  string
	Leg   string
	Paths *regexp.Regexp
	Run   string
	Line  int
}

// Parse reads the registry: one row per line, four fields separated by " | ",
// "#" comments and blank lines ignored. Every error names its line.
func Parse(r io.Reader) ([]Row, error) {
	var rows []Row
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, " | ")
		if len(f) != 4 {
			return nil, fmt.Errorf("%s:%d: want 4 fields separated by \" | \" (name | leg | paths | run), got %d", RegistryPath, n, len(f))
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
			if f[i] == "" {
				return nil, fmt.Errorf("%s:%d: field %d is empty", RegistryPath, n, i+1)
			}
		}
		if prev, dup := seen[f[0]]; dup {
			return nil, fmt.Errorf("%s:%d: row %q is already named on line %d", RegistryPath, n, f[0], prev)
		}
		seen[f[0]] = n
		re, err := regexp.Compile(f[2])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: paths of %q: %v", RegistryPath, n, f[0], err)
		}
		rows = append(rows, Row{Name: f[0], Leg: f[1], Paths: re, Run: f[3], Line: n})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: no rows", RegistryPath)
	}
	return rows, nil
}

// Select picks the rows a diff needs: every row when broad is set or the diff
// touches the registry, else each row whose leg the diff touched or whose
// paths match a changed file. The direction of every error is "run more".
func Select(rows []Row, files, touchedLegs []string, broad bool) []Row {
	all := broad
	for _, f := range files {
		if f == RegistryPath {
			all = true
		}
	}
	legs := map[string]bool{}
	for _, l := range touchedLegs {
		legs[l] = true
	}
	var out []Row
	for _, r := range rows {
		pick := all || (r.Leg != "-" && legs[r.Leg])
		for _, f := range files {
			if pick {
				break
			}
			pick = r.Paths.MatchString(f)
		}
		if pick {
			out = append(out, r)
		}
	}
	return out
}
