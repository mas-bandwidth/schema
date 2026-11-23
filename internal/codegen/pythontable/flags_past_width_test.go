package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestPythonFlagsPastWidthRefuse holds a flags mask to its reader's width W
// (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the newer
// reader reads old data, and never the other way. A newer build's variable
// file with its appended bit set, a narrower unsigned field widened into the
// mask with a bit past W, and a fixed record with a stored bit past W each
// refuse the whole load out_of_range; the twins inside W read. The variable
// and fixed savers raise InvalidValue for a mask past W, so a same-build round
// trip never writes what its own reader refuses. Under the old rule (a mask's
// range was the u64's own) every refusal here read.
func TestPythonFlagsPastWidthRefuse(t *testing.T) {
	slowtest.Gate(t, "a flags mask past the reader's width, both pins")
	reader := loadUnit(t, "package flagspast\nflags Cap { Trace, Verbose }\ntable Root { cap Cap }\n")
	newer := loadUnit(t, "package flagspast\nflags Cap { Trace, Verbose, Extra }\ntable Root { cap Cap }\n")
	narrow := loadUnit(t, "package flagspast\ntable Root { cap uint8 }\n")
	fixed := loadUnit(t, "package flagspastfixed\nflags Cap { Trace, Verbose }\nfixed table Root { cap Cap }\n")
	inputs := map[string][]byte{
		"past":       wideEngineWire(t, newer, "Root", `{"cap":["Trace","Extra"]}`),
		"inside":     wideEngineWire(t, newer, "Root", `{"cap":["Trace","Verbose"]}`),
		"widepast":   wideEngineWire(t, narrow, "Root", `{"cap":5}`),
		"wideinside": wideEngineWire(t, narrow, "Root", `{"cap":3}`),
	}
	for _, tc := range []struct {
		name  string
		probe string
		files map[string][]byte
	}{{"variable", flagsPastVariableProbe, generateOrFail(t, reader)}, {"fixed", flagsPastFixedProbe, generateOrFail(t, fixed)}} {
		dir := t.TempDir()
		writeTree(t, dir, tc.files)
		inputs["main.py"] = []byte(tc.probe)
		for _, out := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
			if strings.TrimSpace(string(out)) != "OK" {
				t.Errorf("%s: %q", tc.name, out)
			}
		}
	}
}

func generateOrFail(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

const flagsPastVariableProbe = pyrun.ImportPrelude + `from flagspast import Root, Refused, InvalidValue, LoadReport
def load(name):
    with open(name, "rb") as f:
        return Root.load(f.read(), LoadReport())
for name in ("past", "widepast"):
    try:
        load(name)
    except Refused as e:
        assert e.reason == "out_of_range", (name, e.reason)
    else:
        raise AssertionError(name + " read a mask bit past W")
assert load("inside").cap == 3 and load("wideinside").cap == 3
r = Root()
r.cap = 5
try:
    r.save()
except InvalidValue:
    pass
else:
    raise AssertionError("the variable save wrote a mask bit past W")
print("OK")
`

const flagsPastFixedProbe = pyrun.ImportPrelude + `from flagspastfixed import Root, Refused, InvalidValue
def image(mask):
    r = Root()
    r.cap = mask
    return Root.fixed_save([r])
low, both = image(1), image(3)
at = [i for i in range(len(low)) if low[i] != both[i]]
assert len(low) == len(both) and len(at) == 1, at
forged = bytearray(low)
forged[at[0]] |= 4
try:
    Root.fixed_load(bytes(forged))
except Refused as e:
    assert e.reason == "out_of_range", e.reason
else:
    raise AssertionError("the fixed load read a mask bit past W")
assert Root.fixed_load(both)[0].cap == 3
r = Root()
r.cap = 5
try:
    Root.fixed_save([r])
except InvalidValue:
    pass
else:
    raise AssertionError("the fixed save wrote a mask bit past W")
print("OK")
`
