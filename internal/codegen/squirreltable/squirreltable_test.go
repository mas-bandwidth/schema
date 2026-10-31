package squirreltable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSquirrelTableDescriptorsLoad generates every unit the tables manifest
// names (testdata/conformance/tables/MANIFEST.txt) into a t.TempDir, loads
// each unit's table module through the loader under the pinned interpreter —
// one sq process for the whole manifest — and constructs every table of the
// closure through its descriptor's make. Each construction is held to its
// descriptor (docs/SPEC-TABLES.md §8.1): every field descriptor names a member
// the instance has, a scalar member holds the descriptor's def (the declared
// default, SPEC §4.2), an array is born at its born length (SPEC §4.6), a
// nested table is an instance, and two constructions share no reference.
// The registry is held to the IR: its Roots are the declared tables and its
// Descriptors the closure, each descriptor's typeId the table's wire id
// (§3.1, §5) and each field's id the field's (§5).
//
// A unit the table surface refuses is listed with its refusal, and the
// refusal must name the construct and docs/PORT-SQUIRREL.md.
func TestSquirrelTableDescriptorsLoad(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if len(units) < 20 {
		t.Fatalf("the manifest names %d units; it names more than twenty", len(units))
	}
	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(probePrelude)
	want := map[string]string{}
	var refused []string
	constructed, held := 0, 0
	for _, unit := range units {
		u := loadUnit(t, root, unit.paths)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", unit.key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			if !strings.Contains(err.Error(), "docs/PORT-SQUIRREL.md") {
				t.Errorf("unit %s: a refusal that does not cite the port's register: %v", unit.key, err)
			}
			refused = append(refused, fmt.Sprintf("%s: %v", unit.key, err))
			continue
		}
		if len(tables) == 0 {
			continue // a manifest unit with no table declares nothing to construct
		}
		constructed++
		out := filepath.Join(dir, unit.key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mod := squirreltable.ModuleName(u)
		fmt.Fprintf(&p, "check(%q, dofile(%q, true).require(%q));\n", unit.key, unit.key+"/"+squirrel.RuntimePrefix+"module.nut", mod)

		closure := map[string]bool{}
		for name := range ir.TableClosure(u) {
			closure[name] = true
		}
		var roots []string
		for _, f := range u.Files {
			for _, st := range f.Tables {
				roots = append(roots, st.Name)
			}
		}
		want[unit.key+" roots"] = strings.Join(roots, ",")
		names := make([]string, 0, len(closure))
		for name := range closure {
			names = append(names, name)
		}
		sort.Strings(names)
		want[unit.key+" descriptors"] = strings.Join(names, ",")
		for _, name := range names {
			st := u.Tables[name]
			if st == nil {
				st = u.Structs[name]
			}
			want[unit.key+" "+name+" typeId"] = fmt.Sprintf("%016x", ir.TableWireId(st.WireName()))
			ids := make([]string, len(st.Fields))
			for i, f := range st.Fields {
				ids[i] = fmt.Sprintf("%s:%016x", f.Name, ir.TableFieldWireId(f))
			}
			want[unit.key+" "+name+" ids"] = strings.Join(ids, ",")
			want[unit.key+" "+name+" made"] = "ok"
			for _, f := range st.Fields {
				if v, ok := bornValue(f); ok {
					want[unit.key+" "+name+"."+f.Name] = v
					held++
				}
			}
		}
	}
	for _, r := range refused {
		t.Logf("refused %s", r)
	}
	if constructed < 20 {
		t.Errorf("only %d manifest units constructed; the surface maps all but the refused (%d)", constructed, len(refused))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q has no key", line)
		}
		if prev, dup := got[k]; dup && strings.HasSuffix(k, " made") {
			v = prev + "; " + v
		}
		got[k] = v
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if got[k] != want[k] {
			t.Errorf("%s = %q, want %q", k, got[k], want[k])
		}
	}
	t.Logf("%d units constructed, %d refused, %d facts held, %d of them a scalar member's born value", constructed, len(refused), len(want), held)
	if held < 100 {
		t.Errorf("only %d scalar members were held to the IR's defaults", held)
	}
}

