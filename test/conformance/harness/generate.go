// `harness generate` — the generated half of the conformance data.
//
// Two artefacts, both from the compiler's own engine (internal/tablewire and
// internal/tabletext, reached through the public compiler API):
//
//   - json/<instance>.json, the docs/SPEC-TABLES.md §16 text of the instance whose
//     wire bytes are already pinned under testdata/wire/tables. The same
//     instance, both ways, so a port never hand-writes an instance again.
//
//   - reports.txt, the §4 read report of every evolution case: one generation's
//     bytes read by the other's type.
//
// The engine is a THIRD implementation of both forms — it was not written from
// the C++ backend and the C++ backend was not written from it — so a text this
// step writes is a real expectation rather than a restatement of what one
// backend happens to do.
//
// Every text it writes is proved complete before it lands: the text is packed
// back to wire bytes and must equal the golden it came from, byte for byte. A
// text that lost a field cannot pass that, so the data cannot silently under-
// specify the instance it claims to describe.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type units struct {
	c      *compiler.Compiler
	loaded map[string]*ir.Unit
	m      *Manifest
}

func newUnits(m *Manifest) *units {
	return &units{c: compiler.New(), loaded: map[string]*ir.Unit{}, m: m}
}

func (u *units) get(key string) (*ir.Unit, error) {
	if unit, ok := u.loaded[key]; ok {
		return unit, nil
	}
	args, err := u.m.UnitPaths(key)
	if err != nil {
		return nil, err
	}
	paths, err := compiler.GatherPaths(args)
	if err != nil {
		return nil, fmt.Errorf("unit %s: %w", key, err)
	}
	u.c.FormatInPlace = false
	unit, err := u.c.Load(paths)
	if err != nil {
		return nil, fmt.Errorf("loading unit %s: %w", key, err)
	}
	u.loaded[key] = unit
	return unit, nil
}

func generate(m *Manifest, jsonDir, reportsPath string) error {
	u := newUnits(m)

	if err := os.MkdirAll(jsonDir, 0o755); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "conformance-generate")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	written := map[string]bool{}
	for _, inst := range m.Instances {
		if inst.NoText {
			// the corpus carries this one on the WIRE only, and says so on its
			// own line: the variable class has no text form in any
			// implementation and the tool refuses a variable root in both
			// directions (§16.2). schema#275 drops the marker and this arm
			// with it — until then a JSON golden here would be a file nothing
			// can write and nothing holds.
			fmt.Printf("conformance: %-22s wire only — the corpus marks it no-text (§16.2, schema#275)\n", inst.Name)
			continue
		}
		unit, err := u.get(inst.Unit)
		if err != nil {
			return err
		}
		wire, err := os.ReadFile(inst.Wire)
		if err != nil {
			return fmt.Errorf("%s: %w", inst.Name, err)
		}

		// wire -> text
		one := filepath.Join(scratch, inst.Name)
		if err := os.MkdirAll(one, 0o755); err != nil {
			return err
		}
		report, err := u.c.UnpackOneFile(unit, inst.Root, wire, one)
		if err != nil {
			return fmt.Errorf("%s: unpack: %w", inst.Name, err)
		}
		if !report.Silent() {
			return fmt.Errorf("%s: its own writer's bytes do not read clean: %+v", inst.Name, report)
		}
		text, err := os.ReadFile(filepath.Join(one, inst.Root+".json"))
		if err != nil {
			return fmt.Errorf("%s: %w", inst.Name, err)
		}
		// A golden carries EXACTLY the §16 text and nothing around it, the way
		// a wire golden carries exactly the wire — AND THE TEXT ENDS WITH ONE
		// NEWLINE, which is part of the form rather than a file convention
		// (§16.1). `schema unpack` writes it, `ToJson` writes it, this engine
		// writes it, and every reader accepts a text with or without one; so
		// the byte goes in the data, and a golden that lost it would put the
		// three writers back into disagreement.
		if !bytes.HasSuffix(text, []byte("\n")) {
			return fmt.Errorf("%s: the engine's text does not end with the newline §16.1 requires", inst.Name)
		}
		if bytes.HasSuffix(text, []byte("\n\n")) {
			return fmt.Errorf("%s: the engine's text ends with more than one newline (§16.1 says exactly one)", inst.Name)
		}

		// and the text back to wire, which is what proves the text COMPLETE:
		// a text that lost a field cannot pack to the bytes it came from
		back, _, backReport, err := u.c.Pack(unit, inst.Root, one)
		if err != nil {
			return fmt.Errorf("%s: pack: %w", inst.Name, err)
		}
		if !backReport.Silent() {
			return fmt.Errorf("%s: the text this step wrote does not read clean: %+v", inst.Name, backReport)
		}
		if len(back) != len(wire) || string(back) != string(wire) {
			return fmt.Errorf("%s: the text does not carry the instance — %d bytes back against %d pinned",
				inst.Name, len(back), len(wire))
		}

		if err := writeFileAtomic(inst.JSON, text); err != nil {
			return err
		}
		written[filepath.Base(inst.JSON)] = true
		fmt.Printf("conformance: %-22s %7d B wire  %8d B text\n", inst.Name, len(wire), len(text))
	}

	// a text no instance names is scaffolding, and it goes now rather than
	// rotting into a golden nobody reads
	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !written[e.Name()] {
			if err := os.Remove(filepath.Join(jsonDir, e.Name())); err != nil {
				return err
			}
			fmt.Printf("conformance: removed %s, which no instance names\n", e.Name())
		}
	}

	// the evolution reports
	var lines []string
	for _, rc := range m.Reports {
		unit, err := u.get(rc.Unit)
		if err != nil {
			return err
		}
		wire, err := os.ReadFile(rc.Wire)
		if err != nil {
			return fmt.Errorf("%s: %w", rc.Name, err)
		}
		// the counters and only the counters: a report is a fact about the
		// DECODE, and asking for a text here would refuse the variable class
		// for a file this step throws away
		report, err := u.c.ReadReport(unit, rc.Root, wire)
		if err != nil {
			return fmt.Errorf("%s: read: %w", rc.Name, err)
		}
		counts := Counts{
			Unknown:      report.Unknown,
			KindMismatch: report.KindMismatch,
			Clamped:      report.Clamped,
			Duplicate:    report.Duplicate,
			Malformed:    report.Malformed,
		}
		lines = append(lines, fmt.Sprintf("%-18s %s", rc.Name, counts))
	}
	sort.Strings(lines)

	header := `# THE READ REPORTS (docs/SPEC-TABLES.md §4), generated by the compiler's own
# engine over the cases testdata/conformance/tables/MANIFEST.txt names.
#
#   <case>  <unknown>,<kind_mismatch>,<clamped>,<duplicate>,<malformed>
#
# Regenerate with: make conformance-generate. A counter that MOVES under an
# unchanged schema is stop-the-line, exactly as a wire golden is.

`
	body := header + strings.Join(lines, "\n") + "\n"
	if err := writeFileAtomic(reportsPath, []byte(body)); err != nil {
		return err
	}
	fmt.Printf("conformance: %d instances, %d report cases\n", len(m.Instances), len(m.Reports))
	return nil
}

// readReports loads the generated expectations.
func readReports(path string) (map[string]Counts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]Counts{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("%s: %q is not a report line", path, line)
		}
		c, err := ParseCounts(f[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out[f[0]] = c
	}
	return out, nil
}
