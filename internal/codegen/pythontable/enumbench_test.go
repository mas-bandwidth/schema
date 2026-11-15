package pythontable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const enumBenchSchema = `package enumbench

enum Hue { Red, Green, Blue }

enum Wide16 { V1, V2, V3, V4, V5, V6, V7, V8, V9, V10, V11, V12, V13, V14, V15, V16 }

table Pair
{
    first  Hue
    second Hue
}

table Many
{
    a Wide16
    b Wide16
    c Wide16
    d Wide16
    e Wide16
    f Wide16
    g Wide16
    h Wide16
}
`

// TestPythonEnumLookupBench measures the enum codec's two lookups
// (docs/PORT-PYTHON.md, THE TABLE FRAME, Enum lookup bench): the shipped dict
// built once in the descriptor against a per-field linear scan of the
// variants, on a two-field table over a three-variant enum and an eight-field
// table over a sixteen-variant enum, under both pinned interpreters. It is a
// functional test; the numbers it logs are the record in docs/PORT-PYTHON.md
// and it asserts only that both lookups read and write the same bytes.
//
// Functional (pythonBinary): run with SCHEMA_SLOW=1.
func TestPythonEnumLookupBench(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it times the emitted table runtime)")
	u := loadUnit(t, enumBenchSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
from enumbench import Many, Pair
from enumbench._schema import InvalidValue, kinds

class Linear:
    """The scan the dict replaced: the variants walked one by one."""

    def validate(self, fd, value, path):
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        if value < fd.lo or value > fd.hi:
            raise InvalidValue(path, "out of range")
        if value != 0 and not any(v == value for v, _ in fd.variants):
            raise InvalidValue(path, "names no variant")

    def read(self, r, fd, ctx):
        reference = r.varint()
        if reference == 0:
            return 0
        wire_id = ctx.ids[reference - 1]
        for value, variant_id in fd.variants:
            if variant_id == wire_id:
                return value
        ctx.unknown += 1
        return 0

    def write(self, w, fd, value, ctx):
        w.varint(ctx.idref(fd.field_id))
        w.byte(fd.kind)
        if value != 0:
            for variant, wire_id in fd.variants:
                if variant == value:
                    w.varint(ctx.idref(wire_id))
                    return
        w.varint(0)

shipped = kinds._REGISTRY["enum"]
linear = Linear()

pair = Pair()
pair.first = 3
pair.second = 2
many = Many()
for name, value in zip("abcdefgh", (16, 15, 12, 9, 8, 5, 2, 1)):
    setattr(many, name, value)

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

for label, value in (("pair", pair), ("many", many)):
    cls = type(value)
    kinds._REGISTRY["enum"] = shipped
    data = value.save()
    kinds._REGISTRY["enum"] = linear
    if value.save() != data or cls.load(data) != value:
        raise SystemExit("enumbench_" + label + ": the linear scan differs from the dict")
    for codec_label, codec in (("dict", shipped), ("linear", linear)):
        kinds._REGISTRY["enum"] = codec
        value.save()
        cls.load(data)
        print(label + "_save_" + codec_label, round(run(lambda: value.save()), 1))
        print(label + "_load_" + codec_label, round(run(lambda: cls.load(data)), 1))
kinds._REGISTRY["enum"] = shipped
print("OK")
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for _, pin := range []struct {
		name string
		out  []byte
	}{
		{"python-3.15.2", pyrun.Run(t, dir, "main.py", inputs)},
		{"python-3.11.18", pyrun.RunFloor(t, dir, "main.py", inputs)},
	} {
		out := string(pin.out)
		for _, table := range []string{"pair", "many"} {
			for _, key := range []string{"save_dict", "save_linear", "load_dict", "load_linear"} {
				value := timedValue(t, out, table+"_"+key)
				if value <= 0 {
					t.Fatalf("%s %s_%s did not report a positive time", pin.name, table, key)
				}
				t.Logf("%s %s_%s %.1f ns/op", pin.name, table, key, value)
			}
		}
	}
}
