package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// offContractSaveSchema has one field per row of the write contract
// (docs/SPEC.md §5.1): a ranged integer, a bits(N), a counted array, a bounded
// string and bytes, an enum, a union, a ranged float, a bare signed and
// unsigned integer, a *string, a []string text list, a map with a text key, a
// bool and an enum-keyed float array.
const offContractSaveSchema = `package probe

enum Mode { Idle, Run, Fly }

union Arm { k int32 }

table Root
{
    ranged uint64 = 1 | min = 1, max = 10
    bits6 bits(6)
    counts [..3]uint16
    name string(4)
    raw bytes(4)
    mode Mode
    arm Arm
    ratio float32 | min = 0.0, max = 1.0, resolution = 0.01
    small int8
    octet uint8
    text *string
    names []string
    lookup map[string]int32
    flag bool
    weights [Mode]float32
}
`

// offContractSavePrelude runs one value through Save and Measure, expecting a
// refusal; a lawful twin is saved, read back through the strict read and saved
// again, which must be the same bytes. A raised error is reported, never
// allowed to escape, because the writer must answer null and not raise.
const offContractSavePrelude = `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("__MOD__");
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local bytesOf = function(a) {
  local s = "";
  foreach (b in a) s += format("%c", b);
  return s;
};
local probe = function(name, setup) {
  local v = T.Root();
  setup(v);
  local raised = false;
  local b = null;
  try { b = T.RootSave(v); } catch (e) { raised = true; }
  local measured = null;
  try { measured = T.RootMeasure(v); } catch (e) { raised = true; }
  local verdict = raised ? "raised" : (b == null ? "refused" : "wrote");
  local blen = b == null ? "-" : b.len().tostring();
  local mtext = measured == null ? "nil" : measured.tostring();
  local loaded = "none";
  local resaved = "none";
  if (!raised && b != null) {
    local back = T.RootLoad(b);
    loaded = back == null ? "refused" : "read";
    if (back != null) {
      local again = T.RootSave(back);
      resaved = again != null && hex(again) == hex(b) ? "same" : "differs";
    }
  }
  print(name + " " + verdict + " " + blen + " " + mtext + " " + loaded + " " + resaved + "\n");
};
`

type offContractCase struct {
	name string
	// goBad and goGood are the same value in the Go engine's model; nil where
	// the engine has no way to hold it (a wrong Squirrel type) or where its
	// writer does not refuse it today (a zero byte in a *string). goGood is nil
	// for the enum-keyed array row, whose twin is held on the Squirrel side.
	goBad   func(*tabletext.Model, *tabletext.Instance)
	goGood  func(*tabletext.Model, *tabletext.Instance)
	nutBad  string
	nutGood string
}

