// Command gotestrows cuts ci-fast's go-test-touched selection into matrix
// rows that each finish inside the fleet's two-minute per-commit cap.
//
//	go run ./tools/gotestrows [-short] [-budget seconds] [-gotable-extras] [-dependents-of "<package>..."] <package>...
//
// It prints one line of JSON, a GitHub Actions matrix `include` list: one
// object per row, {"row": "k/n", "packages": "...", "run": "...", "skip":
// "...", "make": "..."}. A package argument of ./... is every package of the
// main module (`go list ./...`). -dependents-of names the packages whose
// non-test source the diff changed; every main-module package whose build or
// tests depend on one of them (`go list -test`) is selected beside the
// arguments, so a change cannot go green while it breaks a package it did not
// touch.
//
// EVERY SELECTED TEST RUNS IN EXACTLY ONE ROW, by construction, and no row
// list is a second home for the package list:
//
//   - a package with no `split` or `subsplit` line rides whole in one row;
//     the light ones are packed together, greedily, heaviest first, under the
//     budget, by the seconds the table measured for them (a package the
//     table does not name weighs defaultWeight);
//   - a package with `split` lines gets one row per line, `-run` that
//     pattern and `-skip` every earlier line's, and one more row that
//     `-skip`s every one of them: the complement, so a new test lands in that
//     row with no edit anywhere. Because a line skips the lines above it, a
//     broad line (a name range) placed after narrow ones takes only the rest.
//   - `subsplit` divides a single parent's children in the same way. Each
//     child pattern gets a row; its complement keeps future named corpus
//     seeds, and the package complement excludes the parent altogether.
//
// -gotable-extras adds the two make rows go-test-touched has always run when
// gotable is selected (schema#1376): tables-go-fixedform and
// conformance-go-rows-a and -b, each a row of its own.
//
// The table is .github/go-test-rows.txt; it says how it was measured.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const (
	tablePath     = ".github/go-test-rows.txt"
	modulePath    = "github.com/mas-bandwidth/schema/v2"
	defaultWeight = 10.0
)

// Row is one matrix row. Exactly one of Packages or Make is set.
type Row struct {
	Row      string `json:"row"`
	Packages string `json:"packages"`
	Run      string `json:"run"`
	Skip     string `json:"skip"`
	Make     string `json:"make"`
}

type table struct {
	short, plain map[string]float64
	splits       map[string][]string
	subsplits    map[string][]subsplit
}

type subsplit struct {
	parent string
	child  string
}

func main() {
	short := flag.Bool("short", false, "weigh packages by their -short time (a broad diff)")
	budget := flag.Float64("budget", 60, "seconds of test time a packed row may carry")
	extras := flag.Bool("gotable-extras", false, "add the tables-go-fixedform and conformance-go-rows rows")
	changed := flag.String("dependents-of", "", "packages whose non-test source changed: every main-module package whose tests build against one of them is selected too")
	flag.Parse()

	t, err := readTable(tablePath)
	if err != nil {
		fail(err)
	}
	args := flag.Args()
	if strings.TrimSpace(*changed) != "" {
		list, err := exec.Command("go", "list", "-test", "-f", "{{.ImportPath}}|{{join .Deps \" \"}}", "./...").Output()
		if err != nil {
			fail(fmt.Errorf("go list -test ./...: %w", err))
		}
		args = append(args, dependents(string(list), strings.Fields(*changed))...)
	}
	pkgs, err := expand(args)
	if err != nil {
		fail(err)
	}
	rows := plan(t, pkgs, *short, *budget)
	if *extras {
		// the two conformance halves are rows of their own: whole, the target ran
		// 156 s on a loaded bench, past the two-minute cap
		rows = append(rows, Row{Make: "tables-go-fixedform"}, Row{Make: "conformance-go-rows-a"}, Row{Make: "conformance-go-rows-b"}, Row{Make: "cards-conformance-go-negative-control"})
	}
	for i := range rows {
		rows[i].Row = fmt.Sprintf("%d/%d", i+1, len(rows))
	}
	out, err := json.Marshal(rows)
	if err != nil {
		fail(err)
	}
	fmt.Println(string(out))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gotestrows:", err)
	os.Exit(1)
}

// readTable reads the weights and the splits:
//
//	weight <package> <short seconds> <plain seconds>
//	split  <package> <test-name regexp>
//	subsplit <package> <parent regexp> <child regexp>
func readTable(path string) (table, error) {
	t := table{short: map[string]float64{}, plain: map[string]float64{}, splits: map[string][]string{}, subsplits: map[string][]subsplit{}}
	f, err := os.Open(path)
	if err != nil {
		return t, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case fields[0] == "weight" && len(fields) == 4:
			s, err1 := strconv.ParseFloat(fields[2], 64)
			p, err2 := strconv.ParseFloat(fields[3], 64)
			if err := errors.Join(err1, err2); err != nil {
				return t, fmt.Errorf("%s:%d: %w", path, n, err)
			}
			t.short[fields[1]], t.plain[fields[1]] = s, p
		case fields[0] == "split" && len(fields) == 3:
			t.splits[fields[1]] = append(t.splits[fields[1]], fields[2])
		case fields[0] == "subsplit" && len(fields) == 4:
			t.subsplits[fields[1]] = append(t.subsplits[fields[1]], subsplit{parent: fields[2], child: fields[3]})
		default:
			return t, fmt.Errorf("%s:%d: want weight, split or subsplit, got %q", path, n, line)
		}
	}
	return t, sc.Err()
}

