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
// a widened value), a framing vector that reads, the hand-built non-canonical
// spellings of §22 (fields out of declaration order, an id table out of
// first-use order), and the §22.7 edge vectors. Each leg's answer is its own
// Save, and the canonical bytes are the one byte string §22's write algorithm
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

// deriveRewriteManifest writes the manifest a leg's `wire` surface reads for
// the rewrite rows: the units, and one instance line per rewrite row.
func deriveRewriteManifest(m *Manifest, path string) error {
	var b strings.Builder
	b.WriteString("# DERIVED, by `harness run` — do not edit, do not commit.\n")
	b.WriteString("# The rewrite rows of the committed manifest as instance lines, which a\n")
	b.WriteString("# driver's wire surface loads and saves (test/conformance/README.md).\n\n")
	for _, u := range m.Units {
		fmt.Fprintf(&b, "unit %s %s\n", u.Key, strings.Join(u.Paths, " "))
	}
	for _, rw := range m.Rewrites {
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
