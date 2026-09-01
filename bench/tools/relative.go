// Command relative turns benchmark CSVs into the tables PERFORMANCE.md carries:
// absolute per-language rates, THE RELATIVE TABLE (everything as a percentage
// of C — the reference, ruled by Glenn 2026-08-17: "make C the reference. It
// is the 100%. C++ is measured against C."), and A/B deltas between two runs.
//
//	go run ./bench/tools rel      results.csv [more.csv...]
//	go run ./bench/tools abs      results.csv [more.csv...]
//	go run ./bench/tools ab       a.csv b.csv [lang [labelA labelB]]
//	go run ./bench/tools spreads  <pct> results.csv [more.csv...]
//
// This tool enforces bench/BENCH-STANDARD.md §5.3: it carries every CSV's
// preamble through the merge and REFUSES to divide two rows unless they are
// comparable — same corpus_id, bytes_per_op, family, opt, checks, linkage,
// a real inline verdict on both sides, matching cpu and flag preambles, and
// neither row invalid by spread (§2.3) or window (§2.6). A refusal prints
// both rows and the differing field and exits non-zero; it never prints a
// number. There is no --force. If two rows are not comparable, the fix is
// another measurement, not a flag.
//
// Two captioned escapes exist, per §3.4 and §3.1: --label-checks and
// --cross-linkage print the ratio anyway, labelled with the contract
// difference it includes. Everything else refuses.
//
// The headline statistic is max_msgs_per_sec (§2.2): interference only ever
// makes a run slower, so on a contended machine the fastest run is the
// maximum-likelihood estimate of the uncontended cost. The median, min and
// spread are printed alongside. Spread policy (§2.3): rows over 15% are
// noisy and excluded from corpus-median tables; rows over 40% are invalid
// and never printed as numbers.
//
// v1 CSVs (11 columns) still load. Their rows get an empty corpus_id and
// inline=unknown, which makes them un-ratioable by §5.3 — deliberately:
// legacy data cannot be trusted to be comparable, and now it says so.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The bench/corpus/Bench.schema shapes — ONE, since the ruling on #199 left
// a single bench corpus. Presentation order only; the family column, not
// this list, is what the §5.3 refusal rules read. A bench absent from this
// list would silently vanish from every table and from aggregate, so the
// list must know every bench the runners emit.
var benchCorpus = []string{"bench_mixed"}

const bitpacker = "bitpacker"

// paths is every value the §5.1 `path` column takes, in presentation order.
// `round_trip` joined write/read when the gen family's bench_mixed rows went
// data-driven (issue #191, BENCH-STANDARD §2.9): that leg's rows are write
// and round_trip, and read is DERIVED to stderr rather than measured. This
// list is the OUTPUT loop of aggregate, twingate, bands and the absolute
// table, so a path missing from it makes rows silently vanish — which is
// exactly what round_trip did between the tracer landing and this fix (F4).
var paths = []string{"write", "read", "round_trip"}

var order = append(append([]string{}, benchCorpus...), bitpacker)

// langs is the presentation order. c is the reference the relative table is
// expressed against (Glenn, 2026-08-17: "make C the reference. It is the
// 100%. C++ is measured against C." — flipped from cpp, which had been the
// baseline since the table existed).
// js rides in this list so aggregate PRINTS its rows (the output loop walks
// langs, and a language missing from it would trip the straggler refusal —
// exactly the silent-vanish class the refusal exists to catch). It never
// enters the relative table: js rows carry inline=unknown by design (a JIT
// has no AOT artifact for the §4.1 verdict), and §5.3 refuses any ratio
// touching an unknown — relativeTable skips js explicitly rather than
// letting one un-ratioable-by-construction language refuse the whole table.
var langs = []struct{ key, name string }{
	{"c", "C"}, {"cpp", "C++"}, {"rust", "Rust"}, {"cs", "C#"}, {"go", "Go"},
	{"js", "JavaScript"},
}

// reference is the table's 100% language.
const reference = "c"

// §2.3 spread policy thresholds.
const (
	spreadNoisy   = 15.0 // > this: excluded from corpus-median tables
	spreadInvalid = 40.0 // > this: the row never enters a ratio or prints as a number
)

// The two captioned escapes (§3.4, §3.1). Set by flags, never by default.
var (
	labelChecks  = false
	crossLinkage = false
)

