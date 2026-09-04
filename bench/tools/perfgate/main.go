// perfgate: the mechanical half of the zero-cost-diagnostic law (issue #546).
//
//	make perf-gate            measure the reference benches and render a verdict
//	make perf-gate-control    the planted-cost negative control
//	make perf-gate-pin        re-measure the pins and the sitting they came off
//	make perf-gate-pinlock    the LOCK-model refusal, run on every pull request
//
// THE LAW IT ENFORCES. The owner's rule, 2026-09-05, issue #546: "i'm all for
// greater diagnostics, but if it costs speed on read or write, it's not worth
// it." A diagnostic surface is admitted at zero measured cost on the read path
// and the write path, or it is declined. Prose cannot hold that line, because
// the cost of a diagnostic is a number and prose has no numbers in it. This
// does.
//
// WHAT IT MEASURES. The C++ reference, over the bench corpus, on both wires:
//
//	bench_mixed  write        the packet wire's write path (bench/cpp)
//	bench_mixed  round_trip   the packet wire's read path, carried
//	bench_table  write        the table wire's write path (bench/tables/cpp)
//	bench_table  round_trip   the table wire's read path, carried
//
// THE READ ROW IS round_trip, AND THAT IS THE HONEST NAME FOR IT. Neither
// bench measures a read on its own: the decode's output IS the re-encode's
// input, which is how the read side gets its sink discipline for free in every
// language (BENCH-STANDARD.md §2.7), and the read rate both runners print is
// round-trip time minus write time, marked DERIVED and deliberately kept out
// of the CSV. A gate that pinned a derived number would be pinning a
// subtraction of two medians, whose noise is the sum of theirs. So the gate
// pins what was measured. A cost planted on the read path raises round_trip
// and leaves write flat, which is exactly what the negative control shows, and
// a reader of a red verdict can tell a read regression from a write regression
// by which of the two rows moved.
//
// THE BAND, AND WHY IT IS NOT ZERO. "Zero cost" is a claim about the code, and
// a measurement of it lands inside a distribution. The band is the width of
// that distribution on a quiet box, derived by repeated sittings and recorded
// in bench/PERF-PINS beside the numbers it guards. A regression larger than
// the band is a refusal. A regression smaller than the band is invisible to
// this instrument, which the pin file states rather than implies.
//
// THE BOX IS PART OF THE PIN. A rate in messages per second is a fact about
// one machine. bench/PERF-PINS names the box that produced it, and off that
// box the gate REFUSES to render a verdict rather than compare a number to a
// number from different silicon. What still runs anywhere is the negative
// control, whose verdict is a ratio inside one sitting: it needs no pin, and
// it is what certification runs on hardware nobody here owns.
package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pinsPath = "bench/PERF-PINS"

// gateRows: the four rows the gate pins, in report order.
var gateRows = [][2]string{
	{"bench_mixed", "write"},
	{"bench_mixed", "round_trip"},
	{"bench_table", "write"},
	{"bench_table", "round_trip"},
}

// ---------------------------------------------------------------------------
// measured rows

type row struct {
	bench    string
	path     string
	rate     float64 // median_msgs_per_sec
	spread   float64 // spread_pct, within the sitting
	corpusID string
	opt      string
	iters    int64
}

func (r row) key() string { return r.bench + "/" + r.path }

type sitting struct {
	rows        map[string]row
	arch        string
	cpu         string
	os          string
	host        string
	uptime      string
	commit      string
	compiler    string
	date        string
	prefixFlags string
}

// ---------------------------------------------------------------------------
// the box

func boxArch() string { return runtime.GOARCH }
func boxOS() string   { return runtime.GOOS }

func boxCPU() string {
	if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			if strings.HasPrefix(line, "model name") {
				if _, after, ok := strings.Cut(line, ":"); ok {
					return strings.TrimSpace(after)
				}
			}
		}
	}
	return "unknown"
}

func boxHost() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	if i := strings.Index(h, "."); i > 0 {
		h = h[:i]
	}
	return h
}

