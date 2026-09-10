package main

// THE NINE-LANGUAGE TABLE. `-mode table` is the reporting mode on top of the
// fast diagnostic: it asks THE TREE which table runners it carries, measures
// exactly those with the ordinary `-mode fast` machinery, and renders ONE
// markdown table — per language, the fixed form's save and round trip in
// nanoseconds per record and its bytes, against that language's own packet
// wire and against C++'s fixed form.
//
// It adds no measurement of its own and relaxes no gate. Every number below
// comes from a row `parseRowsForIterations` already accepted, at the one
// uniform iteration count per wire that BENCH-STANDARD §2.1 requires, so the
// table cannot mix counts, corpora or checks axes. What this mode adds is the
// row order, the two ratios and the header that says which sitting produced
// them.

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// nineLanguages is the published row order — the same nine
// `bench/tools/pass-driver.sh` names, C++ first because it is the reference
// implementation and the denominator of the last column.
var nineLanguages = []string{"cpp", "c", "go", "rust", "cs", "js", "java", "dart", "elixir"}

// tableOnlyNames covers the languages the paired driver does not pair with a
// packet leg. A leg that lands and adds itself to `names` wins over this.
var tableOnlyNames = map[string]string{"rust": "Rust", "js": "JavaScript", "java": "Java", "dart": "Dart", "elixir": "Elixir"}

func displayName(lang string) string {
	if n := names[lang]; n != "" {
		return n
	}
	if n := tableOnlyNames[lang]; n != "" {
		return n
	}
	return lang
}

// tableRunnerDir is where every table leg lives (bench/tables/README.md).
// Asking the filesystem rather than a list here is what lets one command run
// on a leg branch, on the integration branch and on main without editing:
// the tree it runs on is the authority on which runners it has.
func tableRunnerDir(lang string) string { return filepath.Join("bench", "tables", lang) }

func tableRunnerPresent(lang string) bool {
	info, err := os.Stat(tableRunnerDir(lang))
	return err == nil && info.IsDir()
}

// discoverTableLanguages splits the nine into the ones this tree can measure
// and the ones it cannot, both in publication order.
func discoverTableLanguages() (present, absent []string) {
	for _, lang := range nineLanguages {
		if tableRunnerPresent(lang) {
			present = append(present, lang)
		} else {
			absent = append(absent, lang)
		}
	}
	return present, absent
}

// skipped is a language the table does not carry, and WHY. Naming the reason
// is the point: "no runner on this tree" and "present, but its rows do not
// answer to this corpus" are different facts about the merge, and a reader
// deciding whether a leg is done needs to tell them apart.
type skipped struct {
	Language string `json:"language"`
	Reason   string `json:"reason"`
}

// probeRotation is ONE whole rotation of the 64 records — the smallest run
// that exercises a leg's real corpus load and emits real rows.
const probeRotation = 64

// answers asks whether LANG's legs actually belong to this sitting. It runs
// each of its wires over one rotation and hands the output to the ordinary
// parser, which is the same code that will accept or refuse the measured
// rows: identity, family, CORPUS ID, checks axis and iteration count.
//
// The corpus id is what this catches. A leg whose fixed-form port has not
// landed loads a different set of goldens and reports a different id, and the
// driver already refuses to divide such a row against a paired packet row —
// correctly, because it is a different corpus. Learning that in a second here
// beats learning it three minutes into a pass, and it means the row appears
// on its own the day that leg lands, with nothing here to edit.
func answers(lang string, info buildInfo) error {
	wires := []string{"packet", "table"}
	if !contains(languages, lang) {
		wires = []string{"table"}
	}
	for _, wire := range wires {
		data, err := capture(fastCommand(wire, lang, 0, probeRotation))
		if err != nil {
			return fmt.Errorf("%s/%s did not run: %w", lang, wire, err)
		}
		if _, err := parseRowsForIterations(data, lang, wire, info.CorpusIDs[wire], probeRotation, 0); err != nil {
			// The overwhelmingly common refusal, and the one worth reading in
			// a pasted table, is a leg answering to a different corpus. Say
			// that in a line instead of the parser's whole CSV row.
			if id, bench, ok := firstRowIdentity(data); ok && id != info.CorpusIDs[wire] {
				return fmt.Errorf("%s/%s measures `%s` against corpus `%s`, not this sitting's `%s`", lang, wire, bench, id, info.CorpusIDs[wire])
			}
			return err
		}
	}
	return nil
}

