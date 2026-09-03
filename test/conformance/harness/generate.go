// `harness generate` — the generated half of the conformance data.
//
// Two artefacts, both from the compiler's own engine (internal/tablewire and
// internal/tabletext, reached through the public compiler API):
//
//   - json/<instance>.json, the SPEC-TABLES.md §16 text of the instance whose
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
		return nil, fmt.Errorf("unit %s: %v", key, err)
	}
	u.c.FormatInPlace = false
	unit, err := u.c.Load(paths)
	if err != nil {
		return nil, fmt.Errorf("loading unit %s: %v", key, err)
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
	defer os.RemoveAll(scratch)

	written := map[string]bool{}
	for _, inst := range m.Instances {
		unit, err := u.get(inst.Unit)
		if err != nil {
			return err
		}
		wire, err := os.ReadFile(inst.Wire)
		if err != nil {
			return fmt.Errorf("%s: %v", inst.Name, err)
		}

		// wire -> text
		one := filepath.Join(scratch, inst.Name)
		if err := os.MkdirAll(one, 0o755); err != nil {
			return err
		}
		report, err := u.c.UnpackOneFile(unit, inst.Root, wire, one)
		if err != nil {
			return fmt.Errorf("%s: unpack: %v", inst.Name, err)
		}
		if !report.Silent() {
			return fmt.Errorf("%s: its own writer's bytes do not read clean: %+v", inst.Name, report)
		}
		text, err := os.ReadFile(filepath.Join(one, inst.Root+".json"))
		if err != nil {
			return fmt.Errorf("%s: %v", inst.Name, err)
		}
		// A golden carries EXACTLY the §16 text and nothing around it, the way
		// a wire golden carries exactly the wire: `schema unpack` is writing a
		// FILE and ends one with a newline, and `ToJson` returns a BUFFER and
		// does not. The section fixes the text's shape and says nothing about a
		// trailing newline, so the byte that only one of the two writes is not
		// part of the form and does not go in the data.
		text = bytes.TrimSuffix(text, []byte("\n"))

		// and the text back to wire, which is what proves the text COMPLETE:
		// a text that lost a field cannot pack to the bytes it came from
		back, _, backReport, err := u.c.Pack(unit, inst.Root, one)
		if err != nil {
			return fmt.Errorf("%s: pack: %v", inst.Name, err)
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
			return fmt.Errorf("%s: %v", rc.Name, err)
		}
		one := filepath.Join(scratch, "report-"+rc.Name)
		if err := os.MkdirAll(one, 0o755); err != nil {
			return err
		}
		report, err := u.c.UnpackOneFile(unit, rc.Root, wire, one)
		if err != nil {
			return fmt.Errorf("%s: unpack: %v", rc.Name, err)
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

	header := `# THE READ REPORTS (SPEC-TABLES.md §4), generated by the compiler's own
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
	for _, line := range strings.Split(string(data), "\n") {
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
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		out[f[0]] = c
	}
	return out, nil
}
