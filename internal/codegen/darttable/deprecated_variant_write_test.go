package darttable

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// SPEC-TABLES §2.15 requires refusal for the declared deprecated value;
// a live variant still writes through the same generated API.
func TestDartWriteAssertsDeprecatedEnumVariant(t *testing.T) {
	t.Parallel()
	fixture := filepath.Join("..", "..", "..", "testdata", "conformance", "feature-witnesses", "declared-deprecated-enum")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"DeprWire.dart", "DeprFixed.dart"} {
		if !strings.Contains(string(files[name]), "!= 2, 'deprecated variant is never written (SPEC-TABLES 2.15)'") {
			t.Errorf("%s did not emit the deprecated variant assertion", name)
		}
	}
	t.Run("native", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		writeFiles(t, dir, packetFiles(t, u))
		writeFiles(t, dir, files)
		want, err := os.ReadFile(filepath.Join(fixture, "cases.txt"))
		if err != nil {
			t.Fatal(err)
		}
		const file = "deprecated_probe.dart"
		if err := os.WriteFile(filepath.Join(dir, file), []byte(deprecatedProbe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", file)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || !bytes.Equal(out, want) {
			t.Fatalf("Dart deprecated enum writer probe: %v\ngot:\n%swant:\n%s", err, out, want)
		}
	})
}

const deprecatedProbe = `import 'dart:typed_data';

import 'Probe.dart' as p;
import 'DeprWire.dart' as w;
import 'DeprFixed.dart' as types;
import 'ProbeCook.dart' as c;
import 'ProbeFixed.dart' as f;

void emit(String id, Uint8List bytes) {
  print('$id ${bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join()}');
}

void refuse(String id, void Function() write) {
  try {
    write();
  } on AssertionError catch (error) {
    if (error.message != 'deprecated variant is never written (SPEC-TABLES 2.15)') {
      rethrow;
    }
    print('$id refused');
    return;
  }
  throw StateError('$id wrote Phase.Old');
}

void main() {
  final variable = w.VarRoot()..phase = p.Phase.next;
  emit('sf26_form1_next', w.varRootWireSave(variable));
  variable.phase = p.Phase.old;
  refuse('sf26_form1_old', () => w.varRootWireSave(variable));

  final fixed = types.FixedRoot()..phase = p.Phase.next;
  final values = <types.FixedRoot>[fixed];
  final out = Uint8List(f.fixedRootFixedMeasure(values.length));
  if (f.fixedRootFixedSave(values, values.length, out) != out.length) {
    throw StateError('Form 3 refused Phase.Next');
  }
  emit('sf26_form3_next', out);
  fixed.phase = p.Phase.old;
  refuse('sf26_form3_old', () => f.fixedRootFixedSave(values, values.length, out));

  fixed.phase = p.Phase.next;
  final size = c.FixedRootCook.cookMeasure(fixed);
  print('sf26_cook_measure_next $size');
  for (final order in [Endian.little, Endian.big]) {
    final cooked = Uint8List(size);
    if (!c.FixedRootCook.cook(fixed, cooked, order)) {
      throw StateError('Cook refused Phase.Next');
    }
    emit(order == Endian.little ? 'sf26_cook_next_le' : 'sf26_cook_next_be', cooked);
  }
  fixed.phase = p.Phase.old;
  final oldSize = c.FixedRootCook.cookMeasure(fixed);
  print('sf26_cook_measure_old $oldSize');
  for (final order in [Endian.little, Endian.big]) {
    final cooked = Uint8List(size)..fillRange(0, size, 0x5a);
    refuse(order == Endian.little ? 'sf26_cook_old_le' : 'sf26_cook_old_be',
        () => c.FixedRootCook.cook(fixed, cooked, order));
    if (cooked.any((b) => b != 0x5a)) {
      throw StateError('Cook changed the buffer after refusing Phase.Old');
    }
  }
}
`
