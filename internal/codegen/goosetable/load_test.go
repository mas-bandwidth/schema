package goosetable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cleanReport is what a writer's own bytes owe a load: no event, a read
// (docs/SPEC-TABLES.md §4; test/conformance/harness/generate.go checks the
// same of the reference engine).
const cleanReport = "0,0,0,0,0,false,read"

// loadCase is one wire the probe loads: a report row or an instance row of
// testdata/conformance/tables/MANIFEST.txt.
type loadCase struct {
	name, unit, root, wire string
}

// loadManifest reads the unit lines and the report and instance rows.
func loadManifest(t *testing.T) (units map[string][]string, reports, instances []loadCase) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units = map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch {
		case fields[0] == "unit" && len(fields) >= 3:
			units[fields[1]] = fields[2:]
		case fields[0] == "report" && len(fields) >= 5:
			reports = append(reports, loadCase{fields[1], fields[2], fields[3], fields[4]})
		case fields[0] == "instance" && len(fields) >= 5:
			instances = append(instances, loadCase{fields[1], fields[2], fields[3], fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return units, reports, instances
}

// pinnedReports reads testdata/conformance/tables/reports.txt: case -> line.
func pinnedReports(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("reports.txt: %q is not a report line", line)
		}
		out[fields[0]] = fields[1]
	}
	return out
}

// generateUnit loads a manifest unit and generates both Goose surfaces, the
// load runtime among them; runtime overrides a runtime file's content.
func generateUnit(t *testing.T, paths []string, runtime map[string][]byte) (*ir.Unit, map[string][]byte, error) {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("%v does not load: %v", paths, err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		return u, nil, err
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		return u, nil, err
	}
	maps.Copy(files, tables)
	maps.Copy(files, runtime)
	return u, files, nil
}

// probeUnit is what every load probe shares: the import of every module of
// the unit, the globals it needs, and the statements that build the
// schema_rt::LoadUnit `unit` from the descriptors the table modules declare
// (opened inside the probe's main).
func probeUnit(t *testing.T, u *ir.Unit, files map[string][]byte) (head, build string) {
	t.Helper()
	ns := goose.Namespace(u)
	var mods, tables, unions []string
	for name, data := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
		if !strings.HasSuffix(name, "Table.goose") {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			rest, ok := strings.CutPrefix(line, "let ")
			if !ok {
				continue
			}
			if n, _, ok := strings.Cut(rest, "Descriptor: schema_rt::TableDesc "); ok && !strings.ContainsAny(n, " _") {
				tables = append(tables, n)
			}
			if n, _, ok := strings.Cut(rest, "Descriptor: schema_rt::UnionDesc "); ok && !strings.ContainsAny(n, " _") {
				unions = append(unions, n)
			}
		}
	}
	sort.Strings(mods)
	sort.Strings(tables)
	sort.Strings(unions)
	var h, b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&h, "import .%s;\n", m)
	}
	h.WriteString("import os;\n\n")
	// the key enums of the enum-keyed arrays (docs/SPEC-TABLES.md §3.2): the
	// field descriptor does not name its key enum, so the unit the load
	// resolves against carries them
	described := map[string]bool{}
	for _, n := range tables {
		described[n] = true
	}
	type key struct {
		owner   string
		ordinal int
		global  string
	}
	var keys []key
	for _, f := range u.Files {
		var structs []*ir.Struct
		structs = append(structs, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				structs = append(structs, st)
			}
		}
		for _, st := range structs {
			if !described[st.Name] {
				continue
			}
			for i, fd := range st.Fields {
				if fd.KeyEnum == "" || fd.KeyEnumRef == nil {
					continue
				}
				dup := false
				for _, k := range keys {
					dup = dup || (k.owner == st.Name && k.ordinal == i)
				}
				if dup {
					continue
				}
				g := fmt.Sprintf("_LoadKeys%d", len(keys))
				en := fd.KeyEnumRef
				parts := make([]string, len(en.Variants))
				for vi := range en.Variants {
					parts[vi] = fmt.Sprintf("schema_rt::VariantDesc { name: %q, id: 0x%016x }", en.Variants[vi], ir.TableWireId(en.VariantWireName(vi)))
				}
				fmt.Fprintf(&h, "let %s: schema_rt::VariantDesc[%d] = [%s];\n", g, len(parts), strings.Join(parts, ", "))
				keys = append(keys, key{st.Name, i, g})
			}
		}
	}
	b.WriteString("    var tables: schema_rt::TableDesc[>..] = [];\n")
	for _, n := range tables {
		fmt.Fprintf(&b, "    tables.push(%s::%sDescriptor);\n", ns, n)
	}
	b.WriteString("    var unions: schema_rt::UnionDesc[>..] = [];\n")
	for _, n := range unions {
		fmt.Fprintf(&b, "    unions.push(%s::%sDescriptor);\n", ns, n)
	}
	b.WriteString("    var keys: schema_rt::KeyDesc[>..] = [];\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "    keys.push(schema_rt::KeyDesc { owner: %q, ordinal: %d, variants: %s });\n", k.owner, k.ordinal, k.global)
	}
	// the deprecated variants (docs/SPEC-TABLES.md §2.15): a VariantDesc does
	// not carry `| deprecated`, so the unit the load resolves against does
	// (docs/PORT-GOOSE.md, THE LOADED VALUE)
	b.WriteString("    var deprecated: schema_rt::DeprecatedVariant[>..] = [];\n")
	for _, f := range u.Files {
		for _, d := range f.Decls {
			en, ok := d.(*ir.Enum)
			if !ok {
				continue
			}
			for vi, dep := range en.VariantDeprecated {
				if dep {
					fmt.Fprintf(&b, "    deprecated.push(schema_rt::DeprecatedVariant { enum_name: %q, id: 0x%016x });\n", en.Name, ir.TableWireId(en.VariantWireName(vi)))
				}
			}
		}
	}
	b.WriteString("    let unit = schema_rt::LoadUnit { tables: tables, unions: unions, keys: keys, deprecated: deprecated };\n")
	return h.String(), b.String()
}

