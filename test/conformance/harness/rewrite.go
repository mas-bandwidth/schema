// THE REWRITE SURFACE (docs/SPEC-TABLES.md §22, "THE GATE"): every corpus
// input that READS is read by every leg and written back, and the bytes must be
// the canonical bytes, which the oracle writes. Glenn, 2026-11-03: "can we read
// in the data in each language, and get the valid values in" / "and we test
// this by checking that it can write out that data the same way".
//
// The `wire` surface holds a leg to this over the instances, whose wires are
// already canonical, so reading and writing one is the identity. A rewrite row
// is an input that is NOT canonical, or not written by the reader's own build:
// an evolution wire (a field this reader cannot name, a widened
// value), a framing vector that reads, and the hand-built
// non-canonical spellings of §22 (fields out of declaration order, an id table
// out of first-use order). Each leg's answer is its own Save, and the canonical
// bytes are the one byte string §22's write algorithm owes for the value the
// read leaves, so two legs that pass write identical bytes.
//
// A driver implements nothing new for it: the harness hands each leg's `wire`
// surface a second derived manifest whose instance lines are the rewrite rows,
// which is exactly "Load the wire file, Save, the bytes".
package main

import (
	"fmt"
	"path/filepath"
	"strings"
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
// the rewrite surface: the units, and one instance line per rewrite row.
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
