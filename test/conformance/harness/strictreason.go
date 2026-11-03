// `harness strict-reasons` — the strict read's refusal REASON, compared across
// every leg.
//
// The `report` and `framing` surfaces compare the six counters and the verdict
// of the TOLERANT read, and nothing about WHICH refusal a strict read names.
// The strict read answers one typed reason (docs/SPEC-TABLES.md §6.5, §11), one
// member of the refusal-names enum, and a reason no surface compares is a
// reason that drifts by language. The `strict-reason` surface closes that: for
// every wire a `report` or `framing` line names, the leg runs its strict read
// at the line's root and writes the reason as the enum's value name, or `ok`
// for a read the strict rule accepts, one file per case.
//
// The expectation is strict-reasons.txt, PINNED from the reference leg by
// `harness pin` on the repo's standing convention: C++ writes the pins, every
// other leg compares. A row where the reference's answer disagrees with the
// spec text carries the SPEC's reason and the reference's own answer beside it
// as `reference=<reason>`, so the disagreement is held red against the
// reference too rather than pinned as the answer; `harness pin` keeps such a
// row's reason and refreshes the marker, and drops it once the reference names
// the spec's reason.
//
// It is a column of `harness run`, so every leg's conformance job holds it.
// `harness strict-reasons` (`make conformance-strict-reasons`) runs the same
// comparison and prints the whole disagreement table — leg, case, expected
// reason, the leg's reason — so the owners of each leg's reasons have one list
// to work from.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// strictReasonSurface is the driver surface the gate runs.
const strictReasonSurface = "strict-reason"

const defaultStrictReasons = "testdata/conformance/tables/strict-reasons.txt"

// StrictReason is one pinned row: the reason every leg owes the case, and,
// where the reference's answer is not the spec's, the reference's own answer.
type StrictReason struct {
	Case      string
	Reason    string
	Reference string // the reference's answer where it disagrees with the spec, else ""
}

// strictReasonCases is every case the surface answers, in manifest order:
// the `report` lines, then the `strict` lines, then the `framing` lines.
func strictReasonCases(m *Manifest) []string {
	var out []string
	for _, r := range m.Reports {
		out = append(out, r.Name)
	}
	for _, r := range m.Stricts {
		out = append(out, r.Name)
	}
	for _, f := range m.Framings {
		out = append(out, f.Name)
	}
	return out
}

// deriveStrictManifest writes the manifest the strict-reason surface reads:
// the derived manifest with each `strict` line carried as a `report` line
// after the report lines, so a driver runs its strict read over it as over any
// report wire. Only this surface reads it; `harness run`'s manifest has no
// strict line, so no other surface sees those wires.
func deriveStrictManifest(m *Manifest, path string) error {
	carried := *m
	carried.Reports = append(append([]ReportCase(nil), m.Reports...), m.Stricts...)
	return deriveManifest(&carried, path)
}

// readStrictReasons reads strict-reasons.txt.
func readStrictReasons(path string) (map[string]StrictReason, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]StrictReason{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 && len(f) != 3 {
			return nil, fmt.Errorf("%s: %q is not a strict-reason line", path, line)
		}
		row := StrictReason{Case: f[0], Reason: f[1]}
		if len(f) == 3 {
			ref, ok := strings.CutPrefix(f[2], "reference=")
			if !ok || ref == "" {
				return nil, fmt.Errorf("%s: %q: the third column is reference=<reason>", path, line)
			}
			row.Reference = ref
		}
		if _, dup := out[row.Case]; dup {
			return nil, fmt.Errorf("%s: %q is pinned twice", path, row.Case)
		}
		out[row.Case] = row
	}
	return out, nil
}