// loadProgram is the probe's root: it imports every module of the unit,
// builds the schema_rt::LoadUnit from the descriptors the table modules
// declare, and for each case loads the wire file and prints the §4 report
// as `<case> u,k,w,c,d,m,verdict` — test/conformance/README.md's report line.
func loadProgram(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) string {
	t.Helper()
	head, build := probeUnit(t, u, files)
	var b strings.Builder
	b.WriteString(head)
	b.WriteString(`fn run(unit: schema_rt::LoadUnit, name: const u8[:], root: const u8[:], file: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { print(name, " unreadable"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    schema_rt::load_table(unit, root, data, vals, nodes, rep);
    let m = if rep.malformed { "true" } else { "false" };
    let v = if rep.refused { "refused" } else { "read" };
    print(name, " ", rep.unknown, ",", rep.kind_mismatch, ",", rep.widened, ",", rep.clamped, ",", rep.duplicate, ",", m, ",", v);
}

`)
	b.WriteString("fn main() {\n")
	b.WriteString(build)
	for _, c := range cases {
		fmt.Fprintf(&b, "    run(unit, %q, %q, %q);\n", c.name, c.root, c.name+".bin")
	}
	b.WriteString("}\n")
	return b.String()
}

// runLoads generates one unit, loads every case under the pinned compiler and
// returns case -> report line.
func runLoads(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) map[string]string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name+".bin"] = data
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(loadProgram(t, u, files, cases)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	out := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("probe line %q is not a case and a report", line)
		}
		got[fields[0]] = fields[1]
	}
	return got
}

