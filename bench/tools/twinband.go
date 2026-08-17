// The window gate's second instrument (BENCH-STANDARD.md §2.6.1): twin legs
// catch state within a pass, historical bands catch state between passes.
//
//	go run ./bench/tools twingate twin-a.csv twin-b.csv
//	go run ./bench/tools bands    current.csv prior.csv [more-prior.csv...]
//
// twingate compares the two aggregates of an A/A twin pass — the SAME binary
// (same file, same inode) run as two interleaved legs at alternating
// positions in the round order. Per row, the twin ratio maxA/maxB must sit
// within the row's own spread band (the larger of the two twins' spread_pct):
// identical bytes measured twice in one window may differ only by the noise
// the window itself exhibits. A row outside its band is printed as
// `state-suspect:` — twin disagreement, state-selective interference — and
// the exit status is non-zero; the rel tool refuses to ratio a row its pass
// preamble marks `# twin_suspect:`. This instrument exists because
// row-and-binary-selective machine state walked through the §2.6 control-leg
// gate undetected: three rows sat collapsed 4-8x at exact plateaus across two
// windows whose control deltas read 0.0-0.3%.
//
// bands compares each row of the current pass against the min/max envelope of
// prior VALID same-configuration rows (same corpus_id, bytes_per_op, family,
// linkage, checks, opt — the §2.6.1 "same-configuration" terms; the preamble
// identities are NOT required to match, because bands deliberately reach
// across passes). A row landing outside its band by more than §2.3's noisy
// threshold (15%) is printed as `band-break:` and publishes only with the
// mark (§2.6.1 — a mark, not a refusal). A row with no same-configuration
// prior is `no-band:` — a configuration's first outing has no history to
// disagree with, which is a fact, not a pass.
package main

import (
	"fmt"
	"os"
	"sort"
)

// sortedKeys returns the union of both maps' keys in presentation order
// (langs blocks, corpus order, write before read), then any stragglers.
func sortedKeys(ms ...map[key]row) []key {
	seen := map[key]bool{}
	var out []key
	for _, l := range langs {
		for _, b := range order {
			for _, p := range []string{"write", "read"} {
				k := key{l.key, b, p}
				for _, m := range ms {
					if _, ok := m[k]; ok && !seen[k] {
						seen[k] = true
						out = append(out, k)
					}
				}
			}
		}
	}
	var rest []key
	for _, m := range ms {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				rest = append(rest, k)
			}
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		a, b := rest[i], rest[j]
		if a.lang != b.lang {
			return a.lang < b.lang
		}
		if a.bench != b.bench {
			return a.bench < b.bench
		}
		return a.path < b.path
	})
	return append(out, rest...)
}

