package gotable

import (
	"bytes"
	"maps"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoTestgenBoundProbes is the gate of testgen's bound probes
// (docs/SPEC-TABLES.md §16.11, "The bound probes"). testgen of tables/examples,
// and of tables/examples/Ranges.schema alone, emits a probe for every bound a
// root table's own field lines declare (counted here from the declaration, not
// from the generator), plus the nested ones the corpus reaches through an
// element, a type body, an enum-keyed slot and a union arm. Every probe RUNs
// and PASSes in a t.TempDir() unit. Then one range clamp is removed from a
// generated reader and its probe has to fail.
func TestGoTestgenBoundProbes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		paths  []string
		nested []string // probes reached through a path, Table/probe
	}{
		{"examples", []string{"tables/examples"}, []string{
			"KeyedConfig/scores.per_team[Red]:above",
			"PackConfig/thresholds[Easy]:above",
			"ShipEntry/hardpoints[0]:above",
			"WeaponConfig/effect.debuff.amount:above",
			"LoadoutConfig/attachments[0].slot:above",
			"RangedSigned/edges:count",
		}},
		{"ranges", []string{"tables/examples/Ranges.schema"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnitPaths(t, "../../..", c.paths)
			tests, err := Testgen(u)
			if err != nil {
				t.Fatalf("testgen: %v", err)
			}
			want := append(boundProbesDeclared(u), c.nested...)
			sort.Strings(want)
			all := bytes.Join(sortedValues(tests), nil)
			for _, p := range want {
				table, probe, _ := strings.Cut(p, "/")
				if !bytes.Contains(all, []byte("func "+table+"GenPastBound(probe string) []byte")) {
					t.Fatalf("no past-bound image builder for %s", table)
				}
				if !bytes.Contains(all, []byte("\t\t{\""+probe+"\", ")) {
					t.Fatalf("testgen emits no probe %s of %s", probe, table)
				}
			}
			if len(want) < 10 {
				t.Fatalf("only %d bound probes counted: %v", len(want), want)
			}
			stub := "package " + u.Package + "\n"
			add := func(files map[string][]byte) { maps.Copy(files, tests) }
			out, err := runGeneratedUnit(t, u, stub, add, "-v", "-run", "GenBoundProbes")
			if err != nil {
				t.Fatalf("emitted bound probes: %v\n%s", err, out)
			}
			for _, p := range want {
				table, probe, _ := strings.Cut(p, "/")
				line := "--- PASS: Test" + table + "GenBoundProbes/" + probe + " "
				if !bytes.Contains(out, []byte(line)) {
					t.Fatalf("probe %s did not RUN and PASS:\n%s", p, out)
				}
			}
			if n := bytes.Count(out, []byte("--- FAIL")); n != 0 {
				t.Fatalf("%d failures:\n%s", n, out)
			}
			// EVERY EMITTED PROBE RUNS AND PASSES, not only the counted ones
			emitted := len(regexp.MustCompile(`(?m)^\t\t\{"[^"]+:(above|below|count)", `).FindAll(all, -1))
			passed := len(regexp.MustCompile(`(?m)^\s+--- PASS: Test\w+GenBoundProbes/`).FindAll(out, -1))
			if passed != emitted || emitted < len(want)-len(c.nested) {
				t.Fatalf("%d probes emitted, %d passed, %d counted:\n%s", emitted, passed, len(want), out)
			}
			t.Logf("%d bound probes emitted, RUN and PASS (%d counted from the declaration and the named paths)", passed, len(want))
		})
	}

	// A READER WITH ONE RANGE CLAMP REMOVED: TeamConfig.spawn_count's upper
	// clamp on its own kind (kind 4) is cut, so 65 stands, and the probe fails.
	t.Run("clamp_removed", func(t *testing.T) {
		t.Parallel()
		u := loadUnitPaths(t, "../../..", []string{"tables/examples"})
		tests, err := Testgen(u)
		if err != nil {
			t.Fatal(err)
		}
		out, err := runGeneratedUnit(t, u, "package "+u.Package+"\n", func(files map[string][]byte) {
			maps.Copy(files, tests)
			src := files["KeyedTable.go"]
			at := bytes.Index(src, []byte("func TeamConfigLoadBody(r *TableReader"))
			if at < 0 {
				t.Fatal("no TeamConfigLoadBody")
			}
			direct := bytes.Index(src[at:], []byte("v := int32(r.Get32())"))
			if direct < 0 {
				t.Fatal("no direct-kind read of spawn_count")
			}
			clamp := []byte("} else if v > 64 {")
			c := bytes.Index(src[at+direct:], clamp)
			if c < 0 {
				t.Fatal("no upper clamp of spawn_count")
			}
			b := at + direct + c
			edited := bytes.Clone(src[:b])
			edited = append(edited, []byte("} else if false {")...)
			edited = append(edited, src[b+len(clamp):]...)
			files["KeyedTable.go"] = edited
		}, "-v", "-run", "^TestTeamConfigGenBoundProbes$")
		if err == nil || !bytes.Contains(out, []byte("--- FAIL: TestTeamConfigGenBoundProbes/spawn_count:above")) {
			t.Fatalf("a reader with its spawn_count clamp removed went unnoticed: %v\n%s", err, out)
		}
		if !bytes.Contains(out, []byte("--- PASS: TestTeamConfigGenBoundProbes/spawn_count:below")) {
			t.Fatalf("the untouched lower clamp should still pass:\n%s", out)
		}
	})

	// The kinds the corpus does not declare: a fixed-point range,
	// bytes(N), a plain text list (its count and an element's length), a
	// 128-bit range, a fixed [N] count, a required field beside the probe, a
	// guarded field, a scalar union arm, a pointer, a map and a compressed
	// float range named as not probed, and a variable table read through its
	// builder.
	t.Run("kinds", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
union Pick { n int16 | min = -5, max = 5
 m uint8 }
type Inner { k uint8 = 1 | min = 1, max = 9 }
table Leaf { v int32 }
table Kinds {
    fx    fixed(8, 8) | min = -2, max = 3
    bs    bytes(3)
    tags  [..2]string(3)
    big   int128 | min = -10, max = 10
    on    bool
    if on { gated uint16 = 4 | min = 4, max = 40 }
    pick  Pick
    inner Inner
    trio  [3]int8
    gauge float32 | min = 0, max = 1, resolution = 0.25
}
table Var {
    must int32 = 3 | required, min = 2, max = 7
    name string(5)
    leaf *Leaf
    by   map[string(4)]int32
    n    uint32 = 1 | min = 1, max = 3
}
`)
		tests, err := Testgen(u)
		if err != nil {
			t.Fatalf("testgen: %v", err)
		}
		src := tests["ProbeTable_gen_test.go"]
		for _, s := range []string{"// not probed: Var.leaf", "// not probed: Var.by", "// not probed: Kinds.gauge"} {
			if !bytes.Contains(src, []byte(s)) {
				t.Fatalf("the generated file does not name %q:\n%s", s, src)
			}
		}
		want := []string{
			"Var/must:below", "Var/must:above", "Kinds/fx:below", "Kinds/fx:above",
			"Kinds/bs:above", "Kinds/tags:count", "Kinds/tags[0]:above",
			"Kinds/big:below", "Kinds/big:above", "Kinds/gated:below", "Kinds/gated:above",
			"Kinds/trio:count", "Kinds/pick.n:below", "Kinds/pick.n:above", "Kinds/inner.k:below", "Kinds/inner.k:above",
			"Var/name:above", "Var/n:below", "Var/n:above",
		}
		out, err := runGeneratedUnit(t, u, "package probe\n", func(files map[string][]byte) { maps.Copy(files, tests) }, "-v", "-run", "GenBoundProbes")
		if err != nil {
			t.Fatalf("emitted bound probes: %v\n%s", err, out)
		}
		for _, p := range want {
			table, probe, _ := strings.Cut(p, "/")
			if !bytes.Contains(out, []byte("--- PASS: Test"+table+"GenBoundProbes/"+probe+" ")) {
				t.Fatalf("probe %s did not RUN and PASS:\n%s", p, out)
			}
		}
		if got := len(regexp.MustCompile(`(?m)^\s+--- PASS: Test\w+GenBoundProbes/`).FindAll(out, -1)); got != len(want) {
			t.Fatalf("%d probes passed, want exactly %d:\n%s", got, len(want), out)
		}
	})
}

// boundProbesDeclared counts, from the declaration alone, the probes §16.11
// owes for every root table's own field lines: each integer range end its own
// wire kind can step past, a bits(N) narrower than its kind, a string(N) or
// bytes(N), and a [..N] or [N] count.
func boundProbesDeclared(u *ir.Unit) []string {
	var out []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() {
				continue
			}
			for _, fd := range st.Fields {
				if fd.Deprecated || fd.IsMap() || fd.Type.Pointer {
					continue
				}
				name := st.Name + "/" + fd.Name
				if (fd.Array == ir.ArrayCounted || fd.Array == ir.ArrayFixed) && fd.KeyEnum == "" {
					out = append(out, name+":count")
				}
				if fd.Array != ir.ArrayNone || fd.KeyEnum != "" {
					continue
				}
				switch fd.Type.Kind {
				case ir.TString, ir.TBytes:
					out = append(out, name+":above")
				case ir.TBits:
					if fd.Type.Width < 8*ir.TableKindWidth(ir.TableScalarKind(fd)) {
						out = append(out, name+":above")
					}
				case ir.TInt, ir.TFixed:
					lo, hi, ok := ir.TableRawRange(fd)
					if !ok {
						continue
					}
					bits := uint(8 * ir.TableKindWidth(ir.TableScalarKind(fd)))
					kmin, kmax := big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
					if ir.TableKindSigned(ir.TableScalarKind(fd)) {
						kmin = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
						kmax = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
					}
					if lo.Cmp(kmin) > 0 {
						out = append(out, name+":below")
					}
					if hi.Cmp(kmax) < 0 {
						out = append(out, name+":above")
					}
				}
			}
		}
	}
	return out
}

func sortedValues(m map[string][]byte) [][]byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out [][]byte
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
