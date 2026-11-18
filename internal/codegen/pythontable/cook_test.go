package pythontable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cookSchema = `package cookcorpus

enum Shade { Red, Blue, Green }
flags Traits { Fast, Strong }
union Pick
{
    number int32
    leaf Leaf
    next *Node
    ping
    extra WithList
    words string(8)
}

union Outer
{
    choice Pick
}

table WithList
{
    numbers []int32
}

fixed table Leaf
{
    enabled bool
    small   int8
    count   uint32
    wide    int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    huge    uint128
    label   string(8)
    raw     bytes(8)
    shade   Shade
    traits  Traits
}

table Node
{
    value int32
    score uint64
    next  *Node
}

table Root
{
    live  bool
    total int64
    head  *Node
    tail  *Node
    leaf  Leaf
    stamp uint16
    pairs [2]Leaf
    counted [..2]Leaf
    banks [Shade]Leaf
    spare ?Leaf
    blob *bytes
    text_blob *string
    pick Pick
    scores []int32
    flags []bool
    friends []Leaf
    values map[int32]int32
    points map[uint8]Leaf
    matrix map[uint8]map[uint8]int32
    labels map[string(8)]int32
    links []Node
    outer Outer
    trail [2]Pick
    many [..2]Pick
    maybe ?int32
    optional_pairs ?[2]Leaf
    optional_ints ?[2]int32
}
`

// TestPythonCook holds the cooked form's runtime surface (docs/SPEC-TABLES.md
// §7, §7.1, §7.2, §7.4, §7.6; docs/PORT-PYTHON.md, THE TABLE FRAME) to the
// compiler's own cook: the runtime writes the tool's bytes in both byte orders,
// answers the measure, opens the file the tool wrote, and refuses the damage
// cook-check names. The corpus is a fixed root (a nested fixed table and the
// full-width integers, 128-bit included) and a variable root with a pointer
// chain whose tail is shared. The runtime carries bool, the integers, by-value
// tables and *T pointers, so the forged frames below exercise those shapes.
func TestPythonCook(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict analyzer")
	t.Run("write", testPythonCookWrite)
	t.Run("open", testPythonCookOpen)
	t.Run("negative-controls", testPythonCookNegativeControls)
}

const cookWriteProbe = `from cookcorpus import Leaf, Node, Root, Pick
from cookcorpus._schema import cook

def full():
    r = Root()
    r.live = True
    r.total = -5
    r.stamp = 65535
    r.pairs[0].small = -3
    r.pairs[1].label = "hi"
    r.counted = [Leaf()]
    r.counted[0].count = 42
    r.banks[1].small = -2
    r.banks[3].label = "last"
    r.spare = Leaf()
    r.spare.small = 5
    r.blob = b"\x00\xff\x08"
    r.text_blob = "hello"
    r.pick.type = 1
    r.pick.number = -11
    r.scores = [12, -7, 42]
    r.flags = [True, False, True]
    r.friends = [Leaf(), Leaf()]
    r.friends[0].small = -3
    r.friends[1].label = "list"
    r.values = {7: -3, -2: 11}
    r.points = {2: Leaf()}
    r.points[2].label = "map"
    r.matrix = {2: {3: -8, 1: 7}, 1: {5: 2}}
    r.labels = {"é": 4, "a": -2}
    r.links = [Node()]
    r.links[0].value = 8
    r.links[0].next = Node()
    r.links[0].next.value = -9
    r.outer.type = 1
    r.outer.choice.type = 1
    r.outer.choice.number = 21
    r.trail[0].type = 1
    r.trail[0].number = -8
    r.trail[1].type = 6
    r.trail[1].words = "ok"
    r.many = [Pick()]
    r.many[0].type = 5
    r.many[0].extra.numbers = [4, 8]
    r.maybe = -14
    r.optional_pairs = [Leaf(), Leaf()]
    r.optional_pairs[1].label = "held"
    r.optional_ints = [4, -8]
    l = r.leaf
    l.enabled = True
    l.small = -7
    l.count = 0xFFFFFFFF
    l.wide = -(1 << 120) + 12345
    l.huge = (1 << 127) + 987654321
    l.label = "café"
    l.raw = b"\x00\xff"
    l.shade = 2
    l.traits = 3
    a = Node()
    a.value = 1
    a.score = (1 << 64) - 1
    b = Node()
    b.value = -2
    b.score = 42
    c = Node()
    c.value = 3
    c.score = 0
    a.next = b
    b.next = c
    r.head = a
    r.tail = b
    return r

def defaults():
    r = Root()
    r.live = False
    r.total = 0
    r.stamp = 0
    l = r.leaf
    l.enabled = False
    l.small = 0
    l.count = 0
    l.wide = 0
    l.huge = 0
    return r

def leaf():
    v = Leaf()
    v.enabled = True
    v.small = -128
    v.count = 4294967295
    v.wide = -(1 << 127)
    v.huge = (1 << 128) - 1
    v.label = "hello"
    v.raw = b"\x00\x01"
    v.shade = 3
    v.traits = 1
    return v

def union_leaf():
    r = Root(); r.pick.type = 2; r.pick.leaf.small = -4; r.pick.leaf.label = "leaf"; return r

def union_pointer():
    r = Root(); r.pick.type = 3; r.pick.next = Node(); r.pick.next.value = 17; return r

def union_void():
    r = Root(); r.pick.type = 4; return r

def union_list():
    r = Root(); r.pick.type = 5; r.pick.extra.numbers = [2, 9]; return r

def union_text():
    r = Root(); r.pick.type = 6; r.pick.words = "hé"; return r

def nested_union_list():
    r = Root(); r.outer.type = 1; r.outer.choice.type = 5; r.outer.choice.extra.numbers = [3]; return r

for name, value in (("full", full()), ("empty", Root()), ("defaults", defaults()), ("leaf", leaf()), ("union_leaf", union_leaf()), ("union_pointer", union_pointer()), ("union_void", union_void()), ("union_list", union_list()), ("nested_union_list", nested_union_list()), ("union_text", union_text())):
    wire = value.save()
    print("WIRE", name, wire.hex())
    measure = cook.cook_measure(value)
    for order in (1, 2):
        out = cook.cook(value, order)
        print("COOK", name, order, measure, len(out), out.hex())
print("OK")
`

