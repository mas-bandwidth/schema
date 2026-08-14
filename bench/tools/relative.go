// Command relative turns benchmark CSVs into the tables PERFORMANCE.md carries:
// absolute per-language rates, THE RELATIVE TABLE (everything as a percentage
// of C++), and A/B deltas between two runs.
//
//	go run ./bench/tools rel      results.csv [more.csv...]
//	go run ./bench/tools abs      results.csv [more.csv...]
//	go run ./bench/tools ab       a.csv b.csv [lang [labelA labelB]]
//	go run ./bench/tools spreads  <pct> results.csv [more.csv...]
//
// Ported from Python 2026-08-14. Tools are Go: statically typed, it pushes
// back, and it is one toolchain instead of two.
//
// C is included as the fifth language. Its absence from the predecessor is why
// PERFORMANCE.md carried four rows after the C backend reached wire parity.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The benchmark corpus, in the order the tables present it.
var corpus = []string{
	"rigidbody_moving", "rigidbody_at_rest", "chat", "test", "inputpacket",
	"shipcreate", "ship_shallow", "probe_header", "probebits", "probearray",
	"testdata",
}

const batch = "message_batch"

var order = append(append([]string{}, corpus...), batch)

// langs is the presentation order. cpp is the baseline the relative table is
// expressed against — a baseline, NOT a reference implementation.
var langs = []struct{ key, name string }{
	{"cpp", "C++"}, {"c", "C"}, {"rust", "Rust"}, {"cs", "C#"}, {"go", "Go"},
}

type key struct{ lang, bench, path string }

type row struct {
	iters, bytes, runs      int
	med, mn, mx, mb, spread float64
}

// load reads one CSV, skipping preamble lines and keeping only Release rows.
// Debug rows would follow a second preamble; the harness is never run --debug
// for published numbers.
func load(path string) (map[key]row, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	rows := map[key]row{}
	var meta []string
	build := "Release"

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			meta = append(meta, line)
			if strings.HasPrefix(line, "# build:") {
				build = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			}
			continue
		}
		if strings.HasPrefix(line, "lang,") {
			continue
		}
		if build != "Release" {
			continue
		}
		fs := strings.Split(line, ",")
		if len(fs) < 11 {
			return nil, nil, fmt.Errorf("%s:%d: expected 11 fields, got %d", path, lineNo, len(fs))
		}
		num := func(i int) float64 {
			v, err := strconv.ParseFloat(strings.TrimSpace(fs[i]), 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s:%d: field %d: %v\n", path, lineNo, i, err)
				os.Exit(2)
			}
			return v
		}
		rows[key{fs[0], fs[1], fs[2]}] = row{
			iters: int(num(3)), bytes: int(num(4)), runs: int(num(5)),
			med: num(6), mn: num(7), mx: num(8), mb: num(9), spread: num(10),
		}
	}
	return rows, meta, sc.Err()
}

