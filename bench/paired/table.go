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
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// nineRowOrder is the published ROW ORDER and NOTHING ELSE — the same nine
// `bench/tools/pass-driver.sh` names, C++ first because it is the reference
// implementation and the denominator of the last column.
//
// WHICH languages exist, and what each one is, is main.go's business and only
// main.go's: `languages` (published paired), `unpublishedLanguages` (paired,
// not published yet), `tableOnlyLanguages` (table wire only) and
// `pendingLanguages` (named here, no leg in this driver). This file used to
// keep a second roster beside that one and the two drifted — dart was in this
// list and in no list of main.go's, and elixir was called table-only here
// while main.go paired it — so the row set below is DERIVED and a language is
// classified in exactly one place.
var nineRowOrder = []string{"cpp", "c", "go", "rust", "cs", "js", "java", "dart", "elixir"}

// nineLanguages is the published row SET: main.go's roster, in that order.
var nineLanguages = orderedRoster()

// orderedRoster puts every name main.go carries — the languages this driver
// can measure, plus the pending rows it cannot — into publication order. A
// roster name the order forgot still gets a row, at the end, because dropping
// it silently is the failure this derivation exists to prevent.
func orderedRoster() []string {
	roster := append(allLanguages(), pendingLanguages...)
	out := make([]string, 0, len(roster))
	for _, lang := range nineRowOrder {
		if contains(roster, lang) {
			out = append(out, lang)
		}
	}
	for _, lang := range roster {
		if !contains(out, lang) {
			out = append(out, lang)
		}
	}
	return out
}