func twingateCmd(paths []string) {
	if len(paths) != 2 {
		usage()
	}
	a, _, err := load(paths[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	b, _, err := load(paths[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	suspects := 0
	for _, k := range sortedKeys(a, b) {
		ra, oka := a[k]
		rb, okb := b[k]
		if !oka || !okb {
			fmt.Printf("state-suspect: %s/%s/%s (row present in only one twin — the twins did not measure the same suite)\n",
				k.lang, k.bench, k.path)
			suspects++
			continue
		}
		if rb.mx == 0 {
			fmt.Printf("state-suspect: %s/%s/%s (twin B rate is zero)\n", k.lang, k.bench, k.path)
			suspects++
			continue
		}
		dev := (ra.mx/rb.mx - 1.0) * 100.0
		if dev < 0 {
			dev = -dev
		}
		// The band is §2.3's own spread formula recomputed at FULL PRECISION
		// from the rate columns, not the CSV's two-decimal spread_pct print.
		// Measured (certified-space-1, EPYC core 15): twins agreeing to
		// 0.9 ppm were refused because a true spread of 0.0008% printed as
		// 0.00, collapsing the band to zero — the gate refused the QUIETEST
		// rows of the pass, on the quietest machine in the estate, by
		// construction. Same quantity, full precision; not a policy change.
		spreadOf := func(r row) float64 {
			if r.med == 0 {
				return 0
			}
			return (r.mx - r.mn) / r.med * 100.0
		}
		band := spreadOf(ra)
		if s := spreadOf(rb); s > band {
			band = s
		}
		if dev > band {
			fmt.Printf("state-suspect: %s/%s/%s twin ratio departs 1.0 by %.1f%% > spread band %.1f%% (twin disagreement — state-selective interference)\n",
				k.lang, k.bench, k.path, dev, band)
			suspects++
		} else {
			fmt.Printf("twin-ok: %s/%s/%s dev %.1f%% within band %.1f%%\n", k.lang, k.bench, k.path, dev, band)
		}
	}
	if suspects > 0 {
		fmt.Fprintf(os.Stderr, "twingate: %d state-suspect row(s) — §2.6.1 refuses these rows; re-run the window\n", suspects)
		os.Exit(4)
	}
}

// sameConfig is §2.6.1's "same-configuration": the terms under which a prior
// row's envelope binds this row. iters is included — a rescaled count is a
// different measurement even over the same corpus.
func sameConfig(a, b row) bool {
	return a.corpusID != "" && a.corpusID == b.corpusID &&
		a.bytes == b.bytes && a.iters == b.iters && a.family == b.family &&
		a.linkage == b.linkage && a.checks == b.checks && a.opt == b.opt
}

func bandsCmd(paths []string) {
	if len(paths) < 2 {
		usage()
	}
	cur, _, err := load(paths[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	type band struct {
		lo, hi float64
		n      int
	}
	bands := map[key]*band{}
	for _, p := range paths[1:] {
		rows, meta, err := load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		// §2.6.1: only prior VALID rows contribute — an INVALID window's rows
		// band nothing, and a spread-invalid row (§2.3) is not a measurement.
		id := parseIdentity(p, meta)
		if id.window != "" && id.window != "OK" {
			fmt.Fprintf(os.Stderr, "bands: skipping %s entirely — # window: %s\n", p, id.window)
			continue
		}
		for k, r := range rows {
			if r.spread > spreadInvalid {
				continue
			}
			c, ok := cur[k]
			if !ok || !sameConfig(c, r) {
				continue
			}
			bd := bands[k]
			if bd == nil {
				bd = &band{lo: r.mn, hi: r.mx}
				bands[k] = bd
			}
			if r.mn < bd.lo {
				bd.lo = r.mn
			}
			if r.mx > bd.hi {
				bd.hi = r.mx
			}
			bd.n++
		}
	}
	breaks := 0
	for _, k := range sortedKeys(cur) {
		r := cur[k]
		bd := bands[k]
		if bd == nil {
			fmt.Printf("no-band: %s/%s/%s (no prior VALID same-configuration row — first outing for this configuration)\n",
				k.lang, k.bench, k.path)
			continue
		}
		switch {
		case r.mx > bd.hi*(1.0+spreadNoisy/100.0):
			fmt.Printf("band-break: %s/%s/%s best %.0f ABOVE band [%.0f, %.0f] by %.1f%% (> %.0f%% §2.3 noisy threshold; %d prior rows) — publishes only with this mark\n",
				k.lang, k.bench, k.path, r.mx, bd.lo, bd.hi, (r.mx/bd.hi-1.0)*100.0, spreadNoisy, bd.n)
			breaks++
		case r.mx < bd.lo*(1.0-spreadNoisy/100.0):
			fmt.Printf("band-break: %s/%s/%s best %.0f BELOW band [%.0f, %.0f] by %.1f%% (> %.0f%% §2.3 noisy threshold; %d prior rows) — publishes only with this mark\n",
				k.lang, k.bench, k.path, r.mx, bd.lo, bd.hi, (1.0-r.mx/bd.lo)*100.0, spreadNoisy, bd.n)
			breaks++
		default:
			fmt.Printf("band-ok: %s/%s/%s best %.0f within band [%.0f, %.0f] (±%.0f%%; %d prior rows)\n",
				k.lang, k.bench, k.path, r.mx, bd.lo, bd.hi, spreadNoisy, bd.n)
		}
	}
	if breaks > 0 {
		fmt.Fprintf(os.Stderr, "bands: %d band-break row(s) — each publishes only with its mark (§2.6.1)\n", breaks)
		os.Exit(5)
	}
}
