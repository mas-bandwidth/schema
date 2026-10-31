package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestWireIdentityMapDart holds docs/PORTING.md M6's identity half on the
// Dart leg (docs/SPEC-TABLES.md §3.1): the save numbers nodes through an
// identity map, so a node two pointers reach is ONE record and reads back as
// one object, two distinct nodes of equal content stay two, and a pointer
// that reaches a node still OPEN on the walk is a cycle the save refuses by
// name rather than a shared reference it writes. The byte-for-byte half is
// the conformance wire surface (graph_shared, list_shared) and
// TestVariableClassBeyondCardConstructsDart.
//
// The reversed witness takes the OPEN-entry test out of the generated
// runtime, so a cycle numbers as an ordinary shared node, and requires the
// probe red on the cycle alone.
func TestWireIdentityMapDart(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := buildUnit(t, "package ring\n\ntable Node\n{\n    name string(8)\n    next *Node\n    other *Node\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dart := dartBinary(t)
	if out, err := runIdentityProbe(t, dart, files, packetFiles(t, u), nil); err != nil {
		t.Fatalf("the identity map probe failed: %v\n%s", err, out)
	}
	const open = "    if (open.contains(obj)) {\n"
	blind := func(f map[string][]byte) {
		for name, src := range f {
			if s := string(src); strings.Contains(s, open) {
				f[name] = []byte(strings.Replace(s, open, "    if (open.contains(obj) && obj is! Object) {\n", 1))
			}
		}
	}
	out, err := runIdentityProbe(t, dart, files, packetFiles(t, u), blind)
	if err == nil {
		t.Fatalf("the probe stayed green with the OPEN-entry test removed: the gate is blind\n%s", out)
	}
	if !strings.Contains(string(out), "a cycle saved") || strings.Contains(string(out), "shared") {
		t.Errorf("the reversed witness must fail on the cycle alone:\n%s", out)
	}
}

func runIdentityProbe(t *testing.T, dart string, files, packet map[string][]byte, edit func(map[string][]byte)) ([]byte, error) {
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
	if err := os.WriteFile(filepath.Join(dir, "identity_probe.dart"), []byte(identityProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "identity_probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const identityProbe = `import 'dart:io';
import 'RingWire.dart' as w;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    failed = true;
  }
}

w.Node node(String name) => w.Node()..name = name;

void main() {
  // ONE node two pointers reach is one record and one object on the read
  final leaf = node('leaf');
  final root = node('root')
    ..next = leaf
    ..other = leaf;
  final shared = w.nodeWireSave(root);
  final back = w.nodeWireLoad(shared);
  check(back.next != null && identical(back.next, back.other), 'a shared node did not read back as one object');
  // two distinct nodes of equal content stay two, and cost one record more
  final twins = node('root')
    ..next = node('leaf')
    ..other = node('leaf');
  final two = w.nodeWireSave(twins);
  final again = w.nodeWireLoad(two);
  check(again.next != null && again.other != null && !identical(again.next, again.other), 'two equal nodes were merged into one shared one');
  check(two.length > shared.length, 'the shared node was written more than once (${shared.length} bytes, twins ${two.length})');
  // A CYCLE IS REFUSED at the OPEN entry, by name
  final a = node('a');
  final b = node('b')..next = a;
  a.next = b;
  try {
    w.nodeWireSave(a);
    check(false, 'a cycle saved');
  } on StateError catch (e) {
    check(e.message.contains('data cycle'), 'a cycle was refused for another reason: ${e.message}');
  }
  final self = node('self');
  self.other = self;
  try {
    w.nodeWireSave(self);
    check(false, 'a cycle saved (a node naming itself)');
  } on StateError catch (e) {
    check(e.message.contains('data cycle'), 'a self cycle was refused for another reason: ${e.message}');
  }
  if (failed) exit(1);
}
`
