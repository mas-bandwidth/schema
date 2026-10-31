package main

// THE PROOF TABLE: the 3.0.0 gate. Glenn, 2026-10-31: "We need to prove this
// before v3.0.0 not just hope that it's correct and complete." The bar: "we
// won't make 3.0.0 until it is correct and at parity."
//
// The table is GENERATED, never kept by hand. Its rows are the conformance
// manifest's features (every harness surface, and every unit's family of
// cases across the surfaces) and the 3.0.0 correctness rows of the porting
// register (docs/PORTING.md); its columns are the languages the harness ran.
// A cell is one of three things:
//
//   - ✅ PROVEN: it names the passing gate (the conformance cases, or the
//     register cell's named test or target), the CI job that runs that gate,
//     and the gate's negative control (make/negative-controls.json, or a
//     named negative-control test the register cites).
//   - N/A BY DESIGN: every case it would hold is answered absent under a
//     ruling in docs/rulings.json, an entry that quotes Glenn verbatim with a
//     date; or a register cell marked `—` that a ruling names, or that CITES
//     the clause exempting it: `"<quote>" (docs/PORTING.md ...)` or
//     `"<quote>" (docs/SPEC-TABLES.md §N)`, where the quote is found in that
//     file (outside the register's own table rows) or the cell is red.
//   - RED: anything else. An absent case with no ruling, a failing case, a
//     register cell that is ❌ or `—` with no ruling, and a gate with no
//     negative control are all red, by name.
//
// The table does not hold a release by being printed: `make release` refuses
// a commit whose proof report is not green at that exact commit
// (VerifyRelease), and ci-full prints the red list on every merge to main.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	conformanceManifestPath = "testdata/conformance/tables/MANIFEST.txt"
	portingPath             = "docs/PORTING.md"
	controlsPath            = "make/negative-controls.json"
	rulingsPath             = "docs/rulings.json"
	proofPath               = "docs/proof.json"
	// referenceLang is the conformance reference leg (test/conformance/harness).
	referenceLang = "cpp"
)

// Surfaces is the harness's surface list, in its order
// (test/conformance/harness/run.go). LoadResults refuses a results file
// whose surfaces differ, so the two cannot drift apart silently.
var Surfaces = []string{"wire", "message", "report", "retain", "retain-save", "json-read", "json-write", "json-hostile",
	"cook", "cook-write", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery",
	"cook-reason", "block-reason"}

// PortingRows are the porting register's 3.0.0 correctness rows. A
// performance-only row is outside the table: "Yes, this is after 3.0.0"
// (ruling perf-after-3.0.0).
var PortingRows = []string{"M5", "M6", "M10", "M11", "M12", "M14", "M17", "M18", "M19", "M20", "M21",
	"I2", "I3", "I4", "I6", "I7", "I11", "I12", "I13", "I15", "J1", "J2", "J3", "J4", "J6"}

// Cell statuses.
const (
	StatusProven = "proven"
	StatusNA     = "n/a"
	StatusRed    = "red"
)

// Cell is one language's answer to one row, with what proves it or why it is red.
type Cell struct {
	Row     string `json:"row"`
	Lang    string `json:"lang"`
	Status  string `json:"status"`
	Gate    string `json:"gate,omitempty"`
	Job     string `json:"job,omitempty"`
	Control string `json:"control,omitempty"`
	Ruling  string `json:"ruling,omitempty"`
	Cites   string `json:"cites,omitempty"`
	Why     string `json:"why,omitempty"`
}

// Proof is the generated table.
type Proof struct {
	Note  string   `json:"note"`
	Langs []string `json:"langs"`
	Rows  []string `json:"rows"`
	Cells []Cell   `json:"cells"`
}

// Red is every red cell, in table order.
func (p Proof) Red() []Cell {
	var out []Cell
	for _, c := range p.Cells {
		if c.Status == StatusRed {
			out = append(out, c)
		}
	}
	return out
}

// Cell finds one cell.
func (p Proof) Cell(row, lang string) (Cell, bool) {
	for _, c := range p.Cells {
		if c.Row == row && c.Lang == lang {
			return c, true
		}
	}
	return Cell{}, false
}

// Results is the harness's --results file (test/conformance/harness/results.go).
type Results struct {
	Langs   []string                            `json:"langs"`
	Results map[string]map[string]SurfaceResult `json:"results"`
}

