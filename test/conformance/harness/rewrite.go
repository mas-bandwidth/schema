// THE REWRITE SURFACE (docs/SPEC-TABLES.md §22, "THE GATE"): every corpus
// input that READS is read by every leg and written back, and the bytes must be
// the canonical bytes, which the oracle writes. Glenn, 2026-11-03: "can we read
// in the data in each language, and get the valid values in" / "and we test
// this by checking that it can write out that data the same way".
//
// The `wire` surface holds a leg to this over the instances, whose wires are
// already canonical, so reading and writing one is the identity. A rewrite row
// is an input that is NOT canonical, or not written by the reader's own build,
// or an edge vector of §22: an evolution wire (a field this reader cannot name,
// a widened value), a framing vector that reads, a lawful field reordering
// whose ID table remains in first-use order, and the §22.7 edge vectors.
// Non-first-use ID tables and repeated fields refuse and have no rewrite row.
// Each leg's answer is its own Save, and the canonical bytes are the one byte
// string §22's write algorithm
// owes for the value the read leaves, so two legs that pass write identical
// bytes.
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

// RewriteCase is one rewrite row: the input wire, the reader's unit and root,
// and where the canonical bytes live.
type RewriteCase struct {
	Name      string
	Unit      string
	Root      string
	Wire      string
	Canonical string
}

// RewriteCanonical is where a rewrite row's canonical bytes live.
func RewriteCanonical(jsonDir, name string) string {
	return filepath.Join(filepath.Dir(jsonDir), "rewrite", name+".bin")
}