// dumpGoose is the Goose half of THE VALUE DUMP: the flat LoadVal table (and
// the LoadNode table it names) walked in field order, one line per value,
// `<path>=<state resolved value>`, printed exactly as referenceDump's Go
// walk prints the reference's tabletext.Instance (internal/tablewire.Decode). An absent
// value is resolved through its descriptor's declared default (docs/PORT-
// GOOSE.md, THE LOADED VALUE, THE THREE STATES), because that is the value the
// reference keeps; a table's or array's absent children take the type's zero,
// as the reference's reset does. A pointer node is labelled on first visit (the
// root is 0) and named by its label after, so a shared or cyclic graph prints
// finitely.
const dumpGoose = `
fn find_table(u: schema_rt::LoadUnit, name: const u8[:]) -> i64 {
    for t, i in u.tables { if t.name == name { return i; } }
    return -1;
}

fn f64_bits(x: f64) -> u64 {
    var a: f64[1] = [x];
    let b = bytes_of(a);
    var v: u64 = 0;
    for i in 8 {
        let c: u64 = b[i];
        v |= c << ((8 * i) as! u64);
    }
    return v;
}

fn add_hex(out: u8[>..]&, b: const u8[:], at: i64, n: i64) {
    let digits = "0123456789abcdef";
    for i in n {
        let c = b[at + i];
        out.push(digits[c >> 4]);
        out.push(digits[c & 15]);
    }
}

fn add_hex_u(out: u8[>..]&, v: u64, digits: i64) {
    let hex = "0123456789abcdef";
    for k in digits {
        let sh = ((digits - 1 - k) * 4) as! u64;
        out.push(hex[(v >> sh) & 15]);
    }
}

struct DumpCtx {
    unit: schema_rt::LoadUnit,
    vals: const schema_rt::LoadVal[:],
    nodes: const schema_rt::LoadNode[:],
    data: const u8[:],
    labels: i64[>..]&,
    next: i64&,
    root: i64,
    root_tab: i64,
    path: u8[65536],
    plen: i64,
}

fn path_push(c: DumpCtx&, b: u8) { c.path[c.plen] = b; c.plen += 1; }

fn path_bytes(c: DumpCtx&, s: const u8[:]) { for b in s { path_push(c, b); } }

fn path_int(c: DumpCtx&, n: i64) {
    var x = n;
    if x < 0 { path_push(c, 45); x = -x; }
    if x == 0 { path_push(c, 48); return; }
    let first = c.plen;
    while x > 0 {
        path_push(c, (48 + (x % 10)) as u8);
        x = x / 10;
    }
    var a = first;
    var b = c.plen - 1;
    while a < b {
        let t = c.path[a];
        c.path[a] = c.path[b];
        c.path[b] = t;
        a += 1;
        b -= 1;
    }
}

fn head_of(c: DumpCtx&, d: schema_rt::FieldDesc, v: schema_rt::LoadVal) -> u8[>..] {
    var head: u8[>..] = [];
    for i in c.plen { head.push(c.path[i]); }
    head.push(61);
    if d.optional { head.push(if v.state == schema_rt::ValAbsent { 45 } else { 43 }); }
    return head;
}

recursive fn dump_record(c: DumpCtx&, tab: i64, v: schema_rt::LoadVal, pre: const u8[:]) {
    if tab < 0 {
        var head: u8[>..] = [];
        for i in c.plen { head.push(c.path[i]); }
        format(head, "=", pre, "tab#0");
        print(head);
        return;
    }
    let st = c.unit.tables[tab];
    {
        var head: u8[>..] = [];
        for i in c.plen { head.push(c.path[i]); }
        format(head, "=", pre, "tab#", st.fields.len);
        print(head);
    }
    for f, i in st.fields {
        let save = c.plen;
        path_push(c, 46);
        path_bytes(c, f.name);
        let slot = v.first + i;
        dump_value(c, f, c.vals[slot], true);
        c.plen = save;
    }
}

recursive fn dump_default_record(c: DumpCtx&, tab: i64, pre: const u8[:]) {
    if tab < 0 {
        var head: u8[>..] = [];
        for i in c.plen { head.push(c.path[i]); }
        format(head, "=", pre, "tab#0");
        print(head);
        return;
    }
    let st = c.unit.tables[tab];
    {
        var head: u8[>..] = [];
        for i in c.plen { head.push(c.path[i]); }
        format(head, "=", pre, "tab#", st.fields.len);
        print(head);
    }
    for f, i in st.fields {
        let save = c.plen;
        path_push(c, 46);
        path_bytes(c, f.name);
        var z = schema_rt::LoadVal { state: schema_rt::ValAbsent, shape: f.shape, .. };
        dump_value(c, f, z, true);
        c.plen = save;
    }
}

recursive fn dump_value(c: DumpCtx&, d: schema_rt::FieldDesc, v: schema_rt::LoadVal, use_default: bool) {
    let absent = v.state == schema_rt::ValAbsent;

    if d.shape == schema_rt::ShapeTab {
        let t = find_table(c.unit, d.ref);
        var pre: const u8[:] = "";
        if d.optional { pre = if v.state == schema_rt::ValAbsent { "-" } else { "+" }; }
        if v.state == schema_rt::ValPresent && v.made {
            dump_record(c, if v.tab >= 0 { v.tab } else { t }, v, pre);
        } else {
            dump_default_record(c, t, pre);
        }
        return;
    }

    if d.shape == schema_rt::ShapeArr {
        var n: i64 = d.bound;
        if v.state == schema_rt::ValPresent {
            if d.array == schema_rt::ArrayList { n = v.len; } else { n = v.count; }
        }
        {
            var head = head_of(c, d, v);
            format(head, "arr#", n);
            print(head);
        }
        let e = d.elem;
        guard e else { return; }
        var ekd = e;
        ekd.kind = d.ek;
        for i in n {
            let save = c.plen;
            path_push(c, 91);
            path_int(c, i);
            path_push(c, 93);
            if v.state == schema_rt::ValPresent {
                let es = v.first + i;
                let ev = c.vals[es];
                if e.shape == schema_rt::ShapeTab {
                    if ev.state == schema_rt::ValPresent && ev.made {
                        dump_record(c, if ev.tab >= 0 { ev.tab } else { find_table(c.unit, e.ref) }, ev, "");
                    } else {
                        dump_default_record(c, find_table(c.unit, e.ref), "");
                    }
                } else {
                    dump_value(c, ekd, ev, false);
                }
            } else if e.shape == schema_rt::ShapeTab {
                dump_default_record(c, find_table(c.unit, e.ref), "");
            } else {
                var z = schema_rt::LoadVal { state: schema_rt::ValAbsent, shape: e.shape, .. };
                dump_value(c, ekd, z, false);
            }
            c.plen = save;
        }
        return;
    }

    if d.shape == schema_rt::ShapeMap {
        var n: i64 = 0;
        if v.state == schema_rt::ValPresent { n = v.len; }
        {
            var head = head_of(c, d, v);
            format(head, "map#", n);
            print(head);
        }
        for i in n {
            let save = c.plen;
            path_push(c, 91);
            path_int(c, i);
            path_push(c, 93);
            let es = v.first + i;
            let ev = c.vals[es];
            dump_record(c, ev.tab, ev, "");
            c.plen = save;
        }
        return;
    }

    if d.shape == schema_rt::ShapeStr || d.shape == schema_rt::ShapeText || d.shape == schema_rt::ShapeBytes {
        var head = head_of(c, d, v);
        if d.shape == schema_rt::ShapeStr && d.wide {
            format(head, "u");
            if v.state == schema_rt::ValPresent {
                let units = v.len / 2;
                for i in units {
                    if i > 0 { head.push(44); }
                    format(head, schema_rt::unit_at(c.data, v.off + 2 * i));
                }
            }
            print(head);
            return;
        }
        format(head, "s");
        if absent && use_default && d.has_def {
            add_hex(head, d.def_s, 0, d.def_s.len);
        } else if v.state == schema_rt::ValPresent {
            add_hex(head, c.data, v.off, v.len);
        }
        print(head);
        return;
    }

    if d.shape == schema_rt::ShapeBool {
        var head = head_of(c, d, v);
        var b = v.bits.lo != 0;
        if absent && use_default && d.has_def { b = d.def_b; }
        format(head, if b { "b1" } else { "b0" });
        print(head);
        return;
    }

    if d.shape == schema_rt::ShapeInt {
        var head = head_of(c, d, v);
        var bits = v.bits;
        if absent && use_default && d.has_def { bits = d.def_i; }
        if schema_rt::kind_wide(d.kind) {
            format(head, "w", bits.hi as! i64, ":", bits.lo as! i64);
        } else {
            format(head, "i", bits.lo as! i64);
        }
        print(head);
        return;
    }

    if d.shape == schema_rt::ShapeFloat {
        var head = head_of(c, d, v);
        var bits: u64 = v.bits.lo;
        if d.kind == schema_rt::KindF32 {
            if absent && use_default && d.has_def { bits = schema_rt::f32_bits(d.def_f as f32) as! u64; }
            let b32 = bits as! u32;
            if (b32 & 0x7F800000) == 0x7F800000 && (b32 & 0x007FFFFF) != 0 {
                format(head, "n");
                add_hex_u(head, schema_rt::widen_f32(b32), 16);
            } else {
                format(head, "g");
                add_hex_u(head, bits, 8);
            }
        } else {
            if absent && use_default && d.has_def { bits = f64_bits(d.def_f); }
            if (bits & 0x7FF0000000000000) == 0x7FF0000000000000 && (bits & 0x000FFFFFFFFFFFFF) != 0 {
                format(head, "n");
            } else {
                format(head, "d");
            }
            add_hex_u(head, bits, 16);
        }
        print(head);
        return;
    }

    if d.shape == schema_rt::ShapeEnum {
        var head = head_of(c, d, v);
        var e: u64 = v.bits.lo;
        if absent && use_default && d.has_def { e = d.def_i.lo; }
        format(head, "e", e as! i64);
        print(head);
        return;
    }

    if d.shape == schema_rt::ShapeSBlob || d.shape == schema_rt::ShapeBBlob {
        var head = head_of(c, d, v);
        if v.state == schema_rt::ValPresent && v.bits.lo != 0 {
            let k = (v.bits.lo as! i64) - 2;
            if k >= 0 && k < c.nodes.len {
                let nd = c.nodes[k];
                format(head, "x");
                add_hex(head, c.data, nd.off, nd.len);
            } else {
                format(head, "null");
            }
        } else {
            format(head, "null");
        }
        print(head);
        return;
    }

    if d.shape == schema_rt::ShapePtr {
        if v.state != schema_rt::ValPresent || v.bits.lo == 0 {
            var head = head_of(c, d, v);
            format(head, "null");
            print(head);
            return;
        }
        let index = v.bits.lo as! i64;
        if index == 1 {
            let lab = c.labels[1];
            if lab >= 0 {
                var head = head_of(c, d, v);
                format(head, "^", lab);
                print(head);
                return;
            }
        }
        let k = index - 2;
        if k < 0 || k >= c.nodes.len {
            var head = head_of(c, d, v);
            format(head, "null");
            print(head);
            return;
        }
        let nd = c.nodes[k];
        if nd.tab < 0 || nd.val < 0 {
            var head = head_of(c, d, v);
            format(head, "null");
            print(head);
            return;
        }
        let lab = c.labels[index];
        if lab >= 0 {
            var head = head_of(c, d, v);
            format(head, "^", lab);
            print(head);
            return;
        }
        let nl = c.next;
        c.next = nl + 1;
        c.labels[index] = nl;
        {
            var head = head_of(c, d, v);
            format(head, "@", nl);
            print(head);
        }
        dump_record(c, nd.tab, c.vals[nd.val], "");
        return;
    }

    if d.shape == schema_rt::ShapeUnion {
        var tag: i64 = 0;
        if v.state == schema_rt::ValPresent { tag = v.bits.lo as! i64; }
        {
            var head = head_of(c, d, v);
            format(head, "t", tag);
            print(head);
        }
        if tag > 0 && v.made && v.count > 0 {
            let u = v.tab;
            if u >= 0 && u < c.unit.unions.len {
                let arms = c.unit.unions[u].arms;
                if tag <= arms.len {
                    let a = arms[tag - 1];
                    if a.shape != schema_rt::ShapeVoid {
                        let save = c.plen;
                        path_bytes(c, ".arm");
                        dump_value(c, a, c.vals[v.first], false);
                        c.plen = save;
                    }
                }
            }
        }
        return;
    }

    var head = head_of(c, d, v);
    format(head, "?");
    print(head);
}

fn dump_instance(unit: schema_rt::LoadUnit, name: const u8[:], root: i64, vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], data: const u8[:]) {
    if root < 0 { return; }
    var labels: i64[>..] = [];
    for i in nodes.len + 2 { labels.push(-1); }
    labels[1] = 0;
    var next: i64 = 1;
    var c = DumpCtx { unit: unit, vals: vals, nodes: nodes, data: data, labels: labels, next: next, root: root, root_tab: vals[root].tab, plen: 0, .. };
    path_bytes(c, name);
    dump_record(c, vals[root].tab, vals[root], "");
}

fn dump_run(unit: schema_rt::LoadUnit, name: const u8[:], root: const u8[:], file: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { print("CASE ", name, " unreadable"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let r = schema_rt::load_table(unit, root, data, vals, nodes, rep);
    let m = if rep.malformed { "true" } else { "false" };
    let v = if rep.refused { "refused" } else { "read" };
    print("CASE ", name, " ", rep.unknown, ",", rep.kind_mismatch, ",", rep.widened, ",", rep.clamped, ",", rep.duplicate, ",", m, ",", v);
    dump_instance(unit, root, r, vals[..], nodes[..], data[..]);
}
`

