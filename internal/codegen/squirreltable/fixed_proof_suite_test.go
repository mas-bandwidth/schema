package squirreltable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The 49 contract-independent rows this suite names, by their roadmap ids
// (docs/roadmap.sexp). The eight rows the strict-only cut rewrites are owed to
// the strict-only leg and are not run here.
//
//	port/squirrel/fixed/file-envelope
//	port/squirrel/fixed/batch-capacity
//	port/squirrel/fixed/plan-selection
//	port/squirrel/fixed/compiled-plans
//	port/squirrel/fixed/definition-hash
//	port/squirrel/fixed/fixed-closure
//	port/squirrel/fixed/retirement
//	port/squirrel/fixed/retired-runtime
//	port/squirrel/fixed/optional-values
//	port/squirrel/fixed/field-evolution
//	port/squirrel/fixed/live-extents
//	port/squirrel/fixed/writing
//	port/squirrel/fixed/union-guards
//	port/squirrel/fixed/plan-execution
//	port/squirrel/fixed/interoperability
//	port/squirrel/fixed/bool-values
//	port/squirrel/fixed/signed-integers
//	port/squirrel/fixed/unsigned-integers
//	port/squirrel/fixed/signed-128
//	port/squirrel/fixed/unsigned-128
//	port/squirrel/fixed/ranged-integers
//	port/squirrel/fixed/bits-values
//	port/squirrel/fixed/float32-values
//	port/squirrel/fixed/float64-values
//	port/squirrel/fixed/compressed-floats
//	port/squirrel/fixed/signed-fixed-point
//	port/squirrel/fixed/unsigned-fixed-point
//	port/squirrel/fixed/flags-values
//	port/squirrel/fixed/enum-values
//	port/squirrel/fixed/utf8-values
//	port/squirrel/fixed/utf16-values
//	port/squirrel/fixed/byte-buffers
//	port/squirrel/fixed/nested-types
//	port/squirrel/fixed/nested-fixed-tables
//	port/squirrel/fixed/fixed-arrays
//	port/squirrel/fixed/counted-arrays
//	port/squirrel/fixed/keyed-arrays
//	port/squirrel/fixed/nested-keyed-arrays
//	port/squirrel/fixed/union-values
//	port/squirrel/fixed/union-field-arms
//	port/squirrel/fixed/payload-free-arms
//	port/squirrel/fixed/union-arrays
//	port/squirrel/fixed/optional-scalars
//	port/squirrel/fixed/optional-nesting
//	port/squirrel/fixed/optional-arrays
//	port/squirrel/fixed/scalar-defaults
//	port/squirrel/fixed/text-bytes-flags-defaults
//	port/squirrel/fixed/fixed-file-roundtrip
//	port/squirrel/fixed/record-measurement

// TestSquirrelFixedProofSuiteValues runs all 49 contract-independent
// fixed-table rows through the shared fixedproof value suite against the live
// C++ oracle.
func TestSquirrelFixedProofSuiteValues(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	fixedproof.RunValueSuite(t, squirrelFixedProofRunner{})
}

// TestSquirrelFixedProofSuiteNegativeControl runs both the file negative
// control and the writer negative control across all 49 rows under a hard 2 GiB
// memory ceiling.
func TestSquirrelFixedProofSuiteNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	fixedproof.RunNegativeControlSuite(t, squirrelFixedProofRunner{})
}

// TestSquirrelFixedProofEvolution runs the 16 non-value fixed-table rows
// (fixed-file-roundtrip and the 15 evolution/layout/plan/lineage/file rows)
// through the shared fixedproof evolution suite.
func TestSquirrelFixedProofEvolution(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	repo := filepath.Join("..", "..", "..")
	fixedproof.RunEvolutionSuite(t, repo, squirrelFixedProofRunner{})
}

