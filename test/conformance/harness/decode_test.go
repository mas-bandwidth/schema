// The corpus read back through the TOOL's own engine.
//
// reports.txt and the wire goldens are shared expectations: the per-language
// drivers are held to them by `harness run`, and `harness generate` writes the
// counters from internal/tablewire. Neither of those runs tablewire.Decode
// against the corpus IN A GO TEST, so a decode change that moved a field's
// STATE without moving a counter — presence is the one that has no counter —
// could pass `go test ./...`, `make check`, `make conformance-generate` and
// the C++ conformance leg while the tool and the reference disagreed about
// what a row decodes to.
//
// These tests close that: every report row decodes through tablewire.Decode
// here, the counters are held to the pinned row, and the STATE OF EVERY
// OPTIONAL FIELD is pinned as a signature beside it — for the hostile rows
// and for the shared instance the two writers agree on. The signature is
// complete by construction (it walks the whole decoded value), so a row's pin
// cannot silently omit the field that matters.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE PINNED PRESENCE (docs/SPEC-TABLES.md §2.3). Each entry is the complete
// signature of the decoded value's optional fields: every `?T` and every
// `?[N]T`/`?[..N]T` the walk reaches, `present` or `absent`, a counted array
// carrying the count it decoded. Written from §2.3's rules, not from a run:
//
//   - an optional array whose body carries a FOREIGN ELEMENT KIND is §3's
//     element-kind mismatch, and the field is left at its declared default,
//     which for an optional is ABSENT;
//   - every in-body event short of that leaves the field PRESENT, because the
//     field rode: a count past the bound keeps the prefix and clamps, and a
//     variant id no build names leaves that slot at None and counts unknown.
var pinnedPresence = map[string][]string{
	// the element kind 8 where this reader declares 13: absent, and `marks`
	// was never on the wire at all
	"trace_elem_kind": {"marks=absent", "trace=absent"},
	// four elements against a bound of three: present, the prefix kept
	"trace_count_past_bound": {"marks=absent", "trace=present[3]"},
	// a fixed optional enum array carrying an id no variant names: present,
	// the slot at None
	"marks_unknown_variant": {"marks=present", "trace=absent"},
}

// THE SHARED INSTANCE the PR body claims and every leg reads: the optional
// array at three depths, in both spellings, over tables, scalars and enums.
var pinnedInstancePresence = map[string][]string{
	"message_trace": {
		"body.transact.checkpoints=present",
		"body.transact.edits[0].body.insert.modes=present[0]",
		"body.transact.snapshots=present",
		"marks=present",
		"trace=present[2]",
	},
}

func conformanceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// corpus loads the manifest and the pinned reports with the repository root as
// the working directory, which is what every path in the manifest assumes.
func corpus(t *testing.T) (*Manifest, map[string]Counts, *units) {
	t.Helper()
	t.Chdir(conformanceRoot(t))
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := readReports(defaultReports)
	if err != nil {
		t.Fatal(err)
	}
	return m, pinned, newUnits(m)
}

func decodeWire(t *testing.T, u *units, unitKey, root, wire string) (*tabletext.Instance, tabletext.Report) {
	t.Helper()
	unit, err := u.get(unitKey)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(wire)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(unit)
	def := m.Lookup(root)
	if def == nil {
		t.Fatalf("%s: the unit declares no root %q", wire, root)
	}
	inst := m.New(def)
	var report tabletext.Report
	_, err = tablewire.Decode(m, inst, data, &report)
	if _, refused := errors.AsType[*tablewire.FormRefusal](err); refused {
		// A REFUSAL IS THE ANSWER (docs/SPEC-TABLES.md §3): the verdict rides
		// on the report and nothing was decoded to count over
		return inst, report
	}
	if err != nil {
		t.Fatalf("%s: the decode refused the root: %v", wire, err)
	}
	return inst, report
}

// optionalSignature is the state of every optional field the decoded value
// reaches, in a stable order — the whole value, so a pin cannot leave a field
// out. A counted optional array carries the count it decoded, because
// "present and empty" is the value the presence bit exists to spell.
func optionalSignature(inst *tabletext.Instance) []string {
	var out []string
	walkOptionals(inst, "", 0, &out)
	sort.Strings(out)
	return out
}

