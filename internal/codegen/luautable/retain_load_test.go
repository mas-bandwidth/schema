package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The OLD unit is pointered (Root carries `ptr`), so a wire written by a newer
// build rides a node table (§3.1). The NEW unit adds a field the old cannot
// name inside Child, a newer union arm, a whole newer table `NewKind` (an
// unknown node type), and a newer `future` root field. Loading this newer wire
// with retention must keep the unknown fields whole, count the excluded classes
// one `retain_lost` each, and never report `malformed`
// (docs/SPEC-TABLES.md §6.6).
const retainOldSchema = `package probe
table Child { n int32 }
union Choice { child Child
 empty }
table Root { child Child
 choice Choice
 ptr Child }
`

const retainNewSchema = `package probe
table Child { n int32
 extra string(32) }
union Choice { child Child
 empty
 note string(32) }
table NewKind { m int32 }
table Root { child Child
 choice Choice
 ptr NewKind
 future string(32) }
`

func retainCompile(t *testing.T, src string) (*ir.Unit, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u, dir
}

func retainWire(t *testing.T, u *ir.Unit, json string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(json), &report) {
		t.Fatalf("the newer value does not read: %+v", report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

type rec struct {
	node    uint32
	depth   uint32
	id      uint64
	kind    uint8
	payload []byte
}

func (r rec) key() string {
	return fmt.Sprintf("%d/%d/%d/%d/%x", r.node, r.depth, r.id, r.kind, r.payload)
}

// unknownFieldIds returns the field ids of the OLD unit that the NEW unit's
// value cannot name — the ids that retention must keep whole.
func unknownFieldIds(old *ir.Unit, new *ir.Unit) map[uint64]bool {
	oldKnown := map[uint64]bool{}
	for _, id := range ir.TableWireIds(old) {
		oldKnown[id] = true
	}
	out := map[uint64]bool{}
	for _, id := range ir.TableWireIds(new) {
		if !oldKnown[id] {
			out[id] = true
		}
	}
	return out
}

// expectedFieldKinds maps every field the NEW unit can spell to the wire kind
// it rides under (docs/SPEC-TABLES.md §3). A retained record must carry the
// field's own kind, so a wrong kind in the Luau walk is caught field-by-field.
func expectedFieldKinds(u *ir.Unit) map[uint64]uint8 {
	out := map[uint64]uint8{}
	for _, tb := range u.Tables {
		for _, f := range tb.Fields {
			out[ir.TableFieldWireId(f)] = uint8(ir.TableWireFieldKind(f))
		}
	}
	return out
}

func retainRunLuau(t *testing.T, g *generatedUnit, wire []byte, capacity int) *retainResult {
	t.Helper()
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal retain = require(\"./retain\")\nlocal load = require(\"./load\")\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString("local function tohex(bytes) local parts = {} for i = 0, #bytes - 1 do parts[i + 1] = string.format(\"%02x\", string.byte(bytes, i + 1)) end return table.concat(parts) end\n")
	b.WriteString("local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }\n")
	b.WriteString("local v, store = retain.LoadRetain(\"Root\", inputs.wire, report, " + strconv.Itoa(capacity) + ")\n")
	b.WriteString("print(string.format(\"report %d %d %d %s %d %d\", report.retained, report.retainLost, report.unknown, tostring(report.malformed), store.used, store.count))\n")
	b.WriteString("local at = 0\nwhile at < store.used do\n")
	b.WriteString("\tlocal total = buffer.readu32(store.bytes, at)\n")
	b.WriteString("\tlocal node = buffer.readu32(store.bytes, at + 4)\n")
	b.WriteString("\tlocal depth = buffer.readu32(store.bytes, at + 8)\n")
	b.WriteString("\tlocal plen = buffer.readu32(store.bytes, at + 12)\n")
	b.WriteString("\tlocal idhex = tohex(buffer.readstring(store.bytes, at + 16, 8))\n")
	b.WriteString("\tlocal kind = buffer.readu8(store.bytes, at + 24)\n")
	b.WriteString("\tlocal payload = buffer.readstring(store.bytes, at + 26 + depth * 8, plen)\n")
	b.WriteString("\tprint(string.format(\"rec %d %d %s %d %s\", node, depth, idhex, kind, tohex(payload)))\n")
	b.WriteString("\tat = at + total\nend\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))
	r := &retainResult{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == "report" {
			r.retained, _ = strconv.Atoi(f[1])
			r.lost, _ = strconv.Atoi(f[2])
			r.unknown, _ = strconv.Atoi(f[3])
			r.malformed = f[4] == "true"
			r.used, _ = strconv.Atoi(f[5])
			r.count, _ = strconv.Atoi(f[6])
		} else if f[0] == "rec" && len(f) == 6 {
			node, _ := strconv.Atoi(f[1])
			depth, _ := strconv.Atoi(f[2])
			idBytes, err := hex.DecodeString(f[3])
			if err != nil || len(idBytes) != 8 {
				t.Fatalf("record id is not 8 hex bytes: %v", err)
			}
			id := binary.LittleEndian.Uint64(idBytes)
			kind, _ := strconv.Atoi(f[4])
			payload, err := hex.DecodeString(f[5])
			if err != nil {
				t.Fatalf("record payload is not hex: %v", err)
			}
			r.records = append(r.records, rec{node: uint32(node), depth: uint32(depth), id: id, kind: uint8(kind), payload: payload})
		}
	}
	return r
}

type retainResult struct {
	retained, lost, unknown int
	malformed               bool
	used, count             int
	records                 []rec
}

func TestLuauLoadRetainKeepsUnknown(t *testing.T) {
	t.Parallel()
	oldUnit, _ := retainCompile(t, retainOldSchema)
	newUnit, _ := retainCompile(t, retainNewSchema)

	oldModel := tabletext.NewModel(oldUnit)
	oldValue := oldModel.New(oldModel.Lookup("Root"))
	var oracleReport tabletext.Report
	keep := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	wire := retainWire(t, newUnit, `{"child":{"n":7,"extra":"node kept"},"choice":{"note":"arm kept"},"ptr":{"m":9},"future":"root kept"}`)
	ok, err := tablewire.DecodeRetain(oldModel, oldValue, wire, keep, &oracleReport)
	if !ok || err != nil || oracleReport.Malformed {
		t.Fatalf("the oracle read failed: %v %v %+v", ok, err, oracleReport)
	}

	unknown := unknownFieldIds(oldUnit, newUnit)
	// the newer unit's unknown ids must be non-empty and include child.extra,
	// the note arm id, NewKind and future.
	if len(unknown) < 3 {
		t.Fatalf("expected several unknown ids, got %v", unknown)
	}
	// the oracle must count at least one excluded class (the NewKind node type
	// and the note arm) as retained_lost.
	if oracleReport.RetainLost < 1 {
		t.Fatalf("oracle retain_lost=%d, want >= 1 (NewKind node type + note arm)", oracleReport.RetainLost)
	}

	g := generateFiles(t, map[string]string{"Probe.schema": retainOldSchema})
	big := retainRunLuau(t, g, wire, 1<<20)
	if big.malformed {
		t.Fatalf("LoadRetain raised malformed on a good read")
	}
	if big.retained != oracleReport.Retained || big.lost != oracleReport.RetainLost || big.unknown != oracleReport.Unknown {
		t.Fatalf("retain counters: got retained=%d lost=%d unknown=%d, oracle %+v", big.retained, big.lost, big.unknown, oracleReport)
	}
	if big.count != big.retained || len(big.records) != big.retained {
		t.Fatalf("the store holds %d records but the report says %d retained (decoded %d)", big.count, big.retained, len(big.records))
	}
	// the wire's node table bounds the node index the walk may name: the root
	// is node 1 and node record k is node k+2, so no retained record may name a
	// node past the table's count+1 (docs/SPEC-TABLES.md §3.1).
	_, whole := tablewire.NodeRecordTypes(wire)
	if !whole {
		t.Fatal("the wire's node table does not parse whole")
	}
	// each decoded record must name an id the old unit cannot, carry a
	// non-empty payload, live on a legal node index, and ride the kind the NEW
	// unit declares for that field — a wrong kind (a planted bug) is caught
	// field-by-field.
	kinds := expectedFieldKinds(newUnit)
	seen := map[string]bool{}
	for _, rc := range big.records {
		if !unknown[rc.id] {
			t.Fatalf("record id %x is not an unknown field id; got %v", rc.id, unknown)
		}
		if want, ok := kinds[rc.id]; !ok || rc.kind != want {
			t.Fatalf("record id %x has kind %d, want %d (from the NEW unit)", rc.id, rc.kind, want)
		}
		if len(rc.payload) == 0 {
			t.Fatalf("record %x has an empty payload", rc.id)
		}
		if rc.node < 1 || rc.node > 2 {
			t.Fatalf("record %x has node %d, want 1..2 (root plus the one node record)", rc.id, rc.node)
		}
		if seen[rc.key()] {
			t.Fatalf("duplicate record %s", rc.key())
		}
		seen[rc.key()] = true
	}

	// a tiny capacity must drop WHOLE records into retain_lost; a zero capacity
	// keeps nothing.
	small := retainRunLuau(t, g, wire, 8)
	if small.retained >= big.retained {
		t.Fatalf("a tiny capacity kept %d records (big kept %d)", small.retained, big.retained)
	}
	if small.lost <= big.lost {
		t.Fatalf("a tiny capacity lost %d (big lost %d): no capacity loss reported", small.lost, big.lost)
	}
	if small.count != small.retained {
		t.Fatalf("tiny store count %d != retained %d", small.count, small.retained)
	}
	none := retainRunLuau(t, g, wire, 0)
	if none.retained != 0 || none.lost != big.retained+big.lost || none.count != 0 {
		t.Fatalf("a zero capacity kept %d/lost %d/count %d (want 0/%d/0)", none.retained, none.lost, none.count, big.retained+big.lost)
	}

	// a wire written by THIS build (nothing unknown) retains nothing.
	clean := retainWire(t, oldUnit, `{"child":{"n":1},"choice":{"empty":{}},"ptr":{"n":1}}`)
	cleanValue := oldModel.New(oldModel.Lookup("Root"))
	var cr tabletext.Report
	ok, err = tablewire.DecodeRetain(oldModel, cleanValue, clean, &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}, &cr)
	if !ok || err != nil || cr.Retained != 0 || cr.RetainLost != 0 {
		t.Fatalf("a wire with nothing unknown retained something (Go): %v %v %+v", ok, err, cr)
	}
	cl := retainRunLuau(t, g, clean, 1<<20)
	if cl.malformed || cl.retained != 0 || cl.lost != 0 || cl.unknown != 0 || len(cl.records) != 0 {
		t.Fatalf("a clean wire retained something in Luau: %+v", cl)
	}
}