// TestSquirrelFixedBatch4097Roundtrip proves that 4,097 fixed records save and
// read back without an internal 4096 cap, and refuse only when the caller
// supplies a smaller capacity.
func TestSquirrelFixedBatch4097Roundtrip(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table Point {\n    x int32 = 1\n    y int32 = 2\n}\n")
	files, err := sqGenerateAll(u, nil)
	if err != nil {
		t.Fatalf("squirreltable.Generate: %v", err)
	}
	dir := t.TempDir()
	sqWriteFiles(t, dir, files)
	script := fmt.Sprintf(`local T = dofile("schema_module.nut", true).require(%q);
local r = array(4097);
for (local i = 0; i < 4097; i++) {
  local p = T.Point();
  p.X = i + 1;
  p.Y = -(i + 1);
  r[i] = p;
}
local m = T.PointFixedMeasure(4097);
if (m <= 0) throw "FixedMeasure(4097)=" + m;
local wire = blob(m);
local n = T.PointFixedSave(wire, r);
if (n != m) throw "FixedSave(4097)=" + n + " want " + m + " reason=" + T.FixedReason;
local back = array(4097);
for (local i = 0; i < 4097; i++) back[i] = T.Point();
local got = T.PointFixedLoad(wire, back, null);
if (got != 4097) throw "FixedLoad(4097)=" + got + " reason=" + T.FixedReason;
if (back[0].X != 1 || back[0].Y != -1) throw "record 0 mismatch";
if (back[2048].X != 2049 || back[2048].Y != -2049) throw "record 2048 mismatch";
if (back[4096].X != 4097 || back[4096].Y != -4097) throw "record 4096 mismatch";
local short = array(4096);
for (local i = 0; i < 4096; i++) short[i] = T.Point();
local refused = T.PointFixedLoad(wire, short, 4096);
if (refused != -1) throw "expected -1 for 4097 records into cap=4096, got " + refused;
print("OK 4097\n");
`, squirreltable.ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
	if out != "OK 4097" {
		t.Fatalf("got %q, want OK 4097", out)
	}
}

type squirrelFixedProofRunner struct{}

func (squirrelFixedProofRunner) Name() string { return "squirrel" }

func sqGenerateAll(u *ir.Unit, lineage map[string][]squirreltable.FixedLineageEntry) (map[string][]byte, error) {
	files, err := squirrel.Generate(u)
	if err != nil {
		return nil, err
	}
	var tables map[string][]byte
	if lineage != nil {
		tables, err = squirreltable.GenerateLineage(u, lineage)
	} else {
		tables, err = squirreltable.Generate(u)
	}
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	return files, nil
}

func sqWriteFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const sqFileHelpers = `local mkblob = function(arr) {
  local b = blob(arr.len());
  for (local i = 0; i < arr.len(); i++) b[i] = arr[i];
  return b;
};
local readfile = function(path) {
  local f = file(path, "rb");
  local n = f.len();
  local b = n == 0 ? blob(0) : f.readblob(n);
  f.close();
  return b;
};
local writefile = function(path, b, n) {
  local f = file(path, "wb");
  if (n > 0) {
    local sub = blob(n);
    for (local i = 0; i < n; i++) sub[i] = b[i];
    f.writeblob(sub);
  }
  f.close();
};
`

func (squirrelFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	files, err := sqGenerateAll(u, nil)
	if err != nil {
		t.Fatalf("%s: squirreltable.Generate: %v", tc.ID, err)
	}
	dir := t.TempDir()
	sqWriteFiles(t, dir, files)
	mod := squirreltable.ModuleName(u)
	setup := squirrelSetupFor(tc.ID)
	script := fmt.Sprintf("%[5]slocal T = dofile(\"schema_module.nut\", true).require(%[1]q);\n"+
		"local r = array(%[3]d);\n"+
		"for (local i = 0; i < %[3]d; i++) r[i] = T.%[2]s();\n"+
		"%[4]s\n"+
		"local m = T.%[2]sFixedMeasure(%[3]d);\n"+
		"local out = blob(m > 0 ? m : 65536);\n"+
		"local n = T.%[2]sFixedSave(out, r);\n"+
		"if (n != m) throw \"FixedMeasure=\" + m + \" FixedSave=\" + n + \" reason=\" + T.FixedReason;\n"+
		"writefile(\"out.bin\", out, n);\n",
		mod, tc.Root, tc.Count, setup, sqFileHelpers)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	sqrun.Run(t, dir, "main.nut", nil)
	out, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (squirrelFixedProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wireBytes []byte) ([]byte, error) {
	t.Helper()
	files, err := sqGenerateAll(u, map[string][]squirreltable.FixedLineageEntry{})
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	sqWriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "in.bin"), wireBytes, 0o644); err != nil {
		return nil, err
	}
	mod := squirreltable.ModuleName(u)
	script := fmt.Sprintf("%[4]slocal T = dofile(\"schema_module.nut\", true).require(%[1]q);\n"+
		"local wire = readfile(\"in.bin\");\n"+
		"local rows = array(%[3]d);\n"+
		"for (local i = 0; i < %[3]d; i++) rows[i] = T.%[2]s();\n"+
		"local got = T.%[2]sFixedLoad(wire, rows, null);\n"+
		"if (got != %[3]d) { print(\"ERR:\" + T.FixedReason + \"\\n\"); return; }\n"+
		"local m = T.%[2]sFixedMeasure(%[3]d);\n"+
		"local out = blob(m > 0 ? m : 65536);\n"+
		"local n = T.%[2]sFixedSave(out, rows);\n"+
		"if (n != m) { print(\"ERR:save:\" + T.FixedReason + \"\\n\"); return; }\n"+
		"writefile(\"resaved.bin\", out, n);\n"+
		"print(\"OK\\n\");\n",
		mod, tc.Root, tc.Count, sqFileHelpers)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		return nil, err
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	if !strings.HasPrefix(strings.TrimSpace(out), "OK") {
		return nil, fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return os.ReadFile(filepath.Join(dir, "resaved.bin"))
}

func (squirrelFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	files, err := sqGenerateAll(u, nil)
	if err != nil {
		return nil, err
	}
	mod := squirreltable.ModuleName(u)
	patchSquirrelFixedWriterForMutation(t, tc.ID, mod, files)
	dir := t.TempDir()
	sqWriteFiles(t, dir, files)
	setup := squirrelSetupFor(tc.ID)
	script := fmt.Sprintf("%[5]slocal T = dofile(\"schema_module.nut\", true).require(%[1]q);\n"+
		"local r = array(%[3]d);\n"+
		"for (local i = 0; i < %[3]d; i++) r[i] = T.%[2]s();\n"+
		"%[4]s\n"+
		"local m = T.%[2]sFixedMeasure(%[3]d);\n"+
		"local out = blob(m > 0 ? m : 65536);\n"+
		"local n = T.%[2]sFixedSave(out, r);\n"+
		"if (n != m) { print(\"ERR:\" + T.FixedReason + \"\\n\"); return; }\n"+
		"writefile(\"out.bin\", out, n);\n"+
		"print(\"OK\\n\");\n",
		mod, tc.Root, tc.Count, setup, sqFileHelpers)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		return nil, err
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	if !strings.HasPrefix(strings.TrimSpace(out), "OK") {
		return nil, fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return os.ReadFile(filepath.Join(dir, "out.bin"))
}

func normalizeSqFieldKey(raw string) string {
	parts := strings.Split(raw, ".")
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if strings.HasSuffix(p, "Present") && len(p) > len("Present") {
			parts[i] = strings.ToLower(p[:len(p)-len("Present")]) + "_present"
		} else {
			parts[i] = strings.ToLower(p)
		}
	}
	return strings.Join(parts, ".")
}

// squirrelEnumOrdinals renders the unit's enum variant names as a Squirrel
// table from name to ordinal, so the dump reports the wire's number for an
// enum field the table surface stores by name.
func squirrelEnumOrdinals(u *ir.Unit) string {
	var b strings.Builder
	b.WriteString("local enumOrd = {};\n")
	for _, e := range u.Enums {
		for i, name := range e.Variants {
			fmt.Fprintf(&b, "enumOrd.rawset(%s, %d);\n", squirrel.StringLit([]byte(name)), i+1)
		}
	}
	return b.String()
}

// squirrelUnionArms renders each union's class beside its arm members by tag,
// so the dump can follow the selected arm and never a pre-allocated sibling.
func squirrelUnionArms(u *ir.Unit) string {
	names := make([]string, 0, len(u.Unions)+len(u.TableUnions))
	seen := map[string]bool{}
	for name := range u.Unions {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for name := range u.TableUnions {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("local unionArms = [];\n")
	b.WriteString("local addUnion = function(make, members) { unionArms.append([make().getclass(), members]); };\n")
	for _, name := range names {
		un := u.Unions[name]
		if un == nil {
			un = u.TableUnions[name]
		}
		b.WriteString(fmt.Sprintf("addUnion(function() { return T.%s(); }, [null", name))
		for _, v := range un.Variants {
			if v.Void() {
				b.WriteString(", null")
				continue
			}
			b.WriteString(", " + squirrel.StringLit([]byte(ir.GoExportName(v.Name))))
		}
		b.WriteString("]);\n")
	}
	b.WriteString("local unionArm = function(obj) {\n")
	b.WriteString("  if (!(\"Type\" in obj)) return null;\n")
	b.WriteString("  local c = obj.getclass();\n")
	b.WriteString("  foreach (u in unionArms) if (u[0] == c) { local m = u[1][obj.Type]; return m == null ? \"\" : m; }\n")
	b.WriteString("  return null;\n")
	b.WriteString("};\n")
	return b.String()
}

func (squirrelFixedProofRunner) LoadLineage(
	t *testing.T,
	u *ir.Unit,
	root string,
	lineage map[string][]fixedproof.LineageEntry,
	wireBytes []byte,
	cap int,
) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	sqLineage := make(map[string][]squirreltable.FixedLineageEntry)
	for k, entries := range lineage {
		out := make([]squirreltable.FixedLineageEntry, len(entries))
		for i, e := range entries {
			out[i] = squirreltable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			}
		}
		sqLineage[k] = out
	}
	files, err := sqGenerateAll(u, sqLineage)
	if err != nil {
		t.Fatalf("squirreltable.GenerateLineage: %v", err)
	}
	dir := t.TempDir()
	sqWriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "in.bin"), wireBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	mod := squirreltable.ModuleName(u)
	capArg := "null"
	allocN := max(64, len(wireBytes)/8)
	if cap >= 0 && cap <= allocN {
		allocN = cap
	}
	script := fmt.Sprintf(`%[5]s%[6]slocal T = dofile("schema_module.nut", true).require(%[1]q);
%[7]slocal wire = readfile("in.bin");
local rows = array(%[4]d);
for (local i = 0; i < %[4]d; i++) rows[i] = T.%[2]s();
local got = T.%[2]sFixedLoad(wire, rows, %[3]s);
if (got < 0) {
  print("REFUSED " + T.FixedReason + "\n");
  return;
}
rows.resize(got);
print("OK " + got + "\n");
local w = ("FixedWidened" in T) ? T.FixedWidened : 0;
print("COUNTERS 0 0 0 " + w + " 0\n");
local dump_fields = null;
dump_fields = function(prefix, obj) {
  if (typeof obj != "instance") return;
  local arm = unionArm(obj);
  if (arm != null) {
    print("FIELD " + prefix + ".Type=" + obj.Type + "\n");
    if (arm == "") return;
  }
  local keys = [];
  foreach (k, _ in obj.getclass()) {
    if (k == "constructor" || k == "Type") continue;
    keys.append(k);
  }
  foreach (k in keys) {
    if (arm != null && k != arm) continue;
    local v = obj[k];
    local key = prefix + "." + k;
    local vt = typeof v;
    if (vt == "string") {
      if (enumOrd.rawin(v)) print("FIELD " + key + "=" + enumOrd[v] + "\n");
      else print("FIELD " + key + "=\"" + v + "\"\n");
    } else if (vt == "bool") {
      print("FIELD " + key + "=" + (v ? 1 : 0) + "\n");
    } else if (vt == "integer" || vt == "float") {
      print("FIELD " + key + "=" + v + "\n");
    } else if (vt == "instance") {
      dump_fields(key, v);
    } else if (vt == "array") {
      print("FIELD " + key + "_count=" + v.len() + "\n");
      for (local i = 0; i < v.len(); i++) {
        local ev = v[i];
        local ekey = key + "[" + i + "]";
        local et = typeof ev;
        if (et == "string") {
          if (enumOrd.rawin(ev)) print("FIELD " + ekey + "=" + enumOrd[ev] + "\n");
          else print("FIELD " + ekey + "=\"" + ev + "\"\n");
        } else if (et == "bool") {
          print("FIELD " + ekey + "=" + (ev ? 1 : 0) + "\n");
        } else if (et == "integer" || et == "float") {
          print("FIELD " + ekey + "=" + ev + "\n");
        } else if (et == "instance") {
          dump_fields(ekey, ev);
        }
      }
    }
  }
};
for (local idx = 0; idx < got && idx < 8; idx++) {
  dump_fields("r" + idx, rows[idx]);
}
local m = T.%[2]sFixedMeasure(got);
local out = blob(m > 0 ? m : 65536);
local n = T.%[2]sFixedSave(out, rows);
if (n >= 0) writefile("resaved.bin", out, n);
`, mod, root, capArg, allocN, sqFileHelpers, squirrelEnumOrdinals(u), squirrelUnionArms(u))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	fields := map[string]string{}
	count := -1
	reason := ""
	var counters fixedproof.LoadCounters
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if after, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, strings.TrimSpace(after), fields, nil, fixedproof.LoadCounters{}
		}
		if after, ok := strings.CutPrefix(line, "OK "); ok {
			_, _ = fmt.Sscanf(after, "%d", &count)
			reason = "ok"
		} else if after, ok := strings.CutPrefix(line, "FIELD "); ok {
			if k, v, ok := strings.Cut(after, "="); ok {
				fields[normalizeSqFieldKey(k)] = v
			}
		} else if after, ok := strings.CutPrefix(line, "COUNTERS "); ok {
			var u0, k0, c0, w0, d0 int
			_, _ = fmt.Sscanf(after, "%d %d %d %d %d", &u0, &k0, &c0, &w0, &d0)
			counters = fixedproof.LoadCounters{Unknown: u0, KindMismatch: k0, Clamped: c0, Widened: w0, Duplicate: d0}
		}
	}
	var resaved []byte
	if count >= 0 {
		resaved, _ = os.ReadFile(filepath.Join(dir, "resaved.bin"))
	}
	return count, reason, fields, resaved, counters
}

