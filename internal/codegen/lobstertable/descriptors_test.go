package lobstertable_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLobsterDescriptors holds the reflection surface to the IR and to the
// Go reference: the unit registry's six sets, every declaration's doc and
// tags, the deprecated enum variant flag, the enum-keyed array's key enum
// and variants, the map's entry table, and the deprecated/float-range/
// timestamp field flags (docs/SPEC-TABLES.md §8.1-§8.3, §2.8, §2.15;
// docs/PORT-LOBSTER.md, THE CARD MAP). It then loads one wire through
// runtime/load.lobster, saves it through runtime/schema_save.lobster and
// re-loads the saved bytes, holding the report, the saved bytes and the value
// dump to the Go engine's (THE TASK: load, save and re-load agree byte for
// byte and report for report).
//
// M13: the descriptor is a `let` global, so it is constant and published at
// load (docs/PORTING.md M13). J1: a fresh row read through the generated
// accessor and through the descriptor's defaults must agree (docs/PORTING.md
// J1).
func TestLobsterDescriptors(t *testing.T) {
	t.Parallel()
	p := loadDescriptorProbe(t)
	wire, saved, loadReport, loadDump, reloadReport, reloadDump := probeReference(t, p.u)
	out := runDescriptorsProbe(t, p, wire, nil)
	want := map[string]string{
		"package":       "descprobe",
		"types":         "1",
		"tables":        "1",
		"enums":         "2",
		"flags":         "0",
		"unions":        "0",
		"constants":     "0",
		"typedoc":       "A type no table reaches.",
		"typedtags":     "inspected",
		"tabledoc":      "The holder table.",
		"tabletags":     "inspected",
		"fielddoc":      "The mode field.",
		"fieldtags":     "annotated",
		"variants":      "2",
		"vartypecnt":    "1",
		"deprecated":    "1",
		"vardeprecated": "1",
		"keyed":         "2",
		"keyedvariants": "2",
		"keyedenum":     "Mode",
		"mapentry":      "1",
		"mapentryreg":   "HolderEntriesEntry",
		"mapentryfield": "HolderEntriesEntry",
		"depfield":      "1",
		"floatrange":    "1",
		"timestamp":     "1",
		"constdesc":     "1",
		"agreement":     "7",
		"loadreport":    loadReport,
		"saved":         saved,
		"reloadreport":  reloadReport,
		"loaddump":      loadDump,
		"reloaddump":    reloadDump,
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, out[k], v, out)
		}
	}
	t.Logf("types %s tables %s enums %s flags %s unions %s constants %s; deprecated variants %s; keyed keys %s (enum %s); map entries %s; deprecated fields %s; float ranges %s; timestamps %s;const descriptors %s; accessor/descriptor agreement %s; load/save/re-load: report %s bytes %s dump equal",
		out["types"], out["tables"], out["enums"], out["flags"], out["unions"], out["constants"],
		out["vardeprecated"], out["keyedvariants"], out["keyedenum"], out["mapentryreg"],
		out["depfield"], out["floatrange"], out["timestamp"], out["constdesc"], out["agreement"],
		out["loadreport"], out["saved"])
}