// SurfaceResult is one leg's answer to one surface.
type SurfaceResult struct {
	Absent   bool              `json:"absent"`
	Failures int               `json:"failures"`
	Cases    map[string]string `json:"cases"`
}

// LoadResults reads one results file or a directory of them (one per leg, as
// the CI legs write them) and merges them. A leg named twice is refused.
func LoadResults(p string) (Results, error) {
	info, err := os.Stat(p)
	if err != nil {
		return Results{}, err
	}
	files := []string{p}
	if info.IsDir() {
		files, err = filepath.Glob(filepath.Join(p, "*.json"))
		if err != nil {
			return Results{}, err
		}
		sort.Strings(files)
	}
	out := Results{Results: map[string]map[string]SurfaceResult{}}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return Results{}, err
		}
		var r Results
		if err := json.Unmarshal(data, &r); err != nil {
			return Results{}, fmt.Errorf("%s: %w", f, err)
		}
		for _, l := range r.Langs {
			if _, dup := out.Results[l]; dup {
				return Results{}, fmt.Errorf("%s: the %s leg is named twice", f, l)
			}
			for s := range r.Results[l] {
				if !slices.Contains(Surfaces, s) {
					return Results{}, fmt.Errorf("%s: surface %q is not one tools/matrix knows; the harness and Surfaces have drifted", f, s)
				}
			}
			out.Langs = append(out.Langs, l)
			out.Results[l] = r.Results[l]
		}
	}
	if len(out.Langs) == 0 {
		return Results{}, fmt.Errorf("%s: no leg's results", p)
	}
	// THE COLUMN ORDER is the harness registry's, however the results came:
	// the reference first, the rest by name. One file from `make conformance`
	// and fifteen per-leg files from CI compute the same table.
	sort.SliceStable(out.Langs, func(i, j int) bool {
		a, b := out.Langs[i], out.Langs[j]
		if (a == referenceLang) != (b == referenceLang) {
			return a == referenceLang
		}
		return a < b
	})
	return out, nil
}

// Ruling is one entry of docs/rulings.json: Glenn's words verbatim, the date,
// and the cells it makes not applicable.
type Ruling struct {
	ID      string     `json:"id"`
	Date    string     `json:"date"`
	Quotes  []string   `json:"quotes"`
	Source  string     `json:"source"`
	Earlier []Earlier  `json:"earlier,omitempty"`
	Applies []Selector `json:"applies,omitempty"`
}

// Earlier is a prior ruling on the same question, cited by commit.
type Earlier struct {
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// Selector names the cells a ruling covers: conformance cases (surface and a
// case-name pattern, path.Match syntax) or a porting-register row; Langs
// empty means every language.
type Selector struct {
	Surface string   `json:"surface,omitempty"`
	Case    string   `json:"case,omitempty"`
	Row     string   `json:"row,omitempty"`
	Langs   []string `json:"langs,omitempty"`
}

// Rulings is docs/rulings.json.
type Rulings struct {
	Note    string   `json:"note"`
	Rulings []Ruling `json:"rulings"`
}

// LoadRulings reads the rulings file and refuses an entry with no id, no date,
// no verbatim quote or a duplicate id.
func LoadRulings(root string) (Rulings, error) {
	data, err := os.ReadFile(filepath.Join(root, rulingsPath))
	if err != nil {
		return Rulings{}, err
	}
	var r Rulings
	if err := json.Unmarshal(data, &r); err != nil {
		return Rulings{}, fmt.Errorf("%s: %w", rulingsPath, err)
	}
	seen := map[string]bool{}
	date := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	for _, x := range r.Rulings {
		switch {
		case x.ID == "":
			return Rulings{}, fmt.Errorf("%s: a ruling has no id", rulingsPath)
		case seen[x.ID]:
			return Rulings{}, fmt.Errorf("%s: two rulings are named %q", rulingsPath, x.ID)
		case !date.MatchString(x.Date):
			return Rulings{}, fmt.Errorf("%s: ruling %s has no date (YYYY-MM-DD)", rulingsPath, x.ID)
		case len(x.Quotes) == 0:
			return Rulings{}, fmt.Errorf("%s: ruling %s quotes nothing; a ruling is Glenn's words verbatim", rulingsPath, x.ID)
		case x.Source == "":
			return Rulings{}, fmt.Errorf("%s: ruling %s names no source", rulingsPath, x.ID)
		}
		seen[x.ID] = true
	}
	return r, nil
}

func (r Rulings) caseRuling(surface, name, lang string) string {
	for _, x := range r.Rulings {
		for _, s := range x.Applies {
			if s.Surface == "" || s.Surface != surface || !langIn(s.Langs, lang) {
				continue
			}
			if ok, _ := path.Match(s.Case, name); ok {
				return x.ID
			}
		}
	}
	return ""
}

func (r Rulings) rowRuling(row, lang string) string {
	for _, x := range r.Rulings {
		for _, s := range x.Applies {
			if s.Row == row && langIn(s.Langs, lang) {
				return x.ID
			}
		}
	}
	return ""
}

func langIn(langs []string, lang string) bool {
	return len(langs) == 0 || slices.Contains(langs, lang)
}

// Controls is make/negative-controls.json reduced to what a cell cites: each
// control target and the CI job that runs it.
type Controls map[string]string

// LoadControls reads make/negative-controls.json.
func LoadControls(root string) (Controls, error) {
	data, err := os.ReadFile(filepath.Join(root, controlsPath))
	if err != nil {
		return nil, err
	}
	var m struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", controlsPath, err)
	}
	out := Controls{}
	for _, g := range m.Groups {
		job := "CI full / negative controls (" + g.Name + ")"
		if g.When == "nightly" {
			job = "Certify / negative controls, nightly (" + g.Name + ")"
		}
		for _, t := range g.Targets {
			out[t] = job
		}
	}
	return out, nil
}