// CheckLineage compiles the whole handed lineage, including the unit's own
// layout, and validates every entry's tree before any file selects it.
func (squirrelFixedProofRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	sqLineage := make(map[string][]squirreltable.FixedLineageEntry)
	for k, entries := range lineage {
		out := make([]squirreltable.FixedLineageEntry, len(entries))
		for i, e := range entries {
			out[i] = squirreltable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			}
		}
		sqLineage[k] = out
	}
	if _, err := sqGenerateAll(u, sqLineage); err != nil {
		return err.Error()
	}
	for _, e := range lineage[root] {
		if len(e.Layout) < 4 {
			return "lineage entry has no layout"
		}
		if count := int(binary.LittleEndian.Uint32(e.Layout[:4])); count > ir.TableFixedLeafCap {
			return "lineage entry is past the plan bound"
		}
	}
	return ""
}

func (squirrelFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root string, scenario string) ([]byte, string) {
	t.Helper()
	files, err := sqGenerateAll(u, nil)
	if err != nil {
		t.Fatalf("squirreltable.Generate: %v", err)
	}
	dir := t.TempDir()
	mod := squirreltable.ModuleName(u)
	var body string
	patchUnzeroed := false
	switch scenario {
	case "optional_absent_zeros":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].Plain.A = 11; r[0].Plain.B = 22;
r[0].OptPresent = false;
r[0].Opt.A = 0x12345678; r[0].Opt.B = 0x23456789;
local m = T.%[1]sFixedMeasure(1);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "live_extents_base":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].Arr = ["One"];
r[0].OptPresent = false;
r[0].Tail = 77;
local m = T.%[1]sFixedMeasure(1);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "writing_slack_zeros":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].Arr = [0x1122];
r[0].S = "A";
r[0].B = mkblob([0x42]);
r[0].Pick.Type = 1; r[0].Pick.Narrow = 7;
local m = T.%[1]sFixedMeasure(1);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "closure_small_42":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].X = 42;
local m = T.%[1]sFixedMeasure(1);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
if (n < 0) { print("REFUSED " + T.FixedReason + "\n"); return; }
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "outer_nested":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].Head = 7;
r[0].Inner.A = 11; r[0].Inner.B = "one";
r[0].Items[1].A = 33; r[0].Items[1].B = "six";
r[0].MaybePresent = true; r[0].Maybe.A = 44;
r[0].Tail = 9;
local m = T.%[1]sFixedMeasure(1);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
if (n < 0) { print("REFUSED " + T.FixedReason + "\n"); return; }
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "writing_slack_batch", "writing_slack_unzeroed":
		patchUnzeroed = scenario == "writing_slack_unzeroed"
		body = fmt.Sprintf(`local r = [T.%[1]s(), T.%[1]s()];
for (local i = 0; i < 2; i++) {
  r[i].Arr = [0x1122];
  r[i].S = "A";
  r[i].B = mkblob([0x42]);
  r[i].Pick.Type = 1; r[i].Pick.Narrow = 7;
}
local m = T.%[1]sFixedMeasure(2);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
if (n != m) { print("REFUSED " + T.FixedReason + "\n"); return; }
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "w14_writer_offsets":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].A = 0xA1;
r[0].B = 0x0B0B0B0B;
r[0].Inner.P = 0x1E1E1E1E;
r[0].Inner.Q = 0x2E2E;
r[0].OptPresent = true; r[0].Opt = 0x0D0D0D0D;
r[0].Arr = [0x3E3E, 0x4E4E];
r[0].S = "seven";
r[0].Tail = 0x7A7A7A7A;
local m = T.%[1]sFixedMeasure(1);
local out = blob(m);
local n = T.%[1]sFixedSave(out, r);
if (n != m) { print("REFUSED " + T.FixedReason + "\n"); return; }
writefile("out.bin", out, n);
print("OK\n");
`, root)
	case "writing_overcount":
		body = fmt.Sprintf(`local r = [T.%[1]s()];
r[0].Arr = [1, 2, 3, 4];
local m = T.%[1]sFixedMeasure(1);
local out = blob(m > 0 ? m : 65536);
local n = T.%[1]sFixedSave(out, r);
if (n < 0) { print("REFUSED " + T.FixedReason + "\n"); return; }
writefile("out.bin", out, n);
print("OK\n");
`, root)
	default:
		t.Fatalf("unknown SaveCustom scenario %q", scenario)
	}
	if patchUnzeroed {
		modFile := mod + ".nut"
		src := string(files[modFile])
		target := "b.writeblob(zero);"
		if !strings.Contains(src, target) {
			t.Fatalf("writing_slack_unzeroed: mutation target %q not found", target)
		}
		files[modFile] = []byte(strings.Replace(src, target, target+" b[start + 8] = 0xAA;", 1))
	}
	sqWriteFiles(t, dir, files)
	script := fmt.Sprintf("%slocal T = dofile(\"schema_module.nut\", true).require(%q);\n%s", sqFileHelpers, mod, body)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
	if after, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return nil, strings.TrimSpace(after)
	}
	wire, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return wire, ""
}