// the depth cap is the pointer class's: a graph may cycle, and the signature
// only has to reach the fields a corpus row declares.
const signatureDepth = 12

func walkOptionals(inst *tabletext.Instance, prefix string, depth int, out *[]string) {
	if inst == nil || depth > signatureDepth {
		return
	}
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		path := prefix + fv.Def.Name
		if fv.Def.Type.Optional {
			state := "absent"
			switch {
			case fv.Present && fv.Def.Array == ir.ArrayCounted:
				state = fmt.Sprintf("present[%d]", fv.Count)
			case fv.Present:
				state = "present"
			}
			*out = append(*out, path+"="+state)
		}
		if fv.Def.Array == ir.ArrayNone {
			walkOptionals(fv.Cell.Tab, armPath(path, fv.Def, &fv.Cell), depth+1, out)
			walkOptionals(fv.Cell.Node, path+".", depth+1, out)
			continue
		}
		for j := range fv.Elems {
			elem := fmt.Sprintf("%s[%d]", path, j)
			walkOptionals(fv.Elems[j].Tab, armPath(elem, fv.Def, &fv.Elems[j]), depth+1, out)
			walkOptionals(fv.Elems[j].Node, elem+".", depth+1, out)
		}
	}
}

// armPath names the union ARM a payload sits under, so a signature reads the
// way the text form does — `body.insert.modes`, not `body.modes`.
func armPath(path string, f *ir.Field, cell *tabletext.Cell) string {
	un := tabletext.UnionOf(f)
	if un == nil || cell.U == 0 || int(cell.U) > len(un.Variants) {
		return path + "."
	}
	return path + "." + un.Variants[cell.U-1].Name + "."
}

func checkSignature(t *testing.T, name string, want, got []string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%s: the optional fields decode to\n  %s\nand the corpus pins\n  %s",
			name, strings.Join(got, " "), strings.Join(want, " "))
	}
}

// TestReportRowsDecodeThroughTheEngine runs tablewire.Decode over every
// evolution case the manifest names and holds it to the pinned row — the
// counters for every case, and the complete optional-field signature for the
// cases that carry one.
func TestReportRowsDecodeThroughTheEngine(t *testing.T) {
	m, pinned, u := corpus(t)
	seen := map[string]bool{}
	for _, rc := range m.Reports {
		want, ok := pinned[rc.Name]
		if !ok {
			t.Errorf("%s: reports.txt names no such case — run: make conformance-generate", rc.Name)
			continue
		}
		inst, report := decodeWire(t, u, rc.Unit, rc.Root, rc.Wire)
		got := Counts{
			Unknown:      report.Unknown,
			KindMismatch: report.KindMismatch,
			Clamped:      report.Clamped,
			Duplicate:    report.Duplicate,
			Malformed:    report.Malformed,
			Refused:      report.Refused,
		}
		if got != want {
			t.Errorf("%s: the engine reads %s and the corpus pins %s", rc.Name, got, want)
		}
		if sig, pinnedRow := pinnedPresence[rc.Name]; pinnedRow {
			seen[rc.Name] = true
			checkSignature(t, rc.Name, sig, optionalSignature(inst))
		}
	}
	for name := range pinnedPresence {
		if !seen[name] {
			t.Errorf("%s: pinned here, but the manifest names no report case by that name", name)
		}
	}
}

// TestInstancesDecodeTheirPinnedPresence holds the same walk to the instances
// both writers agree on: the wire a reader accepts as clean must place every
// optional field exactly where the corpus says it is.
func TestInstancesDecodeTheirPinnedPresence(t *testing.T) {
	m, _, u := corpus(t)
	seen := map[string]bool{}
	for _, inst := range m.Instances {
		want, ok := pinnedInstancePresence[inst.Name]
		if !ok {
			continue
		}
		seen[inst.Name] = true
		value, report := decodeWire(t, u, inst.Unit, inst.Root, inst.Wire)
		if !report.Silent() {
			t.Errorf("%s: its own writer's bytes do not read clean: %+v", inst.Name, report)
		}
		checkSignature(t, inst.Name, want, optionalSignature(value))
	}
	for name := range pinnedInstancePresence {
		if !seen[name] {
			t.Errorf("%s: pinned here, but the manifest names no instance by that name", name)
		}
	}
}
