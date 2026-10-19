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

const retainOldSchema = `package probe
table Child { n int32 }
union Choice { child Child
 empty }
table Root { child Child
 choice Choice }
`

const retainNewSchema = `package probe
table Child { n int32
 extra string(32) }
union Choice { child Child
 empty
 note string(32) }
table Root { child Child
 choice Choice
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

// retainUnknownPayloads returns the payload bytes of every ROOT field whose id
// the older unit cannot name, read from the newer wire's own framing. The wire
// is the schema-go engine's write, so the expected bytes are its answer and not
// this test's arithmetic.
func retainUnknownPayloads(t *testing.T, wire []byte, old *ir.Unit) map[uint64][]byte {
	t.Helper()
	known := map[uint64]bool{}
	for _, id := range ir.TableWireIds(old) {
		known[id] = true
	}
	len := len(wire)
	count := int(binary.LittleEndian.Uint64(wire[len-8:]))
	end := len - 8 - count*8
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[end+i*8:])
	}
	out := map[uint64][]byte{}
	at := 1
	leb := func() uint64 {
		var v uint64
		var s uint
		for {
			x := wire[at]
			at++
			v |= uint64(x&0x7f) << s
			if x < 0x80 {
				return v
			}
			s += 7
		}
	}
	for {
		ref := leb()
		if ref == 0 {
			break
		}
		kind := wire[at]
		at++
		start := at
		switch {
		case kind == 12 || kind == 13 || kind == 14 || kind == 16 || kind == 31 || kind == 33:
			n := int(leb())
			at += n
		case kind == 4:
			at += 4
		case kind == 15:
			ref := leb()
			if ref != 0 {
				at++
				n := int(leb())
				at += n
			}
		default:
			t.Fatalf("retain test parser: kind %d at root is not modelled", kind)
		}
		id := ids[ref-1]
		if !known[id] {
			out[id] = append([]byte(nil), wire[start:at]...)
		}
	}
	return out
}

func TestLuauLoadRetainKeepsUnknown(t *testing.T) {
	t.Parallel()
	oldUnit, _ := retainCompile(t, retainOldSchema)
	newUnit, _ := retainCompile(t, retainNewSchema)

	oldModel := tabletext.NewModel(oldUnit)
	oldValue := oldModel.New(oldModel.Lookup("Root"))
	var oracleReport tabletext.Report
	keep := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	wire := retainWire(t, newUnit, `{"child":{"n":7,"extra":"node kept"},"choice":{"note":"arm kept"},"future":"root kept"}`)
	ok, err := tablewire.DecodeRetain(oldModel, oldValue, wire, keep, &oracleReport)
	if !ok || err != nil || oracleReport.Malformed {
		t.Fatalf("the oracle read failed: %v %v %+v", ok, err, oracleReport)
	}
	want := retainUnknownPayloads(t, wire, oldUnit)

	g := generateFiles(t, map[string]string{"Probe.schema": retainOldSchema})
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
	b.WriteString(`local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local v, store = retain.LoadRetain("Root", inputs.wire, report)
print(string.format("report %d %d %d %s", report.retained, report.retainLost, report.unknown, tostring(report.malformed)))
local at = 0
while at < store.used do
	local total = buffer.readu32(store.bytes, at)
	local node = buffer.readu32(store.bytes, at + 4)
	local depth = buffer.readu32(store.bytes, at + 8)
	local plen = buffer.readu32(store.bytes, at + 12)
	local idparts = {}
	for i = 0, 7 do idparts[i + 1] = string.format("%02x", buffer.readu8(store.bytes, at + 23 - i)) end
	local id = table.concat(idparts)
	local kind = buffer.readu8(store.bytes, at + 24)
	local parts = {}
	for i = 0, plen - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(store.bytes, at + 26 + depth * 8 + i)) end
	print(string.format("rec %s %d %d %d %s", id, kind, node, depth, table.concat(parts)))
	at = at + total
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))
	var retained, lost, unknown int
	var malformed string
	got := map[uint64][]byte{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == "report" {
			retained, _ = strconv.Atoi(f[1])
			lost, _ = strconv.Atoi(f[2])
			unknown, _ = strconv.Atoi(f[3])
			malformed = f[4]
		} else if f[0] == "rec" {
			id, _ := strconv.ParseUint(f[1], 16, 64)
			payload, err := hex.DecodeString(f[5])
			if err != nil {
				t.Fatalf("record payload is not hex: %v", err)
			}
			got[id] = payload
		}
	}
	if malformed != "false" {
		t.Fatalf("LoadRetain raised malformed on a good read")
	}
	if retained != oracleReport.Retained || lost != oracleReport.RetainLost || unknown != oracleReport.Unknown {
		t.Fatalf("retain counters: got retained=%d lost=%d unknown=%d, oracle %+v", retained, lost, unknown, oracleReport)
	}
	for id, wantBytes := range want {
		payload, ok := got[id]
		if !ok {
			t.Fatalf("the record for id %#x was not retained; got %v", id, got)
		}
		if string(payload) != string(wantBytes) {
			t.Fatalf("id %#x: retained %x, the wire carries %x", id, payload, wantBytes)
		}
	}

	cleanModel := tabletext.NewModel(oldUnit)
	clean := retainWire(t, oldUnit, `{"child":{"n":1},"choice":{"empty":{}}}`)
	_ = cleanModel
	cleanValue := oldModel.New(oldModel.Lookup("Root"))
	var cr tabletext.Report
	ok, err = tablewire.DecodeRetain(oldModel, cleanValue, clean, &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}, &cr)
	if !ok || err != nil || cr.Retained != 0 || cr.RetainLost != 0 {
		t.Fatalf("a wire with nothing unknown retained something: %v %v %+v", ok, err, cr)
	}
}
