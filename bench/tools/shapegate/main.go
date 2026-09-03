// shapegate — the mechanical guarantee behind the one-benchmark rule.
//
//	go run ./bench/tools/shapegate        # or: make shape-gate
//
// THE RULE IT ENFORCES. The estate has exactly one sanctioned benchmark: this
// repo's data-driven bench. Shape knowledge lives in two places and nowhere
// else — bench/corpus/*.schema (the definition) and the code the compiler
// GENERATES from it. The nine language runners are hand-written and stay that
// way, but they are SHAPE-BLIND: timing, buffers, CSV and loops only. A runner
// that names a field, hardcodes a wire size, or grows its own timed loop over a
// hand-serialized struct is the divergence class the owner named — nine
// hand-written approximations of one benchmark, drifting apart silently.
//
// Prose cannot hold that line. This does.
//
// WHAT IT CHECKS.
//
//	names     No shape identifier from bench/corpus/*.schema — type, enum,
//	          union, flags or field name, in any case form — appears anywhere
//	          under bench/. The vocabulary is EXTRACTED from the corpus, so it
//	          tracks the corpus automatically: rename a field and the gate
//	          starts guarding the new name in the same commit. Scoped to the
//	          measurement tier on purpose — naming a shape is not a benchmark,
//	          and test/ names them constantly and correctly.
//
//	timing    No timing primitive appears anywhere in the repository outside
//	          the sanctioned runner and tool directories. This is what catches
//	          a scratch harness parked in bench/results/, an ad-hoc timer added
//	          to a test, or a perf example under cmd/.
//
//	paths     No SOURCE file whose path says "benchmark" (bench, perf, profile,
//	          timing, throughput, criterion, jmh) appears outside the
//	          sanctioned prefixes. Result data and documentation under bench/
//	          are not source and are not scanned.
//
//	consts    No distinctive wire constant of a corpus shape — its byte size,
//	          its bit count — appears as a literal in a runner. A shape-blind
//	          runner derives sizes from the committed corpus at run time; a
//	          hand-coded one has to write the shape's size down somewhere.
//
//	ledger    Every exemption in bench/SHAPE-GATE.allow still matches a real
//	          file at exactly its recorded count. An entry whose file is gone
//	          FAILS. An entry whose count DROPPED fails too, and prints the new
//	          number to write. The debt can only shrink.
//
// WHAT IT CANNOT SEE. Stated plainly, because a gate that oversells itself is
// worse than none:
//
//   - It runs in THIS repository's CI. A hand-coded serialize benchmark in
//     another repository is outside its reach entirely.
//   - Markdown is not scanned. A benchmark pasted into a fenced code block in a
//     .md file passes.
//   - The name vocabulary drops identifiers shorter than 4 characters and a
//     stoplist of ordinary programming words, because `x`, `if` and `delta` are
//     corpus field names and grepping for them would fire on everything. A
//     hand-coded bench written using ONLY those names would pass — it would
//     also be unreadable.
//   - Wire constants below 32 are not distinctive enough to guard, so the 14
//     and 20 byte shapes are guarded by name only.
//   - It is a lexical check. Code that computes a field name or a size at run
//     time to evade it passes, and nothing short of a human reading the diff
//     would catch that.
//   - It guards MEASUREMENT, not CORRECTNESS. A shape-blind runner driving a
//     defective emitter is still shape-blind and still passes here. #198 is
//     the worked example: the Go emitter wrote a fixed scalar array twice, 32
//     wire bytes where the other eight languages write 16, and Go-to-Go
//     round-trips passed clean because both ends shared the defect. Nothing in
//     this gate can see that. Cross-language wire agreement is the conformance
//     suite's job, and issue #203 records the corpus blind spot that let it
//     through.
//   - It does not read comments differently from code. A shape NAME in a prose
//     comment counts as a hit, which is why several ledger entries below are
//     comments and say so. That is deliberate: a hand-coded bench announces
//     itself in its own header comment, and teaching the gate to skip comments
//     would teach it to skip the announcement.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ------------------------------------------------------------------------------------------
// where the sanctioned machinery lives
// ------------------------------------------------------------------------------------------

// runnerDirs — the nine language legs and the bench tooling. Timing primitives
// and bench-shaped paths are legal here and illegal everywhere else.
var runnerDirs = []string{
	"bench/c/",
	"bench/cpp/",
	"bench/cs/",
	"bench/dart/",
	"bench/elixir/",
	"bench/go/",
	"bench/java/",
	"bench/js/",
	"bench/rust/",
	"bench/tools/",
	"bench/run.sh",
	// the TABLES leg (bench/tables/README.md): its own runners, its own
	// registry and its own pass, for the corpus_id reason that page states.
	// Same rule inside it as everywhere else — a runner names the generated
	// type at one call site and no field, and the ledger below counts it.
	"bench/tables/",
}