// dumpProgram is the probe's root for THE VALUE DUMP: it builds the unit as
// loadProgram does and prints each case's §4 report line, prefixed by CASE and
// the case name, followed by the case's value dump lines.
func dumpProgram(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) string {
	t.Helper()
	head, build := probeUnit(t, u, files)
	var b strings.Builder
	b.WriteString(head)
	b.WriteString(dumpGoose)
	b.WriteString("fn main() {\n")
	b.WriteString(build)
	for _, c := range cases {
		fmt.Fprintf(&b, "    dump_run(unit, %q, %q, %q);\n", c.name, c.root, c.name+".bin")
	}
	b.WriteString("}\n")
	return b.String()
}

// runLoadsAndDumps generates one unit, loads every case under the pinned
// compiler, and returns case -> §4 report line and case -> THE VALUE DUMP
// lines.
func runLoadsAndDumps(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) (map[string]string, map[string][]string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name+".bin"] = data
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(dumpProgram(t, u, files, cases)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n")
	reports := map[string]string{}
	dumps := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "CASE "); ok {
			fields := strings.Fields(rest)
			if len(fields) != 2 {
				t.Fatalf("probe line %q is not CASE <name> <report>", line)
			}
			cur = fields[0]
			reports[cur] = fields[1]
			continue
		}
		if cur == "" {
			t.Fatalf("probe line %q does not follow a CASE line", line)
		}
		dumps[cur] = append(dumps[cur], line)
	}
	return reports, dumps
}

