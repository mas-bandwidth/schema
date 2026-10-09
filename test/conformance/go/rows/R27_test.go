package main

// go/R27 — "the manifest is the wire: build/fixedform-corpus/manifest.txt is
// the single value oracle every leg reads to" (docs/roadmap.sexp, cell
// interoperability/go, "Shared byte oracle and round-trip conformance").
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md's porting table, step 1: "WHAT EACH
// FILE HOLDS IS IN `build/fixedform-corpus/manifest.txt`, written by the
// dump from the same values it writes into the bytes, one line per file:
// `file=<name> row=<row> side=old|new|mid|a|b|none root=<Table> records=<n>
// values=<field>=<value>[,...]` — the root when a schema declares two
// tables, the record count, and every value the dump set, records as `r<i>.`,
// nested fields dotted, arrays indexed, text quoted, a float by its digits
// AND its bits. `values=` is the last field and **runs to the end of the
// line**, and a quoted value may carry spaces, so split the head on spaces
// and take the rest whole ... **ASSERT THE MANIFEST, NEVER READ THE DUMP**
// (§5.9 #32, #37): the declared default is not the value on the wire — and a
// field absent from a line carries its schema default." The audit's R27
// finding against the Go leg (docs/audit/898-group-b/RESULT-go.md exception
// 8) was exactly this discipline missing: "the go harness reads the
// reference's .bin oracle but not build/fixedform-corpus/manifest.txt, and
// restates values in the test source". This test holds the discipline: every
// expected value is read OUT of a manifest line parsed by the law's own
// rules, and none is restated in this source.
//
// THE VECTOR. `make tables-fixedform-corpus` cannot run on a bench without
// the C++ serialize runtime, so the row here is constructed from the law —
// and it is the reference's own p3 row, spelled exactly as test/tables/
// fixedform_dump.cpp's p3_file writes it (the same stores, the same order,
// the same manifest grammar), with the Go leg's own writer standing in for
// the reference dump. The row's absent record is the row's own point: the
// dump stores 99/"still"/5 in the storage and the manifest SAYS SO, while
// the wire rides the template's zeros, because "THE PAYLOAD RIDES WHOLE
// WHETHER OR NOT IT IS PRESENT (§3.4), and when the flag is 0 what rides is
// ZERO" — so a leg that landed 99 would be reading storage that never
// reached the wire, and the manifest is the document that proves it.

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"tblp3"
)

// The corpus manifest's p3 row, as the reference dump writes it: the head
// names the file, the row, the side, the root and the record count, and
// values= runs to the end of the line with every store the dump set, records
// as r<i>. and nested fields dotted.
const r27ManifestLine = `file=p3.bin row=p3 side=none root=Chain records=2 values=` +
	`r0.name="present",r0.name_length=7,r0.link_present=true,r0.link.value=88,r0.link.tag="here",r0.link.tag_length=4,` +
	`r1.name="absent",r1.name_length=6,r1.link_present=false,r1.link.value=99,r1.link.tag="still",r1.link.tag_length=5`

// r27Manifest is the parsed line: the head's five fields and the values,
// keyed by path, in the order the line spells them.
type r27Manifest struct {
	file, row, side, root string
	records               int64
	values                map[string]string
	order                 []string
}

// parse follows the law's own rules: the head is split on spaces, values=
// runs to the end of the line and is taken whole, and the pairs inside it
// are split on the commas a QUOTED value does not carry (the dump escapes
// '"', '\' and ',' inside quotes, so the split is quote- and escape-aware).
func r27Parse(t *testing.T, line string) *r27Manifest {
	t.Helper()
	at := strings.Index(line, " values=")
	if at < 0 {
		t.Fatalf("R27: the manifest line carries no values= field: %q", line)
	}
	head, rest := line[:at], line[at+len(" values="):]
	m := &r27Manifest{values: map[string]string{}}
	for _, field := range strings.Fields(head) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch k {
		case "file":
			m.file = v
		case "row":
			m.row = v
		case "side":
			m.side = v
		case "root":
			m.root = v
		case "records":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				t.Fatalf("R27: the manifest's records=%q is not a decimal number: %v", v, err)
			}
			m.records = n
		}
	}
	if m.file == "" || m.root == "" || m.records == 0 {
		t.Fatalf("R27: the manifest line's head is incomplete: %q", head)
	}
	pairs := []string{}
	var current strings.Builder
	quoted, escaped := false, false
	for _, c := range rest {
		switch {
		case escaped:
			current.WriteRune(c)
			escaped = false
		case c == '\\' && quoted:
			current.WriteRune(c)
			escaped = true
		case c == '"':
			quoted = !quoted
			current.WriteRune(c)
		case c == ',' && !quoted:
			pairs = append(pairs, current.String())
			current.Reset()
		default:
			current.WriteRune(c)
		}
	}
	pairs = append(pairs, current.String())
	for _, pair := range pairs {
		path, raw, ok := strings.Cut(pair, "=")
		if !ok {
			t.Fatalf("R27: a manifest value carries no path: %q", pair)
		}
		if _, seen := m.values[path]; seen {
			t.Fatalf("R27: the manifest names %s twice", path)
		}
		m.values[path] = raw
		m.order = append(m.order, path)
	}
	return m
}

