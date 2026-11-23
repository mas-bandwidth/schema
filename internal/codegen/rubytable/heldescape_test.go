package rubytable

// KIND 31 IS HELD BY NAME AND NEVER WRITTEN (docs/SPEC-TABLES.md §3, R17: the
// newer reader reads old data, never the other way). The escape an older
// reader once skipped by its L is gone, so the Ruby reader meets 31 only as
// damage: at a field it cannot name and at one it declares, at an arm it
// cannot name and at one it declares, under the plain and the retaining load,
// and in a message announcement, exactly as it meets 33 and 34. Red on the old
// runtime, which skipped an unnamed 31 by its L and counted it unknown.

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const heldEscapeSchema = `package heldprobe
union Pick
{
    a int32
    b string(8)
}
table Root
{
    n int32
    p Pick
}
fixed table Msg { x int32 }
`

// heldWire is a variable-form file: the form byte, the body and its
// terminator, the id table and its count.
func heldWire(body []byte, ids ...uint64) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	out = append(out, 0)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func heldLeb(n int) []byte {
	var out []byte
	for n >= 0x80 {
		out = append(out, byte(n&0x7f)|0x80)
		n >>= 7
	}
	return append(out, byte(n))
}

// heldAnnouncement is a message announcement carrying one vocabulary entry.
func heldAnnouncement(entry []byte) []byte {
	payload := append([]byte{byte(ir.TableKindU8)}, heldLeb(len(entry))...)
	payload = append(payload, entry...)
	body := binary.LittleEndian.AppendUint64([]byte{1, byte(ir.TableKindU64)}, 123)
	body = append(body, 2, byte(ir.TableKindArray))
	body = append(body, heldLeb(len(payload))...)
	body = append(body, payload...)
	return heldWire(body, 0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD)
}

func heldEntry(kind byte, shape ...byte) []byte {
	return append(append(binary.LittleEndian.AppendUint64(nil, 17), kind), shape...)
}

const heldEscapeProbe = `# frozen_string_literal: true

require_relative "heldprobe"

def refused(name)
  yield
  raise "#{name}: read"
rescue Heldprobe::Refused => e
  raise "#{name}: refused #{e.reason}, not wire_damaged" unless e.reason == "wire_damaged"
end

raise "named control" unless Heldprobe::Root.load(File.binread("named_control.bin")).n == 7
raise "arm control" unless Heldprobe::Root.load(File.binread("arm_control.bin")).p.a == 7
%w[unnamed unnamed_empty named arm_named arm_unnamed].each do |name|
  wire = File.binread("#{name}.bin")
  refused("load #{name}") { Heldprobe::Root.load(wire) }
  refused("retain #{name}") { Heldprobe::Root.load_retain(wire, Heldprobe::TableRetain.new) }
end
Heldprobe.announce_read(Heldprobe::MessageVocabulary.new, File.binread("ann_control.bin"))
%w[ann_31 ann_33 ann_34].each do |name|
  refused(name) { Heldprobe.announce_read(Heldprobe::MessageVocabulary.new, File.binread("#{name}.bin")) }
end
puts "held"
`

func TestRubyKindHeldEscapeRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	files, err := Generate(loadUnit(t, heldEscapeSchema))
	if err != nil {
		t.Fatal(err)
	}
	n, p, a, zzz := ir.TableWireId("n"), ir.TableWireId("p"), ir.TableWireId("a"), ir.TableWireId("zzz")
	esc, i32, union := byte(ir.TableKindHeldEscape), byte(ir.TableKindI32), byte(ir.TableKindUnion)
	inputs := map[string][]byte{
		"main.rb":           []byte(heldEscapeProbe),
		"named_control.bin": heldWire([]byte{1, i32, 7, 0, 0, 0}, n),
		"arm_control.bin":   heldWire([]byte{1, union, 2, i32, 4, 7, 0, 0, 0}, p, a),
		"unnamed.bin":       heldWire([]byte{1, esc, 2, 0xAA, 0xBB}, zzz),
		"unnamed_empty.bin": heldWire([]byte{1, esc, 0}, zzz),
		"named.bin":         heldWire([]byte{1, esc, 0}, n),
		"arm_named.bin":     heldWire([]byte{1, union, 2, esc, 0}, p, a),
		"arm_unnamed.bin":   heldWire([]byte{1, union, 2, esc, 2, 0xAA, 0xBB}, p, zzz),
		"ann_control.bin":   heldAnnouncement(heldEntry(i32, 1, 4, 7)),
		"ann_31.bin":        heldAnnouncement(heldEntry(esc)),
		"ann_33.bin":        heldAnnouncement(heldEntry(byte(ir.TableKindRetiredWstring))),
		"ann_34.bin":        heldAnnouncement(heldEntry(byte(ir.TableKindReservedFloat16))),
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out := strings.TrimSpace(runBothRubyPins(t, dir, "main.rb", inputs)); out != "held" {
		t.Fatalf("kind 31 was read where it is damage: %s", out)
	}
}
