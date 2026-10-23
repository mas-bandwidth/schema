package darttable

// TestFixedNestedDefaultCheckIsTyped holds the fixed form's elision rule for a
// nested message field to docs/SPEC-TABLES.md §3.3: a field at its declared
// default elides, and a nested message's default is an ALL-DEFAULT BODY, never
// the int 0 an object does not equal. It is pure Go over the generated text of
// the block and examples schemas, so it is red at BASE on every machine,
// toolchain or not.
//
// The rule copied is the Go fixed emitter's
// (internal/codegen/gotable/messagewrite.go, emitMessageRides): measure the
// nested body and ride when it holds more than the terminator reference. The
// C++ reference writes the same `body > msgRefBits` test
// (internal/codegen/cpptable/messagecodec.go).

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedNestedDefaultCheckIsTyped(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"../../../tables/block", "../../../tables/examples"} {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			u := loadDirUnit(t, dir)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			nested := map[string]bool{}
			for _, f := range u.Files {
				for _, st := range f.Tables {
					for _, field := range st.Fields {
						if field.Type.Kind != ir.TNamed || field.IsList() || field.IsMap() {
							continue
						}
						if _, ok := field.Type.Ref.(*ir.Struct); ok {
							nested[dartName(field.Name)] = true
						}
					}
				}
			}
			if len(nested) == 0 {
				t.Fatalf("%s declares no nested message field, so the test would not pin the rule", dir)
			}

			typed := false
			for name, data := range files {
				src := string(data)
				for field := range nested {
					re := regexp.MustCompile(`\.` + regexp.QuoteMeta(field) + `\s*!=\s*0\b`)
					if m := re.FindString(src); m != "" {
						t.Errorf("%s: a nested message field is compared to the int 0 (%q), so the skip-the-default check is always true (docs/SPEC-TABLES.md §3.3)", name, m)
					}
				}
				if strings.Contains(src, "SaveMessageBody(probe") && strings.Contains(src, ".bits > tableMessageRefBits") {
					typed = true
				}
			}
			if !typed {
				t.Errorf("%s: the emitter writes no measuring-body default check for a nested message field (want a probe writer and `probe.bits > tableMessageRefBits`)", dir)
			}
		})
	}
}

// TestNestedDefaultMessageBytesMatchGoEmitter is the byte-equality test: a
// fixed table whose nested message field holds its default value, written by
// the Dart fixed form, must produce the same bytes as the Go emitter's output
// for the same value (docs/SPEC-TABLES.md §3.3). It runs the leg's own Dart
// toolchain; a missing toolchain is a skip by name unless
// SCHEMA_REQUIRE_CORPUS is set, which makes it a failure by name instead.
//
// The expected bytes are the GO EMITTER'S OWN LIVE PROGRAM, never a hand-written
// vector: this test generates the Go fixed library for the same schema, builds
// and runs a program that writes one default Root, and prints those bytes. The
// Dart probe writes the same value through the Dart fixed library, so the two
// emitters are compared directly.
func TestNestedDefaultMessageBytesMatchGoEmitter(t *testing.T) {
	t.Parallel()
	bin := nestedDefaultDartBinary(t)

	u := unitFrom(t, `package probe

fixed table Inner
{
    a int32 = 0
    b bool = false
}

fixed table Root
{
    tag  int32
    body Inner
}
`)

	want := goNestedDefaultMessageBytes(t, u)

	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate table: %v", err)
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
	home := runtimeHome(u) + "Fixed.dart"
	rootLib := fixedTableBase(u, "Root") + "Fixed.dart"
	probe := strings.NewReplacer(
		"@HOME@", home,
		"@ROOT@", rootLib,
	).Replace(`import 'dart:typed_data';

import './@HOME@' as h;
import './@ROOT@' as r;

void main() {
  final value = h.Root();
  h.initRoot(value);
  final values = <h.Root>[value];
  final measure = r.rootMeasureMessages(values, h.TableReport());
  final buffer = Uint8List(measure);
  final wrote = r.rootSaveMessages(values, buffer, h.TableReport());
  if (wrote != measure) { throw StateError('save $wrote != $measure'); }
  final out = StringBuffer();
  for (final b in buffer) {
    out.write(b.toRadixString(16).padLeft(2, '0'));
  }
  print('BYTES ' + out.toString());
}
`)
	if err := os.WriteFile(filepath.Join(dir, "nested_default_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--enable-asserts", "nested_default_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the nested-default probe did not run green: %v\n%s", err, out)
	}
	line := ""
	for l := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(l, "BYTES "); ok {
			line = strings.TrimSpace(after)
		}
	}
	if line == "" {
		t.Fatalf("the nested-default probe printed no bytes:\n%s", out)
	}
	got, err := hex.DecodeString(line)
	if err != nil {
		t.Fatalf("probe bytes %q: %v", line, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the Dart fixed form wrote the default nested message field: got %x, the Go emitter writes %x (docs/SPEC-TABLES.md §3.3)", got, want)
	}
}

// nestedDefaultDartBinary resolves the leg's Dart without skipping before the
// test can say why: an absent SDK is a SKIP by name, and under
// SCHEMA_REQUIRE_CORPUS (every make leg sets it) it is a FAILURE by name.
func nestedDefaultDartBinary(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("DART"); env != "" {
		return env
	}
	if dart, err := filepath.Abs("../../../dist/dart-sdk-3.13.2/bin/dart"); err == nil {
		if _, err := os.Stat(dart); err == nil {
			return dart
		}
	}
	if dart, err := exec.LookPath("dart"); err == nil {
		return dart
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no Dart toolchain is present: set DART, unpack dist/dart-sdk-3.13.2, or put dart on the PATH")
	}
	t.Skip("no Dart toolchain: set DART, unpack dist/dart-sdk-3.13.2, or put dart on the PATH")
	return ""
}

// goNestedDefaultMessageBytes generates the Go fixed library for u, compiles
// and runs it in a temporary module, and returns the bytes its message form
// writes for one Root with every field at its default. The generated program
// names the same value the Dart probe builds, so the comparison is between the
// two emitters rather than between Dart and a constant.
func goNestedDefaultMessageBytes(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go packet: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go table: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["bytes_test.go"] = []byte(`package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestBytes(t *testing.T) {
	var value Root
	RootReset(&value)
	buffer := make([]byte, RootMeasureMessages([]*Root{&value}, nil))
	n := RootSaveMessages([]*Root{&value}, buffer, nil)
	if n < 0 {
		panic("save refused")
	}
	fmt.Printf("BYTES %s\n", hex.EncodeToString(buffer[:n]))
}
`)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestBytes$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Go nested-default writer did not run green: %v\n%s", err, out)
	}
	line := ""
	for l := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(l, "BYTES "); ok {
			line = strings.TrimSpace(after)
		}
	}
	if line == "" {
		t.Fatalf("the Go nested-default writer printed no bytes:\n%s", out)
	}
	got, err := hex.DecodeString(line)
	if err != nil {
		t.Fatalf("Go bytes %q: %v", line, err)
	}
	return got
}