// §3.4's three checks values, each with the semantics its caption must name.
// The refusal logic never consults this map — ANY two differing values
// refuse without --label-checks, a value this map has never heard of
// included — it exists so a labelled ratio says what each side's number
// actually paid for, not just which token it carried.
var checksMeaning = map[string]string{
	"removed":  "debug asserts and bounds/range checks compile out",
	"always":   "bounds, range and sticky-error checks in every build by contract",
	"contract": "debug asserts compile out; wire/API contract validation stays in every build",
}

func checksSemantics(v string) string {
	if m, ok := checksMeaning[v]; ok {
		return m
	}
	return "semantics unrecorded — not a §3.4 value"
}

type key struct{ lang, bench, path, codec string }

// codecs is the presentation dimension of the §5.1 codec column: "" for
// every row whose language ships one codec (all five AOT languages, and the
// js rt/bits families, which measure the runtime library itself), then the
// two js generated tiers — flat is THE js path (the 2026-08-18 ruling),
// runtime rides as labeled supplementary rows.
var codecs = []string{"", "flat", "runtime"}

type row struct {
	iters, bytes, runs      int
	med, mn, mx, mb, spread float64
	// CSV v2 columns (§5.1). v1 rows carry corpusID "" and inline "unknown".
	corpusID, family, linkage, checks, opt, inline string
	// the appended codec column (§5.1, 2026-08-18): "" | "flat" | "runtime"
	codec string

	file int    // index into the dataset's identities
	raw  string // the CSV line as loaded, printed verbatim in refusals
	line int
}

// dataset is rows plus the preamble identity of every file they came from —
// the preamble is CARRIED THROUGH the merge (§5.3), not discarded.
type dataset struct {
	rows map[key]row
	ids  []identity
}

// identity is the preamble subset that decides comparability. Everything here
// can move a number by more than the effects being measured: an anonymous
// namespace on one benchmark's packet types moved a figure 2x with no library
// change, and the same source at -O2 and -O3 can reverse which language wins
// a row.
type identity struct {
	path                   string
	host, arch, cpu, build string
	cppCompiler, cppFlags  string
	cCompiler, cFlags      string
	goVer, rustVer, netVer string
	window                 string // "" (unstated), else the # window: verdict
	// §2.6.1: rows the pass's own twin gate marked state-suspect, from the
	// `# twin_suspect:` preamble line. Such a row never enters a ratio.
	twinSuspect map[key]bool
}

func (i identity) String() string {
	return fmt.Sprintf("%s/%s %s build=%s", i.host, i.arch, i.cpu, i.build)
}

// differs returns the fields on which two runs disagree — the whole-file
// comparability check used at merge time. Ratio-time checks are per-row
// (guardRatio) and strictly per §5.3.
func (i identity) differs(j identity) []string {
	var d []string
	cmp := func(name, a, b string) {
		if a != b {
			d = append(d, fmt.Sprintf("%s:\n      A: %s\n      B: %s", name, a, b))
		}
	}
	cmp("host", i.host, j.host)
	cmp("arch", i.arch, j.arch)
	cmp("cpu", i.cpu, j.cpu)
	cmp("build", i.build, j.build)
	cmp("cpp compiler", i.cppCompiler, j.cppCompiler)
	cmp("cpp flags", i.cppFlags, j.cppFlags)
	cmp("c compiler", i.cCompiler, j.cCompiler)
	cmp("c flags", i.cFlags, j.cFlags)
	cmp("go", i.goVer, j.goVer)
	cmp("rust", i.rustVer, j.rustVer)
	cmp("dotnet", i.netVer, j.netVer)
	return d
}

// load reads one CSV, skipping preamble lines into meta and keeping only
// Release rows. Debug rows would follow a second preamble; the harness is
// never run --debug for published numbers. Both v1 (11 columns) and v2
// (17 columns, §5.1) rows load; v1 rows become un-ratioable.
func load(path string) (map[key]row, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }() // read-only: a close error carries nothing

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
		if len(fs) != 11 && len(fs) != 17 && len(fs) != 18 {
			return nil, nil, fmt.Errorf("%s:%d: expected 11 (v1), 17 (v2) or 18 (v2 + codec) fields, got %d", path, lineNo, len(fs))
		}
		num := func(i int) float64 {
			v, err := strconv.ParseFloat(strings.TrimSpace(fs[i]), 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s:%d: field %d: %v\n", path, lineNo, i, err)
				os.Exit(2)
			}
			return v
		}
		r := row{
			iters: int(num(3)), bytes: int(num(4)), runs: int(num(5)),
			med: num(6), mn: num(7), mx: num(8), mb: num(9), spread: num(10),
			corpusID: "", inline: "unknown", // v1: un-ratioable by §5.3
			raw: line, line: lineNo,
		}
		if len(fs) >= 17 {
			r.corpusID = strings.TrimSpace(fs[11])
			r.family = strings.TrimSpace(fs[12])
			r.linkage = strings.TrimSpace(fs[13])
			r.checks = strings.TrimSpace(fs[14])
			r.opt = strings.TrimSpace(fs[15])
			r.inline = strings.TrimSpace(fs[16])
		}
		if len(fs) == 18 {
			r.codec = strings.TrimSpace(fs[17])
		}
		rows[key{fs[0], fs[1], fs[2], r.codec}] = r
	}
	return rows, meta, sc.Err()
}

