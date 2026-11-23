package darttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// SPEC-TABLES §2.15 requires refusal for the declared deprecated value;
// a live variant still writes through the same generated API.
func TestDartWriteAssertsDeprecatedEnumVariant(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package depr

enum Phase
{
    Live
    Old | deprecated
    Next
}

table VarRoot
{
    phase Phase
}

fixed table FixedRoot
{
    phase Phase
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"DeprWire.dart", "DeprFixed.dart"} {
		if !strings.Contains(string(files[name]), "!= 2, 'deprecated enum variant Phase.Old'") {
			t.Errorf("%s did not emit the deprecated variant assertion", name)
		}
	}
	t.Run("native", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		writeFiles(t, dir, packetFiles(t, u))
		writeFiles(t, dir, files)
		for _, probe := range []struct {
			name   string
			source string
		}{
			{"variable", deprecatedVariableProbe},
			{"fixed", deprecatedFixedProbe},
		} {
			t.Run(probe.name, func(t *testing.T) {
				file := probe.name + "_deprecated_probe.dart"
				body := probe.source + deprecatedAssertHelper
				if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", file)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("Dart deprecated enum writer probe: %v\n%s", err, out)
				}
			})
		}
	})
}

const deprecatedAssertHelper = `
void assertDeprecated(String form, void Function() write) {
  try {
    write();
  } on AssertionError catch (error) {
    if (error.message != 'deprecated enum variant Phase.Old') {
      rethrow;
    }
    return;
  }
  throw StateError('$form wrote Phase.Old');
}
`

const deprecatedVariableProbe = `import 'Probe.dart' as p;
import 'DeprWire.dart' as w;

void main() {
  final value = w.VarRoot()..phase = p.Phase.next;
  final bytes = w.varRootWireSave(value);
  if (bytes.isEmpty || bytes[0] != 1) {
    throw StateError('variable writer refused Phase.Next');
  }
  value.phase = p.Phase.old;
  assertDeprecated('Form 1', () => w.varRootWireSave(value));
}
`

const deprecatedFixedProbe = `import 'dart:typed_data';

import 'DeprFixed.dart' as types;
import 'Probe.dart' as p;
import 'ProbeFixed.dart' as f;

void main() {
  final value = types.FixedRoot()..phase = p.Phase.next;
  final values = <types.FixedRoot>[value];
  final out = Uint8List(f.fixedRootFixedMeasure(values.length));
  if (f.fixedRootFixedSave(values, values.length, out) != out.length ||
      out.isEmpty || out[0] != 3) {
    throw StateError('fixed writer refused Phase.Next');
  }
  value.phase = p.Phase.old;
  assertDeprecated('Form 3', () => f.fixedRootFixedSave(values, values.length, out));
}
`
