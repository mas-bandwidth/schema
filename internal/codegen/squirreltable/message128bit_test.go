package squirreltable_test

// THE MESSAGE FORM CARRIES A 128-BIT FIELD EXACTLY (docs/SPEC-TABLES.md §3.3):
// an int128, a uint128 and a 128-bit fixed-point value ride as two 64-bit
// lanes, low half first, on the batch bitstream, with the same ranged offset
// and the same sixteen-byte base the Go engine writes. SimState (the scalars
// corpus) declares every shape the rule covers — a ranged int128, a raw
// uint128, a ranged fixed(112,16), a ranged ufixed(112,16), and an array of
// uint128 — at its default and at non-default values that set the top bit of
// each lane and the bounds of every ranged field.
//
// The Go engine is the oracle in its own subtest: it encodes each value and
// decodes it back to the same lanes. Squirrel then saves the same values (no
// raise, the batch decodes in the engine to the same value) and loads the
// engine's batch to the same value.

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type msgWideCase struct {
	name string
	vals map[string][]*big.Int
}

func msgWideFields(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if ir.TableKindWidth(ir.TableWireScalarKind(f)) == 16 {
			out = append(out, f)
		}
	}
	return out
}

func msgWide128(lo, hi uint64) *big.Int {
	v := new(big.Int).SetUint64(hi)
	v.Lsh(v, 64)
	return v.Or(v, new(big.Int).SetUint64(lo))
}

func msgWideCases(t *testing.T, fields []*ir.Field) []msgWideCase {
	t.Helper()
	lower := map[string][]*big.Int{}
	upper := map[string][]*big.Int{}
	for _, f := range fields {
		rawLo, rawHi, ranged := ir.TableRawRange(f)
		if f.Array != ir.ArrayNone {
			lower[f.Name] = []*big.Int{msgWide128(0x8000000000000000, 0x8000000000000000), msgWide128(0x8000000000000000, 0x8000000000000000)}
			upper[f.Name] = []*big.Int{msgWide128(0xffffffffffffffff, 0xffffffffffffffff), msgWide128(0xffffffffffffffff, 0xffffffffffffffff)}
			continue
		}
		if ranged {
			lower[f.Name] = []*big.Int{new(big.Int).Set(rawLo)}
			upper[f.Name] = []*big.Int{new(big.Int).Set(rawHi)}
		} else {
			lower[f.Name] = []*big.Int{msgWide128(0x8000000000000000, 0x8000000000000000)}
			upper[f.Name] = []*big.Int{msgWide128(0xffffffffffffffff, 0xffffffffffffffff)}
		}
	}
	return []msgWideCase{{name: "default"}, {name: "lower", vals: lower}, {name: "upper", vals: upper}}
}

func msgWideInstance(t *testing.T, model *tabletext.Model, st *ir.Struct, c msgWideCase) *tabletext.Instance {
	t.Helper()
	inst := model.New(st)
	for name, vals := range c.vals {
		f := nfField(t, inst, name)
		if f.Def.Array == ir.ArrayNone {
			f.Cell.Wide = vals[0]
			continue
		}
		f.Count = len(vals)
		for i, v := range vals {
			f.Elems[i].Wide = v
		}
	}
	return inst
}

func msgWideExtent(f *tabletext.Field) int {
	if f.Def.Array == ir.ArrayCounted || f.Def.Array == ir.ArrayList {
		return f.Count
	}
	if f.Def.Array != ir.ArrayNone {
		return len(f.Elems)
	}
	return 1
}

// msgWideLanes renders one instance's wide fields as the lanes the probe
// prints: a scalar's two lanes, an array's elements' lanes in order.
func msgWideLanes(t *testing.T, inst *tabletext.Instance, fields []*ir.Field) []string {
	t.Helper()
	parts := make([]string, len(fields))
	for i, f := range fields {
		fld := nfField(t, inst, f.Name)
		var tokens []string
		if f.Array == ir.ArrayNone {
			tokens = append(tokens, msgWideLane(fld.Cell.Wide))
		} else {
			for j := 0; j < msgWideExtent(fld); j++ {
				tokens = append(tokens, msgWideLane(fld.Elems[j].Wide))
			}
		}
		parts[i] = strings.Join(tokens, " ")
	}
	return parts
}

func msgWideLane(v *big.Int) string {
	if v == nil {
		v = new(big.Int)
	}
	lo, hi := split128(v)
	return fmt.Sprintf("%016x,%016x", lo, hi)
}

func msgWideLiteral(vals []*big.Int, array bool) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = squirrel.Int128Lit(v)
	}
	if array {
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return parts[0]
}