// legControl is the conformance negative control that sabotages lang's own
// leg: conformance-negative-control-<lang> or -<lang>-<what>, and for the
// reference the bare control that breaks a C++ dump.
func (c Controls) legControl(lang string) (string, string) {
	var names []string
	for t := range c {
		if t == "conformance-negative-control-"+lang || strings.HasPrefix(t, "conformance-negative-control-"+lang+"-") {
			names = append(names, t)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", ""
	}
	return names[0], c[names[0]]
}

// CaseUnits maps each conformance case to its unit, from the manifest: a
// case line's name is its second field and its unit the first later field
// that names a declared unit. Units come back in manifest order.
func CaseUnits(root string) (map[string]string, []string, error) {
	f, err := os.Open(filepath.Join(root, conformanceManifestPath))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	units := map[string]bool{}
	var order []string
	var lines [][]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) < 2 || strings.HasPrefix(w[0], "#") {
			continue
		}
		if w[0] == "unit" {
			if !units[w[1]] {
				units[w[1]] = true
				order = append(order, w[1])
			}
			continue
		}
		lines = append(lines, w)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	for _, w := range lines {
		for _, f := range w[2:] {
			if units[f] {
				out[w[1]] = f
				break
			}
		}
	}
	return out, order, nil
}

// unitOf is the unit a results case belongs to. The json-write surface's
// answers are the instance name with .json.
func unitOf(units map[string]string, name string) string {
	if u, ok := units[name]; ok {
		return u
	}
	if u, ok := units[strings.TrimSuffix(name, ".json")]; ok {
		return u
	}
	return ""
}

// PortingCells reads the register: for each wanted row, its first cell table's
// cells by column. A row whose section or table is missing is an error, so a
// renamed technique cannot drop out of the gate.
func PortingCells(root string, rows []string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(root, portingPath))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	out := map[string]map[string]string{}
	for _, id := range rows {
		start := -1
		for i, l := range lines {
			if strings.HasPrefix(l, "### "+id+" ") {
				start = i
				break
			}
		}
		if start < 0 {
			return nil, fmt.Errorf("%s: no section ### %s", portingPath, id)
		}
		var header []string
		var control strings.Builder
		for i := start + 1; i < len(lines); i++ {
			l := lines[i]
			if strings.HasPrefix(l, "### ") || strings.HasPrefix(l, "## ") {
				break
			}
			// the section's **Negative control.** paragraph names the
			// REFERENCE column's controls (the table's first column)
			if strings.HasPrefix(l, "**Negative control.**") {
				for j := i; j < len(lines) && strings.TrimSpace(lines[j]) != ""; j++ {
					control.WriteString(lines[j])
					control.WriteString(" ")
				}
			}
			if !strings.HasPrefix(l, "|") {
				continue
			}
			if header == nil {
				header = splitTableRow(l)
				i++ // the alignment row
				continue
			}
			cells := splitTableRow(l)
			row := map[string]string{}
			for j, h := range header {
				if j < len(cells) {
					row[strings.TrimSpace(h)] = strings.TrimSpace(cells[j])
				}
			}
			if control.Len() > 0 {
				row[referenceControlKey] = control.String()
				row[referenceColumnKey] = strings.TrimSpace(header[0])
			}
			out[id] = row
			break
		}
		if out[id] == nil {
			return nil, fmt.Errorf("%s: section %s has no cell table", portingPath, id)
		}
	}
	return out, nil
}

