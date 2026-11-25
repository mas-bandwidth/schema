package darttable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// depthWriteDartUnit is internal/tabletext/depth_write_test.go's
// depthWriteUnit: a chain of nodes, each holding one EMPTY container of a kind
// the writer always emits.
const depthWriteDartUnit = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

const depthWriteDartProbe = `import 'dart:io';
import 'DepthwriteWire.dart' as w;

void check(bool ok, String what) {
  if (!ok) throw Exception(what);
}

void probe<T>(String name, T Function() make, void Function(T, T?) link, String Function(T) toJson, bool Function(String) readsBack) {
  T chain(int nodes) {
    T? cur;
    for (var i = 0; i < nodes; i++) {
      final n = make();
      link(n, cur);
      cur = n;
    }
    final root = make();
    link(root, cur);
    return root;
  }
  final text = toJson(chain(127));
  check(readsBack(text), '$name: the 127-node text did not read back');
  var refused = false;
  try {
    toJson(chain(128));
  } on StateError catch (e) {
    check(e.message.contains('depth cap'), '$name: refused, but not by the depth cap: ${e.message}');
    refused = true;
  }
  check(refused, '$name: 128 nodes put the last container at depth 129 and the write did not refuse');
}

bool back(void Function(w.TableTextReport) read) {
  final r = w.TableTextReport();
  read(r);
  return !r.refused;
}

void main() {
  probe<w.ArrNode>('ArrNode', w.ArrNode.new, (n, next) => n.next = next, w.arrNodeToJson, (t) => back((r) => w.arrNodeFromJson(t, r)));
  probe<w.UnionNode>('UnionNode', w.UnionNode.new, (n, next) => n.next = next, w.unionNodeToJson, (t) => back((r) => w.unionNodeFromJson(t, r)));
  probe<w.MapNode>('MapNode', w.MapNode.new, (n, next) => n.next = next, w.mapNodeToJson, (t) => back((r) => w.mapNodeFromJson(t, r)));
  probe<w.KeyedNode>('KeyedNode', w.KeyedNode.new, (n, next) => n.next = next, w.keyedNodeToJson, (t) => back((r) => w.keyedNodeFromJson(t, r)));
  probe<w.FlagsNode>('FlagsNode', w.FlagsNode.new, (n, next) => n.next = next, w.flagsNodeToJson, (t) => back((r) => w.flagsNodeFromJson(t, r)));
  stdout.writeln('JSON_WRITE_DEPTH_OK');
}
`

// TestDartWriteDepthCapCountsEveryContainer: THE WRITE COUNTS EVERY CONTAINER
// THE READ DOES (docs/SPEC-TABLES.md §16.2, §16.14 W2), as the oracle's
// TestWriteDepthCapCountsEveryContainer. 127 nodes under the root put the last
// node's container at depth 128: the text writes and reads back. 128 nodes put
// it at 129: toJson throws StateError naming the cap, with asserts on and off.
func TestDartWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, depthWriteDartUnit)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(depthWriteDartProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	// both builds: the refusal is not a debug assert
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := testproc.Command(t, dartBinary(t), args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "JSON_WRITE_DEPTH_OK") {
			t.Fatalf("Dart probe %v: %v\n%s", args, err, out)
		}
	}
}
