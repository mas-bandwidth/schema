package squirreltable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cookShape carries the chain each CASE's fixture is generated with, matching
// test/conformance/harness/run.go's cookShape: a property of the fixture
// generator, not of the conformance data.
var cookShape = map[string][]string{
	"Scene":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"SceneValued": {"--ref", "head", "--chain", "ListNode", "--next", "next", "--values"},
	"Depot":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"Album":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"TreeNode":    {"--ref", "left", "--chain", "TreeNode", "--next", "left"},
	"ListNode":    {"--ref", "next", "--chain", "ListNode", "--next", "next"},
}

type cookRow struct {
	key  string
	unit string
	root string
	dump string
}

type cookPatch struct {
	offset int64
	width  int
	value  uint64
}

type cookForgery struct {
	name    string
	kind    string
	subject string
	base    string
	pointer int
	patches []cookPatch
	extent  int64
	verdict string
}

func readCookRows(t *testing.T, path string) ([]cookRow, []cookForgery, map[string]string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []cookRow
	var forgeries []cookForgery
	refusals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "cook":
			if len(fields) >= 5 {
				rows = append(rows, cookRow{key: fields[1], unit: fields[2], root: fields[3], dump: fields[4]})
			}
		case "refusal":
			if len(fields) >= 3 {
				refusals[fields[1]] = fields[2]
			}
		case "forgery":
			if len(fields) < 12 {
				continue
			}
			pointer := 0
			if fields[5] == "null" {
				pointer = -1
			} else {
				n, err := strconv.Atoi(fields[5])
				if err != nil {
					t.Fatalf("%s: forgery %s: %q is not a lead", path, fields[1], fields[5])
				}
				pointer = n
			}
			var patches []cookPatch
			offs := strings.Split(fields[6], ",")
			wids := strings.Split(fields[7], ",")
			vals := strings.Split(fields[8], ",")
			if len(offs) != len(wids) || len(offs) != len(vals) {
				t.Fatalf("%s: forgery %s: patch columns disagree", path, fields[1])
			}
			for i := range offs {
				off, err := strconv.ParseInt(offs[i], 0, 64)
				if err != nil {
					t.Fatalf("%s: forgery %s: %q is not an offset", path, fields[1], offs[i])
				}
				wid, err := strconv.Atoi(wids[i])
				if err != nil {
					t.Fatalf("%s: forgery %s: %q is not a width", path, fields[1], wids[i])
				}
				val, err := strconv.ParseUint(vals[i], 0, 64)
				if err != nil {
					t.Fatalf("%s: forgery %s: %q is not a word", path, fields[1], vals[i])
				}
				if off < 0 {
					continue
				}
				patches = append(patches, cookPatch{offset: off, width: wid, value: val})
			}
			extent, err := strconv.ParseInt(fields[9], 0, 64)
			if err != nil {
				t.Fatalf("%s: forgery %s: %q is not an extent", path, fields[1], fields[9])
			}
			forgeries = append(forgeries, cookForgery{
				name: fields[1], kind: fields[2], subject: fields[3], base: fields[4],
				pointer: pointer, patches: patches, extent: extent, verdict: fields[10],
			})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, forgeries, refusals
}

