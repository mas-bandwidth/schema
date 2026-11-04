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

// migrateVerdicts explicitly records the strict oracle's admission of unchanged
// hostile/framing inputs. Accepted framing values must still match their
// independently authored goldens; this operation cannot repin a changed value.
func migrateVerdicts(m *Manifest, manifest, jsonDir string) error {
	u := newUnits(m)
	body, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	lines := strings.Split(string(body), "\n")
	verdicts := map[string]string{}
	for _, h := range m.Hostiles {
		unit, err := u.get(h.Unit)
		if err != nil {
			return err
		}
		model := tabletext.NewModel(unit)
		root := model.Lookup(h.Root)
		if root == nil {
			return fmt.Errorf("%s: root missing", h.Name)
		}
		data, err := os.ReadFile(filepath.Join(h.Tree, h.Root+".json"))
		if err != nil {
			return err
		}
		var report tabletext.Report
		verdict := "refused"
		if model.Read(model.New(root), data, &report) && !report.Refused {
			verdict = "read"
		}
		verdicts["json-hostile/"+h.Name] = verdict
	}
	for _, f := range m.Framings {
		unit, err := u.get(f.Unit)
		if err != nil {
			return err
		}
		model := tabletext.NewModel(unit)
		root := model.Lookup(f.Root)
		if root == nil {
			return fmt.Errorf("%s: root missing", f.Name)
		}
		data, err := os.ReadFile(f.Wire)
		if err != nil {
			return err
		}
		value := model.New(root)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, value, data, &report)
		if err != nil && !tablewire.ReadRefusal(err) {
			return err
		}
		verdict := "refused"
		if ok && !report.Refused {
			verdict = "read"
			want, err := os.ReadFile(f.Value)
			if err != nil {
				return fmt.Errorf("%s: accepted input has no complete expected value: %w", f.Name, err)
			}
			got, err := model.Write(value)
			if err != nil || !bytes.Equal(oneText(got), oneText(want)) {
				return fmt.Errorf("%s: accepted value changed: %v", f.Name, err)
			}
		}
		verdicts["framing/"+f.Name] = verdict
	}
	var changes []string
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		verdict, ok := verdicts[f[0]+"/"+f[1]]
		if !ok {
			continue
		}
		old := f[5]
		f[5] = verdict
		if f[0] == "json-hostile" {
			f = f[:6]
		}
		if f[0] == "framing" && verdict == "refused" {
			f[6] = "-"
		}
		lines[i] = strings.Join(f, " ")
		if old != verdict {
			changes = append(changes, fmt.Sprintf("| %s | %s | `%s` | %s |", f[0], f[1], old, verdict))
		}
	}
	if err := writeFileAtomic(manifest, []byte(strings.Join(lines, "\n"))); err != nil {
		return err
	}
	ledger := "# Single-read verdict migration\n\nInputs are unchanged. Accepted framing values were checked against their existing authored goldens. Counter tuples are retired; refused inputs owe no value.\n\n| Surface | Case | Previous expectation | Admission |\n| --- | --- | --- | --- |\n" + strings.Join(changes, "\n") + "\n"
	return writeFileAtomic(filepath.Join(filepath.Dir(jsonDir), "VERDICT-MIGRATION.md"), []byte(ledger))
}