// Keys PortingCells stores beside the language columns: the section's
// Negative control paragraph and the column it speaks for.
const (
	referenceControlKey = "@negative-control"
	referenceColumnKey  = "@reference"
)

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return strings.Split(line, "|")
}

// Inputs is everything the table is computed from.
type Inputs struct {
	Results   Results
	CaseUnits map[string]string
	Units     []string
	Porting   map[string]map[string]string
	Controls  Controls
	Rulings   Rulings
	// Docs is the text a `—` cell may cite, by path: docs/PORTING.md with
	// its table rows taken out (a cell cannot cite itself or its neighbors)
	// and docs/SPEC-TABLES.md, each with its whitespace collapsed.
	Docs map[string]string
}

// LoadInputs reads every input but the results from the tree at root.
func LoadInputs(root string, results Results) (Inputs, error) {
	in := Inputs{Results: results}
	var err error
	if in.CaseUnits, in.Units, err = CaseUnits(root); err != nil {
		return in, err
	}
	if in.Porting, err = PortingCells(root, PortingRows); err != nil {
		return in, err
	}
	if in.Controls, err = LoadControls(root); err != nil {
		return in, err
	}
	if in.Rulings, err = LoadRulings(root); err != nil {
		return in, err
	}
	if in.Docs, err = LoadCitable(root); err != nil {
		return in, err
	}
	return in, nil
}

// citablePaths are the files a `—` cell may cite for its exemption.
var citablePaths = []string{portingPath, "docs/SPEC-TABLES.md"}

// citation is a `—` cell's citation: a quote in double quotes, then the file
// it is in, in parentheses, with any section after the path.
var citation = regexp.MustCompile(`"([^"]+)"\s*\((docs/(?:PORTING|SPEC-TABLES)\.md)\b[^)]*\)`)

// minCitation is the shortest quote that counts: a clause, never a word.
const minCitation = 20

// LoadCitable reads the citable files, PORTING.md without its table rows.
func LoadCitable(root string) (map[string]string, error) {
	docs := map[string]string{}
	for _, p := range citablePaths {
		data, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			return nil, err
		}
		text := string(data)
		if p == portingPath {
			var keep []string
			for l := range strings.SplitSeq(text, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(l), "|") {
					keep = append(keep, l)
				}
			}
			text = strings.Join(keep, "\n")
		}
		docs[p] = collapseSpace(text)
	}
	return docs, nil
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// citedExemption checks a `—` cell's citations. It returns the first one
// found in its file, or why the cell's citation does not hold; both are empty
// when the cell cites nothing.
func citedExemption(docs map[string]string, text string) (cites, bad string) {
	for _, m := range citation.FindAllStringSubmatch(text, -1) {
		quote, path := collapseSpace(m[1]), m[2]
		if len([]rune(quote)) < minCitation {
			bad = fmt.Sprintf("cites %q in %s, shorter than a clause (%d characters)", quote, path, minCitation)
			continue
		}
		if !strings.Contains(docs[path], quote) {
			bad = fmt.Sprintf("cites %q, which %s does not carry", quote, path)
			continue
		}
		return fmt.Sprintf("%s: %q", path, quote), ""
	}
	return "", bad
}

const proofNote = "Generated by `go run ./tools/matrix proof -write` from the conformance harness's per-case results, docs/PORTING.md, make/negative-controls.json and docs/rulings.json. Do not edit by hand."

