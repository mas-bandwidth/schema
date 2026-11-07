// The corpus read through the compiler engine: one acceptance verdict and
// complete accepted values, with optional presence pinned independently.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A legal unknown enum variant leaves the slot at None and keeps presence.
var pinnedPresence = map[string][]string{
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
func corpus(t *testing.T) (*Manifest, map[string]string, *units) {
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
	ok, _ := strictDecode(m, inst, data, &report)
	if !ok || report.Refused {
		return nil, report
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

// The pinned verdict carries no repair-counter interpretation.
func pinnedRowRefuses(want string) bool { return want == "refused" }

// Each row is held to its acceptance and complete pinned value. Legacy fixed
// form-1 admission gaps remain failures until their corpus rows are corrected.
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
		sig, pinnedRow := pinnedPresence[rc.Name]
		seen[rc.Name] = pinnedRow
		if pinnedRowRefuses(want) {
			if inst != nil || !report.Refused {
				t.Errorf("%s: the one read refuses the row pinned %s and the engine reads %+v", rc.Name, want, report)
			}
			continue
		}
		if inst == nil || report.Refused {
			t.Errorf("%s: the engine reads %+v and the corpus pins %s", rc.Name, report, want)
			continue
		}
		checkGolden(t, rc.Name, rc.Value, true, u, rc.Unit, inst)
		if pinnedRow {
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
		if value == nil || report.Refused {
			t.Errorf("%s: its own writer's bytes are refused: %+v", inst.Name, report)
			continue
		}
		checkSignature(t, inst.Name, want, optionalSignature(value))
	}
	for name := range pinnedInstancePresence {
		if !seen[name] {
			t.Errorf("%s: pinned here, but the manifest names no instance by that name", name)
		}
	}
}