// goDump is the Go half of THE VALUE DUMP (dumpGoose): the reference engine's
// decoded Instance, walked in field order and printed one line per value.
type goDump struct {
	m      *tabletext.Model
	lines  []string
	labels map[*tabletext.Instance]int
	next   int
}

func (d *goDump) line(path, token string) { d.lines = append(d.lines, path+"="+token) }

func (d *goDump) rec(inst *tabletext.Instance, path, pre string) {
	if inst == nil {
		d.line(path, pre+"tab#0")
		return
	}
	d.line(path, fmt.Sprintf("%stab#%d", pre, len(inst.Fields)))
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		d.slot(fv, fv.Def, path+"."+fv.Def.Name)
	}
}

func (d *goDump) defRec(st *ir.Struct, path string) { d.rec(d.m.New(st), path, "") }

func (d *goDump) slot(fv *tabletext.Field, f *ir.Field, path string) {
	pre := ""
	if f.Type.Optional {
		if fv.Present {
			pre = "+"
		} else {
			pre = "-"
		}
	}
	switch {
	case f.IsMap():
		d.line(path, fmt.Sprintf("%smap#%d", pre, len(fv.Entries)))
		for i := range fv.Entries {
			d.rec(fv.Entries[i].Tab, fmt.Sprintf("%s[%d]", path, i), "")
		}
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		d.line(path, pre+"s"+hex.EncodeToString(fv.Cell.Str))
	case f.Array != ir.ArrayNone:
		d.line(path, fmt.Sprintf("%sarr#%d", pre, len(fv.Elems)))
		e := *f
		e.Array = ir.ArrayNone
		e.KeyEnum = ""
		for i := range fv.Elems {
			d.value(&fv.Elems[i], &e, fmt.Sprintf("%s[%d]", path, i), "")
		}
	default:
		d.value(&fv.Cell, f, path, pre)
	}
}

func (d *goDump) value(c *tabletext.Cell, f *ir.Field, path, pre string) {
	t := f.Type
	switch {
	case t.Pointer && t.Blob():
		if c.Blob == nil {
			d.line(path, pre+"null")
			return
		}
		d.line(path, pre+"x"+hex.EncodeToString(c.Blob.Data))
		return
	case t.Pointer:
		if c.Node == nil {
			d.line(path, pre+"null")
			return
		}
		if n, ok := d.labels[c.Node]; ok {
			d.line(path, pre+fmt.Sprintf("^%d", n))
			return
		}
		n := d.next
		d.next++
		d.labels[c.Node] = n
		d.line(path, pre+fmt.Sprintf("@%d", n))
		d.rec(c.Node, path, "")
		return
	case t.Kind == ir.TBool:
		d.line(path, pre+map[bool]string{true: "b1", false: "b0"}[c.B])
		return
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		d.line(path, pre+goFloat(c.F, t.Kind == ir.TFloat32))
		return
	case t.Kind == ir.TWString:
		us := make([]string, len(c.Units))
		for i, u := range c.Units {
			us[i] = fmt.Sprint(u)
		}
		d.line(path, pre+"u"+strings.Join(us, ","))
		return
	case t.Kind == ir.TString || t.Kind == ir.TBytes:
		d.line(path, pre+"s"+hex.EncodeToString(c.Str))
		return
	}
	if un := tabletext.UnionOf(f); un != nil {
		d.line(path, pre+fmt.Sprintf("t%d", c.U))
		if c.U >= 1 {
			if c.Tab != nil {
				d.rec(c.Tab, path+".arm", "")
			} else if c.Arm != nil {
				af := *c.Arm.Def
				d.slot(c.Arm, &af, path+".arm")
			}
		}
		return
	}
	if tabletext.EnumOf(f) != nil {
		d.line(path, pre+fmt.Sprintf("e%d", int64(c.U)))
		return
	}
	if tabletext.StructOf(f) != nil {
		if c.Tab == nil {
			d.line(path, pre+"tab#0")
			return
		}
		d.rec(c.Tab, path, pre)
		return
	}
	if ir.TableKindWide(ir.TableScalarKind(f)) {
		d.line(path, pre+goWide(c.Wide))
		return
	}
	d.line(path, pre+fmt.Sprintf("i%d", int64(c.U)))
}