// Compute builds the table. The columns are the legs the results carry, in
// their order; rows are the surfaces, the unit families and the register rows.
func Compute(in Inputs) Proof {
	p := Proof{Note: proofNote, Langs: append([]string{}, in.Results.Langs...)}
	for _, s := range Surfaces {
		p.Rows = append(p.Rows, "surface "+s)
	}
	// a unit no case names (a writer-side unit an evolution case reads from)
	// is not a family of cases, so it is not a row
	named := map[string]bool{}
	for _, l := range in.Results.Langs {
		for _, r := range in.Results.Results[l] {
			for cs := range r.Cases {
				if u := unitOf(in.CaseUnits, cs); u != "" {
					named[u] = true
				}
			}
		}
	}
	for _, u := range in.Units {
		if named[u] {
			p.Rows = append(p.Rows, "cases "+u)
		}
	}
	for _, r := range PortingRows {
		p.Rows = append(p.Rows, "porting "+r)
	}
	for _, row := range p.Rows {
		for _, lang := range p.Langs {
			p.Cells = append(p.Cells, computeCell(in, row, lang))
		}
	}
	return p
}

func computeCell(in Inputs, row, lang string) Cell {
	kind, name, _ := strings.Cut(row, " ")
	if kind == "porting" {
		return portingCell(in, row, name, lang)
	}
	c := Cell{Row: row, Lang: lang}
	legResults := in.Results.Results[lang]
	type answer struct{ surface, name, verdict string }
	var answers []answer
	for _, s := range Surfaces {
		if kind == "surface" && s != name {
			continue
		}
		// THE CASES OWED are every case any leg answered on this surface, so a
		// leg whose results lack one (an answer deleted, a case dropped) is
		// red on it by name rather than green on fewer cases.
		for _, cs := range owedCases(in.Results, s) {
			if kind == "cases" && unitOf(in.CaseUnits, cs) != name {
				continue
			}
			v, ok := legResults[s].Cases[cs]
			if !ok {
				v = "missing"
			}
			answers = append(answers, answer{s, cs, v})
		}
	}
	sort.Slice(answers, func(i, j int) bool {
		if answers[i].surface != answers[j].surface {
			return answers[i].surface < answers[j].surface
		}
		return answers[i].name < answers[j].name
	})
	if len(answers) == 0 {
		c.Status, c.Why = StatusRed, "no case of this row was answered: the leg ran nothing here"
		return c
	}
	var passed, ruled []answer
	var absent, failed, missing []string
	rulings := map[string]bool{}
	for _, a := range answers {
		switch a.verdict {
		case "pass", "named":
			passed = append(passed, a)
		case "absent":
			if id := in.Rulings.caseRuling(a.surface, a.name, lang); id != "" {
				ruled = append(ruled, a)
				rulings[id] = true
				continue
			}
			absent = append(absent, a.surface+"/"+a.name)
		case "missing":
			missing = append(missing, a.surface+"/"+a.name)
		default:
			failed = append(failed, a.surface+"/"+a.name)
		}
	}
	c.Ruling = strings.Join(sortedKeys(rulings), ",")
	switch {
	case len(missing) > 0:
		c.Status, c.Why = StatusRed, fmt.Sprintf("%d case(s) have no answer at all in the leg's results: %s", len(missing), firstFew(missing))
		return c
	case len(failed) > 0:
		c.Status, c.Why = StatusRed, fmt.Sprintf("%d case(s) fail: %s", len(failed), firstFew(failed))
		return c
	case len(absent) > 0:
		c.Status, c.Why = StatusRed, fmt.Sprintf("%d case(s) answered absent with no ruling: %s", len(absent), firstFew(absent))
		return c
	case len(passed) == 0:
		c.Status, c.Why = StatusNA, fmt.Sprintf("every case answered absent under ruling %s", c.Ruling)
		return c
	}
	control, job := in.Controls.legControl(lang)
	if control == "" {
		c.Status, c.Why = StatusRed, fmt.Sprintf("the %s conformance leg has no negative control (no conformance-negative-control-%s in %s)", lang, lang, controlsPath)
		return c
	}
	c.Status = StatusProven
	c.Gate = fmt.Sprintf("make conformance: %d case(s) pass, e.g. %s/%s", len(passed), passed[0].surface, passed[0].name)
	c.Job = "CI full / conformance (" + lang + ")"
	c.Control = control + " (" + job + ")"
	return c
}

var backticked = regexp.MustCompile("`([^`]+)`")

// isGateToken is a register token that names a gate: a Go test or a make
// target, not a source path.
func isGateToken(t string) bool {
	if strings.ContainsAny(t, " /:.(){}<>=,") {
		return false
	}
	return strings.HasPrefix(t, "Test") || strings.Contains(t, "-")
}