// TestLobsterDescriptorsNegativeControl plants one bug at a time in the
// emitted descriptor surface and requires the matching check to go red: the
// deprecated variant flag and marker, the map entry table (registry and field
// descriptor), the keyed array's key enum and variants, the deprecated field,
// the float range, the timestamp, M13's `let` descriptor and J1's accessor /
// descriptor agreement. Each is the flag the DESCRIPTOR GAPS and CORPUS GAP
// rulings name; a flag nobody can break is a flag the test does not pin.
func TestLobsterDescriptorsNegativeControl(t *testing.T) {
	t.Parallel()
	p := loadDescriptorProbe(t)
	wire, _, _, _, _, _ := probeReference(t, p.u)
	control := runDescriptorsProbe(t, p, wire, nil)
	type ctl struct {
		key      string
		sabotage func(map[string][]byte)
	}
	cases := map[string]ctl{
		"deprecated_variant_flag": {"vardeprecated", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `deprecated: true, doc: "The retired variant."`, `deprecated: false, doc: "The retired variant."`)
		}},
		"deprecated_variant_marker": {"deprecated", func(files map[string][]byte) {
			src := string(files["descriptors.lobster"])
			src = strings.ReplaceAll(src, "def deprecated_variant(enum_name:string, variant:string):\n    push(deprecated_names, DeprecatedName { enum_name: enum_name, variant: variant })",
				"def deprecated_variant(enum_name:string, variant:string):\n    return")
			files["descriptors.lobster"] = []byte(src)
		}},
		"map_entry_registry": {"mapentryreg", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `schema_rt.map_entry("Holder", "entries", "HolderEntriesEntry")`, `schema_rt.map_entry("Holder", "entries", "HolderEntriesEntryX")`)
		}},
		"keyed_variants": {"keyedvariants", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `schema_rt.keyed_keys("Holder", "nums"`, `schema_rt.keyed_keys("Holder", "numsx"`)
		}},
		"keyed_enum": {"keyedenum", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `schema_rt.keyed_enum("Holder", "nums", "Mode")`, `schema_rt.keyed_enum("Holder", "nums", "ModeX")`)
		}},
		"deprecated_field": {"depfield", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `deprecated: true, s: "int"`, `deprecated: false, s: "int"`)
		}},
		"float_range": {"floatrange", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `franged: true`, `franged: false`)
		}},
		"timestamp": {"timestamp", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `timestamp: true`, `timestamp: false`)
		}},
		"constant_descriptor": {"constdesc", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `let HolderDescriptor =`, `var HolderDescriptor =`)
		}},
		"accessor_descriptor": {"agreement", func(files map[string][]byte) {
			replaceIn(files, "ProbeTable.lobster", `Old: 7,`, `Old: 8,`)
		}},
	}
	var names []string
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tc := cases[name]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := runDescriptorsProbe(t, loadDescriptorProbe(t), wire, tc.sabotage)
			if got[tc.key] == control[tc.key] {
				t.Fatalf("%s did not turn %s red: control %q, planted %q", name, tc.key, control[tc.key], got[tc.key])
			}
		})
	}
}

func replaceIn(files map[string][]byte, name, old, new string) {
	src := string(files[name])
	if !strings.Contains(src, old) {
		panic(fmt.Sprintf("%s does not contain %q", name, old))
	}
	files[name] = []byte(strings.ReplaceAll(src, old, new))
}

// descriptorProbe is the probe unit's generated Lobster surface.
type descriptorProbe struct {
	u     *ir.Unit
	files map[string][]byte
	mods  []string
}

