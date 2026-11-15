package darttable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDartArrayElementKindsClosed holds docs/SPEC-TABLES.md §3's closed list on
// the Dart leg: an array body (kind 14) may carry element kinds 1–13, 15, 17
// and 18–30, an enum-keyed body (kind 16) may carry 1–11, 13 and 18–30. Any
// other element kind is damage under any count, a count of 0 included, in a
// field this reader declares as in one it only skips. Each pinned array_elem_*
// framing vector rides an unknown root field of graphdemo Scene, so only the
// wire-alone walk can meet it; each runs through Scene's strict load and the
// verdict alone is compared (docs/rulings.json refusal-verdict-only). The two
// kind-17 controls read.
func TestDartArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	rows := []struct {
		name string
		read bool
	}{
		{name: "array_elem_kind_32_count_2", read: false},
		{name: "array_elem_kind_31_count_1", read: false},
		{name: "array_elem_kind_16_count_1", read: false},
		{name: "array_elem_unassigned_count_0", read: false},
		{name: "array_elem_reserved_count_0", read: false},
		{name: "array_elem_zero_count_0", read: false},
		{name: "array_elem_kind_17_count_0", read: true},
		{name: "array_elem_kind_17_count_1", read: true},
	}
	for _, row := range rows {
		if _, err := os.Stat(filepath.Join(framing, row.name+".bin")); err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", row.name, err)
		}
	}
	u := loadDirUnit(t, filepath.Join(root, "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string][]byte{}
	for name, src := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	for name, src := range files {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	if !strings.Contains(string(set["GraphdemoWire.dart"]), "sceneWireLoad(") {
		t.Fatal("GraphdemoWire.dart carries no sceneWireLoad: the entry point moved")
	}
	var script strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&script, "  ('%s', %t),\n", row.name, row.read)
	}
	set["array_elem_probe.dart"] = []byte(strings.Replace(arrayElemKindProbe, "ROWS\n", script.String(), 1))
	dir := t.TempDir()
	writeFiles(t, dir, set)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "array_elem_probe.dart", framing)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the array element-kind probe failed: %v\n%s", err, out)
	}
}

const arrayElemKindProbe = `import 'dart:io';
import 'dart:typed_data';
import 'GraphdemoWire.dart' as g;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stdout.writeln('FAIL: $what');
    failed = true;
  }
}

void main(List<String> args) {
  Uint8List wire(String name) => File('${args[0]}/$name.bin').readAsBytesSync();
  for (final (row, reads) in [
ROWS
  ]) {
    final r = g.TableWireReport();
    g.sceneWireLoad(wire(row), r);
    check(r.refused != reads, '$row: the load ${reads ? "refused" : "read"} (want ${reads ? "read" : "refused"})');
  }
  if (failed) exit(1);
  stdout.writeln('ok: every array element-kind vector answered its verdict');
}
`
