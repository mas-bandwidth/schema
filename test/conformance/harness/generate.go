// `harness generate` — the generated half of the conformance data.
//
// Two artefacts, both from the compiler's own engine (internal/tablewire and
// internal/tabletext):
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookedReportClean holds a cook's report to clean: any count is a reason the
// fixture is not the tool's.
func cookedReportClean(inst Instance, r compiler.TableReport) bool {
	return r.Silent()
}

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
			// own line: it is past the text form's depth cap by the form's own
			// rule (§16.7), and a text the engine wrote for it would be one
			// every reader refuses
			fmt.Printf("conformance: %-22s wire only — the corpus marks it no-text (§16.7)\n", inst.Name)
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

		out, err := outPath(jsonDir, inst.JSON)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(out, text); err != nil {
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

	// THE COOKED FILES A RUNTIME'S WRITER IS HELD TO (docs/SPEC-TABLES.md §7.6),
	// in both byte orders. They come from `schema cook` through the same public
	// driver the command uses, because the TOOL IS THE REFERENCE: a runtime that
	// writes a cook writes the tool's bytes or it has written a second format.
	// The wire is the format of record, so the pair is (this instance's wire,
	// this unit's build version) and nothing here is hand-authored.
	for _, cw := range m.CookWrites {
		inst, err := m.LookupInstance(cw.Instance)
		if err != nil {
			return err
		}
		unit, err := u.get(inst.Unit)
		if err != nil {
			return err
		}
		wire, err := os.ReadFile(inst.Wire)
		if err != nil {
			return fmt.Errorf("%s: %w", cw.Instance, err)
		}
		for _, arm := range []struct {
			path string
			big  bool
		}{{cw.Little, false}, {cw.Big, true}} {
			cooked, _, report, err := u.c.Cook(unit, inst.Root, wire, compiler.CookOptions{Big: arm.big})
			if err != nil {
				return fmt.Errorf("%s: cook: %w", cw.Instance, err)
			}
			if !cookedReportClean(inst, report) {
				return fmt.Errorf("%s: its own writer's bytes do not read clean: %+v", cw.Instance, report)
			}
			out, err := outPath(jsonDir, arm.path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if err := writeFileAtomic(out, cooked); err != nil {
				return err
			}
		}
		fmt.Printf("conformance: %-22s cooked both orders\n", cw.Instance)
	}

	// the evolution reports, and the value each read that reads leaves
	var lines []string
	values := map[string]bool{}
	for _, rc := range m.Reports {
		unit, err := u.get(rc.Unit)
		if err != nil {
			return err
		}
		wire, err := os.ReadFile(rc.Wire)
		if err != nil {
			return fmt.Errorf("%s: %w", rc.Name, err)
		}
		// A report describes the decode even when damage discards the whole
		// instance. Read the engine's verdict directly: ReadReport also
		// returns an unpacking diagnostic when no value can be produced.
		root := unit.Tables[rc.Root]
		if root == nil {
			return fmt.Errorf("%s: root %q names no table", rc.Name, rc.Root)
		}
		model := tabletext.NewModel(unit)
		var report tabletext.Report
		value := model.New(root)
		ok, err := tablewire.Decode(model, value, wire, &report)
		// A read's refusal is a report verdict. The storage-cap refusal is
		// this build's resource limit rather than the corpus's answer, so it
		// stops generation with every other engine error.
		_, storageCap := errors.AsType[*tablewire.CountRefusal](err)
		if err != nil && (!tablewire.ReadRefusal(err) || storageCap) {
			return fmt.Errorf("%s: read: %w", rc.Name, err)
		}
		if !ok && err == nil {
			return fmt.Errorf("%s: decoder stopped without a verdict", rc.Name)
		}
		// the one read's verdict in the manifest's columns until layer 3
		// re-pins it (docs/plans/strict-only.md): a form refusal is Refused,
		// every other refusal Malformed
		counts := Counts{
			Unknown:   report.Unknown,
			Widened:   report.Widened,
			Malformed: !ok && !tablewire.Refused(err),
			Refused:   tablewire.Refused(err),
		}
		lines = append(lines, fmt.Sprintf("%-18s %s", rc.Name, counts))
		values[filepath.Base(rc.Value)] = true
		valuePath, err := outPath(jsonDir, rc.Value)
		if err != nil {
			return err
		}
		if err := writeValue(valuePath, ok && !counts.Malformed && !counts.Refused, func() ([]byte, error) {
			return model.Write(value)
		}); err != nil {
			return fmt.Errorf("%s: %w", rc.Name, err)
		}
	}
	sort.Strings(lines)
	if len(m.Reports) > 0 {
		valueDir, err := outPath(jsonDir, filepath.Dir(m.Reports[0].Value))
		if err != nil {
			return err
		}
		if err := pruneValues(valueDir, values); err != nil {
			return err
		}
	}

	// THE WRITE REFUSALS (docs/SPEC-TABLES.md §16.14, W0): the oracle must
	// refuse every one, or the row asks every leg for an answer the reference
	// does not give.
	for _, wr := range m.JSONWriteRefusals {
		unit, err := u.get(wr.Unit)
		if err != nil {
			return err
		}
		if err := oracleRefusesJSONWrite(unit, wr); err != nil {
			return fmt.Errorf("%s: %w", wr.Name, err)
		}
	}

	// THE HOSTILE ROWS' VALUES: the oracle's text read of each tree, then its
	// writer, so a leg that counts the same events and lands a different
	// value is red (test/conformance/README.md, `json-hostile`). The engine's
	// verdict must be the row's own standard verdict, or the row and the
	// oracle disagree and the corpus cannot say which value is owed.
	for _, h := range m.Hostiles {
		if err := hostileValue(u, h, jsonDir); err != nil {
			return fmt.Errorf("%s: %w", h.Name, err)
		}
	}

	header := `# THE READ REPORTS (docs/SPEC-TABLES.md §4), generated by the compiler's own
# engine over the cases testdata/conformance/tables/MANIFEST.txt names.
#
#   <case>  <unknown>,<kind_mismatch>,<widened>,<clamped>,<duplicate>,<malformed>,<verdict>
#
# Regenerate with: make conformance-generate. A counter that MOVES under an
# unchanged schema is stop-the-line, exactly as a wire golden is.

`
	body := header + strings.Join(lines, "\n") + "\n"
	if err := writeFileAtomic(reportsPath, []byte(body)); err != nil {
		return err
	}
	fmt.Printf("conformance: %d instances, %d report cases, %d cooked pairs\n", len(m.Instances), len(m.Reports), len(m.CookWrites))
	return nil
}

// writeValue writes the oracle's value golden when the read reads, and
// removes a stale one when it does not: a refused or damaged read owes no
// value, and a golden it left behind would be a value nobody owes.
func writeValue(path string, reads bool, text func() ([]byte, error)) error {
	if !reads {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	body, err := text()
	if err != nil {
		return fmt.Errorf("the engine's writer refused the value: %w", err)
	}
	body = oneText(body)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, body)
}

// oneText is the writer's text as a document: THE TEXT ENDS WITH ONE NEWLINE
// (§16.1), which every leg's ToJson writes and the engine's Write leaves to
// its caller.
func oneText(body []byte) []byte {
	if len(body) == 0 || body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	return body
}

// pruneValues removes a value golden no row names.
func pruneValues(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
			fmt.Printf("conformance: removed %s, which no report row names\n", e.Name())
		}
	}
	return nil
}