func loadDescriptorProbe(t *testing.T) *descriptorProbe {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{"testdata/descriptors/Probe.schema"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("probe unit does not load: %v", err)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatalf("type surface refuses the probe: %v", err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatalf("table surface refuses the probe: %v", err)
	}
	maps.Copy(files, tables)
	if _, ok := files["descriptors.lobster"]; !ok {
		t.Fatal("the descriptors runtime is not emitted")
	}
	p := &descriptorProbe{u: u, files: files}
	for name := range files {
		if strings.HasSuffix(name, "Table.lobster") {
			p.mods = append(p.mods, strings.TrimSuffix(name, ".lobster"))
		}
	}
	sort.Strings(p.mods)
	return p
}

// probeReference builds the probe wire in Go: the Holder row, with a map
// entry, both keyed slots and the non-deprecated Old.Fresh variant; then it
// rewrites the variant's wire id to Old.Gone, the DEPRECATED variant, so a
// reader must yield the declared default rather than Gone (docs/SPEC-TABLES.md
// §2.15). It returns the wire and the Go engine's answer to load, save and
// re-load.
func probeReference(t *testing.T, u *ir.Unit) (wire []byte, saved, loadReport, loadDump, reloadReport, reloadDump string) {
	t.Helper()
	m := tabletext.NewModel(u)
	holder := m.Lookup("Holder")
	inst := m.New(holder)
	field := func(name string) *tabletext.Field {
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == name {
				return &inst.Fields[i]
			}
		}
		t.Fatalf("Holder has no field %s", name)
		return nil
	}
	field("mode").Cell.U = 2 // Mode.Slow
	if len(field("nums").Elems) != 2 {
		t.Fatalf("nums has %d keyed slots, want 2", len(field("nums").Elems))
	}
	field("nums").Elems[0].I, field("nums").Elems[0].U = 11, 11
	field("nums").Elems[1].I, field("nums").Elems[1].U = 22, 22
	entries := field("entries")
	entry := m.NewMapEntry(entries.Def)
	entry.Fields[0].Cell.Str = []byte("k")
	entry.Fields[1].Cell.I, entry.Fields[1].Cell.U = 9, 9
	entries.Entries = []tabletext.Cell{{Tab: entry}}
	old := u.Enums["Old"]
	if old == nil {
		t.Fatal("the probe has no Old enum")
	}
	field("retired").Cell.U = uint64(tabletext.EnumValue(old, "Fresh"))
	var err error
	wire, err = tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the reference writer refuses the probe: %v", err)
	}
	fresh := ir.TableWireId(old.VariantWireNameOf("Fresh"))
	gone := ir.TableWireId(old.VariantWireNameOf("Gone"))
	from := binary.LittleEndian.AppendUint64(nil, fresh)
	wire = bytes.Replace(wire, from, binary.LittleEndian.AppendUint64(nil, gone), 1)
	if bytes.Contains(wire, from) {
		t.Fatal("the probe wire still names Fresh; the patch did not find the id table entry")
	}
	loadReport, loadDump = goLoad(u, "Holder", wire)
	savedBytes, err := goSave(&loadUnit{u: u}, "Holder", wire)
	if err != nil {
		t.Fatalf("the Go writer refuses the probe: %v", err)
	}
	saved = hex.EncodeToString(savedBytes)
	reloadReport, reloadDump = goLoad(u, "Holder", savedBytes)
	if loadReport != "0,0,0,0,0,false,read" || reloadReport != "0,0,0,0,0,false,read" {
		t.Fatalf("the reference does not read the probe cleanly: load %s, re-load %s", loadReport, reloadReport)
	}
	return wire, saved, loadReport, loadDump, reloadReport, reloadDump
}

func runDescriptorsProbe(t *testing.T, p *descriptorProbe, wire []byte, sabotage func(map[string][]byte)) map[string]string {
	t.Helper()
	files := map[string][]byte{}
	maps.Copy(files, p.files)
	if sabotage != nil {
		sabotage(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := probeProgram(p.u, p.mods)
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", map[string][]byte{"w0": wire})))
	out := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			out[k] = v
		}
	}
	// M13: the descriptor is a `let` global, so it is constant and published
	// at load; a `var` descriptor is the plain-mutable form the gate rejects.
	src := string(files["ProbeTable.lobster"])
	if strings.Contains(src, "let HolderDescriptor =") && !strings.Contains(src, "var HolderDescriptor =") {
		out["constdesc"] = "1"
	} else {
		out["constdesc"] = "0"
	}
	return out
}

