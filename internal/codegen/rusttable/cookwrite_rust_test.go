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
	// the feature witness, as testdata/conformance/tables/MANIFEST.txt maps it
	"featurewitnesses": "testdata/conformance/feature-witnesses/full/Features.schema",
	"tblfixedevo":      "test/tables/fixedevo",
	"tblvarevo1":       "test/tables/varevo/VarEvo1.schema",
	"tblvarevo3":       "test/tables/varevo/VarEvo3.schema",
	"cooktorture":      "test/tables/cooktorture/CookTorture.schema",
	"cooktorturewide":  "test/tables/cooktorturewide/CookTortureWide.schema",
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
var cookWriteOwed = map[string]string{}

type cookWriteCase struct {
	name, unit, root string
	wire             []byte
	input            []byte // a recipe's source value, before applying share
	share            string
	text             []byte // the value rendered for the Rust writer probe
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
	recipes := map[string]map[string]string{}
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
			recipes[fields[1]] = map[string]string{}
			for _, part := range fields[2:] {
				if key, value, ok := strings.Cut(part, "="); ok {
					recipes[fields[1]][key] = value
				}
			}
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
		if path := recipes[row]["input"]; path != "" {
			c.input, err = os.ReadFile(filepath.Join(repo, path))
			if err != nil {
				t.Fatal(err)
			}
			c.share = recipes[row]["share"]
			c.text = c.input
		} else if len(wire) > 0 && wire[0] == 3 {
			c.text, err = m.Write(cookCaseValue(t, m, c))
			if err != nil {
				t.Fatal(err)
			}
		}
		c.little = engineCook(t, m, c, nil, false)
		c.big = engineCook(t, m, c, nil, true)
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
			// integrity_runtime.rs is emitted only for a unit with an integrity
			// root (featurewitnesses), and its _table.rs imports it
			if file == rusttable.VariableRuntimeModule+".rs" || file == rusttable.BuildVersionModule+".rs" || file == rusttable.TableEnumsModule+".rs" || file == "integrity_runtime.rs" || strings.HasSuffix(file, "_table.rs") {
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
	main.WriteString("fn main() {\n")
	for _, c := range cases {
		path := filepath.Join(work, c.name+".wire")
		input, construct := c.wire, "load"
		if c.text != nil {
			path = filepath.Join(work, c.name+".json")
			input, construct = c.text, "from_json_bytes"
		}
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		snake := ir.RustSnake(c.root)
		fmt.Fprintf(&main, "    {\n        use crate::%s::%s as table;\n", c.unit, tableModule[c.unit+"."+c.root])
		fmt.Fprintf(&main, "        let input = std::fs::read(%q).unwrap();\n        let mut report = Default::default();\n        let mut value = table::%s_%s(&input, &mut report).expect(\"cook value construction refused\");\n", path, snake, construct)
		if c.share != "" {
			from, to, ok := strings.Cut(c.share, ":")
			if !ok || c.unit != "blobdemo" || c.root != "Catalog" || from != "thumb" || to != "alias" {
				t.Fatalf("unsupported recipe %s", c.share)
			}
			fmt.Fprint(&main, "        value.alias = value.thumb.clone();\n")
		}
		fmt.Fprintf(&main, "        for (order, suffix) in [(1u64, \"\"), (2u64, \"-be\")] {\n            let cooked = table::%s_cook_write(&value, order).expect(\"the cook refused\");\n            std::fs::write(format!(\"{}{}.cook\", %q, suffix), cooked).unwrap();\n        }\n        assert!(table::%s_cook_write(&value, 3).is_err());\n    }\n", snake, filepath.Join(work, c.name), snake)
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
		fmt.Fprintf(&main, "        let text = std::fs::read(%q).unwrap();\n", filepath.Join(work, fc.row+".json"))
		fmt.Fprintf(&main, "        let mut report = Default::default();\n        let mut value = table::%s_from_json_bytes(&text, &mut report).expect(\"finite probe JSON refused\");\n", snake)
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
			if bytes.Equal(engineCook(t, models[instances[row][0]], &cookWriteCase{root: instances[row][1], wire: wire}, plant, false),
				engineCook(t, models[instances[row][0]], &cookWriteCase{root: instances[row][1], wire: wire}, nil, false)) {
				t.Errorf("witness %q: the plant does not move the engine's cook of %s, so it proves nothing", rule, row)
			}
			t.Logf("witness %q: %s is owed to the load side (%s); the plant moves the engine's bytes and binds this leg once the row is carried", rule, row, cookWriteOwed[key])
			return
		}
		planted := engineCook(t, models[c.unit], c, plant, false)
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
	// DEAD STORAGE IS ZERO (§7.6): the engine now ignores a dead logical
	// value, so planting logical defaults no longer changes its image. Probe
	// the physical span independently and plant a wrong byte in that image.
	deadWitness := func(rule, row string, locate func(*ir.Unit, *tabletext.Instance) (int64, int64, bool)) {
		t.Helper()
		var c *cookWriteCase
		for _, candidate := range cases {
			if candidate.name == row {
				c = candidate
				break
			}
		}
		if c == nil {
			t.Fatalf("%s: no case %s", rule, row)
		}
		at, size, ok := locate(units[c.unit], cookCaseValue(t, models[c.unit], c))
		if !ok || size <= 0 {
			t.Fatalf("%s: no dead storage witness", rule)
		}
		for _, suffix := range []string{"", "-be"} {
			image := got[row+suffix]
			start, end := int64(64)+at, int64(64)+at+size
			if start < 64 || end > int64(len(image)) {
				t.Fatalf("%s: span outside image", rule)
			}
			for _, b := range image[start:end] {
				if b != 0 {
					t.Fatalf("%s: dead storage is nonzero", rule)
				}
			}
			wrong := bytes.Clone(image)
			wrong[start] = 1
			if bytes.Equal(image, wrong) {
				t.Fatalf("%s: wrong dead byte did not move the image", rule)
			}
		}
	}
	deadWitness("counted tail is zero", "root_full", func(u *ir.Unit, inst *tabletext.Instance) (int64, int64, bool) {
		fv, ok := inst.FieldByKey("weapons")
		if !ok || fv.Count >= len(fv.Elems) {
			return 0, 0, false
		}
		fl := ir.RecordLayout(u, inst.Def).FieldByName("weapons")
		pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
		stride := pieces[0].Size / fl.Field.ArrayBound
		return pieces[0].Offset + int64(fv.Count)*stride, (fl.Field.ArrayBound - int64(fv.Count)) * stride, true
	})
	deadWitness("absent optional payload is zero", "keyed_config", absentCookPayload)
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
	t.Logf("cook rows held %d, both-endian images %d; recipe/zero-dead/finite witnesses active", len(cases), 2*len(cases))
}

// cookCaseValue reads ordinary wire inputs, or constructs a recipe's source
// value and identity independently of the wire which must refuse shared blobs.
func cookCaseValue(t *testing.T, m *tabletext.Model, c *cookWriteCase) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(c.root))
	var rep tabletext.Report
	if c.input != nil {
		if !m.Read(inst, c.input, &rep) || !rep.Silent() {
			t.Fatalf("%s: recipe JSON refused: %+v", c.root, rep)
		}
		from, to, ok := strings.Cut(c.share, ":")
		if !ok {
			t.Fatalf("%s: recipe has no share pair", c.name)
		}
		source, a := inst.FieldByKey(from)
		target, b := inst.FieldByKey(to)
		if !a || !b || source.Cell.Blob == nil || !target.Def.Type.Blob() {
			t.Fatalf("%s: recipe sharing site is not a blob", c.name)
		}
		target.Cell.Blob = source.Cell.Blob
	} else if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
		t.Fatalf("%s: the oracle did not decode the wire: %v", c.root, err)
	}
	return inst
}

