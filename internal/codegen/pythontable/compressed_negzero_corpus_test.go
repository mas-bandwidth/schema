package pythontable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE PYTHON LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Python package, under both pinned interpreters: load
// <base>.json with the Python text read, set <path> to -0 in the native value,
// save <form>, and owe <expect>.<form>.bin byte for byte.
//
// Beside each row, every other Python writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable writer (every
// root's), the root's save(), the retaining save (a variable root's), the
// fixed save and the message form (a fixed table's), the JSON writer and the
// cook writer. A compressed row's -0
// output equals its twin's; the plain `p` rows are the control, whose -0 rides
// bit-exact, so their output differs from the twin's.
//
// It runs the pinned interpreters, so it is a slow test; the conformance lane
// runs it (make/python-compressed-negzero.mk). TestPythonCompressedFloatStoresAreGuarded
// is its fast twin.

const pythonNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// pythonNegZeroNoWriter names the corpus rows whose <form> writer the Python
// surface does not have, and why. The row's root still answers every twin
// its writers owe.
// A fixed table's save() writes the fixed form, but the runtime's variable
// writer (_schema.save.save_table) takes any root, so every row is carried and
// the map is empty.
var pythonNegZeroNoWriter = map[string]string{}

type pythonNegZeroRow struct {
	name, form, root, base, path, expect string
}

