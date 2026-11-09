// Command gotestrows cuts ci-fast's go-test-touched selection into matrix
// rows that each finish inside the fleet's two-minute per-commit cap.
//
//	go run ./tools/gotestrows [-short] [-budget seconds] [-gotable-extras] <package>...
//
// It prints one line of JSON, a GitHub Actions matrix `include` list: one
// object per row, {"row": "k/n", "packages": "...", "run": "...", "skip":
// "...", "make": "..."}. A package argument of ./... is every package of the
// main module (`go list ./...`).
//
// EVERY SELECTED TEST RUNS IN EXACTLY ONE ROW, by construction, and no row
// list is a second home for the package list:
//
//   - a package with no `split` line in the table rides whole in one row;
//     the light ones are packed together, greedily, heaviest first, under the
//     budget, by the seconds the table measured for them (a package the
//     table does not name weighs defaultWeight);
//   - a package with `split` lines gets one row per line, `-run` that
//     pattern, and one more row that `-skip`s every one of them: the
//     complement, so a new test lands in that row with no edit anywhere.
//
// -gotable-extras adds the two make rows go-test-touched has always run when
// gotable is selected (schema#1376): tables-go-fixedform and
// conformance-go-rows, each a row of its own.
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
}

func main() {
	short := flag.Bool("short", false, "weigh packages by their -short time (a broad diff)")
	budget := flag.Float64("budget", 60, "seconds of test time a packed row may carry")
	extras := flag.Bool("gotable-extras", false, "add the tables-go-fixedform and conformance-go-rows rows")
	flag.Parse()

	t, err := readTable(tablePath)
	if err != nil {
		fail(err)
	}
	pkgs, err := expand(flag.Args())
	if err != nil {
		fail(err)
	}
	rows := plan(t, pkgs, *short, *budget)
	if *extras {
		rows = append(rows, Row{Make: "tables-go-fixedform"}, Row{Make: "conformance-go-rows"})
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
func readTable(path string) (table, error) {
	t := table{short: map[string]float64{}, plain: map[string]float64{}, splits: map[string][]string{}}
	f, err := os.Open(path)
	if err != nil {
		return t, err
	}
	defer f.Close()
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
		default:
			return t, fmt.Errorf("%s:%d: want `weight <pkg> <short> <plain>` or `split <pkg> <regexp>`, got %q", path, n, line)
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

func plan(t table, pkgs []string, short bool, budget float64) []Row {
	weights := t.plain
	if short {
		weights = t.short
	}
	var rows []Row
	var whole []string
	for _, p := range pkgs {
		pats := t.splits[p]
		if len(pats) == 0 {
			whole = append(whole, p)
			continue
		}
		for _, pat := range pats {
			rows = append(rows, Row{Packages: p, Run: pat})
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
