// `harness cross-read` diagnoses one saved set of language-driver answers.
// It checks canonical byte equality and has the reference read the answers
// back, localizing a refusal to its case. The nightly does not repeat this
// per writer: `harness run --only cpp` validates the shared canonical corpus,
// and every language's run checks its own answers against those same bytes.
// Equality makes extra reference reads of identical copies redundant.
// See test/conformance/README.md, "The nightly".
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// crossSurfaces are the surfaces with a second direction, in matrix order.
var crossSurfaces = []string{"wire", "json-read", "message", "json-write"}

// crossCase is one port answer the reference re-reads.
type crossCase struct {
	name string // the case: the instance or message name
	file string // the port's answer, as `harness run` left it
	want []byte // C++'s canonical write for the case
	// the manifest line that points the reference at file
	line string
	// out is the file the reference writes for it
	out string
	// refusal is the row's named writer refusal, which the reference may
	// answer instead of bytes (docs/SPEC-TABLES.md §2.5)
	refusal string
}

// crossRead runs the second direction for one leg over the writes it left
// under writes/<surface>/, and returns the coverage rows and the verdict.
func crossRead(w io.Writer, m *Manifest, reportsPath, driversPath, work, lang, writes string, claims *claimSet) ([]coverageRow, bool, error) {
	drivers, err := discoverDrivers(driversPath)
	if err != nil {
		return nil, false, err
	}
	reference := drivers[0]
	reports, err := readReports(reportsPath)
	if err != nil {
		return nil, false, err
	}
	var rows []coverageRow
	var failures []string
	for _, surface := range crossSurfaces {
		dir := filepath.Join(writes, surface)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			// the leg did not run the surface: the first direction's table
			// already says so, and there is nothing here to re-read
			rows = append(rows, coverageRow{Lang: lang, Direction: directionCppReads, Surface: surface, SurfaceAbsent: true})
			continue
		}
		want, err := expectations(m, surface, reports, "")
		if err != nil {
			return nil, false, err
		}
		cases, err := crossCases(m, surface, dir, want)
		if err != nil {
			return nil, false, err
		}
		outcome := map[string]string{}
		var problems []string
		// THE PORT'S BYTES ARE C++'S, case by case
		for _, c := range cases {
			got, err := os.ReadFile(c.file)
			if err != nil {
				return nil, false, err
			}
			if !bytes.Equal(got, c.want) {
				outcome[c.name] = outcomeFail
				problems = append(problems, "the port's bytes are not C++'s canonical write: "+describeDiff(c.out, c.want, got))
			}
		}
		// AND C++ READS THEM BACK, byte-identical
		more, err := referenceReads(m, reference, surface, cases, filepath.Join(work, "cross-read", lang, surface), outcome)
		if err != nil {
			return nil, false, err
		}
		problems = append(problems, more...)
		for _, p := range problems {
			failures = append(failures, fmt.Sprintf("  %s / %s: %s", lang, surface, p))
		}
		byClass := map[string]*coverageRow{}
		var order []string
		for _, c := range cases {
			class := ""
			if claims != nil {
				class = claims.cases[surface][answerName(surface, c.name)].class
			}
			r := byClass[class]
			if r == nil {
				r = &coverageRow{Lang: lang, Direction: directionCppReads, Surface: surface, Class: class}
				byClass[class] = r
				order = append(order, class)
			}
			r.Cases++
			if outcome[c.name] == outcomeFail {
				r.Failed++
			} else {
				r.Answered++
			}
		}
		if len(cases) == 0 {
			rows = append(rows, coverageRow{Lang: lang, Direction: directionCppReads, Surface: surface})
		}
		sort.Strings(order)
		for _, class := range order {
			rows = append(rows, *byClass[class])
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nCROSS-READ — the C++ reference reads what %s wrote\n\n", lang)
	for _, r := range rows {
		label := r.Surface
		if r.Class != "" {
			label += " (" + r.Class + ")"
		}
		fmt.Fprintf(&b, "  %-22s %s\n", label, coverageCell(r))
	}
	b.WriteString("\n")
	sort.Strings(failures)
	if len(failures) > 0 {
		fmt.Fprintf(&b, "FAILURES\n%s\n\n", strings.Join(failures, "\n"))
	} else {
		fmt.Fprintf(&b, "cross-read: C++ accepts every case %s answered, byte-identical, and every one is C++'s own write\n", lang)
	}
	_, _ = io.WriteString(w, b.String())
	return rows, len(failures) == 0, nil
}

// answerName is the file a surface's case is written as: json-write's is the
// instance's name and `.json`, every other surface's is the case's name.
func answerName(surface, name string) string {
	if surface == "json-write" {
		return name + ".json"
	}
	return name
}

// crossCases lists the cases a leg answered on one surface — a file where the
// answer goes, not `<case>.absent` — each with the manifest line that hands it
// to the reference.
func crossCases(m *Manifest, surface, dir string, want []expectation) ([]crossCase, error) {
	wantBy := map[string][]byte{}
	for _, e := range want {
		wantBy[e.name] = e.want
	}
	answered := func(name string) (string, bool, error) {
		path := filepath.Join(dir, answerName(surface, name))
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return path, true, nil
	}
	var out []crossCase
	switch surface {
	case "wire", "json-read":
		for _, inst := range m.Instances {
			if inst.NoText && surface == "json-read" {
				continue
			}
			path, ok, err := answered(inst.Name)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			out = append(out, crossCase{
				name: inst.Name, file: path, want: wantBy[inst.Name], refusal: inst.GoWireRefusal,
				line: fmt.Sprintf("instance %s %s %s %s", inst.Name, inst.Unit, inst.Root, repoRelative(path)),
				out:  inst.Name,
			})
		}
	case "message":
		for _, msg := range m.Messages {
			path, ok, err := answered(msg.Name)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			out = append(out, crossCase{
				name: msg.Name, file: path, want: wantBy[msg.Name],
				line: fmt.Sprintf("message %s %s %s %s %s", msg.Name, msg.Connection, msg.Root, msg.FileWire, repoRelative(path)),
				out:  msg.Name,
			})
		}
	case "json-write":
		for _, inst := range m.Instances {
			if inst.NoText {
				continue
			}
			path, ok, err := answered(inst.Name)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			out = append(out, crossCase{
				name: inst.Name, file: path, want: wantBy[inst.Name+".json"],
				line: fmt.Sprintf("json-text %s %s %s %s", inst.Name, inst.Unit, inst.Root, repoRelative(path)),
				out:  inst.Name + ".json",
			})
		}
	default:
		return nil, fmt.Errorf("%q has no second direction", surface)
	}
	// a path the reference splits on whitespace cannot carry a space
	for _, c := range out {
		if strings.ContainsAny(c.file, " \t") {
			return nil, fmt.Errorf("%s: the reference reads manifest fields split on whitespace, and this path has some: %s", c.name, c.file)
		}
	}
	return out, nil
}

// referenceSurface is the reference surface that re-reads a port's surface.
func referenceSurface(surface string) string {
	switch surface {
	case "json-read":
		return "wire" // the port's answer is wire bytes; C++ loads and saves them
	case "json-write":
		return "json-text"
	}
	return surface
}

// crossManifest is the substituted manifest the reference reads: every unit and
// connection, and the lines that point at the port's files.
func crossManifest(m *Manifest, path string, cases []crossCase) error {
	var b strings.Builder
	b.WriteString("# DERIVED, by `harness cross-read` — do not edit, do not commit.\n")
	b.WriteString("# The lines point the reference at a PORT's answers (test/conformance/README.md, \"The nightly\").\n\n")
	for _, u := range m.Units {
		fmt.Fprintf(&b, "unit %s %s\n", u.Key, strings.Join(u.Paths, " "))
	}
	for _, c := range m.Connections {
		fmt.Fprintf(&b, "connection %s %s 0x%016x %s\n", c.Key, c.Unit, c.BuildVersion, c.Wire)
	}
	for _, c := range cases {
		b.WriteString(c.line + "\n")
	}
	return writeFileAtomic(path, []byte(b.String()))
}

// referenceReads hands the cases to the reference in one process, and when it
// refuses, once per case so the refusal is named. It records a failing case in
// outcome and returns what failed.
func referenceReads(m *Manifest, reference driver, surface string, cases []crossCase, work string, outcome map[string]string) ([]string, error) {
	if len(cases) == 0 {
		return nil, nil
	}
	refSurface := referenceSurface(surface)
	batch := filepath.Join(work, "all")
	code, stderr, err := referenceRun(m, reference, refSurface, cases, batch)
	if err != nil {
		return nil, err
	}
	var problems []string
	if code == 0 {
		for _, c := range cases {
			if p := judgeReread(c, filepath.Join(batch, "out")); p != "" {
				outcome[c.name] = outcomeFail
				problems = append(problems, p)
			}
		}
		return problems, nil
	}
	// THE BATCH REFUSED: one case at a time, so the refusal has a name
	for i, c := range cases {
		one := filepath.Join(work, fmt.Sprintf("case-%03d", i))
		code, stderr, err := referenceRun(m, reference, refSurface, cases[i:i+1], one)
		if err != nil {
			return nil, err
		}
		var p string
		switch {
		case code == 2:
			p = fmt.Sprintf("%s: the REFERENCE has no %s surface (exit 2) — it may not be absent", c.name, refSurface)
		case code != 0:
			p = fmt.Sprintf("%s: C++ REFUSED the port's bytes (the reference exited %d)\n%s", c.name, code, indent(stderr, "    "))
		default:
			p = judgeReread(c, filepath.Join(one, "out"))
		}
		if p != "" {
			outcome[c.name] = outcomeFail
			problems = append(problems, p)
		}
	}
	if len(problems) == 0 {
		// the batch failed and no case alone does: say so rather than pass it
		problems = append(problems, fmt.Sprintf("the reference refused the batch (exit %d) and accepted every case alone\n%s", code, indent(stderr, "    ")))
	}
	return problems, nil
}

// referenceRun writes the substituted manifest for cases under dir and runs the
// reference's surface over it into dir/out.
func referenceRun(m *Manifest, reference driver, surface string, cases []crossCase, dir string) (int, string, error) {
	if err := os.RemoveAll(dir); err != nil {
		return 0, "", err
	}
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return 0, "", err
	}
	manifest := filepath.Join(dir, "manifest.txt")
	if err := crossManifest(m, manifest, cases); err != nil {
		return 0, "", err
	}
	return runDriver(reference, manifest, surface, out)
}

// judgeReread holds the reference's re-save of one port answer to the port's
// own bytes, and says what is wrong, or "" when nothing is.
func judgeReread(c crossCase, out string) string {
	answer := filepath.Join(out, c.out)
	got, err := os.ReadFile(answer)
	if err != nil {
		if reason, rerr := os.ReadFile(answer + ".absent"); rerr == nil {
			if c.refusal != "" && string(reason) == c.refusal {
				return "" // the row's named writer refusal, which C++ gives too (§2.5)
			}
			return fmt.Sprintf("%s: C++ answered ABSENT (%q) for the port's bytes", c.name, string(reason))
		}
		return fmt.Sprintf("%s: C++ wrote nothing for the port's bytes", c.name)
	}
	port, err := os.ReadFile(c.file)
	if err != nil {
		return fmt.Sprintf("%s: %v", c.name, err)
	}
	if !bytes.Equal(got, port) {
		return "C++ re-saved the port's bytes differently: " + describeDiff(c.out, port, got)
	}
	return ""
}