func boxUptime() string {
	out, err := exec.Command("uptime").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func repoCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func compilerVersion() string {
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	out, err := exec.Command(cxx, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// ---------------------------------------------------------------------------
// running the two reference benches
//
// NEITHER LEG'S FLAGS ARE SPELLED HERE, deliberately. The packet leg is
// bench/run.sh's own cpp leg and the table leg is bench/tables/cpp/leg, so the
// gate compiles the same translation units, with the same flags, as the
// published passes do. A gate built with flags of its own would be measuring
// a build nobody ships.

func run(name string, args []string, prefixFlags string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "BENCH_CXXFLAGS_PREFIX="+prefixFlags)
	return cmd.Run()
}

func runCapture(name string, args []string, prefixFlags string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "BENCH_CXXFLAGS_PREFIX="+prefixFlags)
	err := cmd.Run()
	return buf.Bytes(), err
}

// measure builds and runs both reference benches and returns the sitting.
// prefixFlags rides into both compiles. The negative control is its only user
// and `pin` refuses a sitting that carries any.
func measure(prefixFlags string) (*sitting, error) {
	s := &sitting{
		rows:        map[string]row{},
		arch:        boxArch(),
		cpu:         boxCPU(),
		os:          boxOS(),
		host:        boxHost(),
		uptime:      boxUptime(),
		commit:      repoCommit(),
		compiler:    compilerVersion(),
		date:        time.Now().Format("2006-01-02"),
		prefixFlags: prefixFlags,
	}

	// the packet wire. bench/run.sh refuses to overwrite a results file, which
	// is a rule about the published ledger under bench/results/ and not about
	// a scratch file under build/, so the gate removes its own and lets the
	// refusal stand everywhere else.
	pkt := "build/perfgate/packet.csv"
	if err := os.MkdirAll("build/perfgate", 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(pkt); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := run("bench/run.sh", []string{"--only", "cpp", "--bare", "--out", pkt}, prefixFlags); err != nil {
		return nil, fmt.Errorf("packet leg: %w", err)
	}
	pktCSV, err := os.ReadFile(pkt)
	if err != nil {
		return nil, err
	}
	if err := collect(s, pktCSV); err != nil {
		return nil, fmt.Errorf("packet leg: %w", err)
	}

	// the table wire
	if err := run("bench/tables/cpp/leg", []string{"build"}, prefixFlags); err != nil {
		return nil, fmt.Errorf("table leg build: %w", err)
	}
	tblCSV, err := runCapture("bench/tables/cpp/leg", []string{"run", "--csv"}, prefixFlags)
	if err != nil {
		return nil, fmt.Errorf("table leg: %w", err)
	}
	if err := collect(s, tblCSV); err != nil {
		return nil, fmt.Errorf("table leg: %w", err)
	}

	for _, want := range gateRows {
		if _, ok := s.rows[want[0]+"/"+want[1]]; !ok {
			return nil, fmt.Errorf("the sitting is missing the pinned row %s/%s. A gate that cannot see a row cannot guard it", want[0], want[1])
		}
	}
	return s, nil
}

// collect parses whichever CSV rows a leg emitted and keeps the gate's four.
// The two runners share one CSV schema (BENCH-STANDARD.md §5), so one parser
// reads both.
func collect(s *sitting, data []byte) error {
	rd := csv.NewReader(bytes.NewReader(data))
	rd.FieldsPerRecord = -1
	recs, err := rd.ReadAll()
	if err != nil {
		return err
	}
	var header []string
	for _, rec := range recs {
		if len(rec) == 0 {
			continue
		}
		if rec[0] == "lang" {
			header = rec
			continue
		}
		if header == nil {
			continue
		}
		if rec[0] != "cpp" {
			continue
		}
		m := map[string]string{}
		for i, h := range header {
			if i < len(rec) {
				m[h] = rec[i]
			}
		}
		r := row{bench: m["bench"], path: m["path"], corpusID: m["corpus_id"], opt: m["opt"]}
		r.rate, _ = strconv.ParseFloat(m["median_msgs_per_sec"], 64)
		r.spread, _ = strconv.ParseFloat(m["spread_pct"], 64)
		r.iters, _ = strconv.ParseInt(m["iters"], 10, 64)
		for _, want := range gateRows {
			if want[0] == r.bench && want[1] == r.path {
				s.rows[r.key()] = r
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// the pin file

type pins struct {
	directives map[string]string
	rates      map[string]float64
	corpus     map[string]string
	lines      []string // the file verbatim, for the re-pin rewrite
}

var pinLine = regexp.MustCompile(`^pin\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*$`)

func loadPins(path string) (*pins, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return parsePins(path, f)
}

// parsePins reads the file the working tree has, and, for the pin lock, the
// one the base branch had.
func parsePins(path string, r io.Reader) (*pins, error) {
	p := &pins{directives: map[string]string{}, rates: map[string]float64{}, corpus: map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		p.lines = append(p.lines, line)
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if m := pinLine.FindStringSubmatch(t); m != nil {
			rate, err := strconv.ParseFloat(m[3], 64)
			if err != nil {
				return nil, fmt.Errorf("%s: unreadable rate on %q", path, t)
			}
			p.rates[m[1]+"/"+m[2]] = rate
			p.corpus[m[1]+"/"+m[2]] = m[4]
			continue
		}
		fields := strings.Fields(t)
		if len(fields) < 2 {
			return nil, fmt.Errorf("%s: unreadable line %q", path, t)
		}
		p.directives[fields[0]] = strings.TrimSpace(strings.TrimPrefix(t, fields[0]))
	}
	return p, sc.Err()
}

func (p *pins) num(key string) (float64, error) {
	v, ok := p.directives[key]
	if !ok {
		return 0, fmt.Errorf("%s: missing directive %s", pinsPath, key)
	}
	return strconv.ParseFloat(v, 64)
}

// requiredDirectives: the sitting a re-pin has to state. The pin file is the
// gate's whole authority, so a pin whose sitting is unstated is a number with
// no provenance, and the gate refuses to read one.
var requiredDirectives = []string{
	"box.arch", "box.cpu", "box.os",
	"band.pct", "spread.max.pct",
	"sitting.date", "sitting.host", "sitting.uptime", "sitting.commit",
	"sitting.compiler", "sitting.opt", "sitting.repeats", "sitting.worst.pct",
}

func (p *pins) validate() error {
	for _, k := range requiredDirectives {
		if strings.TrimSpace(p.directives[k]) == "" {
			return fmt.Errorf("%s: the sitting does not state %s", pinsPath, k)
		}
	}
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		rate, ok := p.rates[k]
		if !ok {
			return fmt.Errorf("%s: no pin for %s", pinsPath, k)
		}
		// A zero pin is the skeleton, not a measurement. Every row divides by
		// its pin, so an unmeasured file has to be a refusal rather than a
		// division by nothing.
		if rate <= 0 {
			return fmt.Errorf("%s: the pin for %s is %g, which is the unmeasured skeleton. Cut the pins with `make perf-gate-pin` on a quiet box", pinsPath, k, rate)
		}
	}
	for _, k := range []string{"band.pct", "spread.max.pct"} {
		v, err := p.num(k)
		if err != nil {
			return err
		}
		if v <= 0 {
			return fmt.Errorf("%s: %s is %g. A width of zero is not a width, and a gate with one refuses everything or nothing", pinsPath, k, v)
		}
	}
	if len(p.rates) != len(gateRows) {
		return fmt.Errorf("%s: %d pins for %d gate rows. The file and the gate disagree about what is guarded", pinsPath, len(p.rates), len(gateRows))
	}
	return nil
}

func (p *pins) onThisBox() bool {
	return p.directives["box.arch"] == boxArch() &&
		p.directives["box.os"] == boxOS() &&
		p.directives["box.cpu"] == boxCPU()
}

// ---------------------------------------------------------------------------
// the verdict

type verdict struct {
	key      string
	pinned   float64
	measured float64
	deltaPct float64 // negative is slower than the pin
	spread   float64
	red      bool
	note     string
}

func compare(s *sitting, p *pins, band, spreadMax float64) ([]verdict, bool) {
	var out []verdict
	red := false
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		r := s.rows[k]
		pinned := p.rates[k]
		v := verdict{key: k, pinned: pinned, measured: r.rate, spread: r.spread}
		v.deltaPct = (r.rate - pinned) / pinned * 100
		switch {
		case p.corpus[k] != "" && r.corpusID != "" && p.corpus[k] != r.corpusID:
			v.red, v.note = true, "CORPUS MOVED (pinned "+p.corpus[k]+", measured "+r.corpusID+"): different work, no comparison"
		case r.spread > spreadMax:
			v.red, v.note = true, fmt.Sprintf("SITTING TOO NOISY (spread %.1f%% over the %.1f%% cap): this box cannot render a verdict now", r.spread, spreadMax)
		case v.deltaPct < -band:
			v.red, v.note = true, fmt.Sprintf("SLOWER THAN THE PIN BY %.2f%%, past the %.2f%% band", -v.deltaPct, band)
		case v.deltaPct > band:
			v.note = fmt.Sprintf("faster than the pin by %.2f%%: the pin is stale, not the code", v.deltaPct)
		default:
			v.note = "within band"
		}
		if v.red {
			red = true
		}
		out = append(out, v)
	}
	return out, red
}

func printVerdicts(vs []verdict, band float64) {
	fmt.Printf("%-24s %14s %14s %9s %8s  %s\n", "row", "pinned M/s", "measured M/s", "delta", "spread", "verdict")
	for _, v := range vs {
		mark := "ok  "
		if v.red {
			mark = "RED "
		}
		fmt.Printf("%-24s %14.3f %14.3f %8.2f%% %7.1f%%  %s%s\n",
			v.key, v.pinned/1e6, v.measured/1e6, v.deltaPct, v.spread, mark, v.note)
	}
	fmt.Printf("band: %.2f%% (bench/PERF-PINS states how it was derived)\n", band)
}

// ---------------------------------------------------------------------------
// commands

func cmdMeasure(args []string) int {
	repeats := 1
	parseFlags(args, map[string]*string{}, map[string]*int{"repeats": &repeats})
	for i := 0; i < repeats; i++ {
		s, err := measure(os.Getenv("BENCH_CXXFLAGS_PREFIX"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "perfgate measure:", err)
			return 1
		}
		fmt.Printf("# sitting %d/%d  %s  %s  %s\n", i+1, repeats, s.date, s.host, s.uptime)
		for _, want := range gateRows {
			r := s.rows[want[0]+"/"+want[1]]
			fmt.Printf("sample %-12s %-11s %14.0f %6.1f %s\n", r.bench, r.path, r.rate, r.spread, r.corpusID)
		}
	}
	return 0
}

// cmdBand is how the band in bench/PERF-PINS was derived, kept as a command so
// the derivation is repeatable rather than a remembered number: N full
// sittings, and for each row the worst deviation of any sitting from the
// median of the sittings.
func cmdBand(args []string) int {
	repeats := 7
	parseFlags(args, map[string]*string{}, map[string]*int{"repeats": &repeats})
	samples := map[string][]float64{}
	for i := 0; i < repeats; i++ {
		s, err := measure("")
		if err != nil {
			fmt.Fprintln(os.Stderr, "perfgate band:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "== sitting %d/%d: %s\n", i+1, repeats, s.uptime)
		for _, want := range gateRows {
			k := want[0] + "/" + want[1]
			samples[k] = append(samples[k], s.rows[k].rate)
		}
	}
	worst := 0.0
	fmt.Printf("%-24s %9s %9s %9s %9s\n", "row", "median", "min", "max", "worst dev")
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		v := append([]float64(nil), samples[k]...)
		sort.Float64s(v)
		med := v[len(v)/2]
		if len(v)%2 == 0 {
			med = (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
		dev := 0.0
		for _, x := range v {
			d := (x - med) / med * 100
			if d < 0 {
				d = -d
			}
			if d > dev {
				dev = d
			}
		}
		if dev > worst {
			worst = dev
		}
		fmt.Printf("%-24s %9.3f %9.3f %9.3f %8.2f%%\n", k, med/1e6, v[0]/1e6, v[len(v)-1]/1e6, dev)
	}
	fmt.Printf("\nworst deviation across %d sittings: %.2f%%\n", repeats, worst)
	fmt.Printf("band to write into %s: %.1f (worst deviation, doubled, rounded up to a tenth)\n", pinsPath, roundBand(worst*2))
	return 0
}

func roundBand(x float64) float64 {
	// up to the next tenth of a percent
	return float64(int((x+0.0999)*10)) / 10
}

func cmdCheck(args []string) int {
	advisory := false
	flags := map[string]*bool{"advisory": &advisory}
	parseBoolFlags(args, flags)

	p, err := loadPins(pinsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate check:", err)
		return 1
	}
	if err := p.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "perfgate check:", err)
		return 1
	}
	band, err := p.num("band.pct")
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate check:", err)
		return 1
	}
	spreadMax, err := p.num("spread.max.pct")
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate check:", err)
		return 1
	}

	if !p.onThisBox() {
		fmt.Printf("NOT THE PINNED BOX.\n")
		fmt.Printf("  pinned   %s / %s / %s\n", p.directives["box.arch"], p.directives["box.os"], p.directives["box.cpu"])
		fmt.Printf("  this box %s / %s / %s\n", boxArch(), boxOS(), boxCPU())
		fmt.Printf("A rate in messages per second is a fact about one machine, and the gate\n")
		fmt.Printf("will not divide one box's number by another's. Re-pin from this box with a\n")
		fmt.Printf("pull request that states its sitting, or run the gate where the pins were cut.\n")
		if advisory {
			fmt.Printf("advisory: the structural half passed. %s parses, and every gate row has a pin.\n", pinsPath)
			return 0
		}
		return 2
	}

	s, err := measure("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate check:", err)
		return 1
	}
	vs, red := compare(s, p, band, spreadMax)
	fmt.Printf("perf gate: %s, %s\n%s\n\n", s.host, s.date, s.uptime)
	printVerdicts(vs, band)
	if red {
		fmt.Printf("\nPERF GATE RED. A read or a write on the reference got slower than the pin by\n")
		fmt.Printf("more than the band. If the change added a diagnostic, the law (issue #546)\n")
		fmt.Printf("declines it: a diagnostic is admitted at zero measured cost on the read and\n")
		fmt.Printf("write paths, or not at all. If the change is a deliberate cost the owner\n")
		fmt.Printf("accepted, the pins move in their own pull request, which states its sitting.\n")
		return 1
	}
	fmt.Printf("\nperf gate green.\n")
	return 0
}

func cmdPin(args []string) int {
	repeats := 1
	parseFlags(args, map[string]*string{}, map[string]*int{"repeats": &repeats})

	if strings.TrimSpace(os.Getenv("BENCH_CXXFLAGS_PREFIX")) != "" {
		fmt.Fprintf(os.Stderr, "perfgate pin: BENCH_CXXFLAGS_PREFIX is set (%q).\n", os.Getenv("BENCH_CXXFLAGS_PREFIX"))
		fmt.Fprintf(os.Stderr, "That hook exists for the negative control and for nothing else. A pin cut\n")
		fmt.Fprintf(os.Stderr, "under flags the shipped build does not carry is a pin for a build nobody runs.\n")
		return 1
	}

	// the pins are the median of `repeats` sittings, and the worst deviation
	// across them is written into the file beside them, so the band's derivation
	// travels with the numbers rather than living in someone's memory.
	samples := map[string][]float64{}
	var last *sitting
	for i := 0; i < repeats; i++ {
		s, err := measure("")
		if err != nil {
			fmt.Fprintln(os.Stderr, "perfgate pin:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "== sitting %d/%d: %s\n", i+1, repeats, s.uptime)
		for _, want := range gateRows {
			k := want[0] + "/" + want[1]
			samples[k] = append(samples[k], s.rows[k].rate)
		}
		last = s
	}
	worst := 0.0
	medians := map[string]float64{}
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		v := append([]float64(nil), samples[k]...)
		sort.Float64s(v)
		med := v[len(v)/2]
		if len(v)%2 == 0 {
			med = (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
		medians[k] = med
		for _, x := range v {
			d := (x - med) / med * 100
			if d < 0 {
				d = -d
			}
			if d > worst {
				worst = d
			}
		}
	}

	p, err := loadPins(pinsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate pin:", err)
		return 1
	}
	set := map[string]string{
		"box.arch":          last.arch,
		"box.cpu":           last.cpu,
		"box.os":            last.os,
		"sitting.date":      last.date,
		"sitting.host":      last.host,
		"sitting.uptime":    last.uptime,
		"sitting.commit":    last.commit,
		"sitting.compiler":  last.compiler,
		"sitting.opt":       optOf(last),
		"sitting.repeats":   strconv.Itoa(repeats),
		"sitting.worst.pct": fmt.Sprintf("%.2f", worst),
	}
	var out []string
	seen := map[string]bool{}
	wrotePins := false
	for _, line := range p.lines {
		t := strings.TrimSpace(line)
		if fields := strings.Fields(t); len(fields) > 0 && !strings.HasPrefix(t, "#") {
			if v, ok := set[fields[0]]; ok {
				out = append(out, fields[0]+strings.Repeat(" ", pad(fields[0]))+v)
				seen[fields[0]] = true
				continue
			}
			if strings.HasPrefix(t, "pin ") {
				if !wrotePins {
					for _, want := range gateRows {
						k := want[0] + "/" + want[1]
						out = append(out, fmt.Sprintf("pin %-12s %-11s %14.0f %s", want[0], want[1], medians[k], last.rows[k].corpusID))
					}
					wrotePins = true
				}
				continue
			}
		}
		out = append(out, line)
	}
	if err := os.WriteFile(pinsPath, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "perfgate pin:", err)
		return 1
	}
	fmt.Printf("re-pinned %s from %d sitting(s) on %s (worst deviation %.2f%%)\n", pinsPath, repeats, last.host, worst)
	fmt.Printf("REVIEW THE band.pct LINE BY HAND: this command records the sitting and the\n")
	fmt.Printf("numbers, and leaves the band alone. Widening a band is a decision, never a\n")
	fmt.Printf("side effect of re-measuring.\n")
	return 0
}

func optOf(s *sitting) string {
	for _, r := range s.rows {
		if r.opt != "" {
			return r.opt
		}
	}
	return "unknown"
}

func pad(key string) int {
	return max(20-len(key), 1)
}

// ---------------------------------------------------------------------------
// the negative control
//
// A gate nobody has seen go red is a gate nobody has tested. This plants the
// exact shape the law refuses (a counter behind a runtime flag, one added
// branch per field, on the READ path only) into a scratch copy of the
// emitter's output, builds the same benches against it, and requires the gate
// to call it. The plant is OFF at run time: the control measures the price of
// the branch alone, which is the honest claim the law makes. Nothing under
// generated/ or internal/codegen/ is touched, so the C/C++ lock (bench/LOCK)
// is not approached.

const plantBlock = `
// ---------------------------------------------------------------------------
// PLANTED BY bench/tools/perfgate: the negative control for issue #546.
// This file is a scratch copy under build/. The tracked tree has no such code.
#ifndef SCHEMA_PERFGATE_PLANT_DEFINED
#define SCHEMA_PERFGATE_PLANT_DEFINED
#include <stdlib.h>
#include <stdint.h>
// The shape of the diagnostic the law refuses: one counter, one runtime flag,
// one added branch per field read. The flag comes from the environment at
// static-initialization time, so no compiler can fold the branch away, and the
// gate never sets it, so what gets measured is the branch and not the counting.
static bool schema_perfgate_diag_enabled = ( getenv( "SCHEMA_PERFGATE_DIAG" ) != NULL );
static uint64_t schema_perfgate_diag_count = 0;
#define SCHEMA_PERFGATE_PLANT() if ( schema_perfgate_diag_enabled ) { schema_perfgate_diag_count++; }
#endif
// ---------------------------------------------------------------------------
`

var (
	readFnStart  = regexp.MustCompile(`^SCHEMA_READ_INLINE bool Read`)
	writeFnStart = regexp.MustCompile(`^SCHEMA_WRITE_INLINE bool Write`)
	readMacro    = regexp.MustCompile(`^read_[a-z0-9_]+\(`)
	loadFnStart  = regexp.MustCompile(`bool [A-Za-z0-9_]*Load(Body)?\(`)
	saveFnStart  = regexp.MustCompile(`bool [A-Za-z0-9_]*(Save|Measure)[A-Za-z0-9_]*\(`)
	caseLine     = regexp.MustCompile(`^case 0x[0-9a-fA-F]+ull:`)
)

// plantPacket: the packet wire's generated header. read_* is a macro and it
// appears on the read path and nowhere else, so the site is the call.
func plantPacket(src string) (string, int) {
	lines := strings.Split(src, "\n")
	inRead := false
	planted := 0
	var out []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case readFnStart.MatchString(line):
			inRead = true
		case writeFnStart.MatchString(line):
			inRead = false
		}
		if inRead && readMacro.MatchString(t) {
			out = append(out, indentOf(line)+"SCHEMA_PERFGATE_PLANT()")
			planted++
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), planted
}

// plantTable: the table wire's generated header. The read path dispatches on
// field id, so the site is the case label: one plant per field, once.
func plantTable(src string) (string, int) {
	lines := strings.Split(src, "\n")
	inLoad := false
	planted := 0
	var out []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case loadFnStart.MatchString(line):
			inLoad = true
		case saveFnStart.MatchString(line):
			inLoad = false
		}
		out = append(out, line)
		if inLoad && caseLine.MatchString(t) {
			out = append(out, indentOf(line)+"SCHEMA_PERFGATE_PLANT()")
			planted++
		}
	}
	return strings.Join(out, "\n"), planted
}

func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func injectBlock(src string) string {
	i := strings.Index(src, "#pragma once")
	if i < 0 {
		return plantBlock + src
	}
	j := i + len("#pragma once")
	return src[:j] + "\n" + plantBlock + src[j:]
}

// writePlanted copies a generated tree into build/perfgate/planted/<name> and
// plants the cost in the one header that carries the read path.
func writePlanted(srcDir, dstDir, target string, plant func(string) (string, int)) (int, error) {
	if err := os.RemoveAll(dstDir); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, err
	}
	planted := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return 0, err
		}
		text := string(b)
		if e.Name() == target {
			text, planted = plant(text)
			text = injectBlock(text)
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), []byte(text), 0o644); err != nil {
			return 0, err
		}
	}
	if planted == 0 {
		return 0, fmt.Errorf("planted nothing in %s. The control would prove the gate green for the wrong reason", target)
	}
	return planted, nil
}

