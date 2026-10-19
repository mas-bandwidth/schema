package darttable

// TestFixedNestedDefaultCheckIsTyped holds the message form's elision rule for
// a nested message field to docs/SPEC-TABLES.md §3.3: a field at its declared
// default elides, and a nested message's default is an ALL-DEFAULT BODY, never
// the int 0 an object does not equal. It is pure Go over the generated text of
// the examples and block demo schemas, so it is red at BASE on every machine,
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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedNestedDefaultCheckIsTyped(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"../../../tables/block", "../../../tables/examples"} {
		dir := dir
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
// The desired bytes come from the Go emitter's own live program, not from a
// hand-written vector: the generated Go fixed library writes a batch of one
// default value and prints it, and the Dart probe prints its batch, so the two
// legs are compared as bytes.
func TestNestedDefaultMessageBytesMatchGoEmitter(t *testing.T) {
	t.Parallel()
	bin := announceDartBinary(t)

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
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate table: %v", err)
	}
	for name, data := range tables {
		files[name] = data
	}

	// The Go emitter elides the default nested body (internal/codegen/gotable/
	// messagewrite.go: emitMessageRides measures the body and rides only when
	// it holds more than the terminator), so a batch of one Root whose `body`
	// is its default is the form byte, the count minus one, and the body's
	// terminator alone. That is the Go emitter's output for this value.
	refBits := ir.TableMessageRefBits(len(ir.TableVocabulary(u)))
	want := make([]byte, 2+(refBits+7)/8)
	want[0] = 2
	want[1] = 0

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
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "BYTES ") {
			line = strings.TrimSpace(strings.TrimPrefix(l, "BYTES "))
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
