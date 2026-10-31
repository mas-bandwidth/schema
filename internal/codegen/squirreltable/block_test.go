package squirreltable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// blockRoot maps each block case name in testdata/conformance/tables/MANIFEST.txt
// to the root table in its unit, matching test/conformance/go/blockdump.go.
var blockRoot = map[string]string{
	"block_render": "RenderFrame",
	"block_padded": "PaddedFrame",
}

type blockRow struct {
	key  string
	unit string
	root string
	bin  string
	dump string
}

func readBlockRows(t *testing.T, path string) ([]blockRow, []cookForgery, map[string]string) {
	t.Helper()
	_, forgeries, refusals := readCookRows(t, path)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []blockRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] == "block" && len(fields) >= 5 {
			root, ok := blockRoot[fields[1]]
			if !ok {
				t.Fatalf("%s: block %s: no root table mapping", path, fields[1])
			}
			rows = append(rows, blockRow{
				key:  fields[1],
				unit: fields[2],
				root: root,
				bin:  fields[3],
				dump: fields[4],
			})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, forgeries, refusals
}

// TestSquirrelBlockReadMatchesTheCorpus reads every block row of
// testdata/conformance/tables/MANIFEST.txt through the Squirrel block surface
// (docs/SPEC-TABLES.md §19.2): Open checks the prologue and bounds every
// out-of-line array triple in O(1), then the projection and every out-of-line
// array row are walked through the generated descriptors and compared byte for
// byte against the corpus's canonical block dumps. The hostile forgery battery
// (§19.2, §19.4, §11) is then replayed: each block forgery a `refusal` line
// names must answer that exact reason, along with unaligned_base and bad_layout
// when the caller passes an unaligned lead, a null blob, or an unknown root.
func TestSquirrelBlockReadMatchesTheCorpus(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows, forgeries, refusals := readBlockRows(t, manifest)

	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}

	blocks := map[string][]byte{}
	inputs := map[string][]byte{}
	for _, r := range rows {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.bin)))
		if err != nil {
			t.Fatal(err)
		}
		blocks[r.key] = data
		inputs[r.key+".block"] = data
	}

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
		if f.kind != "block" {
			continue
		}
		reason, ok := refusals[f.name]
		if !ok || reason == "ok" {
			continue
		}
		data := append([]byte(nil), blocks[f.base]...)
		for _, p := range f.patches {
			var word [8]byte
			binary.LittleEndian.PutUint64(word[:], p.value)
			if p.offset+int64(p.width) > int64(len(data)) {
				t.Fatalf("forgery %s: the patch at %d+%d does not fit %s (%d bytes)", f.name, p.offset, p.width, f.base, len(data))
			}
			copy(data[p.offset:p.offset+int64(p.width)], word[:p.width])
		}
		switch {
		case f.extent >= 0 && f.extent < int64(len(data)):
			data = data[:f.extent]
		case f.extent > int64(len(data)):
			grown := make([]byte, f.extent)
			copy(grown, data)
			data = grown
		}
		rootName, ok := blockRoot[f.subject]
		if !ok {
			rootName = f.subject
		}
		input := "hostile." + f.name
		inputs[input] = data
		hostiles = append(hostiles, hostileCase{
			name: f.name, base: f.base, root: rootName, input: input,
			null: f.pointer < 0, lead: f.pointer, reason: reason,
		})
	}
	// Pin caller placement and unknown-root refusals directly alongside the
	// manifest forgery battery.
	refusals["block_unaligned_lead"] = "unaligned_base"
	hostiles = append(hostiles, hostileCase{
		name: "block_unaligned_lead", base: "block_render", root: "RenderFrame",
		input: "block_render.block", null: false, lead: 8, reason: "unaligned_base",
	})
	refusals["block_null_bytes"] = "unaligned_base"
	hostiles = append(hostiles, hostileCase{
		name: "block_null_bytes", base: "block_render", root: "RenderFrame",
		input: "block_render.block", null: true, lead: 0, reason: "unaligned_base",
	})
	refusals["block_unknown_root"] = "bad_layout"
	hostiles = append(hostiles, hostileCase{
		name: "block_unknown_root", base: "block_render", root: "NoSuchFrame",
		input: "block_render.block", null: false, lead: 0, reason: "bad_layout",
	})

	byUnit := map[string]bool{}
	for _, r := range rows {
		byUnit[r.unit] = true
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(`local inputs = dofile("inputs.nut", true);
local u = function(b, at, n) {
  local v = 0;
  for (local i = n - 1; i >= 0; i--) v = (v << 8) | (b[at + i] & 0xff);
  return v;
};
local s = function(b, at, n) {
  local v = u(b, at, n);
  if (n < 8) {
    local bit = 1 << (n * 8 - 1);
    if ((v & bit) != 0) v -= (bit << 1);
  }
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
  if (storage == "Float") {
    if (size == 4) return format("0x%08x", u(b, at, 4));
    return format("0x%08x%08x", u(b, at + 4, 4), u(b, at, 4));
  }
  if (storage == "Signed") return format("%d", s(b, at, size));
  return format("%d", u(b, at, size));
};
local walkRecord;
walkRecord = function(T, info, b, storage, path) {
  foreach (f in info.fields) {
    if (f.outOfLine) continue;
    local name = path.len() == 0 ? f.name : path + "." + f.name;
    if (f.counted) {
      local used = s(b, storage + f.countOffset, 4);
      print("  " + name + " = " + text(b, storage + f.offset, used) + "\n");
    } else {
      local slots = f.isArray ? f.bound : 1;
      for (local sl = 0; sl < slots; sl++) {
        local where = f.isArray ? name + "[" + sl + "]" : name;
        local value = storage + f.offset + sl * f.elemSize;
        if (f.table != null) {
          walkRecord(T, T.blockRecords[f.table], b, value, where);
        } else {
          print("  " + where + " = " + scalar(b, value, f.storage, f.elemSize) + "\n");
        }
      }
    }
    if (f.optional) {
      print("  " + name + "#present = " + (((b[storage + f.presentOffset] & 0xff) != 0) ? "true" : "false") + "\n");
    }
  }
};
local dump = function(T, root, key) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.blockOpen(root, inputs[key + ".block"], report);
  if (h == null) {
    print("FAIL " + key + " " + (("reason" in report) ? report.reason : "?") + "\n");
    return;
  }
  local b = h.Bytes;
  local form = h.Form;
  print("BEGIN " + key + "\n");
  print(format("projection %s @0\n", form.projection.name));
  walkRecord(T, form.projection, b, 0, "");
  foreach (a in form.arrays) {
    local offsetOf = u(b, a.offsetOfOffset, 8);
    local count = u(b, a.countOffset, 4);
    local stride = u(b, a.strideOffset, 4);
    local rowInfo = T.blockRecords[a.elem];
    print(format("array %s %s @%d count=%d stride=%d\n", a.name, rowInfo.name, offsetOf, count, stride));
    for (local r = 0; r < count; r++) {
      local at = offsetOf + r * stride;
      print(format("row %d @%d\n", r, at));
      walkRecord(T, rowInfo, b, at, "");
    }
  }
  print("END " + key + "\n");
};
local hostile = function(T, name, input, root, lead) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.blockOpen(root, inputs[input], report, lead);
  if (h == null) print("HOSTILE " + name + " " + (("reason" in report) ? report.reason : "?") + "\n");
  else print("HOSTILE " + name + " ok\n");
};
local hostileNull = function(T, name, root) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.blockOpen(root, null, report);
  if (h == null) print("HOSTILE " + name + " " + (("reason" in report) ? report.reason : "?") + "\n");
  else print("HOSTILE " + name + " ok\n");
};
`)

	modules := map[string]string{}
	for key := range byUnit {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("block rows name unit %q, which the manifest does not declare", key)
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
			if blockUnitOfCase(rows, h.base) != key {
				continue
			}
			if h.null {
				fmt.Fprintf(&p, "hostileNull(T_%s, %q, %q);\n", key, h.name, h.root)
			} else {
				fmt.Fprintf(&p, "hostile(T_%s, %q, %q, %q, %d);\n", key, h.name, h.input, h.root, h.lead)
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
			t.Errorf("block %s: the Squirrel read does not match %s\n--- got ---\n%s--- want ---\n%s", r.key, r.dump, got, want)
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
		name, got := fields[1], fields[2]
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
	t.Logf("read %d block rows and %d named refusals", count, hostileCount)
}

func blockUnitOfCase(rows []blockRow, name string) string {
	for _, r := range rows {
		if r.key == name {
			return r.unit
		}
	}
	return ""
}
