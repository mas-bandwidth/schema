package pythontable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Proof-row files register their own constructors and value assertions here.
// The adapter is shared; no proof row needs a second interpreter harness.
var pythonFixedSetups = map[string]string{}
var pythonFixedChecks = map[string]string{}

type pythonFixedProofRunner struct{}

func (pythonFixedProofRunner) Name() string { return "python" }

func pythonFixedLineage(lineage map[string][]fixedproof.LineageEntry) map[string][]pythontable.FixedLineageEntry {
	out := map[string][]pythontable.FixedLineageEntry{}
	for name, entries := range lineage {
		for _, e := range entries {
			out[name] = append(out[name], pythontable.FixedLineageEntry{Wire: e.Wire, Layout: e.Layout, Record: e.Record, Retired: e.Retired, Reason: e.Reason, Writer: e.Writer})
		}
	}
	return out
}

func pythonFixedRun(t *testing.T, files map[string][]byte, script string, inputs map[string][]byte) (string, []byte) {
	t.Helper()
	var first string
	var saved []byte
	for index, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		in := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + script)}
		maps.Copy(in, inputs)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", in)
		if err != nil {
			t.Fatalf("fixed probe: %v\n%s\n%s", err, out, stderr)
		}
		value, readErr := os.ReadFile(filepath.Join(dir, "saved"))
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if index == 0 {
			first = string(out)
			saved = value
		} else if first != string(out) || string(saved) != string(value) {
			t.Fatalf("the interpreter pins disagree:\ncurrent %s\nfloor %s", first, out)
		}
	}
	return first, saved
}

func (pythonFixedProofRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	return u.Tables[root] != nil && ir.TableFixedEmitted(u, u.Tables[root])
}

func (pythonFixedProofRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	files, err := pythontable.GenerateLineage(u, pythonFixedLineage(lineage))
	if err != nil {
		return err.Error()
	}
	script := fmt.Sprintf("from %s._schema.fixedform import _ROOTS\nr = _ROOTS.get(%q)\nprint('no_fixed_root' if r is None else next((('missing_writer_metadata' if e.get('missing_writer_metadata') else e['refusal']) for e in r['known'].values() if e.get('missing_writer_metadata') or e['refusal'] and e['refusal'] != 'layout_unsupported'), ''))\n", u.Package, root)
	out, _ := pythonFixedRun(t, files, script, nil)
	return strings.TrimSpace(out)
}

const pythonFixedDump = `import json, struct

def dump(spec, value, prefix):
    shape = spec["shape"]
    if shape == "table":
        for field in spec["children"]:
            dump(field, getattr(value, field["name"]), prefix + "." + field["name"])
    elif shape == "optional":
        print("FIELD " + prefix + "_present=" + str(int(value is not None)))
        if value is not None:
            dump(spec["children"][0], value, prefix)
    elif shape in ("array", "keyed"):
        values = value._values if shape == "keyed" else value
        if spec.get("counted", False):
            print("FIELD " + prefix + "_count=" + str(len(values)))
        for i, item in enumerate(values):
            dump(spec["children"][0], item, prefix + "[" + str(i) + "]")
    elif shape == "union":
        print("FIELD " + prefix + ".type=" + str(value.type))
        if value.type:
            arm = spec["children"][value.type-1]
            if arm["shape"] != "void":
                dump(arm, getattr(value, arm["name"]), prefix + "." + arm["name"])
    elif shape == "string":
        print("FIELD " + prefix + "=" + json.dumps(value, ensure_ascii=False))
        print("FIELD " + prefix + "_length=" + str(len(value.encode("utf-8"))))
    elif shape == "bytes":
        print("FIELD " + prefix + "_length=" + str(len(value)))
        for i, item in enumerate(value):
            print("FIELD " + prefix + "[" + str(i) + "]=" + str(item))
    elif shape == "float":
        bits = int.from_bytes(struct.pack("<f" if spec["size"] == 4 else "<d", value), "little")
        print("FIELD " + prefix + "=" + ("0x%08X" if spec["size"] == 4 else "0x%016X") % bits)
    elif shape != "void":
        print("FIELD " + prefix + "=" + str(int(value)))
`

func (pythonFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	files, err := pythontable.GenerateLineage(u, pythonFixedLineage(lineage))
	if err != nil {
		return -1, err.Error(), nil, nil, fixedproof.LoadCounters{}
	}
	capArg := "4096"
	if capacity >= 0 {
		capArg = strconv.Itoa(capacity)
	}
	script := fmt.Sprintf(`from %[1]s import %[2]s, Refused, LoadReport
from %[1]s._schema.fixedform import _ROOTS
%[3]s
report = LoadReport()
try:
    with open("wire", "rb") as f:
        rows = %[2]s.fixed_load(f.read(), %[4]s, report)
except Refused as err:
    print("REFUSED " + err.reason)
else:
    print("OK", len(rows), report.widened)
    for i, row in enumerate(rows[:8]):
        dump(_ROOTS[%[2]q]["spec"], row, "r" + str(i))
    with open("saved", "wb") as f:
        f.write(%[2]s.fixed_save(rows))
`, u.Package, root, pythonFixedDump, capArg)
	out, saved := pythonFixedRun(t, files, script, map[string][]byte{"wire": wire})
	fields := map[string]string{}
	count := -1
	reason := ""
	var counters fixedproof.LoadCounters
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if value, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, value, fields, nil, fixedproof.LoadCounters{}
		}
		if value, ok := strings.CutPrefix(line, "OK "); ok {
			fmt.Sscanf(value, "%d %d", &count, &counters.Widened)
			reason = "ok"
		}
		if value, ok := strings.CutPrefix(line, "FIELD "); ok {
			if k, v, ok := strings.Cut(value, "="); ok {
				fields[k] = v
			}
		}
	}
	return count, reason, fields, saved, counters
}

