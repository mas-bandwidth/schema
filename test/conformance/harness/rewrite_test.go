package main

import (
	"bytes"
	"os"
	"testing"
)

// EVERY INPUT THAT READS IS A REWRITE ROW (docs/SPEC-TABLES.md §22.7): each
// report row whose pinned verdict reads and each framing row whose hand verdict
// reads is read by every leg and written back, so a row added to either list
// without its rewrite row would be an input no leg is held to writing.
func TestEveryReadingRowIsARewriteRow(t *testing.T) {
	m, pinned, _ := corpus(t)
	rows := map[string]RewriteCase{}
	for _, rw := range m.Rewrites {
		rows[rw.Name] = rw
	}
	need := func(name, unit, root, wire string) {
		rw, ok := rows[name]
		if !ok {
			t.Errorf("%s reads and has no rewrite row: add `rewrite %s %s %s %s`", name, name, unit, root, wire)
			return
		}
		if rw.Unit != unit || rw.Root != root || rw.Wire != wire {
			t.Errorf("rewrite %s reads %s %s %s, and its source row reads %s %s %s", name, rw.Unit, rw.Root, rw.Wire, unit, root, wire)
		}
	}
	for _, r := range m.Reports {
		if c := pinned[r.Name]; !c.Malformed && !c.Refused {
			need(r.Name, r.Unit, r.Root, r.Wire)
		}
	}
	for _, fc := range m.Framings {
		if !fc.Damaged && !fc.Refused {
			need(fc.Name, fc.Unit, fc.Root, fc.Wire)
		}
	}
}

// THE CANONICAL BYTES ARE THE ORACLE'S, AND A FIXED POINT: each committed
// rewrite/<case>.bin is what the oracle writes for its input today, and what it
// writes again for itself. A canonical file edited by hand, or an oracle whose
// writer moved, is red here before any leg is asked.
func TestRewriteCanonicalBytesAreTheOraclesFixedPoint(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.Rewrites) == 0 {
		t.Fatal("the manifest names no rewrite row")
	}
	for _, rw := range m.Rewrites {
		unit, err := u.get(rw.Unit)
		if err != nil {
			t.Fatal(err)
		}
		input, err := os.ReadFile(rw.Wire)
		if err != nil {
			t.Fatal(err)
		}
		pinned, err := os.ReadFile(rw.Canonical)
		if err != nil {
			t.Fatalf("%s: %v — run: make conformance-generate", rw.Name, err)
		}
		got, err := rewriteOnce(unit, rw.Root, input)
		if err != nil {
			t.Errorf("%s: %v", rw.Name, err)
			continue
		}
		if !bytes.Equal(got, pinned) {
			t.Errorf("%s: the oracle writes %d bytes, and %d are pinned", rw.Name, len(got), len(pinned))
			continue
		}
		again, err := rewriteOnce(unit, rw.Root, pinned)
		if err != nil || !bytes.Equal(again, pinned) {
			t.Errorf("%s: the canonical bytes are not a fixed point (%v)", rw.Name, err)
		}
	}
}

// A READ THAT REFUSES HAS NO REWRITE ROW (docs/SPEC-TABLES.md §22.7): the
// hand-built non-canonical spellings are form-1 grammar at a declared fixed
// root, which the strict read refuses before a field (§3.4), so the surface
// must not name them and no leg is asked to write back a value no read leaves.
func TestHandBuiltRefusedInputsHaveNoRewriteRow(t *testing.T) {
	m, _, _ := corpus(t)
	for _, name := range []string{"rewrite_fields_reversed", "rewrite_ids_reversed", "rewrite_field_twice"} {
		for _, rw := range m.Rewrites {
			if rw.Name == name {
				t.Errorf("rewrite %s names a form-1 fixed-root input that the strict read refuses", name)
			}
		}
	}
}

