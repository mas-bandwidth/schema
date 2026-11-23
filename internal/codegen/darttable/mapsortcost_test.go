package darttable

import (
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A MAP'S SORT COSTS O(n log n) WHATEVER ORDER ITS KEYS ARRIVE IN
// (docs/SPEC-TABLES.md §2.8, the cost row: the sort runs once per map; review
// 2026-11-21 DART-1). The JSON read takes a map's members in the order the
// document sends them and sorts them; the writer sorts a builder-filled map.
// Both sorted with an insertion sort whose string compare encoded both keys
// to UTF-8 on every comparison, so a document with its keys in DESCENDING
// order cost n(n-1)/2 compares and twice as many allocations.

const mapSortCostSchema = "package book\n\ntable Book\n{\n    names map[string(16)]int32\n    ids map[int32]int32\n    marks map[uint32]int32\n}\n"

// sortCallRE matches every call of the map sort and of the JSON map read in
// the emitted Dart, up to the end of its argument list's first line.
var sortCallRE = regexp.MustCompile(`(_stableSortedBy|_jsonMap)<[^(\n]*>\([^\n]*`)

// TestDartMapSortIsMergeSortOverKeysBuiltOnce is the fast half: the emitted
// runtime carries no insertion sort, every map sort names its key builder and
// its key compare, and no compare handed to a sort encodes a key itself.
func TestDartMapSortIsMergeSortOverKeysBuiltOnce(t *testing.T) {
	t.Parallel()
	files, err := Generate(buildUnit(t, mapSortCostSchema))
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["BookWire.dart"])
	if wire == "" {
		t.Fatal("no BookWire.dart emitted")
	}
	if strings.Contains(wire, "_stableSorted<") || strings.Contains(wire, "while (j >= 0 && cmp(") {
		t.Error("the runtime still carries the insertion sort")
	}
	if !strings.Contains(wire, "List<T> _stableSortedBy<T, K>(List<T> l, K Function(T) keyOf, int Function(K a, K b) cmp) {") {
		t.Error("the runtime has no _stableSortedBy over keys built once")
	}
	calls := sortCallRE.FindAllString(wire, -1)
	want := map[string]int{
		"(BookNamesEntry e) => utf8.encode(e.key), _bytesCompare": 0,
		"(BookIdsEntry e) => e.key, _intCompare":                  0,
		"(BookMarksEntry e) => e.key, _u64Compare":                0,
	}
	for _, c := range calls {
		if strings.Contains(c, "_bytesCompare(utf8.encode(") {
			t.Errorf("a sort's compare encodes its keys on every comparison: %s", c)
		}
		for k := range want {
			if strings.Contains(c, k) {
				want[k]++
			}
		}
	}
	for k, n := range want {
		// the writer's sort (save, and the numbering walk) and the JSON read
		if n < 2 {
			t.Errorf("%q: %d sort calls, want the writer's and the JSON read's", k, n)
		}
	}
}

// TestDartMapSortCountsCompares is the run half (behind the slow gate): the
// generated key compare is instrumented to count, and a map of n keys sent in
// DESCENDING order is read from JSON and saved from a builder. Each costs at
// most n * ceil(log2 n) compares; the insertion sort cost n(n-1)/2. It counts
// compares, never time.
func TestDartMapSortCountsCompares(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	files, err := Generate(buildUnit(t, mapSortCostSchema))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runMapSortCost(t, files); err != nil {
		t.Fatalf("the map sort is not O(n log n) on descending keys: %v\n%s", err, out)
	}
}

// mapSortCostN is the probe's map size: large enough that the quadratic sort
// (n(n-1)/2 = 8386560 compares) and the bound (n * 12 = 49152) are three
// orders apart, small enough that even the quadratic sort finishes.
const mapSortCostN = 4096

func runMapSortCost(t *testing.T, files map[string][]byte) ([]byte, error) {
	t.Helper()
	set := map[string][]byte{}
	for name, src := range files {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	const compare = "int _bytesCompare(List<int> a, List<int> b) {\n"
	wire := string(set["BookWire.dart"])
	if strings.Count(wire, compare) != 1 {
		t.Fatalf("the emitted key compare: %d occurrences of %q", strings.Count(wire, compare), compare)
	}
	set["BookWire.dart"] = []byte(strings.Replace(wire, compare, "int probeCompares = 0;\n"+compare+"  probeCompares++;\n", 1))
	dir := t.TempDir()
	writeFiles(t, dir, set)
	bound := mapSortCostN * bits.Len(uint(mapSortCostN-1))
	probe := fmt.Sprintf(mapSortCostProbe, mapSortCostN, bound)
	if err := os.WriteFile(filepath.Join(dir, "map_sort_cost.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", "map_sort_cost.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const mapSortCostProbe = `import 'dart:io';
import 'BookWire.dart' as w;

const n = %d;
const bound = %d;

String key(int i) => 'k' + i.toString().padLeft(6, '0');

void main() {
  var failed = false;
  // the JSON read: n string keys, written in descending order
  final text = StringBuffer('{"names":{');
  for (var i = n - 1; i >= 0; i--) {
    text.write('"${key(i)}":$i');
    if (i > 0) {
      text.write(',');
    }
  }
  text.write('}}');
  w.probeCompares = 0;
  final r = w.TableTextReport();
  final v = w.bookFromJson(text.toString(), r);
  final readCompares = w.probeCompares;
  stdout.writeln('json read: $n descending keys, $readCompares compares (bound $bound)');
  if (r.refused) {
    stderr.writeln('FAIL: the read refused: ${r.reason}');
    failed = true;
  }
  if (readCompares > bound) {
    stderr.writeln('FAIL: the JSON read made $readCompares compares for $n keys, over n log2 n = $bound');
    failed = true;
  }
  if (v.names.length != n) {
    stderr.writeln('FAIL: ${v.names.length} entries landed, want $n');
    failed = true;
  } else {
    for (var i = 0; i < n; i++) {
      if (v.names[i].key != key(i) || v.names[i].value != i) {
        stderr.writeln('FAIL: entry $i is ${v.names[i].key}=${v.names[i].value}, want ${key(i)}=$i');
        failed = true;
        break;
      }
    }
  }
  // the writer: a builder-filled map in descending order
  final built = w.Book();
  for (var i = n - 1; i >= 0; i--) {
    built.names.add(w.BookNamesEntry()
      ..key = key(i)
      ..value = i);
  }
  w.probeCompares = 0;
  final saved = w.bookWireSave(built);
  final saveCompares = w.probeCompares;
  stdout.writeln('save: $n descending keys, $saveCompares compares (bound ${2 * bound})');
  // the save sorts once for the numbering walk and once to write
  if (saveCompares > 2 * bound) {
    stderr.writeln('FAIL: the save made $saveCompares compares for $n keys, over 2 n log2 n = ${2 * bound}');
    failed = true;
  }
  final back = w.bookWireLoad(saved);
  if (back.names.length != n || back.names.first.key != key(0) || back.names.last.key != key(n - 1)) {
    stderr.writeln('FAIL: the saved map does not read back ascending');
    failed = true;
  }
  if (failed) {
    exit(1);
  }
}
`