func goFloat(x float64, f32 bool) string {
	if math.IsNaN(x) {
		return fmt.Sprintf("n%016x", math.Float64bits(x))
	}
	if f32 {
		return fmt.Sprintf("g%08x", math.Float32bits(float32(x)))
	}
	return fmt.Sprintf("d%016x", math.Float64bits(x))
}

func goWide(v *big.Int) string {
	if v == nil {
		v = new(big.Int)
	}
	two128 := new(big.Int).Lsh(big.NewInt(1), 128)
	x := new(big.Int).Mod(v, two128)
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	lo := new(big.Int).And(x, mask).Uint64()
	hi := new(big.Int).Rsh(x, 64).Uint64()
	return fmt.Sprintf("w%d:%d", int64(hi), int64(lo))
}

// referenceDump decodes one wire with the Go reference engine and returns its
// value dump, or nil when the reference refuses or cannot finish the read.
func referenceDump(t *testing.T, u *ir.Unit, rootName string, wire []byte) []string {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	if err != nil || !ok {
		return nil
	}
	d := &goDump{m: m, labels: map[*tabletext.Instance]int{inst: 0}, next: 1}
	d.rec(inst, rootName, "")
	return d.lines
}

// firstDivergence names the first line two dumps differ at.
func firstDivergence(got, want []string) (int, string, string) {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return i, got[i], want[i]
		}
	}
	if len(got) != len(want) {
		if len(got) > n {
			return n, got[n], "<end>"
		}
		return n, "<end>", want[n]
	}
	return -1, "", ""
}

func dumpPath(line string) string {
	if i := strings.IndexByte(line, '='); i >= 0 {
		return line[:i]
	}
	return line
}

// TestGooseTableLoadMatchesTheReports loads every report row of
// testdata/conformance/tables/MANIFEST.txt through the emitted load runtime
// (runtime/load.goose) and compares the §4 report, u,k,w,c,d,m,verdict, with
// reports.txt row for row: unknown ids skipped and counted, widening on read,
// clamps, duplicates, damage and the form refusal (docs/SPEC-TABLES.md §3,
// §4). Every instance row — a writer's own bytes — must load clean. A unit
// the Goose surfaces refuse is named with its row count.
func TestGooseTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	units, reports, instances := loadManifest(t)
	pinned := pinnedReports(t)
	byUnit := map[string][]loadCase{}
	var order []string
	for _, c := range append(append([]loadCase{}, reports...), instances...) {
		if _, ok := units[c.unit]; !ok {
			t.Fatalf("%s names unit %q, which the manifest does not declare", c.name, c.unit)
		}
		if _, ok := byUnit[c.unit]; !ok {
			order = append(order, c.unit)
		}
		byUnit[c.unit] = append(byUnit[c.unit], c)
	}
	isReport := map[string]bool{}
	for _, r := range reports {
		isReport[r.name] = true
		if _, ok := pinned[r.name]; !ok {
			t.Fatalf("%s: reports.txt pins no such case", r.name)
		}
	}
	compared, cleanLoads, refusedRows := 0, 0, 0
	valuesCompared, instancesCompared := 0, 0
	var refusedUnits []string
	for _, key := range order {
		u, files, err := generateUnit(t, units[key], nil)
		if err != nil {
			refusedRows += len(byUnit[key])
			refusedUnits = append(refusedUnits, fmt.Sprintf("%s (%d rows): %v", key, len(byUnit[key]), err))
			continue
		}
		got, dumps := runLoadsAndDumps(t, u, files, byUnit[key])
		for _, c := range byUnit[key] {
			want := cleanReport
			if isReport[c.name] {
				want = pinned[c.name]
				compared++
			} else {
				cleanLoads++
			}
			if got[c.name] != want {
				t.Errorf("%s (unit %s, root %s): the Goose load reports %q, the reference %q", c.name, key, c.root, got[c.name], want)
			}
			if isReport[c.name] {
				continue
			}
			wire, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.wire)))
			if err != nil {
				t.Fatal(err)
			}
			wantDump := referenceDump(t, u, c.root, wire)
			if wantDump == nil {
				t.Errorf("%s (unit %s): the Go reference does not decode the instance", c.name, key)
				continue
			}
			if at, gline, wline := firstDivergence(dumps[c.name], wantDump); at >= 0 {
				field := dumpPath(gline)
				if field == "<end>" {
					field = dumpPath(wline)
				}
				t.Errorf("%s (unit %s, instance %s, field %s): the Goose value %q is not the reference's %q", c.name, key, c.name, field, gline, wline)
				continue
			}
			valuesCompared += len(wantDump)
			instancesCompared++
		}
	}
	for _, r := range refusedUnits {
		t.Logf("refused unit %s", r)
	}
	if compared+cleanLoads+refusedRows != len(reports)+len(instances) {
		t.Errorf("compared %d report rows and %d instances and refused %d rows, want %d", compared, cleanLoads, refusedRows, len(reports)+len(instances))
	}
	t.Logf("compared %d of %d report rows with reports.txt and loaded %d of %d instances clean; %d rows in %d refused units", compared, len(reports), cleanLoads, len(instances), refusedRows, len(refusedUnits))
	t.Logf("compared %d values across %d of %d instance rows with the Go reference's decode", valuesCompared, instancesCompared, len(instances))
}

