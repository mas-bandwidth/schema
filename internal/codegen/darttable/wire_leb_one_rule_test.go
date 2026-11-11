package darttable

// THE ONE LEB RULE (docs/SPEC-TABLES.md §3): a form-1 wire's every LEB128 is
// canonical, at most ten bytes, the tenth at most 1. The typed load reads that
// rule (TableWireReader.readLeb); each wire-alone walk — wireFramingBad,
// wireUnreachedNode, wireBlobShared and wireBadPointerIndex, built on
// _wireBody and _wirePayload — must read it by the same rule, so the whole
// port has one answer for one wire.
//
// These wires put a ten-byte kind-17 index and a non-minimal length where the
// load only skips a field and where it reads one, plus a non-minimal length in
// an unknown-type record body the typed load never opens. The Go engine
// (internal/tablewire) refuses each, and the emitted Dart must refuse each for
// the same reason, with the same wire-alone verdicts.
//
// Its reversed witness is TestDartWireWalksReadLebAsTheLoadNegativeControl.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
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

const wireLebSchema = `package probe
table Child { x uint64 }
table Root {
 child *Child
 s string(8)
}
`

// formOneWire frames a root body with its id table (§3): the form byte, the
// body (its terminator included), then the ids and their count.
func formOneWire(body []byte, ids []uint64) []byte {
	wire := []byte{1}
	wire = append(wire, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

type wireLebCase struct {
	name    string
	wire    []byte
	refused bool
	// The wire-alone verdicts the reference gives on the same bytes.
	framing, unreached, blobShared, badPointer bool
}

func buildWireLebCases(t *testing.T, u *ir.Unit) []wireLebCase {
	t.Helper()
	child := ir.TableWireId("child")
	str := ir.TableWireId("s")
	const unknownType = uint64(0x9988776655443322)
	const unknownField = uint64(0x1122334455667788)
	ids := []uint64{child, str, unknownType, unknownField, ir.TableNodeWireId}
	// 2^63 needs ten bytes: nine continuations and a tenth byte of 1.
	tens := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	// Zero's canonical spelling is one byte; 0x80 0x00 is the same value with
	// a redundant high group, which §3 makes malformed.
	nonMinimal := []byte{0x80, 0x00}
	field := func(ref, kind int, payload ...byte) []byte {
		return append([]byte{byte(ref), byte(kind)}, payload...)
	}
	recordBody := append(field(4, 12, nonMinimal...), 0)
	nodeTable := append([]byte{1, 3, byte(len(recordBody))}, recordBody...)
	withNodeTable := append(field(1, 17, 2), field(5, 12, byte(len(nodeTable)))...)
	withNodeTable = append(withNodeTable, nodeTable...)
	withNodeTable = append(withNodeTable, 0)
	cases := []wireLebCase{
		{
			name:    "ten-byte pointer index, unknown id",
			wire:    formOneWire(append(field(4, 17, tens...), 0), ids),
			framing: false, unreached: false, blobShared: false, badPointer: true,
		},
		{
			name:    "ten-byte pointer index, field the load reads",
			wire:    formOneWire(append(field(1, 17, tens...), 0), ids),
			framing: false, unreached: false, blobShared: false, badPointer: true,
		},
		{
			name:    "non-minimal length, unknown id",
			wire:    formOneWire(append(field(4, 12, nonMinimal...), 0), ids),
			framing: true, unreached: false, blobShared: false, badPointer: false,
		},
		{
			name:    "non-minimal length, field the load reads",
			wire:    formOneWire(append(field(2, 12, nonMinimal...), 0), ids),
			framing: true, unreached: false, blobShared: false, badPointer: false,
		},
		{
			name:    "non-minimal length in an unknown-type record body",
			wire:    formOneWire(withNodeTable, ids),
			framing: true, unreached: false, blobShared: false, badPointer: false,
		},
	}
	m := tabletext.NewModel(u)
	for i := range cases {
		v := m.New(u.Tables["Root"])
		var report tabletext.Report
		ok, err := tablewire.Decode(m, v, cases[i].wire, &report)
		cases[i].refused = !ok || err != nil || report.Refused
		if !cases[i].refused {
			t.Fatalf("%s: the reference accepts the crafted wire", cases[i].name)
		}
	}
	return cases
}

func runWireLebProbe(t *testing.T, files map[string][]byte, cases []wireLebCase) ([]byte, error) {
	t.Helper()
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, tc := range cases {
		fmt.Fprintf(&b, `{final data=bytes(%q);final r=w.TableWireReport();w.rootWireLoad(data,r);
check(r.refused==%t,'%s: load');
final reader=w.TableWireReader(data,data.length);reader.report=w.TableWireReport();reader.open();reader.scanNodeTable();
check(reader.wireFramingBad()==%t,'%s: framing');
check(reader.wireUnreachedNode()==%t,'%s: unreached node');
check(reader.wireBlobShared()==%t,'%s: blob shared');
check(reader.wireBadPointerIndex()==%t,'%s: bad pointer index');}
`, hex.EncodeToString(tc.wire), tc.refused, tc.name, tc.framing, tc.name, tc.unreached, tc.name, tc.blobShared, tc.name, tc.badPointer, tc.name)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestDartWireWalksReadLebAsTheLoad(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := unitFrom(t, wireLebSchema)
	cases := buildWireLebCases(t, u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	if out, err := runWireLebProbe(t, files, cases); err != nil {
		t.Fatalf("the LEB walk probe failed: %v\n%s", err, out)
	}
}

// TestDartWireWalksReadLebAsTheLoadNegativeControl puts the nine-byte,
// non-canonical reader back into the emitted source in memory and requires the
// probe red on the wires the one rule holds.
func TestDartWireWalksReadLebAsTheLoadNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := unitFrom(t, wireLebSchema)
	cases := buildWireLebCases(t, u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	if patched := revertWireLeb(files); patched == 0 {
		t.Fatal("no generated Wire.dart carried _wireLeb")
	}
	out, err := runWireLebProbe(t, files, cases)
	if err == nil {
		t.Fatalf("the probe stayed green with the nine-byte LEB reader restored:\n%s", out)
	}
	if !strings.Contains(string(out), "ten-byte pointer index, unknown id: framing") {
		t.Fatalf("the reversed witness did not fail the ten-byte LEB framing check:\n%s", out)
	}
}

// legacyWireLeb is the nine-byte reader this gate reverses to: it stops before
// the tenth byte and tests neither minimality nor the tenth byte's range.
const legacyWireLeb = `  (int, int)? _wireLeb(Uint8List buf, int off) {
    var v = 0;
    var shift = 0;
    var i = off;
    while (i < buf.length && shift < 63) {
      final b = buf[i];
      i++;
      v |= (b & 0x7F) << shift;
      if (b & 0x80 == 0) return (v, i);
      shift += 7;
    }
    return null;
  }
`

// revertWireLeb replaces the emitted _wireLeb method with the legacy reader and
// answers how many files it changed.
func revertWireLeb(files map[string][]byte) int {
	const sig = "  (int, int)? _wireLeb(Uint8List buf, int off) {"
	patched := 0
	for name, src := range files {
		s := string(src)
		i := strings.Index(s, sig)
		if i < 0 {
			continue
		}
		j := strings.Index(s[i:], "\n  }\n")
		if j < 0 {
			continue
		}
		files[name] = []byte(s[:i] + legacyWireLeb + s[i+j+len("\n  }\n"):])
		patched++
	}
	return patched
}