func parseIdentity(path string, meta []string) identity {
	id := identity{path: path}
	get := func(line, prefix string) (string, bool) {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(after), true
		}
		return "", false
	}
	for _, m := range meta {
		if v, ok := get(m, "# host:"); ok {
			// "host: x  arch: y  os: z" arrives on one line
			for part := range strings.SplitSeq(v, "  ") {
				part = strings.TrimSpace(part)
				switch {
				case strings.HasPrefix(part, "arch:"):
					id.arch = strings.TrimSpace(strings.TrimPrefix(part, "arch:"))
				case part != "" && id.host == "":
					id.host = part
				}
			}
		}
		if v, ok := get(m, "# cpu:"); ok {
			id.cpu = v
		}
		if v, ok := get(m, "# build:"); ok {
			id.build = v
		}
		if v, ok := get(m, "# cpp compiler:"); ok {
			id.cppCompiler = v
		}
		if v, ok := get(m, "# cpp flags:"); ok {
			id.cppFlags = v
		}
		if v, ok := get(m, "# c compiler:"); ok {
			id.cCompiler = v
		}
		if v, ok := get(m, "# c flags:"); ok {
			id.cFlags = v
		}
		if v, ok := get(m, "# go:"); ok {
			id.goVer = v
		}
		if v, ok := get(m, "# rust:"); ok {
			id.rustVer = v
		}
		if v, ok := get(m, "# dotnet:"); ok {
			id.netVer = v
		}
		// "# window: OK" alone, or "# control_delta_pct: 2.3   window: OK"
		// (never the twin_gate line, which carries its own verdict)
		if idx := strings.LastIndex(m, "window:"); idx >= 0 && !strings.HasPrefix(m, "# twin") {
			id.window = strings.TrimSpace(m[idx+len("window:"):])
		}
		// §2.6.1: "# twin_suspect: lang/bench/path reason;lang/bench/path reason"
		if v, ok := get(m, "# twin_suspect:"); ok {
			if id.twinSuspect == nil {
				id.twinSuspect = map[key]bool{}
			}
			for entry := range strings.SplitSeq(v, ";") {
				entry = strings.TrimSpace(entry)
				name, _, _ := strings.Cut(entry, " ")
				parts := strings.Split(name, "/")
				// lang/bench/path, or lang/bench/path/codec for tiered rows
				if len(parts) == 4 {
					id.twinSuspect[key{parts[0], parts[1], parts[2], parts[3]}] = true
				} else if len(parts) == 3 {
					id.twinSuspect[key{parts[0], parts[1], parts[2], ""}] = true
				}
			}
		}
	}
	return id
}

// refuse reports why two things cannot be compared and exits non-zero,
// rather than dividing two numbers that were produced under different
// conditions. Every wrong cross-language claim in this estate has come from
// exactly that: two numbers put side by side that measured different work.
// A tool that cannot refuse is not a check. It never prints a number.
func refuse(what string, a, b string, diffs []string) {
	fmt.Fprintf(os.Stderr, "relative: REFUSING to %s\n\n", what)
	fmt.Fprintf(os.Stderr, "  A: %s\n  B: %s\n\n", a, b)
	fmt.Fprintln(os.Stderr, "  they disagree on:")
	for _, d := range diffs {
		fmt.Fprintf(os.Stderr, "    %s\n", d)
	}
	fmt.Fprintln(os.Stderr, "\n  These numbers are not comparable (BENCH-STANDARD.md §5.3).")
	fmt.Fprintln(os.Stderr, "  There is no override. Re-measure both legs on one machine, in one")
	fmt.Fprintln(os.Stderr, "  window, with matched conditions — the fix is another measurement,")
	fmt.Fprintln(os.Stderr, "  not a flag.")
	os.Exit(3)
}

