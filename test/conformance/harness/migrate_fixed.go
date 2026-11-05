package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// migrateFixed is the explicit, one-time corpus transition from historical
// form-1 fixed instances. It never rewrites an old input: those bytes remain
// refusal vectors. New positive bytes come from the existing complete JSON.
//
// A declared fixed root above the form-3 ceiling has NO FILE FORM
// (docs/SPEC-TABLES.md §3.4): its file save refuses and a read at that root
// refuses form 1. Such an instance cannot stay a positive input, so its
// instance row is retired and its historical bytes become a refusal row like
// every other form-1 file at a fixed root, named in the ledger. Every
// cook-write row that reaches the instance is retired with it, since a save
// with no file form has no cooked bytes to pin, and its two cooked files go.
//
// A framing vector is a form-1 file. At a declared fixed root the rule
// answers it by its form byte before any framing is read (docs/rulings.json
// fixed-root-refuses-form1), so its row owes `refused` and no value; the
// bytes are kept, and a value file no row names any longer is removed.
func migrateFixed(m *Manifest, manifest, jsonDir string) error {
	u := newUnits(m)
	body, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	lines := strings.Split(string(body), "\n")
	retired := map[int]bool{}
	var moves, cooked []string
	for _, inst := range m.Instances {
		unit, err := u.get(inst.Unit)
		if err != nil {
			return err
		}
		model := tabletext.NewModel(unit)
		root := model.Lookup(inst.Root)
		if root == nil {
			return fmt.Errorf("%s: root missing", inst.Name)
		}
		if !root.FixedDeclared {
			continue
		}
		old, err := os.ReadFile(inst.Wire)
		if err != nil {
			return err
		}
		if len(old) > 0 && old[0] == 3 {
			continue
		}
		if !ir.TableFixedEmitted(unit, root) {
			for i, line := range lines {
				f := strings.Fields(line)
				if len(f) >= 5 && f[0] == "instance" && f[1] == inst.Name {
					retired[i] = true
				}
				if len(f) == 4 && f[0] == "cook-write" && f[1] == inst.Name {
					retired[i] = true
				}
			}
			for _, cw := range m.CookWrites {
				if cw.Instance == inst.Name {
					cooked = append(cooked, cw.Little, cw.Big)
				}
			}
			refusal := "previous_form_" + inst.Name
			lines = append(lines, fmt.Sprintf("report %s %s %s %s", refusal, inst.Unit, inst.Root, inst.Wire))
			moves = append(moves, fmt.Sprintf("| %s | `%s` | (none: no file form) | %s |", inst.Name, inst.Wire, refusal))
			continue
		}
		if inst.NoText {
			return fmt.Errorf("%s: a wire-only form-1 instance at a fixed root with a file form has no JSON to encode form 3 from", inst.Name)
		}
		text, err := os.ReadFile(inst.JSON)
		if err != nil {
			return fmt.Errorf("%s: %w", inst.Name, err)
		}
		value := model.New(root)
		var report tabletext.Report
		if !model.Read(value, text, &report) || report.Refused {
			return fmt.Errorf("%s: source JSON refuses: %+v", inst.Name, report)
		}
		canonical, err := model.Write(value)
		if err != nil || !bytes.Equal(oneText(canonical), oneText(text)) {
			return fmt.Errorf("%s: source JSON is not complete canonical text: %v", inst.Name, err)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			// A fixed root past the form-3 ceiling has no file form; its
			// historical bytes stay where they are and the ledger says so.
			moves = append(moves, fmt.Sprintf("| %s | `%s` | (none) | %v |", inst.Name, inst.Wire, err))
			continue
		}
		back := model.New(root)
		ok, err := tablewire.Decode(model, back, wire, &report)
		if err != nil || !ok || report.Refused {
			return fmt.Errorf("%s: new wire refuses: %v", inst.Name, err)
		}
		textBack, err := model.Write(back)
		if err != nil || !bytes.Equal(oneText(textBack), oneText(text)) {
			return fmt.Errorf("%s: new wire changed a value: %v", inst.Name, err)
		}
		out := filepath.Join(filepath.Dir(jsonDir), "wire", inst.Name+".bin")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(out, wire); err != nil {
			return err
		}
		for i, line := range lines {
			f := strings.Fields(line)
			if len(f) >= 5 && f[0] == "instance" && f[1] == inst.Name {
				lines[i] = strings.Replace(line, inst.Wire, filepath.ToSlash(out), 1)
			}
		}
		refusal := "previous_form_" + inst.Name
		lines = append(lines, fmt.Sprintf("report %s %s %s %s", refusal, inst.Unit, inst.Root, inst.Wire))
		moves = append(moves, fmt.Sprintf("| %s | `%s` | `%s` | %s |", inst.Name, inst.Wire, filepath.ToSlash(out), refusal))
	}
	framingValues, err := migrateFraming(m, u, lines)
	if err != nil {
		return err
	}
	kept := lines[:0]
	for i, line := range lines {
		if !retired[i] {
			kept = append(kept, line)
		}
	}
	if err := writeFileAtomic(manifest, []byte(strings.Join(kept, "\n"))); err != nil {
		return err
	}
	if err := pruneFramingValues(framingValues); err != nil {
		return err
	}
	for _, path := range cooked {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if len(moves) == 0 {
		return nil
	}
	ledgerPath := filepath.Join(filepath.Dir(jsonDir), "FIXED-MIGRATION.md")
	moves = mergeLedger(ledgerPath, moves)
	ledger := "# Single-read corpus migration\n\nEach new positive wire is encoded from the existing complete canonical JSON and read back with every value checked. Historical bytes remain unchanged and become `previous_form` refusal rows.\n\n| Instance | Historical input | Positive input | Historical refusal case |\n| --- | --- | --- | --- |\n" + strings.Join(moves, "\n") + "\n"
	return writeFileAtomic(ledgerPath, []byte(ledger))
}

// migrateFraming rewrites, in place, every framing row whose root is a
// declared fixed table and whose file carries a form byte other than 3 to the verdict the rule
// owes it, `refused` with no value. It returns every value file a row named
// before, true where a row still names it, so the ones no row names any
// longer can be removed.
func migrateFraming(m *Manifest, u *units, lines []string) (map[string]bool, error) {
	named := map[string]bool{}
	for _, fc := range m.Framings {
		if fc.Value != "" && !named[fc.Value] {
			named[fc.Value] = false
		}
		unit, err := u.get(fc.Unit)
		if err != nil {
			return nil, err
		}
		root := tabletext.NewModel(unit).Lookup(fc.Root)
		if root == nil {
			return nil, fmt.Errorf("%s: root missing", fc.Name)
		}
		if !root.FixedDeclared || fc.Refused {
			continue
		}
		wire, err := os.ReadFile(fc.Wire)
		if err != nil {
			return nil, err
		}
		// an empty file has no form byte to refuse by: it is damage, and its
		// row keeps the verdict it owes
		if len(wire) == 0 || wire[0] == 3 {
			continue
		}
		for i, line := range lines {
			f := strings.Fields(line)
			if len(f) != 7 || f[0] != "framing" || f[1] != fc.Name {
				continue
			}
			at := strings.Index(line, f[4]) + len(f[4])
			verdict := strings.Index(line[at:], f[5])
			lines[i] = line[:at+verdict] + fmt.Sprintf("%-20s -", "refused")
		}
	}
	for _, fc := range m.Framings {
		if fc.Value != "" && !rewritten(lines, fc.Name) {
			named[fc.Value] = true
		}
	}
	return named, nil
}

// rewritten reports whether the framing row of the case now owes `refused`.
func rewritten(lines []string, name string) bool {
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) == 7 && f[0] == "framing" && f[1] == name {
			return f[5] == "refused"
		}
	}
	return false
}

// pruneFramingValues removes the value files a framing row named before the
// migration and no framing row names after it.
func pruneFramingValues(named map[string]bool) error {
	for path, still := range named {
		if still {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// mergeLedger keeps the ledger's earlier rows, in their order, and adds the
// rows of this run, a row of this run replacing an earlier one of the same
// instance.
func mergeLedger(path string, moves []string) []string {
	name := func(row string) string {
		f := strings.Split(row, "|")
		if len(f) < 2 {
			return ""
		}
		return strings.TrimSpace(f[1])
	}
	fresh := map[string]string{}
	for _, row := range moves {
		fresh[name(row)] = row
	}
	var out []string
	if body, err := os.ReadFile(path); err == nil {
		for _, row := range strings.Split(string(body), "\n") {
			n := name(row)
			if !strings.HasPrefix(row, "| ") || n == "Instance" || strings.HasPrefix(row, "| ---") {
				continue
			}
			if r, ok := fresh[n]; ok {
				out = append(out, r)
				delete(fresh, n)
				continue
			}
			out = append(out, row)
		}
	}
	for _, row := range moves {
		if _, ok := fresh[name(row)]; ok {
			out = append(out, row)
		}
	}
	return out
}
