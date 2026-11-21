package luautable_test

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUAU FORM OF THE ACCESSOR AND DESCRIPTOR AGREEMENT GATE (docs/PORTING.md
// J1).
//
// WHAT THE LUAU LEG HAS INSTEAD OF A GENERATED ACCESSOR. Luau generates no
// accessor: a cook node is read by the one reflective walk
// (schema_cookread.luau) at offsets it derives from the field descriptors, so
// there is no second path to drift from the descriptor. What can still be
// wrong is the layout itself, and the cook's layout has TWO INDEPENDENT
// DERIVATIONS, exactly the pair J1 exists to hold together:
//
//   - the compiler's own model (ir.RecordLayout), the one the C++
//     static_asserts, the C# checks and the build version come from; and
//   - the Luau runtime's: schema_cookread.luau's C.layout derives every
//     record's size and alignment and every field's offset and size from the
//     descriptors by its OWN arithmetic (align_up over the pieces of a field).
//
// This gate dumps the Luau derivation for EVERY record the descriptors of
// EVERY cook-carrying unit of the corpus register, and requires it equal to
// the compiler's, record by record and field by field. The block form needs no
// such pair: its descriptors (<Base>Block.luau) are emitted from ir.BlockLayout
// itself, one derivation, and the block surfaces hold the values read at them.
//
// REVERSED WITNESS (TestLuauLayoutAgreementNegativeControl): a pointer slot
// widened by eight in the runtime's arithmetic must red the records that hold
// one, and nothing that holds none.

const luauLayoutProbe = `local C = require("./schema_cookread")
local S = require("./schema_tables")
for _, m in { MODULES } do require("./" .. m) end
for _, name in { NAMES } do
	local ok, d = pcall(S.lookup, name)
	if ok and d.fields ~= nil then
		local rec = C.layout.record(name)
		print("rec " .. name .. " " .. rec.size .. " " .. rec.align)
		local offsets = C.layout.offsets(d.fields)
		for i, fd in d.fields do
			local fl = C.layout.field(fd, offsets[i])
			print("fld " .. name .. " " .. fd.name .. " " .. fl.start .. " " .. fl.size)
		end
	end
end
`

// luauLayoutLines runs the probe over one unit and answers its lines.
func luauLayoutLines(t *testing.T, g *generatedUnit, names []string) []string {
	t.Helper()
	var mods, lits []string
	for name := range g.files {
		if base, ok := strings.CutSuffix(name, "Table.luau"); ok {
			mods = append(mods, fmt.Sprintf("%q", base+"Table"))
		}
	}
	sort.Strings(mods)
	for _, n := range names {
		lits = append(lits, fmt.Sprintf("%q", n))
	}
	main := strings.Replace(luauLayoutProbe, "{ MODULES }", "{ "+strings.Join(mods, ", ")+" }", 1)
	main = strings.Replace(main, "{ NAMES }", "{ "+strings.Join(lits, ", ")+" }", 1)
	out := runProbe(t, g, main, nil)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// compilerLayoutLines is the compiler's model of the same records, in the
// probe's spelling, keyed by record name.
func compilerLayoutLines(u *ir.Unit, name string) []string {
	st := u.Structs[name]
	if st == nil {
		st = u.Tables[name]
	}
	if st == nil {
		return nil
	}
	l := ir.RecordLayout(u, st)
	out := []string{fmt.Sprintf("rec %s %d %d", name, l.Size, l.Align)}
	for _, f := range l.Fields {
		out = append(out, fmt.Sprintf("fld %s %s %d %d", name, f.Field.Name, f.Offset, f.Size))
	}
	return out
}

type layoutUnit struct {
	key   string
	g     *generatedUnit
	u     *ir.Unit
	names []string
}

// luauLayoutUnits is every manifest unit whose generated tree carries the
// cook reader, with every declared table and type name.
func luauLayoutUnits(t *testing.T) []layoutUnit {
	t.Helper()
	units, _ := readManifest(t)
	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []layoutUnit
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if !g.loaded || g.files["schema_cookread.luau"] == nil {
			continue
		}
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for n := range u.Tables {
			names = append(names, n)
		}
		for n := range u.Structs {
			names = append(names, n)
		}
		sort.Strings(names)
		out = append(out, layoutUnit{key, g, u, names})
	}
	if len(out) == 0 {
		t.Fatal("no manifest unit carries the cook reader: the gate is blind")
	}
	return out
}

// layoutDisagreements answers every line on which the Luau derivation and the
// compiler's differ, and how many records were compared.
func layoutDisagreements(t *testing.T, lu layoutUnit, lines []string) (bad []string, records int) {
	t.Helper()
	got := map[string][]string{}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) >= 2 {
			got[f[1]] = append(got[f[1]], line)
		}
	}
	for name, luau := range got {
		want := compilerLayoutLines(lu.u, name)
		if want == nil {
			continue
		}
		records++
		if strings.Join(luau, "\n") != strings.Join(want, "\n") {
			for i := 0; i < max(len(luau), len(want)); i++ {
				var l, w string
				if i < len(luau) {
					l = luau[i]
				}
				if i < len(want) {
					w = want[i]
				}
				if l != w {
					bad = append(bad, fmt.Sprintf("%s: luau %q, compiler %q", lu.key, l, w))
				}
			}
		}
	}
	return bad, records
}

