package darttable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), on the Dart leg. Every reader answers wire_damaged for an id
// table of more than 4096 entries, so rootWireSave refuses (throws StateError,
// the leg's writer refusal) a value that needs 4097 and writes the one that
// needs exactly 4096, which the read accepts. The unit is
// tablewire.IdCapSchema: 4095, 4096 and 4097 ids.
//
// A FUNCTIONAL TEST: it runs the generated Dart, behind slowtest.Gate.
func TestDartWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := unitFrom(t, tablewire.IdCapSchema())
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "idcap_probe.dart"), []byte(idCapDartProbe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", "idcap_probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("id cap probe: %v\n%s", err, out)
	}
}

const idCapDartProbe = `import 'dart:typed_data';
import 'IdcapWire.dart';
void check(bool ok, String what) { if (!ok) throw StateError(what); }
Root value(int x, int y) {
  final v = Root();
  for (var i = 0; i < v.keys.length; i++) { v.keys[i] = 1; }
  v.x = x;
  v.y = y;
  return v;
}
int count(Uint8List b) => ByteData.sublistView(b).getUint64(b.length - 8, Endian.little);
void main() {
  for (final row in [[0, 0, 4095], [1, 0, 4096]]) {
    final wire = rootWireSave(value(row[0], row[1]));
    check(count(wire) == row[2], '${row[2]} ids: count word ${count(wire)}');
    final report = TableWireReport();
    rootWireLoad(wire, report);
    check(!report.refused, '${row[2]} ids: the read refused the file');
  }
  var refused = false;
  try {
    rootWireSave(value(1, 1));
  } on StateError {
    refused = true;
  }
  check(refused, '4097 ids: the save wrote a file');
}
`
