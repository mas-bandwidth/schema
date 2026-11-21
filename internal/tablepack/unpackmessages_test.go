package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const unpackMessagesSchema = `package unpackmessages
fixed table M
{
    a uint8
    b string(4)
}
`

// unpackMessagesBatch is a lawful batch of two bodies of M and its
// announcement.
func unpackMessagesBatch(t *testing.T) (*tabletext.Model, []byte, []byte) {
	t.Helper()
	_, m := textListsUnitOf(t, unpackMessagesSchema)
	st := m.Lookup("M")
	insts := []*tabletext.Instance{m.New(st), m.New(st)}
	for i, inst := range insts {
		inst.Fields[0].Cell.U = uint64(5 + i)
		inst.Fields[0].Cell.I = int64(5 + i)
		inst.Fields[1].Cell.Str = []byte("hi")
		inst.Fields[1].Count = 2
	}
	batch, err := tablewire.EncodeMessages(m, insts)
	if err != nil {
		t.Fatal(err)
	}
	return m, tablewire.Announce(m.Unit), batch
}

// TestDecodeMessagesFalseCarriesItsRefusal pins the contract that makes
// UnpackMessages's `!ok` branch unreachable (TABLEWIRE-6, schema review of
// 2026-11-21): over every one-bit flip of a lawful batch, a read that does not
// return the batch returns the refusal with it, and delivers no body
// (docs/SPEC-TABLES.md §3.3, §6.5). No damaged batch leaves earlier bodies
// standing.
func TestDecodeMessagesFalseCarriesItsRefusal(t *testing.T) {
	t.Parallel()
	m, announcement, batch := unpackMessagesBatch(t)
	st := m.Lookup("M")
	refused := 0
	for bit := 0; bit < len(batch)*8; bit++ {
		flipped := append([]byte(nil), batch...)
		flipped[bit/8] ^= 1 << (bit % 8)
		var v tablewire.Vocabulary
		if err := v.AnnounceRead(announcement, &tabletext.Report{}); err != nil {
			t.Fatal(err)
		}
		insts := []*tabletext.Instance{m.New(st), m.New(st)}
		var report tabletext.Report
		n, ok, err := tablewire.DecodeMessages(m, insts, flipped, &v, &report)
		if ok != (err == nil) {
			t.Fatalf("bit %d: ok=%v with err=%v: a read that does not return must refuse", bit, ok, err)
		}
		if !ok {
			refused++
			if n != 0 && !tablewire.Refused(err) {
				t.Fatalf("bit %d: a damaged batch delivered %d bodies", bit, n)
			}
		}
	}
	if refused == 0 {
		t.Fatal("no flip was refused: the batch exercises nothing")
	}
}

// TestUnpackMessagesDamageWritesNothing is the CLI's half: a damaged batch is
// answered by the engine's refusal, nothing is written, and the answer never
// claims that bodies before the damage stand (TABLEWIRE-6).
func TestUnpackMessagesDamageWritesNothing(t *testing.T) {
	t.Parallel()
	m, announcement, batch := unpackMessagesBatch(t)
	damaged := append(append([]byte(nil), batch...), 0xff) // bytes after the pad
	dir := t.TempDir()
	trees := []tablepack.MessageTree{{Root: "M", Dir: filepath.Join(dir, "a")}, {Root: "M", Dir: filepath.Join(dir, "b")}}
	_, err := tablepack.UnpackMessages(m, trees, announcement, damaged, false)
	if err == nil || !tablewire.ReadRefusal(err) {
		t.Fatalf("a damaged batch was answered %v, want the engine's refusal", err)
	}
	if strings.Contains(err.Error(), "stand") {
		t.Fatalf("the refusal claims earlier bodies stand: %v", err)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused batch wrote %d entries", len(entries))
	}
	// the lawful twin unpacks both trees
	if _, err := tablepack.UnpackMessages(m, trees, announcement, batch, false); err != nil {
		t.Fatalf("the lawful batch was refused: %v", err)
	}
}