func displayName(lang string) string {
	if n := names[lang]; n != "" {
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
//
// A RUNNER DIRECTORY IS NOT ENOUGH: the language also has to be one this
// driver knows how to generate and build (`allLanguages()`). A pending row
// whose runner appears before its wiring does would otherwise be handed
// straight to a build that has no case for it, and the honest answer for it is
// the same either way — absent, with the reason said out loud.
func discoverTableLanguages() (present, absent []string) {
	for _, lang := range nineLanguages {
		if tableRunnerPresent(lang) && contains(allLanguages(), lang) {
			present = append(present, lang)
		} else {
			absent = append(absent, lang)
		}
	}
	return present, absent
}

// absentReason names WHY a row is not measured, and the two reasons are
// different facts about the merge: this tree carries no runner, or this driver
// carries no leg.
func absentReason(lang string) string {
	if !contains(allLanguages(), lang) {
		return "no leg in this driver yet (" + tableRunnerDir(lang) + " is not built, generated or measured here)"
	}
	return "no runner on this tree (" + tableRunnerDir(lang) + ")"
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
		// Not "no runner": a row can also be absent because this driver has no
		// leg for it (absentReason says which), and the wrapper's log should
		// not claim the one when it is the other.
		fmt.Fprintln(os.Stderr, "not measured on this tree:", strings.Join(absent, ","))
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
		absent = append(absent, skipped{Language: lang, Reason: absentReason(lang)})
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

// ---------------------------------------------------------------------------
// THE READ ARM: `-mode plan-read` (BENCH-STANDARD.md §1.10)
// ---------------------------------------------------------------------------
//
// The gate is the packet wire, not a ratio (Glenn, 2026-09-11): for EACH lane,
// identity and plan, judged separately, the fixed table's read of a record must
// be FASTER than the SAME leg's packet-wire read of the SAME logical record.
// Both sides are MEASURED reads out of ONE invocation (`--read-arm`), never a
// round trip minus a write. Beside it stands the older, C++-only anchor
// (docs/SPEC-TABLES.md §3.4): the identity plan against the hand-written
// straight-line reader of test/bench/fixedform_measure.cpp, bound 1.5x.
//
// What this tree can and cannot measure is said from the tree:
//
//   - the identity lane and the packet read: the C++ runner's `--read-arm`;
//   - the plan lane: needs the lineage corpus §1.10 names, and the board
//     prints the missing files by name rather than a number;
//   - the anchor: built beside the C++ legs by -mode build; a reference that
//     does not compile or does not run is printed as that refusal;
//   - every other leg: its runner has no read arm yet, named per leg.
//
// The sitting runs inside the confirmation pass's quiet window (window.go),
// bracketed by the C++ packet control leg (§2.6): a moved control or a
// competing process makes the window INVALID, and an INVALID window prints its
// numbers with NO verdict and exits non-zero.

const (
	readArmIterations int64 = 4000000 // records per sample, both wires (§2.1: fixed per benchmark)
	anchorReps              = 100000  // rotations of the 64 records per anchor sample
	anchorBestOf            = 2       // the reference's own best-of within one round
	anchorBound             = 1.5     // docs/SPEC-TABLES.md §3.4
	anchorSource            = "test/bench/fixedform_measure.cpp"
	anchorBinary            = "build/paired/anchor-cpp"
	spreadNoisy             = 15.0 // BENCH-STANDARD §2.3
	spreadInvalid           = 40.0
	controlTolerance        = 0.05 // §2.6
)

// lineageCorpus is the plan lane's input (§1.10 work list items 1–7). The plan
// lane is measured only when every one of these is on the tree AND the leg's
// runner answers for it; neither is true here yet.
var lineageCorpus = []string{"bench/corpus/VOLD_bench_lineage.schema", "bench/corpus/schema.lock", "bench/paired/corpus/bench_lineage.bin", "bench/paired/corpus/bench_lineage.layout"}

func planLaneRefusal(present func(string) bool) string {
	var missing []string
	for _, p := range lineageCorpus {
		if !present(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return "no lineage corpus on this tree: " + strings.Join(missing, ", ") + " absent (BENCH-STANDARD §1.10 work list items 1–7)"
	}
	return "the C++ runner has no plan lane yet (BENCH-STANDARD §1.10 work list item 8)"
}

// THE C LEG'S PLAN LANE reads the lineage pair the Rust leg committed
// (bench/tables/rust/lineage/ on 337aaacf), carried byte for byte under the C
// leg's own paths because the standard's home for it (bench/corpus/ +
// bench/paired/corpus/, §1.10 items 1–6) is outside this leg's card. The
// runner loads the .bin and the .layout (they ride its corpus id); the lock is
// compiled in (the unit generated beside it) and is recorded with the build.
const cLineageDir = "bench/tables/c/lineage"

var cLineageCorpus = []string{cLineageDir + "/schema.lock", cLineageDir + "/bench_lineage.bin", cLineageDir + "/bench_lineage.layout"}

// cPlanRefusal is "" when the C plan lane can be built from this tree.
func cPlanRefusal(present func(string) bool) string {
	var missing []string
	for _, p := range cLineageCorpus {
		if !present(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return "no lineage corpus on this tree: " + strings.Join(missing, ", ") + " absent (BENCH-STANDARD §1.10 work list items 1–7)"
	}
	return ""
}

// cPlanBuild is where the C plan lane's unit is staged and generated: the
// paired unit's two schema files beside the lineage's lock, generated a second
// time so its lineage holds the older layout. Nothing under it is committed.
const cPlanBuild = "build/paired/plan-c-unit"

// cPlanDefines are what the plan build adds to the C table leg's flag line:
// the table leg's own two, and BENCH_PLAN, which compiles the plan lane in.
var cPlanDefines = []string{"-ffp-contract=off", "-DBENCH_MATCHED", "-DBENCH_PLAN"}

// cPlanModules are the generated headers the plan binary was compiled from,
// recorded with the build so a regeneration after it refuses the sitting.
func cPlanModules() []string {
	found, _ := filepath.Glob(filepath.Join(cPlanBuild, "new", "*.h"))
	sort.Strings(found)
	return found
}

// buildCPlan generates the paired unit beside the lineage's lock and compiles
// the C table runner against it with -DBENCH_PLAN.
func buildCPlan(schema, cc string, cFlags []string, serC string) error {
	if err := os.RemoveAll(cPlanBuild); err != nil {
		return err
	}
	unit := filepath.Join(cPlanBuild, "unit")
	if err := os.MkdirAll(unit, 0755); err != nil {
		return err
	}
	var unitSchemas []string
	for _, from := range append(append([]string(nil), pairedSchemas...), cLineageDir+"/schema.lock") {
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		to := filepath.Join(unit, filepath.Base(from))
		if err := os.WriteFile(to, b, 0644); err != nil {
			return err
		}
		if strings.HasSuffix(to, ".schema") {
			unitSchemas = append(unitSchemas, to)
		}
	}
	newDir := filepath.Join(cPlanBuild, "new")
	if err := run(append([]string{schema, "generate", "--lang", "c", "--out", newDir}, unitSchemas...)...); err != nil {
		return fmt.Errorf("generating the paired unit beside %s/schema.lock: %w", cLineageDir, err)
	}
	_ = os.Remove(binary("plan", "c")) // a stale binary must never stand in for this build's
	a := append([]string{cc}, cFlags...)
	a = append(a, cPlanDefines...)
	a = append(a, "-I"+newDir, "-I"+serC, "bench/tables/c/table_main.c", filepath.Join(serC, "serialize.c"), "-o", binary("plan", "c"), "-lm")
	return run(a...)
}

const elixirLineageDir = "bench/tables/elixir/lineage"

var elixirLineageCorpus = []string{elixirLineageDir + "/schema.lock", elixirLineageDir + "/bench_lineage.bin", elixirLineageDir + "/bench_lineage.layout"}

func elixirPlanRefusal(present func(string) bool) string {
	var missing []string
	for _, p := range elixirLineageCorpus {
		if !present(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return "no lineage corpus on this tree: " + strings.Join(missing, ", ") + " absent (BENCH-STANDARD §1.10 work list items 1–7)"
	}
	return ""
}

const elixirPlanBuild = "build/paired/plan-elixir-unit"

func buildElixirPlan(schema string) error {
	if err := os.RemoveAll(elixirPlanBuild); err != nil {
		return err
	}
	unit := filepath.Join(elixirPlanBuild, "unit")
	if err := os.MkdirAll(unit, 0755); err != nil {
		return err
	}
	renamed := []string{"Bench.schema", "Wrap.schema"}
	for i, to := range renamed {
		b, err := os.ReadFile(pairedSchemas[i])
		if err != nil {
			return err
		}
		to = filepath.Join(unit, to)
		if err := os.WriteFile(to, b, 0644); err != nil {
			return err
		}
		renamed[i] = to
	}
	lockBytes, err := os.ReadFile(elixirLineageDir + "/schema.lock")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unit, "schema.lock"), lockBytes, 0644); err != nil {
		return err
	}
	newDir := filepath.Join(elixirPlanBuild, "new")
	if err := run(append([]string{schema, "generate", "--lang", "elixir", "--out", newDir}, renamed...)...); err != nil {
		return fmt.Errorf("generating the paired unit beside %s/schema.lock: %w", elixirLineageDir, err)
	}
	return compileElixirUnit("plan")
}

// THE C# LEG'S PLAN LANE reads the same lineage pair under bench/tables/cs/lineage/
// and compiles a second assembly with BENCH_PLAN defined against the unit
// generated beside bench/tables/cs/lineage/schema.lock.
const csLineageDir = "bench/tables/cs/lineage"

var csLineageCorpus = []string{csLineageDir + "/schema.lock", csLineageDir + "/bench_lineage.bin", csLineageDir + "/bench_lineage.layout"}

func csPlanRefusal(present func(string) bool) string {
	var missing []string
	for _, p := range csLineageCorpus {
		if !present(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return "no lineage corpus on this tree: " + strings.Join(missing, ", ") + " absent (BENCH-STANDARD §1.10 work list items 1–7)"
	}
	return ""
}

const csPlanBuild = "build/paired/plan-cs-unit"

func csPlanModules() []string {
	found, _ := filepath.Glob(filepath.Join(csPlanBuild, "new", "*.cs"))
	sort.Strings(found)
	return found
}

const csPlanProject = `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <Nullable>annotations</Nullable>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
    <InvariantGlobalization>true</InvariantGlobalization>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <DefineConstants>$(DefineConstants);BENCH_MATCHED;BENCH_PLAN</DefineConstants>
    <SerializeCsRoot Condition="'$(SerializeCsRoot)' == ''">../../../../serialize.cs</SerializeCsRoot>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="../../../bench/tables/cs/src/Program.cs" />
    <Compile Include="new/*.cs" Exclude="new/*Block.cs;new/*Cook.cs" />
    <Compile Include="$(SerializeCsRoot)/src/Serialize.cs" />
    <Compile Include="$(SerializeCsRoot)/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`

func buildCSPlan(schema, serCs string) error {
	if err := os.RemoveAll(csPlanBuild); err != nil {
		return err
	}
	unit := filepath.Join(csPlanBuild, "unit")
	if err := os.MkdirAll(unit, 0755); err != nil {
		return err
	}
	var unitSchemas []string
	for _, from := range append(append([]string(nil), pairedSchemas...), csLineageDir+"/schema.lock") {
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		to := filepath.Join(unit, filepath.Base(from))
		if err := os.WriteFile(to, b, 0644); err != nil {
			return err
		}
		if strings.HasSuffix(to, ".schema") {
			unitSchemas = append(unitSchemas, to)
		}
	}
	newDir := filepath.Join(csPlanBuild, "new")
	if err := run(append([]string{schema, "generate", "--lang", "cs", "--out", newDir}, unitSchemas...)...); err != nil {
		return fmt.Errorf("generating the paired unit beside %s/schema.lock: %w", csLineageDir, err)
	}
	proj := filepath.Join(csPlanBuild, "paired.csproj")
	if err := os.WriteFile(proj, []byte(csPlanProject), 0644); err != nil {
		return err
	}
	_ = os.Remove(binary("plan", "cs"))
	return run("dotnet", "build", proj, "-c", "Release", "-v", "q", "--nologo", "-property:UseSharedCompilation=false", "-property:SerializeCsRoot="+serCs, "--output", filepath.Dir(binary("plan", "cs")))
}

// THE DART LEG'S PLAN LANE reads the lineage pair the Rust leg committed
// (bench/tables/rust/lineage/ on 337aaacf), carried byte for byte under the
// Dart leg's own paths because the standard's home for it (bench/corpus/ +
// bench/paired/corpus/, §1.10 items 1–6) is outside this leg's card. The
// runner loads the .bin and the .layout (they ride its corpus id); the lock is
// compiled in (the unit generated beside it) and is recorded with the build.
const dartLineageDir = "bench/tables/dart/lineage"

var dartLineageCorpus = []string{dartLineageDir + "/schema.lock", dartLineageDir + "/bench_lineage.bin", dartLineageDir + "/bench_lineage.layout"}

// dartPlanRefusal is "" when the Dart plan lane can be built from this tree.
func dartPlanRefusal(present func(string) bool) string {
	var missing []string
	for _, p := range dartLineageCorpus {
		if !present(p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return "no lineage corpus on this tree: " + strings.Join(missing, ", ") + " absent (BENCH-STANDARD §1.10 work list items 1–7)"
	}
	return ""
}

// dartPlanBuild is where the Dart plan lane's unit is staged and generated:
// the paired unit's two schema files beside the lineage's lock, generated a
// second time so its lineage holds the older layout. Nothing under it is
// committed.
const dartPlanBuild = "build/paired/plan-dart-unit"

// dartPlanModules are the generated Dart sources the plan binary was compiled
// from, recorded with the build so a regeneration after it refuses the sitting.
func dartPlanModules() []string {
	found, _ := filepath.Glob(filepath.Join(dartPlanBuild, "generated", "bench", "paired", "dart", "*.dart"))
	sort.Strings(found)
	return found
}

// buildDartPlan generates the paired unit beside the lineage's lock and
// compiles the Dart table runner against it into one AOT executable.
func buildDartPlan(schema, dart string) error {
	if err := os.RemoveAll(dartPlanBuild); err != nil {
		return err
	}
	unit := filepath.Join(dartPlanBuild, "unit")
	if err := os.MkdirAll(unit, 0755); err != nil {
		return err
	}
	var unitSchemas []string
	for _, from := range append(append([]string(nil), pairedSchemas...), dartLineageDir+"/schema.lock") {
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		to := filepath.Join(unit, filepath.Base(from))
		if err := os.WriteFile(to, b, 0644); err != nil {
			return err
		}
		if strings.HasSuffix(to, ".schema") {
			unitSchemas = append(unitSchemas, to)
		}
	}
	pairedDir := filepath.Join(dartPlanBuild, "generated", "bench", "paired", "dart")
	if err := run(append([]string{schema, "generate", "--lang", "dart", "--out", pairedDir}, unitSchemas...)...); err != nil {
		return fmt.Errorf("generating the paired unit beside %s/schema.lock: %w", dartLineageDir, err)
	}
	packetDir := filepath.Join(dartPlanBuild, "generated", "bench", "dart")
	if err := run(schema, "generate", "--lang", "dart", "--out", packetDir, "bench/corpus/Bench.schema"); err != nil {
		return err
	}
	runnerDir := filepath.Join(dartPlanBuild, "bench", "tables", "dart")
	if err := os.MkdirAll(runnerDir, 0755); err != nil {
		return err
	}
	runnerBytes, err := os.ReadFile(filepath.Join("bench", "tables", "dart", "table_main.dart"))
	if err != nil {
		return err
	}
	runnerSource := filepath.Join(runnerDir, "table_main.dart")
	if err := os.WriteFile(runnerSource, runnerBytes, 0644); err != nil {
		return err
	}
	_ = os.Remove(binary("plan", "dart")) // a stale binary must never stand in for this build's
	return run(dart, "compile", "exe", "-o", binary("plan", "dart"), runnerSource)
}

func lineageDirFor(leg string) string {
	switch leg {
	case "cs":
		return csLineageDir
	case "dart":
		return dartLineageDir
	case "elixir":
		return elixirLineageDir
	}
	return cLineageDir
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// readArmCorpusPaths is the read arm's corpus; the plan build also loads the
// lineage pair, so its ONE id covers all three lanes' files.
func readArmCorpusPaths(plan bool) map[string]string {
	return readArmCorpusPathsFor("c", plan)
}

func readArmCorpusPathsFor(leg string, plan bool) map[string]string {
	paths := corpusPaths("table")
	maps.Copy(paths, corpusPaths("packet"))
	if plan {
		dir := lineageDirFor(leg)
		for _, name := range []string{"bench_lineage.bin", "bench_lineage.layout"} {
			paths[name] = dir + "/" + name
		}
	}
	return paths
}

type anchorBuild struct {
	Command []string
	Refusal string // empty when built
}

// buildAnchor compiles the straight-line reference with the C++ legs' own
// flag line. It never fails the build: a reference that does not compile is a
// named refusal on the read arm's anchor line, and the legs are unaffected.
func buildAnchor(cxx string, flags []string, ser string) anchorBuild {
	_ = os.Remove(anchorBinary) // a stale binary must never stand in for this build's
	args := append([]string{cxx}, flags...)
	args = append(args, "-Igenerated/bench/paired/cpp", "-I"+ser, anchorSource, "-o", anchorBinary)
	fmt.Fprintln(os.Stderr, "+", strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return anchorBuild{Command: args}
	}
	_, _ = os.Stderr.Write(output) // the compiler's own words; the refusal below names the first error
	return anchorBuild{Command: args, Refusal: "does not compile: " + firstCompilerError(string(output), err)}
}

func firstCompilerError(output string, err error) string {
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "error:") {
			return strings.TrimSpace(line)
		}
	}
	return err.Error()
}

// anchorSample is one round of the reference: its own best-of over the
// identity plan (arm A, the shipped loop) and the straight-line reader (arm B).
type anchorSample struct {
	IdentityNs     float64 `json:"identity_ns_per_record"`
	StraightLineNs float64 `json:"straight_line_ns_per_record"`
}

var (
	anchorLayoutLine   = regexp.MustCompile(`(?m)^hand layout == emitter, 64 records, \d+ bytes each$`)
	anchorIdentityLine = regexp.MustCompile(`(?m)^READ\s+A plan-driven \(identity\)\s+([0-9.]+) ns/record$`)
	anchorStraightLine = regexp.MustCompile(`(?m)^READ\s+B straight-line\s+([0-9.]+) ns/record$`)
)

// parseAnchor reads the reference's report. Its own correctness gate (the
// hand layout equals the emitter's, byte for byte) must have printed, and each
// arm's sample must clear the 200ms floor at the requested repetitions.
func parseAnchor(output []byte, reps int) (anchorSample, error) {
	text := string(output)
	if !anchorLayoutLine.MatchString(text) {
		return anchorSample{}, errors.New("the reference did not print its hand-layout gate")
	}
	value := func(re *regexp.Regexp, arm string) (float64, error) {
		m := re.FindStringSubmatch(text)
		if m == nil {
			return 0, fmt.Errorf("the reference printed no arm %s line", arm)
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil || v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			return 0, fmt.Errorf("the reference's arm %s is not a positive number", arm)
		}
		if seconds := v * float64(reps) * 64 / 1e9; seconds < fastMinimumSeconds {
			return 0, fmt.Errorf("the reference's arm %s sample is %.3fs, under the 200ms floor; raise its repetitions", arm, seconds)
		}
		return v, nil
	}
	a, err := value(anchorIdentityLine, "A")
	if err != nil {
		return anchorSample{}, err
	}
	b, err := value(anchorStraightLine, "B")
	if err != nil {
		return anchorSample{}, err
	}
	return anchorSample{IdentityNs: a, StraightLineNs: b}, nil
}

// readArmRow is one measured read of one round, as the runner reported it.
type readArmRow struct {
	Ns    float64 `json:"ns_per_record"`
	Bytes float64 `json:"bytes_per_record"`
}

// parseReadArm accepts exactly the read arm's two rows: identity, family,
// path, checks, corpus id, iterations and the 200ms floor, all checked.
func parseReadArm(data []byte, id string, iterations int64) (fixed, packet readArmRow, err error) {
	rows, err := parseReadArmLeg(data, "cpp", id, iterations, false)
	return rows.Fixed, rows.Packet, err
}

// readArmRows is one invocation's measured reads; Plan is zero unless asked.
type readArmRows struct{ Fixed, Plan, Packet readArmRow }

// parseReadArmLeg is parseReadArm for any leg, and with plan the plan lane's
// third row (bench_plan_read, family table, checks contract) is REQUIRED: a
// plan build that printed no plan row is refused, never read as two lanes.
func parseReadArmLeg(data []byte, lang, id string, iterations int64, plan bool) (readArmRows, error) {
	var none readArmRows
	want := map[string][2]string{"bench_fixed_read": {"table", "contract"}, "bench_mixed_read": {"gen", expectedChecks(lang, "packet")}}
	if plan {
		want["bench_plan_read"] = [2]string{"table", "contract"}
	}
	got := map[string]readArmRow{}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	for {
		cols, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return none, e
		}
		if len(cols) == 0 || strings.HasPrefix(cols[0], "#") || cols[0] == "lang" {
			continue
		}
		if len(cols) != 17 || cols[0] != lang {
			return none, errors.New("invalid read-arm CSV row")
		}
		axes, ok := want[cols[1]]
		if !ok || cols[2] != "read" || cols[12] != axes[0] || cols[14] != axes[1] || cols[11] != id || cols[5] != "1" {
			return none, fmt.Errorf("wrong read-arm row identity: %v", cols)
		}
		if _, dup := got[cols[1]]; dup {
			return none, fmt.Errorf("duplicate read-arm row %s", cols[1])
		}
		rate, e1 := strconv.ParseFloat(cols[8], 64)
		size, e2 := strconv.ParseFloat(cols[4], 64)
		iters, e3 := strconv.ParseInt(cols[3], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || rate <= 0 || size <= 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
			return none, fmt.Errorf("invalid read-arm numbers: %v", cols)
		}
		if iters != iterations {
			return none, fmt.Errorf("read-arm row %s ran %d iterations, not %d", cols[1], iters, iterations)
		}
		if float64(iters)/rate < fastMinimumSeconds {
			return none, fmt.Errorf("read-arm row %s sample is under the 200ms floor", cols[1])
		}
		got[cols[1]] = readArmRow{Ns: 1e9 / rate, Bytes: size}
	}
	if len(got) != len(want) {
		if plan {
			return none, errors.New("the plan build's read arm must report bench_fixed_read, bench_plan_read and bench_mixed_read")
		}
		return none, errors.New("the read arm must report bench_fixed_read and bench_mixed_read")
	}
	return readArmRows{Fixed: got["bench_fixed_read"], Plan: got["bench_plan_read"], Packet: got["bench_mixed_read"]}, nil
}

// rowStats is one measured row across the sitting's rounds. Best is the
// headline (§2.2: interference only ever slows a run), the median stands
// beside it, and spread is §2.3's (max − min) / median over RATES.
type rowStats struct {
	BestNs    float64   `json:"best_ns_per_record"`
	MedianNs  float64   `json:"median_ns_per_record"`
	WorstNs   float64   `json:"worst_ns_per_record"`
	SpreadPct float64   `json:"spread_pct"`
	Samples   []float64 `json:"samples_ns_per_record"`
}

func statsOf(samples []float64) rowStats {
	s := rowStats{Samples: append([]float64(nil), samples...)}
	if len(samples) == 0 {
		return s
	}
	sorted := slices.Sorted(slices.Values(samples))
	s.BestNs, s.WorstNs = sorted[0], sorted[len(sorted)-1]
	s.MedianNs = medianOf(append([]float64(nil), samples...))
	// rates: best rate is 1/best ns
	s.SpreadPct = (1/s.BestNs - 1/s.WorstNs) / (1 / s.MedianNs) * 100
	return s
}

// spreadMark is §2.3's disposition: "" publishes, "noisy" publishes with the
// mark, "invalid" is never used in a ratio or a verdict.
func spreadMark(s rowStats) string {
	switch {
	case s.SpreadPct > spreadInvalid:
		return "invalid"
	case s.SpreadPct > spreadNoisy:
		return "noisy"
	}
	return ""
}

// faster is the one automatic verdict this arm has: the lane against the same
// leg's packet read, best against best, refused when either row's spread is
// invalid or the window is not OK.
func faster(lane, packet rowStats, windowOK bool) string {
	if !windowOK {
		return "— (window INVALID)"
	}
	if spreadMark(lane) == "invalid" || spreadMark(packet) == "invalid" {
		return "— (spread > 40%)"
	}
	verdict := "FAIL"
	if lane.BestNs < packet.BestNs {
		verdict = "PASS"
	}
	if spreadMark(lane) == "noisy" || spreadMark(packet) == "noisy" {
		verdict += " (noisy)"
	}
	return verdict
}

type anchorResult struct {
	Refusal      string         `json:"refusal,omitempty"`
	Identity     rowStats       `json:"identity"`
	StraightLine rowStats       `json:"straight_line"`
	Ratio        float64        `json:"ratio_best_over_best"`
	RoundRatios  []float64      `json:"round_ratios"`
	Verdict      string         `json:"verdict"`
	Samples      []anchorSample `json:"samples"`
}

func anchorOf(samples []anchorSample, refusal string, windowOK bool) anchorResult {
	a := anchorResult{Refusal: refusal, Samples: samples}
	if refusal != "" {
		a.Verdict = "— (refused)"
		return a
	}
	var ids, straights []float64
	for _, s := range samples {
		ids, straights = append(ids, s.IdentityNs), append(straights, s.StraightLineNs)
		a.RoundRatios = append(a.RoundRatios, s.IdentityNs/s.StraightLineNs)
	}
	a.Identity, a.StraightLine = statsOf(ids), statsOf(straights)
	if len(samples) == 0 {
		a.Verdict = "— (no sample)"
		return a
	}
	a.Ratio = a.Identity.BestNs / a.StraightLine.BestNs
	switch {
	case !windowOK:
		a.Verdict = "— (window INVALID)"
	case spreadMark(a.Identity) == "invalid" || spreadMark(a.StraightLine) == "invalid":
		a.Verdict = "— (spread > 40%)"
	case a.Ratio <= anchorBound:
		a.Verdict = "PASS"
	default:
		a.Verdict = "FAIL"
	}
	return a
}

// readArmBoard is the read arm's whole rendered input, kept apart from the
// measuring so a test can build one without a machine.
type readArmBoard struct {
	Host         string       `json:"host"`
	OS           string       `json:"os"`
	Arch         string       `json:"arch"`
	CPU          string       `json:"cpu"`
	Compiler     string       `json:"cpp_compiler"`
	Flags        string       `json:"cpp_table_flags"`
	Revision     string       `json:"revision"`
	Serialize    string       `json:"serialize_revision"`
	Dirty        []string     `json:"dirty_paths"`
	Rounds       int          `json:"rounds"`
	Iterations   int64        `json:"iterations_per_sample"`
	AnchorReps   int          `json:"anchor_repetitions"`
	CorpusID     string       `json:"corpus_id_identity"`
	PlanRefusal  string       `json:"plan_lane_refusal"`
	Window       string       `json:"window"`
	WindowReason string       `json:"window_reason,omitempty"`
	ControlDelta float64      `json:"control_delta_pct"`
	Load         string       `json:"load1_range"`
	Receipt      string       `json:"receipt"`
	FixedBytes   float64      `json:"fixed_bytes_per_record"`
	PacketBytes  float64      `json:"packet_bytes_per_record"`
	Identity     rowStats     `json:"identity_read"`
	Packet       rowStats     `json:"packet_read"`
	GateIdentity string       `json:"gate_identity"`
	GatePlan     string       `json:"gate_plan"`
	Anchor       anchorResult `json:"anchor"`
	Absent       []skipped    `json:"other_legs"`
	// THE LEG this board measures: "" and "cpp" are the C++ board, "c" the
	// C one, "cs" the C# one, "elixir" the Elixir one. Plan is measured only
	// on a leg whose plan build ran (PlanRefusal empty); its corpus id is the
	// identity row's, one invocation's union.
	Leg       string   `json:"leg,omitempty"`
	Lane      string   `json:"lane,omitempty"`
	Plan      rowStats `json:"plan_read"`
	PlanBytes float64  `json:"plan_bytes_per_record,omitempty"`
	Widened   string   `json:"plan_widened,omitempty"`
}

func (b readArmBoard) leg() string {
	if b.Leg == "" {
		return "cpp"
	}
	return b.Leg
}

// legTitle is the leg as the board's title names it.
func legTitle(leg string) string {
	switch leg {
	case "cpp":
		return "C++"
	case "c":
		return "C"
	case "elixir":
		return "Elixir"
	case "cs":
		return "C#"
	case "dart":
		return "Dart"
	}
	return leg
}

func (b readArmBoard) windowOK() bool {
	return b.Window == "OK" && b.ControlDelta <= controlTolerance*100
}

// otherLegs names every leg but C++ with the reason it has no line yet.
func otherLegs() []skipped { return otherLegsBut("cpp") }

// otherLegsBut names every leg but the measured one.
func otherLegsBut(leg string) []skipped {
	var out []skipped
	for _, lang := range nineLanguages {
		if lang == leg {
			continue
		}
		reason := absentReason(lang)
		if tableRunnerPresent(lang) && contains(allLanguages(), lang) {
			reason = "runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)"
			if contains(planReadLanguages, lang) {
				reason = "runner present; its reads are on the plan-read board (-mode plan-read -langs " + lang + "), not this read arm"
			}
		}
		out = append(out, skipped{Language: lang, Reason: reason})
	}
	return out
}

func nsCell(s rowStats) string {
	if len(s.Samples) == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", s.BestNs)
}

func (b readArmBoard) markdown() string {
	var w strings.Builder
	dirty := "clean"
	if len(b.Dirty) > 0 {
		dirty = "dirty, build-regenerated only: " + strings.Join(b.Dirty, ", ")
	}
	window := b.Window
	if !b.windowOK() {
		window = "INVALID"
	}
	if b.WindowReason != "" {
		window += " (" + b.WindowReason + ")"
	}
	leg := b.leg()
	fmt.Fprintf(&w, "# The read arm: %s\n\n```\n", legTitle(leg))
	w.WriteString("# fixed form: the identity lane, the plan lane, and the same leg's packet wire\n")
	lane := "none"
	if b.Lane != "" {
		lane = b.Lane
	}
	fmt.Fprintf(&w, "# host %s / arch %s/%s / %s / rev %s (%s) / serialize %s / lane %s / rounds %d\n", b.Host, b.OS, b.Arch, b.CPU, b.Revision, dirty, b.Serialize, lane, b.Rounds)
	planMeasured := b.PlanRefusal == "" && len(b.Plan.Samples) > 0
	if b.PlanRefusal == "" && !planMeasured {
		// the plan build ran but the sitting stopped before a plan row
		b.PlanRefusal = "not measured: the sitting stopped before its first plan row"
	}
	if planMeasured {
		fmt.Fprintf(&w, "# corpus_id(identity) %s / corpus_id(plan) %s (one invocation, one corpus: identity, lineage and packet files)\n", b.CorpusID, b.CorpusID)
	} else {
		fmt.Fprintf(&w, "# corpus_id(identity) %s / corpus_id(plan) — (%s)\n", b.CorpusID, b.PlanRefusal)
	}
	fmt.Fprintf(&w, "# window: %s / control_delta_pct %.2f (bound %.0f) / load1 %s\n", window, b.ControlDelta, controlTolerance*100, b.Load)
	fmt.Fprintf(&w, "# iterations: %s records per sample per wire, 1 discarded warmup + 1 measured run per round (§2.1, §2.4)\n", grouped(b.Iterations))
	if planMeasured {
		fmt.Fprintf(&w, "# bytes_per_op: fixed %.1f B/record, plan (lineage file) %.1f B/record, packet %.0f B/record (§5.3 rule 2 — the division is this driver's alone)\n", b.FixedBytes, b.PlanBytes, b.PacketBytes)
	} else {
		fmt.Fprintf(&w, "# bytes_per_op: fixed %.1f B/record, packet %.0f B/record (§5.3 rule 2 — the division is this driver's alone)\n", b.FixedBytes, b.PacketBytes)
	}
	w.WriteString(checksLine(leg))
	switch {
	case leg != "cpp":
		fmt.Fprintf(&w, "# anchor: none for %s (SPEC-TABLES §3.4's straight-line reference, %s, is the C++ leg's alone)\n", legTitle(leg), anchorSource)
	case b.Anchor.Refusal != "":
		fmt.Fprintf(&w, "# C++ anchor: — %s %s  (bound %.1fx, SPEC-TABLES §3.4)\n", anchorSource, b.Anchor.Refusal, anchorBound)
	default:
		fmt.Fprintf(&w, "# C++ anchor: identity %.1f ns/record / straight-line %.1f = %.2fx  (bound %.1fx, SPEC-TABLES §3.4)  %s\n", b.Anchor.Identity.BestNs, b.Anchor.StraightLine.BestNs, b.Anchor.Ratio, anchorBound, b.Anchor.Verdict)
	}
	w.WriteString("\nleg      identity read    plan read    packet read    GATE identity  GATE plan    plan/identity\n")
	w.WriteString("         ns/record        ns/record    ns/record      vs packet      vs packet    x (reported)\n")
	gateIdentity := b.GateIdentity
	gatePlan := b.GatePlan
	if !b.windowOK() {
		gateIdentity = faster(b.Identity, b.Packet, false)
		if planMeasured {
			gatePlan = faster(b.Plan, b.Packet, false)
		}
	}
	ratio := "—"
	if b.windowOK() && planMeasured && len(b.Identity.Samples) > 0 {
		ratio = fmt.Sprintf("%.2f", b.Plan.BestNs/b.Identity.BestNs)
	}
	fmt.Fprintf(&w, "%-8s %12s %12s %14s    %-14s %-12s %s\n", leg, nsCell(b.Identity), nsCell(b.Plan), nsCell(b.Packet), gateIdentity, gatePlan, ratio)
	for _, s := range b.Absent {
		fmt.Fprintf(&w, "%-8s %12s %12s %14s    %-14s %-12s %s     %s\n", s.Language, "—", "—", "—", "—", "—", "—", s.Reason)
	}
	w.WriteString("```\n\n")
	if planMeasured {
		w.WriteString("Lower is better. The ns columns are each row's BEST round (§2.2); the GATE compares best against best, per lane, never an average; plan/identity is REPORTED, never a verdict.\n\n")
	} else {
		w.WriteString("Lower is better. The ns columns are each row's BEST round (§2.2); the GATE compares best against best, per lane, never an average. The plan lane and plan/identity are absent, not estimated: " + b.PlanRefusal + ".\n\n")
	}
	w.WriteString("| row | best ns/record | median | worst | spread (rates) | mark |\n|---|---:|---:|---:|---:|---|\n")
	row := func(name string, s rowStats) {
		if len(s.Samples) == 0 {
			fmt.Fprintf(&w, "| %s | — | — | — | — | not measured |\n", name)
			return
		}
		fmt.Fprintf(&w, "| %s | %.1f | %.1f | %.1f | %.1f%% | %s |\n", name, s.BestNs, s.MedianNs, s.WorstNs, s.SpreadPct, spreadMark(s))
	}
	row(leg+" `bench_fixed_read` (identity lane, family table)", b.Identity)
	if planMeasured {
		row(leg+" `bench_plan_read` (plan lane, family table)", b.Plan)
	}
	row(leg+" `bench_mixed_read` (packet read, family gen)", b.Packet)
	if leg == "cpp" && b.Anchor.Refusal == "" {
		row("anchor arm A, identity plan (shipped loop)", b.Anchor.Identity)
		row("anchor arm B, hand-written straight-line", b.Anchor.StraightLine)
	}
	if leg != "cpp" {
		fmt.Fprintf(&w, "\nEvery row comes out of ONE `--read-arm` invocation per round, the wires rotating which runs first; none is derived from a round trip (§2.9). The identity lane's op is one record of the shipped `LOAD` of the whole 64-record file (hash check and plan run inside the clock); the packet read's op is one read stream plus the generated read of one %.0f-byte record.", b.PacketBytes)
		if planMeasured {
			fmt.Fprintf(&w, " The plan lane's op is one record of the same `LOAD` over the lineage file (64 records one generation back, read through the static plan the lock's lineage entry compiled into the unit generated beside it; %s).", b.Widened)
		}
		w.WriteString("\n")
	} else {
		fmt.Fprintf(&w, "\nBoth GATE rows come out of ONE `--read-arm` invocation per round, the two wires alternating which runs first; neither is derived from a round trip (§2.9). The identity lane's op is one record of the shipped `LOAD` of the whole 64-record file (hash check and plan run inside the clock); the packet read's op is one `ReadStream` plus the generated read of one %.0f-byte record. The anchor is the reference's own process (arms A and B side by side, its best of %d per round at %s rotations of the 64 records), compiled with the C++ legs' flag line, and its ratio is best A over best B across the rounds.\n", b.PacketBytes, anchorBestOf, grouped(int64(b.AnchorReps)))
	}
	if leg == "cpp" && b.Anchor.Refusal != "" {
		fmt.Fprintf(&w, "\nThe anchor did not run: `%s` %s. No ratio is printed for it and none is estimated.\n", anchorSource, b.Anchor.Refusal)
	}
	fmt.Fprintf(&w, "\nCompiler: %s. Flags: `%s`. Window receipt: %s\n", b.Compiler, b.Flags, b.Receipt)
	if !b.windowOK() {
		w.WriteString("\n**The window is INVALID: these numbers carry no verdict and no ratio may be published from them (§2.6).**\n")
	}
	return w.String()
}

// dirtyPaths is the porcelain status as paths. The read arm accepts a tree
// whose only changes are files under generated/ — the build regenerates them
// from the committed emitter, and a revision whose committed copy is stale
// would otherwise refuse every C++ sitting — and NAMES them on the board.
// An UNTRACKED file is accepted only inside the measured leg's own emitter
// output (generated/bench/paired/<leg>/ or generated/bench/<leg>/): a module
// the emitter now writes that the committed copy predates (the Dart unit's
// BenchWire.dart on sprint/schema-dart). Any other change refuses the sitting.
func dirtyPaths(porcelain string, legs ...string) ([]string, error) {
	var paths []string
	for line := range strings.SplitSeq(porcelain, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 {
			return nil, fmt.Errorf("unreadable status line %q", line)
		}
		p := strings.TrimSpace(line[3:])
		if !strings.HasPrefix(p, "generated/") || (strings.HasPrefix(line, "??") && !legEmitted(p, legs)) {
			return nil, fmt.Errorf("the tree is dirty outside the build's own regenerated files: %s", p)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// legEmitted is true when p is a file (never a directory) directly inside one
// of the legs' own generated output directories.
func legEmitted(p string, legs []string) bool {
	if strings.HasSuffix(p, "/") {
		return false
	}
	for _, leg := range legs {
		for _, dir := range []string{"generated/bench/paired/" + leg + "/", "generated/bench/" + leg + "/"} {
			if rest, ok := strings.CutPrefix(p, dir); ok && rest != "" && !strings.Contains(rest, "/") {
				return true
			}
		}
	}
	return false
}

// porcelain is `git status --porcelain` UNTRIMMED: its first column is
// significant (" M" is a worktree change), and gitValue trims it away.
func porcelain() (string, error) {
	b, err := capture(command{args: []string{"git", "status", "--porcelain"}})
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(b), nil
}

// readArmLegs are the legs whose runner carries `--read-arm` on this tree.
var readArmLegs = []string{"cpp", "c", "cs", "dart", "elixir"}

// checksLine is the board's §1.9 checks axis. It names the measured leg's
// packet axis as expectedChecks has it: the Dart and Elixir boards' packet
// rows are checks=contract, which the C/C++/C#/Go sentence alone never says.
func checksLine(leg string) string {
	line := "# checks: table=contract, packet C/C++/C#=removed, packet Go=always"
	if leg == "dart" || leg == "elixir" {
		line += ", packet " + legTitle(leg) + "=" + expectedChecks(leg, "packet")
	}
	return line + " (§1.9)\n"
}

// planGateClaim is the sentence the board prints for what the leg's plan gate
// checked before the clock. It names only checks the leg's runner makes.
//
// The C runner (bench/tables/c/table_main.c, gates P2 to P4) derives the plan's
// answer from the 64 identity records it already held byte for byte to the C++
// reference (plan_expected_build, after bfb8be239's plan_expected), requires the
// plan read to save to those bytes with the counters exactly docs/
// FIXED-FORM-ALGORITHM.md section 5.4's (widened = 64 records x min(their_n,
// my_n) = 64 x 64 = 4096, every other counter zero), reads again into a
// poisoned destination (gate P3), and refuses an answer equal to the identity
// corpus's (gate P4). The Elixir runner (bench/tables/elixir/runner.exs gates
// P2 to P4) pins the same answer and counters and repeats an ordinary load; it
// supplies no poisoned destination, so its sentence claims none.
func planGateClaim(leg string) string {
	switch leg {
	case "c":
		return "its gate before the clock requires all 64 records to equal the plan's answer derived from the byte-gated identity corpus (every OLD field value, and the appended field at its declared default), the counters exactly widened 4096 (64 per record, min(their_n, my_n) of the stats bound) with every other counter zero, a poisoned destination reading to the same answer, and an answer that is not the identity corpus's"
	case "elixir":
		return "its gate before the clock requires all 64 records to equal the plan's answer derived from the byte-gated identity corpus (every OLD field value, and the appended field at its declared default), the counters exactly widened 4096 (64 per record, min(their_n, my_n) of the stats bound) with every other counter zero, a second ordinary load saving to the same bytes, and an answer that is not the identity corpus's"
	}
	return "its gate before the clock requires 64 records read with no counter raised but widened, a poisoned destination reading to the same answer, and an answer that is not the identity corpus's"
}

// readArmIterationsFor is the per-sample record count for a read-arm sitting:
// 4,000,000 for native legs, 400,000 for the BEAM leg so each sample stays comfortably
// above §2.1's 200ms floor without dragging a 7-round sitting across minutes.
func readArmIterationsFor(leg string) int64 {
	if leg == "elixir" {
		return 400000
	}
	return readArmIterations
}

// planReadPassOn is the whole of `-mode plan-read`: gate, window, control,
// rounds, control, verdicts, board, with the sitting's pinned core recorded.
func planReadPassOn(langs []string, out string, rounds int, info buildInfo, receipt, lane string) (resultErr error) {
	if len(langs) != 1 || !contains(readArmLegs, langs[0]) {
		return errors.New("plan-read measures ONE leg per sitting, and only a leg whose runner has a read arm (-langs cpp, -langs c, -langs cs, -langs dart or -langs elixir)")
	}
	leg := langs[0]
	if rounds < 7 {
		return errors.New("plan-read requires at least 7 rounds (§2.1)")
	}
	if out == "" {
		return errors.New("plan-read requires -out (a new results directory)")
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		return errors.New("refusing to overwrite results directory")
	}
	status, err := porcelain()
	if err != nil {
		return err
	}
	dirty, err := dirtyPaths(status, leg)
	if err != nil {
		return err
	}
	for _, wire := range wiresFor(leg) {
		if info.Binaries[binary(wire, leg)] == "" {
			return fmt.Errorf("cached build lacks %s/%s; build it first (-mode build -langs %s)", leg, wire, leg)
		}
	}
	// THE PLAN LANE runs where the leg's plan build ran: the C, C#, Dart or
	// Elixir leg, with the lineage corpus on the tree. That one build then times
	// all three reads.
	planRefusal := planLaneRefusal(fileExists)
	readWire, plan := "table", false
	switch leg {
	case "c":
		planRefusal = cPlanRefusal(fileExists)
		if planRefusal == "" && info.Binaries[binary("plan", "c")] == "" {
			planRefusal = "the cached build has no C plan binary (" + binary("plan", "c") + "); rebuild with -mode build -langs c"
		}
		if planRefusal == "" {
			readWire, plan = "plan", true
		}
	case "elixir":
		planRefusal = elixirPlanRefusal(fileExists)
		if planRefusal == "" && info.Binaries[binary("plan", "elixir")] == "" {
			planRefusal = "the cached build has no Elixir plan modules; rebuild with -mode build -langs elixir"
		}
		if planRefusal == "" {
			readWire, plan = "plan", true
		}
	case "cs":
		planRefusal = csPlanRefusal(fileExists)
		if planRefusal == "" && info.Binaries[binary("plan", "cs")] == "" {
			planRefusal = "the cached build has no C# plan assembly (" + binary("plan", "cs") + "); rebuild with -mode build -langs cs"
		}
		if planRefusal == "" {
			readWire, plan = "plan", true
		}
	case "dart":
		planRefusal = dartPlanRefusal(fileExists)
		if planRefusal == "" && info.Binaries[binary("plan", "dart")] == "" {
			planRefusal = "the cached build has no Dart plan binary (" + binary("plan", "dart") + "); rebuild with -mode build -langs dart"
		}
		if planRefusal == "" {
			readWire, plan = "plan", true
		}
	}
	id, err := corpusIDOf(readArmCorpusPathsFor(leg, plan))
	if err != nil {
		return err
	}
	iters := readArmIterationsFor(leg)
	arm := func(flags ...string) []string {
		a := append([]string{"--read-arm"}, flags...)
		a = append(a, "--packet-wire-dir", "testdata/wire", "--packet-variant-dir", "bench/corpus/variants")
		if plan {
			a = append(a, "--lineage-dir", lineageDirFor(leg))
		}
		return a
	}
	readArgs := func(round int) []string {
		return arm("--csv", "--round", strconv.Itoa(round), "--iterations", strconv.FormatInt(iters, 10), "--packet-iterations", strconv.FormatInt(iters, 10))
	}
	// THE GATES BEFORE ANY CLOCK: the shared producer's identity check, both
	// legs, and the read arm's own (both wires' goldens, every variant, and
	// on the plan build the lineage's).
	if err := gate(langs); err != nil {
		return err
	}
	if data, err := capture(runner(readWire, leg, arm("--gate")...)); err != nil || len(bytes.TrimSpace(data)) != 0 {
		return errors.Join(fmt.Errorf("%s read-arm gate failed or emitted timing rows", leg), err)
	}
	anchorRefusal := ""
	if leg != "cpp" {
		anchorRefusal = "none for this leg"
	} else if note := info.Tools["cpp_anchor"]; !strings.HasPrefix(note, "built: ") {
		anchorRefusal = note
		if anchorRefusal == "" {
			anchorRefusal = "not built by this build"
		}
	}
	parent := filepath.Dir(out)
	if e := os.MkdirAll(parent, 0755); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(parent, ".paired-read-")
	if e != nil {
		return e
	}
	fmt.Fprintln(os.Stderr, "raw read-arm evidence:", tmp)
	info.Rounds = rounds
	b, _ := json.MarshalIndent(info, "", "  ")
	if e = os.WriteFile(filepath.Join(tmp, "build.json"), append(b, '\n'), 0644); e != nil {
		return e
	}
	serialize, flags, compiler := info.Runtimes["serialize"], info.Tools[leg+"_table_flags"], firstLine(info.Tools[leg])
	switch leg {
	case "c":
		serialize = info.Runtimes["serialize.c"]
		if plan {
			flags = info.Tools["c_plan_flags"]
		}
	case "cs":
		serialize = info.Runtimes["serialize.cs"]
		if plan && info.Tools["cs_plan_flags"] != "" {
			flags = info.Tools["cs_plan_flags"]
		}
		if compiler == "" {
			compiler = firstLine(info.Tools["dotnet"])
		}
	case "dart":
		serialize = "none (generated AOT)"
		if plan {
			flags = info.Tools["dart_plan_flags"]
		}
	case "elixir":
		serialize = "none"
		if plan {
			flags = info.Tools["elixir_plan_flags"]
		}
	}
	board := readArmBoard{
		Host: info.Host, OS: info.OS, Arch: info.Arch, CPU: info.CPU, Compiler: compiler, Flags: flags,
		Revision: info.Revision, Serialize: serialize, Dirty: dirty, Rounds: rounds, Iterations: iters, AnchorReps: anchorReps,
		CorpusID: id, PlanRefusal: planRefusal, Window: "INVALID", Receipt: strings.TrimSpace(receipt), Absent: otherLegsBut(leg),
		GatePlan: "— (absent)", Leg: leg, Lane: lane,
	}
	window, err := beginWindow(tmp, receipt)
	if err != nil {
		return err
	}
	var fixedNs, planNs, packetNs []float64
	var anchors []anchorSample
	measureErr := func() error {
		control := func(label string) (map[string]float64, error) {
			controlWire, cIters := "packet", readArmIterations
			if contains(tableOnlyLanguages, leg) {
				// a table-only leg (Dart) has no packet runner: its control is
				// its own table leg at the table wire's iteration count
				controlWire, cIters = "table", 400000
			}
			cArgs := []string{"--csv", "--round", "0"}
			if leg == "elixir" {
				cIters = iters
				cArgs = append(cArgs, "--iterations", strconv.FormatInt(cIters, 10))
			}
			output, runErr := window.capture(runner(controlWire, leg, cArgs...), "control-"+label+"-"+controlWire)
			writeErr := os.WriteFile(filepath.Join(tmp, "control-"+label+"-"+controlWire+".csv"), output, 0644)
			if err := errors.Join(runErr, writeErr); err != nil {
				return nil, err
			}
			rows, err := parseRowsForIterations(output, leg, controlWire, info.CorpusIDs[controlWire], cIters, fastMinimumSeconds)
			if err != nil {
				return nil, err
			}
			rates := map[string]float64{}
			for _, row := range rows {
				rates[row.cols[2]] = row.rate
			}
			return rates, nil
		}
		before, err := control("start")
		if err != nil {
			return err
		}
		for round := range rounds {
			steps := []string{"read", "anchor"}
			if round%2 == 1 {
				steps = []string{"anchor", "read"}
			}
			for _, step := range steps {
				phase := fmt.Sprintf("round-%d-%s-%s", round, leg, step)
				fmt.Fprintln(os.Stderr, phase)
				switch step {
				case "read":
					output, runErr := window.capture(runner(readWire, leg, readArgs(round)...), phase)
					if err := errors.Join(runErr, os.WriteFile(filepath.Join(tmp, phase+".csv"), output, 0644)); err != nil {
						return err
					}
					rows, err := parseReadArmLeg(output, leg, id, iters, plan)
					if err != nil {
						return err
					}
					fixedNs, packetNs = append(fixedNs, rows.Fixed.Ns), append(packetNs, rows.Packet.Ns)
					board.FixedBytes, board.PacketBytes = rows.Fixed.Bytes, rows.Packet.Bytes
					if plan {
						planNs, board.PlanBytes = append(planNs, rows.Plan.Ns), rows.Plan.Bytes
					}
				case "anchor":
					if anchorRefusal != "" {
						continue
					}
					output, runErr := window.capture(command{args: []string{anchorBinary, strconv.Itoa(anchorReps), strconv.Itoa(anchorBestOf)}}, phase)
					if err := os.WriteFile(filepath.Join(tmp, phase+".txt"), output, 0644); err != nil {
						return err
					}
					if runErr != nil {
						// A reference that builds but does not run is the same
						// kind of fact as one that does not build: named, not
						// estimated, and the legs' numbers still stand.
						anchorRefusal = "builds but does not run: " + runErr.Error()
						anchors = nil
						continue
					}
					sample, err := parseAnchor(output, anchorReps)
					if err != nil {
						return err
					}
					anchors = append(anchors, sample)
				}
			}
		}
		after, err := control("end")
		if err != nil {
			return err
		}
		for key, start := range before {
			delta := math.Abs(after[key]/start - 1)
			board.ControlDelta = math.Max(board.ControlDelta, delta*100)
			if delta > controlTolerance {
				return fmt.Errorf("%s control %s moved %.1f%% (> 5%%); repeat the sitting", legTitle(leg), key, delta*100)
			}
		}
		if now, err := porcelain(); err != nil || now != status {
			return errors.Join(errors.New("source changed during the sitting"), err)
		}
		return verifyRecordedArtifacts(info)
	}()
	windowErr := window.finish(measureErr)
	board.Window, board.WindowReason = window.evidence.Verdict, window.evidence.Reason
	board.Load = loadRangeOf(window.evidence.Samples)
	board.Identity, board.Packet = statsOf(fixedNs), statsOf(packetNs)
	if len(fixedNs) == rounds {
		board.GateIdentity = faster(board.Identity, board.Packet, board.windowOK())
	} else {
		board.GateIdentity = "— (incomplete)"
	}
	if plan {
		board.Plan = statsOf(planNs)
		board.Widened = planGateClaim(leg)
		if len(planNs) == rounds {
			board.GatePlan = faster(board.Plan, board.Packet, board.windowOK())
		} else {
			board.GatePlan = "— (incomplete)"
		}
	}
	board.Anchor = anchorOf(anchors, anchorRefusal, board.windowOK())
	if anchorRefusal == "" && len(anchors) != rounds {
		board.Anchor.Verdict = "— (incomplete)"
	}
	text := board.markdown()
	if err := os.WriteFile(filepath.Join(tmp, "READ.md"), []byte(text), 0644); err != nil {
		return errors.Join(windowErr, err)
	}
	jb, _ := json.MarshalIndent(board, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "read.json"), append(jb, '\n'), 0644); err != nil {
		return errors.Join(windowErr, err)
	}
	if err := os.Rename(tmp, out); err != nil {
		return errors.Join(windowErr, err)
	}
	fmt.Print(text)
	fmt.Fprintln(os.Stderr, "read-arm board:", filepath.Join(out, "READ.md"))
	return windowErr
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// loadRangeOf is loadRange over a window's own samples.
func loadRangeOf(samples []windowSample) string {
	low, high, seen := math.Inf(1), math.Inf(-1), false
	for _, s := range samples {
		if v, ok := load1(s.Load); ok {
			low, high, seen = math.Min(low, v), math.Max(high, v), true
		}
	}
	if !seen {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f–%.2f", low, high)
}

// ---------------------------------------------------------------------------
// THE PLAN-READ BOARD (BENCH-STANDARD.md §1.10, "The gate is the packet wire,
// not a ratio"). `-mode plan-read -langs <leg>` measures, per leg, the fixed
// form's identity-lane read, its plan-lane read and the SAME leg's packet-wire
// read, each timed alone, all three out of ONE runner invocation per round, and
// prints the per-leg line with its two GATE verdicts. The GATE is the only
// automatic verdict: a lane not faster than the leg's own packet read is FAIL,
// both numbers standing beside it. plan/identity is REPORTED, never judged.
// ---------------------------------------------------------------------------

// planReadLanguages are the legs whose table runner answers `--plan-read`.
// Every other leg is NAMED on the board with its reason, never estimated.
var planReadLanguages = []string{"rust"}

// planReadBoardSitting is true when -mode plan-read names only legs whose
// runner answers `--plan-read` (the plan-read board, planReadPass); a sitting
// of a read-arm leg (readArmLegs: C++, C, C#) is planReadPassOn's. The two
// arms measure the same three reads; they merge into one when a leg carries
// both.
func planReadBoardSitting(langs []string) bool {
	if len(langs) == 0 {
		return false
	}
	for _, lang := range langs {
		if !contains(planReadLanguages, lang) {
			return false
		}
	}
	return true
}

// The row names a plan-read invocation may emit, with the family and the
// wire each is judged as. bench_plan_read is optional in the parser: a leg
// whose lineage corpus has not landed names that lane and emits no row for it.
// A leg in planReadLineageLegs is held to it (parsePlanReadRows).
var planReadRows = map[string]struct{ family, wire string }{
	"bench_fixed_read": {"table", "table"},
	"bench_plan_read":  {"table", "table"},
	"bench_mixed_read": {"gen", "packet"},
}

// planLaneAbsent is the reason the plan lane is named rather than measured
// for a leg whose runner has no lineage corpus to read.
const planLaneAbsent = "no lineage corpus for this leg on this tree (BENCH-STANDARD.md §1.10 work list items 1-6)"

// planReadLineageLegs are the legs whose plan-read arm READS the lineage
// corpus, so a sitting of theirs with no bench_plan_read row is a refusal and
// never a NOT MEASURED line.
var planReadLineageLegs = []string{"rust"}

// THE LINEAGE CORPUS (BENCH-STANDARD.md §1.10). §1.10 homes it in
// bench/corpus/ (the VOLD schema, the unit's lock) and bench/paired/corpus/
// (the C++ dump's bytes); it landed with the Rust leg's plan lane, under that
// leg's own directory, and the move to the standard's home is the C++ dump's
// work-list items. The three files are what the plan-read invocation hashes
// into its corpus id, the lock included, because a lock edit silently changes
// which plan ran.
const rustLineageDir = "bench/tables/rust/lineage"

var lineageCorpusFiles = []string{"bench_lineage.bin", "bench_lineage.layout", "schema.lock"}

// planReadCorpusFiles is what the plan-read invocation loads: the table
// pass's five files, the packet pass's two and the lineage corpus's three.
// Its corpus id is therefore its own by §1.6's definition: the identity and
// the plan rows SHARE it (one invocation), and neither divides against a row
// of the ordinary table pass, whose id is different.
func planReadCorpusFiles() map[string]string {
	paths := map[string]string{
		"bench_mixed.bin":          "testdata/wire/bench_mixed.bin",
		"bench_mixed.variants.bin": "bench/corpus/variants/bench_mixed.variants.bin",
	}
	for _, name := range []string{"bench_table.bin", "bench_table.lengths", "bench_table.variants.bin", "bench_fixed.bin", "bench_fixed.layout"} {
		paths[name] = "bench/paired/corpus/" + name
	}
	for _, name := range lineageCorpusFiles {
		paths[name] = rustLineageDir + "/" + name
	}
	return paths
}

// THE RUST PLAN LANE'S BUILD. The plan lane needs a reader whose lineage holds
// the VOLD layout, and the ordinary paired unit is generated with no lock, so
// the unit is generated a SECOND time, from the same two schema files copied
// byte for byte beside the committed lock, and src/main.rs is built against
// that generation with its `lineage` feature on, as `table-rust-plan`. The
// VOLD generation is built beside it: the runner's producer and its gate use
// it to re-derive the committed OLD file, never inside a clock.
//
// Every manifest here is BUILD WIRING under build/, written whole on every
// build; nothing tracked is written, so the sitting's checkpoint stays clean.
const rustPlanBuild = "build/paired/lineage"

// rustPlanModules are the generated sources the plan binary is built from,
// recorded in build.json beside the binary so a regeneration after the build
// is refused like any other changed artifact.
func rustPlanModules() ([]string, error) {
	var out []string
	for _, dir := range []string{"new", "old"} {
		files, err := filepath.Glob(filepath.Join(rustPlanBuild, dir, "*.rs"))
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("the rust plan lane's %s generation is missing under %s", dir, rustPlanBuild)
		}
		out = append(out, files...)
	}
	return out, nil
}

func rustCrateManifest(name, comment string) string {
	return "# " + comment + "\n# Written by bench/paired -mode build (rustPlanManifest); build wiring, not generator output.\n" +
		"[package]\nname = \"" + name + "\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[lib]\npath = \"lib.rs\"\n\n" +
		"[features]\nblock = []\ncook = []\n\n" +
		"[dependencies]\nserialize = { package = \"serialize-official\", path = \"../../serialize.rs\" }\n"
}

// rustPlanManifest is the plan binary's package: the tracked runner source,
// named by absolute path, against the two generations above.
func rustPlanManifest(mainRS string) string {
	return "# table-rust-plan: bench/tables/rust/src/main.rs with the plan lane (BENCH-STANDARD.md §1.10).\n" +
		"# Written by bench/paired -mode build (rustPlanManifest); build wiring, not a tracked file.\n" +
		"[package]\nname = \"benchtablesrustplan\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n" +
		"[[bin]]\nname = \"table-rust-plan\"\npath = " + strconv.Quote(mainRS) + "\n\n" +
		"[features]\ndefault = [\"lineage\"]\nlineage = []\n\n" +
		"[dependencies]\nbenchpaired = { package = \"benchlineage\", path = \"../new\" }\nbenchvold = { path = \"../old\" }\n" +
		"serialize = { package = \"serialize-official\", path = \"../../serialize.rs\" }\n"
}

func buildRustPlan(schema string, cargoEnv []string) error {
	if err := os.RemoveAll(rustPlanBuild); err != nil {
		return err
	}
	// TWO STAGED UNITS, because a lock binds the unit it sits beside: the
	// paired unit's two files beside the lineage's lock, and the VOLD file
	// alone, which the committed lock (package bench) does not belong to.
	unit, oldUnit := filepath.Join(rustPlanBuild, "unit"), filepath.Join(rustPlanBuild, "old-unit")
	for _, dir := range []string{unit, oldUnit, filepath.Join(rustPlanBuild, "runner")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	staged := map[string]string{
		rustLineageDir + "/schema.lock":               filepath.Join(unit, "schema.lock"),
		rustLineageDir + "/VOLD_bench_lineage.schema": filepath.Join(oldUnit, "VOLD_bench_lineage.schema"),
	}
	var unitSchemas []string
	for _, from := range pairedSchemas {
		to := filepath.Join(unit, filepath.Base(from))
		staged[from], unitSchemas = to, append(unitSchemas, to)
	}
	for from, to := range staged {
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, b, 0644); err != nil {
			return err
		}
	}
	newDir, oldDir := filepath.Join(rustPlanBuild, "new"), filepath.Join(rustPlanBuild, "old")
	if err := run(append([]string{schema, "generate", "--lang", "rust", "--out", newDir}, unitSchemas...)...); err != nil {
		return fmt.Errorf("generating the paired unit beside %s/schema.lock: %w", rustLineageDir, err)
	}
	if err := run(schema, "generate", "--lang", "rust", "--out", oldDir, filepath.Join(oldUnit, "VOLD_bench_lineage.schema")); err != nil {
		return err
	}
	for dir, text := range map[string]string{
		filepath.Join(newDir, "Cargo.toml"):                  rustCrateManifest("benchlineage", "The paired unit generated beside bench/tables/rust/lineage/schema.lock: its lineage holds the VOLD layout."),
		filepath.Join(oldDir, "Cargo.toml"):                  rustCrateManifest("benchvold", "The VOLD generation (bench/tables/rust/lineage/VOLD_bench_lineage.schema): the OLD file's writer."),
		filepath.Join(rustPlanBuild, "runner", "Cargo.toml"): rustPlanManifest(abs("bench/tables/rust/src/main.rs")),
	} {
		if err := os.WriteFile(dir, []byte(text), 0644); err != nil {
			return err
		}
	}
	if err := execute(command{args: []string{cargoExecutable(), "build", "--release", "--quiet", "--manifest-path", filepath.Join(rustPlanBuild, "runner", "Cargo.toml"), "--target-dir", rustTargetDir}, env: cargoEnv}); err != nil {
		return err
	}
	built, err := os.ReadFile(filepath.Join(rustTargetDir, "release", "table-rust-plan"))
	if err != nil {
		return err
	}
	if err := os.Remove(binary("plan", "rust")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(binary("plan", "rust"), built, 0755)
}

// fnvCorpusID is §1.6's id over named files: sorted basenames, each fed as
// the name, a zero byte, then the contents.
func fnvCorpusID(paths map[string]string, read func(string) ([]byte, error)) (string, error) {
	h := uint64(0xcbf29ce484222325)
	feed := func(b []byte) {
		for _, v := range b {
			h ^= uint64(v)
			h *= 0x100000001b3
		}
	}
	for _, k := range slices.Sorted(maps.Keys(paths)) {
		b, err := read(paths[k])
		if err != nil {
			return "", err
		}
		feed([]byte(k))
		feed([]byte{0})
		feed(b)
	}
	return fmt.Sprintf("%016x", h), nil
}

func planReadCommand(lang string, round int, counts map[string]int64) command {
	return runner("plan", lang, "--plan-read", "--csv", "--round", strconv.Itoa(round),
		"--iterations", strconv.FormatInt(counts["table"], 10),
		"--packet-iterations", strconv.FormatInt(counts["packet"], 10),
		"--packet-wire-dir", "testdata/wire", "--packet-variant-dir", "bench/corpus/variants")
}

// parsePlanReadRows accepts one round's CSV from one leg: one measured run per
// row, the iteration count this sitting asked of the row's wire, one corpus id
// for every row, the row's own family and checks axis, and the identity and
// packet rows both present. A missing comparand is a refusal, not a blank.
func parsePlanReadRows(data []byte, lang, id string, counts map[string]int64) (map[string]sample, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	rows := map[string]sample{}
	for {
		cols, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 || strings.HasPrefix(cols[0], "#") || cols[0] == "lang" {
			continue
		}
		if len(cols) != 17 || cols[0] != lang {
			return nil, fmt.Errorf("invalid %s plan-read CSV row", lang)
		}
		kind, known := planReadRows[cols[1]]
		if !known || cols[2] != "read" || cols[12] != kind.family || cols[11] != id || cols[5] != "1" {
			return nil, fmt.Errorf("wrong %s plan-read row identity: %v", lang, cols)
		}
		if _, dup := rows[cols[1]]; dup {
			return nil, fmt.Errorf("duplicate %s plan-read row %s", lang, cols[1])
		}
		if want := expectedChecks(lang, kind.wire); cols[14] != want {
			return nil, fmt.Errorf("unexpected %s/%s checks axis %q; expected %q", lang, cols[1], cols[14], want)
		}
		rate, err := strconv.ParseFloat(cols[8], 64)
		if err != nil || math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
			return nil, errors.New("invalid rate")
		}
		n, err := strconv.ParseFloat(cols[4], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
			return nil, errors.New("invalid bytes/op")
		}
		iters, err := strconv.ParseInt(cols[3], 10, 64)
		if err != nil || iters != counts[kind.wire] {
			return nil, fmt.Errorf("%s/%s iterations must match the requested %s count %d", lang, cols[1], kind.wire, counts[kind.wire])
		}
		if cols[6] != cols[8] || cols[7] != cols[8] {
			return nil, errors.New("per-round rates must agree")
		}
		rows[cols[1]] = sample{cols: cols, rate: rate, bytes: n, iters: iters}
	}
	need := []string{"bench_fixed_read", "bench_mixed_read"}
	if contains(planReadLineageLegs, lang) {
		need = append(need, "bench_plan_read")
	}
	for _, need := range need {
		if _, ok := rows[need]; !ok {
			return nil, fmt.Errorf("%s plan-read emitted no %s row", lang, need)
		}
	}
	return rows, nil
}

// planReadStat is one row's statistic over the rounds: best (the headline,
// §2.2), median beside it, and the rate spread §2.3 disposes on.
type planReadStat struct {
	BestNs   float64   `json:"best_ns_per_record"`
	MedianNs float64   `json:"median_ns_per_record"`
	WorstNs  float64   `json:"worst_ns_per_record"`
	Spread   float64   `json:"spread_pct"`
	Bytes    float64   `json:"bytes_per_record"`
	Rounds   []float64 `json:"ns_per_record_by_round"`
}

func newPlanReadStat(rates []float64, bytesPerRecord float64) planReadStat {
	s := planReadStat{Bytes: bytesPerRecord}
	for _, r := range rates {
		s.Rounds = append(s.Rounds, 1e9/r)
	}
	sorted := slices.Clone(rates)
	median := medianOf(sorted) // sorts in place
	s.BestNs, s.WorstNs, s.MedianNs = 1e9/sorted[len(sorted)-1], 1e9/sorted[0], 1e9/median
	s.Spread = (sorted[len(sorted)-1] - sorted[0]) / median * 100
	return s
}

// disposition is §2.3's: a row over 40% spread is invalid and enters no
// verdict; over 15% it is noisy and says so.
func (s planReadStat) disposition() string {
	switch {
	case s.Spread > 40:
		return "invalid"
	case s.Spread > 15:
		return "noisy"
	}
	return "normal"
}

// gateVerdict is the per-lane GATE: the lane's best read against the packet
// read's best, both measured in this sitting. A row §2.3 invalidates yields no
// verdict at all rather than a PASS or a FAIL it could not support.
func gateVerdict(lane, packet planReadStat) string {
	if lane.disposition() == "invalid" || packet.disposition() == "invalid" {
		return "INVALID"
	}
	if lane.BestNs < packet.BestNs {
		return "PASS"
	}
	return "FAIL"
}

type planReadLeg struct {
	Language     string        `json:"language"`
	Identity     planReadStat  `json:"identity_read"`
	Plan         *planReadStat `json:"plan_read,omitempty"`
	PlanAbsent   string        `json:"plan_absent,omitempty"`
	Packet       planReadStat  `json:"packet_read"`
	GateIdentity string        `json:"gate_identity"`
	GatePlan     string        `json:"gate_plan"`
}

type planReadBoard struct {
	Host     string            `json:"host"`
	OS       string            `json:"os"`
	Arch     string            `json:"arch"`
	CPU      string            `json:"cpu"`
	Revision string            `json:"revision"`
	Dirty    bool              `json:"dirty"`
	Rounds   int               `json:"rounds"`
	CorpusID string            `json:"corpus_id"`
	Counts   map[string]int64  `json:"iterations_per_wire"`
	Load     string            `json:"load1_range"`
	Noise    []string          `json:"observed_noise"`
	Window   string            `json:"window_verdict"`
	Lane     string            `json:"lane,omitempty"`
	Note     string            `json:"noise_note"`
	Legs     []planReadLeg     `json:"legs"`
	Absent   []skipped         `json:"absent"`
	Runtimes map[string]string `json:"runtimes"`
	Tools    map[string]string `json:"tools"`
}

// windowVerdict is OK only when the sitting's own process journal saw no
// foreign work and every measured row published at normal spread. Anything
// else is NOISY with the reason, and a NOISY window flips no roadmap task.
func windowVerdict(noise []string, legs []planReadLeg) string {
	var why []string
	if len(noise) > 0 {
		why = append(why, fmt.Sprintf("%d foreign-process warnings", len(noise)))
	}
	for _, leg := range legs {
		stats := map[string]planReadStat{"identity": leg.Identity, "packet": leg.Packet}
		if leg.Plan != nil {
			stats["plan"] = *leg.Plan
		}
		for _, name := range slices.Sorted(maps.Keys(stats)) {
			if d := stats[name].disposition(); d != "normal" {
				why = append(why, fmt.Sprintf("%s %s read %s (%.1f%% spread)", leg.Language, name, d, stats[name].Spread))
			}
		}
	}
	if len(why) == 0 {
		return "OK"
	}
	return "NOISY: " + strings.Join(why, "; ")
}

func buildPlanReadLeg(lang string, rounds []map[string]sample) (planReadLeg, error) {
	rates := map[string][]float64{}
	sizes := map[string]float64{}
	for _, round := range rounds {
		for name, row := range round {
			if seen, ok := sizes[name]; ok && seen != row.bytes {
				return planReadLeg{}, fmt.Errorf("%s %s bytes per record changed between rounds", lang, name)
			}
			sizes[name] = row.bytes
			rates[name] = append(rates[name], row.rate)
		}
	}
	for name, values := range rates {
		if len(values) != len(rounds) {
			return planReadLeg{}, fmt.Errorf("%s %s is missing from some rounds", lang, name)
		}
	}
	leg := planReadLeg{Language: lang, Identity: newPlanReadStat(rates["bench_fixed_read"], sizes["bench_fixed_read"]), Packet: newPlanReadStat(rates["bench_mixed_read"], sizes["bench_mixed_read"])}
	leg.GateIdentity = gateVerdict(leg.Identity, leg.Packet)
	if values, ok := rates["bench_plan_read"]; ok {
		plan := newPlanReadStat(values, sizes["bench_plan_read"])
		leg.Plan, leg.GatePlan = &plan, gateVerdict(plan, leg.Packet)
	} else {
		leg.PlanAbsent, leg.GatePlan = planLaneAbsent, "NOT MEASURED"
	}
	return leg, nil
}

func (b planReadBoard) markdown() string {
	var t strings.Builder
	dirty := ""
	if b.Dirty {
		dirty = " (dirty)"
	}
	t.WriteString("# The plan-read board — fixed form vs the same leg's packet wire\n\n```\n")
	t.WriteString("# fixed form: the identity lane, the plan lane, and the same leg's packet wire\n")
	planID := "— (plan lane not measured)"
	for _, leg := range b.Legs {
		if leg.Plan != nil {
			planID = b.CorpusID + " (one invocation: shared with the identity rows, and with no row of the ordinary table pass)"
		}
	}
	lane := "none"
	if b.Lane != "" {
		lane = b.Lane
	}
	fmt.Fprintf(&t, "# host %s / %s/%s %s / rev %s%s / lane %s / rounds %d / corpus_id(identity) %s / corpus_id(plan) %s\n",
		b.Host, b.OS, b.Arch, b.CPU, b.Revision, dirty, lane, b.Rounds, b.CorpusID, planID)
	for _, leg := range b.Legs {
		if leg.Plan != nil {
			fmt.Fprintf(&t, "# bytes_per_op (%s): fixed %.1f B/record (identity), %.1f B/record (plan: the OLDER layout's file), packet %.0f B/record (§5.3 rule 2 — the division is this driver's alone)\n", leg.Language, leg.Identity.Bytes, leg.Plan.Bytes, leg.Packet.Bytes)
			continue
		}
		fmt.Fprintf(&t, "# bytes_per_op (%s): fixed %.1f B/record, packet %.0f B/record (§5.3 rule 2 — the division is this driver's alone)\n", leg.Language, leg.Identity.Bytes, leg.Packet.Bytes)
	}
	fmt.Fprintf(&t, "# iterations per sample: table %s records, packet %s records (one count per wire, §2.1)\n", grouped(b.Counts["table"]), grouped(b.Counts["packet"]))
	t.WriteString("# checks: table=contract, packet C/C++/C#/Rust=removed, packet Go=always (§1.9)\n")
	t.WriteString("# C++ anchor: not in this sitting (the C++ leg has no plan-read arm on this tree)\n")
	fmt.Fprintf(&t, "# window: %s · load1 %s\n", b.Window, b.Load)
	t.WriteString("\nleg      identity read    plan read    packet read    GATE identity  GATE plan     plan/identity\n")
	t.WriteString("         ns/record        ns/record    ns/record      vs packet      vs packet     x (reported)\n")
	for _, lang := range nineRowOrder {
		if leg, ok := legFor(b.Legs, lang); ok {
			plan, ratio := "—", "—"
			if leg.Plan != nil {
				plan, ratio = fmt.Sprintf("%.1f", leg.Plan.BestNs), fmt.Sprintf("%.2f", leg.Plan.BestNs/leg.Identity.BestNs)
			}
			fmt.Fprintf(&t, "%-8s %13.1f %12s %14.1f %16s %-13s %s\n", lang, leg.Identity.BestNs, plan, leg.Packet.BestNs, leg.GateIdentity, leg.GatePlan, ratio)
			continue
		}
		for _, s := range b.Absent {
			if s.Language == lang {
				fmt.Fprintf(&t, "%-8s %13s %12s %14s %16s %-13s %s     %s\n", lang, "—", "—", "—", "—", "—", "—", s.Reason)
			}
		}
	}
	t.WriteString("```\n\nBest of the rounds is the headline (§2.2); median, worst and spread stand beside it:\n\n")
	t.WriteString("| leg | row | best ns/record | median | worst | spread | disposition (§2.3) |\n|---|---|---:|---:|---:|---:|---|\n")
	for _, leg := range b.Legs {
		rows := []struct {
			name string
			s    *planReadStat
		}{{"bench_fixed_read (identity)", &leg.Identity}, {"bench_plan_read (plan)", leg.Plan}, {"bench_mixed_read (packet)", &leg.Packet}}
		for _, row := range rows {
			if row.s == nil {
				fmt.Fprintf(&t, "| %s | %s | — | — | — | — | not measured: %s |\n", leg.Language, row.name, leg.PlanAbsent)
				continue
			}
			fmt.Fprintf(&t, "| %s | %s | %.2f | %.2f | %.2f | %.1f%% | %s |\n", leg.Language, row.name, row.s.BestNs, row.s.MedianNs, row.s.WorstNs, row.s.Spread, row.s.disposition())
		}
	}
	fmt.Fprintf(&t, "\nOperator context: %s\n\nThe GATE columns are the verdict, per leg: a lane not faster (best against best) than the same leg's packet read is FAIL. plan/identity is reported, never judged. A row over 40%% spread enters no verdict (INVALID). Not certified: this is the fast machinery's sitting, not `-mode run`'s seal.\n", b.Note)
	return t.String()
}

func legFor(legs []planReadLeg, lang string) (planReadLeg, bool) {
	for _, leg := range legs {
		if leg.Language == lang {
			return leg, true
		}
	}
	return planReadLeg{}, false
}

// planReadPass is `-mode plan-read`: the no-clock gates, then the requested
// rounds of every leg interleaved, at one count per wire raised uniformly
// until every sample clears 200ms, then the board. It reuses the fast
// diagnostic's collector, so every invocation is bracketed by process samples
// and the evidence directory carries the raw CSV of every attempt.
func planReadPass(langs []string, out string, info buildInfo, config fastConfig, rounds int, lane string) (resultErr error) {
	if rounds < 1 || rounds > 7 {
		return errors.New("plan-read rounds must be 1..7")
	}
	for _, lang := range langs {
		if !contains(planReadLanguages, lang) {
			return fmt.Errorf("%s has no plan-read arm in its runner on this tree (plan-read legs: %s)", lang, strings.Join(planReadLanguages, ","))
		}
		if info.Binaries[binary("plan", lang)] == "" {
			return fmt.Errorf("cached build lacks %s/plan; build it first (-mode build -langs %s)", lang, lang)
		}
	}
	if out == "" {
		return errors.New("plan-read requires -out (a new report directory)")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		return errors.New("refusing to overwrite plan-read directory")
	}
	if info.Dirty || gitValue(".", "status", "--porcelain") != "" {
		return errors.New("plan-read requires a clean source/build checkpoint")
	}
	id, err := fnvCorpusID(planReadCorpusFiles(), os.ReadFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	f := fastCollector{dir: out, evidence: fastEvidence{Status: "INCOMPLETE_PLAN_READ", Started: time.Now().UTC().Format(time.RFC3339Nano), Config: config, Build: info, Languages: langs, Qualification: "Plan-read sitting (BENCH-STANDARD.md §1.10): three measured read rows per leg out of one invocation per round; not a -mode run confirmation."}}
	f.journal, err = os.Create(filepath.Join(out, "process-samples.jsonl"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, f.journal.Close())
		f.evidence.Ended = time.Now().UTC().Format(time.RFC3339Nano)
		if resultErr != nil {
			f.evidence.Reason = resultErr.Error()
		}
		resultErr = errors.Join(resultErr, f.save())
	}()
	if _, err := f.capture(ctx, command{args: []string{"build/paired/corpus", "verify"}}, "corpus-gate"); err != nil {
		return err
	}
	for _, lang := range langs {
		data, err := f.capture(ctx, runner("plan", lang, "--plan-read", "--gate", "--packet-wire-dir", "testdata/wire", "--packet-variant-dir", "bench/corpus/variants"), "gate-"+lang+"-plan-read")
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(data)) != 0 {
			return errors.New("plan-read correctness gate emitted timing rows")
		}
	}
	counts := map[string]int64{"table": config.TableIterations, "packet": config.PacketIterations}
	var collected map[string][]map[string]sample
	for pass := range fastPasses {
		collected = map[string][]map[string]sample{}
		raised := map[string]int64{}
		for round := range rounds {
			for i := range langs {
				lang := langs[(i+round)%len(langs)]
				phase := fmt.Sprintf("round-%d-%s-plan-read-attempt-%d", round, lang, pass)
				fmt.Fprintln(os.Stderr, phase, "iterations table", counts["table"], "packet", counts["packet"])
				data, runErr := f.capture(ctx, planReadCommand(lang, round, counts), phase)
				if err := os.WriteFile(filepath.Join(out, phase+".csv"), data, 0644); err != nil {
					return errors.Join(runErr, err)
				}
				if runErr != nil {
					return runErr
				}
				rows, err := parsePlanReadRows(data, lang, id, counts)
				if err != nil {
					return err
				}
				adequate := true
				a := fastAttempt{Round: round, Language: lang, Wire: "plan-read", Attempt: pass, Iterations: counts["table"], Raw: phase + ".csv"}
				for _, name := range slices.Sorted(maps.Keys(rows)) {
					row := rows[name]
					wire := planReadRows[name].wire
					next, ok, err := fastNextIterations([]sample{row}, counts[wire])
					if err != nil {
						return err
					}
					if !ok {
						adequate, raised[wire] = false, max(raised[wire], next)
					}
					a.Metrics = append(a.Metrics, fastMetric{Path: name, Iterations: row.iters, Rate: row.rate, Seconds: float64(row.iters) / row.rate, Bytes: row.bytes})
				}
				a.Adequate = adequate
				f.evidence.Attempts = append(f.evidence.Attempts, a)
				if err := f.save(); err != nil {
					return err
				}
				collected[lang] = append(collected[lang], rows)
			}
		}
		if len(raised) == 0 {
			break
		}
		if pass == fastPasses-1 {
			return fmt.Errorf("plan-read still has a sample below 200ms after %d uniform attempts", fastPasses)
		}
		maps.Copy(counts, raised)
	}
	f.evidence.FinalCounts = counts
	if err := checkBuildRevision(info.Revision, gitValue(".", "rev-parse", "HEAD")); err != nil {
		return err
	}
	if err := verifyRecordedArtifacts(info); err != nil {
		return err
	}
	// The evidence directory may sit inside the checkout; every OTHER path is
	// source, and a source change mid-sitting voids it, as in fast mode.
	statusArgs := []string{"status", "--porcelain", "--", "."}
	if rel, err := filepath.Rel(abs("."), abs(out)); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		statusArgs = append(statusArgs, ":(exclude,top,literal)"+filepath.ToSlash(rel))
	}
	if gitValue(".", statusArgs...) != "" {
		return errors.New("source changed during plan-read sitting")
	}
	board := planReadBoard{Host: info.Host, OS: info.OS, Arch: info.Arch, CPU: info.CPU, Revision: info.Revision, Dirty: info.Dirty, Rounds: rounds, CorpusID: id, Counts: counts, Load: loadRange(out), Noise: f.evidence.Noise, Note: config.Noise, Lane: lane, Runtimes: info.Runtimes, Tools: info.Tools}
	for _, lang := range langs {
		leg, err := buildPlanReadLeg(lang, collected[lang])
		if err != nil {
			return err
		}
		board.Legs = append(board.Legs, leg)
	}
	for _, lang := range nineLanguages {
		if !contains(langs, lang) {
			reason := "not requested in this sitting"
			if !contains(planReadLanguages, lang) {
				reason = "no plan-read arm in this leg's runner on this tree"
			}
			board.Absent = append(board.Absent, skipped{Language: lang, Reason: reason})
		}
	}
	board.Window = windowVerdict(board.Noise, board.Legs)
	if err := os.WriteFile(filepath.Join(out, "PLAN-READ.md"), []byte(board.markdown()), 0644); err != nil {
		return err
	}
	js, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "plan-read.json"), append(js, '\n'), 0644); err != nil {
		return err
	}
	f.evidence.Status = "COMPLETED_PLAN_READ"
	fmt.Print(board.markdown())
	return nil
}