// refuseRatio is the §5.3 refusal: both rows, the differing fields, exit 3.
func refuseRatio(ds *dataset, ka, kb key, a, b row, diffs []string) {
	ida, idb := ds.ids[a.file], ds.ids[b.file]
	aDesc := fmt.Sprintf("%s:%d: %s", ida.path, a.line, a.raw)
	bDesc := fmt.Sprintf("%s:%d: %s", idb.path, b.line, b.raw)
	refuse(fmt.Sprintf("ratio %s/%s/%s against %s/%s/%s", ka.lang, ka.bench, ka.path,
		kb.lang, kb.bench, kb.path), aDesc, bDesc, diffs)
}

// captions accumulates the §3.4/§3.1 labels a captioned escape requires; they
// are printed under any table whose ratios crossed a contract difference.
var captions = map[string]bool{}

// guardRatio implements §5.3's nine refusal rules for one pair of rows.
// It returns the reasons the ratio must be refused; an empty slice means the
// rows are comparable (possibly with captions recorded for the escapes).
func guardRatio(ds *dataset, ka, kb key, a, b row) []string {
	var d []string
	ida, idb := ds.ids[a.file], ds.ids[b.file]

	// 1. corpus_id — or either empty (v1 rows land here)
	switch {
	case a.corpusID == "" || b.corpusID == "":
		d = append(d, "corpus_id: empty (v1 row or missing verdict pass) — un-ratioable")
	case a.corpusID != b.corpusID:
		d = append(d, fmt.Sprintf("corpus_id: A=%s B=%s (the two runners did not load the same goldens)", a.corpusID, b.corpusID))
	}
	// 2. bytes_per_op
	if a.bytes != b.bytes {
		d = append(d, fmt.Sprintf("bytes_per_op: A=%d B=%d", a.bytes, b.bytes))
	}
	// 3. family
	if a.family != b.family {
		d = append(d, fmt.Sprintf("family: A=%q B=%q", a.family, b.family))
	}
	// 4. opt — two EXPLICIT levels must match: C at -O2 against C++ at -O3
	// is the §0 coin flip and always refuses. `default` is different in
	// kind: §3.3 records it for languages that HAVE no optimization level
	// (Go, C#), so their one row is that language's only truth and rides
	// in every level's table — refusing it would make the five-language
	// table unprintable by construction, which the §5.3 amendment of
	// 2026-08-15 names as the bug it was. The mix is captioned, always,
	// no flag: it is a definition, not an escape.
	if a.opt != b.opt {
		if a.opt == "default" || b.opt == "default" {
			captions[fmt.Sprintf("%s opt=%s vs %s opt=%s — a language without an optimization-level knob rides in every level's table (§3.3).",
				ka.lang, a.opt, kb.lang, b.opt)] = true
		} else {
			d = append(d, fmt.Sprintf("opt: A=%q B=%q", a.opt, b.opt))
		}
	}
	// 5. checks (unless --label-checks, which prints the §3.4 caption).
	// Three-value logic (§3.4): ANY two differing values refuse —
	// removed/always, removed/contract, contract/always alike — and the
	// caption names both sides' SEMANTICS, not just the tokens.
	if a.checks != b.checks {
		if labelChecks {
			captions[fmt.Sprintf("%s checks=%s (%s) vs %s checks=%s (%s) — this ratio includes the cost of a different safety contract.",
				ka.lang, a.checks, checksSemantics(a.checks), kb.lang, b.checks, checksSemantics(b.checks))] = true
		} else {
			d = append(d, fmt.Sprintf("checks: A=%q B=%q (a contract difference, not a language difference — --label-checks prints it captioned)", a.checks, b.checks))
		}
	}
	// 6. linkage (unless --cross-linkage, likewise captioned)
	if a.linkage != b.linkage {
		if crossLinkage {
			captions[fmt.Sprintf("%s linkage=%s vs %s linkage=%s — this ratio includes the runtimes' packaging difference.",
				ka.lang, a.linkage, kb.lang, b.linkage)] = true
		} else {
			d = append(d, fmt.Sprintf("linkage: A=%q B=%q (a packaging difference — --cross-linkage prints it captioned)", a.linkage, b.linkage))
		}
	}
	// 7. inline unknown on either side
	if a.inline == "unknown" || b.inline == "unknown" {
		d = append(d, fmt.Sprintf("inline: A=%q B=%q (a number without an inline verdict is not comparable to one with it)", a.inline, b.inline))
	}
	// 10. codec (appended 2026-08-18 with the js flat tier): a runtime-call
	// generated number and a flat number measure different shipped code —
	// a ratio across them is a code-change delta wearing a language-delta
	// costume. "" (a language with one codec, or a pre-codec CSV) never
	// matches a labeled tier either: the old js rows were runtime-call.
	if a.codec != b.codec {
		d = append(d, fmt.Sprintf("codec: A=%q B=%q (different shipped codecs are different measurements)", a.codec, b.codec))
	}
	// 8. the source CSVs' cpu lines, or the flag lines for the languages
	// involved. A MISSING # cpu line refuses too: two absent preambles
	// compare "" == "" and would certify any two machines as the same one.
	if ida.cpu == "" || idb.cpu == "" {
		d = append(d, fmt.Sprintf("# cpu: A=%q B=%q — a CSV without a # cpu line carries no machine identity, so nothing certifies these rows as comparable", ida.cpu, idb.cpu))
	} else if ida.cpu != idb.cpu {
		d = append(d, fmt.Sprintf("# cpu: A=%q B=%q", ida.cpu, idb.cpu))
	}
	for _, lang := range []string{ka.lang, kb.lang} {
		switch lang {
		case "cpp":
			if ida.cppCompiler != idb.cppCompiler || ida.cppFlags != idb.cppFlags {
				d = append(d, fmt.Sprintf("cpp compiler/flags:\n      A: %s | %s\n      B: %s | %s", ida.cppCompiler, ida.cppFlags, idb.cppCompiler, idb.cppFlags))
			}
		case "c":
			if ida.cCompiler != idb.cCompiler || ida.cFlags != idb.cFlags {
				d = append(d, fmt.Sprintf("c compiler/flags:\n      A: %s | %s\n      B: %s | %s", ida.cCompiler, ida.cFlags, idb.cCompiler, idb.cFlags))
			}
		case "go":
			if ida.goVer != idb.goVer {
				d = append(d, fmt.Sprintf("go: A=%q B=%q", ida.goVer, idb.goVer))
			}
		case "rust":
			if ida.rustVer != idb.rustVer {
				d = append(d, fmt.Sprintf("rust: A=%q B=%q", ida.rustVer, idb.rustVer))
			}
		case "cs":
			if ida.netVer != idb.netVer {
				d = append(d, fmt.Sprintf("dotnet: A=%q B=%q", ida.netVer, idb.netVer))
			}
		}
	}
	// 9. either row invalid by spread (§2.3) or window (§2.6)
	if a.spread > spreadInvalid {
		d = append(d, fmt.Sprintf("spread_pct: A=%.1f%% > %.0f%% — row A is INVALID", a.spread, spreadInvalid))
	}
	if b.spread > spreadInvalid {
		d = append(d, fmt.Sprintf("spread_pct: B=%.1f%% > %.0f%% — row B is INVALID", b.spread, spreadInvalid))
	}
	// A stated window verdict must be exactly OK (case-folded): "invalid",
	// "Invalid" or any other value refuses. The old == "INVALID" comparison
	// let a lowercase "# window: invalid" publish ratios.
	windowBad := func(w string) bool { return w != "" && !strings.EqualFold(w, "OK") }
	if windowBad(ida.window) {
		d = append(d, fmt.Sprintf("window: %s is marked # window: %s — only OK publishes (its control legs disagree, or the verdict is not one this tool knows)", ida.path, ida.window))
	}
	if windowBad(idb.window) && idb.path != ida.path {
		d = append(d, fmt.Sprintf("window: %s is marked # window: %s — only OK publishes (its control legs disagree, or the verdict is not one this tool knows)", idb.path, idb.window))
	}
	// §2.6.1: a row the pass's own twin gate marked state-suspect never
	// enters a ratio — twin disagreement is state-selective interference.
	if ida.twinSuspect[ka] {
		d = append(d, fmt.Sprintf("twin gate: %s/%s/%s is marked state-suspect in %s — twin disagreement, state-selective interference (§2.6.1); re-measure the window", ka.lang, ka.bench, ka.path, ida.path))
	}
	if idb.twinSuspect[kb] {
		d = append(d, fmt.Sprintf("twin gate: %s/%s/%s is marked state-suspect in %s — twin disagreement, state-selective interference (§2.6.1); re-measure the window", kb.lang, kb.bench, kb.path, idb.path))
	}
	return d
}

