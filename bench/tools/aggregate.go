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
// one pass. Provenance is enforced the same way: every input file's preamble
// identity must match the first file's (rounds from two machines are not one
// pass), the same input path may not be given twice (the same round twice
// yields runs=N+1 spread=0.00 — fake stability), and two files stamped with
// the same `# round: K` may not both contribute rows to one key.
//
// controlmedian prints the corpus-median headline rate (max_msgs_per_sec
// over the control leg's family gen cpp rows — §2.6 names the family-gen
// corpus median, and the rt/bits rows the same runner also emits are not
// part of the window gate) of a control-leg CSV: a pass is VALID only if
// its start and end control legs agree within 5%.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// aggRow is one (lang,bench,path)'s accumulation across rounds.
type aggRow struct {
	first  row               // the identifying columns, from the first round seen
	rates  []float64         // one headline rate per round (mx == med == mn at runs=1)
	rounds map[string]string // `# round: K` value -> the file that contributed it
}

// parseRound extracts the driver's `# round: K` stamp from a preamble, or ""
// when the file carries none (a control leg, or a pre-stamp CSV).
func parseRound(meta []string) string {
	for _, m := range meta {
		if after, ok := strings.CutPrefix(m, "# round:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func aggregateCmd(paths []string) {
	if len(paths) < 1 {
		usage()
	}
	acc := map[key]*aggRow{}
	var keys []key
	passCorpus := ""
	seenPath := map[string]string{}
	var passID identity
	passIDSet := false
	for _, p := range paths {
		// the same round file twice yields runs=N+1 spread=0.00 — fake
		// stability manufactured from one measurement. Refuse by path.
		clean := filepath.Clean(p)
		if prev, dup := seenPath[clean]; dup {
			fmt.Fprintf(os.Stderr, "aggregate: REFUSING: input %s given twice (as %s and %s)\n", clean, prev, p)
			fmt.Fprintln(os.Stderr, "  aggregating one round twice manufactures runs and a 0.00 spread from a single measurement")
			os.Exit(2)
		}
		seenPath[clean] = p
		rows, meta, err := load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		// provenance travels with every file: rounds from two machines (or
		// two compilers, or two flag sets) are not one pass. Bare per-round
		// files carry an empty identity and match each other; a file with a
		// different stated identity refuses.
		id := parseIdentity(p, meta)
		if !passIDSet {
			passID, passIDSet = id, true
		} else if d := passID.differs(id); len(d) > 0 {
			fmt.Fprintf(os.Stderr, "aggregate: REFUSING: %s was measured under a different identity than %s\n", p, passID.path)
			for _, one := range d {
				fmt.Fprintf(os.Stderr, "    %s\n", one)
			}
			fmt.Fprintln(os.Stderr, "  rounds from different machines/compilers/flags are not one pass")
			os.Exit(2)
		}
		round := parseRound(meta)
		for k, r := range rows {
			a, ok := acc[k]
			if !ok {
				a = &aggRow{first: r, rounds: map[string]string{}}
				acc[k] = a
				keys = append(keys, k)
				if passCorpus == "" {
					passCorpus = r.corpusID
				}
			} else {
				f := a.first
				if f.iters != r.iters || f.bytes != r.bytes || f.corpusID != r.corpusID ||
					f.family != r.family || f.linkage != r.linkage || f.checks != r.checks ||
					f.opt != r.opt || f.inline != r.inline {
					fmt.Fprintf(os.Stderr, "aggregate: REFUSING: %s/%s/%s changed identity between rounds\n  first: %s\n  %s: %s\n",
						k.lang, k.bench, k.path, f.raw, p, r.raw)
					fmt.Fprintln(os.Stderr, "  a pass whose rounds measured different things is not one pass")
					os.Exit(2)
				}
			}
			if round != "" {
				if prev, dup := a.rounds[round]; dup {
					fmt.Fprintf(os.Stderr, "aggregate: REFUSING: %s/%s/%s appears twice for round %s (from %s and %s)\n",
						k.lang, k.bench, k.path, round, prev, p)
					fmt.Fprintln(os.Stderr, "  one round contributes one rate per row; a duplicate is a re-run or a copy, not a round")
					os.Exit(2)
				}
				a.rounds[round] = p
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
	// presentation order: language blocks (as run.sh emits), corpus order within.
	// The order list is presentation, but this loop is also the OUTPUT loop —
	// a bench OR PATH missing from the lists would silently vanish from the
	// pass, which is exactly what happened to real_packet on certified-space-1's
	// first window (added to the runners by #61, never to the list) and again
	// to round_trip when the gen bench_mixed rows went data-driven (#191).
	// Printed keys are counted and any straggler REFUSES the aggregation.
	printed := 0
	for _, l := range langs {
		for _, b := range order {
			for _, p := range paths {
				for _, c := range codecs {
					k := key{l.key, b, p, c}
					a, ok := acc[k]
					if !ok {
						continue
					}
					printed++
					rates := append([]float64{}, a.rates...)
					sort.Float64s(rates)
					n := len(rates)
					med := median(rates)
					mn, mx := rates[0], rates[n-1]
					spread := (mx - mn) / med * 100.0
					mb := med * float64(a.first.bytes) / (1024.0 * 1024.0)
					// the codec column is appended only on rows that carry
					// one (§5.1 append-only: untiered rows stay 17 fields)
					codecCol := ""
					if k.codec != "" {
						codecCol = "," + k.codec
					}
					fmt.Printf("%s,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f,%s,%s,%s,%s,%s,%s%s\n",
						k.lang, k.bench, k.path, a.first.iters, a.first.bytes, n,
						med, mn, mx, mb, spread,
						a.first.corpusID, a.first.family, a.first.linkage,
						a.first.checks, a.first.opt, a.first.inline, codecCol)
				}
			}
		}
	}
	if printed != len(acc) {
		fmt.Fprintf(os.Stderr, "aggregate: REFUSING: %d row key(s) accumulated but not printed — the order list does not know every bench the rounds measured:\n", len(acc)-printed)
		for _, k := range keys {
			if !slices.Contains(order, k.bench) || !slices.Contains(paths, k.path) {
				fmt.Fprintf(os.Stderr, "    %s/%s/%s\n", k.lang, k.bench, k.path)
			}
		}
		os.Exit(2)
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
		// §2.6 names the family-gen corpus median: the control leg is "the
		// C++ family gen runner", and the rt/bits rows the same binary also
		// emits are not part of the window gate.
		if k.lang == "cpp" && r.family == "gen" {
			rates = append(rates, r.mx)
		}
	}
	if len(rates) == 0 {
		fmt.Fprintf(os.Stderr, "controlmedian: no cpp family-gen rows in %s\n", paths[0])
		os.Exit(2)
	}
	fmt.Printf("%.0f\n", median(rates))
}