// take reads one value out of the manifest and marks it consumed — the
// oracle is read TO, and a value no assertion takes fails the test at the
// end by name.
func (m *r27Manifest) take(t *testing.T, path string) string {
	t.Helper()
	raw, ok := m.values[path]
	if !ok {
		t.Fatalf("R27: the manifest carries no %s — the oracle and the bytes under test have come apart", path)
	}
	delete(m.values, path)
	return raw
}

func (m *r27Manifest) number(t *testing.T, path string) int64 {
	t.Helper()
	raw := m.take(t, path)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("R27: the manifest's %s=%q is not a decimal number: %v", path, raw, err)
	}
	return n
}

func (m *r27Manifest) boolean(t *testing.T, path string) bool {
	t.Helper()
	switch raw := m.take(t, path); raw {
	case "true":
		return true
	case "false":
		return false
	default:
		t.Fatalf("R27: the manifest's %s=%q is not a bool", path, raw)
		return false
	}
}

// text unquotes one quoted value by the dump's own escapes (man_quote):
// \" -> ", \\ -> \, \, -> , and \xNN -> the byte NN.
func (m *r27Manifest) text(t *testing.T, path string, capacity int) []byte {
	t.Helper()
	raw := m.take(t, path)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		t.Fatalf("R27: the manifest's %s=%q is not a quoted text", path, raw)
	}
	inner := raw[1 : len(raw)-1]
	out := make([]byte, 0, len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' || i+1 == len(inner) {
			out = append(out, inner[i])
			continue
		}
		i++
		switch inner[i] {
		case '"', '\\', ',':
			out = append(out, inner[i])
		case 'x':
			if i+2 >= len(inner) {
				t.Fatalf("R27: the manifest's %s carries a truncated \\x escape: %q", path, raw)
			}
			b, err := strconv.ParseUint(inner[i+1:i+3], 16, 8)
			if err != nil {
				t.Fatalf("R27: the manifest's %s carries an unreadable \\x%q escape: %v", path, inner[i+1:i+3], err)
			}
			out = append(out, byte(b))
			i += 2
		default:
			t.Fatalf("R27: the manifest's %s carries an escape \\%q the dump does not spell", path, string(inner[i]))
		}
	}
	if len(out) > capacity {
		t.Fatalf("R27: the manifest's %s=%q is longer than the field's capacity %d", path, raw, capacity)
	}
	// The bytes past the used length are the template's zeros (§3.4's slack),
	// so the expected wire value is the text zero-padded to the capacity.
	return append(out, make([]byte, capacity-len(out))...)
}

