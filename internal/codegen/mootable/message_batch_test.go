package mootable_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE MESSAGE FORM'S BATCH, held to the Go engine's reader over DAMAGE
// (docs/SPEC-TABLES.md §3.3): the pinned batches of fixed roots — one body
// each, and a batch of three — are mutated (every truncation, the form byte, the count byte, bytes after the
// pad, every byte flipped three ways), and moo's LoadMessages must answer each
// mutation as tablewire.DecodeMessages does: damaged input is refused whole,
// with no values; otherwise the refusal by name, body count and evolution
// counters agree. No partial result after damage is compared (§4).

type batchCase struct {
	name, conn, root, wire string
}

var batchCases = []batchCase{
	{"login_full", "backend_conn", "LoginRequest", "testdata/wire/tables/login_full_message.bin"},
	{"match_full", "backend_conn", "MatchResult", "testdata/wire/tables/match_full_message.bin"},
	{"store_full", "backend_conn", "StorePurchase", "testdata/wire/tables/store_full_message.bin"},
	{"backend_batch", "backend_conn", "LoginRequest", "testdata/wire/tables/backend_batch_damaged_second_message.bin"},
}

// mutations is the damage swept over one wire, each distinct, in a fixed order.
func mutations(wire []byte) [][]byte {
	seen := map[string]bool{}
	var out [][]byte
	add := func(b []byte) {
		if !seen[string(b)] {
			seen[string(b)] = true
			out = append(out, b)
		}
	}
	add(wire)
	for n := range wire {
		add(append([]byte(nil), wire[:n]...))
	}
	for _, tail := range [][]byte{{0}, {0xFF}, {0, 0}} {
		add(append(append([]byte(nil), wire...), tail...))
	}
	for _, form := range []byte{0, 1, 3, 4, 0xFF} {
		b := append([]byte(nil), wire...)
		b[0] = form
		add(b)
	}
	for _, count := range []byte{0, 1, 2, 3, 0x7F, 0xFF} {
		b := append([]byte(nil), wire...)
		b[1] = count
		add(b)
	}
	for i := 1; i < len(wire); i++ {
		for _, f := range []func(byte) byte{func(x byte) byte { return x ^ 0xFF }, func(x byte) byte { return x ^ 1 }, func(byte) byte { return 0 }} {
			b := append([]byte(nil), wire...)
			b[i] = f(b[i])
			add(b)
		}
	}
	return out
}

func packCases(cases [][]byte) []byte {
	var out []byte
	for _, c := range cases {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(c)))
		out = append(out, c...)
	}
	return out
}

func damageProbe(g *generatedUnit, root string, capacity int) string {
	return fmt.Sprintf(`--!strict
local inputs = require("./inputs")
local MS = require("./table_message")
%slocal cases = inputs.cases
local at = 0
local i = 0
while at < buffer.len(cases) do
	local len = buffer.readu32(cases, at)
	local data = buffer.create(len)
	buffer.copy(data, 0, cases, at + 4, len)
	at += 4 + len
	i += 1
	local v = MS.NewVocabulary()
	local rep: any = {}
	if not MS.AnnounceRead(v, inputs.conn, rep) then print("ANNOUNCE " .. tostring(rep.reason)) return end
	local n, values = MS.LoadMessages(v, %q, data, %d, rep)
	if rep.malformed and #values ~= 0 then print("DAMAGE RETURNED VALUES") continue end
	print(string.format("%%d %%d %%d,%%d,%%d,%%d,%%d,%%s,%%d %%s", i, n, rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, tostring(rep.malformed), rep.blobShared or 0, tostring(rep.reason)))
end
`, g.requireAll(), root, capacity)
}