type writtenCook struct {
	wire    []byte
	measure int
	length  [2]int
	cook    [2][]byte
}

type cookRunner func(t testing.TB, dir, main string, inputs map[string][]byte) []byte

func cookPins() map[string]cookRunner {
	return map[string]cookRunner{
		"python-3.15.2":  pyrun.Run,
		"python-3.11.18": pyrun.RunFloor,
	}
}

func testPythonCookWrite(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, cookSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "cookcorpus"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "cookcorpus/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
	for pin, runner := range cookPins() {
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + cookWriteProbe)}))
		if err := verifyCookWrite(u, out); err != nil {
			t.Errorf("%s: %v", pin, err)
		}
	}
}

// parsedCookWrite is the probe's output, parsed once.
type parsedCookWrite map[string]*writtenCook

func parseCookWrite(out string) (parsedCookWrite, error) {
	if !strings.Contains(out, "OK\n") {
		return nil, fmt.Errorf("the probe did not finish:\n%s", out)
	}
	seen := parsedCookWrite{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "WIRE":
			if len(fields) != 3 {
				return nil, fmt.Errorf("bad WIRE line %q", line)
			}
			raw, err := hex.DecodeString(fields[2])
			if err != nil {
				return nil, fmt.Errorf("WIRE %s is not hex: %w", fields[1], err)
			}
			seen[fields[1]] = &writtenCook{wire: raw}
		case "COOK":
			if len(fields) != 6 {
				return nil, fmt.Errorf("bad COOK line %q", line)
			}
			got := seen[fields[1]]
			if got == nil {
				return nil, fmt.Errorf("COOK %s arrives before its wire", fields[1])
			}
			order, err := strconv.Atoi(fields[2])
			if err != nil || order < 1 || order > 2 {
				return nil, fmt.Errorf("COOK %s has no byte order: %q", fields[1], fields[2])
			}
			measure, err := strconv.Atoi(fields[3])
			if err != nil {
				return nil, fmt.Errorf("COOK %s measure %q: %w", fields[1], fields[3], err)
			}
			length, err := strconv.Atoi(fields[4])
			if err != nil {
				return nil, fmt.Errorf("COOK %s length %q: %w", fields[1], fields[4], err)
			}
			raw, err := hex.DecodeString(fields[5])
			if err != nil {
				return nil, fmt.Errorf("COOK %s is not hex: %w", fields[1], err)
			}
			got.measure = measure
			got.length[order-1] = length
			got.cook[order-1] = raw
		}
	}
	return seen, nil
}

