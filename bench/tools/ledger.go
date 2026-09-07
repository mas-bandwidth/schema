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
//	go run ./bench/tools ledger --check    # exit 1 if a LOCKED leg's newest
//	                                       # point regressed beyond the noise
//	                                       # gate against the previous sitting
//	                                       # on the same (machine, corpus)
//	                                       # axis, or if the NEWEST certified
//	                                       # PASS on such an axis separates c
//	                                       # from cpp beyond the §2.8 tie band
//
// The check is the anti-churn instrument's teeth: a regression on a locked leg
// goes RED, it does not drift (BENCH-STANDARD's own law, given its time axis).
// The gate is max(2 * the two sittings' summed spread, 5%) — generous, because
// a same-box cross-sitting rerun of frozen code has measured ~3 points of TU
// layout and thermal noise; a real regression clears it. ONE gate definition,
// in noiseGate below; the locked legs are a list, not a fork of the logic.
//
// LOCKING C AND C++ TOGETHER (owner, 2026-09-07: "So we should now be able to
// lock C and C++ together now in perf and make sure we don't regress"). Two
// teeth, not one:
//
//  1. the time axis — c gets the same per-axis regression gate cpp has, so a
//     C regression on the same machine goes RED too; and
//  2. the pair axis — the NEWEST certified pass carrying both legs' bench_mixed
//     round_trip rows on each (machine, corpus) axis is held to §2.8's tie
//     band, so the two legs cannot drift apart while each separately holds its
//     own curve. Older passes on the axis are printed as history, not gated:
//     the lock is about the pair's CURRENT state (owner, 2026-09-07: "land
//     parity between C and C++ and then lock"), so a pass that RECORDED a
//     separation which a later pass CLOSED stays in the record without redding
//     CI forever, and a regression from parity reds the moment the pass that
//     shows it is committed.
//
// WHAT THE LOCK COVERS, EXACTLY: bench_mixed, family gen, path round_trip,
// best rate. That is the headline statistic §2.8 rules on, and it is the whole
// of the lock. The bitpacker rows and the write path are OUTSIDE it, and not
// by oversight: in 2026-09-07-arm64-studio-bitpacker-checked-read-pass.csv —
// a `window: OK` pass this pair lock calls green at 106.1% — bitpacker/read
// has c at 161.4% of cpp. A band over that row would be a new ruling about a
// different statistic, and it is NOT invented here; it would need its own
// §2.8-style paragraph in BENCH-STANDARD.md first.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// lockedLegs are the languages whose curve the ledger gates. cpp is the
// reference the whole table normalizes on; c was added 2026-09-07 when the two
// legs were locked together — same clang backend, same word codec, and §2.8
// reports them as one figure, so one of them silently regressing while the
// other holds is exactly the churn this instrument exists to catch. Adding a
// leg here is the whole change: the check below is generic over the list.
var lockedLegs = []string{"cpp", "c"}

// tieBandFloor is §2.8's floor on the c/cpp tie band, in percentage points,
// and it is render.awk's constant — the two must agree or the table and the
// gate would disagree about the same sitting.
const tieBandFloor = 3.0

// invalidSpread is §2.3's INVALID line, in percentage points, and it is also
// render.awk's constant: a row whose spread exceeds it prints "—" in the
// headline table and never yields a figure at all. The pair lock refuses the
// same row for the same reason, so the table and the gate agree by
// construction and not by luck — before this, render.awk would print no
// number for a leg the Go lock was busy ruling on.
const invalidSpread = 40.0

// lookback is how many earlier points on an axis the newest one is measured
// against. The newest point is compared to the BEST (lowest ns/msg) of them,
// not to its immediate neighbour.
//
// Neighbour-only comparison was defeated by one commit: land two CSVs that are
// each ~50% slower than the record, and the newest is compared against the
// other regressed file, so the pair exits 0 and the axis has quietly reset at
// the worse level. Against the best of the previous three, a regression has to
// beat the recent best, which two files landing together cannot arrange.
//
// PLAINLY, THE HOLE THAT REMAINS: a single commit landing FOUR OR MORE points
// on one axis can still reset the baseline, because the fourth point pushes
// the last pre-commit point out of the window. Three is a depth, not a proof.
// The instrument's answer to a suspicious series is to read it, and plain
// `ledger` prints the whole series for exactly that. bench/README.md,
// "Locking C and C++ together", says the same thing.
const lookback = 3