var controlTest = regexp.MustCompile(`NegativeControl|GoesRed|Sabotage`)

func portingCell(in Inputs, row, id, lang string) Cell {
	c := Cell{Row: row, Lang: lang}
	text, ok := in.Porting[id][lang]
	if !ok {
		c.Status, c.Why = StatusRed, fmt.Sprintf("%s has no %s column", id, lang)
		return c
	}
	switch {
	case strings.HasPrefix(text, "✅"):
		var gates, controls []string
		for _, m := range backticked.FindAllStringSubmatch(text, -1) {
			t := m[1]
			if !isGateToken(t) {
				continue
			}
			if _, ok := in.Controls[t]; ok || controlTest.MatchString(t) || strings.Contains(t, "negative-control") {
				controls = append(controls, t)
				continue
			}
			gates = append(gates, t)
		}
		if in.Porting[id][referenceColumnKey] == lang {
			for _, m := range backticked.FindAllStringSubmatch(in.Porting[id][referenceControlKey], -1) {
				if isGateToken(m[1]) {
					controls = append(controls, m[1])
				}
			}
		}
		controls = append(controls, in.Controls.controlsFor(gates)...)
		if len(gates) == 0 && len(controls) > 0 {
			// a cell that names only its control names the gate the control
			// breaks by the control's own name
			gates = controls[:1]
		}
		if len(gates) == 0 {
			c.Status, c.Why = StatusRed, "✅ but the cell names no gate (a test or a make target), only paths"
			return c
		}
		if len(controls) == 0 {
			c.Status, c.Why = StatusRed, fmt.Sprintf("✅ but its gate %s has no negative control", gates[0])
			c.Gate = gates[0]
			return c
		}
		c.Status = StatusProven
		c.Gate = gates[0]
		c.Job = "Certify / test (make test): compiler/porting_test.go holds every named gate to be run by the tree"
		if job, ok := in.Controls[controls[0]]; ok {
			c.Control = controls[0] + " (" + job + ")"
		} else {
			c.Control = controls[0] + " (Certify / test (make test))"
		}
		return c
	case strings.HasPrefix(text, "—"):
		if r := in.Rulings.rowRuling(id, lang); r != "" {
			c.Status, c.Ruling = StatusNA, r
			c.Why = "not applicable by design"
			return c
		}
		cites, bad := citedExemption(in.Docs, text)
		if cites != "" {
			c.Status, c.Cites = StatusNA, cites
			c.Why = "not applicable by design, by the clause it cites"
			return c
		}
		if bad != "" {
			c.Status, c.Why = StatusRed, "— "+bad
			return c
		}
		c.Status, c.Why = StatusRed, "— (the platform cannot) with no ruling in "+rulingsPath+" and no cited clause of "+strings.Join(citablePaths, " or ")+": "+excerpt(strings.TrimSpace(strings.TrimPrefix(text, "—")))
		return c
	case strings.HasPrefix(text, "❌"):
		c.Status, c.Why = StatusRed, "❌ "+excerpt(strings.TrimSpace(strings.TrimPrefix(text, "❌")))
		return c
	}
	c.Status, c.Why = StatusRed, "the cell carries none of ✅ ❌ —: "+excerpt(text)
	return c
}

func excerpt(s string) string {
	r := []rune(s)
	if len(r) > 140 {
		return string(r[:140]) + "…"
	}
	return s
}

