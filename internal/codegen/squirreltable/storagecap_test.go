package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or record of it costs two wire bytes,
// so 8191 of them measure under the 2^40-byte cap and 8192 measure over it.
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

// storageCapProbe measures every vector, and reads each over vector through
// ReadStrict and the tolerant load. Big's constructor throws, so a read that
// builds even one Big before it refuses fails by name instead of building
// 2^24-element arrays. The Huge probes (hugeCount empty bodies of a record
// just under 2^40 bytes, as a list and as node records) hold the term check
// and the running node total: either sum is past 2^63, which wraps a Squirrel
// integer, so only a check made before the sum answers storage_over_cap.
const storageCapProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("cap/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local R = schema.require("schema_table_refusal");
foreach (c in ["list_at", "list_over", "nodes_at", "nodes_over"]) {
  local rep = { reason = null };
  local n = L.LoadMeasure(T, "Root", inputs[c], rep);
  print(c + ".measure=" + n + "," + rep.reason + "\n");
  if (c.find("_over") == null) continue;
  local strict = null;
  try {
    local report = {};
    local v = R.ReadStrict(T, "Root", inputs[c], report);
    strict = v == null ? report.reason : "read";
  } catch (e) { strict = e; }
  print(c + ".strict=" + strict + "\n");
  local tolerant = null;
  try {
    local report = L.report(null);
    local v = L.loadTolerant(T, "Root", inputs[c], report);
    tolerant = (v == null) + "," + (("reason" in report) ? report.reason : null) + "," + report.refused + "," + report.malformed;
  } catch (e) { tolerant = e; }
  print(c + ".tolerant=" + tolerant + "\n");
}
foreach (c in ["huge_list", "huge_nodes"]) {
  local rep = { reason = null };
  local n = L.LoadMeasure(T, "Holder", inputs[c], rep);
  print(c + ".measure=" + n + "," + rep.reason + "\n");
}
`

// hugeCount Huge bodies (2^40 - 2^27 bytes each) command just past 2^63
// bytes.
const hugeCount = 1<<23 + 1<<11

// storageCapHugeWire is a form-1 Holder holding n empty Huge bodies, as its
// items list or as node records.
func storageCapHugeWire(u *ir.Unit, nodes bool, n uint64) []byte {
	var body []byte
	var id uint64
	kind := byte(ir.TableKindArray)
	if nodes {
		body = binary.AppendUvarint(nil, n)
		for range n {
			body = append(body, 2, 1, 0)
		}
		id, kind = math.MaxUint64, byte(ir.TableKindString)
	} else {
		body = binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, n)
		for range n {
			body = append(body, 1, 0)
		}
		id = ir.TableFieldWireId(u.Tables["Holder"].Fields[0])
	}
	wire := []byte{1, 1, kind}
	wire = binary.AppendUvarint(wire, uint64(len(body)))
	wire = append(wire, body...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, id)
	count := uint64(1)
	if nodes {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(u.Tables["Huge"].WireName()))
		count = 2
	}
	return binary.LittleEndian.AppendUint64(wire, count)
}

// storageCapRun generates the unit, plants the throwing Big constructor, and
// runs the probe with patch applied to the emitted load runtime (nil for
// none).
func storageCapRun(t *testing.T, patch func(string) string) map[string]string {
	t.Helper()
	repo, err := filepath.Abs(storageCapDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repo, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadSource(t, string(src)+"\nfixed table Huge\n{\n    a [8191]Big\n}\n\ntable Holder\n{\n    items []Huge\n    next  *Huge\n}\n")
	dir := t.TempDir()
	mod := emitUnit(t, dir, "cap", u)
	modPath := filepath.Join(dir, "cap", mod+".nut")
	text, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatal(err)
	}
	const ctor = "    this.A = array(16777216);\n"
	if strings.Count(string(text), ctor) != 1 {
		t.Fatalf("the emitted module has no single %q", ctor)
	}
	writeFile(t, modPath, []byte(strings.Replace(string(text), ctor, "    throw \"built a Big\";\n"+ctor, 1)))
	if patch != nil {
		loadPath := filepath.Join(dir, "cap", "schema_table_load.nut")
		text, err := os.ReadFile(loadPath)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, loadPath, []byte(patch(string(text))))
	}
	inputs := map[string][]byte{
		"huge_list":  storageCapHugeWire(u, false, hugeCount),
		"huge_nodes": storageCapHugeWire(u, true, hugeCount),
	}
	for _, c := range []string{"list_at", "list_over", "nodes_at", "nodes_over"} {
		data, err := os.ReadFile(filepath.Join(repo, c+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c] = data
	}
	writeFile(t, filepath.Join(dir, "main.nut"), fmt.Appendf(nil, storageCapProbe, mod))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	return got
}

// storageCapFailures answers what the probe's answers break of §11: an at
// vector measures in (0, 2^40], and every over vector and both Huge probes
// refuse by name, the reads without building a Big.
func storageCapFailures(got map[string]string) []string {
	var bad []string
	for _, c := range []string{"list_at", "nodes_at"} {
		size, why, _ := strings.Cut(got[c+".measure"], ",")
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil || n <= 0 || n > ir.MaxSizeBytes || why != "null" {
			bad = append(bad, c+" measure "+got[c+".measure"])
		}
	}
	for _, c := range []string{"list_over", "nodes_over"} {
		want := map[string]string{
			".measure":  "-1,storage_over_cap",
			".strict":   "storage_over_cap",
			".tolerant": "true,storage_over_cap,true,false",
		}
		for k, w := range want {
			if got[c+k] != w {
				bad = append(bad, fmt.Sprintf("%s%s: got %q, want %q", c, k, got[c+k], w))
			}
		}
	}
	for _, c := range []string{"huge_list", "huge_nodes"} {
		if got[c+".measure"] != "-1,storage_over_cap" {
			bad = append(bad, c+" measure "+got[c+".measure"])
		}
	}
	return bad
}

// TestSquirrelStorageCapRefusedAtMeasure holds the storage cap at load
// (docs/SPEC-TABLES.md §11, §6.5) on the shared vectors: LoadMeasure answers
// a size for each at vector and -1 with the reason storage_over_cap for each
// over vector, and ReadStrict and the tolerant load refuse each over vector
// by name before building anything.
func TestSquirrelStorageCapRefusedAtMeasure(t *testing.T) {
	t.Parallel()
	if bad := storageCapFailures(storageCapRun(t, nil)); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestSquirrelStorageCapNegativeControl removes each check from a copy of
// the emitted load runtime and requires the probe to go red: the extent term,
// the running node total, the final total, and the load's guard.
func TestSquirrelStorageCapNegativeControl(t *testing.T) {
	t.Parallel()
	plants := []struct{ name, from, to string }{
		{"term", "if (count > 0 && size > 0 && count > (STORAGE_CAP - start) / size) {", "if (false) {"},
		{"node total", "    if (total > STORAGE_CAP) return MEASURE_OVER_CAP;\n", ""},
		{"final total", "return total > STORAGE_CAP ? MEASURE_OVER_CAP : total;", "return total;"},
		{"load guard", "regionMeasure(T, c, bytes) == MEASURE_OVER_CAP) {", "false) {"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			got := storageCapRun(t, func(src string) string {
				if strings.Count(src, p.from) != 1 {
					t.Fatalf("the plant's anchor is not once in the load runtime: %q", p.from)
				}
				return strings.Replace(src, p.from, p.to, 1)
			})
			if len(storageCapFailures(got)) == 0 {
				t.Fatalf("the load runtime with the %s check removed still holds the cap", p.name)
			}
		})
	}
}