func (squirrelFixedProofRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	files, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("squirreltable.Generate: %v", err)
	}
	mod := squirreltable.ModuleName(u) + ".nut"
	return strings.Contains(string(files[mod]), "_M."+root+"FixedSave <-")
}

func patchSquirrelFixedWriterForMutation(t *testing.T, id, mod string, files map[string][]byte) {
	t.Helper()
	modFile := mod + ".nut"
	modSrc := string(files[modFile])
	rtSrc := string(files["schema_table_fixed.nut"])

	switch id {
	case "bool-values":
		target := "? 1 : 0"
		if !strings.Contains(modSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files[modFile] = []byte(strings.Replace(modSrc, target, "? 0 : 1", 1))
	case "float32-values", "compressed-floats", "scalar-defaults":
		target := "_M.put(b, at, 4, bits);"
		if !strings.Contains(rtSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files["schema_table_fixed.nut"] = []byte(strings.Replace(rtSrc, target, "_M.put(b, at, 4, bits ^ 1);", 1))
	case "float64-values":
		target := "_M.put(b, at, 8, bits);"
		if !strings.Contains(rtSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files["schema_table_fixed.nut"] = []byte(strings.Replace(rtSrc, target, "_M.put(b, at, 8, bits ^ 1);", 1))
	case "utf8-values", "byte-buffers", "nested-types", "text-bytes-flags-defaults":
		target := "& 0xff;"
		if !strings.Contains(modSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files[modFile] = []byte(strings.Replace(modSrc, target, "^ 1;", 1))
	case "utf16-values":
		target := "= u & 0xff;"
		if !strings.Contains(modSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files[modFile] = []byte(strings.Replace(modSrc, target, "= (u ^ 1) & 0xff;", 1))
	case "enum-values", "live-extents":
		target := "ev = 1;"
		if !strings.Contains(modSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files[modFile] = []byte(strings.Replace(modSrc, target, "ev = 0;", 1))
	case "optional-scalars", "optional-nesting", "optional-arrays", "optional-values":
		target := "Present ? 1 : 0"
		if !strings.Contains(modSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files[modFile] = []byte(strings.Replace(modSrc, target, "Present ? 0 : 0", 1))
	case "union-values", "union-field-arms", "payload-free-arms", "union-arrays", "writing", "union-guards":
		target := ".Type);"
		if !strings.Contains(modSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files[modFile] = []byte(strings.Replace(modSrc, target, ".Type * 0);", 1))
	default:
		target := "b[at + i] = (v >> (8 * i)) & 0xff;"
		if !strings.Contains(rtSrc, target) {
			t.Fatalf("%s: mutation target %q not found", id, target)
		}
		files["schema_table_fixed.nut"] = []byte(strings.Replace(rtSrc, target, "b[at + i] = ((v ^ 1) >> (8 * i)) & 0xff;", 1))
	}
}

func squirrelSetupFor(id string) string {
	switch id {
	case "bool-values":
		return `r[0].A = false; r[0].B = true;
r[1].A = true; r[1].B = false;`
	case "signed-integers":
		return `r[1].A = 127; r[1].B = 32767; r[1].C = 2147483647; r[1].D = 9223372036854775807;
r[2].A = -1; r[2].B = 0; r[2].C = 1; r[2].D = -2;`
	case "unsigned-integers":
		return `r[1].A = 0; r[1].B = 0; r[1].C = 0; r[1].D = 0;
r[2].A = 1; r[2].B = 256; r[2].C = 65536; r[2].D = 4294967296;`
	case "signed-128":
		return `r[1].A = [-1, 9223372036854775807];
r[2].A = [1, 0];`
	case "unsigned-128":
		return `r[1].A = [0, 0];
r[2].A = [1, 0];`
	case "ranged-integers":
		return `r[0].A = -5; r[0].B = -1000000000000; r[0].C = 0;
r[1].A = 5; r[1].B = 1000000000000; r[1].C = 1000;`
	case "bits-values":
		return `r[0].A = 7; r[0].B = 4095; r[0].C = 1048575; r[0].D = 1099511627775;
r[1].A = 0; r[1].B = 0; r[1].C = 0; r[1].D = 0;`
	case "float32-values":
		return `r[1].A = -1024.25; r[1].B = 2048.75;`
	case "float64-values":
		return `r[1].A = -987654321.125; r[1].B = 123456789.875;`
	case "compressed-floats":
		return `r[0].A = 0.25; r[0].B = -0.5;
r[1].A = 0.0; r[1].B = -1.0;
r[2].A = 1.0; r[2].B = 1.0;`
	case "signed-fixed-point":
		return `r[0].A = -(8 << 16); r[0].B = -(100 << 16); r[0].C = [-16777216, -1];
r[1].A = (8 << 16); r[1].B = (100 << 16); r[1].C = [0x00fa0000, 0];`
	case "unsigned-fixed-point":
		return `r[0].A = 0; r[0].B = 0; r[0].C = [0, 0];
r[1].A = (16 << 16); r[1].B = (100 << 16); r[1].C = [(1000 << 16), 0];`
	case "flags-values":
		return `r[0].Caps = 0; r[1].Caps = 5; r[2].Caps = 7;`
	case "enum-values":
		return `r[0].G = null; r[1].G = "Gold"; r[2].G = "Bronze";`
	case "utf8-values":
		return `r[1].S = "hello";`
	case "utf16-values":
		return `r[1].W = [0x0041, 0x03b1, 0xd83d, 0xde00];`
	case "byte-buffers":
		return `r[1].Blob = mkblob([0xff, 0x00, 0x5a, 0x01, 0x02, 0x03]);`
	case "nested-types":
		return `r[0].Badge.Title = "hi";`
	case "nested-fixed-tables":
		return `r[0].Child.N = -2147483648; r[1].Child.N = 2147483647;`
	case "fixed-arrays":
		return `r[0].Tag = [-2147483648, 0, 2147483647];`
	case "counted-arrays":
		return `r[1].Nums = [10, 65535, 32768];`
	case "keyed-arrays":
		return `r[0].Slots = [-2147483648, 2147483647];`
	case "nested-keyed-arrays":
		return `r[0].Slots[0].N = 11; r[0].Slots[1].N = -22;`
	case "union-values":
		return `r[0].Body.Type = 1; r[0].Body.V.X = 11; r[0].Body.V.Y = -22;
r[1].Body.Type = 2; r[1].Body.T.N = 41;
r[2].Body.Type = 3; r[2].Body.N = 1073741821;`
	case "union-field-arms":
		return `r[0].Body.Type = 1; r[0].Body.Sa = -5;
r[1].Body.Type = 2; r[1].Body.Ta = "abc";
r[2].Body.Type = 3; r[2].Body.Aa = [-1, 0, 1];`
	case "payload-free-arms":
		return `r[0].Lead = 10; r[0].Pick.Type = 1; r[0].Pick.Alpha = 99; r[0].Trail = 20;
r[1].Lead = 30; r[1].Pick.Type = 2; r[1].Pick.Alpha = 777; r[1].Trail = 40;
r[2].Lead = 50; r[2].Pick.Type = 3; r[2].Pick.Alpha = 888; r[2].Trail = 60;`
	case "union-arrays":
		return `r[0].Picks = [T.Pick(), T.Pick(), T.Pick()];
r[0].Picks[0].Type = 1; r[0].Picks[0].A = 3;
r[0].Picks[1].Type = 2; r[0].Picks[1].B = -1099511627776;
r[0].Picks[2].Type = 1; r[0].Picks[2].A = 0;`
	case "optional-scalars":
		return `r[1].N = -123456; r[1].NPresent = true; r[1].G = "Silver"; r[1].GPresent = true;`
	case "optional-nesting":
		return `r[1].P.X = 99; r[1].P.Y = -99; r[1].PPresent = true;`
	case "optional-arrays":
		return `r[1].Xs = [7, -8, 9]; r[1].XsPresent = true;`
	case "scalar-defaults":
		return `r[1].X = -9; r[1].G = "Bronze"; r[1].F = -2.25;`
	case "text-bytes-flags-defaults":
		return `r[1].S = "xyz"; r[1].B = mkblob([0x00, 0xff, 0x5a, 0x01]); r[1].C = 4;`
	case "record-measurement":
		return `r[0].A = 10; r[0].B = 20; r[1].A = 30; r[1].B = 40;`
	case "fixed-file-roundtrip":
		return `r[1].A = 22; r[1].B = "test";`
	case "file-envelope":
		return `r[1].X = 10; r[1].Y = 20;`
	case "batch-capacity":
		return `r[1].X = 3; r[1].Y = 4;
r[2].X = 5; r[2].Y = 6;`
	case "plan-selection":
		return `r[1].X = 33; r[1].Y = 44;`
	case "compiled-plans":
		return `r[1].A = 100; r[1].B = -20; r[1].C = 55;`
	case "definition-hash":
		return `r[1].X = 9; r[1].B = "abc";`
	case "fixed-closure":
		return `r[1].N.A = 30; r[1].N.B = 40; r[1].Z = 70;`
	case "retirement":
		return `r[1].A = 10; r[1].B = 20; r[1].C = 30;`
	case "retired-runtime":
		return `r[1].X = 44; r[1].Y = 55; r[1].Z = 66;`
	case "optional-values":
		return `r[1].Plain.A = 11; r[1].Plain.B = 22;
r[1].OptPresent = true; r[1].Opt.A = 33; r[1].Opt.B = 44;`
	case "field-evolution":
		return `r[1].B = 100; r[1].C = 200;`
	case "live-extents":
		return `r[1].Arr = ["One"]; r[1].OptPresent = false; r[1].Tail = 88;`
	case "writing":
		return `r[1].Arr = [0x1122]; r[1].S = "A"; r[1].B = mkblob([0x42]); r[1].Pick.Type = 1; r[1].Pick.Narrow = 7;`
	case "union-guards":
		return `r[1].U.Type = 2; r[1].U.Nested.X = 12; r[1].U.Nested.Y = 0x12345678; r[1].Tail = 77;`
	case "plan-execution":
		return `r[1].A = 10; r[1].Sub.P = 30; r[1].Sub.Q = 40; r[1].Xs = [50, 60];`
	case "interoperability":
		return `r[1].A = 42; r[1].S = "test";`
	default:
		panic("missing squirrelSetupFor case: " + id)
	}
}

func squirrelCheckFor(id string) string {
	switch id {
	case "bool-values":
		return `if (rows[0].A != false || rows[0].B != true || rows[1].A != true || rows[1].B != false) throw "bool-values mismatch";`
	case "signed-integers":
		return `if (rows[0].A != -128 || rows[1].A != 127 || rows[1].D != 9223372036854775807 || rows[2].D != -2) throw "signed-integers mismatch";`
	case "unsigned-integers":
		return `if (rows[0].A != 255 || rows[1].D != 0 || rows[2].D != 4294967296) throw "unsigned-integers mismatch";`
	case "signed-128":
		return `if (rows[0].A[0] != 0 || rows[1].A[1] != 9223372036854775807 || rows[2].A[0] != 1) throw "signed-128 mismatch";`
	case "unsigned-128":
		return `if (rows[0].A[0] != -1 || rows[1].A[0] != 0 || rows[2].A[0] != 1 || rows[2].A[1] != 0) throw "unsigned-128 mismatch";`
	default:
		return ""
	}
}

var _ = hex.EncodeToString
