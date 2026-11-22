package pythontable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// f32EndsDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS HELD
// AT FLOAT32 (docs/SPEC-TABLES.md §3.3); its cases.tsv says what each row owes.
const f32EndsDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// f32EndsGenerated generates the corpus's Probe.schema for Python.
func f32EndsGenerated(t *testing.T) map[string][]byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(f32EndsDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnit(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestPythonCompressedF32EndsDescriptors is the fast pin: every descriptor of
// a compressed float32 carries the float32 nearest each declared end (0.1f is
// 0.10000000149011612, -0.3f is -0.30000001192092896), never the double the
// schema spells, which the float32 end itself exceeds. A plain float32 beside
// it carries no range at all.
func TestPythonCompressedF32EndsDescriptors(t *testing.T) {
	t.Parallel()
	var all strings.Builder
	for _, data := range f32EndsGenerated(t) {
		all.Write(data)
	}
	gen := all.String()
	for _, want := range []string{"fmax=0.10000000149011612", "fmin=-0.30000001192092896"} {
		if !strings.Contains(gen, want) {
			t.Errorf("no descriptor carries the float32 end %s", want)
		}
	}
	for _, double := range []string{"fmax=0.1,", "fmax=0.1)", "fmin=-0.3,", "fmin=-0.3)"} {
		if strings.Contains(gen, double) {
			t.Errorf("a descriptor carries the double end %q, which the float32 end exceeds", double)
		}
	}
	// The control: a plain float32 has no range test.
	plain, err := Generate(loadUnit(t, "package plainf\n\ntable P\n{\n    x float32\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var p strings.Builder
	for _, data := range plain {
		p.Write(data)
	}
	if !strings.Contains(p.String(), "float_range=False") || strings.Contains(p.String(), "float_range=True") {
		t.Errorf("a plain float32 carries a range test")
	}
}

// TestPythonCompressedF32EndsCorpus runs every cases.tsv row on both Python
// pins: the variable form (V.load, save), the fixed form (F.fixed_load,
// F.fixed_save) and JSON (from_json), each save checked byte for byte against
// the corpus.
func TestPythonCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	data, err := os.ReadFile(filepath.Join(f32EndsDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	entries, err := os.ReadDir(f32EndsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") || strings.HasSuffix(e.Name(), ".json") {
			b, err := os.ReadFile(filepath.Join(f32EndsDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			inputs[e.Name()] = b
		}
	}
	var probe strings.Builder
	probe.WriteString(pyrun.ImportPrelude + f32EndsProbeHead)
	var rows [][]string
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", n+1, len(f))
		}
		rows = append(rows, f)
		name, op, form, root, input, path, bits := f[0], f[1], f[2], f[3], f[4], f[5], f[6]
		switch op {
		case "read":
			fmt.Fprintf(&probe, "step(%q, lambda: save(%q, load(%q, %q, %q)))\n", name, root, root, form, input)
		case "write":
			if _, err := strconv.ParseUint(bits, 16, 32); err != nil {
				t.Fatalf("%s: bits %q: %v", name, bits, err)
			}
			if (form == "variable") != (root == "V") {
				t.Fatalf("%s: form %s under root %s", name, form, root)
			}
			fmt.Fprintf(&probe, "step(%q, lambda: write(%q, %q, %q, 0x%s))\n", name, root, input, path, bits)
		default:
			t.Fatalf("%s: op %q", name, op)
		}
	}
	files := f32EndsGenerated(t)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		ins := map[string][]byte{"main.py": []byte(probe.String())}
		maps.Copy(ins, inputs)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", ins)
		if err != nil {
			t.Fatalf("%s: %v\n%s", pin, err, stderr)
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			id, answer, _ := strings.Cut(line, " ")
			got[id] = answer
		}
		for _, r := range rows {
			want := "refused"
			if r[7] != "refused" {
				b, err := os.ReadFile(filepath.Join(f32EndsDir, r[7]+"."+f32EndsOwnForm(r)+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				want = fmt.Sprintf("held %x", b)
			}
			if got[r[0]] != want {
				t.Errorf("%s %s: answered %q, want %q", filepath.Base(pin), r[0], got[r[0]], want)
			}
		}
	}
}

// f32EndsOwnForm is the form a row's save is in: the write row's form, or the
// read row's root's own form (V variable, F fixed).
func f32EndsOwnForm(row []string) string {
	if row[1] == "write" {
		return row[2]
	}
	if row[3] == "V" {
		return "variable"
	}
	return "fixed"
}

const f32EndsProbeHead = `import struct
from f32ends import F, InvalidValue, Refused, V

ROOTS = {"V": V, "F": F}


def raw(name):
    with open(name, "rb") as f:
        return f.read()


def load(root, form, stem):
    if form == "json":
        return ROOTS[root].from_json(raw(stem + ".json").decode())
    if form == "variable":
        return V.load(raw(stem + ".variable.bin"))
    rows = F.fixed_load(raw(stem + ".fixed.bin"))
    assert len(rows) == 1, rows
    return rows[0]


def save(root, value):
    if root == "V":
        return value.save()
    return F.fixed_save([value])


def write(root, stem, path, bits):
    value = load(root, "json", stem)
    v = struct.unpack("<f", struct.pack("<I", bits))[0]
    name, _, index = path.partition("[")
    if index:
        getattr(value, name)[int(index.rstrip("]"))] = v
    else:
        setattr(value, name, v)
    return save(root, value)


def step(name, fn):
    try:
        out = fn()
    except (Refused, InvalidValue):
        print(name, "refused")
    except Exception as e:
        print(name, "error", type(e).__name__, str(e).replace("\n", " "))
    else:
        print(name, "held", bytes(out).hex())

`
