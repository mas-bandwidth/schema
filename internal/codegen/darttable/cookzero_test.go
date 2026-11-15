package darttable

// A COUNTED ARRAY'S DEAD SLOTS ARE ZERO IN THE DART COOK WRITER
// (docs/SPEC-TABLES.md §7.2, §7.6; docs/rulings.json fixed-dead-bytes-are-zero):
// the slots past the live count hold the zero the region fill wrote, never the
// element's declared defaults. root_full and archive are counted arrays below
// their capacity; both are cooked through the generated writer in each byte
// order and compared byte for byte with the committed fixtures.

import (
	"bytes"
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestDartCookWriteZeroesDeadSlots(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart (it runs the generated cook writer)")
	dartBin := dartBinary(t)
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadDirUnit(t, filepath.Join(repo, "tables", "examples"))
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		instance, fromJSON, cook, library string
	}{
		{"root_full", "rootConfigFromJson", "RootConfigCookWire", "TablesCook.dart"},
		{"archive", "archiveConfigFromJson", "ArchiveConfigCookWire", "NestedCook.dart"},
	} {
		row := row
		t.Run(row.instance, func(t *testing.T) {
			t.Parallel()
			probe := strings.NewReplacer(
				"@LIB@", row.library,
				"@FROMJSON@", row.fromJSON,
				"@COOK@", row.cook,
			).Replace(cookZeroProbe)
			probePath := filepath.Join(dir, "cook_zero_"+row.instance+".dart")
			if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
				t.Fatal(err)
			}
			text := filepath.Join(repo, "testdata", "conformance", "tables", "json", row.instance+".json")
			cmd := exec.Command(dartBin, "--enable-asserts", probePath, text)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: the generated cook writer failed: %v\n%s", row.instance, err, out)
			}
			got := map[string][]byte{}
			for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
				order, text, ok := strings.Cut(line, " ")
				if !ok {
					t.Fatalf("%s: the probe printed %q", row.instance, line)
				}
				raw, err := hex.DecodeString(text)
				if err != nil {
					t.Fatalf("%s: the probe printed %q: %v", row.instance, line, err)
				}
				got[order] = raw
			}
			for _, order := range []struct{ name, suffix string }{
				{"le", ""},
				{"be", "-be"},
			} {
				want, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "cook-write", row.instance+order.suffix+".cook"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got[order.name], want) {
					at := 0
					for at < len(got[order.name]) && at < len(want) && got[order.name][at] == want[at] {
						at++
					}
					t.Fatalf("%s%s: the cook differs from the committed fixture at byte %d: got %d bytes, want %d", row.instance, order.suffix, at, len(got[order.name]), len(want))
				}
			}
		})
	}
}

const cookZeroProbe = `import 'dart:io';
import 'dart:typed_data';
import 'TabledemoWire.dart' as w;
import 'TabledemoCook.dart';
import '@LIB@' as c;
void main(List<String> args) {
  final report = w.TableTextReport();
  final value = w.@FROMJSON@(File(args[0]).readAsStringSync(), report);
  if (report.refused) {
    stderr.writeln('the committed text did not load');
    exit(1);
  }
  for (final order in [Endian.little, Endian.big]) {
    final size = c.@COOK@.cookMeasure(value);
    if (size <= 0) {
      stderr.writeln('the measure refused the value');
      exit(1);
    }
    final bytes = Uint8List(size);
    final written = TableCookWriteReport();
    if (!c.@COOK@.cook(value, bytes, order, report: written) || written.reason != 'ok') {
      stderr.writeln('the cook refused the value: ${written.reason}');
      exit(1);
    }
    final hex = bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    stdout.writeln('${order == Endian.little ? 'le' : 'be'} $hex');
  }
}
`
