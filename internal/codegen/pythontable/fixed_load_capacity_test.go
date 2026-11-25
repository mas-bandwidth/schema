package pythontable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

func TestPythonFixedLoadCapacityWitness(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	read := func(relative string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	unit := fixedproof.ParseCheckUnit(t, string(read("test/fixed-empty-batch/Batch.schema")))
	files, err := pythontable.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"main.py":   []byte(pythonFixedLoadCapacityProbe),
		"cases.txt": read("testdata/conformance/feature-witnesses/fixed-load-capacity/cases.txt"),
		"one.bin":   read("testdata/wire/fixed-empty-batch/one-record.bin"),
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		stdout, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil {
			t.Fatalf("fixed load capacities with %s: %v\n%s%s", pin, err, stdout, stderr)
		}
		t.Logf("%s:\n%s", pin, stdout)
	}
}

const pythonFixedLoadCapacityProbe = pyrun.ImportPrelude + `
from pathlib import Path
from fixedemptybatch import Batch, InvalidValue, Refused

wire = Path('one.bin').read_bytes()
for line in Path('cases.txt').read_text().splitlines():
    if not line or line.startswith('#'):
        continue
    case, operation, capacity, expected = line.split()
    if operation == 'load':
        try:
            if capacity == 'omitted':
                values = Batch.fixed_load(wire)
            else:
                values = Batch.fixed_load(wire, capacity=int(capacity))
        except Refused as error:
            observed = error.reason
        else:
            observed = 'read:' + str(len(values))
            assert len(values) == 1 and values[0].a == 7 and values[0].name == 'hello', case
    elif operation == 'save-empty':
        try:
            Batch.fixed_save([])
        except InvalidValue as error:
            assert error.rule == 'a fixed file holds at least one record', case
            observed = 'invalid_value'
        else:
            raise AssertionError(case + ': empty fixed save returned bytes')
    else:
        raise AssertionError(case + ': unknown operation')
    assert observed == expected, f'{case}: got {observed}, want {expected}'
    print(case, observed)
`
