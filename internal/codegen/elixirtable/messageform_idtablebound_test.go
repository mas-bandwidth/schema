package elixirtable

// THE ANNOUNCEMENT'S ID TABLE HAS A MAXIMUM (docs/SPEC-TABLES.md §3, THE
// DISTINCTNESS CHECK): a reader refuses any id table over 4096 entries before
// it does distinctness work, whatever the buffer would hold, so the proof
// stays bounded. Each row is the unit's own announcement with its id table
// grown, and the engine (internal/tablewire) reads the same bytes as the
// oracle.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestElixirAnnouncementIdTableBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	units, _ := messageManifest(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	u := messageLoadUnit(t, root, units["backenddemo"])
	ann := tablewire.Announce(u)
	ids := idTableBoundIds(ann)
	// the lawful ten-entry file the repeated_id row damages, by naming its
	// first id again in its last entry
	ten := idTableBoundReferenced(ann, idTableBoundGrown(ids, 10))

	rows := []struct {
		name string
		data []byte
		want string
	}{
		{"unit_announcement", ann, "ok"},
		{"at_bound", idTableBoundReferenced(ann, idTableBoundGrown(ids, ir.TableWireIdTableBound)), "ok"},
		{"over_bound", idTableBoundReferenced(ann, idTableBoundGrown(ids, ir.TableWireIdTableBound+1)), "refused"},
		{"repeated_id", idTableBoundFile(ten, idTableBoundRepeated(idTableBoundIds(ten))), "refused"},
	}

	dir := t.TempDir()
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, row.name+".bin")
			if err := os.WriteFile(path, row.data, 0o600); err != nil {
				t.Fatal(err)
			}
			engine := announceVerdict(row.data)
			if engine != row.want {
				t.Fatalf("the engine reads the %s id table as %s, want %s", row.name, engine, row.want)
			}
			out, err := runMessageProbe(t, u, path, `  data = File.read!(file())
  case M.announce_read(data) do
    {:ok, _} -> IO.puts("verdict=ok")
    {:error, _, _} -> IO.puts("verdict=refused")
  end
`)
			if err != nil {
				t.Fatalf("%s: %v\n%s", row.name, err, out)
			}
			got := ""
			for line := range strings.SplitSeq(out, "\n") {
				if verdict, ok := strings.CutPrefix(strings.TrimSpace(line), "verdict="); ok {
					got = verdict
				}
			}
			if got == "" {
				t.Fatalf("the reader gave no verdict for %s:\n%s", row.name, out)
			}
			if got != row.want || got != engine {
				t.Fatalf("%s: the reader says %s, the engine %s, want %s", row.name, got, engine, row.want)
			}
		})
	}
}

// idTableBoundIds is the announcement's trailer ids, in order.
func idTableBoundIds(ann []byte) []uint64 {
	count := binary.LittleEndian.Uint64(ann[len(ann)-8:])
	start := len(ann) - 8 - int(count)*8
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(ann[start+i*8:])
	}
	return ids
}

// idTableBoundGrown keeps the announcement's own ids and appends fresh
// distinct ones until the table has n entries.
func idTableBoundGrown(ids []uint64, n int) []uint64 {
	out := append([]uint64(nil), ids...)
	seen := make(map[uint64]bool, n)
	for _, id := range out {
		seen[id] = true
	}
	for id := uint64(1); len(out) < n; id++ {
		if seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	return out
}

// idTableBoundRepeated puts one id in two entries.
func idTableBoundRepeated(ids []uint64) []uint64 {
	out := append([]uint64(nil), ids...)
	out[len(out)-1] = out[0]
	return out
}

// idTableBoundReferenced is the announcement with its id table replaced by
// ids, a table that keeps the announcement's own entries first: every entry
// past them is referenced by one unknown u8 field appended to the root body
// in table order, so the table is exactly the body's first-use order
// (docs/SPEC-TABLES.md §3) and a reader refuses it only for its size or a
// repeated entry.
func idTableBoundReferenced(ann []byte, ids []uint64) []byte {
	own := idTableBoundIds(ann)
	body := ann[:len(ann)-8-len(own)*8]
	// the root body's terminator is its last byte; the fields go before it
	out := append([]byte(nil), body[:len(body)-1]...)
	for ref := len(own) + 1; ref <= len(ids); ref++ {
		out = binary.AppendUvarint(out, uint64(ref))
		out = append(out, byte(ir.TableKindU8), 0)
	}
	out = append(out, 0)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

// idTableBoundFile replaces the announcement's id table with ids.
func idTableBoundFile(ann []byte, ids []uint64) []byte {
	count := binary.LittleEndian.Uint64(ann[len(ann)-8:])
	body := ann[:len(ann)-8-int(count)*8]
	out := append([]byte(nil), body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

// announceVerdict is the engine's answer on the same announcement bytes.
func announceVerdict(data []byte) string {
	var report tabletext.Report
	v := &tablewire.Vocabulary{}
	if err := v.AnnounceRead(data, &report); err != nil {
		return "refused"
	}
	return "ok"
}
