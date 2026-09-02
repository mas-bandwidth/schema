package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// ---- the corpus of graphs, built by hand ----
//
// The pointered corpus has no TEXT form — §16.2 refuses one, because the text
// form of a variable-length table reads through its builder — so the wire
// instances a round trip needs are built here, in the shapes §3.1 and §6.3 are
// written against: aliasing, a back-reference at the root, recursion through a
// pointer edge, a node nothing points at through the root's own fields, a
// variable table nested by value, a bounded array of them, an enum-keyed array,
// an optional, and a null in every pointer-shaped slot.

type graph struct {
	name string
	root string
	make func(m *tabletext.Model) *tabletext.Instance
}

func field(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	panic("no field " + name + " on " + inst.Def.Name)
}

func setStr(inst *tabletext.Instance, name, v string) {
	f := field(inst, name)
	f.Cell.Str = []byte(v)
	f.Count = len(v)
}

func setInt(inst *tabletext.Instance, name string, v int64) {
	f := field(inst, name)
	f.Cell.I, f.Cell.U = v, uint64(v)
}

func setNode(inst *tabletext.Instance, name string, target *tabletext.Instance) {
	field(inst, name).Cell.Node = target
}

func graphs() []graph {
	return []graph{
		{"empty-scene", "Scene", func(m *tabletext.Model) *tabletext.Instance {
			return m.New(m.Lookup("Scene"))
		}},
		{"one-node", "Scene", func(m *tabletext.Model) *tabletext.Instance {
			scene := m.New(m.Lookup("Scene"))
			setStr(scene, "name", "one")
			head := m.New(m.Lookup("ListNode"))
			setInt(head, "value", 7)
			setStr(head, "name", "head")
			setNode(scene, "head", head)
			return scene
		}},
		{"chain-and-alias", "Scene", func(m *tabletext.Model) *tabletext.Instance {
			// §3.1's own worked example, in this corpus's names: a chain, a
			// shared node named three times, and a back-reference to the root
			scene := m.New(m.Lookup("Scene"))
			setStr(scene, "name", "chain")
			setInt(scene, "version", 3)
			a := m.New(m.Lookup("ListNode"))
			setInt(a, "value", 1)
			setStr(a, "name", "a")
			b := m.New(m.Lookup("ListNode"))
			setInt(b, "value", 2)
			setStr(b, "name", "b")
			setNode(a, "next", b)
			setNode(scene, "head", a)
			// alias names the SAME node the chain's head is: one index, one node
			setNode(scene, "alias", a)
			settings := m.New(m.Lookup("Settings"))
			setInt(settings, "quality", 4)
			setStr(settings, "label", "shared")
			setNode(scene, "settings", settings)
			return scene
		}},
		{"tree", "Scene", func(m *tabletext.Model) *tabletext.Instance {
			scene := m.New(m.Lookup("Scene"))
			setStr(scene, "name", "tree")
			root := m.New(m.Lookup("TreeNode"))
			setStr(root, "label", "root")
			left := m.New(m.Lookup("TreeNode"))
			setStr(left, "label", "left")
			right := m.New(m.Lookup("TreeNode"))
			setStr(right, "label", "right")
			setNode(root, "left", left)
			setNode(root, "right", right)
			setNode(scene, "tree", root)
			return scene
		}},
		{"nested-by-value-and-array", "Scene", func(m *tabletext.Model) *tabletext.Instance {
			// a VARIABLE table nested BY VALUE, and a bounded array of them: the
			// mode propagates up through by-value nesting and every walk
			// descends into it
			scene := m.New(m.Lookup("Scene"))
			setStr(scene, "name", "layers")
			shared := m.New(m.Lookup("ListNode"))
			setInt(shared, "value", 99)
			setStr(shared, "name", "shared")
			ground := field(scene, "ground").Cell.Tab
			setInt(ground, "depth", 5)
			setNode(ground, "head", shared)
			layers := field(scene, "layers")
			layers.Count = 2
			setInt(layers.Elems[0].Tab, "depth", 1)
			setNode(layers.Elems[0].Tab, "head", shared)
			setInt(layers.Elems[1].Tab, "depth", 2)
			second := m.New(m.Lookup("ListNode"))
			setInt(second, "value", 5)
			setNode(layers.Elems[1].Tab, "head", second)
			meta := field(scene, "meta").Cell.Tab
			setInt(meta, "build", 42)
			setStr(meta, "tag", "m")
			return scene
		}},
		{"keyed-and-optional", "Depot", func(m *tabletext.Model) *tabletext.Instance {
			// the two TABLE-BODY constructs in the VARIABLE class: an enum-keyed
			// array of variable tables, and an optional fixed table
			depot := m.New(m.Lookup("Depot"))
			setStr(depot, "name", "depot")
			banks := field(depot, "banks")
			node := m.New(m.Lookup("ListNode"))
			setInt(node, "value", 11)
			setStr(node, "name", "bank")
			for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(banks.Def); slot++ {
				setInt(banks.Elems[slot].Tab, "depth", int64(slot))
			}
			setNode(banks.Elems[tabletext.KeyedFirstSlot()].Tab, "head", node)
			spare := field(depot, "spare")
			spare.Present = true
			setInt(spare.Cell.Tab, "build", 9)
			setStr(spare.Cell.Tab, "tag", "sp")
			setNode(depot, "head", node)
			return depot
		}},
	}
}