// TestGooseTableLoadNegativeControl patches the emitted load runtime in
// memory — one anchor, matched exactly once — so a field read under a kind
// that grew no longer counts `widened` (docs/SPEC-TABLES.md §4). The same
// comparison over the unit's report rows must then go red on the row that
// pins the widening, and only there.
func TestGooseTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	units, reports, _ := loadManifest(t)
	pinned := pinnedReports(t)
	const unit = "tblscalars2"
	var cases []loadCase
	for _, r := range reports {
		if r.unit == unit {
			cases = append(cases, r)
		}
	}
	if len(cases) == 0 {
		t.Fatalf("the manifest has no report rows for %s", unit)
	}
	u, files, err := generateUnit(t, units[unit], nil)
	if err != nil {
		t.Fatalf("unit %s refused: %v", unit, err)
	}
	got := runLoads(t, u, files, cases)
	for _, c := range cases {
		if got[c.name] != pinned[c.name] {
			t.Fatalf("unpatched: %s reports %q, reports.txt %q", c.name, got[c.name], pinned[c.name])
		}
	}

	const anchor = "if !entry || i != 0 { c.report.widened += 1; }"
	rt := string(goosetable.Runtime()["load.goose"])
	if n := strings.Count(rt, anchor); n != 1 {
		t.Fatalf("the anchor appears %d times in runtime/load.goose, want exactly once", n)
	}
	patched := strings.Replace(rt, anchor, "if !entry || i != 0 { }", 1)
	u, files, err = generateUnit(t, units[unit], map[string][]byte{"load.goose": []byte(patched)})
	if err != nil {
		t.Fatal(err)
	}
	got = runLoads(t, u, files, cases)
	var red []string
	for _, c := range cases {
		if got[c.name] != pinned[c.name] {
			red = append(red, fmt.Sprintf("%s: %q against %q", c.name, got[c.name], pinned[c.name]))
		}
	}
	if len(red) != 1 || !strings.HasPrefix(red[0], "field_scalar_widen:") {
		t.Fatalf("the patched runtime went red on %v, want field_scalar_widen alone", red)
	}
	t.Logf("patched runtime: the comparison goes red on %s", red[0])
}

// runProbe writes the generated files and the probe's main.goose into a temp
// dir and runs it under the pinned compiler with the given input files.
func runProbe(t *testing.T, files map[string][]byte, program string, inputs map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n")
}

// sourceUnit checks one schema source from a temp file.
func sourceUnit(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("%s does not load: %v", name, err)
	}
	return u
}

// deprecatedShip is the reader's enum: Bomber retired in place (§2.15).
const deprecatedShip = `package dep

enum Ship
{
    Fighter
    Bomber | deprecated
    Scout
}

table Root
{
    a Ship = Scout
    b Ship
    c []Ship
    d [2]Ship
}
`

// TestGooseTableLoadDeprecatedVariantReadsTheDefault writes Bomber with a
// schema where it is live, then loads the bytes where it is `| deprecated`
// (docs/SPEC-TABLES.md §2.15, READ): a field reads its DECLARED default (a =
// Scout), a field with none reads None (b), and an element reads the
// ELEMENT's default, None (c[0], d[0]); a live variant beside it (Scout)
// keeps its value, and nothing counts unknown. Every value is held to the Go
// reference engine's decode of the same bytes (internal/tablewire enumCell),
// and the reversed witness — the same bytes under the live schema — reads
// Bomber everywhere, so the default is the deprecation's and not the wire's.
func TestGooseTableLoadDeprecatedVariantReadsTheDefault(t *testing.T) {
	t.Parallel()
	live := sourceUnit(t, "Dep.schema", strings.Replace(deprecatedShip, "Bomber | deprecated", "Bomber", 1))
	dep := sourceUnit(t, "Dep.schema", deprecatedShip)

	m := tabletext.NewModel(live)
	v := m.New(live.Tables["Root"])
	var rep tabletext.Report
	if !m.Read(v, []byte(`{"a":"Bomber","b":"Bomber","c":["Bomber","Scout"],"d":["Bomber","Scout"]}`), &rep) {
		t.Fatalf("the writer's instance does not read: %+v", rep)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}

	// the oracle: the Go reference engine's values, as ordinals (0 = None)
	oracle := func(u *ir.Unit) string {
		model := tabletext.NewModel(u)
		inst := model.New(u.Tables["Root"])
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, inst, wire, &report); err != nil || !ok || report.Unknown != 0 || report.Malformed {
			t.Fatalf("oracle decode: ok=%v err=%v report=%+v", ok, err, report)
		}
		var parts []string
		for _, key := range []string{"a", "b"} {
			f, _ := inst.FieldByKey(key)
			parts = append(parts, fmt.Sprintf("%s=%d", key, f.Cell.U))
		}
		for _, key := range []string{"c", "d"} {
			f, _ := inst.FieldByKey(key)
			for i, e := range f.Elems {
				parts = append(parts, fmt.Sprintf("%s%d=%d", key, i, e.U))
			}
		}
		return strings.Join(parts, " ") + " unknown=0"
	}

	gooseRead := func(u *ir.Unit) string {
		files, err := goose.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := goosetable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		head, build := probeUnit(t, u, files)
		var b strings.Builder
		b.WriteString(head)
		b.WriteString("fn main() {\n")
		b.WriteString(build)
		b.WriteString(`    var data: u8[>..] = [];
    if !read_file("dep.bin", data) { print("unreadable"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(unit, "Root", data, vals, nodes, rep);
    if root < 0 { print("not loaded"); return; }
    let f = vals[root].first;
    let c = vals[f + 2];
    let d = vals[f + 3];
    print("a=", vals[f].bits.lo, " b=", vals[f + 1].bits.lo, " c0=", vals[c.first].bits.lo, " c1=", vals[c.first + 1].bits.lo, " d0=", vals[d.first].bits.lo, " d1=", vals[d.first + 1].bits.lo, " unknown=", rep.unknown);
}
`)
		return runProbe(t, files, b.String(), map[string][]byte{"dep.bin": wire})
	}

	wantDep, wantLive := oracle(dep), oracle(live)
	if wantDep == wantLive {
		t.Fatalf("the oracle reads the deprecated and the live schema alike (%s): the witness proves nothing", wantDep)
	}
	if got := gooseRead(dep); got != wantDep {
		t.Errorf("deprecated: the Goose load reads %q, the reference %q", got, wantDep)
	} else {
		t.Logf("deprecated: %s (the reference agrees)", got)
	}
	if got := gooseRead(live); got != wantLive {
		t.Errorf("live (the reversed witness): the Goose load reads %q, the reference %q", got, wantLive)
	} else {
		t.Logf("live: %s (the reference agrees)", got)
	}
}

