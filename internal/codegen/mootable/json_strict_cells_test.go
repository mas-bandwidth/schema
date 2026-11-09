package mootable

// THE STRICT TEXT-FORM CELLS (docs/roadmap-evidence/moo.sexp
// port/moo/vt/text-form and port/moo/vt/text-form-variable; STRICT-100):
// one subtest per roadmap row, each held only through the one strict read
// (<Root>FromJson: the whole value or a refusal, a silent report on a read)
// and the canonical writer (<Root>ToJson byte for byte as
// internal/tabletext writes it).
//
//	text-form           every golden instance of the manifest and every
//	                    json-hostile tree: the golden reads silently and
//	                    writes back byte for byte; a hostile tree reads to
//	                    its manifest verdict (read or refused, nothing
//	                    finer: docs/rulings.json refusal-verdict-only)
//	text-form-variable  the same, over only the variable units graphdemo,
//	                    blobdemo, listdemo and mapdemo (&node defined and
//	                    referenced, blobs, lists, maps, both directions,
//	                    and their hostile trees)
//
// TestMooTextFormStrictCellsNegativeControl plants its faults in the real
// artifact, the runtime json.moo (Runtime()["json.moo"], overridden into
// every generated unit): the reader's key lookup shifted by one field,
// and the writer's indent narrowed by one space. Every row must go red
// under at least one plant, and every plant must turn some row red.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type textStrictCell struct {
	row   string
	units map[string]bool // nil: every unit
}

var textStrictCells = []textStrictCell{
	{"text-form", nil},
	{"text-form-variable", map[string]bool{"graphdemo": true, "blobdemo": true, "listdemo": true, "mapdemo": true}},
}

func (c textStrictCell) carries(unit string) bool { return c.units == nil || c.units[unit] }

// textStrictCellFailures runs one row over the golden and hostile corpora
// and answers what differs, and how many cases it held. runtime, when set,
// overrides runtime modules in every generated unit.
func textStrictCellFailures(t *testing.T, c textStrictCell, runtime map[string][]byte) (fails []string, held int) {
	t.Helper()
	m := readTextManifest(t)
	byUnit := map[string][]textInstance{}
	for _, in := range m.instances {
		if c.carries(in.unit) {
			byUnit[in.unit] = append(byUnit[in.unit], in)
		}
	}
	for _, key := range sortedKeys(byUnit) {
		u := loadUnit(t, m.units[key])
		if !unitCarriesJson(u) {
			continue
		}
		inputs := map[string][]byte{}
		var cases []textCase
		for _, in := range byUnit[key] {
			text, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "conformance", "tables", "json", in.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			inputs[in.name] = text
			cases = append(cases, textCase{in.root, in.name})
		}
		res := textRun(t, u, cases, inputs, runtime)
		for i, in := range byUnit[key] {
			held++
			if res[i][0] != "0,0,0,0,0,false,read," {
				fails = append(fails, fmt.Sprintf("%s (%s %s): the strict read reported %s, want a silent read", in.name, key, in.root, res[i][0]))
			}
			if want := string(inputs[in.name]); res[i][1] != want {
				fails = append(fails, fmt.Sprintf("%s (%s %s): wrote\n%s\nwant\n%s", in.name, key, in.root, res[i][1], want))
			}
		}
	}
	hostileByUnit := map[string][]textHostile{}
	for _, h := range m.hostile {
		if c.carries(h.unit) {
			hostileByUnit[h.unit] = append(hostileByUnit[h.unit], h)
		}
	}
	for _, key := range sortedKeys(hostileByUnit) {
		u := loadUnit(t, m.units[key])
		if !unitCarriesJson(u) {
			continue
		}
		inputs := map[string][]byte{}
		var cases []textCase
		for _, h := range hostileByUnit[key] {
			text, err := os.ReadFile(filepath.Join(repoRoot, h.tree, h.root+".json"))
			if err != nil {
				t.Fatal(err)
			}
			inputs[h.name] = text
			cases = append(cases, textCase{h.root, h.name})
		}
		res := textRun(t, u, cases, inputs, runtime)
		for i, h := range hostileByUnit[key] {
			held++
			cols := strings.SplitN(res[i][0], ",", 8)
			if len(cols) != 8 {
				t.Fatalf("%s (%s %s): the probe printed %q", h.name, key, h.root, res[i][0])
			}
			if cols[6] != h.verdict {
				fails = append(fails, fmt.Sprintf("%s (%s %s): the strict read %s, want %s", h.name, key, h.root, res[i][0], h.verdict))
			}
		}
	}
	return fails, held
}

func TestMooTextFormStrictCells(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	for _, c := range textStrictCells {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			fails, held := textStrictCellFailures(t, c, nil)
			for _, f := range fails {
				t.Error(f)
			}
			if held == 0 {
				t.Fatal("the row holds nothing")
			}
			t.Logf("%s: %d golden and hostile cases held through the strict read", c.row, held)
		})
	}
}

// TestMooTextFormStrictCellsNegativeControl plants, one at a time, a defect
// in the real artifact json.moo: the reader's key lookup shifted to the
// neighbouring field, and the writer's indent narrowed to one space. Every
// row must go red under at least one plant (the reader plant breaks the
// strict read, the writer plant the byte-for-byte write-back), and every
// plant must turn some row red: no row's gate is green by holding nothing.
func TestMooTextFormStrictCellsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	plant := func(name, from, to string) struct {
		name    string
		runtime map[string][]byte
	} {
		src := string(Runtime()["json.moo"])
		if strings.Count(src, from) != 1 {
			t.Fatalf("%s: json.moo carries %q %d times, want 1: the control would be vacuous", name, from, strings.Count(src, from))
		}
		return struct {
			name    string
			runtime map[string][]byte
		}{name, map[string][]byte{"json.moo": []byte(strings.Replace(src, from, to, 1))}}
	}
	plants := []struct {
		name    string
		runtime map[string][]byte
	}{
		plant("read-key-lookup", "\t\t\tk[fd.json] = i\n", "\t\t\tk[fd.json] = if i < #d.fields then i + 1 else i\n"),
		plant("write-indent", `s = "\n" .. string.rep("  ", depth)`, `s = "\n" .. string.rep(" ", depth)`),
	}
	var mu sync.Mutex
	reddened := map[string]int{}
	t.Run("rows", func(t *testing.T) {
		for _, c := range textStrictCells {
			t.Run(c.row, func(t *testing.T) {
				t.Parallel()
				red := 0
				for _, p := range plants {
					fails, _ := textStrictCellFailures(t, c, p.runtime)
					if len(fails) > 0 {
						red++
						mu.Lock()
						reddened[p.name]++
						mu.Unlock()
						t.Logf("%s: %d findings, first %s", p.name, len(fails), fails[0])
					}
				}
				if red == 0 {
					t.Fatalf("the %s row stayed green under every plant", c.row)
				}
			})
		}
	})
	for _, p := range plants {
		if reddened[p.name] == 0 {
			t.Errorf("the %s plant turned no row red", p.name)
		}
	}
}