type point struct {
	file    string
	date    string // preamble date, the sitting's stamp
	machine string // "arch host" from the preamble
	corpus  string // corpus_id column
	lang    string
	path    string  // write | round_trip
	nsMsg   float64 // 1e9 / max rate (§2.2: max is the contract statistic)
	spread  float64 // spread_pct column
	window  string  // preamble `window:` verdict: OK | INVALID | "" (a sitting)
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
		code := checkLockedLegs(pts)
		if c := checkPairLock(pts); c != 0 {
			code = c
		}
		os.Exit(code)
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
	date, host, arch, window := "", "", "", ""
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
		// §2.6's window verdict. The driver stamps it on the same preamble
		// line as control_delta_pct; a sitting has no control legs and so
		// carries no verdict at all.
		if strings.HasPrefix(line, "# ") {
			if fields := strings.Fields(line); len(fields) > 0 {
				for i, f := range fields {
					if f == "window:" && i+1 < len(fields) {
						window = fields[i+1]
					}
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
		// render.awk's codec guard, kept identical: js emits two gen tiers
		// and only the flat tier is THE js path. Files written before the
		// column existed have no field 18 and are unaffected.
		if len(c) >= 18 && c[17] == "runtime" {
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
			window: window,
		})
	}
	// The window stamp usually precedes the rows, but the preamble order is
	// not normative, so stamp every row once the whole file has been read.
	for i := range pts {
		pts[i].window = window
	}
	return pts
}

// noiseGate is THE gate, in percentage points: 2x the two sittings' summed
// spread, floored at 5%. One definition, used by every locked leg — a gate
// that differed per language would be a gate nobody could reason about.
func noiseGate(prevSpread, lastSpread float64) float64 {
	gate := 2 * (prevSpread + lastSpread)
	if gate < 5.0 {
		gate = 5.0
	}
	return gate
}

// checkLockedLegs gates the time axis: on every (machine, corpus) axis with at
// least two points, each locked leg's newest round_trip point must not sit
// above the BEST of the previous `lookback` points by more than the gate.
// Absolute rates do not compare across machines (§2), which is why the machine
// leads the key.
//
// THE AXIS IS BUILT FROM BOTH-LEG FILES ONLY. A point enters a locked leg's
// series only from a CSV that carries EVERY locked leg's bench_mixed gen
// round_trip row. The lock is a claim about the pair, so a single-leg
// experiment file is by construction not a like measurement of it: the Studio
// c axis was interleaved with four single-leg C experiments
// (packet-void-c-studio, c-defaults-c-studio, c-utf8-c-studio, c-wide-c-studio,
// 228-242 ns/msg) among passes at 224-233, and the c-defaults -> c-utf8 step
// alone is +5.35% against a 5.00% gate — so the next single-leg C experiment
// committed last would have red CI for something that is not a regression of
// the locked pair at all. This also narrows the pre-existing cpp gate the same
// way, deliberately: one rule, both legs.
func checkLockedLegs(pts []point) int {
	type axis struct{ lang, machine, corpus string }
	locked := map[string]bool{}
	for _, l := range lockedLegs {
		locked[l] = true
	}
	legsInFile := map[string]map[string]bool{}
	for _, p := range pts {
		if locked[p.lang] && p.path == "round_trip" {
			if legsInFile[p.file] == nil {
				legsInFile[p.file] = map[string]bool{}
			}
			legsInFile[p.file][p.lang] = true
		}
	}
	bothLegs := func(file string) bool {
		for _, l := range lockedLegs {
			if !legsInFile[file][l] {
				return false
			}
		}
		return true
	}
	series := map[axis][]point{}
	for _, p := range pts {
		if locked[p.lang] && p.path == "round_trip" && bothLegs(p.file) {
			k := axis{p.lang, p.machine, p.corpus}
			series[k] = append(series[k], p)
		}
	}
	keys := make([]axis, 0, len(series))
	for k := range series {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.machine != b.machine {
			return a.machine < b.machine
		}
		if a.corpus != b.corpus {
			return a.corpus < b.corpus
		}
		return a.lang < b.lang
	})
	code := 0
	for _, k := range keys {
		s := series[k]
		if len(s) < 2 {
			continue
		}
		last := s[len(s)-1]
		lo := max(len(s)-1-lookback, 0)
		// The BEST of the window, not the neighbour: see lookback.
		best := s[lo]
		for _, p := range s[lo : len(s)-1] {
			if p.nsMsg < best.nsMsg {
				best = p
			}
		}
		gate := noiseGate(best.spread, last.spread)
		regression := (last.nsMsg - best.nsMsg) / best.nsMsg * 100.0
		if regression > gate {
			fmt.Fprintf(os.Stderr,
				"LEDGER RED: %s round_trip regressed %.1f%% on [%s %s] against the axis's recent best (%.1f -> %.1f ns/msg, %s -> %s), gate %.1f%% — a perf regression goes red, it does not drift\n",
				k.lang, regression, k.machine, k.corpus, best.nsMsg, last.nsMsg,
				filepath.Base(best.file), filepath.Base(last.file), gate)
			code = 1
		}
	}
	return code
}

