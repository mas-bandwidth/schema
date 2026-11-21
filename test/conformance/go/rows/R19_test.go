package main

// R19, the Go leg. The law (docs/roadmap.sexp, go/R19): "a clean
// NEW-READS-OLD of an appended field, variant, arm, flag or keyed slot moves
// no counter at all".
//
// THE COMMITTED PAIR (testdata/conformance/r19): old/R19.schema is a fixed
// table Reading {x, y, z}; new/R19.schema appends one field, w = 77, and
// nothing else. new/schema.lock was written by `schema lock` over the old side
// and then over the new one, so it carries the old layout as a lineage entry
// (docs/FIXED-FORM-ALGORITHM.md §5.2). old.bin is the OLD generation's
// ReadingFixedSave of r19Values. make/go.mk generates the two sides into the
// modules tblr19old and tblr19new; both declare package tblr19, so they are
// imported here under aliases.
//
// THE READ: the NEW generation's ReadingFixedLoad over the committed old.bin.
// It must return every record, land x, y and z exactly, land w at its
// declared default 77, and move no counter: Retained, RetainLost, Widened and
// Unknown all zero, and no refusal. r19Law names every way an outcome breaks
// that; TestRowR19NegativeControl shows the law refusing outcomes that break
// it, including the real reader's refusal of a damaged file.
//
// Until 2026-11-21 this row read testdata/wire/tables/m1_open.bin through a
// hand-written header check that returned a zero report on every path, so it
// passed having read nothing; m1_open.bin is the retired form 1, which the
// corpus pins as refused for an M2 reader (testdata/conformance/tables/
// reports.txt m1_open_as_m2), and M1 -> M2 is not an appended-field pair.
//
// Run: cd test/conformance/go && go test ./rows/ -run '^TestRowR19' -count=1

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	r19new "tblr19new"
	r19old "tblr19old"
)

// r19Values are the records the OLD writer wrote into old.bin.
var r19Values = []r19old.Reading{
	{X: 11, Y: 22, Z: 33},
	{X: -4, Y: 5, Z: -2147483648},
}

// r19Outcome is what one NEW read of a file returned.
type r19Outcome struct {
	n      int64
	back   []r19new.Reading
	report r19new.TableReport
}

// r19Read reads wire with the NEW generation. Every slot of back is poisoned
// first, so a field the reader does not write cannot pass as a landed zero or
// a landed default.
func r19Read(wire []byte) r19Outcome {
	o := r19Outcome{back: make([]r19new.Reading, len(r19Values))}
	for i := range o.back {
		o.back[i] = r19new.Reading{X: -1, Y: -1, Z: -1, W: -1}
	}
	o.n = r19new.ReadingFixedLoad(o.back, wire, make([]r19new.TableFixedEntry, 64), &o.report)
	return o
}

// r19Law returns every way o breaks R19's law for the committed pair; an empty
// result is a clean NEW-READS-OLD.
func r19Law(o r19Outcome) []string {
	var broken []string
	r := o.report
	if o.n != int64(len(r19Values)) || r.Refused() {
		broken = append(broken, fmt.Sprintf("the NEW reader did not read the OLD file: n=%d want %d, refusal %q", o.n, len(r19Values), r.Reason))
	}
	if r.Retained != 0 || r.RetainLost != 0 || r.Widened != 0 || r.Unknown != 0 {
		broken = append(broken, fmt.Sprintf("a clean append moved a counter: retained=%d retain_lost=%d widened=%d unknown=%d", r.Retained, r.RetainLost, r.Widened, r.Unknown))
	}
	for i, want := range r19Values {
		if i >= len(o.back) {
			broken = append(broken, fmt.Sprintf("record %d: no slot to land in", i))
			continue
		}
		got := o.back[i]
		if got.X != want.X || got.Y != want.Y || got.Z != want.Z {
			broken = append(broken, fmt.Sprintf("record %d: the writer's fields did not land: got x=%d y=%d z=%d, want x=%d y=%d z=%d", i, got.X, got.Y, got.Z, want.X, want.Y, want.Z))
		}
		if got.W != 77 {
			broken = append(broken, fmt.Sprintf("record %d: the appended field is not its declared default: w=%d, want 77", i, got.W))
		}
	}
	return broken
}

