package rusttable_test

import (
	"bufio"
	"bytes"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookWriteUnits is where each unit the manifest's cook-write rows name is
// declared (make/rust.mk's RUST_TABLE_UNITS, for the units those rows reach).
var cookWriteUnits = map[string]string{
	"tabledemo":   "tables/examples",
	"graphdemo":   "tables/pointers",
	"blobdemo":    "tables/blobs",
	"streamdemo":  "tables/stream",
	"messagedemo": "tables/messages",
	"scalars":     "tables/scalars",
	"tblv1":       "test/tables/V1.schema",
	"tblv2":       "test/tables/V2.schema",
	"tblp1":       "test/tables/P1.schema",
	"tblp3":       "test/tables/P3.schema",
	"tblm1":       "test/tables/M1.schema",
	"tblm2":       "test/tables/M2.schema",
	"mapdemo":     "tables/maps",
	"listdemo":    "tables/lists",
	"widelist":    "tables/widelist",
}

// cookWriteOwed is every root a cook-write row names that has NO FORM-1 READER
// on this leg yet, with the construct that keeps it off the variable class.
// The cook writer itself reaches every construct those rows hold (the
// fixed-point kind and the 128-bit integers included); what such a root lacks
// is the storage struct a load produces and the writer is generated over. The
// rules are exact: a root with no <t>_load must be named here; a root WITH
// one must have its <t>_cook_write, and its rows run and pass byte for byte
// whether or not it is still named — an entry whose reader has landed is
// spent, and the run says so.
var cookWriteOwed = map[string]string{
}

type cookWriteCase struct {
	name, unit, root string
	wire             []byte
	little, big      []byte // the Go engine's cook, which the goldens hold too
}

// TestCookWriteRust holds §7.6's write side on this leg byte for byte against
// the engine `schema cook` runs (internal/tablecook): every manifest cook-write
// row whose root the Rust writer reaches is loaded through the root's form-1
// reader, cooked in both byte orders by the generated <t>_cook_write, and
// compared with tablecook.Cook over the oracle's decode of the same wire — the
// node numbering and its shared nodes and blobs, the C ABI layout, union arms,
// optionals, counted and keyed arrays, the region's alignment and the node
// directory. The REVERSED WITNESSES then plant each rule's wrong answer into
// the oracle's instance and prove the bytes this leg produced tell them apart,
// so a writer that broke any of those rules could not stay green here.
func TestCookWriteRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated cook-write probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	units := map[string]*ir.Unit{}
	models := map[string]*tabletext.Model{}
	loadUnit := func(name string) *ir.Unit {
		if u, ok := units[name]; ok {
			return u
		}
		rel, ok := cookWriteUnits[name]
		if !ok {
			t.Fatalf("a cook-write row names the unit %s, which this test does not map to a schema", name)
		}
		c := compiler.New()
		paths, err := compiler.GatherPaths([]string{filepath.Join(repo, rel)})
		if err != nil {
			t.Fatal(err)
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		units[name] = u
		models[name] = tabletext.NewModel(u)
		return u
	}

	// THE ROWS, from the manifest: each cook-write row reaches an instance,
	// and the instance names the unit, the root and the wire.
	instances := map[string][3]string{}
	var rows []string
	f, err := os.Open(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		switch {
		case len(fields) >= 5 && fields[0] == "instance":
			instances[fields[1]] = [3]string{fields[2], fields[3], fields[4]}
		case len(fields) >= 2 && fields[0] == "cook-write":
			rows = append(rows, fields[1])
		}
	}
	f.Close()
	if len(rows) == 0 {
		t.Fatal("the manifest has no cook-write row, so this test proves nothing")
	}

	var cases []*cookWriteCase
	reached := map[string]bool{}
	for _, row := range rows {
		inst, ok := instances[row]
		if !ok {
			t.Fatalf("cook-write %s names no instance", row)
		}
		u := loadUnit(inst[0])
		key := inst[0] + "." + inst[1]
		files, err := rusttable.Generate(u)
		if err != nil {
			// a unit this backend refuses by name is owed whole, and only a
			// root named on cookWriteOwed may be (§15)
			if _, owed := cookWriteOwed[key]; owed {
				reached[key] = true
				continue
			}
			t.Fatalf("%s: %v", inst[0], err)
		}
		has, loads := false, false
		for name, data := range files {
			if !strings.HasSuffix(name, "_table.rs") {
				continue
			}
			if bytes.Contains(data, []byte("pub fn "+ir.RustSnake(inst[1])+"_cook_write(")) {
				has = true
			}
			if bytes.Contains(data, []byte("pub fn "+ir.RustSnake(inst[1])+"_load(")) {
				loads = true
			}
		}
		_, owed := cookWriteOwed[key]
		reached[key] = true
		switch {
		case !loads && !owed:
			t.Errorf("%s has no form-1 reader on this leg and is not on cookWriteOwed", key)
			continue
		case !loads:
			continue // owed to the load side, by name
		case !has:
			t.Errorf("%s has its form-1 reader but no <t>_cook_write: the writer must reach every root the leg loads", key)
			continue
		case owed:
			t.Logf("cookWriteOwed's %s is spent: its form-1 reader has landed, so its rows run; take it off the list", key)
		}
		wire, err := os.ReadFile(filepath.Join(repo, inst[2]))
		if err != nil {
			t.Fatal(err)
		}
		c := &cookWriteCase{name: row, unit: inst[0], root: inst[1], wire: wire}
		m := models[inst[0]]
		c.little = engineCook(t, m, c.root, wire, nil, false)
		c.big = engineCook(t, m, c.root, wire, nil, true)
		// THE PREMISE: the engine's bytes are the committed goldens
		for big, got := range map[bool][]byte{false: c.little, true: c.big} {
			name := row
			if big {
				name += "-be"
			}
			golden, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "cook-write", name+".cook"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(golden, got) {
				t.Fatalf("%s: the engine's cook is not the committed golden, so the oracle moved", name)
			}
		}
		cases = append(cases, c)
	}
	for key := range cookWriteOwed {
		if !reached[key] {
			t.Errorf("cookWriteOwed names %s, which no cook-write row reaches", key)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// ONE PROBE BINARY: each unit is a module holding its variable runtime, its
	// build version and its codec modules, with the cook feature on.
	var mods, main strings.Builder
	unitNames := make([]string, 0, len(units))
	for name := range units {
		unitNames = append(unitNames, name)
	}
	sort.Strings(unitNames)
	tableModule := map[string]string{}
	for _, name := range unitNames {
		u := units[name]
		files, err := rusttable.Generate(u)
		if err != nil {
			continue // a unit the backend refuses by name, owed above
		}
		dir := filepath.Join(work, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var modules []string
		for file, data := range files {
			if file == rusttable.VariableRuntimeModule+".rs" || file == rusttable.BuildVersionModule+".rs" || file == rusttable.TableEnumsModule+".rs" || strings.HasSuffix(file, "_table.rs") {
				if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
					t.Fatal(err)
				}
				modules = append(modules, strings.TrimSuffix(file, ".rs"))
			}
			if strings.HasSuffix(file, "_table.rs") {
				for _, st := range u.Tables {
					if bytes.Contains(data, []byte("pub fn "+ir.RustSnake(st.Name)+"_cook_write(")) {
						tableModule[name+"."+st.Name] = strings.TrimSuffix(file, ".rs")
					}
				}
			}
		}
		sort.Strings(modules)
		fmt.Fprintf(&mods, "#[allow(dead_code, unused_imports)]\nmod %s {\n    pub use self::%s::*;\n", name, rusttable.BuildVersionModule)
		for _, mod := range modules {
			fmt.Fprintf(&mods, "    #[path = %q]\n    pub mod %s;\n", filepath.Join(dir, mod+".rs"), mod)
		}
		mods.WriteString("}\n")
	}
	main.WriteString(`
macro_rules! cook_case {
    ($name:expr, $wire:expr, $m:path, $load:ident, $cook:ident) => {{
        use $m as table;
        let wire = std::fs::read($wire).unwrap();
        let mut report = Default::default();
        let value = table::$load(&wire, &mut report).expect("the form-1 load refused the wire");
        for (order, suffix) in [(1u64, ""), (2u64, "-be")] {
            let cooked = table::$cook(&value, order).expect("the cook refused");
            std::fs::write(format!("{}{}.cook", $name, suffix), cooked).unwrap();
        }
        // a byte order that is neither refuses, and produces nothing
        assert!(table::$cook(&value, 3).is_err());
    }};
}

fn main() {
`)
	for _, c := range cases {
		wirePath := filepath.Join(work, c.name+".wire")
		if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
			t.Fatal(err)
		}
		snake := ir.RustSnake(c.root)
		fmt.Fprintf(&main, "    cook_case!(%q, %q, crate::%s::%s, %s_load, %s_cook_write);\n",
			filepath.Join(work, c.name), wirePath, c.unit, tableModule[c.unit+"."+c.root], snake, snake)
	}
	// A FLOAT IS FINITE in the cook too (docs/SPEC-TABLES.md §3, §3.3): the
	// same loaded value that cooked above, one float set to a NaN or an
	// infinity, must be REFUSED in both byte orders, as the engine's
	// tablewire.RefuseNonFinite refuses it. The control is the row's own
	// cook above: the refusal is the planted float's and nothing else's.
	for _, fc := range []struct{ row, root, field, ty string }{
		{"root_full", "RootConfig", "weapons[0].damage", "f32"},
		{"profile_elide", "ProfileConfig", "precision", "f64"},
	} {
		mod, ok := tableModule["tabledemo."+fc.root]
		if !ok {
			t.Fatalf("finite: tabledemo.%s has no cook writer", fc.root)
		}
		snake := ir.RustSnake(fc.root)
		fmt.Fprintf(&main, "    {\n        use crate::tabledemo::%s as table;\n", mod)
		fmt.Fprintf(&main, "        let wire = std::fs::read(%q).unwrap();\n", filepath.Join(work, fc.row+".wire"))
		fmt.Fprintf(&main, "        let mut report = Default::default();\n        let mut value = table::%s_load(&wire, &mut report).expect(\"the form-1 load refused the wire\");\n", snake)
		fmt.Fprintf(&main, "        for bad in [%[1]s::NAN, %[1]s::INFINITY, %[1]s::NEG_INFINITY] {\n            value.%[2]s = bad;\n", fc.ty, fc.field)
		fmt.Fprintf(&main, "            for order in [1u64, 2u64] {\n                if table::%s_cook_write(&value, order).is_ok() {\n", snake)
		fmt.Fprintf(&main, "                    println!(\"FINITE: %s cooked a non-finite %s ({bad}) in order {order}\");\n                    std::process::exit(3);\n                }\n            }\n        }\n    }\n", fc.row, fc.field)
	}
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(mods.String()+main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "--cfg", `feature="cook"`, "-o", bin, "main.rs")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the cook writer did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the cook probe did not run: %v\n%s", err, out)
	}
	got := map[string][]byte{}
	for _, c := range cases {
		for _, suffix := range []string{"", "-be"} {
			data, err := os.ReadFile(filepath.Join(work, c.name+suffix+".cook"))
			if err != nil {
				t.Fatal(err)
			}
			got[c.name+suffix] = data
		}
		if !bytes.Equal(got[c.name], c.little) {
			t.Errorf("%s: the little-endian cook is not the engine's (%d bytes against %d)", c.name, len(got[c.name]), len(c.little))
		}
		if !bytes.Equal(got[c.name+"-be"], c.big) {
			t.Errorf("%s: the big-endian cook is not the engine's (%d bytes against %d)", c.name, len(got[c.name+"-be"]), len(c.big))
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// THE REVERSED WITNESSES: each rule's wrong answer, planted into the
	// oracle's instance, must cook to bytes this leg's writer did NOT produce.
	witness := func(rule, row string, plant func(*tabletext.Model, *tabletext.Instance) bool) {
		t.Helper()
		var c *cookWriteCase
		for _, k := range cases {
			if k.name == row {
				c = k
			}
		}
		if c == nil {
			// a row owed to the load side still proves its plant MOVES the
			// engine's bytes, so the witness is not vacuous the day it binds
			key := ""
			if inst, ok := instances[row]; ok {
				key = inst[0] + "." + inst[1]
			}
			if _, owed := cookWriteOwed[key]; !owed {
				t.Fatalf("witness %q: no carried row %s", rule, row)
			}
			wire, err := os.ReadFile(filepath.Join(repo, instances[row][2]))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(engineCook(t, models[instances[row][0]], instances[row][1], wire, plant, false),
				engineCook(t, models[instances[row][0]], instances[row][1], wire, nil, false)) {
				t.Errorf("witness %q: the plant does not move the engine's cook of %s, so it proves nothing", rule, row)
			}
			t.Logf("witness %q: %s is owed to the load side (%s); the plant moves the engine's bytes and binds this leg once the row is carried", rule, row, cookWriteOwed[key])
			return
		}
		planted := engineCook(t, models[c.unit], c.root, c.wire, plant, false)
		if bytes.Equal(planted, got[c.name]) {
			t.Errorf("witness %q: the planted answer cooks to the bytes this leg wrote for %s, so the row does not hold the rule", rule, row)
		}
	}
	// a node two slots name is ONE node, written once (§3.1, §6.3)
	witness("a shared node is one node", "graph_shared", func(m *tabletext.Model, inst *tabletext.Instance) bool {
		for i := range inst.Fields {
			fv := &inst.Fields[i]
			if fv.Def.Name == "alias" && fv.Cell.Node != nil {
				fv.Cell.Node = cloneInstance(fv.Cell.Node)
				return true
			}
		}
		return false
	})
	// a blob two slots name is ONE blob node (§2.5): Lock and Cook pack the
	// graph as they find it
	witness("a shared blob is one node", "blob_shared", func(m *tabletext.Model, inst *tabletext.Instance) bool {
		for i := range inst.Fields {
			fv := &inst.Fields[i]
			if fv.Def.Name == "alias" && fv.Cell.Blob != nil {
				copyBlob := *fv.Cell.Blob
				fv.Cell.Blob = &copyBlob
				return true
			}
		}
		return false
	})
	// a slot past a counted array's live count holds the element type's own
	// declared defaults, not zero (internal/tabletext/value.go's elementZero)
	witness("a slot past the count holds the element's defaults", "root_full", func(m *tabletext.Model, inst *tabletext.Instance) bool {
		for i := range inst.Fields {
			fv := &inst.Fields[i]
			if fv.Def.Name == "weapons" && fv.Count < len(fv.Elems) {
				for k := fv.Count; k < len(fv.Elems); k++ {
					fv.Elems[k] = tabletext.Cell{Tab: zeroInstance(m, fv.Elems[k].Tab)}
				}
				return true
			}
		}
		return false
	})
	// an ABSENT optional holds its declared default (§2.3)
	witness("an absent optional holds its default", "keyed_config", func(m *tabletext.Model, inst *tabletext.Instance) bool {
		planted := false
		walkInstances(inst, func(sub *tabletext.Instance) {
			for i := range sub.Fields {
				fv := &sub.Fields[i]
				if fv.Def.Type.Optional && !fv.Present && fv.Cell.Tab != nil {
					fv.Cell.Tab = zeroInstance(m, fv.Cell.Tab)
					planted = true
				}
			}
		})
		return planted
	})
	// a FIXED-POINT slot holds its RAW integer, units × 2^F, never the whole
	// units (§4.3, §7.2): plant the truncated units into the oracle
	witness("a fixed-point slot holds the raw integer", "scalars_full", func(m *tabletext.Model, inst *tabletext.Instance) bool {
		for i := range inst.Fields {
			fv := &inst.Fields[i]
			if fv.Def.Type.Kind == ir.TFixed && fv.Def.Type.FracBits > 0 && fv.Def.Array == ir.ArrayNone && fv.Cell.Wide != nil && fv.Cell.Wide.Sign() != 0 {
				fv.Cell.Wide = new(big.Int).Rsh(fv.Cell.Wide, uint(fv.Def.Type.FracBits))
				return true
			}
		}
		return false
	})
	// a 128-bit slot holds all sixteen bytes (§7.2): plant the value cut to
	// its low half, which a writer through a u64 would lay down
	witness("a 128-bit slot keeps its high half", "scalars_full", func(m *tabletext.Model, inst *tabletext.Instance) bool {
		mask := new(big.Int).SetUint64(^uint64(0))
		for i := range inst.Fields {
			fv := &inst.Fields[i]
			if fv.Def.Type.Width == 128 && fv.Def.Array == ir.ArrayNone && fv.Cell.Wide != nil {
				low := new(big.Int).And(new(big.Int).Abs(fv.Cell.Wide), mask)
				if low.Cmp(new(big.Int).Abs(fv.Cell.Wide)) != 0 {
					fv.Cell.Wide = low
					return true
				}
			}
		}
		return false
	})
	// the byte order is the write's parameter, never the host's (§7.6)
	for _, c := range cases {
		if bytes.Equal(got[c.name], got[c.name+"-be"]) {
			t.Errorf("%s: the two byte orders cooked to one file", c.name)
		}
	}
}

// engineCook decodes a wire with the oracle, plants an optional change into
// the instance, and cooks it with the engine `schema cook` runs.
func engineCook(t *testing.T, m *tabletext.Model, root string, wire []byte, plant func(*tabletext.Model, *tabletext.Instance) bool, big bool) []byte {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var rep tabletext.Report
	if ok, err := tablewire.Decode(m, inst, wire, &rep); !ok || err != nil {
		t.Fatalf("%s: the oracle did not decode the wire: %v", root, err)
	}
	if plant != nil && !plant(m, inst) {
		t.Fatalf("%s: the witness found nothing to plant, so it proves nothing", root)
	}
	out, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
	if err != nil {
		t.Fatalf("%s: the engine did not cook: %v", root, err)
	}
	return out
}

// cloneInstance is a SECOND node holding the same values: equal content, its
// own identity.
func cloneInstance(inst *tabletext.Instance) *tabletext.Instance {
	c := *inst
	c.Fields = append([]tabletext.Field(nil), inst.Fields...)
	return &c
}

// zeroInstance is a record of the same type with every field zero, which is
// what a writer that ignored declared defaults would lay down.
func zeroInstance(m *tabletext.Model, like *tabletext.Instance) *tabletext.Instance {
	z := m.New(like.Def)
	for i := range z.Fields {
		z.Fields[i].Cell = tabletext.Cell{}
		z.Fields[i].Count = 0
		for k := range z.Fields[i].Elems {
			z.Fields[i].Elems[k] = tabletext.Cell{}
		}
	}
	return z
}

// walkInstances visits an instance and every record it holds by value.
func walkInstances(inst *tabletext.Instance, visit func(*tabletext.Instance)) {
	if inst == nil {
		return
	}
	visit(inst)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		walkInstances(fv.Cell.Tab, visit)
		for k := range fv.Elems {
			walkInstances(fv.Elems[k].Tab, visit)
		}
	}
}