func merge(paths []string) map[key]row {
	all := map[key]row{}
	for _, p := range paths {
		r, _, err := load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for k, v := range r {
			all[k] = v
		}
	}
	return all
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64{}, xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// rel is the median across the corpus of cpp_rate/lang_rate as a percentage.
//
// HIGHER IS SLOWER, and this is the one thing to get right about this table.
// The CSV's med column is median_msgs_per_sec — a RATE — so dividing C++'s rate
// by the language's rate gives the factor by which that language is slower.
// A language at 200% takes twice as long as C++; one at 50% is twice as fast.
// The predecessor's docstring got tangled here mid-sentence ("median over
// benches of cpp_time/lang_time... wait:"), which is a fair warning.
func rel(rows map[key]row, lang, path string) (float64, bool) {
	var ratios []float64
	for _, b := range corpus {
		c, okc := rows[key{"cpp", b, path}]
		l, okl := rows[key{lang, b, path}]
		if !okc || !okl || l.med == 0 {
			return 0, false
		}
		ratios = append(ratios, c.med/l.med*100.0)
	}
	return median(ratios), true
}

func relativeTable(rows map[key]row) string {
	out := []string{
		"<!-- HIGHER IS SLOWER: each cell is C++'s rate divided by this",
		"     language's rate, so 200% means it takes twice as long. -->",
		"| backend | write | read | batch write | batch read |",
		"|---|---:|---:|---:|---:|",
		"| C++ | 100% | 100% | 100% | 100% |",
	}
	for _, l := range langs {
		if l.key == "cpp" {
			continue
		}
		w, okw := rel(rows, l.key, "write")
		r, okr := rel(rows, l.key, "read")
		bw, okbw := ratio(rows, l.key, batch, "write")
		br, okbr := ratio(rows, l.key, batch, "read")
		if !okw || !okr || !okbw || !okbr {
			out = append(out, fmt.Sprintf("| %s | — | — | — | — |", l.name))
			continue
		}
		out = append(out, fmt.Sprintf("| %s | **%.0f%%** | **%.0f%%** | **%.0f%%** | **%.0f%%** |",
			l.name, w, r, bw, br))
	}
	return strings.Join(out, "\n")
}

func ratio(rows map[key]row, lang, bench, path string) (float64, bool) {
	c, okc := rows[key{"cpp", bench, path}]
	l, okl := rows[key{lang, bench, path}]
	if !okc || !okl || l.med == 0 {
		return 0, false
	}
	return c.med / l.med * 100.0, true
}

func absoluteTable(rows map[key]row) string {
	hdr := "| bench | path | B/msg |"
	sep := "|---|---|---:|"
	for _, l := range langs {
		hdr += fmt.Sprintf(" %s M msg/s | %s MB/s |", l.name, l.name)
		sep += "---:|---:|"
	}
	out := []string{hdr, sep}
	for _, b := range order {
		for _, p := range []string{"write", "read"} {
			c, okc := rows[key{"cpp", b, p}]
			bytes := "?"
			if okc {
				bytes = strconv.Itoa(c.bytes)
			}
			cells := []string{b, p, bytes}
			for _, l := range langs {
				if x, ok := rows[key{l.key, b, p}]; ok {
					cells = append(cells, fmt.Sprintf("%.2f", x.med/1e6), fmt.Sprintf("%.0f", x.mb))
				} else {
					cells = append(cells, "—", "—")
				}
			}
			out = append(out, "| "+strings.Join(cells, " | ")+" |")
		}
	}
	return strings.Join(out, "\n")
}

// abTable reports per-row B/A rate multipliers alongside both spreads, so a
// move can be judged against the noise rather than eyeballed.
func abTable(a, b map[key]row, lang, labelA, labelB string) string {
	out := []string{
		fmt.Sprintf("| bench | path | %s M msg/s | %s M msg/s | %s/%s | spread %s%% | spread %s%% |",
			labelA, labelB, labelB, labelA, labelA, labelB),
		"|---|---|---:|---:|---:|---:|---:|",
	}
	for _, bench := range order {
		for _, p := range []string{"write", "read"} {
			ra, oka := a[key{lang, bench, p}]
			rb, okb := b[key{lang, bench, p}]
			if !oka || !okb || ra.med == 0 {
				continue
			}
			out = append(out, fmt.Sprintf("| %s | %s | %.2f | %.2f | **%.2fx** | %.1f | %.1f |",
				bench, p, ra.med/1e6, rb.med/1e6, rb.med/ra.med, ra.spread, rb.spread))
		}
	}
	return strings.Join(out, "\n")
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  relative rel      results.csv [more.csv...]   the relative table (% of C++)
  relative abs      results.csv [more.csv...]   absolute rates per language
  relative ab       a.csv b.csv [lang [A B]]    B/A deltas with spreads
  relative spreads  <pct> results.csv [...]     rows noisier than pct
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "rel":
		if len(os.Args) < 3 {
			usage()
		}
		fmt.Println(relativeTable(merge(os.Args[2:])))
	case "abs":
		if len(os.Args) < 3 {
			usage()
		}
		fmt.Println(absoluteTable(merge(os.Args[2:])))
	case "ab":
		if len(os.Args) < 4 {
			usage()
		}
		a, _, err := load(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		b, _, err := load(os.Args[3])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		lang, la, lb := "cpp", "A", "B"
		if len(os.Args) > 4 {
			lang = os.Args[4]
		}
		if len(os.Args) > 5 {
			la = os.Args[5]
		}
		if len(os.Args) > 6 {
			lb = os.Args[6]
		}
		fmt.Println(abTable(a, b, lang, la, lb))
	case "spreads":
		if len(os.Args) < 4 {
			usage()
		}
		pct, err := strconv.ParseFloat(os.Args[2], 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		rows := merge(os.Args[3:])
		keys := make([]key, 0, len(rows))
		for k := range rows {
			if rows[k].spread > pct {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].lang != keys[j].lang {
				return keys[i].lang < keys[j].lang
			}
			if keys[i].bench != keys[j].bench {
				return keys[i].bench < keys[j].bench
			}
			return keys[i].path < keys[j].path
		})
		for _, k := range keys {
			fmt.Printf("%s %s %s  %.1f%%\n", k.lang, k.bench, k.path, rows[k].spread)
		}
	default:
		usage()
	}
}