// THE ROUND TRIP, and it is the gate: wire -> cook -> wire, byte-identical, in
// BOTH BYTE ORDERS. The wire is the format of record and a cook is an
// accelerator produced beside one, so a fact a cook cannot give back is a fact
// the accelerator lost.
func TestWireCookWireIsByteIdentical(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	for _, g := range graphs() {
		for _, big := range []bool{false, true} {
			name := g.name + "/little"
			if big {
				name = g.name + "/big"
			}
			t.Run(name, func(t *testing.T) {
				wire, err := tablewire.Encode(m, g.make(m))
				if err != nil {
					t.Fatal(err)
				}
				cooked, err := tablecook.Cook(m, decode(t, m, g.root, wire), tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tablecook.Check(m, cooked); err != nil {
					t.Fatalf("the cook this build just wrote does not check: %v", err)
				}
				back, err := tablecook.Uncook(m, m.Lookup(g.root), cooked)
				if err != nil {
					t.Fatal(err)
				}
				again, err := tablewire.Encode(m, back)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(wire, again) {
					t.Fatalf("the wire did not survive the cook:\n in %d bytes: % x\nout %d bytes: % x", len(wire), wire, len(again), again)
				}
			})
		}
	}
}

func decode(t *testing.T, m *tabletext.Model, root string, wire []byte) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Silent() {
		t.Fatalf("the wire this build wrote did not read back clean: %+v", r)
	}
	return inst
}

// The FIXED corpus rides too: the cook of a fixed root table is the same idea
// with nothing in it — one struct behind the header — and the round trip is the
// same round trip. The wire comes from `schema pack` over the pinned trees, so
// this leg is over bytes the pack goldens already hold.
func TestFixedRootsRoundTripFromThePackCorpus(t *testing.T) {
	u := unit(t, "../../tables/examples")
	m := tabletext.NewModel(u)
	c := compiler.New()
	c.FormatInPlace = false
	pinned := "../../tables/pack/pinned"
	entries, err := os.ReadDir(pinned)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		root := e.Name()
		if m.Lookup(root) == nil {
			continue
		}
		wire, _, report, err := c.Pack(u, root, filepath.Join(pinned, root))
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if !report.Silent() {
			t.Fatalf("%s: the pinned tree did not pack clean: %+v", root, report)
		}
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, decode(t, m, root, wire), tablecook.Options{Big: big})
			if err != nil {
				t.Fatalf("%s: %v", root, err)
			}
			if _, err := tablecook.Check(m, cooked); err != nil {
				t.Fatalf("%s: %v", root, err)
			}
			back, err := tablecook.Uncook(m, m.Lookup(root), cooked)
			if err != nil {
				t.Fatalf("%s: %v", root, err)
			}
			again, err := tablewire.Encode(m, back)
			if err != nil {
				t.Fatalf("%s: %v", root, err)
			}
			if !bytes.Equal(wire, again) {
				t.Fatalf("%s (big=%v): the wire did not survive the cook", root, big)
			}
			ran++
		}
	}
	if ran == 0 {
		t.Fatal("no pinned tree was cooked, so this gate proved nothing")
	}
}

// The BUILD VERSION stamped in the header equals `schema build-version`. There
// is no second version id: it is what the store is indexed by AND what `Open`
// checks out of the header (§7, §20).
func TestHeaderStampsTheBuildVersion(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	cooked, err := tablecook.Cook(m, m.New(m.Lookup("Scene")), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	if h.BuildVersion != ir.BuildVersion(u) {
		t.Fatalf("the header stamps 0x%016x and the unit's build version is 0x%016x", h.BuildVersion, ir.BuildVersion(u))
	}
	// and a cook of a FOREIGN build version refuses, which is the whole of the
	// runtime's check beyond the framing
	if _, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u)^1); err == nil {
		t.Fatal("a cook opened against a foreign build version")
	}
}