// checkPairLock gates the pair axis. For a committed CSV carrying both legs'
// bench_mixed round_trip rows, c's percentage of cpp is computed on the best
// rate and held to §2.8's tie band — the two within-sitting spreads summed,
// floored at 3.0 points — exactly as render.awk computes it for the headline
// table. The table and the gate must agree about a sitting or the record would
// say two things at once.
//
// THE LOCK HOLDS THE NEWEST CERTIFIED PASS ON EACH (MACHINE, CORPUS) AXIS, AND
// ONLY THAT ONE. Older passes on the axis print as history and gate nothing.
// The lock is a claim about the pair's CURRENT state — the maintainer's intent
// was "land parity between C and C++ and then lock" — and the record is a
// record: it keeps the passes that FOUND things, not only the ones that were
// clean. 2026-09-07-arm64-studio-serialize-c-1-10-0-pass.csv is exactly that,
// a `window: OK` pass measuring c at 107.4% of cpp against a 5.8-point band,
// and that measurement is the finding — a 7% read-path gap — which the next
// pass, 2026-09-07-arm64-studio-c-read-guard-pass.csv, closed at 98.2% inside
// 6.5. Holding every historical pass forever would leave CI red on a fixed
// finding, and a gate that reds on something already fixed teaches people to
// ignore the gate. Holding the newest certified pass keeps both teeth: the
// record still carries the separation that was found, and a regression from
// parity goes red the moment the pass that shows it is committed — because
// that pass is then the newest one on its axis.
//
// PASSES LOCK HARD, SITTINGS WARN. A pass certifies its window: it ran control
// legs at both ends and the driver stamped `window: OK` (§2.6), so the numbers
// in it are the ones the standard lets you publish a ratio from, and a
// separation there is a real separation. A sitting has no control legs and no
// window verdict — it cannot tell a separation from a drifting box, so reding
// it would be gating on evidence the standard itself refuses to publish
// ratios from. Sittings therefore print and exit 0, every one of them: they
// are informational, so there is no "newest sitting" to single out. This is
// not leniency: measured 2026-09-07, FIVE committed sittings (three Studio
// ninelang at 106.6-109.2%, the x86_64 spacegame at 111.3%, and one macbook
// quick sitting just past a 12.3-point band) sit outside their bands, which is
// exactly the difference a window verdict is there to record. A window stamped
// INVALID does not lock either — §2.6 already refuses to publish ratios from
// it.
//
// §2.3 REFUSES BEFORE §2.8 RULES. A leg whose spread_pct exceeds §2.3's
// INVALID line yields NO verdict here, because render.awk yields no figure for
// it either — it prints "—" and a §2.3 note, and a row that does not publish
// as a number cannot be the c in a c/cpp ratio. Without this the two would
// disagree by construction: the table refusing to print while the gate red on
// the number the table refused. Such a pass is not the axis's newest certified
// pass either, for the same reason: it publishes no figure, so it cannot be
// the figure the lock holds, and the newest pass that DOES publish one keeps
// the axis locked rather than a noisy sitting silently disarming it.
//
// SCOPE, said once: bench_mixed / gen / round_trip / best rate, and nothing
// else. bitpacker and the write path are outside this lock (see the package
// comment for the committed receipt that shows why that matters).
func checkPairLock(pts []point) int {
	type sitting struct {
		file, machine, corpus, date, window string
		c, cpp                              *point
	}
	byFile := map[string]*sitting{}
	for i, p := range pts {
		if p.path != "round_trip" || (p.lang != "c" && p.lang != "cpp") {
			continue
		}
		s, ok := byFile[p.file]
		if !ok {
			s = &sitting{file: p.file, machine: p.machine, corpus: p.corpus, date: p.date, window: p.window}
			byFile[p.file] = s
		}
		// Last row of a language wins, as render.awk's gt[$1] assignment
		// does. NOTE, inert on today's record: "last" is not the same
		// "last" in the two tools. pts is sorted (machine, corpus, date,
		// lang, path) before this loop, so a Go last-row-wins is the last
		// row of the LAST CORPUS in a file; render.awk's is the last row
		// in FILE ORDER. No committed results file carries two corpus_ids
		// among its family-gen rows, so today the two pick the same row.
		// A file that ever did would need one rule chosen and both tools
		// moved to it.
		if p.lang == "c" {
			s.c = &pts[i]
		} else {
			s.cpp = &pts[i]
		}
	}

	var all []*sitting
	for _, s := range byFile {
		if s.c != nil && s.cpp != nil {
			all = append(all, s)
		}
	}
	// Axis order, oldest first: the history of an axis reads down the page and
	// the pass the lock holds is the last of its axis.
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.machine != b.machine {
			return a.machine < b.machine
		}
		if a.corpus != b.corpus {
			return a.corpus < b.corpus
		}
		if a.date != b.date {
			return a.date < b.date
		}
		return a.file < b.file
	})

	// refused is §2.3's line: neither leg publishes as a number, so no ratio
	// is defined and nothing here can rule on it.
	refused := func(s *sitting) bool {
		return s.c.spread > invalidSpread || s.cpp.spread > invalidSpread
	}
	axisKey := func(s *sitting) string { return s.machine + "\x00" + s.corpus }

	// The pass the lock holds on each axis: the newest certified pass that
	// publishes a figure. `all` is oldest-first, so the last write wins.
	held := map[string]*sitting{}
	for _, s := range all {
		if s.window == "OK" && !refused(s) {
			held[axisKey(s)] = s
		}
	}

	code := 0
	for _, s := range all {
		// §2.3 first: a leg over the INVALID line does not publish as a
		// number, so no ratio is defined from it and the lock yields no
		// verdict — exactly what render.awk does with the same row.
		if refused(s) {
			fmt.Fprintf(os.Stderr,
				"ledger: pair-lock skipped — %s: a leg's spread is over §2.3's %.0f%% INVALID line (spread c %.2f, spread cpp %.2f), so the row does not publish as a number and no c/cpp figure is defined; render.awk prints — for it too\n",
				filepath.Base(s.file), invalidSpread, s.c.spread, s.cpp.spread)
			continue
		}
		// §2.8's figure: ns/msg of c over ns/msg of cpp, cpp the denominator
		// at 100%, so a c above 100% is the slower leg.
		pct := s.c.nsMsg / s.cpp.nsMsg * 100.0
		band := s.c.spread + s.cpp.spread
		if band < tieBandFloor {
			band = tieBandFloor
		}
		delta := pct - 100.0
		if delta < 0 {
			delta = -delta
		}
		inside := delta <= band

		// An older certified pass on the axis: history, not a verdict. It is
		// printed either way — a pass that recorded a separation is part of
		// how the pair got here, and hiding it would make the closed finding
		// unreadable in the same output that shows the pass which closed it.
		if s.window == "OK" && held[axisKey(s)] != s {
			where := "inside"
			tail := ""
			if !inside {
				where = "outside"
				tail = " — a separation this pass recorded; the lock holds this axis's newest certified pass instead"
			}
			fmt.Fprintf(os.Stderr, "pair history: %s %.1f%% against %.1f points: %s%s\n",
				filepath.Base(s.file), pct, band, where, tail)
			continue
		}

		if inside {
			// The pass the lock holds says so even when it is green: the
			// output has to name WHICH pass is under the gate, or "history"
			// lines above it would read as findings nobody is acting on.
			if s.window == "OK" {
				fmt.Fprintf(os.Stderr, "pair lock: %s %.1f%% against %.1f points: inside — the newest certified pass that publishes a figure on [%s %s], and the one the lock holds\n",
					filepath.Base(s.file), pct, band, s.machine, s.corpus)
			}
			continue
		}
		what := fmt.Sprintf("c measured %.1f%% of cpp in %s, outside the §2.8 tie band of %.1f points (spread c %.2f + spread cpp %.2f, floor %.1f)",
			pct, filepath.Base(s.file), band, s.c.spread, s.cpp.spread, tieBandFloor)
		const finding = "a separation beyond the combined spread is a finding to investigate, not a number to publish"
		switch s.window {
		case "OK":
			fmt.Fprintf(os.Stderr, "LEDGER RED: %s — the pass certifies its window (§2.6) and is the newest certified pass that publishes a figure on [%s %s], so this is where the pair stands now: %s\n",
				what, s.machine, s.corpus, finding)
			code = 1
		case "INVALID":
			fmt.Fprintf(os.Stderr, "ledger: pair-lock skipped — %s, but the pass is `window: INVALID` and §2.6 refuses to publish ratios from it\n", what)
		default:
			fmt.Fprintf(os.Stderr, "LEDGER WARN: %s — a sitting has no control legs and no window verdict, so it warns rather than reds (§2.6); %s\n", what, finding)
		}
	}
	return code
}