// cmdPlant writes the planted scratch copies and stops, so the transform can
// be read and compiled without spending a quiet window on a timed run.
func cmdPlant(args []string) int {
	parseFlags(args, map[string]*string{}, map[string]*int{})
	pktN, err := writePlanted("generated/bench/cpp", "build/perfgate/planted/cpp", "BenchWire.h", plantPacket)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate plant:", err)
		return 1
	}
	tblN, err := writePlanted("generated/bench/tables/cpp", "build/perfgate/planted/tables-cpp", "BenchTableTable.h", plantTable)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate plant:", err)
		return 1
	}
	fmt.Printf("build/perfgate/planted/cpp/BenchWire.h            %d read sites planted\n", pktN)
	fmt.Printf("build/perfgate/planted/tables-cpp/BenchTableTable.h %d read sites planted\n", tblN)
	return 0
}

func cmdControl(args []string) int {
	parseFlags(args, map[string]*string{}, map[string]*int{})

	p, err := loadPins(pinsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}
	if err := p.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}
	band, err := p.num("band.pct")
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}

	pktN, err := writePlanted("generated/bench/cpp", "build/perfgate/planted/cpp", "BenchWire.h", plantPacket)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}
	tblN, err := writePlanted("generated/bench/tables/cpp", "build/perfgate/planted/tables-cpp", "BenchTableTable.h", plantTable)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}
	fmt.Printf("planted %d read sites in BenchWire.h and %d in BenchTableTable.h (scratch copies under build/perfgate/planted)\n\n", pktN, tblN)

	fmt.Fprintln(os.Stderr, "== control: the clean sitting ==")
	clean, err := measure("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "== control: the planted sitting ==")
	planted, err := measure("-Ibuild/perfgate/planted/cpp -Ibuild/perfgate/planted/tables-cpp")
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate control:", err)
		return 1
	}

	// THE CONTROL COMPARES TWO SITTINGS ON ONE BOX, never the pins: the clean
	// half is measured minutes before the planted half on the same machine, so
	// the verdict holds on hardware that has no pins at all.
	delta := func(s *sitting, k string) float64 { return s.rows[k].rate }
	fmt.Printf("%-24s %14s %14s %9s  %s\n", "row", "clean M/s", "planted M/s", "delta", "verdict")
	pct := map[string]float64{}
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		c, pl := delta(clean, k), delta(planted, k)
		pct[k] = (pl - c) / c * 100
		mark := "ok  "
		if pct[k] < -band {
			mark = "RED "
		}
		fmt.Printf("%-24s %14.3f %14.3f %8.2f%%  %s\n", k, c/1e6, pl/1e6, pct[k], mark)
	}
	fmt.Printf("band: %.2f%%\n\n", band)

	// THE VERDICT IS THE SEPARATION, not either row alone, and that is the
	// deliberate choice here. A raw round_trip drop is the obvious test and it
	// is the fragile one: if the box happens to run faster during the planted
	// half, a real cost can hide inside the drift, and this control has to hold
	// on a shared certification runner nobody here owns. The difference between
	// the two rows cancels whatever the box did to both, and it is also the
	// exact claim the law needs: the cost landed on the READ path. Nothing was
	// planted on the write path, so write is the control's own control.
	fmt.Printf("%-14s %12s %12s %14s  %s\n", "wire", "write", "round_trip", "separation", "verdict")
	ok := true
	for _, b := range []string{"bench_mixed", "bench_table"} {
		w, rt := pct[b+"/write"], pct[b+"/round_trip"]
		sep := w - rt
		mark := "RED  (the plant is visible)"
		if sep <= band {
			mark = "GREEN (the plant is invisible)"
			ok = false
		}
		fmt.Printf("%-14s %11.2f%% %11.2f%% %13.2f%%  %s\n", b, w, rt, sep, mark)
	}
	fmt.Println()

	if !ok {
		fmt.Printf("CONTROL FAILED. One added branch per field on the read path did not separate\n")
		fmt.Printf("round_trip from write by more than the %.2f%% band on both wires. Either the\n", band)
		fmt.Printf("plant did not reach the compiled code, or the band is wide enough to hide the\n")
		fmt.Printf("cheapest diagnostic anybody would actually propose, which is the same thing as\n")
		fmt.Printf("having no gate. A gate nobody has watched go red is a gate nobody has tested.\n")
		return 1
	}
	fmt.Printf("CONTROL PASSED. On both wires the planted read cost separates round_trip from\n")
	fmt.Printf("write by more than the band, so the gate sees it AND says which path it landed\n")
	fmt.Printf("on. That separation is what a red perf-gate verdict is reporting.\n")
	return 0
}

