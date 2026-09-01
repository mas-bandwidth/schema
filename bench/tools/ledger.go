// The absolute ledger (#194): the across-time truth the relative table cannot
// carry. Reads every committed results CSV under bench/results/, takes the
// family-gen bench_mixed rows, and prints one series row per (machine, corpus,
// sitting, language, path): ns/msg, absolute. The relative table re-normalizes
// on every movement; these curves do not — absolute progress is the cpp curve
// going down or holding, catching up is another curve falling toward it, and
// churn is the cpp curve rising while others "improve".
//
// Sittings on different machines or corpora never share an axis: the shape
// and the box are part of the question, so the key leads with them.
//
//	go run ./bench/tools ledger            # the series, CSV on stdout
//	go run ./bench/tools ledger --check    # exit 1 if the newest cpp point
//	                                       # regressed beyond the noise gate
//	                                       # against the previous sitting on
//	                                       # the same (machine, corpus) axis
//
// The check is the anti-churn instrument's teeth: a cpp regression goes RED,
// it does not drift (BENCH-STANDARD's own law, given its time axis). The gate
// is max(2 * the two sittings' summed spread, 5%) — generous, because a
// same-box cross-sitting rerun of frozen code has measured ~3 points of TU
// layout and thermal noise; a real regression clears it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type point struct {
	file    string
	date    string // preamble date, the sitting's stamp
	machine string // "arch host" from the preamble
	corpus  string // corpus_id column
	lang    string
	path    string  // write | round_trip
	nsMsg   float64 // 1e9 / max rate (§2.2: max is the contract statistic)
	spread  float64 // spread_pct column
}

func ledger(args []string) {
	check, dir := false, "bench/results"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--check":
			check = true
		case args[i] == "--dir" && i+1 < len(args):
			dir = args[i+1]
			i++
		default:
			usage()
		}
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.csv"))
	if err != nil || len(files) == 0 {
		fmt.Fprintf(os.Stderr, "ledger: no results under %s\n", dir)
		os.Exit(1)
	}
	sort.Strings(files)

	var pts []point
	for _, f := range files {
		pts = append(pts, parse(f)...)
	}
	if len(pts) == 0 {
		fmt.Fprintln(os.Stderr, "ledger: no family-gen bench_mixed rows in any results file")
		os.Exit(1)
	}

	sort.SliceStable(pts, func(i, j int) bool {
		a, b := pts[i], pts[j]
		if a.machine != b.machine {
			return a.machine < b.machine
		}
		if a.corpus != b.corpus {
			return a.corpus < b.corpus
		}
		if a.date != b.date {
			return a.date < b.date
		}
		if a.lang != b.lang {
			return a.lang < b.lang
		}
		return a.path < b.path
	})

	fmt.Println("machine,corpus_id,date,file,lang,path,ns_per_msg,spread_pct")
	for _, p := range pts {
		fmt.Printf("%s,%s,%s,%s,%s,%s,%.2f,%.2f\n",
			p.machine, p.corpus, p.date, filepath.Base(p.file), p.lang, p.path, p.nsMsg, p.spread)
	}

	if check {
		os.Exit(checkCpp(pts))
	}
}

// parse pulls the family-gen bench_mixed rows and the preamble stamps from one
// results CSV. Files predating the data-driven contract simply contribute no
// rows — absence, not error.
func parse(file string) []point {
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ledger: %s: %v\n", file, err)
		os.Exit(1)
	}
	date, host, arch := "", "", ""
	var pts []point
	for line := range strings.SplitSeq(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, "# date: "); ok {
			date = strings.TrimSpace(after)
			continue
		}
		if after, ok := strings.CutPrefix(line, "# host: "); ok {
			fields := strings.Fields(after)
			host = fields[0]
			for i, f := range fields {
				if f == "arch:" && i+1 < len(fields) {
					arch = fields[i+1]
				}
			}
			continue
		}
		c := strings.Split(line, ",")
		if len(c) < 13 || c[1] != "bench_mixed" || c[12] != "gen" {
			continue
		}
		if c[2] != "write" && c[2] != "round_trip" {
			continue
		}
		maxRate, err1 := strconv.ParseFloat(c[8], 64)
		spread, err2 := strconv.ParseFloat(c[10], 64)
		if err1 != nil || err2 != nil || maxRate <= 0 {
			continue
		}
		pts = append(pts, point{
			file: file, date: date, machine: arch + " " + host, corpus: c[11],
			lang: c[0], path: c[2], nsMsg: 1e9 / maxRate, spread: spread,
		})
	}
	return pts
}

// checkCpp gates the reference: on every (machine, corpus) axis with at least
// two sittings, the newest cpp round_trip point must not sit above the
// previous one by more than the gate.
func checkCpp(pts []point) int {
	type axis struct{ machine, corpus string }
	series := map[axis][]point{}
	for _, p := range pts {
		if p.lang == "cpp" && p.path == "round_trip" {
			k := axis{p.machine, p.corpus}
			series[k] = append(series[k], p)
		}
	}
	code := 0
	for k, s := range series {
		if len(s) < 2 {
			continue
		}
		prev, last := s[len(s)-2], s[len(s)-1]
		gate := 2 * (prev.spread + last.spread)
		if gate < 5.0 {
			gate = 5.0
		}
		regression := (last.nsMsg - prev.nsMsg) / prev.nsMsg * 100.0
		if regression > gate {
			fmt.Fprintf(os.Stderr,
				"LEDGER RED: cpp round_trip regressed %.1f%% on [%s %s] (%.1f -> %.1f ns/msg, %s -> %s), gate %.1f%% — a perf regression goes red, it does not drift\n",
				regression, k.machine, k.corpus, prev.nsMsg, last.nsMsg,
				filepath.Base(prev.file), filepath.Base(last.file), gate)
			code = 1
		}
	}
	return code
}
