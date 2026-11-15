package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// THE EVOLUTION KINDS' SHAPE BENCH (docs/PORT-PYTHON.md, THE TABLE FRAME,
// Bench). The string, bytes and enum codecs (kind_string.py, kind_bytes.py,
// kind_enum.py) run on one table in both of D1's code shapes, under both
// pinned interpreters: the straight-line pair the generator emits for a
// table of scalars against the runtime's descriptor walk over the same table.
// It is a functional test; the numbers it logs are the record in
// docs/PORT-PYTHON.md and it asserts only that both shapes agree.

const vtBenchSchema = `package kindbench

enum Grade { Bronze, Silver, Gold }

table Kinds
{
    name  string(32) = "untitled"
    title string(64)
    tag   bytes(16)  = "ab"
    blob  bytes(32)
    grade Grade      = Silver
    rank  Grade
}
`

func TestPythonEvolutionKindsShapeBench(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtBenchSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if !scalarOnly(u.Tables["Kinds"]) {
		t.Fatal("Kinds does not take the straight-line shape: the bench compares nothing")
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
import kindbench.Core as Core
from kindbench import Kinds
from kindbench._schema.load import load_table
from kindbench._schema.save import save_table

k = Kinds()
k.name = "Aurora Borealis"
k.title = "a ship of the line, first of her name"
k.tag = b"\x01\x02\x03\x04\x05\x06\x07\x08"
k.blob = bytes(range(24))
k.grade = 3
k.rank = 1
data = save_table(k)
if Core._s_save_Kinds(k) != data:
    raise SystemExit("the straight-line save differs from the descriptor walk")
if Core._s_load_Kinds(Kinds, data) != k or load_table(Kinds, data) != k:
    raise SystemExit("the straight-line load differs from the descriptor walk")

N = 10000
ROUNDS = 5

def run(fn):
    best = None
    for _ in range(ROUNDS):
        start = time.perf_counter()
        for _ in range(N):
            fn()
        value = (time.perf_counter() - start) * 1e9 / N
        if best is None or value < best:
            best = value
    return best

def gen_save():
    Core._s_save_Kinds(k)

def desc_save():
    save_table(k)

def gen_load():
    Core._s_load_Kinds(Kinds, data)

def desc_load():
    load_table(Kinds, data)

gen_save()
desc_save()
gen_load()
desc_load()
print("save_generated", round(run(gen_save), 1))
print("save_descriptor", round(run(desc_save), 1))
print("load_generated", round(run(gen_load), 1))
print("load_descriptor", round(run(desc_load), 1))
print("OK")
`
	keys := []string{"save_generated", "save_descriptor", "load_generated", "load_descriptor"}
	for name, runner := range map[string]func(t testing.TB, dir, main string, inputs map[string][]byte) []byte{
		"python-3.15.2":  pyrun.Run,
		"python-3.11.18": pyrun.RunFloor,
	} {
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if !strings.Contains(out, "OK\n") {
			t.Fatalf("%s did not finish:\n%s", name, out)
		}
		for _, key := range keys {
			value := timedValue(t, out, key)
			if value <= 0 {
				t.Fatalf("%s %s did not report a positive time", name, key)
			}
			t.Logf("%s %s %.1f ns/op", name, key, value)
		}
	}
}