func TestLuauLayoutAgreesWithTheCompiler(t *testing.T) {
	t.Parallel()
	total := 0
	for _, lu := range luauLayoutUnits(t) {
		bad, records := layoutDisagreements(t, lu, luauLayoutLines(t, lu.g, lu.names))
		total += records
		for _, b := range bad {
			t.Error(b)
		}
	}
	if total < 20 {
		t.Fatalf("only %d records compared: the gate is blind", total)
	}
	t.Logf("%d records agree with ir.RecordLayout", total)
}

func TestLuauLayoutAgreementNegativeControl(t *testing.T) {
	t.Parallel()
	const from = "\telseif s == \"ptr\" or s == \"sblob\" or s == \"bblob\" or s == \"text\" then\n\t\tadd(8, 8)\n"
	const to = "\telseif s == \"ptr\" or s == \"sblob\" or s == \"bblob\" or s == \"text\" then\n\t\tadd(16, 8)\n"
	turned := 0
	for _, lu := range luauLayoutUnits(t) {
		src := string(lu.g.files["schema_cookread.luau"])
		if strings.Count(src, from) != 1 {
			t.Fatalf("%s: schema_cookread.luau carries the pointer slot %d times, want 1: the control would be vacuous", lu.key, strings.Count(src, from))
		}
		planted := &generatedUnit{files: map[string][]byte{}, loaded: true}
		maps.Copy(planted.files, lu.g.files)
		planted.files["schema_cookread.luau"] = []byte(strings.Replace(src, from, to, 1))
		bad, _ := layoutDisagreements(t, lu, luauLayoutLines(t, planted, lu.names))
		turned += len(bad)
		for _, b := range bad {
			// every turned line is a record that holds a pointer-shaped slot
			name := ""
			for part := range strings.SplitSeq(b, "\"") {
				if f := strings.Fields(part); len(f) >= 2 && (f[0] == "rec" || f[0] == "fld") {
					name = f[1]
					break
				}
			}
			if !holdsPointerSlot(lu.u, name) {
				t.Errorf("the pointer-slot plant turned %s, which holds no pointer-shaped slot", b)
			}
		}
	}
	if turned == 0 {
		t.Fatal("a pointer slot widened by eight turned no line: the gate is watching nothing")
	}
	t.Logf("a pointer slot widened by eight turned %d line(s)", turned)
}

// holdsPointerSlot reports whether the record's storage holds a field the
// plant widens (a pointer, a blob or unbounded text, not an array element),
// directly or inside a record or union arm it holds by value.
func holdsPointerSlot(u *ir.Unit, name string) bool {
	return holdsPointer(u, name, map[string]bool{})
}

func holdsPointer(u *ir.Unit, name string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	var fields []*ir.Field
	if st := u.Structs[name]; st != nil {
		fields = st.Fields
	} else if st := u.Tables[name]; st != nil {
		fields = st.Fields
	} else if un := u.Unions[name]; un != nil {
		for _, v := range un.Variants {
			if v.F != nil {
				fields = append(fields, v.F)
			}
		}
	} else if un := u.TableUnions[name]; un != nil {
		for _, v := range un.Variants {
			if v.F != nil {
				fields = append(fields, v.F)
			}
		}
	}
	for _, f := range fields {
		if f.Array == ir.ArrayNone && (f.Type.Pointer || f.Type.TextUnbounded()) {
			return true
		}
		if !f.Type.Pointer && f.Type.Name != "" && holdsPointer(u, f.Type.Name, seen) {
			return true
		}
	}
	return false
}
