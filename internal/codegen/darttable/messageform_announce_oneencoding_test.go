package darttable

// THE ANNOUNCEMENT'S ONE ENCODING (docs/SPEC-TABLES.md §3, §3.3). An
// announcement is an ordinary form-`1` file: its body ends at its zero
// reference and that terminator is the id table's first byte, and every LEB it
// carries is canonical. The generated Dart read must accept exactly the bytes
// the Go engine accepts, so a unit's own announcement is mutated one rule at a
// time and both readers are asked for the same verdict.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceTerminatorExact is the emitted lines that require a body's zero
// reference to sit exactly at the id table. The negative control removes them.
const announceTerminatorExact = "      if (at != bodyEnd) {\n        break;\n      }\n"

func TestDartAnnounceOneEncoding(t *testing.T) {
	t.Parallel()
	if err := checkAnnounceOneEncoding(t, dartMessageAnnounceRuntime); err != nil {
		t.Fatal(err)
	}
}

func TestDartAnnounceOneEncodingNegativeControl(t *testing.T) {
	t.Parallel()
	if !strings.Contains(dartMessageAnnounceRuntime, announceTerminatorExact) {
		t.Fatal("the emitted announcement read carries no terminator check to remove")
	}
	sabotaged := strings.Replace(dartMessageAnnounceRuntime, announceTerminatorExact, "", 1)
	if sabotaged == dartMessageAnnounceRuntime {
		t.Fatal("the terminator check was not removed")
	}
	if err := checkAnnounceOneEncoding(t, sabotaged); err == nil {
		t.Fatal("the main test stays green with the terminator check removed")
	}
}

// checkAnnounceOneEncoding drives one announcement surface: the unit's own
// announcement must be accepted by both readers, and each one-rule mutation
// must be refused by both, with no exception.
func checkAnnounceOneEncoding(t *testing.T, runtime string) error {
	t.Helper()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	valid := tablewire.Announce(u)

	cases := []struct {
		name   string
		wire   []byte
		accept bool
	}{
		{"valid", valid, true},
		{"padding_after_terminator", announceWithPaddingAfterBody(t, valid), false},
		{"non_minimal_length", announceWithNonMinimalFieldLength(t, valid), false},
		{"tenth_byte_length", announceWithTenthByteFieldLength(t, valid), false},
	}

	for _, c := range cases {
		var vocabulary tablewire.Vocabulary
		var report tabletext.Report
		err := vocabulary.AnnounceRead(c.wire, &report)
		if c.accept {
			if err != nil || !vocabulary.Announced() {
				return fmt.Errorf("Go refused the valid announcement: %v", err)
			}
			continue
		}
		if err == nil || vocabulary.Announced() {
			return fmt.Errorf("Go accepted the %s announcement", c.name)
		}
		if got := tablewire.RefusalReason(err); got != tablewire.ReasonWireDamaged {
			return fmt.Errorf("Go refused the %s announcement as %q, want %q", c.name, got, tablewire.ReasonWireDamaged)
		}
	}

	var body strings.Builder
	for _, c := range cases {
		if c.accept {
			fmt.Fprintf(&body, `
  {
    final v = t.TableVocabulary(entries: List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0)));
    final r = t.TableReport();
    check(t.announceRead(v, Uint8List.fromList(%s), r),
        'the valid announcement was refused: ${r.reason} refused=${r.refused}');
    check(v.announced, 'the accepted announcement is not marked announced');
  }
`, dartBytes(c.wire))
			continue
		}
		fmt.Fprintf(&body, `
  {
    final v = t.TableVocabulary(entries: List<t.TableMessageEntry>.generate(8192, (i) => t.TableMessageEntry(0, 0)));
    final r = t.TableReport();
    check(!t.announceRead(v, Uint8List.fromList(%s), r),
        'Dart accepted the %s announcement');
    check(r.refused, 'Dart did not set the refusal flag for the %s announcement');
    check(!v.announced, 'a refused %s announcement set a vocabulary');
  }
`, dartBytes(c.wire), c.name, c.name, c.name)
	}

	out, err := runAnnounceProbeRuntime(t, u, runtime, body.String())
	if err != nil {
		return fmt.Errorf("the emitted Dart announcement read: %v\n%s", err, out)
	}
	return nil
}