// r19Wire reads the committed OLD file.
func r19Wire(t *testing.T) []byte {
	t.Helper()
	// rows/ -> go/ -> conformance/ -> test/ -> repo, then into testdata.
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "conformance", "r19", "old.bin"))
	if err != nil {
		t.Fatalf("cannot read the committed OLD file: %v", err)
	}
	return wire
}

func TestRowR19(t *testing.T) {
	wire := r19Wire(t)

	// The committed file is the OLD generation's own: its writer, given the
	// same records, writes it byte for byte. Without this the row could pass
	// over a file the new side wrote.
	saved := make([]byte, r19old.ReadingFixedMeasure(int64(len(r19Values))))
	if n := r19old.ReadingFixedSave(r19Values, saved); n != int64(len(saved)) {
		t.Fatalf("the OLD writer did not write its measure: n=%d want %d", n, len(saved))
	}
	if !bytes.Equal(saved, wire) {
		t.Fatalf("testdata/conformance/r19/old.bin is not the OLD writer's file:\n got %x\nwant %x", wire, saved)
	}
	// The NEW generation's own file is a different layout, so the read below
	// crosses the lineage rather than matching the reader's own hash.
	own := make([]byte, r19new.ReadingFixedMeasure(int64(len(r19Values))))
	if len(own) == len(wire) {
		t.Fatalf("the NEW layout measures %d bytes, the OLD file's own size: the pair appends nothing", len(own))
	}

	for _, broken := range r19Law(r19Read(wire)) {
		t.Error(broken)
	}
}

// TestRowR19NegativeControl shows the row can go red. The law refuses
// outcomes that break it, one per clause, and the real NEW reader refuses the
// committed file once it is damaged, which the law then names.
func TestRowR19NegativeControl(t *testing.T) {
	clean := func() r19Outcome {
		o := r19Outcome{n: int64(len(r19Values)), back: make([]r19new.Reading, len(r19Values))}
		for i, v := range r19Values {
			o.back[i] = r19new.Reading{X: v.X, Y: v.Y, Z: v.Z, W: 77}
		}
		return o
	}
	if broken := r19Law(clean()); len(broken) != 0 {
		t.Fatalf("the law refused a clean outcome: %v", broken)
	}
	breaks := map[string]func(*r19Outcome){
		"refused":            func(o *r19Outcome) { o.n = -1; o.report.Refuse(r19new.TableRefuseLayoutUnknown, "") },
		"short":              func(o *r19Outcome) { o.n = 1 },
		"retained":           func(o *r19Outcome) { o.report.Retained = 1 },
		"retain_lost":        func(o *r19Outcome) { o.report.RetainLost = 1 },
		"widened":            func(o *r19Outcome) { o.report.Widened = 1 },
		"unknown":            func(o *r19Outcome) { o.report.Unknown = 1 },
		"old field lost":     func(o *r19Outcome) { o.back[1].Z = 0 },
		"append zero-filled": func(o *r19Outcome) { o.back[0].W = 0 },
	}
	for name, breakIt := range breaks {
		o := clean()
		breakIt(&o)
		if len(r19Law(o)) == 0 {
			t.Errorf("%s: the law passed an outcome that breaks it", name)
		}
	}

	// The real reader over the committed file with its last byte cut off: a
	// short file is refused, and the law names that read as broken.
	wire := r19Wire(t)
	o := r19Read(wire[:len(wire)-1])
	if !o.report.Refused() {
		t.Fatalf("the NEW reader read a truncated OLD file: n=%d %+v", o.n, o.report)
	}
	if len(r19Law(o)) == 0 {
		t.Fatalf("the law passed the NEW reader's refusal of a truncated file: %+v", o.report)
	}
}
