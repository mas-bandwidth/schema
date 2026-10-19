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

// retainScanner walks a newer wire through the OLD unit's shapes and collects
// the unknown FIELDS (kept whole, with every reference resolved — here every
// kept payload is a string, so it is exactly the wire's own bytes) and counts
// the unknown UNION ARMS (an excluded class that counts one `retain_lost`,
// docs/SPEC-TABLES.md §6.6). It is the oracle's own answer: the wire is the
// schema-go engine's write, so the expected bytes are its answer and not this
// test's arithmetic.
type retainScanner struct {
	ids      []uint64
	known    map[uint64]bool
	wire     []byte
	retained map[string]bool
	lost     int
}

func newRetainScanner(wire []byte, old *ir.Unit) *retainScanner {
	s := &retainScanner{known: map[uint64]bool{}, retained: map[string]bool{}}
	for _, id := range ir.TableWireIds(old) {
		s.known[id] = true
	}
	n := len(wire)
	count := int(binary.LittleEndian.Uint64(wire[n-8:]))
	end := n - 8 - count*8
	s.ids = make([]uint64, count)
	for i := range s.ids {
		s.ids[i] = binary.LittleEndian.Uint64(wire[end+i*8:])
	}
	s.wire = wire
	return s
}

func (s *retainScanner) leb(at *int) uint64 {
	var v uint64
	var sh uint
	for {
		x := s.wire[*at]
		*at++
		v |= uint64(x&0x7f) << sh
		if x < 0x80 {
			return v
		}
		sh += 7
	}
}

func (s *retainScanner) skipPayload(kind uint8, at *int) {
	switch kind {
	case 12, 13, 14, 16, 31, 32, 33:
		n := int(s.leb(at))
		*at += n
	case 4:
		*at += 4
	case 15:
		s.leb(at)
		if *at < len(s.wire) {
			*at++
		}
		n := int(s.leb(at))
		*at += n
	default:
		*at = len(s.wire)
	}
}

func (s *retainScanner) readBody(begin, end int) {
	at := begin
	for {
		if at >= end {
			return
		}
		ref := s.leb(&at)
		if ref == 0 || ref > uint64(len(s.ids)) {
			return
		}
		id := s.ids[ref-1]
		if at >= end {
			return
		}
		kind := s.wire[at]
		at++
		if !s.known[id] {
			start := at
			s.skipPayload(kind, &at)
			s.retained[hex.EncodeToString(s.wire[start:at])] = true
			continue
		}
		switch kind {
		case 13:
			n := int(s.leb(&at))
			s.readBody(at, at+n)
			at += n
		case 15:
			aref := s.leb(&at)
			if aref == 0 || aref > uint64(len(s.ids)) {
				return
			}
			armID := s.ids[aref-1]
			if at >= end {
				return
			}
			ak := s.wire[at]
			at++
			n := int(s.leb(&at))
			sub := at
			if !s.known[armID] {
				s.lost++
			} else if ak == 13 {
				s.readBody(sub, sub+n)
			}
			at = sub + n
		default:
			s.skipPayload(kind, &at)
		}
	}
}

type retainResult struct {
	retained, lost, unknown int
	malformed               bool
	payloads                map[string]bool
}