// verifyCookWrite holds the probe's wire and cooks to the compiler's own
// tablecook.Cook in both byte orders, and the measure to the file's length.
func verifyCookWrite(u *ir.Unit, out string) error {
	seen, err := parseCookWrite(out)
	if err != nil {
		return err
	}
	m := tabletext.NewModel(u)
	roots := map[string]string{"full": "Root", "empty": "Root", "defaults": "Root", "leaf": "Leaf", "union_leaf": "Root", "union_pointer": "Root", "union_void": "Root", "union_list": "Root", "nested_union_list": "Root", "union_text": "Root"}
	if len(seen) != len(roots) {
		return fmt.Errorf("the probe wrote %d values, the corpus has %d", len(seen), len(roots))
	}
	for name, root := range roots {
		got := seen[name]
		inst := m.New(m.Lookup(root))
		var report tabletext.Report
		if err := decodeCookWire(m, inst, got.wire, &report); err != nil {
			return fmt.Errorf("%s: the runtime's wire does not decode: %w", name, err)
		}
		little, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			return fmt.Errorf("%s: the tool cooks no little-endian file: %w", name, err)
		}
		big, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
		if err != nil {
			return fmt.Errorf("%s: the tool cooks no big-endian file: %w", name, err)
		}
		for order, want := range [][]byte{little, big} {
			if got.length[order] != len(want) {
				return fmt.Errorf("%s order %d: Cook wrote %d bytes, the tool's file is %d", name, order+1, got.length[order], len(want))
			}
			if got.measure != len(want) {
				return fmt.Errorf("%s order %d: CookMeasure = %d, the tool's file is %d", name, order+1, got.measure, len(want))
			}
			if !bytesEqual(got.cook[order], want) {
				return fmt.Errorf("%s order %d: cook differs from the tool: %s", name, order+1, firstByteDifference(got.cook[order], want))
			}
		}
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func firstByteDifference(got, want []byte) string {
	for i := range want {
		if i >= len(got) {
			return fmt.Sprintf("the runtime wrote %d bytes, the tool %d", len(got), len(want))
		}
		if got[i] != want[i] {
			return fmt.Sprintf("first difference at byte %d: got %02x want %02x", i, got[i], want[i])
		}
	}
	return fmt.Sprintf("the runtime wrote %d bytes, the tool %d", len(got), len(want))
}

const cookOpenProbe = `from cookcorpus import Leaf, Root
from cookcorpus._schema import Refused, cook

roots = {"Root": Root, "Leaf": Leaf}
for line in open("cases", "r", encoding="utf-8").read().splitlines():
    if not line.strip():
        continue
    parts = line.split()
    name, root, path = parts[0], parts[1], parts[2]
    base = int(parts[3]) if len(parts) > 3 else 0
    data = open(path, "rb").read()
    cls = roots[root]
    for label, call in (("open", cook.open_cook), ("check", cook.check_cook)):
        try:
            call(cls, data, base)
            print(name, label, "open")
        except Refused as err:
            print(name, label, err.reason)
print("OK")
`

type cookOpenCase struct {
	name  string
	root  string
	data  []byte
	base  int
	open  string
	check string
}

func testPythonCookOpen(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, cookSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	cases, oracle := cookOpenCases(t, u, files)
	dir := t.TempDir()
	writeTree(t, dir, files)
	var manifest strings.Builder
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + cookOpenProbe)}
	for _, tc := range cases {
		file := "case_" + tc.name
		inputs[file] = tc.data
		fmt.Fprintf(&manifest, "%s %s %s %d\n", tc.name, tc.root, file, tc.base)
	}
	inputs["cases"] = []byte(manifest.String())
	_ = oracle
	for pin, runner := range cookPins() {
		out := string(runner(t, dir, "main.py", inputs))
		if err := verifyCookOpen(cases, out); err != nil {
			t.Errorf("%s: %v", pin, err)
		}
	}
}

