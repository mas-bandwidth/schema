package mootable_test

// A VARIABLE-FORM LOAD IS ONE DECODE (docs/SPEC-TABLES.md §4, §6.5): the file
// is read once, in time linear in its size, whether it accepts or refuses.
// This test holds a 20,000-record pointer chain with one unknown root field to
// a 60-second deadline, on the accepted load and on a refused one whose
// out-of-range value sits in the last record. The report names the field in
// both cases: the unknown id it skipped, and the value it refused.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const oneDecodeSchema = `package onepass

table ListNode
{
    value int32 | min = 0, max = 1000000
    next  *ListNode
}

table Scene
{
    head *ListNode
}
`

func oneDecodeBuildProbe(records int) string {
	return fmt.Sprintf(`--!strict
local T = require("./SceneTable")
local save = require("./table_save")
local St = require("./schema_tables")
local value = T.NewScene()
local head: any = nil
for i = %d, 1, -1 do
	local n = T.NewListNode()
	n.Value = i
	n.Next = head
	head = n
end
value.Head = head
local out, why = save.save(St.lookup("Scene"), value)
if out == nil then
	print("SAVENIL " .. tostring(if why ~= nil then why.name else nil))
	return
end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print(table.concat(parts))
`, records)
}

const oneDecodeLoadProbe = `--!strict
local inputs = require("./inputs")
local load = require("./table_load")
require("./SceneTable")
local report = load.newReport()
local v = load.ReadStrict("Scene", inputs.wire, report)
if v == nil then
	print("refused " .. tostring(report.reason) .. " " .. tostring(report.field))
else
	print("read " .. tostring(report.unknownField))