// announceBodyEnd is where the body ends and the id table begins, located the
// way the trailer rule does: from the end of the wire.
func announceBodyEnd(t *testing.T, wire []byte) int {
	t.Helper()
	if len(wire) < 9 {
		t.Fatalf("the announcement is %d bytes, shorter than its id table", len(wire))
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	span := int(count)*8 + 8
	if span+1 > len(wire) {
		t.Fatalf("the announcement's id table does not fit its %d bytes", len(wire))
	}
	return len(wire) - span
}

// announceWithPaddingAfterBody inserts one byte between the body's terminator
// and the id table. No field claims it.
func announceWithPaddingAfterBody(t *testing.T, wire []byte) []byte {
	t.Helper()
	end := announceBodyEnd(t, wire)
	out := make([]byte, 0, len(wire)+1)
	out = append(out, wire[:end]...)
	out = append(out, 0x00)
	out = append(out, wire[end:]...)
	return out
}

// announceWithNonMinimalFieldLength re-spells the vocabulary field's length
// with a trailing continuation group: the same value, one longer spelling.
func announceWithNonMinimalFieldLength(t *testing.T, wire []byte) []byte {
	t.Helper()
	off := announceVocabularyFieldLength(t, wire)
	_, next := readAnnounceLeb(t, wire, off)
	spelled := append([]byte{}, wire[off:next]...)
	spelled[len(spelled)-1] |= 0x80
	spelled = append(spelled, 0x00)
	return spliceAnnouncement(t, wire, off, next, spelled)
}

// announceWithTenthByteFieldLength re-spells the vocabulary field's length in
// ten bytes whose tenth carries a bit above the 64th value bit.
func announceWithTenthByteFieldLength(t *testing.T, wire []byte) []byte {
	t.Helper()
	off := announceVocabularyFieldLength(t, wire)
	_, next := readAnnounceLeb(t, wire, off)
	spelled := bytes.Repeat([]byte{0x80}, 9)
	spelled = append(spelled, 0x02)
	return spliceAnnouncement(t, wire, off, next, spelled)
}

func spliceAnnouncement(t *testing.T, wire []byte, off, next int, spelled []byte) []byte {
	t.Helper()
	out := make([]byte, 0, len(wire)-(next-off)+len(spelled))
	out = append(out, wire[:off]...)
	out = append(out, spelled...)
	out = append(out, wire[next:]...)
	return out
}

// announceVocabularyFieldLength is the offset of the vocabulary field's LEB
// length in the unit's own announcement.
func announceVocabularyFieldLength(t *testing.T, wire []byte) int {
	t.Helper()
	end := announceBodyEnd(t, wire)
	at := 1
	for at < end {
		ref, next := readAnnounceLeb(t, wire, at)
		at = next
		if ref == 0 {
			break
		}
		if at >= end {
			break
		}
		kind := wire[at]
		at++
		if kind == 14 {
			return at
		}
		if kind == 9 {
			at += 8
			continue
		}
		t.Fatalf("the announcement's body carries an unexpected kind %d", kind)
	}
	t.Fatal("the announcement's body carries no vocabulary field")
	return 0
}

func readAnnounceLeb(t *testing.T, wire []byte, at int) (uint64, int) {
	t.Helper()
	var value uint64
	for i, shift := 0, uint(0); i < 10; i, shift = i+1, shift+7 {
		if at >= len(wire) {
			t.Fatalf("an announcement LEB at %d runs off the wire", at)
		}
		b := wire[at]
		at++
		if i == 9 && b > 1 {
			t.Fatalf("an announcement LEB at %d has a tenth byte above 1", at-1)
		}
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			if i > 0 && b == 0 {
				t.Fatalf("an announcement LEB at %d is not minimal", at-1)
			}
			return value, at
		}
	}
	t.Fatalf("an announcement LEB at %d runs past ten bytes", at)
	return 0, 0
}

// runAnnounceProbeRuntime is runAnnounceProbe over an announcement runtime the
// caller hands in, so the negative control can execute a sabotaged surface.
func runAnnounceProbeRuntime(t *testing.T, u *ir.Unit, runtime, body string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	lib := "import 'dart:typed_data';\n\n" + runtime + "\n" + dartTableAnnouncementConstant(u)
	if err := os.WriteFile(filepath.Join(dir, "announce.dart"), []byte(lib), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`import 'dart:typed_data';

import './announce.dart' as t;

void check(bool ok, String what) {
  if (!ok) {
    throw StateError(what);
  }
}

void main() {
%s
  print('ok');
}
`, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(announceOneEncodingDartBinary(t), "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// announceOneEncodingDartBinary is the leg's own Dart. A missing toolchain is
// a failure when SCHEMA_REQUIRE_CORPUS promised one, and never a green.
func announceOneEncodingDartBinary(t *testing.T) string {
	slowtest.Gate(t, "an external toolchain")

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
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and the Dart SDK is not at dist/dart-sdk-3.13.2 or on the PATH")
	}
	t.Skip("no Dart toolchain: set DART, unpack dist/dart-sdk-3.13.2, or put dart on the PATH")
	return ""
}
