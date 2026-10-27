package darttable

// I13's wire region measure differential for the affected roots of the wire
// fuzzer cause D2 (dart-fix-measure-mixed-and-map-roots): the generated Dart
// <name>WireMeasure (emitMeasure, internal/codegen/darttable/wireform.go) must
// answer the same region-byte count as the Go compiler-side oracle
// tablewire.FileRegionMeasure (internal/tablewire/extent.go, docs/SPEC-TABLES.md
// §6.2) on the same bytes, for listdemo.Mixed and mapdemo Text, Slots and
// Chunks over their testdata/wire/tables/*.bin fixtures.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// measureRootCase is one root the row pins: the unit, the generated wire
// library, the root table name and the fixture the measure runs over.
type measureRootCase struct {
	name string
	unit *ir.Unit
	lib  string
	root string
	bin  string
}

// TestWireMeasureMixedAndMapRootsDart pins the Dart WireMeasure to the Go
// oracle on the four roots of the cause.
func TestWireMeasureMixedAndMapRootsDart(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	dart := dartBinary(t)

	repo := filepath.Join("..", "..", "..")
	listdemo := loadUnit(t, mustSchemaGlob(t, filepath.Join(repo, "tables", "lists"))...)
	mapdemo := loadUnit(t, mustSchemaGlob(t, filepath.Join(repo, "tables", "maps"))...)

	listFiles, err := Generate(listdemo)
	if err != nil {
		t.Fatalf("Generate listdemo: %v", err)
	}
	mapFiles, err := Generate(mapdemo)
	if err != nil {
		t.Fatalf("Generate mapdemo: %v", err)
	}

	listLib := capitalize("listdemo") + "Wire.dart"
	mapLib := capitalize("mapdemo") + "Wire.dart"

	cases := []measureRootCase{
		{"listdemo.Mixed", listdemo, listLib, "Mixed", "list_mixed.bin"},
		{"mapdemo.Text", mapdemo, mapLib, "Text", "map_text.bin"},
		{"mapdemo.Slots", mapdemo, mapLib, "Slots", "map_slots.bin"},
		{"mapdemo.Chunks", mapdemo, mapLib, "Chunks", "map_chunks.bin"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := listFiles
			if c.unit == mapdemo {
				files = mapFiles
			}
			measureWireMeasure(t, dart, c, files)
		})
	}
}

func measureWireMeasure(t *testing.T, dart string, c measureRootCase, files map[string][]byte) {
	t.Helper()
	binAbs, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "tables", c.bin))
	if err != nil {
		t.Fatalf("bin path: %v", err)
	}
	data, err := os.ReadFile(binAbs)
	if err != nil {
		t.Fatalf("read %s: %v", c.bin, err)
	}
	root := c.unit.Tables[c.root]
	if root == nil {
		t.Fatalf("unit %s has no table %s", c.unit.Package, c.root)
	}
	lib, ok := files[c.lib]
	if !ok {
		t.Fatalf("Generate emitted no %s", c.lib)
	}
	dir := t.TempDir()
	writeFiles(t, dir, packetFiles(t, c.unit))
	if err := os.WriteFile(filepath.Join(dir, c.lib), lib, 0o600); err != nil {
		t.Fatal(err)
	}
	fn := dartName(c.root) + "WireMeasure"
	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import '%s';

void main() {
  final data = File(Platform.environment['BIN']!).readAsBytesSync();
  print(%s(data));
}
`, c.lib, fn)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}

	check := func(label string, wire []byte) {
		t.Helper()
		bin := filepath.Join(dir, label+".bin")
		if err := os.WriteFile(bin, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		goBytes, whole, refused := tablewire.FileRegionMeasure(c.unit, root, wire)
		if !whole && !refused {
			t.Fatalf("%s %s: oracle returned a non-whole, non-refused measure (whole=%v refused=%v)", c.name, label, whole, refused)
		}
		want := goBytes
		if refused {
			want = -1
		}
		got := runDartMeasure(t, dart, dir, bin, c.name+" "+label)
		if got != want {
			t.Errorf("%s %s: Dart WireMeasure = %d, Go FileRegionMeasure = %d (whole=%v refused=%v)", c.name, label, got, want, whole, refused)
		}
	}

	check("clean", data)
	// A deterministic mutant whose oracle measure differs from the clean one:
	// the old extent walk, which skipped map entries and mixed-list elements,
	// measured this body differently while the Go reference stayed a clean
	// read (SPEC-TABLES §6.2).
	mutant, _ := divergingMutant(t, c.unit, root, data)
	check("mutant", mutant)
}

func runDartMeasure(t *testing.T, dart, dir, bin, label string) int64 {
	t.Helper()
	cmd := exec.Command(dart, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BIN="+bin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dart probe for %s: %v\n%s", label, err, out)
	}
	got, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatalf("parse dart measure for %s: %v\n%s", label, err, out)
	}
	return got
}

// divergingMutant finds one single-byte mutation of the clean wire whose Go
// region measure is still a clean whole read but differs from the clean
// measure: the mutant exercises the map/list extent walk this test pins.
func divergingMutant(t *testing.T, u *ir.Unit, root *ir.Struct, data []byte) ([]byte, int64) {
	t.Helper()
	base, whole, refused := tablewire.FileRegionMeasure(u, root, data)
	if !whole || refused {
		t.Fatalf("the clean fixture is not a clean whole read (whole=%v refused=%v)", whole, refused)
	}
	for i := 1; i < len(data); i++ {
		for _, v := range []byte{0x00, 0xFF, data[i] ^ 0x80} {
			if v == data[i] {
				continue
			}
			m := append([]byte(nil), data...)
			m[i] = v
			b, w, r := tablewire.FileRegionMeasure(u, root, m)
			if w && !r && b != base {
				return m, b
			}
		}
	}
	t.Fatalf("no single-byte mutant of %s keeps a clean whole read with a different measure", root.Name)
	return nil, 0
}

func mustSchemaGlob(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("glob %s: no schemas", dir)
	}
	return paths
}