func firstFew(xs []string) string {
	if len(xs) > 3 {
		return strings.Join(xs[:3], ", ") + fmt.Sprintf(" and %d more", len(xs)-3)
	}
	return strings.Join(xs, ", ")
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LoadProof reads the committed snapshot, docs/proof.json. A tree with no
// snapshot renders no proof section.
func LoadProof(root string) (*Proof, error) {
	data, err := os.ReadFile(filepath.Join(root, proofPath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Proof
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", proofPath, err)
	}
	return &p, nil
}

// MarshalProof is the snapshot's bytes: indented, with a trailing newline.
func MarshalProof(p Proof) []byte {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

// RenderProof is the page's proof section.
func RenderProof(p Proof) string {
	var b strings.Builder
	b.WriteString("\n## Proof: the 3.0.0 gate\n\n")
	b.WriteString("Generated from the conformance harness's per-case results, the porting register's 3.0.0 correctness rows, `make/negative-controls.json` and `docs/rulings.json` (`go run ./tools/matrix proof -write`); `docs/proof.json` carries every cell's gate, CI job and negative control, or why it is red. ✅ proven: a passing gate, the CI job that runs it, and its negative control. **n/a**: not applicable by design, under a ruling that quotes Glenn verbatim, or, for a register cell marked `—`, by the clause of `docs/PORTING.md` or `docs/SPEC-TABLES.md` it quotes, found in that file. ❌ red. `make release` refuses a commit whose proof is not green at that commit.\n\n")
	red := len(p.Red())
	fmt.Fprintf(&b, "%d of %d cells red.\n\n", red, len(p.Cells))
	b.WriteString("| row")
	for _, l := range p.Langs {
		b.WriteString(" | " + l)
	}
	b.WriteString(" |\n|---|")
	for range p.Langs {
		b.WriteString(":---:|")
	}
	b.WriteString("\n")
	for _, row := range p.Rows {
		b.WriteString("| " + row)
		for _, l := range p.Langs {
			c, _ := p.Cell(row, l)
			switch c.Status {
			case StatusProven:
				b.WriteString(" | ✅")
			case StatusNA:
				b.WriteString(" | n/a")
			default:
				b.WriteString(" | ❌")
			}
		}
		b.WriteString(" |\n")
	}
	return b.String()
}

// RedList prints the red cells, one per line: the checklist parity owes.
func RedList(p Proof) string {
	var b strings.Builder
	for _, c := range p.Red() {
		fmt.Fprintf(&b, "%s / %s: %s\n", c.Row, c.Lang, c.Why)
	}
	return b.String()
}

// Report is what the proof job hands the release: the commit it ran at and
// the red cells by name.
type Report struct {
	Commit string `json:"commit"`
	Cells  int    `json:"cells"`
	Red    []Cell `json:"red"`
	Stale  string `json:"stale,omitempty"`
}

// VerifyRelease is the release lock: a release of commit refuses unless the
// proof report was made at that exact commit, found the committed snapshot in
// step, and holds no red cell. A refusal names the red cells.
func VerifyRelease(r Report, commit string) error {
	if commit == "" || r.Commit == "" {
		return fmt.Errorf("release REFUSES: the proof report names no commit, or no commit is being released")
	}
	if r.Commit != commit {
		return fmt.Errorf("release REFUSES: the proof report is for %s, not the commit being released, %s; the proof must be green at that exact commit", r.Commit, commit)
	}
	if r.Cells == 0 {
		return fmt.Errorf("release REFUSES: the proof report at %s holds no cells", commit)
	}
	if r.Stale != "" {
		return fmt.Errorf("release REFUSES: at %s, %s", commit, r.Stale)
	}
	if len(r.Red) > 0 {
		var names []string
		for _, c := range r.Red {
			names = append(names, c.Row+" / "+c.Lang)
		}
		return fmt.Errorf("release REFUSES: the proof at %s has %d red cell(s) of %d: %s", commit, len(r.Red), r.Cells, firstFew(names))
	}
	return nil
}

// LoadReport reads a proof report.
func LoadReport(p string) (Report, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return Report{}, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, fmt.Errorf("%s: %w", p, err)
	}
	return r, nil
}

// owedCases is every case name any leg answered on surface s, sorted.
func owedCases(r Results, s string) []string {
	set := map[string]bool{}
	for _, l := range r.Langs {
		for cs := range r.Results[l][s].Cases {
			set[cs] = true
		}
	}
	return sortedKeys(set)
}

// controlsFor is every control target make/negative-controls.json holds for
// the named gates by the tree's naming: <stem>-negative-control(s) where the
// gate is the stem or the stem with a suffix (tables-keyed-none-refusal-ndebug
// is broken by tables-keyed-none-refusal-negative-control). A stem of fewer
// than three words is too general to claim a gate.
func (c Controls) controlsFor(gates []string) []string {
	var out []string
	for t := range c {
		stem := strings.TrimSuffix(strings.TrimSuffix(t, "-negative-controls"), "-negative-control")
		if stem == t || strings.Count(stem, "-") < 2 {
			continue
		}
		for _, g := range gates {
			if g == stem || strings.HasPrefix(g, stem+"-") {
				out = append(out, t)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
