// What a port CLAIMS, and the coverage table the nightly publishes
// (test/conformance/README.md, "The nightly"; docs/SPEC-TABLES.md §2.3, §2.8,
// §2.11-§2.14, §4.3, §16.10).
//
// THE CLAIM IS THE COMPILER'S, NOT THIS FILE'S. Each target registers the
// constructs it carries once, in compiler/carries_<target>.go, and
// [compiler.Generate] refuses a unit that uses a construct its target does not
// carry. So a port claims a case exactly when the case's unit uses at least one
// registered construct and the port's target carries every one of them: the
// registration is the port saying, by name, that it carries what this case
// exercises, and the leg owes the answer. A case whose unit uses no registered
// construct is one the registry makes no statement about — the variable,
// message, wide-scalar and byte-buffer classes a port lands one at a time are
// not constructs there — so its absence is coverage, not failure. A class that
// should be enforced is registered as a construct; this file does not invent
// claims the registry does not make.
//
// THE ONE PIECE OF MAPPING DATA is which unit (and root) each case of each
// surface reads, and that is the manifest's own lines: caseIndex reads it out
// of them, in one place, and claims_test.go holds it to the committed corpus.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// caseInfo is what the claim and the coverage table need to know about one
// case of one surface: the registered constructs its unit uses, and its CLASS
// — "fixed" or "variable" for a table root (docs/SPEC-TABLES.md §2.2), or ""
// where a surface's own name already says the form (a block, a forgery).
type caseInfo struct {
	uses  []string
	class string
}

// claimSet is the nightly's claim: per surface and case file, what the case
// uses, and per language, what its target carries.
type claimSet struct {
	cases   map[string]map[string]caseInfo // surface -> expectation name -> info
	carried map[string]map[string]bool     // lang -> construct -> carried
}

// claims answers whether lang claims one case of one surface, and the
// constructs the claim rests on.
func (c *claimSet) claims(lang, surface, name string) (bool, []string) {
	info := c.cases[surface][name]
	if len(info.uses) == 0 {
		return false, nil
	}
	carried, ok := c.carried[lang]
	if !ok {
		return false, nil
	}
	for _, construct := range info.uses {
		if !carried[construct] {
			return false, nil
		}
	}
	return true, info.uses
}

// enforce turns every CLAIMED case lang answered ABSENT into a failure. It
// reads the outcomes scoreAnswer recorded, so it runs only over a surface the
// leg ran: a surface left out of `list` or exited 2 on is a missing surface,
// which the coverage table names, and not a claim about one case.
func (c *claimSet) enforce(lang, surface string, want []expectation, r *result) {
	if lang == referenceLang {
		return // the reference may not be absent at all (run.go), claim or none
	}
	for _, e := range want {
		if r.outcome[e.name] != outcomeAbsent {
			continue
		}
		if claimed, uses := c.claims(lang, surface, e.name); claimed {
			r.failures = append(r.failures, fmt.Sprintf(
				"%s: CLAIMED and answered ABSENT — the case's unit uses %s, which the %s target registers as carried "+
					"(compiler/carries_%s.go), so the leg owes the answer (test/conformance/README.md, \"The nightly\")",
				e.name, strings.Join(uses, ", "), lang, lang))
			r.mark(e.name, outcomeClaimedAbsent)
		}
	}
}

// outcomeClaimedAbsent is a case the leg claims and answered ABSENT: a failure,
// counted apart from a wrong answer so the coverage table can say which.
const outcomeClaimedAbsent = "claimed-absent"

// buildClaims reads the claim for every leg in langs: the corpus's cases from
// the manifest, each case's unit through the compiler, and each leg's carried
// constructs from the registry. A leg whose target has no registration in this
// build is an error rather than a leg that claims nothing: a target behind a
// build tag (schema_leg_luau and its siblings) registers only in a harness
// built with that tag, and a nightly that silently enforced nothing for it
// would be green over a check that never ran.
func buildClaims(m *Manifest, langs []string) (*claimSet, error) {
	cases, err := caseIndex(m, newUnits(m))
	if err != nil {
		return nil, err
	}
	set := &claimSet{cases: cases, carried: map[string]map[string]bool{}}
	for _, lang := range langs {
		constructs, ok := compiler.Carried(lang)
		if !ok {
			return nil, fmt.Errorf("the %s leg's target has no carries registration in this build of the harness — "+
				"build it with -tags schema_leg_%s (compiler/carries_%s.go), or the claim check would enforce nothing for it",
				lang, lang, lang)
		}
		set.carried[lang] = map[string]bool{}
		for _, construct := range constructs {
			set.carried[lang][construct] = true
		}
	}
	return set, nil
}

