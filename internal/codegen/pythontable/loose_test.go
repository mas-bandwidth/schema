package pythontable

import (
	"maps"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// looseSchema is a unit with a table beside a packet type and a packet union
// that no table reaches. Nothing the unit declares may be dropped: Loose and
// LooseChoice are generated exactly as the packet leg emits them for a
// packet-only unit, beside the table classes.
const looseSchema = `package loosevalues

type Leaf
{
    value int32
}

type Loose
{
    a uint8
    b bool
    c uint16 = 10 | min = 10, max = 300
}

union LooseChoice
{
    nothing
    loose Loose
}

table Holder
{
    leaf Leaf
}
`

// looseA, looseB and looseC are the probe's Loose value.
const (
	looseA = 0xA5
	looseB = true
	looseC = 100
)

// TestPythonTablePacketTypesLooseDeclarations holds that a packet type and a
// packet union no table reaches are generated in a unit with tables, and that
// each reads the Go reference's packet wire and writes it back byte-identical
// under both pinned interpreters, with a short read refused. The reference is
// built in Go from the IR by the packet wire's own rules (a field as
// value - min in bits_required(min, max) bits, a bool as one bit, a union as
// its tag in bits_required(0, arms) bits then the selected arm;
// docs/PACKET-WIRE-ALGORITHM.md §2), so the test is not a copy of the emitter.
func TestPythonTablePacketTypesLooseDeclarations(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, looseSchema)
	closure := ir.TableClosure(u)
	if closure["Loose"] || closureUnions(u)["LooseChoice"] {
		t.Fatal("the construct is wrong: a table reaches Loose or LooseChoice, so the test proves nothing about a loose declaration")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refused a unit whose loose packet type uses carried kinds: %v", err)
	}
	module := string(files["loosevalues/Core.py"])
	for _, class := range []string{"class Loose:", "class LooseChoice:", "class LooseChoiceType:"} {
		if !strings.Contains(module, class+"\n") {
			t.Errorf("the generated module drops a loose declaration: %q is absent", class)
		}
	}
	if strings.Contains(module, "Loose.descriptor = ") || strings.Contains(module, "LooseChoice.descriptor = ") {
		t.Error("a loose declaration carries a table descriptor, but no table reaches it")
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	looseWire, choiceWire := looseReference(t, u)
	inputs := map[string][]byte{
		"main.py":    []byte(looseProbe),
		"loose.bin":  looseWire,
		"choice.bin": choiceWire,
	}
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	gotFloor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != gotFloor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, gotFloor)
	}
	for _, line := range []string{
		"loose 165 True 100",
		"choice 2 165 True 100",
		"loose short refused",
		"choice short refused",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	for _, path := range []string{"loosevalues", "loosevalues/_schema"} {
		if out, err := pyrun.Analyze(t, dir, path); err != nil {
			t.Fatalf("mypy --strict over %s:\n%s", path, out)
		}
	}
}

// TestPythonTablePacketTypesLooseNegativeControl plants the fault in a COPY of
// the generated module: the loose type's writer moves c by one, and the
// probe's byte compare against the Go reference must go red by name.
func TestPythonTablePacketTypesLooseNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, looseSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const path = "loosevalues/Core.py"
	text := string(files[path])
	const anchor = "        _s_ri.value = self.c\n"
	if strings.Count(text, anchor) != 1 {
		t.Fatalf("the negative control's anchor %q is not exactly once in %s", anchor, path)
	}
	sabotaged := make(map[string][]byte, len(files))
	maps.Copy(sabotaged, files)
	sabotaged[path] = []byte(strings.Replace(text, anchor, "        _s_ri.value = self.c + 1\n", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	looseWire, choiceWire := looseReference(t, u)
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py":    []byte(looseProbe),
		"loose.bin":  looseWire,
		"choice.bin": choiceWire,
	})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving the loose type's written field left the probe green")
	}
	if !strings.Contains(stderr, "loose wire differs") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name \"loose wire differs\":\n%s", stderr)
	}
}

const looseProbe = pyrun.ImportPrelude + `import loosevalues
from loosevalues.Core import Loose, LooseChoice, LooseChoiceType

with open("loose.bin", "rb") as handle:
    loose_data = handle.read()
with open("choice.bin", "rb") as handle:
    choice_data = handle.read()

value = Loose.read(loose_data)
print("loose", value.a, value.b, value.c)
if value.write() != loose_data:
    raise SystemExit("loose wire differs: " + value.write().hex())
fresh = Loose()
fresh.a = value.a
fresh.b = value.b
fresh.c = value.c
if fresh.write() != loose_data:
    raise SystemExit("loose wire differs on a constructed value: " + fresh.write().hex())

choice = LooseChoice.read(choice_data)
if choice.type != LooseChoiceType.Loose:
    raise SystemExit("the choice read the wrong arm: " + repr(choice))
print("choice", choice.type, choice.loose.a, choice.loose.b, choice.loose.c)
if choice.write() != choice_data:
    raise SystemExit("choice wire differs: " + choice.write().hex())

try:
    Loose.read(loose_data[:-1])
    print("loose short read")
except loosevalues.Refused:
    print("loose short refused")
try:
    LooseChoice.read(choice_data[:-1])
    print("choice short read")
except loosevalues.Refused:
    print("choice short refused")
print("OK")
`

// looseReference is the Go reference's packet wire for the probe's Loose value
// alone and inside LooseChoice's loose arm.
func looseReference(t *testing.T, u *ir.Unit) (loose, choice []byte) {
	t.Helper()
	st := u.Structs["Loose"]
	un := u.Unions["LooseChoice"]
	if st == nil || un == nil {
		t.Fatal("the unit lacks Loose or LooseChoice")
	}
	values := map[string]int64{"a": looseA, "c": looseC}
	if looseB {
		values["b"] = 1
	}
	writeLoose := func(w *refBits) {
		for _, f := range st.Fields {
			v := big.NewInt(values[f.Name])
			switch {
			case f.Type.Kind == ir.TBool:
				w.put(v, 1)
			case f.Type.Kind == ir.TInt && f.HasIntRange:
				w.put(new(big.Int).Sub(v, f.IntMin), int(ir.BitsRequired(f.IntMin, f.IntMax)))
			case f.Type.Kind == ir.TInt && !f.Type.Signed:
				w.put(v, f.Type.Width)
			default:
				t.Fatalf("the reference does not encode Loose.%s", f.Name)
			}
		}
	}
	var lw refBits
	writeLoose(&lw)
	arm := -1
	for i, v := range un.Variants {
		if v.Name == "loose" {
			arm = i + 1
		}
	}
	if arm < 0 {
		t.Fatal("LooseChoice has no loose arm")
	}
	var cw refBits
	cw.put(big.NewInt(int64(arm)), int(ir.BitsRequired(big.NewInt(0), big.NewInt(int64(len(un.Variants))))))
	writeLoose(&cw)
	return lw.bytes(), cw.bytes()
}

// refBits is the packet wire's bit stream: bit i lives in byte i/8 at bit
// position i%8, low bit first, and the stream ends at the next whole byte.
type refBits struct {
	b []byte
	n int
}

func (w *refBits) put(v *big.Int, width int) {
	for i := range width {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v.Bit(i) == 1 {
			w.b[w.n/8] |= 1 << (w.n % 8)
		}
		w.n++
	}
}

func (w *refBits) bytes() []byte { return w.b }
