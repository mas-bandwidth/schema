package darttable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE STRICT READ'S ONE TYPED REASON (docs/SPEC-TABLES.md §6.5, §11):
// cfgWireReadStrict answers the value and ok for a clean wire and, for every
// other wire, null and the one reason TableWireReport.err() decides, in the
// precedence the reference legs use. The wires are the conformance corpus's
// own two generations of Cfg, read by the first: its own wire reads, the
// second generation's clamps and kind-mismatches, a cut file and an empty one
// are damage, and a form byte this read does not carry is unknown_form
// whichever form it is. The tolerant cfgWireLoad still reads the clamped and
// kind-mismatched wires, so the probe also holds the strict read apart from
// it, and a report handed in dirty describes the strict read alone.
func TestDartStrictReadReason(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart SDK (it runs the generated Dart)")
	dart := dartBinary(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(repo, "test/tables/V1.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	home := runtimeHome(u)
	if _, ok := files[home+"Strict.dart"]; !ok {
		t.Fatalf("Generate emitted no %sStrict.dart", home)
	}
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	own := wire("v1_cfg.bin")
	cut := append([]byte(nil), own[:len(own)-1]...)
	newer := append([]byte{9}, own[1:]...)
	message := append([]byte{2}, own[1:]...)
	cases := []struct {
		name     string
		wire     []byte
		reason   string
		tolerant bool
	}{
		{"own", own, "ok", true},
		{"clamped", wire("v2_cfg.bin"), "clamped", true},
		{"kind_mismatch", wire("v2_seams.bin"), "kind_mismatch", true},
		{"cut_id_table", cut, "wire_damaged", false},
		{"empty", nil, "wire_damaged", false},
		{"newer_form", newer, "unknown_form", false},
		{"message_form", message, "unknown_form", false},
	}
	var probe strings.Builder
	fmt.Fprintf(&probe, `import 'dart:typed_data';

import '%[1]sStrict.dart';
import '%[1]sWire.dart';

Uint8List bytes(String s) => Uint8List.fromList([
  for (var i = 0; i < s.length; i += 2) int.parse(s.substring(i, i + 2), radix: 16),
]);

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}

void main() {
`, home)
	for _, c := range cases {
		fmt.Fprintf(&probe, `  {
    final wire = bytes(%[1]q);
    final (value, why) = cfgWireReadStrict(wire);
    check(why.word == %[2]q, '%[3]s strict: ${why.word}');
    check((value != null) == (why == TableRefuseReason.ok), '%[3]s value');
    final dirty = TableWireReport()
      ..malformed = true
      ..refused = true
      ..clamped = 3
      ..kindMismatch = 2
      ..blobShared = 1;
    final (_, again) = cfgWireReadStrict(wire, dirty);
    check(again == why, '%[3]s reused report: ${again.word}');
    check((dirty.err()?.word ?? 'ok') == %[2]q, '%[3]s err');
    final tolerant = TableWireReport();
    cfgWireLoad(wire, tolerant);
    check((!tolerant.malformed && !tolerant.refused) == %[4]v, '%[3]s tolerant');
  }
`, hex.EncodeToString(c.wire), c.reason, c.name, c.tolerant)
	}
	probe.WriteString("}\n")
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "strict_probe.dart"), []byte(probe.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "strict_probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dart strict probe: %v\n%s", err, out)
	}
}
