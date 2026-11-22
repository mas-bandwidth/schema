// THE DISCRIMINATION PROOF: a row that cannot fail is not credited.
//
// A must-refuse row passes when the leg refuses. A leg that refuses the whole
// FORM — reads a fixed root through the variable reader, refuses every file of
// a root, has no reader for the construct at all — refuses the row too, and
// for the wrong reason: it never reached the rule the row was written for. Its
// refusal is the same answer it gives the LAWFUL value, so it proves nothing.
//
// The proof is the lawful twin. Every must-refuse row belongs to a GROUP (the
// same surface, unit and root; for the write contract also the same field
// path; for the foreign and forgery surfaces the image the forgery is made
// from), and the group holds the rows that must be ACCEPTED: read, wrote,
// open. Two rules follow:
//
//  1. THE CORPUS (static): every must-refuse row has at least one lawful twin
//     in the corpus. A group of refusals alone is a set of rows a leg passes
//     by refusing everything, so `harness run` refuses to run on it and the
//     unit test names the row.
//  2. EACH LEG (scored): a must-refuse row the leg refused is CREDITED only
//     when the same leg also ANSWERED at least one of its lawful twins
//     correctly. Otherwise the cell counts it as refusal-only (`+Nr`), it is
//     never a pass, and the matrix prints a REFUSAL-ONLY block naming the leg
//     and surface. For the reference leg it is a failure, as an absence is.
//
// docs/SPEC-CONFORMANCE.md §4 states the scoring, and
// test/conformance/README.md the contract.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// caseRef names one case on one surface: the file name the driver writes.
type caseRef struct{ surface, name string }

// discrimination maps every must-refuse case to its group, and every group to
// the lawful cases that prove a leg reads the form the refusals test.
type discrimination struct {
	refusal map[caseRef]string
	lawful  map[string][]caseRef
}

func (d *discrimination) refuse(surface, name, key string) {
	d.refusal[caseRef{surface, name}] = key
}

func (d *discrimination) accept(surface, name, key string) {
	d.lawful[key] = append(d.lawful[key], caseRef{surface, name})
}