func probeProgram(u *ir.Unit, mods []string) string {
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	fmt.Fprintf(&b, "import .load\nimport .schema_save\nnamespace %s\n", u.Package)
	b.WriteString(dumpLobster)
	b.WriteString(`def docof(name:string) -> string:
    let v = schema_rt.unit_view()
    for(v.types) t:
        if t.name == name:
            return t.doc
    for(v.tables) t:
        if t.name == name:
            return t.doc
    return ""
def tagsof(name:string) -> string:
    let v = schema_rt.unit_view()
    var out = ""
    for(v.types) t:
        if t.name == name:
            for(t.tags) x:
                if out != "": out += ","
                out += x
    for(v.tables) t:
        if t.name == name:
            for(t.tags) x:
                if out != "": out += ","
                out += x
    return out
def fdoc(owner:string, field:string) -> string:
    return schema_rt.field_doc(owner, field)
def ftags(owner:string, field:string) -> string:
    var out = ""
    for(schema_rt.field_tags(owner, field)) x:
        if out != "": out += ","
        out += x
    return out
def hasfield(owner:string, field:string, deprecated:bool, ranged:bool, timestamp:bool) -> bool:
    let t = schema_rt.lookup_table(owner)
    if not t:
        return false
    for(t.fields) f:
        if f.name == field:
            return (if deprecated: f.deprecated else: true) and (if ranged: f.franged else: true) and (if timestamp: f.timestamp else: true)
    return false
def probe(root:string, file:string) -> void:
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_tolerant(root, data, rep)
    print("loadreport " + report_line(rep))
    if not rec:
        print("saved -")
        print("reloadreport -")
        print("loaddump -")
        print("reloaddump -")
        return
    let saved = schema_rt.save(rec)
    print("saved " + hexs(saved))
    let rep2 = schema_rt.LoadReport {}
    let rec2 = schema_rt.load_tolerant(root, saved, rep2)
    print("reloadreport " + report_line(rep2))
    labels = []
    push(labels, rec)
    print("loaddump " + drec(rec))
    if not rec2:
        print("reloaddump -")
        return
    labels = []
    push(labels, rec2)
    print("reloaddump " + drec(rec2))
let v = schema_rt.unit_view()
print("package " + v.package)
print("types {length(v.types)}")
print("tables {length(v.tables)}")
print("enums {length(v.enums)}")
print("flags {length(v.flags)}")
print("unions {length(v.unions)}")
print("constants {length(v.constants)}")
print("typedoc " + docof("Unreached"))
print("typedtags " + tagsof("Unreached"))
print("tabledoc " + docof("Holder"))
print("tabletags " + tagsof("Holder"))
print("fielddoc " + fdoc("Holder", "mode"))
print("fieldtags " + ftags("Holder", "mode"))
for(v.enums) e:
    if e.name == "Mode":
        print("variants {length(e.variants)}")
for(v.types) t:
    if t.name == "Unreached":
        let d = t.d
        if d:
            print("vartypecnt {length(d.fields)}")
var vardep = 0
var keyedelem = ""
for(v.enums) e:
    if e.name == "Old":
        for(e.variants) x:
            if x.name == "Gone" and x.deprecated:
                vardep = 1
print("vardeprecated {vardep}")
print("deprecated " + if schema_rt.is_deprecated_variant("Old", "Gone"): "1" else: "0")
let holder = schema_rt.lookup_table("Holder")
if holder:
    for(holder.fields) f:
        if f.name == "nums":
            print("keyed {length(schema_rt.keyed_variants(f))}")
            print("keyedvariants {length(schema_rt.keyed_variants(f))}")
            print("keyedenum " + schema_rt.keyed_enum_of("Holder", "nums"))
            let e = f.e
            if e:
                keyedelem = e.t
print("keyedelem " + keyedelem)
print("mapentry " + if schema_rt.lookup_table("HolderEntriesEntry"): "1" else: "0")
print("mapentryreg " + schema_rt.map_entry_of("Holder", "entries"))
if holder:
    for(holder.fields) f:
        if f.name == "entries":
            print("mapentryfield " + f.t)
print("depfield " + if hasfield("Holder", "old", true, false, false): "1" else: "0")
print("floatrange " + if hasfield("Holder", "ratio", false, true, false): "1" else: "0")
print("timestamp " + if hasfield("Holder", "stamped", false, false, true): "1" else: "0")
// J1: a fresh row through the generated accessor and the descriptor's
// defaults, two derivations of one layout.
let fresh = NewHolder()
var agree = 0
if holder:
    if int(fresh.Mode) == holder.fields[0].def_int.lo: agree++
    if length(fresh.Nums) == holder.fields[1].bound: agree++
    if length(fresh.Entries) == 0: agree++
    if fresh.Retired == holder.fields[3].def_int.lo: agree++
    if fresh.Old == holder.fields[4].def_int.lo: agree++
    if fresh.Stamped == holder.fields[5].def_int.lo: agree++
    if schema_rt.f64bits(fresh.Ratio) == schema_rt.f64bits(holder.fields[6].def_float): agree++
print("agreement {agree}")
probe("Holder", "w0")
`)
	return b.String()
}
