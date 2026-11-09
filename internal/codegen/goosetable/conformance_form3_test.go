package goosetable_test

// THE GOOSE DRIVER's FORM-3 DISPATCH (docs/SPEC-TABLES.md §3.4). The
// conformance driver loads and saves variable-table instances through the
// variable table wire's `load_table`/`save_table`, which refuse form 3 — the
// FIXED form — by name (wire_load.goose, "any form but 1 is refused"). A fixed
// root's file is form 3, so json-read, json-write and cook-write must dispatch
// a fixed root through its typed record (<Root>FixedLoad, <Root>FixedSave, and
// its generated or descriptor-driven value bridge) and every other root to the
// flat value table, and every fixed root's json-read and json-write rows must
// answer the committed bytes, exactly as the harness holds them.
//
// TestGooseDriverFormThreeDispatchNegativeControl takes the form-3 dispatch
// out of the glue (the driver's behavior before this fix) and requires the
// fixed-root json-read and json-write rows to turn red while the variable rows
// stay green.
//
// Unit tests here use no real time, no socket and no network: each run is
// `goose run` of a generated program in t.TempDir().

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const formThreeFixedHelpers = `
recursive fn fixed_tab_size(unit: schema_rt::LoadUnit, tab: i64) -> i64 {
    if tab < 0 || tab >= unit.tables.len { return 0; }
    var total: i64 = 0;
    for f in unit.tables[tab].fields {
        total += fixed_field_size(unit, f);
    }
    return total;
}
recursive fn fixed_union_arm_bytes(unit: schema_rt::LoadUnit, ui: i64) -> i64 {
    if ui < 0 || ui >= unit.unions.len { return 0; }
    var m: i64 = 0;
    for a in unit.unions[ui].arms {
        if a.shape != schema_rt::ShapeVoid {
            let sz = fixed_field_size(unit, a);
            if sz > m { m = sz; }
        }
    }
    return m;
}
recursive fn fixed_field_inner_size(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc) -> i64 {
    if f.shape == schema_rt::ShapeBool { return 1; }
    if f.shape == schema_rt::ShapeInt || f.shape == schema_rt::ShapeFloat { return f.kw as i64; }
    if f.shape == schema_rt::ShapeEnum {
        if f.variants.len >= 65536 { return 4; }
        if f.variants.len >= 256 { return 2; }
        return 1;
    }
    if f.shape == schema_rt::ShapeStr || f.shape == schema_rt::ShapeBytes {
        let cw: i64 = if f.wide { 2 } else { 1 };
        return 4 + f.bound * cw;
    }
    if f.shape == schema_rt::ShapeTab { return fixed_tab_size(unit, schema_rt::value_table(unit, f.ref)); }
    if f.shape == schema_rt::ShapeUnion { return 1 + fixed_union_arm_bytes(unit, schema_rt::value_union(unit, f.ref)); }
    if f.shape == schema_rt::ShapeArr {
        let elem = f.elem;
        guard elem else { return 0; }
        let esz = fixed_field_size(unit, elem);
        if f.array == schema_rt::ArrayCounted { return 4 + f.bound * esz; }
        return f.bound * esz;
    }
    return 0;
}
recursive fn fixed_field_size(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc) -> i64 {
    let inner = fixed_field_inner_size(unit, f);
    return if f.optional { 1 + inner } else { inner };
}
recursive fn fixed_tab_to_loaded(unit: schema_rt::LoadUnit, tab: i64, b: const u8[:], pos: i64&, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {
    if tab < 0 || tab >= unit.tables.len { return -1; }
    let t = unit.tables[tab];
    let rec = schema_rt::value_reserve(vals, t.fields.len + 1);
    vals[rec] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeTab, made: true, tab: tab, first: rec + 1, count: t.fields.len, .. };
    var i: i64 = 0;
    while i < t.fields.len {
        if !fixed_field_to_loaded(unit, t.fields[i], b, pos, data, vals, rec + 1 + i) { return -1; }
        i += 1;
    }
    return rec;
}
recursive fn fixed_field_to_loaded(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc, b: const u8[:], pos: i64&, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, s: i64) -> bool {
    if f.optional {
        let pres = b[pos] != 0;
        pos += 1;
        if !pres {
            pos += fixed_field_inner_size(unit, f);
            return true;
        }
    }
    if f.shape == schema_rt::ShapeBool {
        let v: u64 = if b[pos] != 0 { 1 } else { 0 };
        pos += 1;
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: f.shape, bits: schema_rt::Int128 { lo: v, hi: 0 }, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeInt || f.shape == schema_rt::ShapeFloat {
        let kw = f.kw as i64;
        var lo: u64 = 0;
        var hi: u64 = 0;
        if kw == 16 {
            lo = schema_rt::fixed_get(b, pos, 8);
            hi = schema_rt::fixed_get(b, pos + 8, 8);
        } else {
            lo = schema_rt::fixed_get(b, pos, kw);
            if f.shape == schema_rt::ShapeInt && f.signed {
                let sign_bit: u64 = (1 as u64) << ((kw * 8 - 1) as! u64);
                if (lo & sign_bit) != 0 {
                    if kw < 8 {
                        lo |= 0xffffffffffffffff << ((kw * 8) as! u64);
                    }
                    hi = 0xffffffffffffffff;
                }
            }
        }
        pos += kw;
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: f.shape, bits: schema_rt::Int128 { lo: lo, hi: hi }, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeEnum {
        let ew: i64 = if f.variants.len >= 65536 { 4 } else if f.variants.len >= 256 { 2 } else { 1 };
        let ord = schema_rt::fixed_get(b, pos, ew);
        pos += ew;
        vals[s] = schema_rt::LoadVal { state: if ord == 0 { schema_rt::ValEmpty } else { schema_rt::ValPresent }, shape: f.shape, bits: schema_rt::Int128 { lo: ord, hi: 0 }, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeStr || f.shape == schema_rt::ShapeBytes {
        let cw: i64 = if f.wide { 2 } else { 1 };
        let used = schema_rt::fixed_u32(b, pos) as i64;
        let nbytes = used * cw;
        let off = data.len;
        data.append(b[pos + 4 .. pos + 4 + nbytes]);
        pos += 4 + f.bound * cw;
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: f.shape, off: off, len: nbytes, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeTab {
        let sub = fixed_tab_to_loaded(unit, schema_rt::value_table(unit, f.ref), b, pos, data, vals);
        if sub < 0 { return false; }
        vals[s] = vals[sub];
        return true;
    }
    if f.shape == schema_rt::ShapeUnion {
        let ui = schema_rt::value_union(unit, f.ref);
        if ui < 0 { return false; }
        let u = unit.unions[ui];
        let max_arm = fixed_union_arm_bytes(unit, ui);
        let tag = schema_rt::fixed_get(b, pos, 1) as i64;
        let rec = schema_rt::value_reserve(vals, 2);
        vals[rec] = schema_rt::LoadVal { state: if tag == 0 { schema_rt::ValEmpty } else { schema_rt::ValPresent }, shape: schema_rt::ShapeUnion, made: true, tab: ui, bits: schema_rt::Int128 { lo: tag as! u64, hi: 0 }, first: rec + 1, count: 1, .. };
        if tag >= 1 && tag <= u.arms.len && u.arms[tag - 1].shape != schema_rt::ShapeVoid {
            var arm_pos = pos + 1;
            if !fixed_field_to_loaded(unit, u.arms[tag - 1], b, arm_pos, data, vals, rec + 1) { return false; }
        }
        pos += 1 + max_arm;
        vals[s] = vals[rec];
        return true;
    }
    if f.shape == schema_rt::ShapeArr {
        let elem = f.elem;
        guard elem else { return false; }
        var live = f.bound;
        if f.array == schema_rt::ArrayCounted {
            live = schema_rt::fixed_u32(b, pos) as i64;
            pos += 4;
        }
        let first = schema_rt::value_reserve(vals, live);
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeArr, made: true, first: first, count: live, len: live, .. };
        var k: i64 = 0;
        while k < live {
            if !fixed_field_to_loaded(unit, elem, b, pos, data, vals, first + k) { return false; }
            k += 1;
        }
        if f.array == schema_rt::ArrayCounted {
            pos += (f.bound - live) * fixed_field_size(unit, elem);
        }
        return true;
    }
    return false;
}
fn fixed_body_to_loaded(unit: schema_rt::LoadUnit, root: const u8[:], b: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {
    var pos: i64 = 0;
    return fixed_tab_to_loaded(unit, schema_rt::value_table(unit, root), b, pos, data, vals);
}
recursive fn fixed_tab_from_loaded(unit: schema_rt::LoadUnit, tab: i64, data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64, out: u8[>..]&) -> bool {
    if tab < 0 || tab >= unit.tables.len { return false; }
    let t = unit.tables[tab];
    var i: i64 = 0;
    while i < t.fields.len {
        let s = schema_rt::value_child(vals, rec, i);
        if !fixed_field_from_loaded(unit, t.fields[i], data, vals, s, out) { return false; }
        i += 1;
    }
    return true;
}
recursive fn fixed_field_from_loaded(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc, data: const u8[:], vals: const schema_rt::LoadVal[:], s: i64, out: u8[>..]&) -> bool {
    let rode = schema_rt::value_rode(vals, s);
    if f.optional {
        schema_rt::fixed_put_bool(out, rode);
        if !rode {
            schema_rt::fixed_put_zeros(out, fixed_field_inner_size(unit, f));
            return true;
        }
    }
    if f.shape == schema_rt::ShapeBool {
        let bv = if rode { vals[s].bits.lo != 0 } else { f.has_def && f.def_b };
        schema_rt::fixed_put_bool(out, bv);
        return true;
    }
    if f.shape == schema_rt::ShapeInt {
        let kw = f.kw as i64;
        let bits = if rode { vals[s].bits } else if f.has_def { f.def_i } else { schema_rt::Int128 { .. } };
        if kw == 16 {
            schema_rt::fixed_put(out, bits.lo, 8);
            schema_rt::fixed_put(out, bits.hi, 8);
        } else {
            schema_rt::fixed_put(out, bits.lo, kw);
        }
        return true;
    }
    if f.shape == schema_rt::ShapeFloat {
        let kw = f.kw as i64;
        let fb = if rode { vals[s].bits.lo } else if f.has_def { if f.is_f32 { schema_rt::fixed_f32_bits(f.def_f as f32) } else { schema_rt::fixed_f64_bits(f.def_f) } } else { 0 };
        schema_rt::fixed_put(out, fb, kw);
        return true;
    }
    if f.shape == schema_rt::ShapeEnum {
        let ew: i64 = if f.variants.len >= 65536 { 4 } else if f.variants.len >= 256 { 2 } else { 1 };
        let ev = if rode { vals[s].bits.lo } else if f.has_def { f.def_i.lo } else { 0 };
        schema_rt::fixed_put(out, ev, ew);
        return true;
    }
    if f.shape == schema_rt::ShapeStr || f.shape == schema_rt::ShapeBytes {
        let cw: i64 = if f.wide { 2 } else { 1 };
        if rode {
            let off = vals[s].off;
            let n = vals[s].len;
            if off < 0 || n < 0 || off > data.len || n > data.len - off { return false; }
            let units = n / cw;
            if units > f.bound || n != units * cw { return false; }
            schema_rt::fixed_put(out, units as! u64, 4);
            out.append(data[off .. off + n]);
            schema_rt::fixed_put_zeros(out, (f.bound - units) * cw);
            return true;
        }
        let def_bytes = if f.has_def && !f.wide { f.def_s } else { "" };
        let units = def_bytes.len / cw;
        if units > f.bound || def_bytes.len != units * cw { return false; }
        schema_rt::fixed_put(out, units as! u64, 4);
        out.append(def_bytes);
        schema_rt::fixed_put_zeros(out, (f.bound - units) * cw);
        return true;
    }
    if f.shape == schema_rt::ShapeTab {
        return fixed_tab_from_loaded(unit, schema_rt::value_table(unit, f.ref), data, vals, if rode { s } else { -1 }, out);
    }
    if f.shape == schema_rt::ShapeUnion {
        let ui = schema_rt::value_union(unit, f.ref);
        if ui < 0 { return false; }
        let u = unit.unions[ui];
        let max_arm = fixed_union_arm_bytes(unit, ui);
        let tag = if rode { vals[s].bits.lo as! i64 } else { 0 };
        if tag < 0 || tag > u.arms.len { return false; }
        schema_rt::fixed_put(out, tag as! u64, 1);
        if tag == 0 {
            schema_rt::fixed_put_zeros(out, max_arm);
            return true;
        }
        let arm = u.arms[tag - 1];
        let before = out.len;
        if arm.shape != schema_rt::ShapeVoid {
            let p = if rode && vals[s].made { vals[s].first } else { -1 };
            if !fixed_field_from_loaded(unit, arm, data, vals, p, out) { return false; }
        }
        let used = out.len - before;
        if used < max_arm {
            schema_rt::fixed_put_zeros(out, max_arm - used);
        }
        return true;
    }
    if f.shape == schema_rt::ShapeArr {
        let elem = f.elem;
        guard elem else { return false; }
        let made = rode && vals[s].made;
        var live = f.bound;
        if f.array == schema_rt::ArrayCounted {
            live = if made { vals[s].len } else { f.min };
            if live < f.min || live > f.bound { return false; }
            schema_rt::fixed_put(out, live as! u64, 4);
        }
        var k: i64 = 0;
        while k < live {
            let es = if made && k < vals[s].count { vals[s].first + k } else { -1 };
            if !fixed_field_from_loaded(unit, elem, data, vals, es, out) { return false; }
            k += 1;
        }
        if f.array == schema_rt::ArrayCounted {
            schema_rt::fixed_put_zeros(out, (f.bound - live) * fixed_field_size(unit, elem));
        }
        return true;
    }
    return false;
}
fn fixed_body_from_loaded(unit: schema_rt::LoadUnit, root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64, out: u8[>..]&) -> bool {
    let tab = schema_rt::value_table(unit, root);
    if !schema_rt::value_rode(vals, rec) || !vals[rec].made || vals[rec].tab != tab { return false; }
    return fixed_tab_from_loaded(unit, tab, data, vals, rec, out);
}
`

