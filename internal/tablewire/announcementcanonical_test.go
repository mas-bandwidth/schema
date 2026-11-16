package tablewire_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// An announcement is an ordinary form-1 file (§3.3); its ID table has the
// same first-use and no-unused-entry contract as every other file.
func TestAnnouncementRequiresCanonicalIdTable(t *testing.T) {
	t.Parallel()
	model := listModel(t, "package canonicalannouncement\nfixed table Root {value uint16}\n")
	canonical := tablewire.Announce(model.Unit)
	body, ids := trailerOf(t, canonical)
	if len(ids) != 2 || len(body) < 12 || body[0] != 1 || body[1] != ir.TableKindU64 || body[10] != 2 || body[11] != ir.TableKindArray {
		t.Fatal("announcement must hold build-version then vocabulary fields")
	}
	withTable := func(body []byte, ids []uint64) []byte {
		wire := append([]byte{ir.TableWireForm}, body...)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}
	unused := ir.TableWireId("unused_announcement_entry")
	for unused == ids[0] || unused == ids[1] {
		unused++
	}
	outOfOrderBody := append([]byte(nil), body...)
	outOfOrderBody[0], outOfOrderBody[10] = 2, 1
	for _, tc := range []struct {
		name string
		wire []byte
		read bool
	}{
		{"canonical", canonical, true},
		{"unused entry", withTable(body, append(append([]uint64(nil), ids...), unused)), false},
		// References are renumbered too, preserving both fields' exact
		// identities and payloads: only the table's first-use order differs.
		{"out-of-order entries", withTable(outOfOrderBody, []uint64{ids[1], ids[0]}), false},
	} {
		var vocabulary tablewire.Vocabulary
		var report tabletext.Report
		err := vocabulary.AnnounceRead(tc.wire, &report)
		if (err == nil) != tc.read || vocabulary.Announced() != tc.read || report.Refused == tc.read {
			t.Errorf("%s: err=%v announced=%v report=%+v", tc.name, err, vocabulary.Announced(), report)
		}
		if !tc.read && len(vocabulary.Entries()) != 0 {
			t.Errorf("%s: refused announcement delivered a vocabulary", tc.name)
		}
	}
}