func TestSquirrelMessage128BitRoundTrip(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/scalars/Scalars.schema"})
	model := tabletext.NewModel(u)
	st := model.Lookup("SimState")
	if st == nil {
		t.Fatal("the scalars corpus declares no SimState")
	}
	if !st.FixedDeclared {
		t.Fatal("SimState is not a fixed table")
	}
	fields := msgWideFields(st)
	if len(fields) != 6 {
		t.Fatalf("SimState carries %d 128-bit fields, want 6", len(fields))
	}
	cases := msgWideCases(t, fields)

	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
		t.Fatalf("the oracle refused the announcement: %v", err)
	}

	insts := map[string]*tabletext.Instance{}
	want := map[string][]string{}
	batches := map[string][]byte{}
	for _, c := range cases {
		inst := msgWideInstance(t, model, st, c)
		insts[c.name] = inst
		want[c.name] = msgWideLanes(t, inst, fields)
		batch, err := tablewire.EncodeMessage(model, inst)
		if err != nil {
			t.Fatalf("%s: the oracle refused the value: %v", c.name, err)
		}
		batches[c.name] = batch
	}

	t.Run("engine", func(t *testing.T) {
		for _, c := range cases {
			back := model.New(st)
			var rep tabletext.Report
			n, ok, err := tablewire.DecodeMessages(model, []*tabletext.Instance{back}, batches[c.name], &vocabulary, &rep)
			if n != 1 || !ok || err != nil || rep.Refused {
				t.Fatalf("%s: the oracle answered %d %v %v %+v on its own batch", c.name, n, ok, err, rep)
			}
			got := msgWideLanes(t, back, fields)
			for i := range fields {
				if got[i] != want[c.name][i] {
					t.Errorf("%s: %s decoded as %q, want %q", c.name, fields[i].Name, got[i], want[c.name][i])
				}
			}
		}
	})

	dir := t.TempDir()
	writeGenerated(t, dir, u)
	module := squirreltable.ModuleName(u)

	var b strings.Builder
	b.WriteString(`local I = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require(` + fmt.Sprintf("%q", module) + `);
local lanes = function(p) { return format("%016x,%016x", p[0], p[1]); };
local arrLanes = function(a) {
  local s = "";
  for (local i = 0; i < a.len(); i++) { if (i > 0) s += " "; s += lanes(a[i]); }
  return s;
};
local voc = {};
local ar = {};
if (!T.AnnounceRead(voc, T.TableMessageAnnouncement, ar)) throw "announce: " + ar.reason;
`)
	for _, c := range cases {
		b.WriteString("{ local v = T.SimState();\n")
		for _, f := range fields {
			vals, ok := c.vals[f.Name]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "  v.%s = %s;\n", squirrel.Member(f), msgWideLiteral(vals, f.Array != ir.ArrayNone))
		}
		fmt.Fprintf(&b, `  local mr = {}; local size = T.SimStateMeasureMessages([v], mr);
  local out = blob(size < 0 ? 65536 : size); local sr = {};
  local n = T.SimStateSaveMessages([v], out, sr);
  if (n < 0) { print("save:%s refused " + sr.reason + "\n"); }
  else { local h = ""; for (local i = 0; i < n; i++) h += format("%%02x", out[i] & 0xff); print("save:%s " + h + "\n"); }
}
`, c.name, c.name)
		b.WriteString("{ local out = [T.SimState()]; local rr = {};\n")
		fmt.Fprintf(&b, "  local n = T.SimStateLoadMessages(out, voc, I[%q], rr);\n", "batch_"+c.name)
		fmt.Fprintf(&b, "  if (n != 1) { print(\"load:%s n=\" + n + \" reason=\" + ((\"reason\" in rr) ? rr.reason : \"-\") + \"\\n\"); }\n", c.name)
		b.WriteString("  else { local w = out[0]; local parts = [")
		for i, f := range fields {
			if i > 0 {
				b.WriteString(", ")
			}
			if f.Array != ir.ArrayNone {
				fmt.Fprintf(&b, "arrLanes(w.%s)", squirrel.Member(f))
			} else {
				fmt.Fprintf(&b, "lanes(w.%s)", squirrel.Member(f))
			}
		}
		fmt.Fprintf(&b, "]; local s = \"\"; for (local i = 0; i < parts.len(); i++) { if (i > 0) s += \"|\"; s += parts[i]; } print(\"load:%s \" + s + \"\\n\"); }\n}\n", c.name)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"ann": tablewire.Announce(u)}
	for name, batch := range batches {
		inputs["batch_"+name] = batch
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		key, rest, _ := strings.Cut(line, " ")
		got[key] = rest
	}

	for _, c := range cases {
		save, ok := got["save:"+c.name]
		if !ok {
			t.Fatalf("%s: the probe printed no save line", c.name)
		}
		if strings.HasPrefix(save, "refused") {
			t.Fatalf("%s: SaveMessages refused: %s", c.name, save)
		}
		wire, err := decodeHex(save)
		if err != nil {
			t.Fatalf("%s: the saved batch is not hex: %v", c.name, err)
		}
		back := model.New(st)
		var rep tabletext.Report
		n, ok, err := tablewire.DecodeMessages(model, []*tabletext.Instance{back}, wire, &vocabulary, &rep)
		if n != 1 || !ok || err != nil || rep.Refused {
			t.Fatalf("%s: the engine refused Squirrel's batch: %d %v %v %+v", c.name, n, ok, err, rep)
		}
		saved := msgWideLanes(t, back, fields)
		for i := range fields {
			if saved[i] != want[c.name][i] {
				t.Errorf("%s: Squirrel saved %s as %q, want %q", c.name, fields[i].Name, saved[i], want[c.name][i])
			}
		}

		load, ok := got["load:"+c.name]
		if !ok {
			t.Fatalf("%s: the probe printed no load line", c.name)
		}
		if strings.HasPrefix(load, "n=") {
			t.Fatalf("%s: LoadMessages did not read the engine's batch: %s", c.name, load)
		}
		loaded := strings.SplitN(load, "|", len(fields))
		if len(loaded) != len(fields) {
			t.Fatalf("%s: the probe printed %d fields, want %d: %s", c.name, len(loaded), len(fields), load)
		}
		for i := range fields {
			if loaded[i] != want[c.name][i] {
				t.Errorf("%s: Squirrel loaded %s as %q, want %q", c.name, fields[i].Name, loaded[i], want[c.name][i])
			}
		}
	}
}