// formThreeCorpus is the manifest's json-read and json-write rows with their
// expectations resolved: json-read wants the instance's wire, json-write the
// instance's canonical JSON.
type formThreeCorpus struct {
	abs       string
	units     map[string][]string
	order     []string
	manifest  string
	unitOf    map[string]map[string]string // surface -> case -> unit
	rootOf    map[string]string            // case -> the root's name
	fixed     map[string]bool              // case -> the root is a fixed table
	unitFixed map[string]bool              // unit -> it has a fixed-root instance
	want      map[string]map[string]string // surface -> case -> expected answer
}

func readFormThreeCorpus(t *testing.T) *formThreeCorpus {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	c := &formThreeCorpus{
		abs:       abs,
		units:     map[string][]string{},
		rootOf:    map[string]string{},
		fixed:     map[string]bool{},
		unitFixed: map[string]bool{},
		unitOf:    map[string]map[string]string{},
		want:      map[string]map[string]string{},
	}
	for _, s := range []string{"json-read", "json-write"} {
		c.want[s] = map[string]string{}
		c.unitOf[s] = map[string]string{}
	}
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	var m strings.Builder
	mf, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()
	sc := bufio.NewScanner(mf)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			c.units[f[1]] = f[2:]
			c.order = append(c.order, f[1])
		case f[0] == "instance" && len(f) >= 5:
			if slices.Contains(f[5:], "no-text") {
				continue
			}
			wire := read(f[4])
			c.fixed[f[1]] = len(wire) > 0 && wire[0] == 3
			if c.fixed[f[1]] {
				c.unitFixed[f[2]] = true
			}
			c.rootOf[f[1]] = f[3]
			json := read("testdata/conformance/tables/json/" + f[1] + ".json")
			c.want["json-read"][f[1]], c.unitOf["json-read"][f[1]] = wire, f[2]
			c.want["json-write"][f[1]], c.unitOf["json-write"][f[1]] = json, f[2]
			fmt.Fprintf(&m, "instance %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	c.manifest = m.String()
	return c
}

// formThreeUnitFiles generates one unit for Goose, the conformance program and
// its full text glue, including the form-3 dispatch. With sabotageDispatch,
// the dispatch's fixed-root branches are omitted (the driver's pre-fix
// behavior).
func formThreeUnitFiles(t *testing.T, key string, paths []string, override map[string][]byte, sabotageDispatch bool) map[string][]byte {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("%s does not load: %v", key, err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("%s: goose: %v", key, err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("%s: goosetable: %v", key, err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, override)
	mods := make([]string, 0, len(files))
	for name := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
	}
	sort.Strings(mods)
	var glue strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&glue, "import .%s;\n", m)
	}
	ns := goose.Namespace(u)
	fmt.Fprintf(&glue, "fn conformance_unit() -> schema_rt::LoadUnit { return %s::%s; }\n", ns, goosetable.UnitName)

	names := func(p string) []string {
		re := regexp.MustCompile(p)
		var out []string
		for _, code := range tables {
			for _, m := range re.FindAllStringSubmatch(string(code), -1) {
				out = append(out, m[1])
			}
		}
		return dedupe(out)
	}
	toLoaded := names(`fn ([A-Za-z0-9_]*)ToLoaded\(`)
	fixedLoad := names(`fn ([A-Za-z0-9_]*)FixedLoad\(`)
	fromLoaded := names(`fn ([A-Za-z0-9_]*)FromLoaded\(`)
	fixedSave := names(`fn ([A-Za-z0-9_]*)FixedSave\(`)
	has := func(list []string, r string) bool {
		return slices.Contains(list, r)
	}
	needFixedHelpers := false
	for _, r := range fixedLoad {
		if !has(toLoaded, r) {
			needFixedHelpers = true
		}
	}

	glue.WriteString("fn conformance_wire(root: const u8[:], data: const u8[:], out: u8[>..<]&) -> i64 {\n")
	for _, st := range ir.TableFixedFormRoots(u) {
		if !has(fixedLoad, st.Name) {
			continue
		}
		fmt.Fprintf(&glue, "    if root == %q {\n", st.Name)
		fmt.Fprintf(&glue, "        var values: %s::%s[256] = [%s::%s { .. }; 256];\n", ns, st.Name, ns, st.Name)
		glue.WriteString("        var rep = schema_rt::FixedReport { .. };\n")
		fmt.Fprintf(&glue, "        let n = %s::%sFixedLoad(values[0..256], data, rep);\n", ns, st.Name)
		glue.WriteString("        if n < 0 || rep.damaged || rep.reason != 0 || rep.oor != 0 { return -3; }\n")
		glue.WriteString("        var saved: u8[>..] = [];\n")
		fmt.Fprintf(&glue, "        if %s::%sFixedSave(values[0..n], saved) < 0 { return -4; }\n", ns, st.Name)
		glue.WriteString("        out.append(saved);\n")
		glue.WriteString("        return 0;\n    }\n")
	}
	glue.WriteString("    return -1;\n}\n")
	glue.WriteString("fn conformance_message(root: const u8[:], announce: const u8[:], wire: const u8[:], out: u8[>..]&) -> i64 { return -1; }\n")
	if needFixedHelpers {
		glue.WriteString(formThreeFixedHelpers)
	}

	glue.WriteString("fn conformance_load(root: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&) -> i64 {\n")
	if !sabotageDispatch {
		for _, r := range fixedLoad {
			fmt.Fprintf(&glue, "    if root == %q {\n", r)
			fmt.Fprintf(&glue, "        var values: %s::%s[1] = [%s::%s { .. }];\n", ns, r, ns, r)
			glue.WriteString("        var rep = schema_rt::FixedReport { .. };\n")
			fmt.Fprintf(&glue, "        let n = %s::%sFixedLoad(values[0..1], data[0..data.len], rep);\n", ns, r)
			glue.WriteString("        if n < 0 || rep.damaged || rep.reason != 0 || rep.oor != 0 { return -1; }\n")
			if has(toLoaded, r) {
				fmt.Fprintf(&glue, "        return %s::%sToLoaded(values[0], conformance_unit(), data, vals);\n", ns, r)
			} else {
				glue.WriteString("        var body: u8[>..] = [];\n")
				fmt.Fprintf(&glue, "        %s::_%sFixedWrite(body, values[0]);\n", ns, r)
				fmt.Fprintf(&glue, "        return fixed_body_to_loaded(conformance_unit(), %q, body, data, vals);\n", r)
			}
			glue.WriteString("    }\n")
		}
	}
	glue.WriteString("    var report = schema_rt::LoadReport { .. };\n")
	glue.WriteString("    let at = schema_rt::load_table(conformance_unit(), root, data[0..data.len], vals, nodes, report);\n")
	glue.WriteString("    if at < 0 || report.body_damaged || report.refused { return -1; }\n")
	glue.WriteString("    return at;\n}\n")

	glue.WriteString("fn conformance_save(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], at: i64, out: u8[>..]&) -> i64 {\n")
	if !sabotageDispatch {
		for _, r := range fixedSave {
			fmt.Fprintf(&glue, "    if root == %q {\n", r)
			if has(fromLoaded, r) {
				fmt.Fprintf(&glue, "        var v = %s::%s { .. };\n", ns, r)
				fmt.Fprintf(&glue, "        if !%s::%sFromLoaded(v, conformance_unit(), data, vals, at) { return 1; }\n", ns, r)
			} else {
				glue.WriteString("        var body: u8[>..] = [];\n")
				fmt.Fprintf(&glue, "        if !fixed_body_from_loaded(conformance_unit(), %q, data, vals, at, body) { return 1; }\n", r)
				fmt.Fprintf(&glue, "        if body.len != %s::%sFixedBodyBytes { return 1; }\n", ns, r)
				fmt.Fprintf(&glue, "        var v = %s::%s { .. };\n", ns, r)
				fmt.Fprintf(&glue, "        %s::_%sFixedRead(body, 0, v);\n", ns, r)
			}
			glue.WriteString("        var tmp: u8[>..] = [];\n")
			fmt.Fprintf(&glue, "        if %s::%sFixedSave([v], tmp) < 0 { return 1; }\n", ns, r)
			glue.WriteString("        out.append(tmp);\n")
			glue.WriteString("        return 0;\n    }\n")
		}
	}
	glue.WriteString("    return -1;\n}\n")

	glue.WriteString("fn conformance_json_read(root: const u8[:], text: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&) -> i64 {\n")
	glue.WriteString("    if text.len == 0 && vals.len == 0 && data.len > 0 { return conformance_load(root, data, vals, nodes); }\n")
	for _, r := range toLoaded {
		fmt.Fprintf(&glue, "    if root == %q {\n", r)
		fmt.Fprintf(&glue, "        var v = %s::%s { .. }; var report = schema_rt::json_report(); let unit = conformance_unit();\n", ns, r)
		fmt.Fprintf(&glue, "        if !%s::%sFromJson(v, text, report) || report.damaged { return -2; }\n", ns, r)
		fmt.Fprintf(&glue, "        let at = %s::%sToLoaded(v, unit, data, vals);\n", ns, r)
		glue.WriteString("        return if at < 0 { -2 } else { at };\n    }\n")
	}
	glue.WriteString("    var report = schema_rt::json_report();\n")
	fmt.Fprintf(&glue, "    let at = schema_rt::json_load_table(%s::TableUnit, %s::TextUnit, root, text, data, vals, nodes, report);\n", ns, ns)
	glue.WriteString("    return if at < 0 || report.damaged { -2 } else { at };\n}\n")

	glue.WriteString("fn conformance_json_write(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64, out: u8[>..]&) -> i64 {\n")
	glue.WriteString("    if at < 0 { return conformance_save(root, data, vals, -1 - at, out); }\n")
	for _, r := range toLoaded {
		fmt.Fprintf(&glue, "    if root == %q {\n", r)
		fmt.Fprintf(&glue, "        var v = %s::%s { .. }; let unit = conformance_unit();\n", ns, r)
		fmt.Fprintf(&glue, "        if !%s::%sFromLoaded(v, unit, data, vals, at) { return -2; }\n", ns, r)
		fmt.Fprintf(&glue, "        let n = %s::%sToJson(v, out);\n", ns, r)
		glue.WriteString("        return if n < 0 { -2 } else { n };\n    }\n")
	}
	fmt.Fprintf(&glue, "    let n = schema_rt::json_save_table(%s::TableUnit, %s::TextUnit, root, data, vals, nodes, at, out);\n", ns, ns)
	glue.WriteString("    return if n < 0 { -2 } else { n };\n}\n")

	glue.WriteString("fn conformance_json_measure(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64) -> i64 {\n")
	for _, r := range toLoaded {
		fmt.Fprintf(&glue, "    if root == %q {\n", r)
		fmt.Fprintf(&glue, "        var v = %s::%s { .. }; let unit = conformance_unit();\n", ns, r)
		fmt.Fprintf(&glue, "        if !%s::%sFromLoaded(v, unit, data, vals, at) { return -2; }\n", ns, r)
		fmt.Fprintf(&glue, "        let n = %s::%sToJsonMeasure(v);\n", ns, r)
		glue.WriteString("        return if n < 0 { -2 } else { n };\n    }\n")
	}
	glue.WriteString("    var out: u8[>..] = [];\n")
	fmt.Fprintf(&glue, "    let n = schema_rt::json_save_table(%s::TableUnit, %s::TextUnit, root, data, vals, nodes, at, out);\n", ns, ns)
	glue.WriteString("    return if n < 0 { -2 } else { n };\n}\n")

	// text-wire entries. They delegate into conformance_load / conformance_save,
	// so omitting those fixed-root branches (sabotageDispatch) is a fault in
	// the same dispatch the driver emits, not in a string it prints.
	glue.WriteString("fn conformance_load_into_vals(root: const u8[:], raw: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&) -> i64 {\n")
	glue.WriteString("    if data.len != 0 || vals.len != 0 { return -1; }\n")
	glue.WriteString("    data.append(raw);\n")
	glue.WriteString("    return conformance_load(root, data, vals, nodes);\n")
	glue.WriteString("}\n")
	glue.WriteString("fn conformance_save_from_vals(root: const u8[:], data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, at: i64, out: u8[>..<]&) -> i64 {\n")
	glue.WriteString("    var tmp: u8[>..] = [];\n")
	glue.WriteString("    let dispatched = conformance_save(root, data, vals, at, tmp);\n")
	glue.WriteString("    if dispatched == 0 { out.append(tmp); return 0; }\n")
	glue.WriteString("    if dispatched > 0 { return dispatched; }\n")
	glue.WriteString("    return schema_rt::save_table(conformance_unit(), root, data, vals, nodes, at, out);\n")
	glue.WriteString("}\n")
	glue.WriteString("fn conformance_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&) {}\n")

	files["conformance_unit.goose"] = []byte(glue.String())
	main, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "main.goose"))
	if err != nil {
		t.Fatal(err)
	}
	files["main.goose"] = main
	text, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "units", key+".goose"))
	if err != nil {
		text, err = os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "unit.goose"))
		if err != nil {
			t.Fatal(err)
		}
	}
	files["unit.goose"] = text
	return files
}