// expand turns the arguments into ./-relative package paths, ./... into every
// package of the main module, without duplicates.
func expand(args []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = "./" + strings.TrimPrefix(strings.TrimSuffix(p, "/"), "./")
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, a := range args {
		for f := range strings.FieldsSeq(a) {
			if f != "./..." {
				add(f)
				continue
			}
			list, err := exec.Command("go", "list", "./...").Output()
			if err != nil {
				return nil, fmt.Errorf("go list ./...: %w", err)
			}
			for p := range strings.FieldsSeq(string(list)) {
				if p == modulePath {
					add(".")
					continue
				}
				add(strings.TrimPrefix(p, modulePath+"/"))
			}
		}
	}
	return out, nil
}

// dependents reads `go list -test -f '{{.ImportPath}}|{{join .Deps " "}}'
// ./...` and returns, ./-relative and sorted, every main-module package whose
// own build or test binary depends on one of changed (./-relative package
// paths). A change to a package's non-test source can break the tests of a
// package that imports it, directly or not; the diff's directories alone
// never name that package (PR #4437 changed the front end and left
// internal/tablewire red with every selected row green). The test variants
// ("p [p.test]", "p_test [p.test]", "p.test") count as p, so a package whose
// tests alone import a changed one is selected too.
func dependents(list string, changed []string) []string {
	want := map[string]bool{}
	for _, c := range changed {
		c = strings.TrimPrefix(strings.TrimSuffix(c, "/"), "./")
		if c == "." || c == "" {
			want[modulePath] = true
			continue
		}
		want[modulePath+"/"+c] = true
	}
	seen := map[string]bool{}
	var out []string
	for line := range strings.Lines(list) {
		path, deps, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		path, _, _ = strings.Cut(path, " ")
		path = strings.TrimSuffix(path, ".test")
		path = strings.TrimSuffix(path, "_test")
		if path != modulePath && !strings.HasPrefix(path, modulePath+"/") {
			continue
		}
		hit := false
		for d := range strings.FieldsSeq(deps) {
			if want[d] {
				hit = true
				break
			}
		}
		if !hit || seen[path] {
			continue
		}
		seen[path] = true
		rel := "."
		if path != modulePath {
			rel = "./" + strings.TrimPrefix(path, modulePath+"/")
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func plan(t table, pkgs []string, short bool, budget float64) []Row {
	weights := t.plain
	if short {
		weights = t.short
	}
	var rows []Row
	var whole []string
	for _, p := range pkgs {
		pats := append([]string(nil), t.splits[p]...)
		children := t.subsplits[p]
		if len(pats) == 0 && len(children) == 0 {
			whole = append(whole, p)
			continue
		}
		// A split row skips every earlier split line's pattern, so a broad
		// line after narrow ones (a family, a name range) takes only what they
		// left and no test runs in two rows.
		for i, pat := range pats {
			rows = append(rows, Row{Packages: p, Run: pat, Skip: strings.Join(pats[:i], "|")})
		}
		// Preserve table order without iterating a map. A slash is Go's
		// hierarchical matcher separator, so the child skip does not skip
		// the parent before its children can be discovered.
		var parents []string
		byParent := map[string][]string{}
		for _, sub := range children {
			if _, ok := byParent[sub.parent]; !ok {
				parents = append(parents, sub.parent)
			}
			byParent[sub.parent] = append(byParent[sub.parent], sub.child)
			rows = append(rows, Row{Packages: p, Run: sub.parent + "/" + sub.child})
		}
		for _, parent := range parents {
			rows = append(rows, Row{Packages: p, Run: parent, Skip: parent + "/(" + strings.Join(byParent[parent], "|") + ")"})
			pats = append(pats, parent)
		}
		rows = append(rows, Row{Packages: p, Skip: strings.Join(pats, "|")})
	}
	weight := func(p string) float64 {
		if w, ok := weights[p]; ok {
			return w
		}
		return defaultWeight
	}
	sort.SliceStable(whole, func(i, j int) bool { return weight(whole[i]) > weight(whole[j]) })
	// first fit, heaviest first
	var bins [][]string
	var load []float64
	for _, p := range whole {
		w := weight(p)
		placed := false
		for i := range bins {
			if load[i]+w <= budget {
				bins[i] = append(bins[i], p)
				load[i] += w
				placed = true
				break
			}
		}
		if !placed {
			bins = append(bins, []string{p})
			load = append(load, w)
		}
	}
	for _, b := range bins {
		sort.Strings(b)
		rows = append(rows, Row{Packages: strings.Join(b, " ")})
	}
	return rows
}
