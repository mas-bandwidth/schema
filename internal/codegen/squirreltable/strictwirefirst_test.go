package squirreltable_test

import (
	"bytes"
	"encoding/binary"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestSquirrelStrictReadChecksWireFirst(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, repo, []string{"tables/blobs"})
	m := tabletext.NewModel(u)
	root := m.Lookup("Catalog")
	cleanValue := m.New(root)
	name, ok := cleanValue.FieldByKey("name")
	if !ok {
		t.Fatal("blobdemo.Catalog has no name field")
	}
	name.Cell.Str = []byte(strings.Repeat("x", 32))
	name.Count = len(name.Cell.Str)
	clean, err := tablewire.Encode(m, cleanValue)
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate the first (string) field in the root body. Its repeated id is
	// wire-alone damage, and the second copy comes after a long string decode.
	fieldEnd := wireFirstFieldEnd(t, clean)
	mutant := make([]byte, 0, len(clean)+fieldEnd-1)
	mutant = append(mutant, clean[:fieldEnd]...)
	mutant = append(mutant, clean[1:fieldEnd]...)
	mutant = append(mutant, clean[fieldEnd:]...)
	t.Run("Go", func(t *testing.T) {
		var goReport tabletext.Report
		goOK, goErr := tablewire.Decode(m, m.New(root), mutant, &goReport)
		if goErr != nil && !tablewire.ReadRefusal(goErr) {
			t.Fatalf("Go engine failed outside a refusal: %v", goErr)
		}
		if goOK || !goReport.Refused {
			t.Fatalf("Go engine accepted duplicate string field: ok=%t refused=%t", goOK, goReport.Refused)
		}
	})

	floatUnit, floatModel, floatRoot := nfLoad(t, `package floatprobe

table Root
{
    name string(32)
    value float64
}
`)
	floatValue := floatModel.New(floatRoot)
	floatName, ok := floatValue.FieldByKey("name")
	if !ok {
		t.Fatal("floatprobe.Root has no name field")
	}
	floatName.Cell.Str = []byte(strings.Repeat("y", 32))
	floatName.Count = len(floatName.Cell.Str)
	floatField, ok := floatValue.FieldByKey("value")
	if !ok {
		t.Fatal("floatprobe.Root has no value field")
	}
	floatField.Cell.F = 3.5
	floatClean, err := tablewire.Encode(floatModel, floatValue)
	if err != nil {
		t.Fatal(err)
	}
	floatFieldEnd := wireFirstFieldEnd(t, floatClean)
	if floatFieldEnd <= 33 {
		t.Fatalf("the encoded long string ends too early: %d", floatFieldEnd)
	}
	finiteBits := make([]byte, 8)
	binary.LittleEndian.PutUint64(finiteBits, math.Float64bits(3.5))
	nonFiniteBits := make([]byte, 8)
	binary.LittleEndian.PutUint64(nonFiniteBits, math.Float64bits(math.Inf(1)))
	if n := bytes.Count(floatClean, finiteBits); n != 1 {
		t.Fatalf("the finite float payload occurs %d times, want once", n)
	}
	floatMutant := bytes.Replace(floatClean, finiteBits, nonFiniteBits, 1)
	t.Run("Go non-finite", func(t *testing.T) {
		var goReport tabletext.Report
		goOK, goErr := tablewire.Decode(floatModel, floatModel.New(floatRoot), floatMutant, &goReport)
		if goErr != nil && !tablewire.ReadRefusal(goErr) {
			t.Fatalf("Go engine failed outside a refusal: %v", goErr)
		}
		if goOK || (goErr == nil && !goReport.Refused) {
			t.Fatalf("Go engine accepted non-finite float after long string: ok=%t refused=%t err=%v", goOK, goReport.Refused, goErr)
		}
	})

	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tableFiles, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tableFiles)
	load := string(files["schema_table_load.nut"])
	instanceAnchor := "local v = c.make();\n  local byType = {};"
	stringAnchor := "local dec = utf8Decode(r.b, r.at, L);"
	if n := strings.Count(load, instanceAnchor); n != 1 {
		t.Fatalf("instance counter anchor appears %d times in generated load runtime, want once", n)
	}
	if n := strings.Count(load, stringAnchor); n < 1 {
		t.Fatal("string counter anchor is absent from generated load runtime")
	}
	load = strings.Replace(load, instanceAnchor, "::strictWireFirst.instances++; local v = c.make();\n  local byType = {};", 1)
	load = strings.Replace(load, "local dec = utf8Decode(r.b, r.at, L);", "::strictWireFirst.strings++; local dec = utf8Decode(r.b, r.at, L);", 1)
	files["schema_table_load.nut"] = []byte(load)
	floatFiles, err := squirrel.Generate(floatUnit)
	if err != nil {
		t.Fatal(err)
	}
	floatTableFiles, err := squirreltable.Generate(floatUnit)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(floatFiles, floatTableFiles)
	floatLoad := string(floatFiles["schema_table_load.nut"])
	if strings.Count(floatLoad, instanceAnchor) != 1 || strings.Count(floatLoad, stringAnchor) < 1 {
		t.Fatal("float unit generated runtime is missing a counter anchor")
	}
	floatLoad = strings.Replace(floatLoad, instanceAnchor, "::strictWireFirst.instances++; local v = c.make();\n  local byType = {};", 1)
	floatLoad = strings.Replace(floatLoad, stringAnchor, "::strictWireFirst.strings++; local dec = utf8Decode(r.b, r.at, L);", 1)
	floatFiles["schema_table_load.nut"] = []byte(floatLoad)

	dir := t.TempDir()
	unitDir := filepath.Join(dir, "blobdemo")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	floatUnitDir := filepath.Join(dir, "floatprobe")
	if err := os.MkdirAll(floatUnitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(unitDir, name), data)
	}
	for name, data := range floatFiles {
		writeFile(t, filepath.Join(floatUnitDir, name), data)
	}
	main := `local I = dofile("inputs.nut", true);
::strictWireFirst <- { instances = 0, strings = 0 };
local S = dofile("blobdemo/schema_module.nut", true);
local T = S.require("BlobdemoTables");
local R = S.require("schema_table_refusal");
local bad = R.ReadStrict(T, "Catalog", I["bad"], {});
print((bad == null ? "refused" : "read") + "|" + ::strictWireFirst.instances.tostring() + "|" + ::strictWireFirst.strings.tostring() + "\n");
::strictWireFirst.instances = 0; ::strictWireFirst.strings = 0;
local FS = dofile("floatprobe/schema_module.nut", true);
local FT = FS.require("FloatprobeTables");
local FR = FS.require("schema_table_refusal");
local nonfinite = FR.ReadStrict(FT, "Root", I["nonfinite"], {});
print((nonfinite == null ? "refused" : "read") + "|" + ::strictWireFirst.instances.tostring() + "|" + ::strictWireFirst.strings.tostring() + "\n");
::strictWireFirst.instances = 0; ::strictWireFirst.strings = 0;
local good = R.ReadStrict(T, "Catalog", I["good"], {});
print((good == null ? "refused" : "read") + "|" + ::strictWireFirst.instances.tostring() + "|" + ::strictWireFirst.strings.tostring() + "\n");
`
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"bad": mutant, "good": clean, "nonfinite": floatMutant}))
	lines := strings.Fields(out)
	if len(lines) != 3 {
		t.Fatalf("strict-read probe returned %q, want three result lines", out)
	}
	if lines[0] != "refused|0|0" {
		t.Fatalf("strict load decoded before refusing wire damage: got %q, want refused|0|0", lines[0])
	}
	if lines[1] != "refused|0|0" {
		t.Fatalf("strict load decoded before refusing non-finite float after long string: got %q, want refused|0|0", lines[1])
	}
	goodParts := strings.Split(lines[2], "|")
	if len(goodParts) != 3 || goodParts[0] != "read" || goodParts[1] == "0" || goodParts[2] == "0" {
		t.Fatalf("accepted corpus instance did not exercise the walk: got %q", lines[2])
	}
}

func wireFirstFieldEnd(t *testing.T, wire []byte) int {
	t.Helper()
	if len(wire) < 4 || wire[0] != 1 {
		t.Fatalf("unexpected table wire header: %x", wire)
	}
	_, n := binary.Uvarint(wire[1:])
	if n <= 0 {
		t.Fatalf("first field reference is malformed: %x", wire)
	}
	kindAt := 1 + n
	if kindAt >= len(wire) || wire[kindAt] != 12 {
		t.Fatalf("first field is not a string: %x", wire[:min(len(wire), 12)])
	}
	_, n = binary.Uvarint(wire[kindAt+1:])
	if n <= 0 {
		t.Fatalf("first string length is malformed: %x", wire)
	}
	end := kindAt + 1 + n
	length, used := binary.Uvarint(wire[kindAt+1:])
	end += int(length)
	if used <= 0 || end > len(wire) {
		t.Fatalf("first string runs past wire: end=%d len=%d", end, len(wire))
	}
	return end
}