// goAnswer is the Go engine's answer to one mutation in moo's line format.
func goAnswer(m *tabletext.Model, vocab *tablewire.Vocabulary, root string, capacity int, data []byte) string {
	insts := make([]*tabletext.Instance, capacity)
	for i := range insts {
		insts[i] = m.New(m.Lookup(root))
	}
	var rep tabletext.Report
	n, _, err := tablewire.DecodeMessages(m, insts, data, vocab, &rep)
	if rep.Malformed {
		return "malformed" // §4: no other result of a damaged read is defined.
	}
	if err != nil {
		var reason string
		if r, ok := errors.AsType[*tablewire.MessageRefusal](err); ok {
			reason = r.Reason.String()
			if r.Reason != tablewire.ReasonBatchTooLarge {
				n = -1
			}
		} else {
			reason = "non_finite_float"
			n = 0
		}
		return fmt.Sprintf("%d %s", n, reason)
	}
	return fmt.Sprintf("%d %d,%d,%d,%d,%d,%v,%d", n, rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed, rep.BlobShared)
}

// mooAnswer reduces a moo line (without its index) to goAnswer's format.
func mooAnswer(line string) string {
	f := strings.SplitN(line, " ", 3)
	if len(f) < 3 {
		return line
	}
	if strings.Contains(f[1], ",true,") {
		if f[0] != "0" && f[0] != "-1" {
			return "malformed batch returned bodies: " + f[0]
		}
		return "malformed"
	}
	if f[2] != "nil" {
		reason, _, _ := strings.Cut(f[2], ":")
		return fmt.Sprintf("%s %s", f[0], reason)
	}
	return fmt.Sprintf("%s %s", f[0], f[1])
}

func runDamage(t *testing.T, capacity int, mutate func([]byte) [][]byte) {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "backenddemo")
	if g.err != nil {
		t.Fatalf("unit backenddemo is refused: %v", g.err)
	}
	total := 0
	for _, c := range batchCases {
		conn := readRoot(t, "testdata/wire/tables/"+c.conn+".bin")
		wire := readRoot(t, c.wire)
		cases := mutate(wire)
		m := tabletext.NewModel(g.unit)
		var vocab tablewire.Vocabulary
		var arep tabletext.Report
		if err := vocab.AnnounceRead(conn, &arep); err != nil {
			t.Fatal(err)
		}
		out := g.run(t, damageProbe(g, c.root, capacity), map[string][]byte{"conn": conn, "cases": packCases(cases)})
		lines := strings.Split(out, "\n")
		if len(lines) != len(cases) {
			t.Fatalf("%s: moo answered %d of %d cases:\n%s", c.name, len(lines), len(cases), out)
		}
		bad := 0
		for i, line := range lines {
			_, rest, _ := strings.Cut(line, " ")
			want := goAnswer(m, &vocab, c.root, capacity, cases[i])
			if got := mooAnswer(rest); got != want {
				if bad++; bad <= 5 {
					t.Errorf("%s: case %d (%d bytes: %x): moo %q (%s), the Go engine %q", c.name, i, len(cases[i]), cases[i], got, rest, want)
				}
			}
		}
		total += len(cases)
	}
	t.Logf("%d damaged batches answered as the Go engine answers them", total)
}

func TestMooMessageBatchDamageMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	runDamage(t, 256, mutations)
}

// TestMooMessageBatchCapacityMatchesTheGoEngine: a batch above the caller's
// capacity is batch_too_large with the wire's count and nothing decoded, at
// every capacity from one body up.
func TestMooMessageBatchCapacityMatchesTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	for _, capacity := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			t.Parallel()
			runDamage(t, capacity, func(w []byte) [][]byte { return [][]byte{w} })
		})
	}
}