// A BIG-ENDIAN COOK BYTE-SWAPS EVERY SCALAR, and this proves it two ways: the
// two cooks differ in exactly the bytes a swap moves, and the big-endian file
// decodes on the Go side when it is read in the other order.
func TestBigEndianCookSwapsEveryScalar(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	build := func() *tabletext.Instance { return graphs()[2].make(m) }

	little, err := tablecook.Cook(m, build(), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	big, err := tablecook.Cook(m, build(), tablecook.Options{Big: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(little) != len(big) {
		t.Fatalf("the two orders produced %d and %d bytes: a swap moves no byte's position", len(little), len(big))
	}
	if bytes.Equal(little, big) {
		t.Fatal("the big-endian cook is byte-identical to the little-endian one, so nothing was swapped")
	}

	// the HEADER is the byte-level pattern this asserts against, because every
	// one of its words is a u64 and a u64's swap is exactly its reversal
	for at := int64(0); at < tablecook.HeaderBytes; at += 8 {
		if at == 16 {
			// the BYTE-ORDER word is the one header field whose VALUE differs
			// between the two: the magic is what refuses a foreign order, and
			// this word is what RECORDS which order wrote it, so a refusal can
			// name the order rather than infer it (§19.1's pair, in a cook)
			if binary.LittleEndian.Uint64(little[at:]) != tablecook.ByteOrderLittle ||
				binary.BigEndian.Uint64(big[at:]) != tablecook.ByteOrderBig {
				t.Fatal("the byte-order word does not record the order that wrote it")
			}
			continue
		}
		l := little[at : at+8]
		b := big[at : at+8]
		for i := 0; i < 8; i++ {
			if l[i] != b[7-i] {
				t.Fatalf("header word at %d is not the byte reversal of the other order's: % x vs % x", at, l, b)
			}
		}
	}

	// A SCALAR IN THE REGION, at the offset the layout model puts it: the graph
	// sets Scene.version to 3, so its four bytes are the two orders' reversal of
	// each other where the compiler's own C ABI model says the field sits. This
	// is the byte-level pattern, taken at a named field rather than at a byte
	// that happened to differ.
	lh0, err := tablecook.ReadHeader(little, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	bh0, err := tablecook.ReadHeader(big, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	version := ir.RecordLayout(u, m.Lookup("Scene")).FieldByName("version")
	if version == nil || version.Size != 4 {
		t.Fatal("Scene.version is not the four-byte scalar this assertion is written against")
	}
	lv := lh0.Data(little)[version.Offset : version.Offset+4]
	bv := bh0.Data(big)[version.Offset : version.Offset+4]
	if !bytes.Equal(lv, []byte{3, 0, 0, 0}) || !bytes.Equal(bv, []byte{0, 0, 0, 3}) {
		t.Fatalf("Scene.version at region offset %d is % x little and % x big", version.Offset, lv, bv)
	}

	// and the file READS in the other order: the magic read bytewise is what
	// establishes it, so a reader needs nothing else to find out
	h, err := tablecook.ReadHeader(big, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	if h.ByteOrder != tablecook.ByteOrderBig {
		t.Fatalf("the big-endian cook records byte order %d", h.ByteOrder)
	}
	if binary.BigEndian.Uint64(big) != tablecook.Magic {
		t.Fatal("the big-endian cook's magic does not read as the constant in big-endian order")
	}
	if binary.LittleEndian.Uint64(little) != tablecook.Magic {
		t.Fatal("the little-endian cook's magic does not read as the constant in little-endian order")
	}
	// the two cooks carry the same DIRECTORY, node for node, once each is read
	// in its own order — the layout is one layout and only the bytes differ
	lh, err := tablecook.ReadHeader(little, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	ld, err := lh.Directory(little)
	if err != nil {
		t.Fatal(err)
	}
	bd, err := h.Directory(big)
	if err != nil {
		t.Fatal(err)
	}
	if len(ld) != len(bd) {
		t.Fatalf("the two orders name %d and %d nodes", len(ld), len(bd))
	}
	for i := range ld {
		if ld[i] != bd[i] {
			t.Fatalf("directory entry %d differs between the orders: %+v vs %+v", i, ld[i], bd[i])
		}
	}
}

// THE ATTRIBUTION IS SEPARABLE: a cook may carry both parts or the data alone,
// and a split moves nothing but the length word.
func TestAttributionSplitsAndRejoinsExactly(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	cooked, err := tablecook.Cook(m, graphs()[2].make(m), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	data, side, err := tablecook.SplitAttribution(cooked)
	if err != nil {
		t.Fatal(err)
	}
	// the data-alone file still OPENS — the runtime never touches the directory
	if _, err := tablecook.ReadHeader(data, ir.BuildVersion(u)); err != nil {
		t.Fatalf("a cook carrying data alone did not open: %v", err)
	}
	// and it cannot be CHECKED, and the refusal says which part is missing
	if _, err := tablecook.Check(m, data); err == nil {
		t.Fatal("a cook with no attribution part was checked anyway")
	}
	rejoined, err := tablecook.JoinAttribution(data, side)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cooked, rejoined) {
		t.Fatal("a split then a join did not reproduce the writer's own bytes")
	}
}
