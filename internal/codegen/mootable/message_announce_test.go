package mootable_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT IS A FORM-1 FILE (docs/SPEC-TABLES.md §3.3, §4): every
// wire-alone condition the file form holds applies to it, inside the unknown
// fields it may gain too — skipped is not unread. moo's verdict is held to the
// Go engine's (docs/rulings.json refusal-verdict-only: the verdict, never a
// reason).

const announceUnknownId = uint64(0x5ca1ab1e5ca1ab1e)

// announceField is one unknown field at the id table's new last slot: the
// reference, the kind byte and the payload that follows it.
func announceField(kind byte, payload ...byte) []byte {
	out := binary.AppendUvarint(nil, 3)
	out = append(out, kind)
	return append(out, payload...)
}

func announceLen(n uint64, body ...byte) []byte {
	out := binary.AppendUvarint(nil, n)
	return append(out, body...)
}

// withUnknownAnnounceFields inserts fields before the body's terminator and
// appends their id to the announcement's own id table.
func withUnknownAnnounceFields(base []byte, fields ...[]byte) []byte {
	count := binary.LittleEndian.Uint64(base[len(base)-8:])
	idsEnd := len(base) - 8
	first := idsEnd - int(count)*8
	out := []byte{base[0]}
	out = append(out, base[1:first-1]...) // the body up to its terminator
	for _, f := range fields {
		out = append(out, f...)
	}
	out = append(out, 0)
	out = append(out, base[first:idsEnd]...)
	out = binary.LittleEndian.AppendUint64(out, announceUnknownId)
	return binary.LittleEndian.AppendUint64(out, count+1)
}

type announceCase struct {
	name   string
	wire   []byte
	accept bool
}

// announceCases is the unit's own announcement with one unknown field added,
// each breaking one wire-alone condition, and one well-formed unknown field.
func announceCases(base []byte) []announceCase {
	nan := []byte{0, 0, 0xc0, 0x7f}
	// an array whose one element is a u64 cut short inside its own L
	shortElement := append(announceLen(6), 9, 1, 0, 0, 0, 0)
	return []announceCase{
		{"ill_formed_text", withUnknownAnnounceFields(base, announceField(12, announceLen(2, 0xff, 0xfe)...)), false},
		{"repeated_field_id", withUnknownAnnounceFields(base, announceField(1, 1), announceField(1, 1)), false},
		{"payload_width", withUnknownAnnounceFields(base, announceField(14, shortElement...)), false},
		{"bool_byte", withUnknownAnnounceFields(base, announceField(1, 2)), false},
		{"nested_after_terminator", withUnknownAnnounceFields(base, announceField(13, announceLen(2, 0, 0)...)), false},
		{"non_finite_float", withUnknownAnnounceFields(base, announceField(10, nan...)), false},
		{"well_formed_unknown", withUnknownAnnounceFields(base, announceField(12, announceLen(2, 'h', 'i')...)), true},
	}
}

func announceReadProbe(g *generatedUnit) string {
	return `--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
` + g.requireAll() + `
local cases = inputs.cases
local at = 0
while at < buffer.len(cases) do
	local len = buffer.readu32(cases, at)
	local data = buffer.create(len)
	buffer.copy(data, 0, cases, at + 4, len)
	at += 4 + len
	local v = MS.NewVocabulary()
	local rep: any = {}
	if MS.AnnounceRead(v, data, rep) then
		print("accept")
	else
		print("refuse")
	end
end
`
}

func announcePackage(t *testing.T) *generatedUnit {
	t.Helper()
	return generateFiles(t, map[string]string{"Announce.schema": `package announceprobe

fixed table Probe { x int32 }
`})
}

func TestMooAnnounceReadHoldsTheWireAloneRules(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g := announcePackage(t)
	cases := announceCases(tablewire.Announce(g.unit))
	packed := make([][]byte, len(cases))
	for i, c := range cases {
		packed[i] = c.wire
	}
	out := g.run(t, announceReadProbe(g), map[string][]byte{"cases": packCases(packed)})
	lines := strings.Split(out, "\n")
	if len(lines) != len(cases) {
		t.Fatalf("moo answered %d of %d announcements:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		var v tablewire.Vocabulary
		var rep tabletext.Report
		goOK := v.AnnounceRead(c.wire, &rep) == nil && v.Announced()
		if goOK != c.accept {
			t.Fatalf("%s: the Go engine's verdict is accept=%t, the case says accept=%t", c.name, goOK, c.accept)
		}
		if got := strings.TrimSpace(lines[i]) == "accept"; got != goOK {
			t.Errorf("%s: moo says %q, the Go engine says accept=%t", c.name, lines[i], goOK)
		}
	}
}

func TestMooAnnounceReadHoldsTheWireAloneRulesNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g := announcePackage(t)
	cases := announceCases(tablewire.Announce(g.unit))
	packed := make([][]byte, len(cases))
	for i, c := range cases {
		packed[i] = c.wire
	}
	inputs := map[string][]byte{"cases": packCases(packed)}
	const anchor = "\tlocal wireFail, wireNonFinite = L.wireCheckAnnouncement(data, 1, start, ids)\n" +
		"\tif wireFail ~= nil or wireNonFinite then\n" +
		"\t\treport.damaged = true\n" +
		"\t\treturn false\n" +
		"\tend\n"
	src := string(g.files["table_message.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the wire-alone walk's anchor is not in table_message.moo exactly once")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), []byte(strings.Replace(src, anchor, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := g.run(t, announceReadProbe(g), inputs)
	lines := strings.Split(out, "\n")
	if len(lines) != len(cases) {
		t.Fatalf("moo answered %d of %d announcements:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		if c.accept {
			continue
		}
		if got := strings.TrimSpace(lines[i]); got != "accept" {
			t.Fatalf("the reader with the wire-alone walk removed still refuses %s: %s\n%s", c.name, got, out)
		}
	}
}