func engineCook(t *testing.T, m *tabletext.Model, c *cookWriteCase, plant func(*tabletext.Model, *tabletext.Instance) bool, big bool) []byte {
	t.Helper()
	inst := cookCaseValue(t, m, c)
	if plant != nil && !plant(m, inst) {
		t.Fatalf("%s: the witness found nothing to plant, so it proves nothing", c.root)
	}
	out, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
	if err != nil {
		t.Fatalf("%s: the engine did not cook: %v", c.root, err)
	}
	return out
}

// absentCookPayload finds one absent by-value optional's physical payload,
// following the IR's C-ABI layout rather than the generated Rust writer.
func absentCookPayload(u *ir.Unit, inst *tabletext.Instance) (int64, int64, bool) {
	for _, fl := range ir.RecordLayout(u, inst.Def).Fields {
		fv, ok := inst.FieldByKey(fl.Field.Name)
		if !ok {
			continue
		}
		if fl.Field.Type.Optional && !fv.Present {
			pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
			return fl.Offset, pieces[len(pieces)-1].Offset - fl.Offset, true
		}
		if fl.Field.Type.Pointer {
			continue
		}
		if fl.Field.Array != ir.ArrayNone {
			ref, ok := fl.Field.Type.Ref.(*ir.Struct)
			if !ok {
				continue
			}
			stride := ir.RecordLayout(u, ref).Size
			for i, cell := range fv.Elems {
				if cell.Tab == nil {
					continue
				}
				if at, size, found := absentCookPayload(u, cell.Tab); found {
					return fl.Offset + int64(i)*stride + at, size, true
				}
			}
		} else if fv.Cell.Tab != nil {
			if at, size, found := absentCookPayload(u, fv.Cell.Tab); found {
				return fl.Offset + at, size, true
			}
		}
	}
	return 0, 0, false
}

// cloneInstance is a SECOND node holding the same values: equal content, its
// own identity.
func cloneInstance(inst *tabletext.Instance) *tabletext.Instance {
	c := *inst
	c.Fields = append([]tabletext.Field(nil), inst.Fields...)
	return &c
}