const strictReasonsHeader = `# THE STRICT READ'S REFUSAL REASON (docs/SPEC-TABLES.md §6.5, §11), one row
# per wire a report, strict or framing line of MANIFEST.txt names: the reason every
# leg's strict read owes it, as the refusal-names enum spells it, or ok.
#
#   <case>  <reason>  [reference=<the reference leg's answer>]
#
# PINNED from the reference leg by: make conformance-pin. A row carrying
# reference= is a case where the reference's answer is not the spec's: the
# reason column is the spec's, held by hand, and the marker is what the
# reference answers today. The pin keeps the reason and refreshes the marker,
# and drops the marker once the reference names the spec's reason. Checked by:
# make conformance-strict-reasons.
`

// writeStrictReasons writes the pinned file in manifest order.
func writeStrictReasons(path string, cases []string, rows map[string]StrictReason) error {
	var b strings.Builder
	b.WriteString(strictReasonsHeader)
	b.WriteString("\n")
	width := 0
	for _, c := range cases {
		width = max(width, len(c))
	}
	for _, c := range cases {
		row := rows[c]
		line := fmt.Sprintf("%-*s %s", width, c, row.Reason)
		if row.Reference != "" {
			line += " reference=" + row.Reference
		}
		b.WriteString(line + "\n")
	}
	return writeFileAtomic(path, []byte(b.String()))
}

// pinStrictReasons runs the reference leg's strict-reason surface and writes
// strict-reasons.txt from it, keeping the spec's reason on every row that
// carries a reference= marker.
func pinStrictReasons(m *Manifest, reference driver, derived, work, path string) error {
	out := filepath.Join(work, "pin", strictReasonSurface)
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	derived = filepath.Join(work, "strict-manifest.txt")
	if err := deriveStrictManifest(m, derived); err != nil {
		return err
	}
	code, stderr, err := runDriver(reference, derived, strictReasonSurface, out)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the %s driver's %s surface exited %d\n%s", reference.lang, strictReasonSurface, code, stderr)
	}
	held := map[string]StrictReason{}
	if _, err := os.Stat(path); err == nil {
		if held, err = readStrictReasons(path); err != nil {
			return err
		}
	}
	cases := strictReasonCases(m)
	rows := map[string]StrictReason{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(out, c))
		if err != nil {
			return fmt.Errorf("the reference leg answered no %s for %s: %w", strictReasonSurface, c, err)
		}
		answer := strings.TrimSpace(string(data))
		row := StrictReason{Case: c, Reason: answer}
		if prior, ok := held[c]; ok && prior.Reference != "" && prior.Reason != answer {
			// the spec's reason stays; the marker says what the reference
			// answers instead
			row = StrictReason{Case: c, Reason: prior.Reason, Reference: answer}
		}
		rows[c] = row
	}
	if err := writeStrictReasons(path, cases, rows); err != nil {
		return err
	}
	fmt.Printf("conformance: pinned %s from the %s leg — %d cases\n", path, reference.lang, len(cases))
	return nil
}

// strictDisagreement is one row of the table the gate prints.
type strictDisagreement struct {
	lang, name, want, got string
}

