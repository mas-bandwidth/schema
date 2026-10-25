package rusttable_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
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
}

// cookWriteOwed is every root a cook-write row names that this leg's writer
// does not reach yet, with the construct that keeps it off. The test holds the
// list EXACT in both directions: a root here that gains a <t>_cook_write, or a
// root missing one that is not here, is a red test.
var cookWriteOwed = map[string]string{
	"scalars.SimState": "the fixed-point kind is outside the variable class this leg carries (§15)",
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
			t.Fatalf("%s: %v", inst[0], err)
		}
		has := false
		for name, data := range files {
			if strings.HasSuffix(name, "_table.rs") && bytes.Contains(data, []byte("pub fn "+ir.RustSnake(inst[1])+"_cook_write(")) {
				has = true
			}
		}
		_, owed := cookWriteOwed[key]
		switch {
		case has && owed:
			t.Errorf("%s now has a cook writer: take it off cookWriteOwed", key)
			continue
		case !has && !owed:
			t.Errorf("%s has no <t>_cook_write and is not on cookWriteOwed", key)
			continue
		case owed:
			reached[key] = true
			continue
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
			t.Fatal(err)
		}
		dir := filepath.Join(work, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var modules []string
		for file, data := range files {
			if file == rusttable.VariableRuntimeModule+".rs" || file == rusttable.BuildVersionModule+".rs" || strings.HasSuffix(file, "_table.rs") {
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
			t.Fatalf("witness %q: no carried row %s", rule, row)
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
