package darttable

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §22.6). The vector under
// testdata/wire/fixed-empty-batch/ is one header and layout followed by one
// record, and the same bytes with the record dropped; the read refuses the
// latter and still reads the former, and the writer refuses a batch of none in
// a debug build.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestDartFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart SDK (it runs the generated reader and writer)")

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := versionSchemaPath(t, filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema"), "Batch")
	dir, codec, typefile, typ, lower := t12Emit(t, u)
	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")

	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import %q as c;
import %q as t;
void check(bool ok, String what) {
  if (!ok) { stderr.writeln('FAIL: $what'); exitCode = 1; }
}
void main() {
  final noRecord = File(%q).readAsBytesSync();
  final oneRecord = File(%q).readAsBytesSync();
  // EVERY FIXED READ ENTRY: a header and a layout followed by no record is
  // refused at any capacity, never a batch of zero.
  for (final capacity in <int>[1, 2, 8]) {
    final values = <t.%s>[for (var i = 0; i < capacity; i++) t.%s()];
    for (final v in values) { t.init%s(v); }
    final report = t.TableFixedReport();
    final n = c.%sFixedLoad(values, capacity, noRecord, noRecord.length,
        c.%sFixedNewPlan(), report);
    check(n == -1, 'a no-record file read as $n records at capacity $capacity');
    check(report.refused != t.TableFixedRefusal.none,
        'a no-record file was not refused at capacity $capacity');
  }
  // A ONE-RECORD FILE STILL READS.
  {
    final values = <t.%s>[t.%s()];
    t.init%s(values[0]);
    final report = t.TableFixedReport();
    final n = c.%sFixedLoad(values, 1, oneRecord, oneRecord.length,
        c.%sFixedNewPlan(), report);
    check(n == 1, 'the one-record file read $n records');
    check(report.refused == t.TableFixedRefusal.none,
        'the one-record file was refused: ${t.TableFixedRefusal.name(report.refused)}');
  }
  // A SAVE OF ZERO RECORDS IS REFUSED (a debug assert, this leg's own idiom).
  {
    var refused = false;
    try {
      c.%sFixedSave(<t.%s>[], 0, Uint8List(c.%sFixedMeasure(0)));
    } on AssertionError {
      refused = true;
    }
    check(refused, 'a save of zero records was not refused');
  }
  if (exitCode == 0) { stdout.writeln('ok'); }
}
`, "./"+codec, "./"+typefile,
		filepath.Join(vector, "no-record.bin"), filepath.Join(vector, "one-record.bin"),
		typ, typ, typ, lower, lower,
		typ, typ, typ, lower, lower,
		lower, typ, lower)

	out := t12RunProbe(t, dir, "fixed_batch_holds_a_record.dart", probe)
	if !strings.Contains(out, "ok") {
		t.Fatalf("the fixed batch rule probe printed no ok: %s", out)
	}
}