// TestGooseTableLoadDispatchTakesARegistration is THE DISPATCH's contract
// (docs/PORT-GOOSE.md, THE LOADED VALUE): a kind joins the walk by ONE
// REGISTRATION — a Loader variant and its load_registry row in
// schema_tables.goose — plus its case functions in a runtime file of its own,
// with load.goose byte for byte unchanged. The probe registers a Loader that
// takes over ShapeInt, delegates to the scalar read and counts one
// `duplicate` per value it placed: every report row of tblscalars2 must keep
// its other six columns, and the rows with integers must count.
func TestGooseTableLoadDispatchTakesARegistration(t *testing.T) {
	t.Parallel()
	units, reports, _ := loadManifest(t)
	const unit = "tblscalars2"
	var cases []loadCase
	for _, r := range reports {
		if r.unit == unit {
			cases = append(cases, r)
		}
	}
	if len(cases) == 0 {
		t.Fatalf("the manifest has no report rows for %s", unit)
	}
	u, files, err := generateUnit(t, units[unit], nil)
	if err != nil {
		t.Fatalf("unit %s refused: %v", unit, err)
	}
	before := runLoads(t, u, files, cases)

	rt := string(goosetable.Runtime()["schema_tables.goose"])
	const variant = "    Void,    // a payload-free union arm\n"
	const row = "LoadReg { shape: ShapeInt, array: 0, wide: false, loader: Loader.Scalar }"
	for _, anchor := range []string{variant, row} {
		if n := strings.Count(rt, anchor); n != 1 {
			t.Fatalf("the anchor %q appears %d times in runtime/schema_tables.goose, want exactly once", anchor, n)
		}
	}
	rt = strings.Replace(rt, variant, variant+"    Probe,\n", 1)
	rt = strings.Replace(rt, row, "LoadReg { shape: ShapeInt, array: 0, wide: false, loader: Loader.Probe }", 1)
	const probe = `namespace schema_rt;

import .schema_tables;
import .load;

fn load_field(k: Loader.Probe, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    c.report.duplicate += 1;
    return scalar_at(c, c.report, r, c.vals[slot], d, field_kind(d), field_kind(d));
}

fn load_item(k: Loader.Probe, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    c.report.duplicate += 1;
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek);
}

fn load_payload(k: Loader.Probe, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    c.report.duplicate += 1;
    let p = arm_value(c, a);
    return arm_placed(c, slot, tag, p, scalar_at(c, c.report, r, c.vals[p], a, a.kind, a.kind));
}
`
	_, patched, err := generateUnit(t, units[unit], map[string][]byte{"schema_tables.goose": []byte(rt), "zz_probe_kind.goose": []byte(probe)})
	if err != nil {
		t.Fatal(err)
	}
	if string(patched["load.goose"]) != string(goosetable.Runtime()["load.goose"]) {
		t.Fatal("the registration reached load.goose")
	}
	after := runLoads(t, u, patched, cases)
	counted := 0
	for _, c := range cases {
		b, a := strings.Split(before[c.name], ","), strings.Split(after[c.name], ",")
		if len(b) != 7 || len(a) != 7 {
			t.Fatalf("%s: report lines %q and %q", c.name, before[c.name], after[c.name])
		}
		for i := range b {
			if i != 4 && a[i] != b[i] {
				t.Errorf("%s: column %d moved from %s to %s under the registered kind", c.name, i, b[i], a[i])
			}
		}
		if a[4] != b[4] {
			counted++
		}
	}
	if counted == 0 {
		t.Fatalf("no row of %s reached the registered kind: the registry is not the dispatch", unit)
	}
	t.Logf("the registered kind took %d of %d rows of %s, load.goose unchanged", counted, len(cases), unit)
}