func pythonNegZeroRows(t *testing.T) []pythonNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(pythonNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []pythonNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, pythonNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

const pythonNegZeroProbe = `
import math, re, struct
from nzgo import Fix, Var, Level, LoadReport, TableRetain, save_messages
from nzgo._schema.cookwrite import cook
from nzgo._schema.save import save_table

# -0 from its bits, so nothing can hand back +0
NZ = struct.unpack('<f', b'\x00\x00\x00\x80')[0]
assert NZ == 0 and math.copysign(1.0, NZ) < 0, "the probe's -0 is not -0"
ROOTS = {'Var': Var, 'Fix': Fix}

def read(name):
    with open(name, 'rb') as f:
        return f.read()

# set writes -0 into the cell a corpus path names: field, field[index],
# field[Variant] for an enum-keyed slot, or field.arm for a union's set arm
def put(v, path):
    m = re.fullmatch(r'(\w+)(?:\[(\w+)\]|\.(\w+))?', path)
    assert m is not None, path
    name, index, arm = m.groups()
    if index is None and arm is None:
        setattr(v, name, NZ)
    elif arm is not None:
        u = getattr(v, name)
        assert u.type != 0, 'the base does not set arm ' + arm
        setattr(u, arm, NZ)
    elif index.isdigit():
        getattr(v, name)[int(index)] = NZ
    else:
        getattr(v, name)[getattr(Level, index)] = NZ

def load(root, base):
    return ROOTS[root].from_json(read(base + '.json').decode()), None

def load_retain(root, base):
    # the retaining save runs over the value its own load answered and the
    # store that load kept (§6.6): the store belongs to that value
    store = TableRetain(bytearray(65536), bytearray(24 * 64))
    return Var.load_retain(read(base + '.variable.bin'), store, LoadReport()), store

# each writer the root has: how its value is loaded, and the save
def writers(root):
    out = [
        ('json', load, lambda v, s: v.to_json().encode()),
        ('cook', load, lambda v, s: cook(v)),
        # the variable form, a fixed root's too
        ('variable', load, lambda v, s: save_table(v)),
    ]
    if root == 'Var':
        out.append(('save', load, lambda v, s: v.save()))
        out.append(('retain', load_retain, lambda v, s: v.save_retain(s, LoadReport())))
    else:
        # save() of a fixed table writes the fixed form
        out.append(('save', load, lambda v, s: v.save()))
        out.append(('fixed', load, lambda v, s: Fix.fixed_save([v])))
        out.append(('message', load, lambda v, s: save_messages(Fix, [v])))
    return out

def run(w, loaded):
    try:
        return bytes(w(*loaded)).hex()
    except Exception as e:
        return 'error:' + type(e).__name__

for name, form, root, base, path in ROWS:
    for wname, loader, w in writers(root):
        plus = loader(root, base)
        minus = loader(root, base)
        put(minus[0], path)
        print(name, wname, run(w, minus), run(w, plus))
`

// TestPythonCompressedNegZeroCorpus runs the corpus on the Python leg.
func TestPythonCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join(pythonNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnit(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}

	rows := pythonNegZeroRows(t)
	inputs := map[string][]byte{}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(pythonNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var py strings.Builder
	py.WriteString(pyrun.ImportPrelude)
	py.WriteString("ROWS = [\n")
	for _, r := range rows {
		inputs[r.base+".json"] = read(r.base + ".json")
		if r.root == "Var" {
			inputs[r.base+".variable.bin"] = read(r.base + ".variable.bin")
		}
		fmt.Fprintf(&py, "    (%q, %q, %q, %q, %q),\n", r.name, r.form, r.root, r.base, r.path)
	}
	py.WriteString("]\n")
	py.WriteString(pythonNegZeroProbe)
	inputs["main.py"] = []byte(py.String())

	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, pin := range []struct {
		name string
		run  func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		out := pin.run(t, dir, "main.py", inputs)
		pythonNegZeroHold(t, pin.name, rows, read, out)
	}
}

func pythonNegZeroHold(t *testing.T, pin string, rows []pythonNegZeroRow, read func(string) []byte, out []byte) {
	t.Helper()
	type pair struct{ minus, plus string }
	got := map[string]map[string]pair{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("%s: the probe printed %q, not a row, a writer and two outputs", pin, line)
		}
		if got[f[0]] == nil {
			got[f[0]] = map[string]pair{}
		}
		got[f[0]][f[1]] = pair{f[2], f[3]}
	}

	held, twins, exempt := 0, 0, 0
	for _, r := range rows {
		ws := got[r.name]
		if ws == nil {
			t.Errorf("%s: %s: the probe did not answer the row", pin, r.name)
			continue
		}
		plain := r.path == "p"
		// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin
		want := hex.EncodeToString(read(r.expect + "." + r.form + ".bin"))
		if why, ok := pythonNegZeroNoWriter[r.name]; ok {
			// held by name, never in silence: the row's writer must still be
			// absent, and its root's other writers still answer its twins
			if _, has := ws[r.form]; has {
				t.Errorf("%s: %s: the Python leg now has a %s writer for %s; drop the exemption (%s)", pin, r.name, r.form, r.root, why)
			}
			t.Logf("%s: %s: not carried on the Python leg: %s", pin, r.name, why)
			exempt++
		} else if p, ok := ws[r.form]; !ok {
			t.Errorf("%s: %s: the Python leg has no %s writer for %s", pin, r.name, r.form, r.root)
		} else if p.minus != want {
			t.Errorf("%s: %s: the Python %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", pin, r.name, r.form, r.path, r.expect, r.form, p.minus, want)
		} else {
			held++
		}
		// THE TWINS: every writer the root has
		for w, p := range ws {
			if strings.HasPrefix(p.minus, "error") || strings.HasPrefix(p.plus, "error") {
				t.Errorf("%s: %s/%s: a writer refused: -0 %s, +0 %s", pin, r.name, w, p.minus, p.plus)
				continue
			}
			switch {
			case plain && p.minus == p.plus:
				t.Errorf("%s: %s/%s: a plain float's -0 did not ride: %s", pin, r.name, w, p.minus)
			case !plain && p.minus != p.plus:
				t.Errorf("%s: %s/%s: a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", pin, r.name, w, p.minus, p.plus)
			default:
				twins++
			}
		}
	}
	if len(rows) != 15 {
		t.Errorf("cases.tsv has %d rows, want 15", len(rows))
	}
	if held+exempt != len(rows) || exempt != len(pythonNegZeroNoWriter) {
		t.Errorf("%s: %d rows held and %d exempt of %d; %d exemptions named", pin, held, exempt, len(rows), len(pythonNegZeroNoWriter))
	}
	t.Logf("%s: %d of %d corpus rows held (%d not carried, by name); %d writer twins held", pin, held, len(rows), exempt, twins)
}