func verifyCookOpen(cases []cookOpenCase, out string) error {
	if !strings.Contains(out, "OK\n") {
		return fmt.Errorf("the probe did not finish:\n%s", out)
	}
	got := map[string]map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		if got[fields[0]] == nil {
			got[fields[0]] = map[string]string{}
		}
		got[fields[0]][fields[1]] = fields[2]
	}
	for _, tc := range cases {
		for _, side := range []struct {
			label string
			want  string
		}{{"open", tc.open}, {"check", tc.check}} {
			if have := got[tc.name][side.label]; have != side.want {
				return fmt.Errorf("%s: %s answered %q, want %q", tc.name, side.label, have, side.want)
			}
		}
	}
	return nil
}

// Decode the input form the generated root carries. Variable roots use form 1;
// an admitted fixed root uses form 3 once that runtime is integrated.
func decodeCookWire(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) error {
	if len(wire) == 0 {
		return fmt.Errorf("empty wire")
	}
	if wire[0] == 1 {
		_, err := tablewire.DecodeVariable(m, inst, wire, report)
		return err
	}
	ok, err := tablewire.Decode(m, inst, wire, report)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("wire refused: %s", report.Reason)
	}
	return nil
}

// cookOracle is the tool's little- and big-endian cooks of the probe's corpus
// values, keyed by the probe's value name.
type cookOracle struct {
	cooks map[string][2][]byte
	m     *tabletext.Model
}

func makeCookOracle(t *testing.T, u *ir.Unit, files map[string][]byte) *cookOracle {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	out := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + cookWriteProbe)}))
	seen, err := parseCookWrite(out)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	roots := map[string]string{"full": "Root", "empty": "Root", "defaults": "Root", "leaf": "Leaf", "union_leaf": "Root", "union_pointer": "Root", "union_void": "Root", "union_list": "Root", "nested_union_list": "Root", "union_text": "Root"}
	oracle := &cookOracle{cooks: map[string][2][]byte{}, m: m}
	for name, root := range roots {
		inst := m.New(m.Lookup(root))
		var report tabletext.Report
		if err := decodeCookWire(m, inst, seen[name].wire, &report); err != nil {
			t.Fatal(err)
		}
		little, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		big, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
		if err != nil {
			t.Fatal(err)
		}
		oracle.cooks[name] = [2][]byte{little, big}
	}
	return oracle
}

