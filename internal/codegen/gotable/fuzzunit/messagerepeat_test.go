package tabledemo_test

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// getBit is bit i of b, least significant first.
func getBit(b []byte, i int) uint8 { return (b[i/8] >> uint(i%8)) & 1 }

// TestGeneratedMessageRefusesRepeatedFieldId holds docs/SPEC-TABLES.md §3.3 for
// the generated reader: a field id twice in one body (one body's fields, then
// a second's, under one terminator) refuses the batch, as the engine does.
func TestGeneratedMessageRefusesRepeatedFieldId(t *testing.T) {
	c := compiler.New()
	paths, _ := compiler.GatherPaths([]string{"../../../../tables/examples"})
	u, _ := c.Load(paths)
	m := tabletext.NewModel(u)
	root := m.Lookup("RangedSigned")
	if !bytes.Equal(tablewire.Announce(u), func() []byte {
		a := make([]byte, tabledemo.AnnounceMeasure())
		tabledemo.Announce(a)
		return a
	}()) {
		t.Fatal("announcements differ; splice not meaningful")
	}
	var voc tablewire.Vocabulary
	var ann tabletext.Report
	voc.AnnounceRead(tablewire.Announce(u), &ann)
	enc := func(text string) []byte {
		inst := m.New(root)
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) {
			t.Fatal(r.Reason)
		}
		b, err := tablewire.EncodeMessage(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	x := enc(`{"i8_span":5}`)
	y := enc(`{"i8_span":-7}`)
	v := voc
	sx := tablewire.MessageSpots(m, root, x, &v)
	v = voc
	sy := tablewire.MessageSpots(m, root, y, &v)
	rb := sx[0].Width
	// body of x up to its terminator, then all of y's body including terminator
	endX := sx[len(sx)-1].Off
	endY := sy[len(sy)-1].Off + rb
	var out []byte
	n := 0
	put := func(bit uint8) {
		if n%8 == 0 {
			out = append(out, 0)
		}
		out[n/8] |= bit << uint(n%8)
		n++
	}
	for i := 0; i < 8; i++ {
		put(getBit(x, i))
	}
	for i := 0; i < 8; i++ {
		put(0)
	}
	for i := 8; i < endX; i++ {
		put(getBit(x[1:], i))
	}
	for i := 8; i < endY; i++ {
		put(getBit(y[1:], i))
	}
	inst := m.New(root)
	v = voc
	var rep tabletext.Report
	eok, eerr := tablewire.DecodeMessage(m, inst, out, &v, &rep)
	gvoc := messagesVocabulary(t)
	vals := make([]tabledemo.RangedSigned, 1)
	var grep tabledemo.TableReport
	gn, gok := tabledemo.RangedSignedLoadMessages(vals, gvoc, out, &grep)
	if eok || gok || gn != 0 {
		t.Errorf("a repeated field id was accepted: engine ok=%v err=%v; generated n=%d ok=%v %+v: %s", eok, eerr, gn, gok, grep, hex.EncodeToString(out))
	}
}

// TestGeneratedMessageRefusesRepeatedKeyedSlot holds docs/SPEC-TABLES.md §3.2
// and §3.3 for the generated reader: the same keyed slot twice in one
// enum-keyed array (ScoreBoard.per_team's Blue key overwritten with Red's)
// refuses the batch, as the engine does.
func TestGeneratedMessageRefusesRepeatedKeyedSlot(t *testing.T) {
	c := compiler.New()
	paths, _ := compiler.GatherPaths([]string{"../../../../tables/examples"})
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.Lookup("KeyedConfig")
	inst := m.New(root)
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"scores":{"per_team":{"Red":1,"Blue":2}}}`), &r) {
		t.Fatal(r.Reason)
	}
	data, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	var voc tablewire.Vocabulary
	voc.AnnounceRead(tablewire.Announce(u), &tabletext.Report{})
	gvoc := messagesVocabulary(t)
	clean := make([]tabledemo.KeyedConfig, 1)
	if n, ok := tabledemo.KeyedConfigLoadMessages(clean, gvoc, data, nil); !ok || n != 1 {
		t.Fatal("the clean batch was refused")
	}
	// the spots: scores, per_team, the Red key, the Blue key, two terminators
	v := voc
	spots := tablewire.MessageSpots(m, root, data, &v)
	if len(spots) != 6 || spots[2].Width != spots[3].Width {
		t.Fatalf("the keys are not where this test expects them: %+v", spots)
	}
	b := append([]byte(nil), data...)
	red, blue := spots[2], spots[3]
	for i := 0; i < blue.Width; i++ {
		pos := 8 + blue.Off + i // spots are relative to data[1:]
		if (red.Value>>uint(i))&1 == 1 {
			b[pos/8] |= 1 << uint(pos%8)
		} else {
			b[pos/8] &^= 1 << uint(pos%8)
		}
	}
	v = voc
	eok, eerr := tablewire.DecodeMessage(m, m.New(root), b, &v, &tabletext.Report{})
	vals := make([]tabledemo.KeyedConfig, 1)
	var grep tabledemo.TableReport
	gn, gok := tabledemo.KeyedConfigLoadMessages(vals, gvoc, b, &grep)
	if eok || gok || gn != 0 {
		t.Errorf("a repeated keyed slot was accepted: engine ok=%v err=%v; generated n=%d ok=%v %+v: %s", eok, eerr, gn, gok, grep, hex.EncodeToString(b))
	}
}

// TestGeneratedMessageRepeatNegativeControl removes each repeat rule from the
// generated reader (every table's copy of it) and requires its own test red on
// the generated side.
func TestGeneratedMessageRepeatNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		file, from, to, run string
	}{
		{"internal/codegen/gotable/fuzzunit/RangesTableMessage.go", "\t\t\tif id == entry.ID {", "\t\t\tif false && id == entry.ID {", "^TestGeneratedMessageRefusesRepeatedFieldId$"},
		{"internal/codegen/gotable/fuzzunit/KeyedTableMessage.go", "if k == id {", "if false && k == id {", "^TestGeneratedMessageRefusesRepeatedKeyedSlot$"},
	} {
		out, failed := slowtest.RunSabotaged(t, "../../../..", "./internal/codegen/gotable/fuzzunit", c.run,
			slowtest.Sabotage{File: c.file, From: c.from, To: c.to, All: true})
		if !failed || !strings.Contains(out, "generated n=1 ok=true") {
			t.Errorf("negative control %s: the rule is gone and the generated reader was not caught\n%s", c.run, out)
		}
	}
}
