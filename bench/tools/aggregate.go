// The pass driver's computation half (BENCH-STANDARD.md §2.4, §2.6).
//
//	go run ./bench/tools aggregate     round0.csv round1.csv [...]
//	go run ./bench/tools controlmedian control.csv
//
// aggregate merges the per-round CSVs an interleaved pass produced (each row
// runs=1, rates from one measured run) into final v2 rows: per (lang, bench,
// path), the max/median/min/spread across rounds. THE DRIVER COMPUTES THE
// STATISTICS, NOT THE RUNNER (§2.4) — each round's rate was measured under
// the same window every other language saw that round.
//
// Rows must agree across rounds on everything that identifies the
// measurement (iters, bytes_per_op, corpus_id, family, linkage, checks, opt,
// inline); a disagreement aborts the aggregation — it means the pass was not
// one pass.
//
// controlmedian prints the corpus-median headline rate (max_msgs_per_sec
// over all cpp rows) of a control-leg CSV, for the §2.6 window gate: a pass
// is VALID only if its start and end control legs agree within 5%.
package main

import (
	"fmt"
	"os"
	"sort"
)

// aggRow is one (lang,bench,path)'s accumulation across rounds.
type aggRow struct {
	first row       // the identifying columns, from the first round seen
	rates []float64 // one headline rate per round (mx == med == mn at runs=1)
}

func aggregateCmd(paths []string) {
	if len(paths) < 1 {
		usage()
	}
	acc := map[key]*aggRow{}
	var keys []key
	passCorpus := ""
	for _, p := range paths {
		rows, _, err := load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for k, r := range rows {
			a, ok := acc[k]
			if !ok {
				acc[k] = &aggRow{first: r, rates: []float64{r.mx}}
				keys = append(keys, k)
				if passCorpus == "" {
					passCorpus = r.corpusID
				}
				continue
			}
			f := a.first
			if f.iters != r.iters || f.bytes != r.bytes || f.corpusID != r.corpusID ||
				f.family != r.family || f.linkage != r.linkage || f.checks != r.checks ||
				f.opt != r.opt || f.inline != r.inline {
				fmt.Fprintf(os.Stderr, "aggregate: REFUSING: %s/%s/%s changed identity between rounds\n  first: %s\n  %s: %s\n",
					k.lang, k.bench, k.path, f.raw, p, r.raw)
				fmt.Fprintln(os.Stderr, "  a pass whose rounds measured different things is not one pass")
				os.Exit(2)
			}
			a.rates = append(a.rates, r.mx)
		}
	}
	// every row of a pass must have loaded the same corpus
	for _, k := range keys {
		if acc[k].first.corpusID != passCorpus {
			fmt.Fprintf(os.Stderr, "aggregate: REFUSING: %s/%s/%s corpus_id %s differs from the pass corpus %s\n",
				k.lang, k.bench, k.path, acc[k].first.corpusID, passCorpus)
			os.Exit(2)
		}
	}

	fmt.Printf("# corpus_id: %s\n", passCorpus)
	fmt.Println("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline")
	// presentation order: language blocks (as run.sh emits), corpus order within
	for _, l := range langs {
		for _, b := range order {
			for _, p := range []string{"write", "read"} {
				k := key{l.key, b, p}
				a, ok := acc[k]
				if !ok {
					continue
				}
				rates := append([]float64{}, a.rates...)
				sort.Float64s(rates)
				n := len(rates)
				med := median(rates)
				mn, mx := rates[0], rates[n-1]
				spread := (mx - mn) / med * 100.0
				mb := med * float64(a.first.bytes) / (1024.0 * 1024.0)
				fmt.Printf("%s,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f,%s,%s,%s,%s,%s,%s\n",
					k.lang, k.bench, k.path, a.first.iters, a.first.bytes, n,
					med, mn, mx, mb, spread,
					a.first.corpusID, a.first.family, a.first.linkage,
					a.first.checks, a.first.opt, a.first.inline)
			}
		}
	}
}

func controlMedianCmd(paths []string) {
	if len(paths) != 1 {
		usage()
	}
	rows, _, err := load(paths[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var rates []float64
	for k, r := range rows {
		if k.lang == "cpp" {
			rates = append(rates, r.mx)
		}
	}
	if len(rates) == 0 {
		fmt.Fprintf(os.Stderr, "controlmedian: no cpp rows in %s\n", paths[0])
		os.Exit(2)
	}
	fmt.Printf("%.0f\n", median(rates))
}