func dedupe(in []string) []string {
	set := map[string]bool{}
	var out []string
	for _, s := range in {
		if !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func runGooseDriver(t *testing.T, dir string, inputs map[string][]byte, allowDriverExit bool, argv ...string) []byte {
	t.Helper()
	if !allowDriverExit {
		return gooserun.RunArgs(t, dir, "main.goose", inputs, argv...)
	}
	gooseBin, why := gooserun.Compiler()
	if gooseBin == "" {
		t.Fatalf("gooserun: the Goose compiler does not resolve: %s", why)
	}
	for name, data := range inputs {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	line := append([]string{"run", "main.goose", "--"}, argv...)
	cmd := exec.Command(gooseBin, line...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil && !strings.Contains(stderr.String(), "driver: ") {
		t.Fatalf("gooserun: goose run main.goose: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes()
}

func runFormThreeSurfaces(t *testing.T, c *formThreeCorpus, override map[string][]byte, sabotageDispatch bool) map[string]map[string]string {
	t.Helper()
	got := map[string]map[string]string{}
	for _, s := range []string{"json-read", "json-write"} {
		got[s] = map[string]string{}
	}
	var mu sync.Mutex
	t.Run("units", func(t *testing.T) {
		for _, key := range c.order {
			var mine []string
			for _, s := range []string{"json-read", "json-write"} {
				for _, u := range c.unitOf[s] {
					if u == key {
						mine = append(mine, s)
						break
					}
				}
			}
			if len(mine) == 0 {
				continue
			}
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				// main.goose reads each json-read row's text from the repo-relative
				// path testdata/conformance/tables/json/<case>.json, so the unit's
				// directory must see the corpus there
				if err := os.Symlink(filepath.Join(c.abs, "testdata"), filepath.Join(dir, "testdata")); err != nil {
					t.Fatal(err)
				}
				files := formThreeUnitFiles(t, key, c.units[key], override, sabotageDispatch)
				files["manifest.txt"] = []byte(c.manifest)
				for _, s := range mine {
					out := runGooseDriver(t, dir, files, sabotageDispatch && c.unitFixed[key], s, "manifest.txt", key)
					mu.Lock()
					for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
						name, h, ok := strings.Cut(line, " ")
						if !ok {
							continue
						}
						name = strings.TrimSuffix(name, ".json")
						if rest, absent := strings.CutPrefix(h, "absent"); absent {
							if _, exists := got[s][name]; !exists {
								got[s][name] = "absent " + strings.TrimPrefix(rest, " ")
							}
							continue
						}
						b, err := hex.DecodeString(h)
						if err != nil {
							t.Errorf("%s %s: %q is not hex", s, name, h)
						}
						got[s][name] = string(b)
					}
					mu.Unlock()
				}
			})
		}
	})
	return got
}

func formThreeFailing(c *formThreeCorpus, got map[string]map[string]string) []string {
	var out []string
	for _, s := range []string{"json-read", "json-write"} {
		for name, want := range c.want[s] {
			if got[s][name] != want {
				out = append(out, s+"/"+name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestGooseDriverFormThreeDispatch is the gate: every fixed root (form 3)
// answers its json-read and json-write rows through the form-3 dispatch, byte
// for byte against the corpus, and every variable root answers through the flat
// value table.
func TestGooseDriverFormThreeDispatch(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the conformance driver)")
	c := readFormThreeCorpus(t)
	got := runFormThreeSurfaces(t, c, nil, false)
	if failing := formThreeFailing(c, got); len(failing) > 0 {
		t.Fatalf("form-3 dispatch left fixed or variable rows red:\n%s", strings.Join(failing, "\n"))
	}
}

// TestGooseDriverFormThreeDispatchNegativeControl plants the fault that was
// in the real artifact before this fix — the driver omitted the form-3 dispatch
// — and requires the fixed-root rows to turn red while the variable rows stay
// green.
func TestGooseDriverFormThreeDispatchNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the conformance driver)")
	c := readFormThreeCorpus(t)
	got := runFormThreeSurfaces(t, c, nil, true)
	for name, isFixed := range c.fixed {
		if !isFixed {
			continue
		}
		for _, s := range []string{"json-read", "json-write"} {
			if got[s][name] == c.want[s][name] {
				t.Errorf("%s: the fixed root %s stayed green without the form-3 dispatch", s, name)
			}
		}
	}
	for _, s := range []string{"json-read", "json-write"} {
		for name, want := range c.want[s] {
			if c.fixed[name] {
				continue
			}
			// a fixed root in the same unit aborts the unit's whole surface
			// (the pre-fix driver exits on the first fixed-root refusal), so a
			// variable row only pins the dispatch in a unit with no fixed root
			if c.unitFixed[c.unitOf[s][name]] {
				continue
			}
			if got[s][name] != want {
				t.Errorf("%s: the variable row %s went red without the form-3 dispatch", s, name)
			}
		}
	}
}