func cookOpenCases(t *testing.T, u *ir.Unit, files map[string][]byte) ([]cookOpenCase, *cookOracle) {
	t.Helper()
	oracle := makeCookOracle(t, u, files)
	little := oracle.cooks["full"][0]
	big := oracle.cooks["full"][1]
	leafCook := oracle.cooks["leaf"][0]
	h, err := tablecook.ReadHeader(little, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := h.Directory(little)
	if err != nil {
		t.Fatal(err)
	}
	if len(dir) < 3 {
		t.Fatalf("the fixture numbers %d nodes and the battery needs three", len(dir))
	}
	entry := func(i int) int64 { return h.AttribOffset() + int64(i)*16 }
	le := binary.LittleEndian
	headSlot := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "head", 0)
	leafSlot := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "leaf", 0)

	cases := []cookOpenCase{
		{name: "valid_little", root: "Root", data: little, open: "open", check: "open"},
		{name: "valid_big", root: "Root", data: big, open: "foreign_order", check: "foreign_order"},
		{name: "valid_fixed", root: "Leaf", data: leafCook, open: "open", check: "open"},
		{name: "valid_union_list", root: "Root", data: oracle.cooks["union_list"][0], open: "open", check: "open"},
		{name: "valid_nested_union_list", root: "Root", data: oracle.cooks["nested_union_list"][0], open: "open", check: "open"},
		{name: "valid_union_text", root: "Root", data: oracle.cooks["union_text"][0], open: "open", check: "open"},
	}
	add := func(name string, data []byte, open, check string) {
		cases = append(cases, cookOpenCase{name: name, root: "Root", data: data, open: open, check: check})
	}
	add("wrong_magic", mutate(little, func(f []byte) { f[0] ^= 0xFF }), "not_a_cook", "not_a_cook")
	reversed := mutate(little, func(f []byte) {
		copy(f[:8], []byte{0x4b, 0x4f, 0x4f, 0x43, 0x4d, 0x48, 0x43, 0x53})
	})
	add("foreign_order", reversed, "foreign_order", "foreign_order")
	add("block_magic", mutate(little, func(f []byte) { le.PutUint64(f, 0x4b4c42414d484353) }), "not_a_cook", "not_a_cook")
	for _, word := range []uint64{0, 2, 3} {
		w := word
		add(fmt.Sprintf("byte_order_%d", w), mutate(little, func(f []byte) { le.PutUint64(f[16:], w) }), "not_a_cook", "not_a_cook")
	}
	add("build_zero", mutate(little, func(f []byte) { le.PutUint64(f[8:], 0) }), "wrong_build_version", "wrong_build_version")
	add("build_flipped", mutate(little, func(f []byte) { le.PutUint64(f[8:], le.Uint64(f[8:])^1) }), "wrong_build_version", "wrong_build_version")
	add("reserved0", mutate(little, func(f []byte) { le.PutUint64(f[48:], 1) }), "reserved_not_zero", "reserved_not_zero")
	add("reserved1", mutate(little, func(f []byte) { le.PutUint64(f[56:], 1) }), "reserved_not_zero", "reserved_not_zero")
	for _, align := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 12, 24, 128} {
		a := align
		add(fmt.Sprintf("alignment_%d", a), mutate(little, func(f []byte) { le.PutUint64(f[40:], a) }), "bad_alignment", "bad_alignment")
	}
	add("truncated", append([]byte(nil), little[:len(little)-1]...), "truncated", "truncated")
	add("extended", append(append([]byte(nil), little...), 0), "truncated", "truncated")
	add("data_past_file", mutate(little, func(f []byte) { le.PutUint64(f[24:], 1<<40) }), "truncated", "truncated")
	add("attrib_past_file", mutate(little, func(f []byte) { le.PutUint64(f[32:], 1<<40) }), "truncated", "truncated")
	add("attrib_not_entries", mutate(little[:len(little)-1], func(f []byte) { le.PutUint64(f[32:], uint64(h.AttribLength)-1) }), "open", "bad_layout")
	add("directory_sentinel", mutate(little, func(f []byte) { le.PutUint64(f[entry(1):], tablecook.NotMaterialized) }), "open", "bad_layout")
	add("directory_unknown_type", mutate(little, func(f []byte) { le.PutUint64(f[entry(1)+8:], 0xDEADBEEF) }), "open", "bad_layout")
	add("directory_descends", mutate(little, func(f []byte) {
		a, b := le.Uint64(f[entry(1):]), le.Uint64(f[entry(2):])
		le.PutUint64(f[entry(1):], b)
		le.PutUint64(f[entry(2):], a)
	}), "open", "bad_layout")
	empty := oracle.cooks["empty"][0]
	emptyHeader, err := tablecook.ReadHeader(empty, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	add("root_wrong_type", mutate(empty, func(f []byte) {
		le.PutUint64(f[emptyHeader.AttribOffset()+8:], ir.TableWireId(u.Tables["Leaf"].WireName()))
	}), "open", "bad_layout")
	liveSlot := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "live", 0)
	add("nonzero_padding", mutate(little, func(f []byte) { f[liveSlot+1] = 1 }), "open", "bad_layout")
	labelAt := leafSlot + cookSlot(t, u, oracle.m, "Leaf", "label", 0)
	labelCount := leafSlot + cookSlot(t, u, oracle.m, "Leaf", "label", 1)
	add("text_negative_count", mutate(little, func(f []byte) { le.PutUint32(f[labelCount:], ^uint32(0)) }), "open", "bad_layout")
	add("text_overlong_count", mutate(little, func(f []byte) { le.PutUint32(f[labelCount:], 9) }), "open", "bad_layout")
	add("text_nonzero_tail", mutate(little, func(f []byte) { f[labelAt+6] = 1 }), "open", "bad_layout")
	rawAt := leafSlot + cookSlot(t, u, oracle.m, "Leaf", "raw", 0)
	rawCount := leafSlot + cookSlot(t, u, oracle.m, "Leaf", "raw", 1)
	add("bytes_negative_count", mutate(little, func(f []byte) { le.PutUint32(f[rawCount:], ^uint32(0)) }), "open", "bad_layout")
	add("bytes_overlong_count", mutate(little, func(f []byte) { le.PutUint32(f[rawCount:], 9) }), "open", "bad_layout")
	add("bytes_nonzero_tail", mutate(little, func(f []byte) { f[rawAt+3] = 1 }), "open", "bad_layout")
	arrayAt := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "counted", 0)
	arrayCount := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "counted", 1)
	stride := ir.RecordLayout(u, u.Tables["Leaf"]).Size
	add("array_negative_count", mutate(empty, func(f []byte) { le.PutUint32(f[arrayCount:], ^uint32(0)) }), "open", "bad_layout")
	add("array_overlong_count", mutate(little, func(f []byte) { le.PutUint32(f[arrayCount:], 3) }), "open", "bad_layout")
	add("array_dead_slot", mutate(little, func(f []byte) { f[arrayAt+stride] = 1 }), "open", "bad_layout")
	optionalAt := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "spare", 0)
	optionalPresent := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "spare", 1)
	add("optional_bad_presence", mutate(little, func(f []byte) { f[optionalPresent] = 2 }), "open", "bad_layout")
	add("optional_dead_payload", mutate(empty, func(f []byte) { f[optionalAt] = 1 }), "open", "bad_layout")
	blobAt := int64(-1)
	textBlobAt := int64(-1)
	for _, d := range dir {
		if d.TypeId == ir.BytesTypeId {
			blobAt = h.DataOffset() + d.Offset
		}
		if d.TypeId == ir.StringTypeId {
			textBlobAt = h.DataOffset() + d.Offset
		}
	}
	if blobAt < 0 || textBlobAt < 0 {
		t.Fatal("blob fixture lacks both node kinds")
	}
	blobSlot := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "blob", 0)
	add("blob_overlong", mutate(little, func(f []byte) { le.PutUint32(f[blobAt:], ^uint32(0)) }), "open", "bad_layout")
	add("blob_padding", mutate(little, func(f []byte) { f[blobAt+4] = 1 }), "open", "bad_layout")
	add("blob_wrong_type", mutate(little, func(f []byte) { le.PutUint64(f[blobSlot:], uint64(textBlobAt-blobSlot)) }), "open", "bad_layout")
	add("blob_unterminated", mutate(little, func(f []byte) { f[textBlobAt+8+5] = 1 }), "open", "bad_layout")
	unionAt := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "pick", 0)
	_, _, _, armOffset := ir.UnionLayout(u, u.TableUnions["Pick"])
	add("union_bad_tag", mutate(empty, func(f []byte) { f[unionAt] = 7 }), "open", "bad_layout")
	add("union_dead_storage", mutate(little, func(f []byte) { f[unionAt+armOffset+4] = 1 }), "open", "bad_layout")
	listAt := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "scores", 0)
	flagsAt := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "flags", 0)
	add("list_negative_count", mutate(empty, func(f []byte) { le.PutUint32(f[listAt+8:], ^uint32(0)) }), "open", "bad_layout")
	add("list_past_extent", mutate(little, func(f []byte) { le.PutUint64(f[listAt:], uint64(h.DataLength)+8) }), "open", "bad_layout")
	add("list_into_record", mutate(little, func(f []byte) { le.PutUint64(f[listAt:], uint64(liveSlot-listAt)) }), "open", "bad_layout")
	add("list_overlap", mutate(little, func(f []byte) {
		flagsTarget := flagsAt + int64(le.Uint64(f[flagsAt:]))
		le.PutUint64(f[listAt:], uint64(flagsTarget-listAt))
		le.PutUint32(f[listAt+8:], 1)
	}), "open", "bad_layout")
	mapAt := h.DataOffset() + cookSlot(t, u, oracle.m, "Root", "values", 0)
	mapStart := mapAt + int64(le.Uint64(little[mapAt:]))
	entryType := ir.RecordLayout(u, u.Tables["Root"]).FieldByName("values").Field.MapEntry
	entryLayout := ir.RecordLayout(u, entryType)
	keyOffset := entryLayout.FieldByName("key").Offset
	add("map_duplicate_key", mutate(little, func(f []byte) {
		copy(f[mapStart+entryLayout.Size+keyOffset:mapStart+entryLayout.Size+keyOffset+4], f[mapStart+keyOffset:mapStart+keyOffset+4])
	}), "open", "bad_layout")
	add("map_descending_key", mutate(little, func(f []byte) { le.PutUint32(f[mapStart+entryLayout.Size+keyOffset:], ^uint32(9)) }), "open", "bad_layout")
	add("root_off_base", mutate(little, func(f []byte) { le.PutUint64(f[entry(0):], 8) }), "open", "bad_layout")
	add("node_overlaps", mutate(little, func(f []byte) { le.PutUint64(f[entry(2):], le.Uint64(f[entry(1):])+1) }), "open", "bad_layout")
	add("node_leaves_region", mutate(little, func(f []byte) {
		le.PutUint64(f[entry(len(dir)-1):], uint64(h.DataLength)-1)
	}), "open", "bad_layout")
	add("directory_misaligned", mutate(little, func(f []byte) { le.PutUint64(f[entry(1):], le.Uint64(f[entry(1):])+1) }), "open", "bad_layout")
	nodeStart := h.DataOffset() + dir[1].Offset
	nextSlot := nodeStart + cookSlot(t, u, oracle.m, "Node", "next", 0)
	add("reference_cycle_not_followed", mutate(little, func(f []byte) { le.PutUint64(f[nextSlot:], uint64(nodeStart-nextSlot)) }), "open", "open")
	add("reference_unnamed", mutate(little, func(f []byte) { le.PutUint64(f[headSlot:], le.Uint64(f[headSlot:])+1) }), "open", "bad_layout")
	add("reference_forward", mutate(little, func(f []byte) { le.PutUint64(f[headSlot:], uint64(h.DataLength)+8) }), "open", "bad_layout")
	add("reference_backward", mutate(little, func(f []byte) { le.PutUint64(f[headSlot:], uint64(-headSlot)) }), "open", "bad_layout")
	add("reference_other_type", mutate(little, func(f []byte) {
		le.PutUint64(f[headSlot:], uint64(leafSlot-headSlot))
	}), "open", "bad_layout")

	cases = append(cases,
		cookOpenCase{name: "base_unaligned", root: "Root", data: little, base: 1, open: "unaligned_base", check: "unaligned_base"},
		cookOpenCase{name: "base_aligned", root: "Root", data: little, base: int(h.Align), open: "open", check: "open"},
	)
	return cases, oracle
}

