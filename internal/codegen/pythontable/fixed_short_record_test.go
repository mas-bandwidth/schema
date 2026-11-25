package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// A PLAN THAT READS PAST A SHORT RECORD REFUSES BY NAME, IN EVERY BUILD.
//
// THE LAW: docs/FIXED-FORM-ALGORITHM.md §4.2 and §5.2 PLAN (fix 3) — every
// compiled entry is bounded by the WRITER'S OWN DECLARED RECORD, and an entry
// reaching past it is `layout_record_too_large`. Python is a managed leg, so it
// refuses in every build, and nothing fails silently.
//
// THE DEFECT, until 2026-11-25: _schema/fixedform.py read the record through
// slices, and a slice past the end of a bytes object is SHORT, not an error. An
// integer read there (_integer, so a guard, a widen, an ordinal) came back 0, a
// copy, count or text op landed FEWER bytes and shrank the image under every
// field behind it, and a dead span past the end read as all zero. A plan that
// reached past the record answered a wrong value with no refusal.
//
// NO LAWFUL PLAN REACHES THAT PATH: compilePythonFixedPlan bounds every entry
// at generate time. So the test plants one, the way R14 forges a lock layout:
// (1) every op kind and a guard, run through _apply / _guarded over a 4-byte
// record body, each reaching past it; (2) the production load, fixed_load and
// fixed_load_into, over a file this build wrote, with the plan of its own
// lineage entry swapped for one forged op that reads two bytes past the record.
//
// THE PLANT: delete the _within call from _integer and _apply, and every case
// below either reads (a 0, a short copy) or reads a wrong value through
// fixed_load: red by name.
func TestPythonFixedShortRecordRefusesByName(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "package rshort\nfixed table T { a uint32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from rshort import T, Refused

from rshort._schema import fixedform as ff

WANT = "layout_record_too_large"
BODY = memoryview(bytes([1, 2, 3, 4]))

def refuses(name, run):
    try:
        run()
    except Refused as e:
        if e.reason != WANT:
            raise AssertionError("short_%s refused %s, want %s" % (name, e.reason, WANT))
    else:
        raise AssertionError("short_%s read past the record with no refusal" % name)

# 1. EVERY OP KIND, each reaching past a 4-byte record body. Nothing is written.
OPS = {
    "copy": {"op": "copy", "src": 2, "dst": 0, "size": 4, "guards": []},
    "widen": {"op": "widen", "src": 2, "dst": 0, "size": 4, "width": 8, "signed": False, "guards": []},
    "widenf": {"op": "widenf", "src": 2, "dst": 0, "size": 4, "width": 8, "guards": []},
    "count": {"op": "count", "src": 2, "dst": 0, "size": 4, "guards": []},
    "text": {"op": "text", "src": 0, "dst": 0, "size": 4, "guards": []},
    "ordinal": {"op": "ordinal", "src": 3, "dst": 0, "size": 2, "width": 2, "remap": [0, 7], "guards": []},
}
for name, op in OPS.items():
    image = bytearray(16)
    refuses(name, lambda: ff._apply(op, BODY, image))
    if len(image) != 16 or any(image):
        raise AssertionError("short_%s wrote the image before it refused" % name)
refuses("guard", lambda: ff._guarded({"guards": [[3, 2, 0, "eq"]]}, BODY))
# an ALL-ZERO dead span running past the end read as dead and passed
refuses("dead", lambda: ff._dead(memoryview(bytes(4)), 2, 6))
refuses("bool", lambda: ff._bool(BODY, 4))
# a const reads no record byte, and an op inside the record still reads
image = bytearray(8)
ff._apply({"op": "const", "src": 0, "dst": 0, "size": 8, "value": 5, "guards": []}, BODY, image)
ff._apply({"op": "copy", "src": 0, "dst": 0, "size": 4, "guards": []}, BODY, image)
if bytes(image) != bytes([1, 2, 3, 4, 0, 0, 0, 0]):
    raise AssertionError("short_control %s" % bytes(image).hex())

# 2. THE LOAD: this build's own file, its own lineage entry's plan forged.
v = T()
v.a = 0x11223344
data = T.fixed_save([v])
if T.fixed_load(data)[0].a != 0x11223344:
    raise AssertionError("short_load_control")
root = ff._ROOTS["T"]
entry = root["known"][root["hash"]]
entry["direct"] = False
entry["plan"] = [{"op": "copy", "src": 2, "dst": 0, "size": 4, "guards": []}]
entry["split"] = 1
refuses("load", lambda: T.fixed_load(data))
targets = [T()]
report = ff.FixedLoadReport()
if T.fixed_load_into(targets, data, report=report) != 0 or not report.refused or report.reason != WANT:
    raise AssertionError("short_load_into %r %r" % (report.refused, report.reason))
print("short ok")
`
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "short ok" {
			t.Fatalf("a plan past a short record on the %s pin: %v\n%s\n%s", p.name, err, out, stderr)
		}
	}
}
