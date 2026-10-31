package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

type step struct {
	o, i uint32
}

type rec struct {
	node    uint32
	depth   uint32
	id      uint64
	kind    uint8
	steps   []step
	payload []byte
}

func (r rec) key() string {
	var steps strings.Builder
	for _, s := range r.steps {
		fmt.Fprintf(&steps, "/%d,%d", s.o, s.i)
	}
	return fmt.Sprintf("%d/%d/%d/%d%s/%x", r.node, r.depth, r.id, r.kind, steps.String(), r.payload)
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
	b.WriteString("local inputs = require(\"./inputs\")\nlocal retain = require(\"./schema_retain\")\nlocal load = require(\"./schema_load\")\n")
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
	b.WriteString("\tlocal steps = {}\n")
	b.WriteString("\tfor i = 0, depth - 1 do steps[i + 1] = string.format(\"%d %d\", buffer.readu32(store.bytes, at + 26 + i * 8), buffer.readu32(store.bytes, at + 30 + i * 8)) end\n")
	b.WriteString("\tprint(string.format(\"rec %d %d %s %d %s %s\", node, depth, idhex, kind, table.concat(steps, \" \"), tohex(payload)))\n")
	b.WriteString("\tat = at + total\nend\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))
	r := &retainResult{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
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
		} else if f[0] == "rec" && len(f) >= 6 && (len(f)-6)%2 == 0 {
			node, _ := strconv.Atoi(f[1])
			depth, _ := strconv.Atoi(f[2])
			idBytes, err := hex.DecodeString(f[3])
			if err != nil || len(idBytes) != 8 {
				t.Fatalf("record id is not 8 hex bytes: %v", err)
			}
			id := binary.LittleEndian.Uint64(idBytes)
			kind, _ := strconv.Atoi(f[4])
			rc := rec{node: uint32(node), depth: uint32(depth), id: id, kind: uint8(kind)}
			for i := range depth {
				o, _ := strconv.Atoi(f[5+2*i])
				ix, _ := strconv.Atoi(f[6+2*i])
				rc.steps = append(rc.steps, step{o: uint32(o), i: uint32(ix)})
			}
			payload, err := hex.DecodeString(f[5+2*depth])
			if err != nil {
				t.Fatalf("record payload is not hex: %v", err)
			}
			rc.payload = payload
			r.records = append(r.records, rc)
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

// retainGoOracle runs schema-go's own generated retaining reader over the same
// wire and prints its report and every retained record in the order it keeps
// them, including the path's ordinal/index pairs (docs/SPEC-TABLES.md §6.6).
// The expected identity is therefore the Go engine's answer, never a
// hand-typed one.
func retainGoOracle(t *testing.T, u *ir.Unit, wire []byte, idCapacity int) *retainResult {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	mod := "module probe\n\ngo 1.26\n"
	for _, data := range files {
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go"`) {
			runtime := tfSerializeGoRuntime(t)
			mod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
			break
		}
	}
	probe := fmt.Sprintf(`package %s

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
	"unsafe"
)

func TestRetainOracle(t *testing.T) {
	wire, err := hex.DecodeString(%q)
	if err != nil { t.Fatal(err) }
	n := RootLoadMeasure(wire)
	if n < 0 { t.Fatal("measure") }
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[off : off+uintptr(n)]
	store := TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]TableRetainID, %d)}
	var report TableReport
	if RootLoadRetain(region, wire, &store, &report) == nil { t.Fatal("load") }
	fmt.Printf("report %%d %%d %%d %%v %%d %%d\n", report.Retained, report.RetainLost, report.Unknown, report.Malformed, store.Used, store.Count)
	for at := 0; at < int(store.Used); {
		b := store.Bytes[at:]
		total := int(binary.LittleEndian.Uint32(b))
		node := binary.LittleEndian.Uint32(b[4:])
		depth := int(binary.LittleEndian.Uint32(b[8:]))
		plen := int(binary.LittleEndian.Uint32(b[12:]))
		id := binary.LittleEndian.Uint64(b[16:])
		var idBytes [8]byte
		binary.LittleEndian.PutUint64(idBytes[:], id)
		kind := b[24]
		fmt.Printf("rec %%d %%d %%x %%d", node, depth, idBytes, kind)
		for i := 0; i < depth; i++ {
			fmt.Printf(" %%d %%d", binary.LittleEndian.Uint32(b[26+8*i:]), binary.LittleEndian.Uint32(b[30+8*i:]))
		}
		fmt.Printf(" %%x\n", b[26+depth*8:26+depth*8+plen])
		at += total
	}
}
`, u.Package, hex.EncodeToString(wire), idCapacity)
	files["oracle_test.go"] = []byte(probe)
	files["go.mod"] = []byte(mod)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestRetainOracle$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return parseRetainLines(t, string(out))
}

// parseRetainLines reads the report and records a retaining probe prints.
func parseRetainLines(t *testing.T, out string) *retainResult {
	t.Helper()
	r := &retainResult{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
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
		} else if f[0] == "rec" && len(f) >= 6 && (len(f)-6)%2 == 0 {
			node, _ := strconv.Atoi(f[1])
			depth, _ := strconv.Atoi(f[2])
			idBytes, err := hex.DecodeString(f[3])
			if err != nil || len(idBytes) != 8 {
				t.Fatalf("record id is not 8 hex bytes: %v", err)
			}
			id := binary.LittleEndian.Uint64(idBytes)
			kind, _ := strconv.Atoi(f[4])
			rc := rec{node: uint32(node), depth: uint32(depth), id: id, kind: uint8(kind)}
			for i := range depth {
				o, _ := strconv.Atoi(f[5+2*i])
				ix, _ := strconv.Atoi(f[6+2*i])
				rc.steps = append(rc.steps, step{o: uint32(o), i: uint32(ix)})
			}
			payload, err := hex.DecodeString(f[5+2*depth])
			if err != nil {
				t.Fatalf("record payload is not hex: %v", err)
			}
			rc.payload = payload
			r.records = append(r.records, rc)
		}
	}
	if r.count != len(r.records) {
		t.Fatalf("the oracle kept %d records but printed %d", r.count, len(r.records))
	}
	return r
}

// retainDeepSchema spells a by-value chain long enough to drive the resolving
// walk's stated nesting cap of 64 bodies (docs/SPEC-TABLES.md §6.6). Every table
// but the last carries a leaf `n` and the next `f`, so one JSON value can stop
// at any body. The pointer anchor keeps the root in the VARIABLE class, which
// is the only class retention is a region round trip through (§6.6).
func retainDeepSchema(depth int) string {
	var b strings.Builder
	b.WriteString("package deep\n")
	// a table must be declared before it is named, so the chain is spelled
	// from the leaf back to the outermost body
	fmt.Fprintf(&b, "table T%d { n int32 }\n", depth)
	for i := depth - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "table T%d { n int32\n f T%d }\n", i, i+1)
	}
	b.WriteString("table Anchor { n int32 }\n")
	b.WriteString("table Root { seed int32\n ptr Anchor\n deep T0 }\n")
	return b.String()
}

