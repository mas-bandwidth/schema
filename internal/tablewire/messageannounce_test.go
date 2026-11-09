package tablewire_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The announcement carries the message closure alone (docs/SPEC-TABLES.md
// §3.3): the fixed tables and what they reach by value, and nothing a
// variable table alone can name.
const messageAnnounceUnit = `package announceprobe

enum Shade { Red, Green }

enum Tint { Pale, Deep }

type Inner
{
    a     int32
    shade Shade
}

union Payload
{
    inner Inner
    tally int32
}

union VarPayload
{
    vx int32
}

fixed table Root
{
    inner   Inner
    payload Payload
    shade   Shade
    n       int32
}

table VarRoot
{
    vtint  Tint
    vp     VarPayload
    vlabel string(8)
    vcount uint32
    vroot  *Root
}
`

func TestMessageAnnounceCarriesMessageClosureAlone(t *testing.T) {
	t.Parallel()
	m := listModel(t, messageAnnounceUnit)
	u := m.Unit

	var vocab tablewire.Vocabulary
	var report tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(u), &report); err != nil {
		t.Fatalf("the unit refused its own announcement: %v %+v", err, report)
	}
	entries := vocab.Entries()
	want := tablewire.MessageVocabulary(u)
	if len(entries) != len(want) {
		t.Fatalf("announced %d entries, the message vocabulary holds %d", len(entries), len(want))
	}
	for i := range want {
		if entries[i].Key() != want[i].Key() {
			t.Fatalf("slot %d: announced %s, want %s", i+1, entries[i].Key(), want[i].Key())
		}
	}
	if full := ir.TableVocabulary(u); len(entries) >= len(full) {
		t.Fatalf("the trimmed announcement holds %d entries, the whole closure holds %d: nothing was trimmed", len(entries), len(full))
	}

	ids := map[uint64]bool{}
	keys := map[string]bool{}
	for _, e := range entries {
		ids[e.Id] = true
		keys[e.Key()] = true
	}
	// The tail takes no slot: not the node-table id, not a blob id, and not
	// any table's name id, fixed or variable.
	for _, id := range []uint64{ir.TableNodeWireId, ir.BytesWireTypeId, ir.StringWireTypeId, ir.WstringWireTypeId} {
		if ids[id] {
			t.Errorf("the announcement carries the tail id %016x", id)
		}
	}
	for name, st := range u.Tables {
		if ids[ir.TableWireId(st.WireName())] {
			t.Errorf("the announcement carries table %s's name id", name)
		}
	}
	// Nothing only the variable table reaches is announced: not its fields,
	// not its enum's variants, not its union's arms.
	varRoot := u.Tables["VarRoot"]
	for _, f := range varRoot.Fields {
		if keys[ir.TableFieldEntry(f).Key()] {
			t.Errorf("the announcement carries variable-only field %s", f.Name)
		}
	}
	tint := u.Enums["Tint"]
	for i := range tint.Variants {
		if ids[ir.TableWireId(tint.VariantWireName(i))] {
			t.Errorf("the announcement carries variable-only variant %s", tint.Variants[i])
		}
	}
	// Everything the fixed root reaches by value is announced.
	root := u.Tables["Root"]
	for _, f := range root.Fields {
		if !keys[ir.TableFieldEntry(f).Key()] {
			t.Errorf("the announcement misses fixed field %s", f.Name)
		}
	}
	shade := u.Enums["Shade"]
	for i := range shade.Variants {
		if !ids[ir.TableWireId(shade.VariantWireName(i))] {
			t.Errorf("the announcement misses variant %s", shade.Variants[i])
		}
	}

	// A body written against the trimmed vocabulary reads back under the
	// announced one.
	inst := place(t, m, "Root", `{"inner":{"a":3,"shade":"Green"},"payload":{"tally":7},"shade":"Red","n":9}`)
	wire, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dst := place(t, m, "Root", `{}`)
	var read tabletext.Report
	ok, err := tablewire.DecodeMessage(m, dst, wire, &vocab, &read)
	if err != nil || !ok {
		t.Fatalf("decode under the trimmed announcement: ok=%t err=%v %+v", ok, err, read)
	}
	if got := dst.Fields[3].Cell.I; got != 9 {
		t.Errorf("read back n=%d, want 9", got)
	}
}

// A unit with no fixed table announces an empty vocabulary, and that
// announcement still reads.
func TestMessageAnnounceEmptyWithoutFixedTable(t *testing.T) {
	t.Parallel()
	m := listModel(t, "package announceprobe\n\ntable VarRoot\n{\n    vcount uint32\n}\n")
	if got := tablewire.MessageVocabulary(m.Unit); len(got) != 0 {
		t.Fatalf("a variable-only unit's message vocabulary holds %d entries, want 0", len(got))
	}
	var vocab tablewire.Vocabulary
	var report tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(m.Unit), &report); err != nil {
		t.Fatalf("the empty announcement was refused: %v %+v", err, report)
	}
	if len(vocab.Entries()) != 0 {
		t.Fatalf("the empty announcement resolved %d entries", len(vocab.Entries()))
	}
}