// ---------------------------------------------------------------------------
// the pin lock, on the model bench/LOCK's standing gate uses
//
// When the C/C++ freeze lifted on 2026-09-05 what replaced it was not nothing.
// It was a standing gate, in the owner's terms: a pull request that moves the
// packet emitters "re-pins the reproduction rows ... in the same PR and states
// the sitting that produced them", with a laptop sitting admitted so long as
// its provenance is stated. This is that rule, applied to these pins.
//
// So the refusal here is NOT "the pins may only move alone". A change that
// legitimately moves the reference has to move the pins in the same diff, and
// forbidding that would forbid the workflow the owner just blessed. What is
// refused is a re-pin with no sitting behind it: numbers edited by hand while
// the sitting.* header still describes the measurement they replaced. A diff
// that moves the pins restates the sitting, and the commit it names is one
// this branch actually contains.
//
// It runs on every pull request, costs no measurement, and prints the size of
// each pin's move so a reviewer sees what was re-pinned without diffing two
// columns of digits.

func cmdPinlock(args []string) int {
	base := "origin/main"
	strs := map[string]*string{"base": &base}
	parseFlags(args, strs, map[string]*int{})

	p, err := loadPins(pinsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate pinlock:", err)
		return 1
	}
	if err := p.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "perfgate pinlock:", err)
		return 1
	}
	fmt.Printf("%s parses, states its sitting, and pins every gate row.\n", pinsPath)

	// The sitting names a commit. A sitting cut on a commit this branch does
	// not contain measured a tree nobody here is proposing to merge.
	sc := p.directives["sitting.commit"]
	if err := exec.Command("git", "merge-base", "--is-ancestor", sc, "HEAD").Run(); err != nil {
		fmt.Printf("\nPERF PIN REFUSAL. sitting.commit is %s, which this branch does not contain.\n", sc)
		fmt.Printf("A sitting cut on a commit outside this history measured a tree nobody is\n")
		fmt.Printf("proposing to merge. Re-cut the pins here with `make perf-gate-pin`.\n")
		return 1
	}

	out, err := exec.Command("git", "diff", "--name-only", base+"...HEAD").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "perfgate pinlock: cannot diff against %s: %v\n", base, err)
		return 1
	}
	touchesPins := false
	for f := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if f == pinsPath {
			touchesPins = true
		}
	}
	if !touchesPins {
		fmt.Printf("sitting.commit %s is in this history, and the diff does not move the pins.\n", sc)
		return 0
	}

	// The pins moved. Read the base's copy and require the sitting to have
	// moved with them.
	old, err := exec.Command("git", "show", base+":"+pinsPath).Output()
	if err != nil {
		fmt.Printf("%s is new on this branch, so there is no earlier sitting to restate.\n", pinsPath)
		fmt.Printf("  sitting %s on %s at %s\n", p.directives["sitting.date"], p.directives["sitting.host"], sc)
		fmt.Printf("  uptime  %s\n", p.directives["sitting.uptime"])
		return 0
	}
	prev, err := parsePins(pinsPath, bytes.NewReader(old))
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfgate pinlock: cannot read the base's pin file:", err)
		return 1
	}

	var stale []string
	for _, k := range []string{"sitting.date", "sitting.uptime", "sitting.commit"} {
		if prev.directives[k] == p.directives[k] {
			stale = append(stale, k)
		}
	}
	moved := false
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		if prev.rates[k] != p.rates[k] {
			moved = true
		}
	}
	if !moved && len(stale) == 3 {
		fmt.Printf("the diff touches %s but moves no pin and no sitting, so there is nothing\n", pinsPath)
		fmt.Printf("to state: prose, or a band the author is answering for by hand.\n")
		return 0
	}

	fmt.Printf("\nthe pins moved. what changed, row by row:\n")
	for _, want := range gateRows {
		k := want[0] + "/" + want[1]
		o, n := prev.rates[k], p.rates[k]
		if o <= 0 {
			fmt.Printf("  %-24s %14.3f M/s (no earlier pin)\n", k, n/1e6)
			continue
		}
		fmt.Printf("  %-24s %14.3f -> %10.3f M/s  %+6.2f%%\n", k, o/1e6, n/1e6, (n-o)/o*100)
	}

	if len(stale) > 0 {
		fmt.Printf("\nPERF PIN REFUSAL. The pins moved and the sitting did not: %s\n", strings.Join(stale, ", "))
		fmt.Printf("are unchanged from %s.\n\n", base)
		fmt.Printf("The pins are what the gate compares a read and a write against, so numbers\n")
		fmt.Printf("edited under a sitting header that describes the measurement they replaced\n")
		fmt.Printf("are numbers with no provenance, and a regression can be made green by typing\n")
		fmt.Printf("over the thing that would have caught it. A re-pin restates its sitting: the\n")
		fmt.Printf("box, the date, the commit, and the load the machine was carrying. Cut it with\n")
		fmt.Printf("`make perf-gate-pin` on a quiet box, which fills those lines in for you.\n")
		return 1
	}
	fmt.Printf("\nthe sitting is restated, which is what a re-pin owes:\n")
	fmt.Printf("  %s on %s at %s\n", p.directives["sitting.date"], p.directives["sitting.host"], sc)
	fmt.Printf("  %s\n", p.directives["sitting.uptime"])
	fmt.Printf("  %s, %s, %s sittings, worst deviation %s%%\n",
		p.directives["box.cpu"], p.directives["sitting.compiler"],
		p.directives["sitting.repeats"], p.directives["sitting.worst.pct"])
	return 0
}