// probePrelude is the probe's check: for one unit's table module, the registry
// and, per descriptor, the construction held to the descriptor. A failed
// check prints its reason in place of "ok".
const probePrelude = `local out = function(key, value) { print(key + "=" + value + "\n"); };
local sorted = function(a) { a.sort(); local s = ""; foreach (i, x in a) s += (i > 0 ? "," : "") + x; return s; };
local hex = function(v) { return format("%016x", v); };
local isblob = function(x) { return typeof x == "blob"; };
local born = function(f) { return f.list ? 0 : (f.counted ? f.min : f.bound); };
local show = function(x) {
  switch (typeof x) {
    case "integer": return x.tostring();
    case "float": return format("%.17g", x);
    case "bool": return x ? "true" : "false";
    case "null": return "null";
    case "string": { local s = "s:"; foreach (c in x) s += format("%02x", c & 0xff); return s; }
  }
  return typeof x;
};
local fieldOk = function(d, f, v, w) {
  if (!(f.member in v)) return "no member " + f.member;
  local x = v[f.member];
  local y = w[f.member];
  switch (f.shape) {
    case "arr":
      if (typeof x != "array") return f.member + " is " + typeof x;
      if (x == y) return f.member + " is shared";
      if (x.len() != born(f)) return f.member + " is born with " + x.len() + " elements, not " + born(f);
      foreach (e in x) if (f.elem.shape == "tab" && typeof e != "instance") return f.member + " element is " + typeof e;
      return null;
    case "tab": case "union":
      if (typeof x != "instance") return f.member + " is " + typeof x;
      if (x == y) return f.member + " is shared";
      return null;
    case "map":
      if (typeof x != "array") return f.member + " is " + typeof x;
      if (x == y) return f.member + " is shared";
      if (x.len() != 0) return f.member + " is born with " + x.len() + " entries, not 0";
      return null;
    case "wstr":
      // wide text is its code units, born empty and never shared (SPEC §4.12)
      if (typeof x != "array") return f.member + " is " + typeof x;
      if (x == y || x == f.def) return f.member + " is shared";
      if (x.len() != 0) return f.member + " is born with " + x.len() + " units, not 0";
      if (f.optional && v[f.member + "Present"] != false) return f.member + "Present is not false";
      return null;
    case "ptr": case "sblob": case "bblob":
      return x == null ? null : f.member + " is not null";
    case "bytes":
      if (!isblob(x)) return f.member + " is " + typeof x;
      if (x == y) return f.member + " is shared";
      if (x.len() != f.def.len()) return f.member + " holds " + x.len() + " bytes, def " + f.def.len();
      for (local i = 0; i < x.len(); i++) if ((x[i] & 0xff) != (f.def[i] & 0xff)) return f.member + " byte " + i;
      return null;
    case "int":
      if (f.width > 8) return x.len() == 2 ? null : f.member + " is not a pair";
  }
  if (x != f.def) return f.member + " = " + x + ", def " + f.def;
  if (f.optional && v[f.member + "Present"] != false) return f.member + "Present is not false";
  return null;
};
local check = function(unit, T) {
  out(unit + " roots", (function() { local s = ""; foreach (i, r in T.Roots) s += (i > 0 ? "," : "") + r; return s; })());
  local names = [];
  foreach (name, d in T.Descriptors) names.append(name);
  out(unit + " descriptors", sorted(names));
  foreach (name, d in T.Descriptors) {
    out(unit + " " + name + " typeId", hex(d.typeId));
    local ids = "";
    foreach (i, f in d.fields) ids += (i > 0 ? "," : "") + f.name + ":" + hex(f.id);
    out(unit + " " + name + " ids", ids);
    local v = d.make();
    local w = d.make();
    local why = null;
    if (typeof v != "instance") why = "make answers " + typeof v;
    foreach (f in d.fields) {
      switch (f.shape) {
        case "bool": case "float": case "enum": case "flags": case "str":
          out(unit + " " + name + "." + f.name, show(v[f.member]));
          break;
        case "int":
          if (f.width <= 8) out(unit + " " + name + "." + f.name, show(v[f.member]));
          break;
      }
      if (why != null) continue;
      why = fieldOk(d, f, v, w);
      if (why == null && d.byId[f.id] != f) why = f.name + " is not indexed by id";
    }
    out(unit + " " + name + " made", why == null ? "ok" : why);
  }
};
`

// bornValue is what a scalar member of table storage is born holding, from
// the IR alone (SPEC §4.2, §5; docs/SPEC-TABLES.md §5): the declared default
// or the zero; an enum is its variant's NAME (null for None); a string its
// bytes; a uint64 or a flags mask the int64 of its bits. Arrays, pointers,
// nested tables, unions and 128-bit values answer false: the descriptor check
// holds their shape.
func bornValue(f *ir.Field) (string, bool) {
	if f.Array != ir.ArrayNone || f.Type.Pointer || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString || f.IsMap() {
		return "", false
	}
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
				return "s:" + hex.EncodeToString([]byte(f.DefVariant)), true
			}
			return "null", true
		case *ir.Flags:
			// a flags mask, below
		default:
			return "", false
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		return strconv.FormatBool(f.HasDefault && f.DefBool), true
	case ir.TString:
		return "s:" + hex.EncodeToString(f.DefBytes), true
	case ir.TFloat32, ir.TFloat64:
		v := 0.0
		if f.HasDefault {
			v = f.DefFloat
		}
		if f.Type.Kind == ir.TFloat32 {
			v = float64(float32(v))
		}
		return fmt.Sprintf("%.17g", v), true
	}
	if f.Type.Width > 64 {
		return "", false
	}
	v := big.NewInt(0)
	if f.HasDefault && f.DefInt != nil {
		v = f.DefInt
	}
	if v.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return strconv.FormatInt(int64(v.Uint64()), 10), true
	}
	return v.String(), true
}

type manifestUnit struct {
	key   string
	paths []string
}

func manifestUnits(t *testing.T, path string) []manifestUnit {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var units []manifestUnit
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 3 && fields[0] == "unit" {
			units = append(units, manifestUnit{key: fields[1], paths: fields[2:]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return units
}

func loadUnit(t *testing.T, root string, rel []string) *ir.Unit {
	t.Helper()
	args := make([]string, len(rel))
	for i, r := range rel {
		args[i] = filepath.Join(root, filepath.FromSlash(r))
	}
	paths, err := compiler.GatherPaths(args)
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load %v: %v", rel, err)
	}
	return u
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
