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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
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

	rows := []struct {
		name string
		data []byte
		want string
	}{
		{"unit_announcement", ann, "ok"},
		{"at_bound", idTableBoundFile(ann, idTableBoundGrown(ids, ir.TableWireIdTableBound)), "ok"},
		{"over_bound", idTableBoundFile(ann, idTableBoundGrown(ids, ir.TableWireIdTableBound+1)), "refused"},
		{"repeated_id", idTableBoundFile(ann, idTableBoundRepeated(idTableBoundGrown(ids, 10))), "refused"},
	}

	dir := t.TempDir()
	for _, row := range rows {
		row := row
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