// corpusDir — the shape definition and its committed data. Shape names are
// legal here by definition; this is the one place they are supposed to be.
const corpusDir = "bench/corpus/"

// goldenGlob — the committed wire goldens. Their FILE SIZES are the shapes'
// wire sizes, so the constant vocabulary needs no parsing to be authoritative.
const goldenGlob = "testdata/wire/bench_*.bin"

const ledgerPath = "bench/SHAPE-GATE.allow"

// sourceExts — the file kinds a benchmark can be written in.
var sourceExts = map[string]bool{
	".c": true, ".h": true, ".inc": true, ".cpp": true, ".hpp": true, ".cc": true,
	".cs": true, ".go": true, ".rs": true, ".java": true, ".js": true, ".mjs": true,
	".dart": true, ".ex": true, ".exs": true, ".sh": true, ".py": true, ".rb": true,
	".kt": true, ".swift": true, ".zig": true,
}

// skipDirs — machine-written, vendored or downloaded trees. Generated code
// names shapes because that is its whole job; scanning it would flag the
// correct design.
//
// `dist` is the Makefile's pinned toolchain drop (the Dart SDK, the JDK, OTP,
// Elixir — see the DART/JAVA/BEAM_PATH comments at the top of the Makefile).
// It is gitignored and absent on CI, which uses setup-dart/setup-java instead,
// so the gate only ever meets it on a fully provisioned developer machine. The
// Dart SDK alone ships `lib/core/stopwatch.dart`; without this line `make
// shape-gate` refuses on exactly the trees that can run the whole bench.
var skipDirs = map[string]bool{
	".git": true, "generated": true, "testdata": true, "build": true,
	"bin": true, "vendor": true, "node_modules": true, "target": true,
	"dist": true,
}

// ------------------------------------------------------------------------------------------
// check 1 — the shape vocabulary, extracted from the corpus
// ------------------------------------------------------------------------------------------

// minNameLen — shorter identifiers are not distinctive. `a`, `b`, `c`, `x`,
// `y`, `z`, `f0`..`f9`, `b7`..`b48` are all real corpus field names and all
// unguardable. Named in the package doc as a known blind spot.
const minNameLen = 4

// nameStoplist — corpus field names that are also ordinary programming words.
// Guarding them would fire on every runner's own variables.
var nameStoplist = map[string]bool{
	"align": true, "flag": true, "bits": true, "blob": true, "delta": true,
	"stats": true, "hits": true, "type": true, "size": true, "data": true,
	"item": true, "kind": true, "next": true, "byte": true, "word": true,
	"read": true, "write": true, "name": true, "value": true, "count": true,
	"index": true, "hash": true, "seed": true, "mask": true, "base": true,
	// English words that happen to be corpus field names. They collide with
	// ordinary prose ("a clean bill of health", "cannot drift from") and would
	// make the gate cry wolf in comments, which is how gates get switched off.
	"chat": true, "drift": true, "health": true,
}