// hostileValue reads one hostile tree's text with the oracle and writes the
// value it leaves beside the tree.
func hostileValue(u *units, h Hostile, jsonDir string) error {
	unit, err := u.get(h.Unit)
	if err != nil {
		return err
	}
	text, err := os.ReadFile(filepath.Join(h.Tree, h.Root+".json"))
	if err != nil {
		return err
	}
	model := tabletext.NewModel(unit)
	st := model.Lookup(h.Root)
	if st == nil {
		return fmt.Errorf("root %q names no table", h.Root)
	}
	value := model.New(st)
	var report tabletext.Report
	ok := model.Read(value, text, &report)
	verdict := "refused"
	if ok {
		verdict = Counts{
			Unknown: report.Unknown,
			Widened: report.Widened,
		}.String()
	}
	if verdict != h.Verdict() {
		return fmt.Errorf("the oracle answers %s and the row says %s", verdict, h.Verdict())
	}
	out, err := outPath(jsonDir, h.Value)
	if err != nil {
		return err
	}
	return writeValue(out, !h.Refused, func() ([]byte, error) { return model.Write(value) })
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

// oracleRefusesJSONWrite loads a json-write-refusal row's wire with the oracle, applies
// its damage in code, and requires the oracle's writer to refuse the value.
func oracleRefusesJSONWrite(unit *ir.Unit, wr JSONWriteRefusal) error {
	root := unit.Tables[wr.Root]
	if root == nil {
		return fmt.Errorf("root %q names no table", wr.Root)
	}
	wire, err := os.ReadFile(wr.Wire)
	if err != nil {
		return err
	}
	model := tabletext.NewModel(unit)
	value := model.New(root)
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, value, wire, &report); !ok || err != nil {
		return fmt.Errorf("the wire does not load: %v", err)
	}
	if err := applyJSONWriteDamage(value, wr.Damage); err != nil {
		return err
	}
	if _, err := model.Write(value); err == nil {
		return fmt.Errorf("the oracle writes a text; the row owes the refusal")
	}
	return nil
}

// applyJSONWriteDamage puts into a top-level text field what no read delivers:
// ill-formed UTF-8 in a narrow field, an unpaired surrogate in a wide one.
func applyJSONWriteDamage(value *tabletext.Instance, damage string) error {
	if damage == "none" {
		return nil
	}
	kind, name, _ := strings.Cut(damage, ":")
	for i := range value.Fields {
		fv := &value.Fields[i]
		if fv.Def.Name != name {
			continue
		}
		if kind == "narrow" {
			fv.Cell.Str = []byte{0x61, 0xff, 0x62}
		} else {
			fv.Cell.Units = []uint16{0x61, 0xd800, 0x62}
		}
		return nil
	}
	return fmt.Errorf("damage %q names no field of %s", damage, value.Def.Name)
}

// outPath is where generate writes one golden. The manifest names every golden
// by its place in the corpus, under the directory that holds the corpus's text
// directory, and generate writes the same relative path under the parent of
// the jsonDir it was given. Over the corpus that is the golden's own place;
// over a test's temporary jsonDir every byte lands in the temporary tree, and
// nothing in the working tree moves.
func outPath(jsonDir, canonical string) (string, error) {
	base, err := filepath.Abs(filepath.Dir(defaultJSONDir))
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the corpus directory %s", canonical, filepath.Dir(defaultJSONDir))
	}
	return filepath.Join(filepath.Dir(jsonDir), rel), nil
}