func (pythonFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root, scenario string) ([]byte, string) {
	t.Helper()
	files, err := pythontable.Generate(u)
	if err != nil {
		return nil, err.Error()
	}
	setup := map[string]string{
		"optional_absent_zeros": "rows = [T()]\nrows[0].plain.a = 11\nrows[0].plain.b = 22\nrows[0].opt = None\n",
		"live_extents_base":     "rows = [T()]\nrows[0].arr = [1]\nrows[0].opt = None\nrows[0].tail = 77\n",
		"writing_slack_batch":   "rows = [T(), T()]\nrows[1].arr = [0x1122]\nrows[1].s = 'A'\nrows[1].b = b'B'\nrows[1].pick.type = 1\nrows[1].pick.narrow = 7\n",
		"writing_overcount":     "rows = [T()]\nrows[0].arr = [1,2,3,4]\n",
		"closure_small_42":      "rows = [T()]\nrows[0].x = 42\n",
		"outer_nested":          "rows = [T()]\nrows[0].head = 7\nrows[0].inner.a = 11\nrows[0].inner.b = 'one'\nrows[0].items[0].a = 10\nrows[0].items[0].b = 'hi'\nrows[0].items[1].a = 33\nrows[0].items[1].b = 'six'\nrows[0].maybe = type(rows[0].inner)()\nrows[0].maybe.a = 44\nrows[0].maybe.b = 'hi'\nrows[0].tail = 9\n",
		"w14_writer_offsets":    "rows = [T()]\nrows[0].a = 0xA1\nrows[0].b = 0x0B0B0B0B\nrows[0].inner.p = 0x1E1E1E1E\nrows[0].inner.q = 0x2E2E\nrows[0].opt = 0x0D0D0D0D\nrows[0].arr = [0x3E3E,0x4E4E]\nrows[0].s = 'seven'\nrows[0].tail = 0x7A7A7A7A\n",
	}[scenario]
	if scenario == "writing_slack_unzeroed" {
		setup = "rows = [T(), T()]\nrows[1].arr = [0x1122]\nrows[1].s = 'A'\nrows[1].b = b'B'\nrows[1].pick.type = 1\nrows[1].pick.narrow = 7\n"
		name := u.Package + "/_schema/fixedform.py"
		s := string(files[name])
		anchor := "image = bytearray(root[\"body\"])"
		if strings.Count(s, anchor) != 2 {
			t.Fatal("fixed image sabotage anchor changed")
		}
		files[name] = []byte(strings.Replace(s, anchor, "image = bytearray([0xAA]) * root[\"body\"]", 1))
	}
	if setup == "" {
		return nil, "unknown scenario " + scenario
	}
	script := fmt.Sprintf("from %s import %s as T, InvalidValue\n%s\ntry:\n    wire = T.fixed_save(rows)\nexcept InvalidValue as err:\n    print('REFUSED ' + err.rule)\nelse:\n    with open('saved','wb') as f:\n        f.write(wire)\n    print('OK')\n", u.Package, root, setup)
	out, saved := pythonFixedRun(t, files, script, nil)
	if reason, ok := strings.CutPrefix(strings.TrimSpace(out), "REFUSED "); ok {
		return nil, reason
	}
	return saved, ""
}

func (pythonFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	setup, ok := pythonFixedSetups[tc.ID]
	if !ok {
		t.Fatalf("no Python construction for proof row %s", tc.ID)
	}
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("from %s import *\nrows = [%s() for _ in range(%d)]\n%s\n%s\nwire = %s.fixed_save(rows)\nassert len(wire) == %s.fixed_measure(len(rows))\nwith open('saved','wb') as f:\n    f.write(wire)\n", u.Package, tc.Root, tc.Count, setup, pythonFixedChecks[tc.ID], tc.Root, tc.Root)
	_, saved := pythonFixedRun(t, files, script, nil)
	return saved
}
func (r pythonFixedProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	n, reason, _, saved, _ := r.LoadLineage(t, u, tc.Root, nil, wire, tc.Count)
	if n != tc.Count {
		return nil, fmt.Errorf("%s: %s", tc.ID, reason)
	}
	return saved, nil
}
func (r pythonFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	files, err := pythontable.Generate(u)
	if err != nil {
		return nil, err
	}
	setup, ok := pythonFixedSetups[tc.ID]
	if !ok {
		return nil, fmt.Errorf("no Python construction for %s", tc.ID)
	}
	name := u.Package + "/_schema/fixedform.py"
	s := string(files[name])
	anchor := "records.append(root[\"prefix\"] + image)"
	if strings.Count(s, anchor) != 1 {
		return nil, fmt.Errorf("fixed writer sabotage anchor changed")
	}
	files[name] = []byte(strings.Replace(s, anchor, "image[0] ^= 1\n        "+anchor, 1))
	script := fmt.Sprintf("from %s import *\nrows = [%s() for _ in range(%d)]\n%s\nwith open('saved','wb') as f:\n    f.write(%s.fixed_save(rows))\n", u.Package, tc.Root, tc.Count, setup, tc.Root)
	_, saved := pythonFixedRun(t, files, script, nil)
	return saved, nil
}
