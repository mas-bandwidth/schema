// THE HAND-AUTHORED TEXTS OF THE JSON-READ SURFACE (docs/SPEC-TABLES.md
// §22.7, "THE GATE"): json-read holds every leg to an instance's wire after
// reading the instance's text, and a text the oracle wrote is already in
// canonical order, so a leg whose builder keeps INSERTION order still passes
// every instance. A json-text row is a text a person wrote OUT of canonical
// order (map keys reversed, by UTF-16 rather than by bytes), so the leg's
// BUILDER sort is what lands the wire's bytes. A json-text row is scored on
// the json-read surface under its own name, against the wire it names.
package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// JSONTextCase is one json-text row: a hand-authored text at
// json/<name>.json, the unit and root it reads as, and the canonical wire a
// leg's Save of the value must write.
type JSONTextCase struct {
	Name string
	Unit string
	Root string
	Wire string
	Text string
}

// jsonReadManifest is the text a leg's json-read surface runs on: the leg's
// own derived manifest, then one instance line per json-text row on a unit
// the leg is asked (omit names the opt-in units it does not list). The
// driver reads each instance's text from json/<name>.json, which for a
// json-text row is the hand-authored text. Only the json-read surface runs on
// it: on the other surfaces a json-text row is not a case.
func jsonReadManifest(legDerived string, m *Manifest, omit map[string]bool) string {
	var b strings.Builder
	b.WriteString(legDerived)
	if !strings.HasSuffix(legDerived, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# the json-text rows (jsontext.go): hand-authored texts, read on json-read alone\n")
	for _, jt := range m.JSONTexts {
		if omit[jt.Unit] {
			continue
		}
		fmt.Fprintf(&b, "instance %s %s %s %s\n", jt.Name, jt.Unit, jt.Root, jt.Wire)
	}
	return b.String()
}

// deriveJSONReadManifest writes jsonReadManifest beside the leg's derived
// manifest.
func deriveJSONReadManifest(legDerived, path string, m *Manifest, omit map[string]bool) error {
	text, err := os.ReadFile(legDerived)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(jsonReadManifest(string(text), m, omit)))
}

// checkJSONText holds one json-text row to what makes it a trap: the oracle's
// strict read takes the text, the value it leaves writes EXACTLY the wire the
// row names, and the text is not the canonical text of that value (a text
// already in canonical order tests nothing an instance does not).
func checkJSONText(u *units, jt JSONTextCase) error {
	unit, err := u.get(jt.Unit)
	if err != nil {
		return err
	}
	text, err := os.ReadFile(jt.Text)
	if err != nil {
		return fmt.Errorf("json-text %s: %w", jt.Name, err)
	}
	wire, err := os.ReadFile(jt.Wire)
	if err != nil {
		return fmt.Errorf("json-text %s: %w", jt.Name, err)
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup(jt.Root)
	if def == nil {
		return fmt.Errorf("json-text %s: unit %s declares no table %s", jt.Name, jt.Unit, jt.Root)
	}
	value := model.New(def)
	var report tabletext.Report
	if !strictReadJSON(model, value, text, &report) {
		return fmt.Errorf("json-text %s: the oracle's strict read refuses the text: %+v", jt.Name, report)
	}
	saved, err := tablewire.Encode(model, value)
	if err != nil {
		return fmt.Errorf("json-text %s: write: %w", jt.Name, err)
	}
	if !bytes.Equal(saved, wire) {
		return fmt.Errorf("json-text %s: the text reads to a value that writes %d bytes, not the %d bytes of %s", jt.Name, len(saved), len(wire), jt.Wire)
	}
	canonical, err := model.Write(value)
	if err != nil {
		return fmt.Errorf("json-text %s: canonical text: %w", jt.Name, err)
	}
	if bytes.Equal(oneText(canonical), text) {
		return fmt.Errorf("json-text %s: the text is already the canonical text of its value, so it tests no builder sort", jt.Name)
	}
	return nil
}