var (
	// `table` rides here beside `type`: bench/corpus/BenchTable.schema
	// declares the tables leg's shape and its nested records as tables, and a
	// declaration keyword missing from this list is a shape name the gate
	// does not guard at all.
	declRe  = regexp.MustCompile(`^\s*(?:type|table|enum|union|flags)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	fieldRe = regexp.MustCompile(`^\s+([a-z_][a-z0-9_]*)\s`)
	bitsRe  = regexp.MustCompile(`=\s*(\d+)\s*bits`)
)

// vocabulary reads every .schema under bench/corpus and returns the guarded
// identifiers, in every case form a port would spell them.
func vocabulary(root string) (map[string]string, []string, error) {
	files, err := filepath.Glob(filepath.Join(root, corpusDir, "*.schema"))
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no .schema files under %s — the gate refuses to run with an empty vocabulary", corpusDir)
	}

	// guarded maps a spelling to the corpus identifier it came from, so a
	// refusal can say WHICH field was named rather than just "a field was".
	guarded := map[string]string{}
	var roots []string

	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, nil, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			var ident string
			if m := declRe.FindStringSubmatch(line); m != nil {
				ident = m[1]
			} else if m := fieldRe.FindStringSubmatch(line); m != nil {
				ident = m[1]
			}
			if ident == "" {
				continue
			}
			if len(ident) < minNameLen || nameStoplist[strings.ToLower(ident)] {
				continue
			}
			roots = append(roots, ident)
			for _, form := range caseForms(ident) {
				if len(form) >= minNameLen && !nameStoplist[strings.ToLower(form)] {
					guarded[form] = ident
				}
			}
		}
		cerr := fh.Close()
		if err := sc.Err(); err != nil {
			return nil, nil, err
		}
		if cerr != nil {
			return nil, nil, cerr
		}
	}
	if len(guarded) == 0 {
		return nil, nil, fmt.Errorf("corpus parsed but yielded no guardable identifiers — the gate refuses to run blind")
	}
	sort.Strings(roots)
	return guarded, dedupe(roots), nil
}

// caseForms — the spellings one identifier takes across nine languages:
// snake_case as written, lowerCamel, UpperCamel, UPPER_SNAKE, and the
// underscore-free flattening some ports use.
func caseForms(ident string) []string {
	parts := strings.Split(ident, "_")
	var camel, pascal, flat strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		t := strings.ToUpper(p[:1]) + p[1:]
		pascal.WriteString(t)
		flat.WriteString(strings.ToLower(p))
		if i == 0 {
			camel.WriteString(strings.ToLower(p))
		} else {
			camel.WriteString(t)
		}
	}
	return dedupe([]string{
		ident,
		camel.String(),
		pascal.String(),
		strings.ToUpper(ident),
		flat.String(),
	})
}

// ------------------------------------------------------------------------------------------
// check 2 — timing primitives, in every language the estate writes
// ------------------------------------------------------------------------------------------

var timingRe = regexp.MustCompile(strings.Join([]string{
	`clock_gettime`, `std::chrono`, `chrono::`, `QueryPerformanceCounter`,
	`mach_absolute_time`, `__rdtsc`, `\bStopwatch\b`, `\bnanoTime\b`,
	`System\.monotonic_time`, `:timer\.tc`, `process\.hrtime`,
	`performance\.now`, `Instant::now`, `time\.Now\(\)`, `criterion::`,
	`BenchmarkDotNet`, `\*testing\.B\b`, `DateTime\.[Nn]ow`,
	`\bhrtime\b`, `getrusage`, `\bperf_event_open\b`,
}, "|"))

// ------------------------------------------------------------------------------------------
// check 3 — bench-shaped paths
// ------------------------------------------------------------------------------------------

var benchPathRe = regexp.MustCompile(`(?i)(^|/|[._-])(bench|benches|benchmark|benchmarks|perf|profile|profiling|timing|throughput|criterion|jmh)([._-]|/|$)`)

// ------------------------------------------------------------------------------------------
// check 4 — distinctive wire constants
// ------------------------------------------------------------------------------------------

// minConst — below this a number is not distinctive enough to accuse anyone of.
// The two smallest corpus shapes' wire sizes fall out here; those shapes are
// guarded by name only. Named in the package doc.
const minConst = 32

// wireConstants — the byte size of every committed golden, and its bit count.
// Derived from the goldens themselves, which are the corpus's own authority,
// so no comment format or emitter behaviour has to hold for this to be right.
// The exact bit total is additionally read from the schema's `= N bits` trailer
// where the corpus states one.
func wireConstants(root string) (map[int]string, error) {
	out := map[int]string{}

	goldens, err := filepath.Glob(filepath.Join(root, goldenGlob))
	if err != nil {
		return nil, err
	}
	for _, g := range goldens {
		st, err := os.Stat(g)
		if err != nil {
			continue
		}
		shape := strings.TrimSuffix(filepath.Base(g), ".bin")
		n := int(st.Size())
		if n >= minConst {
			out[n] = shape + " wire bytes"
		}
		if n*8 >= minConst {
			out[n*8] = shape + " wire bits"
		}
	}

	schemas, _ := filepath.Glob(filepath.Join(root, corpusDir, "*.schema"))
	for _, s := range schemas {
		b, err := os.ReadFile(s)
		if err != nil {
			continue
		}
		for _, m := range bitsRe.FindAllStringSubmatch(string(b), -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= minConst {
				if _, seen := out[n]; !seen {
					out[n] = "declared bit total in " + filepath.Base(s)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no wire constants derived from %s or %s — the gate refuses to run blind", goldenGlob, corpusDir)
	}
	return out, nil
}

// ------------------------------------------------------------------------------------------
// the ledger
// ------------------------------------------------------------------------------------------

// ledgerEntry — one named, capped, dated exemption. The count is EXACT: the
// debt cannot grow, and when it shrinks the gate says so and makes you write
// the smaller number down. When it reaches zero the entry is deleted.
type ledgerEntry struct {
	check string
	path  string
	count int
	line  int
	why   string
}

func readLedger(root string) ([]ledgerEntry, error) {
	f, err := os.Open(filepath.Join(root, ledgerPath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []ledgerEntry
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			return nil, fmt.Errorf("%s:%d: want `<check> <path> <count> <reason>`, got %q", ledgerPath, n, line)
		}
		c, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: count %q is not a number", ledgerPath, n, parts[2])
		}
		switch parts[0] {
		case "names", "timing", "paths", "consts":
		default:
			return nil, fmt.Errorf("%s:%d: unknown check %q (want names|timing|paths|consts)", ledgerPath, n, parts[0])
		}
		out = append(out, ledgerEntry{check: parts[0], path: parts[1], count: c, line: n, why: parts[3]})
	}
	return out, sc.Err()
}

// ------------------------------------------------------------------------------------------
// the scan
// ------------------------------------------------------------------------------------------

type finding struct {
	check  string
	path   string
	count  int
	detail []string
}

// emitLedger — `-ledger` prints the ledger lines the current tree would need,
// so paying debt down is `go run ./bench/tools/shapegate -ledger`, paste, and
// write the reason for anything that survives. Never run in CI.
var emitLedger bool

func main() {
	root := "."
	for _, a := range os.Args[1:] {
		if a == "-ledger" || a == "--ledger" {
			emitLedger = true
			continue
		}
		root = a
	}
	if err := run(root); err != nil {
		fmt.Fprintf(os.Stderr, "\nSHAPE GATE REFUSAL\n\n%v\n\n%s\n", err, theRule)
		os.Exit(1)
	}
}

func run(root string) error {
	guarded, roots, err := vocabulary(root)
	if err != nil {
		return err
	}
	consts, err := wireConstants(root)
	if err != nil {
		return err
	}
	ledger, err := readLedger(root)
	if err != nil {
		return err
	}

	fmt.Printf("shape gate\n")
	fmt.Printf("  corpus vocabulary : %d identifiers, %d spellings guarded\n", len(roots), len(guarded))
	fmt.Printf("  wire constants    : %s\n", formatConsts(consts))
	fmt.Printf("  ledger            : %d exemption(s)\n\n", len(ledger))

	found := map[string]map[string]*finding{
		"names": {}, "timing": {}, "paths": {}, "consts": {},
	}
	add := func(check, path string, n int, detail []string) {
		if n == 0 {
			return
		}
		found[check][path] = &finding{check: check, path: path, count: n, detail: detail}
	}

	err = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if rel != "." && skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ledgerPath {
			return nil
		}

		if !sourceExts[strings.ToLower(filepath.Ext(rel))] {
			return nil
		}

		// paths — a bench-shaped SOURCE file outside the sanctioned prefixes.
		// Results and docs under bench/ are data, not machinery, and are not
		// source, so they never reach here.
		if benchPathRe.MatchString(rel) && !underRunner(rel) && !strings.HasPrefix(rel, corpusDir) {
			add("paths", rel, 1, []string{"a source file whose path names a benchmark, outside the sanctioned runner directories"})
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		text := string(b)

		// timing — legal only in the runner and tool directories.
		if !underRunner(rel) {
			if hits := timingRe.FindAllString(text, -1); len(hits) > 0 {
				add("timing", rel, len(hits), dedupe(hits))
			}
		}

		// names and consts — the measurement tier only, and never the corpus,
		// which is where shape knowledge is supposed to live.
		//
		// WHY NOT REPO-WIDE. Naming a shape is not a benchmark; TIMING a shape
		// is. test/ names these shapes constantly and should — that is what a
		// conformance suite does — and the compiler internals collide with
		// ordinary field words. Scoping names to
		// bench/ keeps the check precise, and the repo-wide timing check above
		// is what actually catches a benchmark parked anywhere else: it cannot
		// measure without reading a clock.
		if !strings.HasPrefix(rel, "bench/") || strings.HasPrefix(rel, corpusDir) {
			return nil
		}
		if n, which := countNames(text, guarded); n > 0 {
			add("names", rel, n, which)
		}
		if n, which := countConsts(text, consts); n > 0 {
			add("consts", rel, n, which)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if emitLedger {
		for _, check := range []string{"names", "timing", "paths", "consts"} {
			for _, path := range sortedFindingPaths(found[check]) {
				fmt.Printf("%s %s %d REASON\n", check, path, found[check][path].count)
			}
		}
		return nil
	}
	return reconcile(found, ledger)
}

func underRunner(rel string) bool {
	for _, d := range runnerDirs {
		if strings.HasPrefix(rel, d) {
			return true
		}
	}
	return false
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func countNames(text string, guarded map[string]string) (int, []string) {
	n := 0
	seen := map[string]bool{}
	for _, tok := range identRe.FindAllString(text, -1) {
		if root, ok := guarded[tok]; ok {
			n++
			seen[tok+" (corpus: "+root+")"] = true
		}
	}
	return n, sortedKeys(seen)
}

var numRe = regexp.MustCompile(`\b\d+\b`)

func countConsts(text string, consts map[int]string) (int, []string) {
	n := 0
	seen := map[string]bool{}
	for _, tok := range numRe.FindAllString(text, -1) {
		v, err := strconv.Atoi(tok)
		if err != nil {
			continue
		}
		if what, ok := consts[v]; ok {
			n++
			seen[tok+" ("+what+")"] = true
		}
	}
	return n, sortedKeys(seen)
}

// ------------------------------------------------------------------------------------------
// reconciliation — findings against the ledger, in both directions
// ------------------------------------------------------------------------------------------

func reconcile(found map[string]map[string]*finding, ledger []ledgerEntry) error {
	var refusals []string

	allowed := map[string]ledgerEntry{}
	for _, e := range ledger {
		allowed[e.check+" "+e.path] = e
	}

	// direction 1 — findings the ledger does not cover, or covers too thinly.
	for _, check := range []string{"names", "timing", "paths", "consts"} {
		for _, path := range sortedFindingPaths(found[check]) {
			f := found[check][path]
			e, ok := allowed[check+" "+path]
			if !ok {
				refusals = append(refusals, fmt.Sprintf(
					"%s  %s\n    %d hit(s), no ledger entry\n    %s",
					strings.ToUpper(check), path, f.count, strings.Join(f.detail, "\n    ")))
				continue
			}
			if f.count > e.count {
				refusals = append(refusals, fmt.Sprintf(
					"%s  %s\n    %d hit(s), ledger allows %d — the exemption GREW\n    %s",
					strings.ToUpper(check), path, f.count, e.count, strings.Join(f.detail, "\n    ")))
			}
		}
	}

	// direction 2 — ledger hygiene. A stale entry is a lie about the tree, and
	// a shrunk one is debt that has been paid and must be written down.
	for _, e := range ledger {
		f, ok := found[e.check][e.path]
		if !ok {
			refusals = append(refusals, fmt.Sprintf(
				"LEDGER  %s:%d\n    `%s %s %d` matches nothing — the file is gone or clean.\n    Delete this line.",
				ledgerPath, e.line, e.check, e.path, e.count))
			continue
		}
		if f.count < e.count {
			refusals = append(refusals, fmt.Sprintf(
				"LEDGER  %s:%d\n    `%s %s %d` is stale — the file now has %d.\n    Lower the count to %d. The ledger only shrinks.",
				ledgerPath, e.line, e.check, e.path, e.count, f.count, f.count))
		}
	}

	total := 0
	for _, m := range found {
		total += len(m)
	}
	if len(refusals) == 0 {
		fmt.Printf("clean — %d file(s) carry shape knowledge, every one on the ledger\n", total)
		return nil
	}
	return fmt.Errorf("%s", strings.Join(refusals, "\n\n"))
}

// theRule — printed under every refusal, so the reader does not have to go
// find out what the gate is for before deciding what to do about it.
const theRule = `The estate has ONE benchmark: this repo's data-driven bench (bench/README.md).
Hand-written runners are fine. Hand-written MEASUREMENT of a schema shape is not.
If a refusal above is a deliberate, owner-ruled exception, add it to ` + ledgerPath + `
with its exact count and the reason.`

// ------------------------------------------------------------------------------------------

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > 8 {
		out = append(out[:8], fmt.Sprintf("... and %d more", len(out)-8))
	}
	return out
}

func sortedFindingPaths(m map[string]*finding) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatConsts(m map[int]string) string {
	var ks []int
	for k := range m {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	var parts []string
	for _, k := range ks {
		parts = append(parts, strconv.Itoa(k))
	}
	return strings.Join(parts, " ")
}
