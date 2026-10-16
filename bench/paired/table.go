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
	"path/filepath"
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

// The row names a plan-read invocation may emit, with the family and the
// wire each is judged as. bench_plan_read is optional: a leg whose lineage
// corpus has not landed names that lane and emits no row for it.
var planReadRows = map[string]struct{ family, wire string }{
	"bench_fixed_read": {"table", "table"},
	"bench_plan_read":  {"table", "table"},
	"bench_mixed_read": {"gen", "packet"},
}

// planLaneAbsent is the reason the plan lane is named rather than measured
// while the lineage corpus is not on the tree.
const planLaneAbsent = "no lineage corpus on this tree (bench/corpus/VOLD_bench_lineage.schema, bench/corpus/schema.lock, bench/paired/corpus/bench_lineage.{bin,layout}: BENCH-STANDARD.md §1.10 work list items 1-6)"

// planReadCorpusFiles is what the plan-read invocation loads: the table
// pass's five files and the packet pass's two (and, when it lands, the
// lineage pair). Its corpus id is therefore its own by §1.6's definition, and
// its rows divide only against each other.
func planReadCorpusFiles() map[string]string {
	paths := map[string]string{
		"bench_mixed.bin":          "testdata/wire/bench_mixed.bin",
		"bench_mixed.variants.bin": "bench/corpus/variants/bench_mixed.variants.bin",
	}
	for _, name := range []string{"bench_table.bin", "bench_table.lengths", "bench_table.variants.bin", "bench_fixed.bin", "bench_fixed.layout"} {
		paths[name] = "bench/paired/corpus/" + name
	}
	return paths
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
	return runner("table", lang, "--plan-read", "--csv", "--round", strconv.Itoa(round),
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
	for _, need := range []string{"bench_fixed_read", "bench_mixed_read"} {
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
	fmt.Fprintf(&t, "# host %s / %s/%s %s / rev %s%s / lane none / rounds %d / corpus_id(identity) %s / corpus_id(plan) %s\n",
		b.Host, b.OS, b.Arch, b.CPU, b.Revision, dirty, b.Rounds, b.CorpusID, "— (plan lane not measured)")
	for _, leg := range b.Legs {
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
func planReadPass(langs []string, out string, info buildInfo, config fastConfig, rounds int) (resultErr error) {
	if rounds < 1 || rounds > 7 {
		return errors.New("plan-read rounds must be 1..7")
	}
	for _, lang := range langs {
		if !contains(planReadLanguages, lang) {
			return fmt.Errorf("%s has no plan-read arm in its runner on this tree (plan-read legs: %s)", lang, strings.Join(planReadLanguages, ","))
		}
		if info.Binaries[binary("table", lang)] == "" {
			return fmt.Errorf("cached build lacks %s/table; build it first (-mode build -langs %s)", lang, lang)
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
		data, err := f.capture(ctx, runner("table", lang, "--plan-read", "--gate", "--packet-wire-dir", "testdata/wire", "--packet-variant-dir", "bench/corpus/variants"), "gate-"+lang+"-plan-read")
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
		for wire, n := range raised {
			counts[wire] = n
		}
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
	board := planReadBoard{Host: info.Host, OS: info.OS, Arch: info.Arch, CPU: info.CPU, Revision: info.Revision, Dirty: info.Dirty, Rounds: rounds, CorpusID: id, Counts: counts, Load: loadRange(out), Noise: f.evidence.Noise, Note: config.Noise, Runtimes: info.Runtimes, Tools: info.Tools}
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