// ---------------------------------------------------------------------------

func parseFlags(args []string, strs map[string]*string, ints map[string]*int) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		if p, ok := strs[a]; ok && i+1 < len(args) {
			*p = args[i+1]
			i++
			continue
		}
		if p, ok := ints[a]; ok && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err == nil {
				*p = n
			}
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return rest
}

func parseBoolFlags(args []string, flags map[string]*bool) {
	for _, a := range args {
		if p, ok := flags[strings.TrimLeft(a, "-")]; ok {
			*p = true
		}
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `perfgate: the zero-cost-diagnostic gate (issue #546)

  perfgate check [-advisory]     measure and compare against bench/PERF-PINS
  perfgate control               plant a read cost and require the gate to go red
  perfgate plant                 write the planted scratch copies and stop
  perfgate measure [-repeats N]  measure only, print the rows
  perfgate band [-repeats N]     derive the noise band from repeated sittings
  perfgate pin [-repeats N]      rewrite the pins and the sitting they came from
  perfgate pinlock [-base REF]   refuse a pull request that moves the pins with code`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "check":
		os.Exit(cmdCheck(args))
	case "plant":
		os.Exit(cmdPlant(args))
	case "control":
		os.Exit(cmdControl(args))
	case "measure":
		os.Exit(cmdMeasure(args))
	case "band":
		os.Exit(cmdBand(args))
	case "pin":
		os.Exit(cmdPin(args))
	case "pinlock":
		os.Exit(cmdPinlock(args))
	default:
		usage()
		os.Exit(2)
	}
}
