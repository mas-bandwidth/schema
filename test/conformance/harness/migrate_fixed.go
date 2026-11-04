package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// migrateFixed is the explicit, one-time corpus transition from historical
// form-1 fixed instances. It never rewrites an old input: those bytes remain
// refusal vectors. New positive bytes come from the existing complete JSON.
func migrateFixed(m *Manifest, manifest, jsonDir string) error {
	u := newUnits(m)
	body, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	lines := strings.Split(string(body), "\n")
	var moves []string
	for _, inst := range m.Instances {
		if inst.NoText {
			continue
		}
		out := filepath.Join(filepath.Dir(jsonDir), "wire", inst.Name+".bin")
		// The variable WideExtent witness remains form 1 after migration.
		// Its destination, not its form byte, tells us it has already moved.
		if filepath.Clean(inst.Wire) == filepath.Clean(out) {
			continue
		}
		unit, err := u.get(inst.Unit)
		if err != nil {
			return err
		}
		model := tabletext.NewModel(unit)
		root := model.Lookup(inst.Root)
		if root == nil {
			return fmt.Errorf("%s: root missing", inst.Name)
		}
		if !root.FixedDeclared && inst.Name != "wide_blob" {
			continue
		}
		old, err := os.ReadFile(inst.Wire)
		if err != nil {
			return err
		}
		if len(old) > 0 && old[0] == 3 {
			continue
		}
		text, err := os.ReadFile(inst.JSON)
		if err != nil {
			return err
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
			return fmt.Errorf("%s: encode: %w", inst.Name, err)
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
		if root.FixedDeclared {
			lines = append(lines, fmt.Sprintf("report %s %s %s %s", refusal, inst.Unit, inst.Root, inst.Wire))
		} else {
			refusal = "oversized fixed regression (fixedroot_test.go)"
		}
		moves = append(moves, fmt.Sprintf("| %s | `%s` | `%s` | %s |", inst.Name, inst.Wire, filepath.ToSlash(out), refusal))
	}
	if err := writeFileAtomic(manifest, []byte(strings.Join(lines, "\n"))); err != nil {
		return err
	}
	if len(moves) == 0 {
		return nil
	}
	ledger := "# Single-read corpus migration\n\nEach new positive wire is encoded from the existing complete canonical JSON and read back with every value checked. Historical bytes remain unchanged.\n\n| Instance | Historical input | Positive input | Historical refusal case |\n| --- | --- | --- | --- |\n" + strings.Join(moves, "\n") + "\n"
	return writeFileAtomic(filepath.Join(filepath.Dir(jsonDir), "FIXED-MIGRATION.md"), []byte(ledger))
}