end
`

func runMooDeadline(t *testing.T, g *generatedUnit, main string, inputs map[string][]byte, limit time.Duration) (string, error) {
	t.Helper()
	moo, why := moorun.Interpreter()
	if moo == "" {
		t.Fatalf("moorun: the moo interpreter does not resolve: %s", why)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.moo"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "inputs.moo"), moorun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, moo, "main.moo")
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("moo did not finish within %s", limit)
	}
	if err != nil {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("moo failed: %w\nstdout:\n%s\nstderr:\n%s", err, stdout.Bytes(), stderr.Bytes())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func readLEB(t *testing.T, b []byte, pos int) (uint64, int) {
	t.Helper()
	var v uint64
	shift := uint(0)
	for {
		if pos >= len(b) || shift >= 64 {
			t.Fatalf("a malformed LEB at byte %d", pos)
		}
		c := b[pos]
		pos++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, pos
		}
		shift += 7
	}
}

func skipWire(t *testing.T, b []byte, pos, end, kind int) (int, bool) {
	t.Helper()
	width := map[int]int{
		1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 2, 8: 4, 9: 8, 10: 4, 11: 8,
		18: 16, 19: 16, 20: 1, 21: 2, 22: 4, 23: 8, 24: 16,
		25: 1, 26: 2, 27: 4, 28: 8, 29: 16, 36: 8,
	}
	if w, ok := width[kind]; ok {
		return pos + w, pos+w <= end
	}
	switch kind {
	case 12, 13, 14, 16, 31, 32:
		n, next := readLEB(t, b, pos)
		return next + int(n), next+int(n) <= end
	case 17, 30:
		_, next := readLEB(t, b, pos)
		return next, next <= end
	case 15:
		ref, next := readLEB(t, b, pos)
		if ref == 0 {
			return next, true
		}
		if next >= end {
			return next, false
		}
		next++
		n, after := readLEB(t, b, next)
		return after + int(n), after+int(n) <= end
	}
	return pos, false
}

// addUnknownRootField inserts one root field this reader does not declare,
// with its id appended to the id table: a legal later addition the read skips
// by its kind and counts unknown.
func addUnknownRootField(t *testing.T, wire []byte, id uint64, kind byte, payload []byte) []byte {
	t.Helper()
	n := len(wire)
	count := binary.LittleEndian.Uint64(wire[n-8:])
	idStart := n - 8 - int(count)*8
	if idStart < 2 || wire[idStart-1] != 0 {
		t.Fatalf("the wire is not a form-1 file: id table at %d, byte before it %02x", idStart, wire[idStart-1])
	}
	field := append(leb(count+1), kind)
	field = append(field, payload...)
	out := make([]byte, 0, len(wire)+len(field)+8)
	out = append(out, wire[:idStart-1]...)
	out = append(out, field...)
	out = append(out, wire[idStart-1:]...)
	out = out[:len(out)-8]
	out = append(out, u64le(id)...)
	out = append(out, u64le(count+1)...)
	return out
}

// outOfRangeLastRecord replaces the value of the last ListNode record with a
// value past the field's declared maximum, so the load refuses out_of_range.
func outOfRangeLastRecord(t *testing.T, wire []byte, listNodeID, valueID uint64, bad uint32) []byte {
	t.Helper()
	out := append([]byte(nil), wire...)
	n := len(out)
	count := binary.LittleEndian.Uint64(out[n-8:])
	idStart := n - 8 - int(count)*8
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(out[idStart+i*8:])
	}
	nodeTableID := uint64(0xFFFFFFFFFFFFFFFF)
	var recs [][2]int
	pos := 1
	for {
		ref, next := readLEB(t, out, pos)
		if ref == 0 {
			break
		}
		kind := int(out[next])
		next++
		stop, ok := skipWire(t, out, next, idStart, kind)
		if !ok {
			t.Fatalf("a root field does not skip: kind %d at byte %d", kind, next)
		}
		if ref < 1 || ref > count {
			t.Fatalf("a root field names reference %d of %d", ref, count)
		}
		if ids[ref-1] == nodeTableID {
			ln, p := readLEB(t, out, next)
			contentEnd := p + int(ln)
			declared, p := readLEB(t, out, p)
			for range declared {
				tref, at := readLEB(t, out, p)
				rlen, body := readLEB(t, out, at)
				if tref < 1 || tref > count {
					t.Fatalf("a node record names reference %d of %d", tref, count)
				}
				if ids[tref-1] == listNodeID {
					recs = append(recs, [2]int{body, body + int(rlen)})
				}
				p = body + int(rlen)
			}
			if p != contentEnd {
				t.Fatalf("the node table does not spend its L")
			}
		}
		pos = stop
	}
	if len(recs) == 0 {
		t.Fatalf("the wire carries no ListNode record")
	}
	last := recs[len(recs)-1]
	p := last[0]
	found := false
	for {
		ref, next := readLEB(t, out, p)
		if ref == 0 {
			break
		}
		kind := int(out[next])
		next++
		stop, ok := skipWire(t, out, next, last[1], kind)
		if !ok {
			t.Fatalf("a ListNode field does not skip: kind %d at byte %d", kind, next)
		}
		if ids[ref-1] == valueID && kind == 4 {
			binary.LittleEndian.PutUint32(out[next:], bad)
			found = true
		}
		p = stop
	}
	if !found {
		t.Fatalf("the last ListNode record carries no value field")
	}
	return out
}

func TestMooLoadIsOneDecodeFunctional(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	const records = 20000
	const limit = 60 * time.Second
	g := generateFiles(t, map[string]string{"Scene.schema": oneDecodeSchema})
	built, err := runMooDeadline(t, g, oneDecodeBuildProbe(records), nil, limit)
	if err != nil {
		t.Fatalf("building the Scene wire: %v", err)
	}
	wire, err := hex.DecodeString(built)
	if err != nil {
		t.Fatalf("the writer's wire is not hex: %v", err)
	}
	unknownID := ir.TableWireId("future")
	accepted := addUnknownRootField(t, wire, unknownID, 4, []byte{7, 0, 0, 0})
	start := time.Now()
	out, err := runMooDeadline(t, g, oneDecodeLoadProbe, map[string][]byte{"wire": accepted}, limit)
	if err != nil {
		t.Fatalf("the accepted load did not finish within %s: %v", limit, err)
	}
	t.Logf("the accepted 20,000-record load answered in %s", time.Since(start))
	if want := "read " + fmt.Sprintf("#%016x", unknownID); out != want {
		t.Fatalf("the accepted load answered %q, want %q", out, want)
	}
	refusedWire := outOfRangeLastRecord(t, accepted, ir.TableWireId("ListNode"), ir.TableWireId("value"), 2000000)
	start = time.Now()
	out, err = runMooDeadline(t, g, oneDecodeLoadProbe, map[string][]byte{"wire": refusedWire}, limit)
	if err != nil {
		t.Fatalf("the refused load did not finish within %s: %v", limit, err)
	}
	t.Logf("the refused 20,000-record load answered in %s", time.Since(start))
	if want := "refused out_of_range value"; out != want {
		t.Fatalf("the refused load answered %q, want %q", out, want)
	}
}