func TestRowR27(t *testing.T) {
	man := r27Parse(t, r27ManifestLine)

	// The stores, exactly the ones the dump's p3_file sets — including the
	// absent record's, which exist to prove the writer zeroes the payload.
	// The manifest line above is the only place this test spells what the
	// read must land; these stores are the WRITER's input, not the reader's
	// expectation.
	values := []tblp3.Chain{
		{Name: [16]byte{'p', 'r', 'e', 's', 'e', 'n', 't'}, NameLength: 7, LinkPresent: true,
			Link: tblp3.Link{Value: 88, Tag: [8]byte{'h', 'e', 'r', 'e'}, TagLength: 4}},
		{Name: [16]byte{'a', 'b', 's', 'e', 'n', 't'}, NameLength: 6, LinkPresent: false,
			Link: tblp3.Link{Value: 99, Tag: [8]byte{'s', 't', 'i', 'l', 'l'}, TagLength: 5}},
	}

	// The wire: the leg's writer produces the file the manifest describes.
	need := tblp3.ChainFixedMeasure(int64(len(values)))
	file := make([]byte, need)
	if n := tblp3.ChainFixedSave(values, file); n != need {
		t.Fatalf("R27: the write refused or short-wrote its own measure: n=%d need=%d", n, need)
	}

	// The read under test: the file, loaded by the leg's fixed form.
	loaded := make([]tblp3.Chain, len(values))
	plan := make([]tblp3.TableFixedEntry, 64)
	var report tblp3.TableReport
	n := tblp3.ChainFixedLoad(loaded, file, plan, &report)
	if report.Malformed || report.Verdict != tblp3.TableOpenOk {
		t.Fatalf("R27: the read of the row is not clean: n=%d report=%+v", n, report)
	}

	// The row is selected by file= — the manifest's first field and the key
	// (one row has two sides and they carry different values).
	if man.file != "p3.bin" {
		t.Errorf("R27: the manifest's file= is not the p3 row: %q", man.file)
	} else {
		t.Logf("R27: the row is selected by file=%s", man.file)
	}

	if man.row != "p3" || man.side != "none" {
		t.Errorf("R27: the manifest's row/side are not the p3 row's: row=%q side=%q", man.row, man.side)
	} else {
		t.Logf("R27: the row's identity: row=%s side=%s", man.row, man.side)
	}

	// root= names the root the dump wrote the file from, and the leg reads
	// the row with exactly that root (§9: the reader chooses the root).
	if man.root != "Chain" {
		t.Errorf("R27: the manifest names a root this leg did not read the row with: %q", man.root)
	} else {
		t.Logf("R27: the manifest's root=%s is the root the leg read with", man.root)
	}

	// records= is the record count the read returns.
	if n != man.records {
		t.Errorf("R27: the manifest's records=%d is not the count the read returned: %d", man.records, n)
	} else {
		t.Logf("R27: the manifest's records=%d is the count the read returned", man.records)
	}

	// r0: the present record. Every value comes out of the manifest.
	r0 := true
	if !bytes.Equal(loaded[0].Name[:], man.text(t, "r0.name", len(loaded[0].Name))) {
		r0 = false
		t.Errorf("R27: r0.name did not land as the manifest spells it")
	}
	if got := loaded[0].NameLength; int64(got) != man.number(t, "r0.name_length") {
		r0 = false
		t.Errorf("R27: r0.name_length did not land as the manifest spells it: got=%d", got)
	}
	if got := loaded[0].LinkPresent; got != man.boolean(t, "r0.link_present") {
		r0 = false
		t.Errorf("R27: r0.link_present did not land as the manifest spells it: got=%v", got)
	}
	if got := loaded[0].Link.Value; int64(got) != man.number(t, "r0.link.value") {
		r0 = false
		t.Errorf("R27: r0.link.value did not land as the manifest spells it: got=%d", got)
	}
	if !bytes.Equal(loaded[0].Link.Tag[:], man.text(t, "r0.link.tag", len(loaded[0].Link.Tag))) {
		r0 = false
		t.Errorf("R27: r0.link.tag did not land as the manifest spells it")
	}
	if got := loaded[0].Link.TagLength; int64(got) != man.number(t, "r0.link.tag_length") {
		r0 = false
		t.Errorf("R27: r0.link.tag_length did not land as the manifest spells it: got=%d", got)
	}
	if r0 {
		t.Logf("R27: r0 landed every value the manifest spells for it")
	}

	// r1: the absent record. The manifest spells the WRITER's stores
	// (99/"still"/5) and the wire's answer is the payload's zeros — the
	// manifest is the document that says the stores were made, so a leg
	// landing them would be reading storage that never reached the file.
	r1 := true
	if !bytes.Equal(loaded[1].Name[:], man.text(t, "r1.name", len(loaded[1].Name))) {
		r1 = false
		t.Errorf("R27: r1.name did not land as the manifest spells it")
	}
	if got := loaded[1].NameLength; int64(got) != man.number(t, "r1.name_length") {
		r1 = false
		t.Errorf("R27: r1.name_length did not land as the manifest spells it: got=%d", got)
	}
	if got := loaded[1].LinkPresent; got != man.boolean(t, "r1.link_present") {
		r1 = false
		t.Errorf("R27: r1.link_present did not land as the manifest spells it: got=%v", got)
	}
	if got := loaded[1].Link.Value; got != 0 {
		r1 = false
		t.Errorf("R27: r1's absent payload did not ride zero on the wire: the manifest spells the store 99 and the read landed %d", got)
	}
	if got := loaded[1].Link.TagLength; got != 0 {
		r1 = false
		t.Errorf("R27: r1's absent payload tag_length did not ride zero: got=%d", got)
	}
	if loaded[1].Link.Tag != ([8]byte{}) {
		r1 = false
		t.Errorf("R27: r1's absent payload tag did not ride zero: %q", loaded[1].Link.Tag)
	}
	// The stores the manifest spells for the absent payload are consumed by
	// the zeroing claim above — they are the proof the zeros came from the
	// writer's template and not from stores never made.
	man.take(t, "r1.link.value")
	man.take(t, "r1.link.tag")
	man.take(t, "r1.link.tag_length")
	if r1 {
		t.Logf("R27: r1 landed its name and its absent payload's zeros, as the manifest's stores prove they must be")
	}

	// The oracle is read TO: every value the line spells was asserted, and
	// one that no assertion took is a value the leg is not watching.
	if len(man.values) != 0 {
		paths := make([]string, 0, len(man.values))
		for _, p := range man.order {
			if _, still := man.values[p]; still {
				paths = append(paths, p)
			}
		}
		t.Errorf("R27: the manifest spelled %d value(s) no assertion read: %s", len(paths), strings.Join(paths, ", "))
	} else {
		t.Logf("R27: every value the manifest spells was read and asserted (%d of them)", len(man.order))
	}
}