func mutate(data []byte, f func([]byte)) []byte {
	out := append([]byte(nil), data...)
	f(out)
	return out
}

// cookSlot is where one field's storage piece sits in its record, from the
// compiler's own layout model.
func cookSlot(t *testing.T, u *ir.Unit, m *tabletext.Model, record, field string, piece int) int64 {
	t.Helper()
	ml := ir.RecordLayout(u, m.Lookup(record))
	fl := ml.FieldByName(field)
	if fl == nil {
		t.Fatalf("%s has no field %s", record, field)
	}
	pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
	if piece >= len(pieces) {
		t.Fatalf("%s.%s has %d storage pieces, not %d", record, field, len(pieces), piece+1)
	}
	return pieces[piece].Offset
}

func testPythonCookNegativeControls(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, cookSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		file   string
		anchor string
		repl   string
	}{
		{
			name:   "order",
			file:   "cookcorpus/_schema/cookwrite.py",
			anchor: "word: Literal[\"little\", \"big\"] = \"big\" if order == 2 else \"little\"",
			repl:   "word: Literal[\"little\", \"big\"] = \"little\"",
		},
		{
			name:   "wide_order",
			file:   "cookcorpus/_schema/cookwrite.py",
			anchor: "buf[at : at + 16] = int(value).to_bytes(16, order, signed=signed)",
			repl:   "buf[at : at + 16] = int(value).to_bytes(16, \"little\", signed=signed)",
		},
		{
			name:   "reference_delta",
			file:   "cookcorpus/_schema/cookwrite.py",
			anchor: "HEADER_BYTES + entry.offset - at, 8",
			repl:   "entry.offset - at, 8",
		},
		{
			name:   "attribution_length",
			file:   "cookcorpus/_schema/cookwrite.py",
			anchor: "_put_uint(buf, 32, len(entries) * DIRECTORY_ENTRY, 8, word)",
			repl:   "_put_uint(buf, 32, len(entries) * 8, 8, word)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sabotaged := cloneFiles(files)
			text := string(sabotaged[tc.file])
			if !strings.Contains(text, tc.anchor) {
				t.Fatalf("the negative control's anchor %q is absent from %s", tc.anchor, tc.file)
			}
			sabotaged[tc.file] = []byte(strings.Replace(text, tc.anchor, tc.repl, 1))
			dir := t.TempDir()
			writeTree(t, dir, sabotaged)
			out := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + cookWriteProbe)}))
			if err := verifyCookWrite(u, out); err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s left both little and big cooks identical to tablecook", tc.name)
			}
		})
	}
	for _, tc := range []struct{ name, anchor, replacement string }{
		{"record-padding", "if any(region[base + start : base + end]):", "if False:"},
		{"wrong-root-type", "if i == 0 and (offset != 0 or type_id != h.root.descriptor.type_id):", "if i == 0 and offset != 0:"},
		{"text-count", "if used < 0 or used > fd.size:", "if False:"},
		{"text-tail", "if any(region[at + used : storage_end]) or any(region[storage_end:count_at]):", "if False:"},
		{"array-count", "if count < 0 or count > fd.bound:", "if False:"},
		{"array-dead-slot", "if any(region[at + count * stride : at + fd.bound * stride]):", "if False:"},
		{"optional-presence", "if present not in (0, 1):", "if False:"},
		{"optional-dead-payload", "if any(region[at : present_at]):", "if False:"},
		{"blob-extent", "if needed > end - offset:", "if False:"},
		{"blob-terminator", "if type_id == h.root.descriptor.string_blob_type_id and region[offset + 8 + length] != 0:", "if False:"},
		{"blob-type", "if found is None or found[1] != expected:", "if found is None:"},
		{"union-tag", "if tag > len(ud.arms):", "if tag > len(ud.arms): tag = 0\n            if False:"},
		{"union-dead-storage", "if any(region[at + ud.tag_size : arm_at]) or any(region[arm_at + arm_size : at + ud.size]):", "if False:"},
		{"list-count", "if count < 0 or (delta == 0) != (count == 0):", "if count < 0: count = 0\n            if (delta == 0) != (count == 0):"},
		{"map-key-order", "if position and previous >= value:", "if False:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cases, _ := cookOpenCases(t, u, files)
			sabotaged := cloneFiles(files)
			const path = "cookcorpus/_schema/cook.py"
			text := string(sabotaged[path])
			if !strings.Contains(text, tc.anchor) {
				t.Fatalf("control anchor absent: %s", tc.anchor)
			}
			if tc.name == "array-count" {
				text = strings.ReplaceAll(text, tc.anchor, tc.replacement)
			} else {
				text = strings.Replace(text, tc.anchor, tc.replacement, 1)
			}
			sabotaged[path] = []byte(text)
			dir := t.TempDir()
			writeTree(t, dir, sabotaged)
			inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + cookOpenProbe)}
			var manifest strings.Builder
			for _, c := range cases {
				filename := "case_" + c.name
				inputs[filename] = c.data
				fmt.Fprintf(&manifest, "%s %s %s %d\n", c.name, c.root, filename, c.base)
			}
			inputs["cases"] = []byte(manifest.String())
			out := string(pyrun.Run(t, dir, "main.py", inputs))
			if err := verifyCookOpen(cases, out); err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s", tc.name)
			}
		})
	}
	t.Run("reserved", func(t *testing.T) {
		t.Parallel()
		oracle := makeCookOracle(t, u, files)
		little := oracle.cooks["full"][0]
		sabotaged := cloneFiles(files)
		const path = "cookcorpus/_schema/cook.py"
		text := string(sabotaged[path])
		const anchor = "if reserved0 != 0 or reserved1 != 0:\n        raise Refused(\"reserved_not_zero\")"
		if !strings.Contains(text, anchor) {
			t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
		}
		sabotaged[path] = []byte(strings.Replace(text, anchor, "if False:\n        raise Refused(\"reserved_not_zero\")", 1))
		reserved := mutate(little, func(f []byte) { binary.LittleEndian.PutUint64(f[48:], 1) })
		cases := []cookOpenCase{{name: "reserved0", root: "Root", data: reserved, open: "reserved_not_zero", check: "reserved_not_zero"}}
		dir := t.TempDir()
		writeTree(t, dir, sabotaged)
		inputs := map[string][]byte{
			"main.py":        []byte(pyrun.ImportPrelude + cookOpenProbe),
			"cases":          []byte("reserved0 Root case_reserved0 0\n"),
			"case_reserved0": reserved,
		}
		out := string(pyrun.Run(t, dir, "main.py", inputs))
		if err := verifyCookOpen(cases, out); err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: reserved still refused as %q", "reserved_not_zero")
		}
	})
}

func cloneFiles(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for name, data := range files {
		out[name] = append([]byte(nil), data...)
	}
	return out
}