// firstRowIdentity reads the bench name and corpus id off the first data row
// of a runner's CSV, without judging it.
func firstRowIdentity(data []byte) (id, bench string, ok bool) {
	for line := range strings.SplitSeq(string(data), "\n") {
		cols := strings.Split(strings.TrimSpace(line), ",")
		if len(cols) != 17 || cols[0] == "lang" || strings.HasPrefix(cols[0], "#") {
			continue
		}
		return cols[11], cols[1], true
	}
	return "", "", false
}

// tableRow is one language's line of the published table.
type tableRow struct {
	Language string  `json:"language"`
	Form     string  `json:"table_form"`
	SaveNs   float64 `json:"save_ns_per_record"`
	TripNs   float64 `json:"round_trip_ns_per_record"`
	Bytes    float64 `json:"bytes_per_record"`
	// PacketTripNs is 0 for a language with no packet leg, which is what the
	// standard means by table-only: there is no own-packet denominator, and
	// the column says so rather than borrowing another language's.
	PacketTripNs float64 `json:"packet_round_trip_ns_per_record,omitempty"`
}

// nineTable is the rendered table's whole input, kept separate from the
// rendering so a test can build one without a machine.
type nineTable struct {
	Host      string            `json:"host"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	CPU       string            `json:"cpu"`
	Revision  string            `json:"revision"`
	Dirty     bool              `json:"dirty"`
	Rounds    int               `json:"rounds"`
	Counts    map[string]int64  `json:"final_iterations_per_wire"`
	CorpusIDs map[string]string `json:"corpus_ids"`
	Load      string            `json:"load1_range"`
	Noise     []string          `json:"observed_noise"`
	Note      string            `json:"noise_note"`
	Lane      string            `json:"lane,omitempty"`
	Rows      []tableRow        `json:"rows"`
	Absent    []skipped         `json:"skipped"`
	Passes    int               `json:"passes"`
}

// The one command's own qualification. It is the fast diagnostic's, restated
// where the table is read, because a table is easier to paste than a README.
const nineQualification = "Not certified. `-mode fast` has reduced iteration counts, no bracketing drift controls and no quiet-window seal. The certified sitting is `-mode run` (seven rounds, quiet window) — see bench/README.md."

// load1 pulls the one-minute figure out of a platform load string: macOS
// `sysctl vm.loadavg` prints `{ 1.20 1.34 1.41 }`, Linux `/proc/loadavg`
// prints `1.20 1.34 1.41 2/512 9182`. The first field that parses as a
// number is load1 on both.
func load1(sample string) (float64, bool) {
	for field := range strings.FieldsSeq(strings.NewReplacer("{", " ", "}", " ", ",", " ").Replace(sample)) {
		if v, err := strconv.ParseFloat(field, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

// loadRange reads the diagnostic's own process journal and reports the
// one-minute load actually observed across the sitting. A loaded host has to
// say so in the header rather than in a footnote nobody carries with the
// table.
func loadRange(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "process-samples.jsonl"))
	if err != nil {
		return "unavailable"
	}
	low, high, seen := math.Inf(1), math.Inf(-1), false
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var sample struct {
			Load string `json:"load"`
		}
		if json.Unmarshal([]byte(line), &sample) != nil {
			continue
		}
		v, ok := load1(sample.Load)
		if !ok {
			continue
		}
		low, high, seen = math.Min(low, v), math.Max(high, v), true
	}
	if !seen {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f–%.2f", low, high)
}

// medianOf is the same median the fast summary uses: the middle of the
// adequate rounds, averaging the two middles on an even count.
func medianOf(values []float64) float64 {
	sort.Float64s(values)
	middle := len(values) / 2
	v := values[middle]
	if len(values)%2 == 0 {
		v = (v + values[middle-1]) / 2
	}
	return v
}

type tableMeasurement struct {
	costs map[string][]float64 // lang/wire/path -> ns per record, one per round
	bytes map[string]float64   // lang/wire -> mean bytes per record
	form  map[string]string    // lang -> the table bench name its rows carried
}

// collectNine re-reads the raw CSV of every attempt that supplied a row. The
// bench NAME is what distinguishes the fixed form from the tolerant one
// (§3.4), and `fast.json`'s metrics do not carry it, so the evidence file the
// attempt already named is the honest place to read it from. Re-parsing also
// re-applies every identity, corpus, checks and duration check.
func newMeasurement() tableMeasurement {
	return tableMeasurement{costs: map[string][]float64{}, bytes: map[string]float64{}, form: map[string]string{}}
}

func collectNine(dir string, e fastEvidence, m tableMeasurement) error {
	for _, attempt := range e.Attempts {
		// A superseded short attempt is evidence of the escalation, never a
		// measurement of this table.
		if !attempt.Adequate || attempt.Iterations != e.FinalCounts[attempt.Wire] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, attempt.Raw))
		if err != nil {
			return err
		}
		rows, err := parseRowsForIterations(data, attempt.Language, attempt.Wire, e.Build.CorpusIDs[attempt.Wire], attempt.Iterations, 0)
		if err != nil {
			return err
		}
		for _, row := range rows {
			key := attempt.Language + "/" + attempt.Wire + "/" + row.cols[2]
			m.costs[key] = append(m.costs[key], 1e9/row.rate)
			wireKey := attempt.Language + "/" + attempt.Wire
			if seen, ok := m.bytes[wireKey]; ok && seen != row.bytes {
				return fmt.Errorf("%s bytes per record changed between rounds", wireKey)
			}
			m.bytes[wireKey] = row.bytes
			if attempt.Wire != "table" {
				continue
			}
			if seen, ok := m.form[attempt.Language]; ok && seen != row.cols[1] {
				return fmt.Errorf("%s measured two table forms in one sitting: %s and %s", attempt.Language, seen, row.cols[1])
			}
			m.form[attempt.Language] = row.cols[1]
		}
	}
	return nil
}

// tableForms names the two table wires a row can have been measured on. A
// leg whose fixed-form port has not landed still measures the tolerant form,
// and the table says which one each row is rather than quietly implying
// they are the same wire.
var tableForms = map[string]string{"bench_fixed": "form 3 (fixed)", "bench_table": "form 2 (tolerant)"}

// A pass is one `-mode fast` sitting. The driver measures the four paired
// languages together — that is the only shape that yields a packet ratio —
// and a table-only language on its own, because it has no packet row to pair.
// The nine-language table is therefore composed of consecutive passes on ONE
// host against ONE build, never of rows carried in from elsewhere.
type ninePass struct {
	name     string
	dir      string
	langs    []string
	evidence fastEvidence
}

// readPass loads a completed pass's own evidence.
func (p *ninePass) read() error {
	data, err := os.ReadFile(filepath.Join(p.dir, "fast.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &p.evidence)
}

// agree refuses two passes a table may not put in the same column. Same build,
// same corpora, same rounds and the SAME ITERATION COUNT PER WIRE: §2.1 fixes
// one count per benchmark across every language, and two passes that settled
// on different counts are two tables, not one.
func agree(first, next fastEvidence) error {
	if first.Build.Revision != next.Build.Revision || first.Build.Host != next.Build.Host || first.Build.Arch != next.Build.Arch {
		return errors.New("passes belong to different builds or hosts")
	}
	for _, wire := range []string{"packet", "table"} {
		if first.Build.CorpusIDs[wire] != next.Build.CorpusIDs[wire] {
			return fmt.Errorf("passes carry different %s corpus ids", wire)
		}
		a, b := first.FinalCounts[wire], next.FinalCounts[wire]
		if a != 0 && b != 0 && a != b {
			return fmt.Errorf("passes settled on different %s iteration counts (%d and %d); repeat them", wire, a, b)
		}
	}
	if first.Config.Rounds != next.Config.Rounds {
		return errors.New("passes ran different round counts")
	}
	return nil
}

func buildNineTable(passes []ninePass, absent []skipped, lane string) (nineTable, error) {
	m := newMeasurement()
	first := passes[0].evidence
	t := nineTable{
		Host: first.Build.Host, OS: first.Build.OS, Arch: first.Build.Arch, CPU: first.Build.CPU,
		Revision: first.Build.Revision, Dirty: first.Build.Dirty, Rounds: first.Config.Rounds,
		Counts: map[string]int64{}, CorpusIDs: first.Build.CorpusIDs, Note: first.Config.Noise,
		Lane: lane, Absent: absent, Passes: len(passes),
	}
	low, high := "", ""
	for i := range passes {
		p := &passes[i]
		if err := agree(first, p.evidence); err != nil {
			return t, err
		}
		if err := collectNine(p.dir, p.evidence, m); err != nil {
			return t, err
		}
		maps.Copy(t.Counts, p.evidence.FinalCounts)
		t.Noise = append(t.Noise, p.evidence.Noise...)
		if r := loadRange(p.dir); r != "unavailable" {
			if low == "" {
				low = r
			}
			high = r
		}
	}
	t.Load = "unavailable"
	if low != "" {
		t.Load = low
		if high != low {
			t.Load = low + " then " + high
		}
	}
	for _, lang := range nineLanguages {
		save, trip := m.costs[lang+"/table/write"], m.costs[lang+"/table/round_trip"]
		if len(save) == 0 && len(trip) == 0 {
			continue
		}
		if len(save) != t.Rounds || len(trip) != t.Rounds {
			return t, fmt.Errorf("incomplete table evidence for %s", lang)
		}
		form, ok := tableForms[m.form[lang]]
		if !ok {
			return t, fmt.Errorf("%s measured an unknown table bench %q", lang, m.form[lang])
		}
		row := tableRow{Language: lang, Form: form, SaveNs: medianOf(save), TripNs: medianOf(trip), Bytes: m.bytes[lang+"/table"]}
		if packet := m.costs[lang+"/packet/round_trip"]; len(packet) > 0 {
			if len(packet) != t.Rounds {
				return t, fmt.Errorf("incomplete packet evidence for %s", lang)
			}
			row.PacketTripNs = medianOf(packet)
		}
		t.Rows = append(t.Rows, row)
	}
	if len(t.Rows) == 0 {
		return t, errors.New("no table row measured")
	}
	return t, nil
}

// reference is C++'s fixed form, the last column's denominator. A sitting
// without it renders every ratio as unavailable rather than promoting some
// other language to the reference.
func (t nineTable) reference() (float64, bool) {
	for _, row := range t.Rows {
		if row.Language == "cpp" && row.Form == tableForms["bench_fixed"] {
			return row.TripNs, true
		}
	}
	return 0, false
}

func (t nineTable) markdown() string {
	var b strings.Builder
	dirty := ""
	if t.Dirty {
		dirty = " (dirty)"
	}
	lane := ""
	if t.Lane != "" {
		lane = ", lane " + t.Lane
	}
	b.WriteString("# The nine-language table\n\n")
	fmt.Fprintf(&b, "`%s` (%s/%s, %s%s) · `%s`%s · %d rounds × %d pass(es) · load1 %s\n\n",
		t.Host, t.OS, t.Arch, t.CPU, lane, t.Revision, dirty, t.Rounds, t.Passes, t.Load)
	fmt.Fprintf(&b, "Corpus ids: packet `%s`, table `%s`. One iteration count per wire, identical across every language (BENCH-STANDARD §2.1): %s packet, %s table.\n\n",
		t.CorpusIDs["packet"], t.CorpusIDs["table"], grouped(t.Counts["packet"]), grouped(t.Counts["table"]))
	fmt.Fprintf(&b, "Operator context: %s\n\n", t.Note)
	b.WriteString("| Language | Wire | Save ns/record | Round trip ns/record | Bytes/record | % of own packet | % of C++ form 3 |\n|---|---|---:|---:|---:|---:|---:|\n")
	reference, haveReference := t.reference()
	for _, row := range t.Rows {
		packet := "—"
		if row.PacketTripNs > 0 {
			packet = fmt.Sprintf("%.0f%%", 100*row.TripNs/row.PacketTripNs)
		}
		against := "—"
		if haveReference {
			against = fmt.Sprintf("%.0f%%", 100*row.TripNs/reference)
		}
		fmt.Fprintf(&b, "| %s | %s | %.1f | %.1f | %.1f | %s | %s |\n",
			displayName(row.Language), row.Form, row.SaveNs, row.TripNs, row.Bytes, packet, against)
	}
	b.WriteString("\nLower is better in every column; ns and bytes are per record over the 64-record corpus. **% of own packet**: that language's own packet wire = 100%, so below 100% means the table wire beats it; `—` is a language with no packet leg, and no other language's packet may stand in for it. **% of C++ form 3**: C++'s fixed form = 100%. Round trip is read and write together; halving both leaves both ratios unchanged.\n")
	if !haveReference {
		b.WriteString("\nThis sitting measured no C++ fixed form, so the last column is unavailable rather than re-based on another language.\n")
	}
	if len(t.Absent) > 0 {
		b.WriteString("\nNot measured on this tree, and not estimated:\n\n")
		for _, s := range t.Absent {
			fmt.Fprintf(&b, "- **%s** — %s\n", displayName(s.Language), s.Reason)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", checksCaption)
	fmt.Fprintf(&b, "\n%s\n", nineQualification)
	if len(t.Noise) > 0 {
		fmt.Fprintf(&b, "\n**This host was not quiet: %d noise warning(s) during the sitting** — see each pass's `fast.json` and `process-samples.jsonl`. Read the rows accordingly.\n", len(t.Noise))
	}
	return b.String()
}

// ninePasses is the sitting's shape: the paired four together, then each
// table-only language on its own, in publication order.
func ninePasses(out string, measured []string) []ninePass {
	var paired, only []string
	for _, lang := range measured {
		if contains(languages, lang) {
			paired = append(paired, lang)
		} else {
			only = append(only, lang)
		}
	}
	var passes []ninePass
	if len(paired) > 0 {
		passes = append(passes, ninePass{name: "paired", dir: filepath.Join(out, "paired"), langs: paired})
	}
	for _, lang := range only {
		passes = append(passes, ninePass{name: lang, dir: filepath.Join(out, lang), langs: []string{lang}})
	}
	return passes
}

// reportTableLanguages is `-mode table-langs`: the tree's own answer, so the
// wrapper that builds the legs and the mode that measures them cannot
// disagree about which languages this tree carries. Present on stdout in the
// driver's own -langs spelling; absent on stderr, named.
func reportTableLanguages() error {
	present, absent := discoverTableLanguages()
	if len(present) == 0 {
		return errors.New("this tree carries no bench/tables/<lang> runner")
	}
	fmt.Println(strings.Join(present, ","))
	if len(absent) > 0 {
		fmt.Fprintln(os.Stderr, "no runner on this tree:", strings.Join(absent, ","))
	}
	return nil
}

// tablePass is the whole of `-mode table`: discover, probe, measure, render.
func tablePass(out string, info buildInfo, config fastConfig, lane string) error {
	if out == "" {
		return errors.New("table requires -out (a new report directory)")
	}
	present, missing := discoverTableLanguages()
	absent := make([]skipped, 0, len(nineLanguages))
	for _, lang := range missing {
		absent = append(absent, skipped{Language: lang, Reason: "no runner on this tree (" + tableRunnerDir(lang) + ")"})
	}
	// One rotation each, before any clock. A leg that is here but does not
	// answer to this corpus is named with its refusal rather than measured.
	measured := make([]string, 0, len(present))
	for _, lang := range present {
		if err := answers(lang, info); err != nil {
			absent = append(absent, skipped{Language: lang, Reason: "runner present, but its rows do not belong to this sitting: " + err.Error()})
			continue
		}
		measured = append(measured, lang)
	}
	sort.Slice(absent, func(i, j int) bool {
		return slices.Index(nineLanguages, absent[i].Language) < slices.Index(nineLanguages, absent[j].Language)
	})
	if len(measured) == 0 {
		return errors.New("no leg on this tree answers to this corpus")
	}
	fmt.Fprintln(os.Stderr, "measuring:", strings.Join(measured, ", "))
	for _, s := range absent {
		fmt.Fprintf(os.Stderr, "skipped %s: %s\n", s.Language, s.Reason)
	}
	passes := ninePasses(out, measured)
	for i := range passes {
		p := &passes[i]
		fmt.Fprintf(os.Stderr, "pass %d/%d: %s\n", i+1, len(passes), p.name)
		if err := fastMeasure(p.langs, p.dir, info, config); err != nil {
			return err
		}
		if err := p.read(); err != nil {
			return err
		}
	}
	t, err := buildNineTable(passes, absent, lane)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "NINE.md"), []byte(t.markdown()), 0644); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "nine.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	fmt.Print(t.markdown())
	fmt.Fprintln(os.Stderr, "nine-language table:", filepath.Join(out, "NINE.md"))
	return nil
}
