package lobstertable_test

import (
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLobsterDescriptors holds the reflection surface to the IR: the unit
// registry's six sets, every declaration's doc and tags, the deprecated enum
// variant marker, the enum-keyed array's key variants and the map's entry
// table (docs/SPEC-TABLES.md §8.1-§8.3, §2.15; docs/PORT-LOBSTER.md, THE CARD
// MAP). The probe unit carries a deprecated field, a float range, a timestamp,
// a deprecated variant and a map, so each corpus gap has a live construct.
func TestLobsterDescriptors(t *testing.T) {
	t.Parallel()
	out := runDescriptorsProbe(t, nil)
	want := map[string]string{
		"package":    "descprobe",
		"types":      "1",
		"tables":     "1",
		"enums":      "2",
		"flags":      "0",
		"unions":     "0",
		"constants":  "0",
		"typedoc":    "A type no table reaches.",
		"typedtags":  "inspected",
		"tabledoc":   "The holder table.",
		"tabletags":  "inspected",
		"fielddoc":   "The mode field.",
		"fieldtags":  "annotated",
		"variants":   "2",
		"vartypecnt": "1",
		"deprecated": "1",
		"keyed":      "2",
		"mapentry":   "1",
		"depfield":   "1",
		"floatrange": "1",
		"timestamp":  "1",
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, out[k], v, out)
		}
	}
	t.Logf("types %s tables %s enums %s flags %s unions %s constants %s; docs/tags held; deprecated variants %s; keyed keys %s; map entries %s; deprecated fields %s; float ranges %s; timestamps %s",
		out["types"], out["tables"], out["enums"], out["flags"], out["unions"], out["constants"], out["deprecated"], out["keyed"], out["mapentry"], out["depfield"], out["floatrange"], out["timestamp"])
}

// TestLobsterDescriptorsNegativeControl drops the deprecated variant marker
// from the emitted runtime and holds the probe red: the marker is the one fact
// the descriptor gaps section names as wrongly loaded, so it is the control.
func TestLobsterDescriptorsNegativeControl(t *testing.T) {
	t.Parallel()
	sabotage := func(files map[string][]byte) {
		src := string(files["descriptors.lobster"])
		src = strings.ReplaceAll(src, "def deprecated_variant(enum_name:string, variant:string):\n    push(deprecated_names, DeprecatedName { enum_name: enum_name, variant: variant })",
			"def deprecated_variant(enum_name:string, variant:string):\n    return")
		files["descriptors.lobster"] = []byte(src)
	}
	out := runDescriptorsProbe(t, sabotage)
	if out["deprecated"] == "1" {
		t.Fatalf("the negative control did not turn the deprecated marker red: %v", out)
	}
}

func runDescriptorsProbe(t *testing.T, sabotage func(map[string][]byte)) map[string]string {
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
	if sabotage != nil {
		sabotage(files)
	}
	var mods []string
	for name := range files {
		if strings.HasSuffix(name, "Table.lobster") {
			mods = append(mods, strings.TrimSuffix(name, ".lobster"))
		}
	}
	sort.Strings(mods)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := probeProgram(u, mods)
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			out[k] = v
		}
	}
	return out
}

func probeProgram(u *ir.Unit, mods []string) string {
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	fmt.Fprintf(&b, "import .load\nnamespace %s\n", u.Package)
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
print("deprecated " + if schema_rt.is_deprecated_variant("Old", "Gone"): "1" else: "0")
let holder = schema_rt.lookup_table("Holder")
if holder:
    for(holder.fields) f:
        if f.name == "nums":
            print("keyed {length(schema_rt.keyed_variants(f))}")
print("mapentry " + if schema_rt.lookup_table("HolderEntriesEntry"): "1" else: "0")
print("depfield " + if hasfield("Holder", "old", true, false, false): "1" else: "0")
print("floatrange " + if hasfield("Holder", "ratio", false, true, false): "1" else: "0")
print("timestamp " + if hasfield("Holder", "when", false, false, true): "1" else: "0")
`)
	return b.String()
}