// retainDeepOldSchema is the same unit BEFORE the deep field is known: the
// reader sees `deep` as an unknown field and the resolving walk captures its
// whole nested payload (docs/SPEC-TABLES.md §6.6).
const retainDeepOldSchema = `package deep
table Anchor { n int32 }
table Root { seed int32
 ptr Anchor }
`

// retainDeepJSON wraps `depth` T bodies under Root's unknown field.
func retainDeepJSON(bodies int) string {
	s := `{"n":1}`
	for i := 1; i < bodies; i++ {
		s = `{"f":` + s + `}`
	}
	return `{"deep":` + s + `}`
}

func TestLuauLoadRetainKeepsUnknown(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
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
	// PER-RECORD IDENTITY, IN ORDER (§6.6): the body, every path step, the
	// field's id and kind, and the resolved payload must be the records
	// schema-go's own generated reader keeps from the same wire, not merely a
	// set of plausible ones.
	oracle := retainGoOracle(t, oldUnit, wire, 1024)
	if oracle.malformed {
		t.Fatalf("the Go oracle raised malformed on a good read")
	}
	if oracle.retained != oracleReport.Retained || oracle.lost != oracleReport.RetainLost || oracle.unknown != oracleReport.Unknown {
		t.Fatalf("the Go oracle disagrees with tablewire: got retained=%d lost=%d unknown=%d, tablewire %+v", oracle.retained, oracle.lost, oracle.unknown, oracleReport)
	}
	if !reflect.DeepEqual(big.records, oracle.records) {
		for i := 0; i < len(big.records) && i < len(oracle.records); i++ {
			if !reflect.DeepEqual(big.records[i], oracle.records[i]) {
				t.Fatalf("record %d: Luau kept %+v, Go oracle kept %+v", i, big.records[i], oracle.records[i])
			}
		}
		t.Fatalf("Luau kept %d records, Go oracle kept %d", len(big.records), len(oracle.records))
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

	// THE CAPACITY BOUND, ONE PAST: a record the remaining capacity cannot hold
	// WHOLE is not kept at all and counts one retain_lost.
	capOldSrc := "package capdemo\ntable Root { seed int32 }\n"
	capNewSrc := "package capdemo\ntable Root { seed int32\n extra string(32) }\n"
	capOld, _ := retainCompile(t, capOldSrc)
	capNew, _ := retainCompile(t, capNewSrc)
	capWire := retainWire(t, capNew, `{"extra":"hello"}`)
	capOracle := retainGoOracle(t, capOld, capWire, 1024)
	if capOracle.retained != 1 || capOracle.used <= 0 {
		t.Fatalf("the capacity fixture kept %+v", capOracle)
	}
	capGo := generateFiles(t, map[string]string{"Cap.schema": capOldSrc})
	atCap := retainRunLuau(t, capGo, capWire, capOracle.used)
	if atCap.retained != 1 || atCap.lost != 0 || !reflect.DeepEqual(atCap.records, capOracle.records) {
		t.Fatalf("capacity %d kept %+v, want the oracle's one record %+v", capOracle.used, atCap, capOracle.records)
	}
	under := retainRunLuau(t, capGo, capWire, capOracle.used-1)
	if under.retained != 0 || under.lost != 1 || under.count != 0 || len(under.records) != 0 {
		t.Fatalf("capacity %d (one past) kept %+v, want 0 retained and one retain_lost", capOracle.used-1, under)
	}

	// THE ID LIST IS THE SAVE'S BOUND AND NOT THE LOAD'S (docs/SPEC-TABLES.md
	// §6.6, THE LIST FILLS AS THE SAVE WALK INTERNS): LoadRetain resets both
	// stores and writes into neither, so an id list ONE ENTRY SHORT of the
	// retained ids drops no record and leaves every load counter where the full
	// list left it. schema-go's generated reader and the Luau one answer the
	// same, record for record.
	distinct := map[uint64]bool{}
	for _, rc := range oracle.records {
		distinct[rc.id] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("the wire retains %d distinct ids; one short of that is not a row", len(distinct))
	}
	idShort := len(distinct) - 1
	idStore := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: idShort}
	idValue := oldModel.New(oldModel.Lookup("Root"))
	var idReport tabletext.Report
	ok, err = tablewire.DecodeRetain(oldModel, idValue, wire, idStore, &idReport)
	if !ok || err != nil || idReport.Malformed {
		t.Fatalf("id capacity %d (one past): the oracle read failed: %v %v %+v", idShort, ok, err, idReport)
	}
	if idReport.Retained != oracleReport.Retained || idReport.RetainLost != oracleReport.RetainLost {
		t.Fatalf("id capacity %d moved a LOAD counter: retained=%d lost=%d, full list %+v",
			idShort, idReport.Retained, idReport.RetainLost, oracleReport)
	}
	idGo := retainGoOracle(t, oldUnit, wire, idShort)
	if !reflect.DeepEqual(idGo.records, oracle.records) {
		t.Fatalf("id capacity %d: the Go reader kept different records than the full list", idShort)
	}
	if !reflect.DeepEqual(big.records, idGo.records) {
		t.Fatalf("id capacity %d: Luau kept %d records, Go kept %d", idShort, len(big.records), len(idGo.records))
	}

	// THE WALK'S NESTING CAP (docs/SPEC-TABLES.md §6.6): 64 nested bodies are
	// the last depth admitted, and one past is dropped on the same rule as any
	// other shape the walk cannot take, counting one retain_lost and never
	// malformed. The record the cap admits is compared with schema-go's own
	// reader, in order.
	for _, row := range []struct {
		bodies   int
		retained int
		lost     int
		what     string
	}{
		{bodies: 64, retained: 1, lost: 0, what: "the last depth the cap admits"},
		{bodies: 65, retained: 0, lost: 1, what: "one past the cap"},
	} {
		t.Run("depth cap "+row.what, func(t *testing.T) {
			deepOld, _ := retainCompile(t, retainDeepOldSchema)
			deepNew, _ := retainCompile(t, retainDeepSchema(row.bodies))
			deepWire := retainWire(t, deepNew, retainDeepJSON(row.bodies))
			deepModel := tabletext.NewModel(deepOld)
			deepValue := deepModel.New(deepModel.Lookup("Root"))
			var deepReport tabletext.Report
			deepKeep := &tablewire.Retain{Capacity: 1 << 16, IdCapacity: 1024}
			deepOK, deepErr := tablewire.DecodeRetain(deepModel, deepValue, deepWire, deepKeep, &deepReport)
			if !deepOK || deepErr != nil || deepReport.Malformed {
				t.Fatalf("%d nested bodies: the oracle read failed on a sound wire: %v %v %+v",
					row.bodies, deepOK, deepErr, deepReport)
			}
			if deepReport.Retained != row.retained || deepReport.RetainLost != row.lost {
				t.Fatalf("%d nested bodies: oracle retained=%d lost=%d, want %d / %d",
					row.bodies, deepReport.Retained, deepReport.RetainLost, row.retained, row.lost)
			}
			deepGo := retainGoOracle(t, deepOld, deepWire, 1024)
			if deepGo.malformed {
				t.Fatalf("%d nested bodies: the Go reader raised malformed", row.bodies)
			}
			if deepGo.retained != row.retained || deepGo.lost != row.lost {
				t.Fatalf("%d nested bodies: Go retained=%d lost=%d, want %d / %d",
					row.bodies, deepGo.retained, deepGo.lost, row.retained, row.lost)
			}
			deepGenerated := generateFiles(t, map[string]string{"Deep.schema": retainDeepOldSchema})
			deepLuau := retainRunLuau(t, deepGenerated, deepWire, 1<<16)
			if deepLuau.malformed {
				t.Fatalf("%d nested bodies: LoadRetain raised malformed on a sound wire", row.bodies)
			}
			if deepLuau.retained != row.retained || deepLuau.lost != row.lost {
				t.Fatalf("%d nested bodies: Luau retained=%d lost=%d, want %d / %d",
					row.bodies, deepLuau.retained, deepLuau.lost, row.retained, row.lost)
			}
			if len(deepLuau.records) != len(deepGo.records) {
				t.Fatalf("%d nested bodies: Luau kept %d records, Go kept %d",
					row.bodies, len(deepLuau.records), len(deepGo.records))
			}
			// THE NESTED PAYLOAD'S INNER LENGTHS ARE THE PORT'S OWN
			// READER-PRIVATE SPELLING (docs/SPEC-TABLES.md §6.6), so the
			// admitted record is compared by its identity and path, not by
			// another port's bytes; the flat records above are compared whole.
			for i := range deepLuau.records {
				a, b := deepLuau.records[i], deepGo.records[i]
				if a.node != b.node || a.depth != b.depth || a.id != b.id || a.kind != b.kind || !reflect.DeepEqual(a.steps, b.steps) || len(a.payload) == 0 {
					t.Fatalf("%d nested bodies: Luau record %d %+v, Go record %+v",
						row.bodies, i, a, b)
				}
			}
		})
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