// retainRunLuau runs a generated retain probe under the pinned interpreter and
// returns the parsed report and retained payloads.
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
	b.WriteString("local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }\n")
	b.WriteString("local v, store = retain.LoadRetain(\"Root\", inputs.wire, report, " + strconv.Itoa(capacity) + ")\n")
	b.WriteString("print(string.format(\"report %d %d %d %s %d %d\", report.retained, report.retainLost, report.unknown, tostring(report.malformed), store.used, buffer.len(store.bytes)))\n")
	b.WriteString("local at = 0\nwhile at < store.used do\n")
	b.WriteString("\tlocal total = buffer.readu32(store.bytes, at)\n")
	b.WriteString("\tlocal depth = buffer.readu32(store.bytes, at + 8)\n")
	b.WriteString("\tlocal plen = buffer.readu32(store.bytes, at + 12)\n")
	b.WriteString("\tlocal parts = {}\n")
	b.WriteString("\tfor i = 0, plen - 1 do parts[i + 1] = string.format(\"%02x\", buffer.readu8(store.bytes, at + 26 + depth * 8 + i)) end\n")
	b.WriteString("\tprint(string.format(\"rec %s\", table.concat(parts)))\n")
	b.WriteString("\tat = at + total\nend\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))
	r := &retainResult{payloads: map[string]bool{}}
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
		} else if f[0] == "rec" {
			payload, err := hex.DecodeString(f[1])
			if err != nil {
				t.Fatalf("record payload is not hex: %v", err)
			}
			r.payloads[string(payload)] = true
		}
	}
	return r
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

	// The newer wire, by the OLD unit's shapes: the unknown root field `future`
	// and the unknown nested field `child.extra` are kept whole; the unknown
	// union arm `choice.note` is an excluded class and counts `retain_lost`.
	scan := newRetainScanner(wire, oldUnit)
	scan.readBody(1, len(wire)-8-int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))*8)
	if scan.lost != 1 {
		t.Fatalf("the scanner expected one lost union arm, got %d", scan.lost)
	}
	if len(scan.retained) != 2 {
		t.Fatalf("the scanner expected two retained fields, got %d", len(scan.retained))
	}

	g := generateFiles(t, map[string]string{"Probe.schema": retainOldSchema})

	big := retainRunLuau(t, g, wire, 1<<20)
	if big.malformed {
		t.Fatalf("LoadRetain raised malformed on a good read")
	}
	if big.retained != oracleReport.Retained || big.lost != oracleReport.RetainLost || big.unknown != oracleReport.Unknown {
		t.Fatalf("retain counters: got retained=%d lost=%d unknown=%d, oracle %+v", big.retained, big.lost, big.unknown, oracleReport)
	}
	if big.retained != len(scan.retained) || big.lost != scan.lost {
		t.Fatalf("retain counters vs scanner: got retained=%d lost=%d, scanner retained=%d lost=%d", big.retained, big.lost, len(scan.retained), scan.lost)
	}
	for p := range scan.retained {
		if !big.payloads[p] {
			t.Fatalf("the retained payload %x was not kept by Luau; got %v", p, big.payloads)
		}
	}
	for p := range big.payloads {
		if !scan.retained[p] {
			t.Fatalf("Luau kept a payload the oracle did not: %x", p)
		}
	}

	// A tiny capacity must not grow the store and must drop WHOLE records into
	// `retain_lost`; a zero capacity keeps nothing.
	small := retainRunLuau(t, g, wire, 8)
	if small.retained >= big.retained {
		t.Fatalf("a tiny capacity kept %d records (big kept %d): the store grew past its ceiling", small.retained, big.retained)
	}
	if small.lost <= big.lost {
		t.Fatalf("a tiny capacity lost %d (big lost %d): the store never reported capacity loss", small.lost, big.lost)
	}
	none := retainRunLuau(t, g, wire, 0)
	if none.retained != 0 || none.lost != big.retained+big.lost {
		t.Fatalf("a zero capacity kept %d and lost %d (want 0 and %d)", none.retained, none.lost, big.retained+big.lost)
	}

	// A wire written by THIS build (nothing unknown) retains nothing in Luau.
	clean := retainWire(t, oldUnit, `{"child":{"n":1},"choice":{"empty":{}}}`)
	cleanValue := oldModel.New(oldModel.Lookup("Root"))
	var cr tabletext.Report
	ok, err = tablewire.DecodeRetain(oldModel, cleanValue, clean, &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}, &cr)
	if !ok || err != nil || cr.Retained != 0 || cr.RetainLost != 0 {
		t.Fatalf("a wire with nothing unknown retained something (Go): %v %v %+v", ok, err, cr)
	}
	cl := retainRunLuau(t, g, clean, 1<<20)
	if cl.malformed || cl.retained != 0 || cl.lost != 0 || cl.unknown != 0 || len(cl.payloads) != 0 {
		t.Fatalf("a clean wire retained something in Luau: %+v", cl)
	}
}
