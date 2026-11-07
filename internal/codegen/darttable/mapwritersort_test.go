package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMapWriterSortDart holds docs/PORTING.md M19's writer half on the Dart
// leg: a map is a sorted entry array (docs/SPEC-TABLES.md §2.8), and the
// WRITER holds the sort, so a map built out of key order saves, numbers and
// writes text in ascending key order. A Dart map field is a List of generated
// entries in whatever order the caller appended them, so nothing upstream of
// the writer sorts it. Two values holding the same entries in two orders must
// save the same bytes and write the same text, and both must read back
// ascending, for a string key (UTF-8 byte order) and a signed integer key.
//
// Its reversed witness is TestMapWriterSortDartNegativeControl.
func TestMapWriterSortDart(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := buildUnit(t, mapSortProbeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dart := dartBinary(t)
	if out, err := runMapSortProbe(t, dart, files, packetFiles(t, u), nil); err != nil {
		t.Fatalf("the map writer does not hold the sort: %v\n%s", err, out)
	}
}

// TestMapWriterSortDartNegativeControl is the gate's negative control
// (tables-dart-map-sort-negative-control): the generated runtime's
// _stableSorted returns its input in stored order, and the same probe must go
// red on the saved bytes.
func TestMapWriterSortDartNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := buildUnit(t, mapSortProbeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dart := dartBinary(t)
	unsorted := func(f map[string][]byte) {
		for name, src := range f {
			s := string(src)
			const sig = "List<T> _stableSorted<T>(List<T> l, int Function(T a, T b) cmp) {\n"
			if strings.Contains(s, sig) {
				f[name] = []byte(strings.Replace(s, sig, sig+"  if (l.isNotEmpty) return List<T>.from(l);\n", 1))
			}
		}
	}
	out, err := runMapSortProbe(t, dart, files, packetFiles(t, u), unsorted)
	if err == nil {
		t.Fatalf("the probe stayed green with the writer's sort removed: the gate is blind\n%s", out)
	}
	if !strings.Contains(string(out), "insertion order changed the saved bytes") {
		t.Errorf("the reversed witness did not fail on the saved bytes:\n%s", out)
	}
}

const mapSortProbeSchema = "package book\n\ntable Book\n{\n    names map[string(8)]int32\n    ids map[int32]int32\n}\n"

func runMapSortProbe(t *testing.T, dart string, files, packet map[string][]byte, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	set := map[string][]byte{}
	for name, src := range packet {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	for name, src := range files {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	if edit != nil {
		edit(set)
	}
	dir := t.TempDir()
	writeFiles(t, dir, set)
	if err := os.WriteFile(filepath.Join(dir, "map_sort_probe.dart"), []byte(mapSortProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "map_sort_probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const mapSortProbe = `import 'dart:convert';
import 'dart:io';
import 'BookWire.dart' as w;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    failed = true;
  }
}

bool same(List<int> a, List<int> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

void main() {
  // the same entries, out of order and in order: values follow their keys
  final shuffled = w.Book();
  final ordered = w.Book();
  final names = <String, int>{'zed': 1, 'été': 2, 'Alpha': 3, 'mid': 4};
  final ids = <int, int>{5: 1, -3: 2, 0: 3, 2147483647: 4, -2147483648: 5};
  for (final k in names.keys) {
    shuffled.names.add(w.BookNamesEntry()..key = k..value = names[k]!);
  }
  for (final k in ids.keys) {
    shuffled.ids.add(w.BookIdsEntry()..key = k..value = ids[k]!);
  }
  final nameKeys = names.keys.toList()..sort((a, b) => _cmp(utf8.encode(a), utf8.encode(b)));
  for (final k in nameKeys) {
    ordered.names.add(w.BookNamesEntry()..key = k..value = names[k]!);
  }
  final idKeys = ids.keys.toList()..sort();
  for (final k in idKeys) {
    ordered.ids.add(w.BookIdsEntry()..key = k..value = ids[k]!);
  }
  final a = w.bookWireSave(shuffled);
  final b = w.bookWireSave(ordered);
  check(same(a, b), 'insertion order changed the saved bytes');
  check(w.bookToJson(shuffled) == w.bookToJson(ordered), 'insertion order changed the text');
  final back = w.bookWireLoad(a);
  check(back.names.map((e) => e.key).join(',') == nameKeys.join(','), 'the saved string keys do not read back ascending');
  check(back.ids.map((e) => e.key).join(',') == idKeys.join(','), 'the saved integer keys do not read back ascending');
  for (final e in back.names) {
    check(e.value == names[e.key], 'the string key ${e.key} lost its value');
  }
  for (final e in back.ids) {
    check(e.value == ids[e.key], 'the integer key ${e.key} lost its value');
  }
  if (failed) exit(1);
}

int _cmp(List<int> a, List<int> b) {
  final n = a.length < b.length ? a.length : b.length;
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
  }
  return a.length.compareTo(b.length);
}
`