func merge(paths []string) *dataset {
	ds := &dataset{rows: map[key]row{}}
	for _, p := range paths {
		r, meta, err := load(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		id := parseIdentity(p, meta)
		if len(ds.ids) > 0 {
			if d := ds.ids[0].differs(id); len(d) > 0 {
				refuse("merge across mismatched runs",
					ds.ids[0].path+" ("+ds.ids[0].String()+")", p+" ("+id.String()+")", d)
			}
		}
		fileIdx := len(ds.ids)
		ds.ids = append(ds.ids, id)
		for k, v := range r {
			v.file = fileIdx
			if old, exists := ds.rows[k]; exists {
				// A later file may only replace a row it agrees with on the
				// measurement conditions; silent overwrites are how rows from
				// a stale corpus disappeared into published tables.
				if old.corpusID != v.corpusID || old.bytes != v.bytes || old.family != v.family ||
					old.linkage != v.linkage || old.checks != v.checks || old.opt != v.opt {
					refuse(fmt.Sprintf("overwrite row %s/%s/%s during merge", k.lang, k.bench, k.path),
						fmt.Sprintf("%s:%d: %s", ds.ids[old.file].path, old.line, old.raw),
						fmt.Sprintf("%s:%d: %s", p, v.line, v.raw),
						[]string{"corpus_id / bytes_per_op / family / linkage / checks / opt do not all match"})
				}
				// Identical identity is not license to replace either: two
				// rows for one key whose NUMBERS differ are two measurements,
				// and a merge that keeps the later one silently launders a
				// re-run into the table. A re-run belongs in a new file or an
				// aggregate, never a silent replace. (A byte-identical row —
				// the same file loaded twice — is harmless and passes.)
				if old.iters != v.iters || old.runs != v.runs ||
					old.med != v.med || old.mn != v.mn || old.mx != v.mx ||
					old.mb != v.mb || old.spread != v.spread {
					refuse(fmt.Sprintf("overwrite row %s/%s/%s during merge", k.lang, k.bench, k.path),
						fmt.Sprintf("%s:%d: %s", ds.ids[old.file].path, old.line, old.raw),
						fmt.Sprintf("%s:%d: %s", p, v.line, v.raw),
						[]string{"identical measurement identity but different numbers — a re-run belongs in a new file or an aggregate, never a silent replace"})
				}
			}
			ds.rows[k] = v
		}
	}
	return ds
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

// rel is the median across the family-gen corpus of c_rate/lang_rate as a
// percentage, computed on the §2.2 headline (max_msgs_per_sec). C is the
// reference (Glenn, 2026-08-17). The corpus is ONE shape since #199, so the
// median is that shape's ratio; the loop stays a median because the shape
// count is a corpus fact, not a table fact.
//
// HIGHER IS SLOWER, and this is the one thing to get right about this table.
// The headline column is a RATE — so dividing C's rate by the language's
// rate gives the factor by which that language is slower. A language at 200%
// takes twice as long as C; one at 50% is twice as fast.
//
// §2.3: benches where either side's spread exceeds 15% are noisy and are
// EXCLUDED from this corpus median (noted on stderr); spreads over 40% never
// get here — guardRatio refuses first.
func rel(ds *dataset, lang, path string) (float64, bool) {
	var ratios []float64
	for _, b := range benchCorpus {
		kc, kl := key{reference, b, path, ""}, key{lang, b, path, ""}
		c, okc := ds.rows[kc]
		l, okl := ds.rows[kl]
		if !okc || !okl || l.mx == 0 {
			return 0, false
		}
		if d := guardRatio(ds, kc, kl, c, l); len(d) > 0 {
			refuseRatio(ds, kc, kl, c, l, d)
		}
		if c.spread > spreadNoisy || l.spread > spreadNoisy {
			fmt.Fprintf(os.Stderr, "note: excluding %s %s from the %s corpus median: spread %s %.1f%% / %s %.1f%% exceeds %.0f%% (§2.3 noisy)\n",
				b, path, lang, reference, c.spread, lang, l.spread, spreadNoisy)
			continue
		}
		ratios = append(ratios, c.mx/l.mx*100.0)
	}
	if len(ratios) == 0 {
		return 0, false
	}
	return median(ratios), true
}

func printCaptions() {
	if len(captions) == 0 {
		return
	}
	var lines []string
	for c := range captions {
		lines = append(lines, c)
	}
	sort.Strings(lines)
	for _, c := range lines {
		fmt.Printf("<!-- CAPTION: %s -->\n", c)
	}
}

func relativeTable(ds *dataset) string {
	out := []string{
		"<!-- HIGHER IS SLOWER: each cell is C's best rate divided by this",
		"     language's best rate, so 200% means it takes twice as long.",
		"     C is the reference (Glenn, 2026-08-17). -->",
		"| backend | write | round_trip |",
		"|---|---:|---:|",
		"| C | 100% | 100% |",
	}
	for _, l := range langs {
		if l.key == reference {
			continue
		}
		if l.key == "js" {
			// DELIBERATE: js rows are inline=unknown by construction (no
			// AOT artifact to verdict, §4.1) and §5.3 refuses ratios on
			// unknown — attempting the ratio would refuse the whole table.
			// js publishes in the absolute table only.
			continue
		}
		// write and round_trip are the gen family's two MEASURED paths
		// under §2.9; the data-driven driver derives read to stderr and
		// emits no read row, so ratioing "read" here would print a table
		// of dashes.
		w, okw := rel(ds, l.key, "write")
		r, okr := rel(ds, l.key, "round_trip")
		if !okw || !okr {
			out = append(out, fmt.Sprintf("| %s | — | — |", l.name))
			continue
		}
		out = append(out, fmt.Sprintf("| %s | **%.0f%%** | **%.0f%%** |",
			l.name, w, r))
	}
	return strings.Join(out, "\n")
}

// absoluteTable is long-format: one row per (bench, path, lang), the §2.2
// headline (best) first with median/min/spread beside it, never optional.
// Noisy rows carry their mark; invalid rows print no numbers (§2.3).
func absoluteTable(ds *dataset) string {
	out := []string{
		"| bench | path | lang | B/msg | best M msg/s | median | min | spread% | |",
		"|---|---|---|---:|---:|---:|---:|---:|---|",
	}
	for _, b := range order {
		for _, p := range paths {
			for _, l := range langs {
				for _, c := range codecs {
					x, ok := ds.rows[key{l.key, b, p, c}]
					if !ok {
						continue
					}
					langLabel := l.key
					codecMark := ""
					if c != "" {
						langLabel = l.key + " (" + c + ")"
						if c == "runtime" {
							codecMark = "supplementary (codec=runtime)"
						}
					}
					if x.spread > spreadInvalid {
						fmt.Fprintf(os.Stderr, "INVALID: %s %s %s spread %.1f%% > %.0f%% — row does not publish (§2.3)\n",
							langLabel, b, p, x.spread, spreadInvalid)
						out = append(out, fmt.Sprintf("| %s | %s | %s | %d | — | — | — | %.1f | INVALID (spread > %.0f%%) |",
							b, p, langLabel, x.bytes, x.spread, spreadInvalid))
						continue
					}
					mark := codecMark
					if x.spread > spreadNoisy {
						if mark != "" {
							mark += ", noisy"
						} else {
							mark = "noisy"
						}
					}
					out = append(out, fmt.Sprintf("| %s | %s | %s | %d | %.2f | %.2f | %.2f | %.1f | %s |",
						b, p, langLabel, x.bytes, x.mx/1e6, x.med/1e6, x.mn/1e6, x.spread, mark))
				}
			}
		}
	}
	return strings.Join(out, "\n")
}

// abTable reports per-row B/A multipliers on the §2.2 headline alongside both
// spreads, so a move can be judged against the noise rather than eyeballed.
// Every pair passes the §5.3 guard first.
func abTable(dsa, dsb *dataset, lang, labelA, labelB string) string {
	out := []string{
		fmt.Sprintf("| bench | path | %s best M msg/s | %s best M msg/s | %s/%s | spread %s%% | spread %s%% |",
			labelA, labelB, labelB, labelA, labelA, labelB),
		"|---|---|---:|---:|---:|---:|---:|",
	}
	// one combined dataset so guardRatio can see both identities
	comb := &dataset{rows: map[key]row{}, ids: append(append([]identity{}, dsa.ids...), dsb.ids...)}
	for _, bench := range order {
		for _, p := range []string{"write", "read"} {
			for _, c := range codecs {
				k := key{lang, bench, p, c}
				ra, oka := dsa.rows[k]
				rb, okb := dsb.rows[k]
				if !oka || !okb || ra.mx == 0 {
					continue
				}
				rb.file += len(dsa.ids)
				if d := guardRatio(comb, k, k, ra, rb); len(d) > 0 {
					refuseRatio(comb, k, k, ra, rb, d)
				}
				label := bench
				if c != "" {
					label = bench + " (" + c + ")"
				}
				out = append(out, fmt.Sprintf("| %s | %s | %.2f | %.2f | **%.2fx** | %.1f | %.1f |",
					label, p, ra.mx/1e6, rb.mx/1e6, rb.mx/ra.mx, ra.spread, rb.spread))
			}
		}
	}
	return strings.Join(out, "\n")
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  relative [flags] rel      results.csv [more.csv...]   the relative table (% of C, the reference; best rate)
  relative [flags] abs      results.csv [more.csv...]   absolute rates per language (best + median/min/spread)
  relative [flags] ab       a.csv b.csv [lang [A B]]    B/A deltas on best rates, with spreads
  relative spreads  <pct> results.csv [...]             rows noisier than pct
  relative aggregate     round0.csv round1.csv [...]    merge per-round rows (§2.4; driver computes stats)
  relative controlmedian control.csv                    corpus-median headline of a control leg (§2.6)
  relative twingate      twin-a.csv twin-b.csv          §2.6.1 A/A twin-leg gate (state-suspect rows exit 4)
  relative bands         current.csv prior.csv [...]    §2.6.1 per-row historical bands (band-break rows exit 5)
  relative ledger [--check] [--dir DIR]                 the absolute ledger series (#194); --check reds a cpp regression

flags (the only escapes; each prints its §3.4/§3.1 caption with the ratio):
  --label-checks    allow ratios across differing checks columns, captioned
  --cross-linkage   allow ratios across differing linkage columns, captioned

Ratios refuse on any BENCH-STANDARD.md §5.3 mismatch (corpus_id, bytes_per_op,
family, opt, checks, linkage, inline=unknown, cpu/flag preambles, spread > 40%,
window INVALID). There is no --force.
`)
	os.Exit(2)
}

func main() {
	args := []string{}
	for _, a := range os.Args[1:] {
		switch a {
		case "--label-checks":
			labelChecks = true
		case "--cross-linkage":
			crossLinkage = true
		default:
			args = append(args, a)
		}
	}
	if len(args) < 1 {
		usage()
	}
	switch args[0] {
	case "ledger":
		ledger(args[1:])
	case "rel":
		if len(args) < 2 {
			usage()
		}
		fmt.Println(relativeTable(merge(args[1:])))
		printCaptions()
	case "abs":
		if len(args) < 2 {
			usage()
		}
		fmt.Println(absoluteTable(merge(args[1:])))
	case "ab":
		if len(args) < 3 {
			usage()
		}
		dsa := merge(args[1:2])
		dsb := merge(args[2:3])
		// ab divides one run by the other, so a whole-file mismatch here is
		// the most misleading thing this tool can produce: a confident
		// multiplier attributing a machine or a flag to a code change. The
		// per-row §5.3 guard still runs on every pair.
		if d := dsa.ids[0].differs(dsb.ids[0]); len(d) > 0 {
			refuse("compare runs", args[1]+" ("+dsa.ids[0].String()+")", args[2]+" ("+dsb.ids[0].String()+")", d)
		}
		lang, la, lb := "cpp", "A", "B"
		if len(args) > 3 {
			lang = args[3]
		}
		if len(args) > 4 {
			la = args[4]
		}
		if len(args) > 5 {
			lb = args[5]
		}
		fmt.Println(abTable(dsa, dsb, lang, la, lb))
		printCaptions()
	case "aggregate":
		aggregateCmd(args[1:])
	case "controlmedian":
		controlMedianCmd(args[1:])
	case "twingate":
		twingateCmd(args[1:])
	case "bands":
		bandsCmd(args[1:])
	case "spreads":
		if len(args) < 3 {
			usage()
		}
		pct, err := strconv.ParseFloat(args[1], 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		ds := merge(args[2:])
		keys := make([]key, 0, len(ds.rows))
		for k := range ds.rows {
			if ds.rows[k].spread > pct {
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
			fmt.Printf("%s %s %s  %.1f%%\n", k.lang, k.bench, k.path, ds.rows[k].spread)
		}
	default:
		usage()
	}
}
