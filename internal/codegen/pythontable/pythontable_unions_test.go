package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unionsSchema = `package unions

table Leaf
{
    value int32
    flag  bool
}

table Other
{
    label int32
}

table Plain
{
    value int32
    flag  bool
}

union Pick
{
    leaf   Leaf
    other  Other
    number int32
    toggle bool
    next   *Leaf
    ping
}

union Inner
{
    pick  Pick
    count uint16
}

table Root
{
    pick  Pick
    inner Inner
    trail [2]Pick
    many  [..3]Pick
    chain Pick
    plain Plain
}
`

const unionsText = `{
  "pick": {"leaf": {"value": 7, "flag": true}},
  "inner": {"pick": {"number": -5}},
  "trail": [{"leaf": {"value": 1, "flag": false}}, {"ping": null}],
  "many": [{"other": {"label": 9}}],
  "chain": {"next": {"value": 42, "flag": false}},
  "plain": {"value": 11, "flag": true}
}`

// unionCanonical is the Go engine's canonical wire for unionsText: the probe
// writes the same value back and must land on these bytes exactly.
func unionCanonical(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(unionsText), &report) || !report.Silent() {
		t.Fatalf("the canonical text did not place cleanly: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the engine refused the canonical value: %v", err)
	}
	return wire
}

const unionsProbe = `from unions import Inner, InvalidValue, Leaf, Other, Pick, Plain, Refused, Root

def arm(cls, name):
    for i, a in enumerate(cls.descriptor.arms, 1):
        if a.name == name:
            return i
    raise SystemExit("union_arm_missing")

r = Root()
leaf = Leaf()
leaf.value = 7
leaf.flag = True
p = r.pick
p.type = arm(Pick, "leaf")
p.leaf = leaf
i = r.inner
i.type = arm(Inner, "pick")
i.pick.type = arm(Pick, "number")
i.pick.number = -5
t0 = r.trail[0]
t0.type = arm(Pick, "leaf")
t0.leaf.value = 1
t0.leaf.flag = False
t1 = r.trail[1]
t1.type = arm(Pick, "ping")
m0 = Pick()
m0.type = arm(Pick, "other")
m0.other = Other()
m0.other.label = 9
r.many = [m0]
chain = Pick()
chain.type = arm(Pick, "next")
chain.next = Leaf()
chain.next.value = 42
chain.next.flag = False
r.chain = chain
r.plain = Plain()
r.plain.value = 11
r.plain.flag = True

data = r.save()
with open("canonical.bin", "rb") as handle:
    want = handle.read()
if data != want:
    raise SystemExit("union_canonical_bytes " + data.hex() + " " + want.hex())
back = Root.load(data)
if back != r:
    raise SystemExit("union_roundtrip_value")
if back.save() != data:
    raise SystemExit("union_roundtrip_bytes")
if Root.load(want).save() != want:
    raise SystemExit("union_canonical_read_back")
if back.trail[1].type != arm(Pick, "ping"):
    raise SystemExit("union_payload_free")
if back.chain.next is None or back.chain.next.value != 42:
    raise SystemExit("union_pointer_arm")
print("union_measure", r.measure() == len(data))

bad_type = len(Pick.descriptor.arms) + 1
bad = Pick()
bad.type = bad_type
r.pick = bad
try:
    r.save()
    print("union_refuse", "wrote")
except InvalidValue:
    print("union_refuse", "type", r.pick.type == bad_type, r.pick.next is None)
r.pick = p

try:
    Root.load(data[:-1])
    print("union_load", "read")
except Refused:
    print("union_load", "refused")

try:
    Root.load(data + b"\x00")
    print("union_trailing", "read")
except Refused:
    print("union_trailing", "refused")

print("OK")
`

func TestPythonVtUnions(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, unionsSchema)
	canonical := unionCanonical(t, u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "canonical.bin"), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + unionsProbe
	got := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	gotFloor := string(pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	if got != gotFloor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, gotFloor)
	}
	if out, err := pyrun.Analyze(t, dir, "unions"); err != nil {
		t.Fatalf("mypy --strict over the generated union package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "unions/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
	for _, line := range []string{
		"union_measure True",
		"union_refuse type True True",
		"union_load refused",
		"union_trailing refused",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
}

// TestPythonVtUnionsNegativeControl moves an arm's length by one in a COPY of
// the generated runtime; the probed canonical write must go red by name.
func TestPythonVtUnionsNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, unionsSchema)
	canonical := unionCanonical(t, u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := make(map[string][]byte, len(files))
	for name, data := range files {
		sabotaged[name] = data
	}
	const path = "unions/_schema/kind_union.py"
	text := string(sabotaged[path])
	const anchor = "w.varint(len(body))"
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "w.varint(len(body) + 1)", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	if err := os.WriteFile(filepath.Join(dir, "canonical.bin"), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + unionsProbe)})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving an arm's length left the probe green")
	}
	const named = "union_canonical_bytes"
	if !strings.Contains(stderr, named) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name %q:\n%s", named, stderr)
	}
}
