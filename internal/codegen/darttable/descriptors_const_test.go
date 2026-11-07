package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDartDescriptorsAreConstants holds docs/PORTING.md M13 on the Dart leg:
// every reflection descriptor is a compile-time constant, so a read allocates
// nothing, runs no lazy initializer and cannot write. Dart initializes a
// top-level or static `final` lazily, behind a check on every access, and a
// `final` collection is still a mutable one; only `const` is built by the
// compiler and frozen. The descriptors are the wire library's card registry
// (`cardDescriptors()`, TableTypeInfo and TableFieldInfo), the unit view
// (`UnitView()`), the extent scan's field and arm tables and the retained-id
// set, beside the block and cook descriptors, which are `static const`
// members of their own classes. A field's storage accessors are static
// tear-offs, which are constants, where a closure would not be.
//
// The fixed form's layout, prefill, cover and identity tables are typed lists
// (Uint8List, Int32List), which Dart cannot make constant; they are built once
// from a const literal and are the named exception.
//
// The source half reads the generated text for the declarations; the run half
// compiles a probe that requires each registry identical across reads and
// unmodifiable, and that the accessors reach storage. The reversed witness
// turns the card registry and the unit view back into finals in a copy of the
// generated source and requires the run half red.
func TestDartDescriptorsAreConstants(t *testing.T) {
	t.Parallel()
	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "pointers", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no tables/pointers schemas: %v", err)
	}
	u := loadUnit(t, schemas...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["GraphdemoWire.dart"])
	view := string(files["GraphdemoView.dart"])
	if wire == "" || view == "" {
		t.Fatal("tables/pointers generated no GraphdemoWire.dart or GraphdemoView.dart")
	}
	for _, check := range []struct{ source, fragment, what string }{
		{wire, "const TableFieldInfo(", "TableFieldInfo has no const constructor"},
		{wire, "const TableTypeInfo(", "TableTypeInfo has no const constructor"},
		{wire, "static const Map<String, TableTypeInfo> _all = <String, TableTypeInfo>{", "the card registry is not one const map"},
		{wire, "Map<String, TableTypeInfo> cardDescriptors() => TableTypeInfo._all;", "cardDescriptors() does not answer the const registry"},
		{wire, "const _measureStructsScene = <Map<int, _MF>>[", "the extent scan's field table is not const"},
		{wire, "const _measureUnionsScene = <Map<int, _MF>>[", "the extent scan's arm table is not const"},
		{wire, "const Set<int> _retainKnown = <int>{", "the retained-id set is not const"},
		{view, "const ViewField(", "ViewField has no const constructor"},
		{view, "const UnitViewInfo _unitView = UnitViewInfo(", "the unit view is not const"},
	} {
		if !strings.Contains(check.source, check.fragment) {
			t.Error(check.what)
		}
	}
	// no descriptor row builds a closure: a closure is a fresh object and is
	// never a constant, so every accessor is a static member's tear-off
	for name, source := range map[string]string{"wire": wire, "view": view} {
		if strings.Count(source, "(dynamic v) =>") != strings.Count(source, "static dynamic _r") {
			t.Errorf("the %s library still builds a closure for a descriptor accessor", name)
		}
	}

	t.Run("dart", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart")
		dart := dartBinary(t)
		if out, err := runDescriptorProbe(t, dart, files, packetFiles(t, u), nil); err != nil {
			t.Fatalf("the descriptor probe failed: %v\n%s", err, out)
		}
		// THE REVERSED WITNESS: the same registries as finals compile and read
		// the same, and the probe must see them writable
		sabotage := func(f map[string][]byte) {
			f["GraphdemoWire.dart"] = []byte(strings.Replace(string(f["GraphdemoWire.dart"]),
				"static const Map<String, TableTypeInfo> _all", "static final Map<String, TableTypeInfo> _all", 1))
			f["GraphdemoView.dart"] = []byte(strings.Replace(string(f["GraphdemoView.dart"]),
				"const UnitViewInfo _unitView", "final UnitViewInfo _unitView", 1))
		}
		out, err := runDescriptorProbe(t, dart, files, packetFiles(t, u), sabotage)
		if err == nil {
			t.Fatalf("the probe stayed green with the registries as finals: the gate is blind\n%s", out)
		}
		for _, want := range []string{"cardDescriptors() is writable", "UnitView() is writable"} {
			if !strings.Contains(string(out), want) {
				t.Errorf("the reversed witness did not report %q:\n%s", want, out)
			}
		}
	})
}

// runDescriptorProbe writes the generated libraries, the packet libraries
// they import, and the probe into one directory, applies edit to the
// generated set, and runs the probe under asserts.
func runDescriptorProbe(t *testing.T, dart string, files, packet map[string][]byte, edit func(map[string][]byte)) ([]byte, error) {
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
	if err := os.WriteFile(filepath.Join(dir, "descriptors_probe.dart"), []byte(descriptorProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "descriptors_probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const descriptorProbe = `import 'dart:io';
import 'GraphdemoWire.dart' as w;
import 'GraphdemoView.dart' as v;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    failed = true;
  }
}

bool writable(void Function() write) {
  try {
    write();
    return true;
  } on UnsupportedError {
    return false;
  }
}

void main() {
  final d = w.cardDescriptors();
  check(identical(d, w.cardDescriptors()), 'cardDescriptors() builds a new registry per read');
  check(d.isNotEmpty, 'cardDescriptors() is empty');
  final scene = d['Scene'];
  check(scene != null, 'cardDescriptors() lacks Scene');
  check(!writable(() => d.remove('Scene')), 'cardDescriptors() is writable');
  if (scene != null) {
    check(!writable(() => scene.fields.clear()), 'a TableTypeInfo field list is writable');
    // the accessors reach storage: every field reads back what it writes
    final value = w.Scene();
    for (final f in scene.fields) {
      final x = f.read(value);
      f.write(value, x);
      check(identical(f.read(value), x), 'Scene.${f.name} does not read back its write');
    }
  }
  final u = v.UnitView();
  check(identical(u, v.UnitView()), 'UnitView() builds a new view per read');
  check(!writable(() => u.tables.clear()), 'UnitView() is writable');
  for (final t in u.tables) {
    check(!writable(() => t.fields.clear()), 'UnitView() ${t.name} field list is writable');
  }
  if (failed) exit(1);
}
`
