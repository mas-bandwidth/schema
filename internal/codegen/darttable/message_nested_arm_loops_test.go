package darttable

// A LIST INSIDE A LIST ELEMENT, THROUGH THE MESSAGE FORM (docs/SPEC-TABLES.md
// §3.3), over the schema and samples of fixed_nested_arm_loops_test.go: the Go
// engine writes each sample as a one-body batch, the generated
// <root>LoadMessages reads it, and the generated <root>SaveMessages writes it
// back. The Dart batch must equal the engine's, and the engine must read the
// Dart batch back to the sample's own value.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartMessageFormNestedArmLoops(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	u := unitFrom(t, nestedArmProbe)
	m, batch, text := nestedArmEngine(t, u, "Box", func(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
		return tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	names := slices.Sorted(maps.Keys(batch))
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name+".wire"), batch[name], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var p strings.Builder
	p.WriteString(`import 'dart:io';
import 'dart:typed_data';
import 'ProbeFixed.dart' as f;

void main() {
  final vocab = f.TableVocabulary(entries: List.generate(4096, (i) => f.TableMessageEntry(0, 0)));
  if (!f.announceRead(vocab, f.tableAnnouncement, f.TableReport())) {
    print('FAIL announcement');
    return;
  }
  for (final name in [`)
	for i, name := range names {
		if i > 0 {
			p.WriteString(", ")
		}
		fmt.Fprintf(&p, "'%s'", name)
	}
	p.WriteString(`]) {
    final data = File('$name.wire').readAsBytesSync();
    final v = f.Box();
    final lr = f.TableReport();
    final n = f.boxLoadMessages([v], data, vocab, lr);
    if (n != 1 || lr.refused || lr.unknown != 0 || lr.widened != 0) {
      print('FAIL $name: load answered $n refused=${lr.refused} ${lr.reason}');
      continue;
    }
    final sr = f.TableReport();
    final out = Uint8List(f.boxMeasureMessages([v], sr));
    final w = f.boxSaveMessages([v], out, sr);
    if (w != out.length || sr.refused) {
      print('FAIL $name: save answered $w of ${out.length} ${sr.reason}');
      continue;
    }
    File('$name.saved').writeAsBytesSync(out);
    print('OK $name');
  }
}
`)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(p.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	var vocab tablewire.Vocabulary
	if err := vocab.AnnounceRead(tablewire.Announce(u), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	nestedArmCompare(t, m, "Box", names, string(out), dir, batch, text,
		func(m *tabletext.Model, inst *tabletext.Instance, data []byte, rep *tabletext.Report) (bool, error) {
			n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, data, &vocab, rep)
			return ok && n == 1, err
		})
}