// buildDiscrimination reads the manifest (and the generated report verdicts)
// into refusal rows and their lawful twins.
func buildDiscrimination(m *Manifest, reports map[string]string) (*discrimination, error) {
	d := &discrimination{refusal: map[caseRef]string{}, lawful: map[string][]caseRef{}}
	at := func(parts ...string) string { return strings.Join(parts, "|") }

	for _, i := range m.Instances {
		key := at("wire", i.Unit, i.Root)
		if i.WireRefused {
			d.refuse("wire", i.Name, key)
		} else {
			d.accept("wire", i.Name, key)
		}
		if !i.NoText {
			d.accept("json-write", i.Name+".json", at("json-write", i.Unit, i.Root))
		}
	}
	for _, wr := range m.JSONWriteRefusals {
		d.refuse("json-write", wr.Name+".json", at("json-write", wr.Unit, wr.Root))
	}

	// THE MESSAGE FORM: a connection's lawful batch is the twin of every
	// refusal against that connection (a variable root's named refusal, a
	// malformed grown announcement, a damaged batch).
	messageConn := map[string]string{}
	for _, msg := range m.Messages {
		messageConn[msg.Name] = msg.Connection
		key := at("message", msg.Connection)
		if msg.Refused() {
			d.refuse("message", msg.Name, key)
		} else {
			d.accept("message", msg.Name, key)
		}
	}
	for _, g := range m.Grown {
		key := at("message", messageConn[g.Message])
		if g.Refused {
			d.refuse("message", g.Name, key)
		} else {
			d.accept("message", g.Name, key)
		}
	}
	for _, b := range m.Batches {
		key := at("message", b.Connection)
		if b.Verdict == "refused" {
			d.refuse("message", b.Name, key)
		} else {
			d.accept("message", b.Name, key)
		}
	}

	for _, r := range m.Reports {
		verdict, ok := reports[r.Name]
		if !ok {
			return nil, fmt.Errorf("reports.txt names no case %q — run: make conformance-generate", r.Name)
		}
		key := at("report", r.Unit, r.Root)
		if verdict == "refused" {
			d.refuse("report", r.Name, key)
		} else {
			d.accept("report", r.Name, key)
		}
	}
	for _, fc := range m.Framings {
		key := at("framing", fc.Unit, fc.Root)
		if fc.Refused {
			d.refuse("framing", fc.Name, key)
		} else {
			d.accept("framing", fc.Name, key)
		}
	}
	for _, rc := range m.Retains {
		key := at("retain", rc.Unit, rc.Root)
		if rc.Refused {
			d.refuse("retain", rc.Name, key)
		} else {
			d.accept("retain", rc.Name, key)
		}
	}
	for _, h := range m.Hostiles {
		key := at("json-hostile", h.Unit, h.Root)
		if h.Refused {
			d.refuse("json-hostile", h.Name, key)
		} else {
			d.accept("json-hostile", h.Name, key)
		}
	}

	// THE FOREIGN ORDER and THE FORGERIES: the twin is the image they are
	// made from, opened on its own surface, and any forgery of the same
	// subject that must open.
	for _, b := range m.Blocks {
		d.accept("block", b.Name, at("block", b.Name))
		d.refuse("block-foreign", b.Name, at("block", b.Name))
	}
	cookRoots := map[string][]string{}
	for _, c := range m.Cooks {
		d.accept("cook", c.Case, at("cook", c.Case))
		d.refuse("cook-foreign", c.Case, at("cook", c.Case))
		cookRoots[c.Root] = append(cookRoots[c.Root], c.Case)
	}
	for _, f := range m.Forgeries {
		surface, key := "forgery", at("block", f.Subject)
		if f.Kind == "cook" {
			surface, key = "cook-forgery", at("cook-forgery", f.Subject)
		}
		if f.Verdict == "refuse" {
			d.refuse(surface, f.Name, key)
		} else {
			d.accept(surface, f.Name, key)
		}
	}
	for root, cases := range cookRoots {
		for _, c := range cases {
			d.accept("cook", c, at("cook-forgery", root))
		}
	}

	// THE WRITE CONTRACT: a lawful value the same writer writes at the same
	// root is the twin. The grain is the root and not the field: the footgun
	// is a leg that refuses the WRITER, and a per-field twin is the corpus's
	// own business (most rows carry one beside them).
	for _, wr := range m.WriteRefusals {
		key := at(wr.Surface(), wr.Unit, wr.Root)
		if wr.Verdict == "refused" {
			d.refuse(wr.Surface(), wr.Name, key)
		} else {
			d.accept(wr.Surface(), wr.Name, key)
		}
	}
	return d, nil
}

// problems is rule 1: every must-refuse row with no lawful twin anywhere in
// the corpus, as one line each, sorted.
func (d *discrimination) problems() []string {
	var out []string
	for ref, key := range d.refusal {
		if len(d.lawful[key]) == 0 {
			out = append(out, fmt.Sprintf("%s / %s: a must-refuse row with no lawful twin (group %s) — a leg that refuses the whole form passes it; add a row in the same group that must be accepted", ref.surface, ref.name, key))
		}
	}
	sort.Strings(out)
	return out
}

// outcomeUncredited is a must-refuse case the leg refused while answering
// none of its lawful twins: the refusal proves nothing, so it is not a pass.
const outcomeUncredited = "refusal-only"

// credit is rule 2, applied to one leg after every surface it ran is scored.
// A refused row whose twins the leg never answered correctly moves from pass
// to refusal-only; the reference leg's is a failure.
func (d *discrimination) credit(lang string, results map[string]*result, reference bool) {
	for ref, key := range d.refusal {
		r := results[ref.surface]
		if r == nil || r.cases[ref.name] != "pass" {
			continue
		}
		credited := false
		for _, twin := range d.lawful[key] {
			if t := results[twin.surface]; t != nil && t.cases[twin.name] == "pass" {
				credited = true
				break
			}
		}
		if credited {
			continue
		}
		r.pass--
		r.uncredited = append(r.uncredited, ref.name)
		r.verdict(ref.name, outcomeUncredited)
		r.mark(ref.name, outcomeUncredited)
		if reference {
			r.failures = append(r.failures, fmt.Sprintf(
				"%s: REFUSAL-ONLY — the reference leg refused this row and answered none of its lawful twins (group %s), so the refusal proves nothing", ref.name, key))
		}
	}
	for _, r := range results {
		sort.Strings(r.uncredited)
	}
}