// unitSource loads a unit by its manifest key; generate.go's units is the one
// in use, and a test hands a table instead.
type unitSource interface {
	get(key string) (*ir.Unit, error)
}

// caseIndex reads, out of the manifest's own lines, which unit and root every
// case of every surface reads, and from the unit the constructs it uses and the
// root's class. A forgery or a refusal names a FIXTURE rather than a unit, so
// those four surfaces carry no entry: no construct reaches them, and their form
// is the surface's name.
func caseIndex(m *Manifest, units unitSource) (map[string]map[string]caseInfo, error) {
	out := map[string]map[string]caseInfo{}
	add := func(surface, name, unit, root string) error {
		u, err := units.get(unit)
		if err != nil {
			return fmt.Errorf("%s / %s: %w", surface, name, err)
		}
		info := caseInfo{uses: compiler.UnitConstructs(u)}
		if root != "" {
			info.class = "fixed"
			if ir.VariableTables(u)[root] {
				info.class = "variable"
			}
		}
		if out[surface] == nil {
			out[surface] = map[string]caseInfo{}
		}
		out[surface][name] = info
		return nil
	}
	for _, inst := range m.Instances {
		if err := add("wire", inst.Name, inst.Unit, inst.Root); err != nil {
			return nil, err
		}
		if inst.NoText {
			continue // no text either way (§16.7), so no case on the text surfaces
		}
		if err := add("json-read", inst.Name, inst.Unit, inst.Root); err != nil {
			return nil, err
		}
		if err := add("json-write", inst.Name+".json", inst.Unit, inst.Root); err != nil {
			return nil, err
		}
	}
	for _, msg := range m.Messages {
		conn, err := m.LookupConnection(msg.Connection)
		if err != nil {
			return nil, err
		}
		if err := add("message", msg.Name, conn.Unit, msg.Root); err != nil {
			return nil, err
		}
	}
	for _, r := range m.Reports {
		if err := add("report", r.Name, r.Unit, r.Root); err != nil {
			return nil, err
		}
	}
	for _, rc := range m.Retains {
		unit := rc.Unit
		if rc.Message {
			conn, err := m.LookupConnection(rc.Connection)
			if err != nil {
				return nil, err
			}
			unit = conn.Unit
		}
		if err := add("retain", rc.Name, unit, rc.Root); err != nil {
			return nil, err
		}
		if len(rc.Saves) > 0 {
			if err := add("retain-save", rc.Name, unit, rc.Root); err != nil {
				return nil, err
			}
		}
	}
	for _, h := range m.Hostiles {
		if err := add("json-hostile", h.Name, h.Unit, h.Root); err != nil {
			return nil, err
		}
	}
	for _, c := range m.Cooks {
		for _, s := range []string{"cook", "cook-foreign"} {
			if err := add(s, c.Case, c.Unit, c.Root); err != nil {
				return nil, err
			}
		}
	}
	for _, cw := range m.CookWrites {
		inst, err := m.LookupInstance(cw.Instance)
		if err != nil {
			return nil, err
		}
		for _, name := range []string{cw.Instance, cw.Instance + "-be"} {
			if err := add("cook-write", name, inst.Unit, inst.Root); err != nil {
				return nil, err
			}
		}
	}
	for _, b := range m.Blocks {
		for _, s := range []string{"block", "block-foreign", "block-dump"} {
			if err := add(s, b.Name, b.Unit, ""); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// coverageRow is one line of the nightly's coverage table: one leg, one
// surface, one class of case. Direction says which way the bytes went —
// "reads-cpp" is a leg answering the C++ corpus (`harness run`), "cpp-reads"
// is the C++ reference loading what the leg wrote (`harness cross-read`).
type coverageRow struct {
	Lang      string `json:"lang"`
	Direction string `json:"direction"`
	Surface   string `json:"surface"`
	Class     string `json:"class,omitempty"`
	Cases     int    `json:"cases"`
	// Answered is the cases that passed: the leg's bytes are the corpus's
	// (reads-cpp), or C++ accepted them and re-saved them byte-identical and
	// they equal C++'s own canonical write (cpp-reads).
	Answered int `json:"answered"`
	Failed   int `json:"failed"`
	// AbsentClaimed is the cases the leg CLAIMS and answered ABSENT: failures.
	AbsentClaimed int `json:"absent_claimed"`
	// NotCarried is the cases the leg answered ABSENT and does not claim: the
	// work left, visible and not red.
	NotCarried int `json:"not_carried"`
	// Named is the cases answered with the row's named writer refusal (§2.5).
	Named int `json:"named"`
	// SurfaceAbsent is a leg that does not run the surface at all.
	SurfaceAbsent bool `json:"surface_absent,omitempty"`
}

const (
	directionReadsCpp = "reads-cpp"
	directionCppReads = "cpp-reads"
)

// coverageRows turns one run's results into the table's rows: one per leg,
// surface and class, in the matrix's order.
func coverageRows(langs []string, results map[string]map[string]*result, want map[string][]expectation, m *Manifest, claims *claimSet) []coverageRow {
	var out []coverageRow
	for _, lang := range langs {
		for _, s := range surfaces {
			r := results[lang][s]
			if r == nil {
				continue
			}
			names := want[s]
			if s == "json-hostile" {
				names, _ = hostileExpectations(m, lang)
			}
			byClass := map[string]*coverageRow{}
			var order []string
			row := func(class string) *coverageRow {
				if byClass[class] == nil {
					byClass[class] = &coverageRow{Lang: lang, Direction: directionReadsCpp, Surface: s, Class: class, SurfaceAbsent: r.absent}
					order = append(order, class)
				}
				return byClass[class]
			}
			if len(names) == 0 {
				row("")
			}
			for _, e := range names {
				class := ""
				if claims != nil {
					class = claims.cases[s][e.name].class
				}
				cr := row(class)
				cr.Cases++
				switch r.outcome[e.name] {
				case outcomePass:
					cr.Answered++
				case outcomeFail:
					cr.Failed++
				case outcomeNamed:
					cr.Named++
				case outcomeClaimedAbsent:
					cr.AbsentClaimed++
				case outcomeAbsent:
					cr.NotCarried++
				default:
					// no outcome: the surface did not run for this leg, which
					// SurfaceAbsent already says, or a driver failure that
					// left no per-case answer, which is a failure
					if !r.absent {
						cr.Failed++
					}
				}
			}
			sort.Strings(order)
			for _, class := range order {
				out = append(out, *byClass[class])
			}
		}
	}
	return out
}

// writeCoverage writes rows as the JSON the coverage job reads back.
func writeCoverage(path string, rows []coverageRow) error {
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// readCoverage reads every coverage-<lang>.json and crossread-<lang>.json under
// dir (each a []coverageRow) and every fixedform-<lang>.txt beside them (one
// word: the leg's fixed-form gate, `pass`, `fail` or `none`), which is the
// shape the nightly's artifacts land in. Only those names: a leg's own answers
// ride the same artifacts, and a json-write answer is a .json file too.
func readCoverage(dir string) ([]coverageRow, map[string]string, error) {
	var rows []coverageRow
	fixed := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(path)
		switch {
		case (strings.HasPrefix(base, "coverage-") || strings.HasPrefix(base, "crossread-")) && strings.HasSuffix(base, ".json"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var some []coverageRow
			if err := json.Unmarshal(data, &some); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			rows = append(rows, some...)
		case strings.HasPrefix(base, "fixedform-") && strings.HasSuffix(base, ".txt"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fixed[strings.TrimSuffix(strings.TrimPrefix(base, "fixedform-"), ".txt")] = strings.TrimSpace(string(data))
		}
		return nil
	})
	return rows, fixed, err
}

// coverageMarkdown is the table the nightly writes to its job summary: one
// matrix per direction, a row per surface and class, a column per leg. A cell
// is what the leg answered over the cases, then what it did not:
//
//	14/16              answered 14 of 16, the other 2 failed
//	12/16 +4nc         answered 12, said ABSENT for 4 it does not claim
//	12/16 !2           answered 12, said ABSENT for 2 it CLAIMS (red)
//	absent             the leg does not run the surface
//
// A failure count rides as `x<N>`. Glenn, 2026-10-25: a case a port does not
// carry yet does not fail the night, and this table is how the gap stays seen.
func coverageMarkdown(w io.Writer, rows []coverageRow, fixed map[string]string) {
	b := &strings.Builder{}
	b.WriteString("# Cross-language conformance — coverage\n\n")
	b.WriteString("Cells: `answered/cases`, then `xN` failed, `!N` claimed by the carries registry and answered ABSENT (red), " +
		"`+Nnc` ABSENT and not claimed (not yet carried; not red), `+Nr` the row's named writer refusal. " +
		"`absent`: the leg does not run that surface. Policy: test/conformance/README.md, \"The nightly\".\n\n")
	for _, dir := range []struct{ key, title string }{
		{directionReadsCpp, "Ports read what C++ writes (and match its bytes)"},
		{directionCppReads, "C++ reads what each port writes (accepts, re-saves byte-identical, equals C++'s own write)"},
	} {
		var langs []string
		type key struct{ surface, class string }
		cells := map[key]map[string]coverageRow{}
		var keys []key
		for _, r := range rows {
			if r.Direction != dir.key {
				continue
			}
			if !slices.Contains(langs, r.Lang) {
				langs = append(langs, r.Lang)
			}
			k := key{r.Surface, r.Class}
			if cells[k] == nil {
				cells[k] = map[string]coverageRow{}
				keys = append(keys, k)
			}
			cells[k][r.Lang] = r
		}
		if len(langs) == 0 {
			continue
		}
		sort.Strings(langs)
		order := map[string]int{}
		for i, s := range surfaces {
			order[s] = i
		}
		sort.SliceStable(keys, func(i, j int) bool {
			if order[keys[i].surface] != order[keys[j].surface] {
				return order[keys[i].surface] < order[keys[j].surface]
			}
			return keys[i].class < keys[j].class
		})
		fmt.Fprintf(b, "## %s\n\n| surface | %s |\n|---|%s\n", dir.title, strings.Join(langs, " | "), strings.Repeat("---|", len(langs)))
		for _, k := range keys {
			label := k.surface
			if k.class != "" {
				label += " (" + k.class + ")"
			}
			fmt.Fprintf(b, "| %s |", label)
			for _, lang := range langs {
				r, ok := cells[k][lang]
				if !ok {
					b.WriteString(" — |")
					continue
				}
				fmt.Fprintf(b, " %s |", coverageCell(r))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(fixed) > 0 {
		var langs []string
		for lang := range fixed {
			langs = append(langs, lang)
		}
		sort.Strings(langs)
		b.WriteString("## The fixed form (form 3), each leg against the C++ byte oracle\n\n| leg | gate |\n|---|---|\n")
		for _, lang := range langs {
			fmt.Fprintf(b, "| %s | %s |\n", lang, fixed[lang])
		}
		b.WriteString("\n")
	}
	_, _ = io.WriteString(w, b.String())
}

func coverageCell(r coverageRow) string {
	if r.SurfaceAbsent {
		return "absent"
	}
	cell := fmt.Sprintf("%d/%d", r.Answered, r.Cases)
	if r.Failed > 0 {
		cell += fmt.Sprintf(" x%d", r.Failed)
	}
	if r.AbsentClaimed > 0 {
		cell += fmt.Sprintf(" !%d", r.AbsentClaimed)
	}
	if r.NotCarried > 0 {
		cell += fmt.Sprintf(" +%dnc", r.NotCarried)
	}
	if r.Named > 0 {
		cell += fmt.Sprintf(" +%dr", r.Named)
	}
	return cell
}