// framedRewrites names the FRAMED rewrite rows, each with the framing row it
// is read as. A rewrite row is framed when its input is the VARIABLE form
// (form 1) of a FIXED-declared root: no read at a fixed root reads form 1
// (docs/SPEC-TABLES.md §3.4, `previous_form`), so a leg's wire surface refuses
// it, and the corpus reads such an input only through the root's variable
// twin, on the framing surface (§3.4's framing battery: "`framing` reads at
// the variable twin and reads"). The oracle writes its canonical bytes the
// same way (rewriteOnce). A framed row's answer is the leg's framing
// write-back (`<case>.wire`), held to the canonical bytes, and
// generateRewrites requires the framing row's save-wire to be those bytes. A
// framed row with no framing row that reads cannot be answered, and is an
// error by name. Every other rewrite row, a variable root's included, stays
// on the wire surface.
func framedRewrites(u *units, m *Manifest) (map[string]FramingCase, error) {
	framed := map[string]FramingCase{}
	for _, rw := range m.Rewrites {
		input, err := os.ReadFile(rw.Wire)
		if err != nil {
			return nil, fmt.Errorf("rewrite %s: %w", rw.Name, err)
		}
		if len(input) == 0 || input[0] != ir.TableWireForm {
			continue
		}
		unit, err := u.get(rw.Unit)
		if err != nil {
			return nil, fmt.Errorf("rewrite %s: %w", rw.Name, err)
		}
		st := unit.Tables[rw.Root]
		if st == nil || !st.FixedDeclared {
			continue
		}
		found := false
		for _, fc := range m.Framings {
			if fc.Name == rw.Name && !fc.Refused {
				framed[rw.Name] = fc
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("rewrite %s: a form-1 input on the fixed root %s.%s, which no framing row of that name reads: no surface can answer it", rw.Name, rw.Unit, rw.Root)
		}
	}
	return framed, nil
}

// framingAnswer is where a leg's framing surface leaves its write-back of a
// row: `<case>.wire` beside the verdict.
func framingAnswer(framingOut, name string) string {
	return filepath.Join(framingOut, name+".wire")
}

// answerFramedRewrites puts each framed rewrite row's answer in the rewrite
// surface's output: the leg's own framing write-back of that input. A row the
// leg's framing surface did not write back is left unanswered, and is scored
// so.
func answerFramedRewrites(framed map[string]FramingCase, framingOut, rewriteOut string) error {
	for name := range framed {
		saved, err := os.ReadFile(framingAnswer(framingOut, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(rewriteOut, name), saved, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// deriveRewriteManifest writes the manifest a leg's `wire` surface reads for
// the rewrite rows: the units, and one instance line per rewrite row, but for
// the rows on a unit in omit (an opt-in unit the leg does not name) and the
// framed rows, which the leg answers on its framing surface (framedRewrites).
// The run scores the leg's answers as the `rewrite` surface (run.go).
func deriveRewriteManifest(m *Manifest, path string, omit map[string]bool, framed map[string]FramingCase) error {
	var b strings.Builder
	b.WriteString("# DERIVED, by `harness run` — do not edit, do not commit.\n")
	b.WriteString("# The rewrite rows of the committed manifest as instance lines, which a\n")
	b.WriteString("# driver's wire surface loads and saves (test/conformance/README.md).\n\n")
	for _, u := range m.Units {
		fmt.Fprintf(&b, "unit %s %s\n", u.Key, strings.Join(u.Paths, " "))
	}
	for _, rw := range m.Rewrites {
		if omit[rw.Unit] {
			continue
		}
		if _, ok := framed[rw.Name]; ok {
			continue
		}
		fmt.Fprintf(&b, "instance %s %s %s %s no-text\n", rw.Name, rw.Unit, rw.Root, rw.Wire)
	}
	return writeFileAtomic(path, []byte(b.String()))
}

// generateRewrites writes every rewrite row's canonical bytes and removes a
// canonical file no row names.
func generateRewrites(u *units, m *Manifest, jsonDir string) error {
	if len(m.Rewrites) == 0 {
		return nil
	}
	framed, err := framedRewrites(u, m)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	dir := ""
	for _, rw := range m.Rewrites {
		out, err := outPath(jsonDir, rw.Canonical)
		if err != nil {
			return err
		}
		dir = filepath.Dir(out)
		input, err := os.ReadFile(rw.Wire)
		if err != nil {
			return fmt.Errorf("rewrite %s: %w", rw.Name, err)
		}
		canonical, err := rewriteOnce(u, rw.Unit, rw.Root, input)
		if err != nil {
			return fmt.Errorf("rewrite %s: the input: %w", rw.Name, err)
		}
		again, err := rewriteOnce(u, rw.Unit, rw.Root, canonical)
		if err != nil {
			return fmt.Errorf("rewrite %s: the canonical bytes: %w", rw.Name, err)
		}
		if !bytes.Equal(again, canonical) {
			return fmt.Errorf("rewrite %s: the canonical bytes are not a fixed point: %d bytes written, %d written again", rw.Name, len(canonical), len(again))
		}
		if fc, ok := framed[rw.Name]; ok {
			// a framed row is answered by the framing write-back, which the
			// framing surface holds to the save-wire: the two must be one
			// byte string, or the leg would owe two answers for one read
			save, err := os.ReadFile(fc.Save)
			if err != nil {
				return fmt.Errorf("rewrite %s: the framing row's save-wire: %w", rw.Name, err)
			}
			if !bytes.Equal(save, canonical) {
				return fmt.Errorf("rewrite %s: the framing row's save-wire (%d bytes) is not the canonical bytes (%d): one read owes one write-back", rw.Name, len(save), len(canonical))
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(out, canonical); err != nil {
			return err
		}
		keep[filepath.Base(out)] = true
		fmt.Printf("conformance: %-22s %7d B in  %7d B canonical\n", rw.Name, len(input), len(canonical))
	}
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".bin") || keep[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
		fmt.Printf("conformance: removed rewrite/%s, which no rewrite row names\n", e.Name())
	}
	return nil
}

// rewriteOnce reads data with the oracle's strict read and writes the value
// back in the root's canonical form.
func rewriteOnce(u *units, unitKey, rootName string, data []byte) ([]byte, error) {
	unit, err := u.get(unitKey)
	if err != nil {
		return nil, err
	}
	var fixed *ir.Struct
	if st := unit.Tables[rootName]; st != nil && st.FixedDeclared && len(data) > 0 && data[0] == ir.TableWireForm {
		unit, err = framingVariableUnit(u.m, unitKey, rootName)
		if err != nil {
			return nil, err
		}
		fixed = unit.Tables[rootName]
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup(rootName)
	if def == nil {
		return nil, fmt.Errorf("unit %s declares no table %s", unitKey, rootName)
	}
	value := model.New(def)
	var report tabletext.Report
	ok, err := strictDecode(model, value, data, &report)
	if err != nil {
		return nil, err
	}
	if !ok || report.Refused {
		return nil, fmt.Errorf("the read does not read (refused %t): a rewrite row's input must", report.Refused)
	}
	if fixed != nil {
		fixed.FixedDeclared = true
		defer func() { fixed.FixedDeclared = false }()
	}
	return tablewire.Encode(model, value)
}