// TestSquirrelVariableSaveRefusesOffContract holds the variable form's Save
// and Measure to the write contract (docs/SPEC.md §5.1 rows 1-8 and 10): a
// value the declaration does not admit answers null and writes nothing, with
// no VM error, while the lawful twin at the boundary writes, reads back and
// re-saves byte for byte. The Go engine's variable writer is asked the same
// values first, so the two legs agree on the verdict.
func TestSquirrelVariableSaveRefusesOffContract(t *testing.T) {
	t.Parallel()
	unit := unitFromSchema(t, offContractSaveSchema)

	field := func(inst *tabletext.Instance, key string) *tabletext.Field {
		f, ok := inst.FieldByKey(key)
		if !ok {
			t.Fatalf("%s declares no field %q", inst.Def.Name, key)
		}
		return f
	}
	setU := func(inst *tabletext.Instance, key string, v uint64) {
		field(inst, key).Cell.U = v
	}
	setStr := func(inst *tabletext.Instance, key string, b []byte) {
		f := field(inst, key)
		f.Cell.Str = append([]byte(nil), b...)
		f.Count = len(b)
	}
	setBlob := func(inst *tabletext.Instance, key string, b []byte) {
		field(inst, key).Cell.Blob = &tabletext.Blob{Data: append([]byte(nil), b...)}
	}
	setNameText := func(m *tabletext.Model, inst *tabletext.Instance, b []byte) {
		f := field(inst, "names")
		cell := m.ElementZero(f.Def)
		vf := field(cell.Tab, "value")
		vf.Cell.Str = append([]byte(nil), b...)
		vf.Count = len(b)
		f.Elems = append(f.Elems, cell)
		f.Count = 1
	}
	setMapKey := func(m *tabletext.Model, inst *tabletext.Instance, b []byte) {
		f := field(inst, "lookup")
		entry := m.New(m.Lookup("RootLookupEntry"))
		kf := field(entry, "key")
		kf.Cell.Str = append([]byte(nil), b...)
		kf.Count = len(b)
		f.Entries = append(f.Entries, tabletext.Cell{Tab: entry})
	}

	bad := func(b []byte) []byte { return append([]byte(nil), b...) }
	texts := []struct {
		name string
		data []byte
	}{
		{"ff_fe", []byte{0xff, 0xfe}},
		{"zero", []byte{'a', 0x00, 'b'}},
		{"overlong", []byte{0xc0, 0x80}},
		{"surrogate", []byte{0xed, 0xa0, 0x80}},
	}
	// Go's variable writer refuses every ill-formed text but a zero byte in a
	// *string, which its blob branch does not test; that row is held on the
	// Squirrel side here.
	goTextField := func(location string) func(*tabletext.Model, *tabletext.Instance, []byte) {
		switch location {
		case "name":
			return func(_ *tabletext.Model, inst *tabletext.Instance, b []byte) { setStr(inst, "name", b) }
		case "text":
			return func(_ *tabletext.Model, inst *tabletext.Instance, b []byte) { setBlob(inst, "text", b) }
		case "names":
			return func(m *tabletext.Model, inst *tabletext.Instance, b []byte) { setNameText(m, inst, b) }
		default:
			return func(m *tabletext.Model, inst *tabletext.Instance, b []byte) { setMapKey(m, inst, b) }
		}
	}
	nutTextField := func(location string, data []byte) string {
		list := "[" + strings.Join(func() []string {
			parts := make([]string, len(data))
			for i, b := range data {
				parts[i] = fmt.Sprintf("0x%02x", b)
			}
			return parts
		}(), ", ") + "]"
		switch location {
		case "name":
			return "v.Name = bytesOf(" + list + ");"
		case "text":
			return "v.Text = bytesOf(" + list + ");"
		case "names":
			return "v.Names = [T.RootNamesText()]; v.Names[0].Value = bytesOf(" + list + ");"
		default:
			return "v.Lookup = [T.RootLookupEntry()]; v.Lookup[0].Key = bytesOf(" + list + ");"
		}
	}
	nutTextTwin := func(location string) string {
		switch location {
		case "name":
			return `v.Name = "abcd";`
		case "text":
			return `v.Text = "abcd";`
		case "names":
			return `v.Names = [T.RootNamesText()]; v.Names[0].Value = "abcd";`
		default:
			return `v.Lookup = [T.RootLookupEntry()]; v.Lookup[0].Key = "k";`
		}
	}
	goTextTwin := func(location string) func(*tabletext.Model, *tabletext.Instance) {
		switch location {
		case "name":
			return func(_ *tabletext.Model, inst *tabletext.Instance) { setStr(inst, "name", []byte("abcd")) }
		case "text":
			return func(_ *tabletext.Model, inst *tabletext.Instance) { setBlob(inst, "text", []byte("abcd")) }
		case "names":
			return func(m *tabletext.Model, inst *tabletext.Instance) { setNameText(m, inst, []byte("abcd")) }
		default:
			return func(m *tabletext.Model, inst *tabletext.Instance) { setMapKey(m, inst, []byte("k")) }
		}
	}

	cases := []offContractCase{
		{
			name:   "ranged_over",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "ranged", 50) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "ranged", 10) },
			nutBad: "v.Ranged = 50;", nutGood: "v.Ranged = 10;",
		},
		{
			name:   "ranged_under",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "ranged", 0) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "ranged", 1) },
			nutBad: "v.Ranged = 0;", nutGood: "v.Ranged = 1;",
		},
		{
			name:   "bits_over",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "bits6", 64) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "bits6", 63) },
			nutBad: "v.Bits6 = 64;", nutGood: "v.Bits6 = 63;",
		},
		{
			name:   "bits_negative",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "bits6", ^uint64(0)) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "bits6", 0) },
			nutBad: "v.Bits6 = -1;", nutGood: "v.Bits6 = 0;",
		},
		{
			name:   "counts_over",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "counts").Count = 4 },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "counts").Count = 3 },
			nutBad: "v.Counts = [1, 2, 3, 4];", nutGood: "v.Counts = [1, 2, 3];",
		},
		{
			name:   "name_long",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setStr(inst, "name", []byte("abcde")) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setStr(inst, "name", []byte("abcd")) },
			nutBad: `v.Name = "abcde";`, nutGood: `v.Name = "abcd";`,
		},
		{
			name: "raw_long",
			goBad: func(_ *tabletext.Model, inst *tabletext.Instance) {
				field(inst, "raw").Cell.Str = []byte{1, 2, 3, 4, 5}
				field(inst, "raw").Count = 5
			},
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) {
				field(inst, "raw").Cell.Str = []byte{1, 2, 3, 4}
				field(inst, "raw").Count = 4
			},
			nutBad: "v.Raw = blob(5);", nutGood: "v.Raw = blob(4);",
		},
		{
			name:   "enum_outside",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "mode", 9) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "mode", 3) },
			nutBad: `v.Mode = "Bogus";`, nutGood: `v.Mode = "Fly";`,
		},
		{
			name:   "union_outside",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "arm", 99) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "arm", 1) },
			nutBad: "v.Arm.Type = 99;", nutGood: "v.Arm.Type = 1; v.Arm.K = 7;",
		},
		{
			name:   "ratio_over",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "ratio").Cell.F = 1.5 },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "ratio").Cell.F = 1.0 },
			nutBad: "v.Ratio = 1.5;", nutGood: "v.Ratio = 1.0;",
		},
		{
			name:   "int8_over",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "small", 300) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "small", 127) },
			nutBad: "v.Small = 300;", nutGood: "v.Small = 127;",
		},
		{
			name:   "uint8_over",
			goBad:  func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "octet", 300) },
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { setU(inst, "octet", 255) },
			nutBad: "v.Octet = 300;", nutGood: "v.Octet = 255;",
		},
		{
			name:   "wrong_type_float",
			nutBad: "v.Small = 1.5;", nutGood: "v.Small = 1;",
		},
		{
			name:   "wrong_type_string",
			nutBad: `v.Small = "x";`, nutGood: "v.Small = 1;",
		},
		// a wrong Squirrel type in a float or a bool field: the Go model has
		// no way to hold one, so the Go side writes the lawful twin only
		{
			name:   "ratio_string",
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "ratio").Cell.F = 0.5 },
			nutBad: `v.Ratio = "x";`, nutGood: "v.Ratio = 0.5;",
		},
		{
			name:   "weights_string",
			nutBad: `v.Weights[1] = "x";`, nutGood: "v.Weights[1] = 0.5;",
		},
		{
			name:   "flag_string",
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "flag").Cell.B = true },
			nutBad: `v.Flag = "x";`, nutGood: "v.Flag = true;",
		},
		{
			name:   "flag_integer",
			goGood: func(_ *tabletext.Model, inst *tabletext.Instance) { field(inst, "flag").Cell.B = true },
			nutBad: "v.Flag = 1;", nutGood: "v.Flag = true;",
		},
	}
	for _, location := range []string{"name", "text", "names", "lookup"} {
		for _, text := range texts {
			c := offContractCase{
				name:    location + "_" + text.name,
				nutBad:  nutTextField(location, text.data),
				nutGood: nutTextTwin(location),
				goGood:  goTextTwin(location),
			}
			if !(location == "text" && text.name == "zero") {
				set := goTextField(location)
				data := bad(text.data)
				c.goBad = func(m *tabletext.Model, inst *tabletext.Instance) { set(m, inst, data) }
			}
			cases = append(cases, c)
		}
	}

	t.Run("go", func(t *testing.T) {
		t.Parallel()
		m := tabletext.NewModel(unit)
		for _, c := range cases {
			if c.goBad != nil {
				inst := m.New(m.Lookup("Root"))
				c.goBad(m, inst)
				out, err := tablewire.Encode(m, inst)
				if err == nil || out != nil {
					t.Errorf("%s: the Go variable writer answered %x, %v, want invalid_value", c.name, out, err)
				}
			}
			if c.goGood == nil {
				continue
			}
			inst := m.New(m.Lookup("Root"))
			c.goGood(m, inst)
			out, err := tablewire.Encode(m, inst)
			if err != nil || len(out) == 0 {
				t.Errorf("%s twin: the Go variable writer answered %x, %v, want bytes", c.name, out, err)
			}
		}
	})

	t.Run("squirrel", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mod := emitUnit(t, dir, "probe", unit)
		var p strings.Builder
		p.WriteString(strings.ReplaceAll(offContractSavePrelude, "__MOD__", mod))
		for _, c := range cases {
			fmt.Fprintf(&p, "probe(%q, function(v) { %s });\n", c.name, c.nutBad)
			fmt.Fprintf(&p, "probe(%q, function(v) { %s });\n", c.name+"_twin", c.nutGood)
		}
		writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
		out := string(sqrun.Run(t, dir, "main.nut", nil))
		got := map[string][]string{}
		for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			f := strings.Fields(line)
			if len(f) != 6 {
				t.Fatalf("probe line %q is not a case, a verdict, a length, a measure and the read-back", line)
			}
			got[f[0]] = f[1:]
		}
		for _, c := range cases {
			row, ok := got[c.name]
			if !ok {
				t.Errorf("%s: the probe printed no row", c.name)
				continue
			}
			if row[0] != "refused" || row[1] != "-" || row[2] != "-1" || row[3] != "none" || row[4] != "none" {
				t.Errorf("%s: Save/Measure answered %v, want refused with no bytes and no read-back (docs/SPEC.md §5.1)", c.name, row)
			}
			twin, ok := got[c.name+"_twin"]
			if !ok {
				t.Errorf("%s twin: the probe printed no row", c.name)
				continue
			}
			if twin[0] != "wrote" || twin[3] != "read" || twin[4] != "same" {
				t.Errorf("%s twin: Save/Load answered %v, want the boundary value written and read back exactly", c.name, twin)
				continue
			}
			n, err := strconv.Atoi(twin[1])
			if err != nil || n <= 0 {
				t.Errorf("%s twin: saved length %q is not a positive number", c.name, twin[1])
				continue
			}
			if twin[2] != twin[1] {
				t.Errorf("%s twin: Measure %s, Save %s bytes", c.name, twin[2], twin[1])
			}
		}
	})
}