// TestMooMessageBatchWriteBounds: SaveMessages and MeasureMessages refuse a
// batch of none or of 257 by name, batch_too_large, and write 256.
func TestMooMessageBatchWriteBounds(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "backenddemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + `
local v = load.ReadStrict("LoginRequest", inputs.file, load.newReport())
assert(v ~= nil)
for _, n in { 0, 1, 255, 256, 257, 300 } do
	local values = {}
	for i = 1, n do values[i] = v end
	local rep: any = {}
	local size = MS.MeasureMessages("LoginRequest", values, rep)
	local out, saved = MS.SaveMessages("LoginRequest", values, rep)
	print(string.format("%d %d %s %s %s", n, size, tostring(out ~= nil), tostring(saved), tostring(rep.reason)))
end
`
	got := g.run(t, main, map[string][]byte{"file": readRoot(t, "testdata/wire/tables/login_full.bin")})
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("unexpected answer:\n%s", got)
	}
	for i, n := range []int{0, 1, 255, 256, 257, 300} {
		f := strings.Fields(lines[i])
		refused := n < 1 || n > 256
		if refused && (f[1] != "-1" || f[2] != "false" || f[4] != "batch_too_large") {
			t.Errorf("a batch of %d is not refused batch_too_large: %s", n, lines[i])
		}
		if !refused && (f[2] != "true" || f[1] != f[3] || f[1] == "-1") {
			t.Errorf("a batch of %d does not save at its measure: %s", n, lines[i])
		}
	}
}

// TestMooMessageBatchDamageNegativeControl: the sweep goes red when the pad
// rule is dropped — a reader that no longer verifies the trailing pad and the
// bytes after it answers a clean batch where the Go engine answers malformed.
func TestMooMessageBatchDamageNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "backenddemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	src := string(g.files["table_message.moo"])
	const anchor = "if got == count and (not ralign(r) or r.off ~= r.n) then"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the pad rule's anchor is not in table_message.moo exactly once")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_message.moo"), []byte(strings.Replace(src, anchor, "if false then", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := readRoot(t, "testdata/wire/tables/backend_conn.bin")
	wire := readRoot(t, "testdata/wire/tables/login_full_message.bin")
	cases := [][]byte{append(append([]byte(nil), wire...), 0)}
	out := g.run(t, damageProbe(g, "LoginRequest", 256), map[string][]byte{"conn": conn, "cases": packCases(cases)})
	_, rest, _ := strings.Cut(out, " ")
	if !strings.Contains(rest, ",false,") {
		t.Fatalf("the patched reader still answers the byte after the pad as malformed: %s", out)
	}
}

// TestMooMessageFormIsFixedTablesOnly holds §3.3's class rule on the moo leg:
// a variable table registers no message slots, and LoadMessages,
// MeasureMessages and SaveMessages refuse a variable root by name before a
// byte is read or written, while the fixed root's batch still measures.
func TestMooMessageFormIsFixedTablesOnly(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Probe.schema": `package probe

fixed table Fixedroot { x int32 }
table Varplain { x int32 }
table Varleaf { x int32 }
table Varroot { leaf *Varleaf }
`})
	var module string
	for _, m := range g.tableModules() {
		module += string(g.files[m+".moo"])
	}
	if !strings.Contains(module, `.slots("Fixedroot"`) {
		t.Error("the fixed root Fixedroot registers no message slots")
	}
	for _, name := range []string{"Varplain", "Varleaf", "Varroot"} {
		if strings.Contains(module, `.slots("`+name+`"`) {
			t.Errorf("the variable table %s registers message slots", name)
		}
	}
	main := "--!strict\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + `
local function refused(name: string, f: () -> ())
	local ok, err = pcall(f)
	assert(not ok, name .. " was not refused")
	assert(string.find(tostring(err), "Varplain", 1, true) ~= nil, name .. " was refused without naming the table: " .. tostring(err))
end
local S = require("./schema_tables")
local v = S.lookup("Varplain").new()
refused("LoadMessages", function() MS.LoadMessages(MS.NewVocabulary(), "Varplain", buffer.fromstring("\2\0"), 1, {}) end)
refused("MeasureMessages", function() MS.MeasureMessages("Varplain", { v }, {}) end)
refused("SaveMessages", function() MS.SaveMessages("Varplain", { v }, {}) end)
assert(MS.MeasureMessages("Fixedroot", { S.lookup("Fixedroot").new() }, {}) > 0, "the fixed root's batch did not measure")
print("fixed-only-ok")
`
	if out := g.run(t, main, nil); !strings.Contains(out, "fixed-only-ok") {
		t.Fatalf("the probe did not finish: %q", out)
	}
}