// strictReasons runs the gate: every registered leg's strict-reason surface
// against the pinned reasons. A leg that does not list the surface prints
// ABSENT, except the reference, whose absence is a failure as it is on every
// surface. A case answered `<case>.absent` is counted, never compared.
func strictReasons(w io.Writer, m *Manifest, reasonsPath, driversPath, work, only, skip, tablePath string) (bool, error) {
	drivers, discovered, err := loadDrivers(driversPath)
	if err != nil {
		return false, err
	}
	skipped, err := skipSet(skip)
	if err != nil {
		return false, err
	}
	pinned, err := readStrictReasons(reasonsPath)
	if err != nil {
		return false, err
	}
	cases := strictReasonCases(m)
	for _, c := range cases {
		if _, ok := pinned[c]; !ok {
			return false, fmt.Errorf("%s pins no reason for %q — run: make conformance-pin", reasonsPath, c)
		}
	}
	if err := materialise(m, work); err != nil {
		return false, err
	}
	derived := filepath.Join(work, "strict-manifest.txt")
	if err := deriveStrictManifest(m, derived); err != nil {
		return false, err
	}

	var b strings.Builder
	b.WriteString("\nSTRICT READ REASONS — leg x the " + fmt.Sprint(len(cases)) + " report, strict and framing wires\n\n")
	var rows []strictDisagreement
	var failures []string
	for i, d := range drivers {
		reference := i == 0 && discovered
		if only != "" && d.lang != only {
			continue
		}
		if skipped[d.lang] {
			fmt.Fprintf(&b, "%-10s skipped (--skip)\n", d.lang)
			continue
		}
		listed, err := listSurfaces(d, derived)
		if err != nil {
			return false, fmt.Errorf("%s: %w", d.lang, err)
		}
		if !listed[strictReasonSurface] {
			if reference {
				failures = append(failures, d.lang+": the REFERENCE leg does not list "+strictReasonSurface)
				fmt.Fprintf(&b, "%-10s FAIL absent\n", d.lang)
				continue
			}
			fmt.Fprintf(&b, "%-10s absent\n", d.lang)
			continue
		}
		out := filepath.Join(work, "out", d.lang, strictReasonSurface)
		if err := os.RemoveAll(out); err != nil {
			return false, err
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return false, err
		}
		code, stderr, err := runDriver(d, derived, strictReasonSurface, out)
		if err != nil {
			return false, err
		}
		if code == 2 && !reference {
			fmt.Fprintf(&b, "%-10s absent\n", d.lang)
			continue
		}
		if code != 0 {
			failures = append(failures, fmt.Sprintf("%s: the driver exited %d\n%s", d.lang, code, indent(stderr, "    ")))
			fmt.Fprintf(&b, "%-10s FAIL (exit %d)\n", d.lang, code)
			continue
		}
		pass, missing, wrong := 0, 0, 0
		for _, c := range cases {
			want := pinned[c].Reason
			data, err := os.ReadFile(filepath.Join(out, c))
			if err != nil {
				if _, absent := os.Stat(filepath.Join(out, c+".absent")); absent == nil {
					missing++
					if reference {
						failures = append(failures, fmt.Sprintf("%s / %s: the REFERENCE leg answered ABSENT", d.lang, c))
					}
					continue
				}
				wrong++
				rows = append(rows, strictDisagreement{d.lang, c, want, "(nothing written)"})
				continue
			}
			got := strings.TrimSpace(string(data))
			if got != want {
				wrong++
				rows = append(rows, strictDisagreement{d.lang, c, want, got})
				continue
			}
			pass++
		}
		cell := fmt.Sprintf("pass %d/%d", pass, len(cases)-missing)
		if wrong > 0 {
			cell = fmt.Sprintf("FAIL %d/%d", pass, len(cases)-missing)
		}
		if missing > 0 {
			cell += fmt.Sprintf(" +%da", missing)
		}
		fmt.Fprintf(&b, "%-10s %s\n", d.lang, cell)
	}

	if len(rows) > 0 {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].lang != rows[j].lang {
				return rows[i].lang < rows[j].lang
			}
			return rows[i].name < rows[j].name
		})
		b.WriteString("\nDISAGREEMENTS (leg, case, expected reason, the leg's reason)\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-10s %-44s %-24s %s\n", r.lang, r.name, r.want, r.got)
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		b.WriteString("\nFAILURES\n  " + strings.Join(failures, "\n  ") + "\n")
	}
	if len(rows) == 0 && len(failures) == 0 {
		b.WriteString("\nstrict read reasons: every leg that answers names the pinned reason\n")
	}
	_, _ = io.WriteString(w, b.String())

	if tablePath != "" {
		var t strings.Builder
		t.WriteString("leg\tcase\texpected\tleg reason\n")
		for _, r := range rows {
			fmt.Fprintf(&t, "%s\t%s\t%s\t%s\n", r.lang, r.name, r.want, r.got)
		}
		if err := writeFileAtomic(tablePath, []byte(t.String())); err != nil {
			return false, err
		}
	}
	return len(rows) == 0 && len(failures) == 0, nil
}