// TestSquirrelCookReadMatchesTheCorpus reads every cook row of
// testdata/conformance/tables/MANIFEST.txt through the Squirrel cook surface
// (docs/SPEC-TABLES.md §7): Open points at the region and every field is read
// at its offset, every pointer by its self-relative delta, and the canonical
// node dump is byte-equal to the corpus's. The hostile battery (§7, §7.4, §11)
// is then replayed: each forgery a `refusal` line names must answer that exact
// reason, so the first failing clause is pinned and not merely a null.
func TestSquirrelCookReadMatchesTheCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows, forgeries, refusals := readCookRows(t, manifest)

	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}

	// materialise the cook fixtures with test/cookgen, exactly as the harness
	// does (test/conformance/harness/run.go).
	work := t.TempDir()
	cookgen := filepath.Join(work, "cookgen")
	build := exec.Command("go", "build", "-o", cookgen, filepath.Join(root, "test", "cookgen"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building cookgen: %v: %s", err, out)
	}
	cooks := map[string][]byte{}
	for _, r := range rows {
		extra, ok := cookShape[r.key]
		if !ok {
			t.Fatalf("cook %s: no fixture shape", r.key)
		}
		out := filepath.Join(work, r.key+".cook")
		args := append([]string{"--bytes", "4096", "--root", r.root, "--out", out}, extra...)
		cmd := exec.Command(cookgen, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cooking %s: %v: %s", r.key, err, out)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		cooks[r.key] = data
	}

	inputs := map[string][]byte{}
	for key, data := range cooks {
		inputs[key+".cook"] = data
	}

	// the hostile battery: the same PATCH over the base fixture the harness
	// applies, and the caller's extent and buffer lead as the manifest states.
	type hostileCase struct {
		name   string
		base   string
		root   string
		input  string
		null   bool
		lead   int
		reason string
	}
	var hostiles []hostileCase
	for _, f := range forgeries {
		if f.kind != "cook" {
			continue
		}
		reason, ok := refusals[f.name]
		if !ok {
			continue // an open row, or a row no reason line names
		}
		if reason == "ok" {
			continue // §7's match-and-point ruling: not a refusal
		}
		data := append([]byte(nil), cooks[f.base]...)
		for _, p := range f.patches {
			var word [8]byte
			binary.LittleEndian.PutUint64(word[:], p.value)
			if p.offset+int64(p.width) > int64(len(data)) {
				t.Fatalf("forgery %s: the patch at %d+%d does not fit %s (%d bytes)", f.name, p.offset, p.width, f.base, len(data))
			}
			copy(data[p.offset:p.offset+int64(p.width)], word[:p.width])
		}
		if f.extent >= 0 && f.extent < int64(len(data)) {
			data = data[:f.extent]
		}
		input := "hostile." + f.name
		inputs[input] = data
		hostiles = append(hostiles, hostileCase{
			name: f.name, base: f.base, root: f.subject, input: input,
			null: f.pointer < 0, lead: f.pointer, reason: reason,
		})
	}

	// byUnit ties each cook row's case to the module its unit generates.
	byUnit := map[string]bool{}
	for _, r := range rows {
		byUnit[r.unit] = true
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(`local inputs = dofile("inputs.nut", true);
local u = function(b, at, n) {
  local v = 0;
  for (local i = n - 1; i >= 0; i--) v = v * 256 + (b[at + i] & 0xff);
  return v;
};
local s = function(b, at, n) {
  if (n == 8) {
    local lo = u(b, at, 4);
    local hi = u(b, at + 4, 4);
    if (hi & 0x80000000) hi -= 4294967296;
    return hi * 4294967296 + lo;
  }
  local v = u(b, at, n);
  local half = 1;
  for (local i = 0; i < n; i++) half *= 256;
  half = half / 2;
  if (v >= half) v -= half * 2;
  return v;
};
local text = function(b, at, used) {
  if (used < 0) used = 0;
  local out = "\"";
  for (local i = 0; i < used; i++) {
    local c = b[at + i] & 0xff;
    if (c >= 0x20 && c < 0x7f && c != 34 && c != 92) out += format("%c", c);
    else out += format("\\x%02x", c);
  }
  return out + "\" len=" + used;
};
local scalar = function(b, at, storage, size) {
  if (storage == "Bool") return ((b[at] & 0xff) != 0) ? "true" : "false";
  if (storage == "Signed") return format("%d", s(b, at, size));
  return format("%d", u(b, at, size));
};
local walkNode, walkStorage;
walkNode = function(T, formName, b, off0, length, offset, seen, order) {
  if (offset < 0 || offset + T.cookForms[formName].size > length) {
    print("ERROR node " + formName + " at " + offset + " does not fit " + length + "\n");
    return;
  }
  if (offset in seen) {
    if (seen[offset] != formName) print("ERROR node " + offset + " is two tables\n");
    return;
  }
  seen[offset] <- formName;
  print(format("node %d %s @%d\n", order.len(), formName, offset));
  order.append(offset);
  walkStorage(T, formName, b, off0, length, offset, "", seen, order);
};
walkStorage = function(T, formName, b, off0, length, storage, path, seen, order) {
  local form = T.cookForms[formName];
  foreach (f in form.fields) {
    local name = path.len() == 0 ? f.name : path + "." + f.name;
    local used = -1;
    if (f.countOffset >= 0) used = s(b, off0 + storage + f.countOffset, 4);
    if (f.isPointer) {
      local delta = s(b, off0 + storage + f.offset, 8);
      if (delta == 0) { print("  " + name + " = null\n"); continue; }
      local target = storage + f.offset + delta;
      if (target < 0 || target >= length) {
        print("ERROR " + name + " resolves outside the region\n");
        continue;
      }
      print(format("  %s = -> @%d\n", name, target));
      walkNode(T, f.table, b, off0, length, target, seen, order);
      continue;
    }
    local slots = f.isArray ? f.bound : 1;
    if (f.storage == "String" || f.storage == "Bytes") {
      print("  " + name + " = " + text(b, off0 + storage + f.offset, used >= 0 ? used : 0) + "\n");
    } else if (f.storage == "Record") {
      for (local sl = 0; sl < slots; sl++) {
        local sp = f.isArray ? name + "[" + sl + "]" : name;
        walkStorage(T, f.table, b, off0, length, storage + f.offset + sl * f.elemSize, sp, seen, order);
      }
    } else {
      for (local sl = 0; sl < slots; sl++) {
        local sp = f.isArray ? name + "[" + sl + "]" : name;
        print("  " + sp + " = " + scalar(b, off0 + storage + f.offset + sl * f.elemSize, f.storage, f.elemSize) + "\n");
      }
    }
    if (f.isArray && f.countOffset >= 0) print(format("  %s#count = %d\n", name, used));
    if (f.presentOffset >= 0) print("  " + name + "#present = " + (((b[off0 + storage + f.presentOffset] & 0xff) != 0) ? "true" : "false") + "\n");
  }
};
local dump = function(T, root, key) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.cookOpen(root, inputs[key + ".cook"], report);
  if (h == null) {
    print("FAIL " + key + " " + (("reason" in report) ? report.reason : "?") + "\n");
    return;
  }
  local seen = {}, order = [];
  print("BEGIN " + key + "\n");
  walkNode(T, root, h.Bytes, h.Region, h.Length, 0, seen, order);
  print("END " + key + "\n");
};
local hostile = function(T, name, root, lead) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.cookOpen(root, inputs[name], report, lead);
  if (h == null) print("HOSTILE " + name + " " + (("reason" in report) ? report.reason : "?") + "\n");
  else print("HOSTILE " + name + " ok\n");
};
local hostileNull = function(T, name, root) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.cookOpen(root, null, report);
  if (h == null) print("HOSTILE " + name + " " + (("reason" in report) ? report.reason : "?") + "\n");
  else print("HOSTILE " + name + " ok\n");
};
`)

	modules := map[string]string{}
	for key := range byUnit {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("cook rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		modules[key] = squirreltable.ModuleName(u)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the table surface refused: %v", key, err)
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
	}

	for key := range byUnit {
		module := modules[key]
		fmt.Fprintf(&p, "local schema_%s = dofile(%q, true);\n", key, filepath.Join(key, "schema_module.nut"))
		fmt.Fprintf(&p, "local T_%s = schema_%s.require(%q);\n", key, key, module)
		for _, r := range rows {
			if r.unit == key {
				fmt.Fprintf(&p, "dump(T_%s, %q, %q);\n", key, r.root, r.key)
			}
		}
		for _, h := range hostiles {
			// every hostile row's base case belongs to the row's own unit: the
			// base's unit is found through the cook rows.
			if cookUnitOfCase(rows, h.base) != key {
				continue
			}
			if h.null {
				fmt.Fprintf(&p, "hostileNull(T_%s, %q, %q);\n", key, h.input, h.root)
			} else {
				fmt.Fprintf(&p, "hostile(T_%s, %q, %q, %d);\n", key, h.input, h.root, h.lead)
			}
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	sections := map[string][]string{}
	var hostileLines []string
	current := ""
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "BEGIN "):
			current = strings.TrimPrefix(line, "BEGIN ")
		case strings.HasPrefix(line, "END "):
			current = ""
		case strings.HasPrefix(line, "HOSTILE "):
			hostileLines = append(hostileLines, line)
		case current != "":
			sections[current] = append(sections[current], line)
		default:
			if line != "" {
				t.Errorf("unexpected output %q", line)
			}
		}
	}

	count := 0
	for _, r := range rows {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.dump)))
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(sections[r.key], "\n")
		if got != "" {
			got += "\n"
		}
		if got != string(want) {
			t.Errorf("cook %s: the Squirrel read does not match %s\n--- got ---\n%s--- want ---\n%s", r.key, r.dump, got, want)
			continue
		}
		count++
	}

	hostileCount := 0
	for _, line := range hostileLines {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Errorf("hostile output %q: want name and reason", line)
			continue
		}
		name, got := strings.TrimPrefix(fields[1], "hostile."), fields[2]
		want, ok := refusals[name]
		if !ok {
			t.Errorf("hostile %s: no refusal line names it", name)
			continue
		}
		if got == "ok" {
			t.Errorf("hostile %s: opened, want refusal %s", name, want)
			continue
		}
		if got != want {
			t.Errorf("hostile %s: refusal %s, want %s", name, got, want)
			continue
		}
		hostileCount++
	}
	if hostileCount != len(hostiles) {
		t.Errorf("hostile battery: read %d of %d named refusals", hostileCount, len(hostiles))
	}
	t.Logf("read %d cook rows and %d named refusals", count, hostileCount)
}

// cookUnitOfCase finds the unit a cook CASE belongs to, so a forgery over that
// case is sent to the module that carries its root.
func cookUnitOfCase(rows []cookRow, name string) string {
	for _, r := range rows {
		if r.key == name {
			return r.unit
		}
	}
	return ""
}
