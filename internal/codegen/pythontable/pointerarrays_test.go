package pythontable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const pointerArraysSchema = `package ptrarr

fixed table Leaf
{
    id     int32
    weight uint64
}

table ScalarGuard
{
    active bool
    origin int32
    if active
    {
        bonus int32
    }
    else
    {
        fallback int32
    }
}

table Node
{
    label    int32
    tag      uint16
    leaf     *Leaf
    children []*Node
    pair     [2]*Leaf
    active   bool
    if active
    {
        guard_next *Node
        guard_kids [..3]*Node
    }
    else
    {
        alt_leaf *Leaf
    }
}

table Shelf
{
    flag   bool
    muted  bool
    items  [..4]*Leaf
    fixed3 [3]*Leaf
    if flag
    {
        extra []*Leaf
        if !muted
        {
            deep *Node
        }
    }
}

table RangedHolder
{
    window [2..4]*Leaf
    note   int32
}

table Other
{
    code int32
    link *Leaf
}

table ValueOnly
{
    count   int32
    guarded ScalarGuard
}

table Root
{
    enabled       bool
    mode          bool
    score         int64
    head          *Node
    list_nodes    []*Node
    fixed_nodes   [3]*Node
    bounded_nodes [..4]*Node
    scalar_sub    ScalarGuard
    shelf         Shelf
    if enabled
    {
        guarded_ptr   *Node
        guarded_list  []*Leaf
        guarded_fixed [2]*Leaf
        if !mode
        {
            nested_guard int32
        }
    }
    else
    {
        fallback_ptr   *Leaf
        fallback_count uint32
    }
}
`

func loadNamedUnit(t *testing.T, fileName, src string) *ir.Unit {
	t.Helper()
	base := strings.TrimSuffix(filepath.Base(fileName), ".schema")
	ast, perrs := parser.Parse(fileName, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", fileName, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: fileName, Name: fileName, Base: base,
		Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", fileName, cerrs[0])
	}
	return u
}

func appendUleb(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func buildForm1File(rootBody []byte, ids []uint64) []byte {
	var out []byte
	out = append(out, 1) // FORM = 1
	out = append(out, rootBody...)
	for _, id := range ids {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], id)
		out = append(out, b[:]...)
	}
	var count [8]byte
	binary.LittleEndian.PutUint64(count[:], uint64(len(ids)))
	out = append(out, count[:]...)
	return out
}

func encodeOracleJSON(t *testing.T, m *tabletext.Model, rootName, js string) []byte {
	t.Helper()
	st := m.Lookup(rootName)
	if st == nil {
		t.Fatalf("table %s missing from model", rootName)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(js), &rep) || rep.Refused {
		t.Fatalf("tabletext.Read(%s, %s) failed: %+v", rootName, js, rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("tablewire.Encode(%s, %s): %v", rootName, js, err)
	}
	return wire
}

type hostileCase struct {
	name   string
	table  string
	wire   []byte
	reason string
}

// buildChainWire returns the Go engine's own wire for a Root whose head is a
// chain of n Nodes, each holding the next as children[0]: deeper than either
// pinned interpreter's recursion limit once n is in the thousands. The file is
// framed by hand, decoded by the engine (which must accept it) and encoded
// again by the engine, so the bytes the probe holds Python to are the
// engine's, not this builder's.
func buildChainWire(t *testing.T, u *ir.Unit, m *tabletext.Model, n int) []byte {
	t.Helper()
	rootSt := u.Tables["Root"]
	nodeSt := u.Tables["Node"]
	field := func(st *ir.Struct, name string) *ir.Field {
		for _, f := range st.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("missing field %s.%s", st.Name, name)
		return nil
	}
	head := field(rootSt, "head")
	label := field(nodeSt, "label")
	children := field(nodeSt, "children")
	// ids: 1 head, 2 the node table, 3 Node, 4 label, 5 children.
	ids := []uint64{
		ir.TableFieldWireId(head),
		^uint64(0),
		ir.TableWireId(nodeSt.WireName()),
		ir.TableFieldWireId(label),
		ir.TableFieldWireId(children),
	}
	var records []byte
	records = appendUleb(records, uint64(n))
	for k := 0; k < n; k++ {
		var body []byte
		body = append(body, 4, byte(ir.TableWireFieldKind(label)))
		var v [4]byte
		binary.LittleEndian.PutUint32(v[:], uint32(k+1))
		body = append(body, v[:]...)
		if k+1 < n {
			var payload []byte
			payload = append(payload, 17, 1)
			payload = appendUleb(payload, uint64(k+3))
			body = append(body, 5, byte(ir.TableWireFieldKind(children)))
			body = appendUleb(body, uint64(len(payload)))
			body = append(body, payload...)
		}
		body = append(body, 0)
		records = appendUleb(records, 3)
		records = appendUleb(records, uint64(len(body)))
		records = append(records, body...)
	}
	var root []byte
	root = append(root, 1, byte(ir.TableWireFieldKind(head)), 2)
	root = append(root, 2, 12)
	root = appendUleb(root, uint64(len(records)))
	root = append(root, records...)
	root = append(root, 0)
	framed := buildForm1File(root, ids)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, framed, &rep)
	if !ok || err != nil || rep.Refused || rep.Unknown != 0 {
		t.Fatalf("Go tablewire.Decode refused the %d-node chain: ok=%v err=%v rep=%+v", n, ok, err, rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("Go tablewire.Encode of the %d-node chain: %v", n, err)
	}
	return wire
}

func buildHostileCorpus(t *testing.T, u *ir.Unit, m *tabletext.Model) []hostileCase {
	t.Helper()
	rootSt := u.Tables["Root"]
	nodeSt := u.Tables["Node"]
	otherSt := u.Tables["Other"]
	leafSt := u.Tables["Leaf"]
	shelfSt := u.Tables["Shelf"]

	fieldID := func(st *ir.Struct, name string) uint64 {
		for _, f := range st.Fields {
			if f.Name == name {
				return ir.TableFieldWireId(f)
			}
		}
		t.Fatalf("missing field %s.%s", st.Name, name)
		return 0
	}
	typeID := func(st *ir.Struct) uint64 {
		return ir.TableWireId(st.WireName())
	}

	const (
		nodeWireID         = ^uint64(0)
		buildVersionWireID = ^uint64(1)
	)

	nodeTypeID := typeID(nodeSt)
	otherTypeID := typeID(otherSt)
	leafTypeID := typeID(leafSt)
	listNodesID := fieldID(rootSt, "list_nodes")
	fixedNodesID := fieldID(rootSt, "fixed_nodes")
	boundedNodesID := fieldID(rootSt, "bounded_nodes")
	guardedPtrID := fieldID(rootSt, "guarded_ptr")
	shelfFieldID := fieldID(rootSt, "shelf")
	nodeChildrenID := fieldID(nodeSt, "children")
	nodeLabelID := fieldID(nodeSt, "label")
	shelfItemsID := fieldID(shelfSt, "items")

	// Helper to encode a kind-14 pointer array field: ref, 14, L, 17, count, indices...
	ptrArrField := func(ref uint64, indices ...uint64) []byte {
		var payload []byte
		payload = append(payload, 17)
		payload = appendUleb(payload, uint64(len(indices)))
		for _, idx := range indices {
			payload = appendUleb(payload, idx)
		}
		var out []byte
		out = appendUleb(out, ref)
		out = append(out, 14)
		out = appendUleb(out, uint64(len(payload)))
		out = append(out, payload...)
		return out
	}

	// Helper to encode NODE_WIRE_ID section: ref, 12, L, declaredCount, (typeRef, recLen, recBody)...
	type nodeRec struct {
		typeRef uint64
		body    []byte
	}
	nodeTableField := func(ref uint64, declared uint64, recs ...nodeRec) []byte {
		var nt []byte
		nt = appendUleb(nt, declared)
		for _, rec := range recs {
			nt = appendUleb(nt, rec.typeRef)
			nt = appendUleb(nt, uint64(len(rec.body)))
			nt = append(nt, rec.body...)
		}
		var out []byte
		out = appendUleb(out, ref)
		out = append(out, 12)
		out = appendUleb(out, uint64(len(nt)))
		out = append(out, nt...)
		return out
	}

	cases := []hostileCase{
		{
			name:  "pointer_to_root_in_array",
			table: "Root",
			wire: func() []byte {
				var body []byte
				body = append(body, ptrArrField(1, 1)...)
				body = append(body, 0)
				return buildForm1File(body, []uint64{listNodesID})
			}(),
		},
		{
			name:  "self_cycle_in_node_record",
			table: "Root",
			wire: func() []byte {
				// ids: 1=listNodesID, 2=nodeWireID, 3=nodeTypeID, 4=nodeChildrenID
				var n2 []byte
				n2 = append(n2, ptrArrField(4, 2)...)
				n2 = append(n2, 0)
				var root []byte
				root = append(root, ptrArrField(1, 2)...)
				root = append(root, nodeTableField(2, 1, nodeRec{3, n2})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID, nodeWireID, nodeTypeID, nodeChildrenID})
			}(),
		},
		{
			name:  "two_node_cycle_in_pointer_arrays",
			table: "Root",
			wire: func() []byte {
				var n2, n3 []byte
				n2 = append(n2, ptrArrField(4, 3)...)
				n2 = append(n2, 0)
				n3 = append(n3, ptrArrField(4, 2)...)
				n3 = append(n3, 0)
				var root []byte
				root = append(root, ptrArrField(1, 2)...)
				root = append(root, nodeTableField(2, 2, nodeRec{3, n2}, nodeRec{3, n3})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID, nodeWireID, nodeTypeID, nodeChildrenID})
			}(),
		},
		{
			name:  "cycle_inside_inactive_guarded_field",
			table: "Root",
			wire: func() []byte {
				// enabled is elided (False), guarded_ptr (ref 1, kind 17) points to node 2 which loops to node 2!
				var n2 []byte
				n2 = append(n2, ptrArrField(4, 2)...)
				n2 = append(n2, 0)
				var root []byte
				root = append(root, 1, 17, 2) // guarded_ptr -> 2
				root = append(root, nodeTableField(2, 1, nodeRec{3, n2})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{guardedPtrID, nodeWireID, nodeTypeID, nodeChildrenID})
			}(),
		},
		{
			name:  "cycle_inside_unknown_field",
			table: "Root",
			wire: func() []byte {
				const unknownFieldID = uint64(0x123456789ABCDEF0)
				var n2 []byte
				n2 = append(n2, ptrArrField(4, 2)...)
				n2 = append(n2, 0)
				var root []byte
				root = append(root, ptrArrField(1, 2)...)
				root = append(root, nodeTableField(2, 1, nodeRec{3, n2})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{unknownFieldID, nodeWireID, nodeTypeID, nodeChildrenID})
			}(),
		},
		{
			name:  "orphan_unreachable_node_record",
			table: "Root",
			wire: func() []byte {
				var root []byte
				root = append(root, ptrArrField(1, 2)...)
				// 2 node records declared, only index 2 referenced; index 3 is orphan!
				root = append(root, nodeTableField(2, 2, nodeRec{3, []byte{0}}, nodeRec{3, []byte{0}})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID, nodeWireID, nodeTypeID})
			}(),
		},
		{
			name:  "pointer_index_past_node_table",
			table: "Root",
			wire: func() []byte {
				var root []byte
				root = append(root, ptrArrField(1, 5)...)
				root = append(root, nodeTableField(2, 1, nodeRec{3, []byte{0}})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID, nodeWireID, nodeTypeID})
			}(),
		},
		{
			name:  "noncanonical_leb_element_index",
			table: "Root",
			wire: func() []byte {
				// payload: ek=17, count=1, non-canonical 0 (0x80, 0x00)
				var root []byte
				root = append(root, 1, 14, 4, 17, 1, 0x80, 0x00, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "noncanonical_leb_array_count",
			table: "Root",
			wire: func() []byte {
				// payload: ek=17, non-canonical count 0 (0x80, 0x00)
				var root []byte
				root = append(root, 1, 14, 3, 17, 0x80, 0x00, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "array_body_len_one",
			table: "Root",
			wire: func() []byte {
				var root []byte
				root = append(root, 1, 14, 1, 17, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "closed_element_kind_forbidden_at_zero_count",
			table: "Root",
			wire: func() []byte {
				// ek=14 (nested array) is outside the closed array element kind set even at count=0!
				var root []byte
				root = append(root, 1, 14, 2, 14, 0, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "element_kind_mismatch_int32_for_pointer",
			table: "Root",
			wire: func() []byte {
				// ek=4 (int32) at count=0 on list_nodes []*Node -> kind_mismatch
				var root []byte
				root = append(root, 1, 14, 2, 4, 0, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "count_over_bound_counted",
			table: "Root",
			wire: func() []byte {
				// bounded_nodes is [..4]*Node; send count=5 null pointers
				var root []byte
				root = append(root, ptrArrField(1, 0, 0, 0, 0, 0)...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{boundedNodesID})
			}(),
		},
		{
			name:  "count_over_bound_fixed",
			table: "Root",
			wire: func() []byte {
				// fixed_nodes is [3]*Node; send count=4 null pointers
				var root []byte
				root = append(root, ptrArrField(1, 0, 0, 0, 0)...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{fixedNodesID})
			}(),
		},
		{
			name:  "count_over_extent_cap",
			table: "Root",
			wire: func() []byte {
				// count = 0x80000000 (> MaxInt32)
				var payload []byte
				payload = append(payload, 17)
				payload = appendUleb(payload, 0x80000000)
				var root []byte
				root = append(root, 1, 14)
				root = appendUleb(root, uint64(len(payload)))
				root = append(root, payload...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "trailing_byte_inside_array_len",
			table: "Root",
			wire: func() []byte {
				// ek=17, count=1, idx=0, plus 1 extra trailing byte inside L=4
				var root []byte
				root = append(root, 1, 14, 4, 17, 1, 0, 0x00, 0)
				return buildForm1File(root, []uint64{listNodesID})
			}(),
		},
		{
			name:  "declared_wrong_target_type_in_pointer_array",
			table: "Root",
			wire: func() []byte {
				// list_nodes expects *Node, but node 2 has declared type Other!
				var root []byte
				root = append(root, ptrArrField(1, 2)...)
				root = append(root, nodeTableField(2, 1, nodeRec{3, []byte{0}})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID, nodeWireID, otherTypeID})
			}(),
		},
		{
			name:  "node_wire_id_inside_nested_table",
			table: "Root",
			wire: func() []byte {
				// shelf (ref 1, kind 13) contains NODE_WIRE_ID (ref 2, kind 12)
				var nested []byte
				nested = append(nested, nodeTableField(2, 0)...)
				nested = append(nested, 0)
				var root []byte
				root = append(root, 1, 13, byte(len(nested)))
				root = append(root, nested...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{shelfFieldID, nodeWireID})
			}(),
		},
		{
			name:  "build_version_wire_id_in_root",
			table: "Root",
			wire: func() []byte {
				var root []byte
				root = append(root, 1, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0)
				return buildForm1File(root, []uint64{buildVersionWireID})
			}(),
		},
		{
			name:  "duplicate_field_id_in_node_record",
			table: "Root",
			wire: func() []byte {
				var n2 []byte
				n2 = append(n2, 4, 4, 1, 0, 0, 0)
				n2 = append(n2, 4, 4, 2, 0, 0, 0)
				n2 = append(n2, 0)
				var root []byte
				root = append(root, ptrArrField(1, 2)...)
				root = append(root, nodeTableField(2, 1, nodeRec{3, n2})...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{listNodesID, nodeWireID, nodeTypeID, nodeLabelID})
			}(),
		},
		{
			name:  "nested_shelf_pointer_array_over_bound",
			table: "Root",
			wire: func() []byte {
				// shelf.items is [..4]*Leaf; send 5 elements inside nested shelf!
				var nested []byte
				nested = append(nested, ptrArrField(2, 0, 0, 0, 0, 0)...)
				nested = append(nested, 0)
				var root []byte
				root = append(root, 1, 13, byte(len(nested)))
				root = append(root, nested...)
				root = append(root, 0)
				return buildForm1File(root, []uint64{shelfFieldID, shelfItemsID, leafTypeID})
			}(),
		},
	}

	for i := range cases {
		c := &cases[i]
		st := m.Lookup(c.table)
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(st), c.wire, &rep)
		if ok || !tablewire.ReadRefusal(err) || !rep.Refused {
			t.Fatalf("hostile case %s was NOT refused by Go tablewire.Decode: ok=%v err=%v rep=%+v", c.name, ok, err, rep)
		}
		c.reason = tablewire.RefusalReason(err)
	}
	return cases
}

// The deep-chain lengths: the long one is the 20,000-node chain the save must
// carry, and the short one is half of it, so the step counts of the two show
// the cost is linear.
const (
	chainShort = 10000
	chainLong  = 20000
)

func TestPythonVtPointerArrays(t *testing.T) {
	t.Parallel()

	u := loadNamedUnit(t, "PtrArr.schema", pointerArraysSchema)
	rootSt := u.Tables["Root"]
	rangedSt := u.Tables["RangedHolder"]

	fieldByName := func(st *ir.Struct, name string) *ir.Field {
		for _, f := range st.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("missing field %s.%s", st.Name, name)
		return nil
	}

	// 1. Pure-Go emitter & descriptor split assertions.
	if got := codecOf(fieldByName(rootSt, "head")); got != "pointer" {
		t.Errorf("codecOf(Root.head) = %q, want \"pointer\"", got)
	}
	for _, fn := range []string{"list_nodes", "fixed_nodes", "bounded_nodes", "guarded_list", "guarded_fixed"} {
		f := fieldByName(rootSt, fn)
		if got := codecOf(f); got != "pointer_array" {
			t.Errorf("codecOf(Root.%s) = %q, want \"pointer_array\"", fn, got)
		}
		if got := pythonType(rootSt, f); got != "list[object]" {
			t.Errorf("pythonType(Root.%s) = %q, want \"list[object]\"", fn, got)
		}
	}
	if got := codecOf(fieldByName(rangedSt, "window")); got != "pointer_array" {
		t.Errorf("codecOf(RangedHolder.window) = %q, want \"pointer_array\"", got)
	}
	if got := defaultExpr("Root", fieldByName(rootSt, "list_nodes")); got != "[]" {
		t.Errorf("defaultExpr(Root.list_nodes) = %q, want \"[]\"", got)
	}
	if got := defaultExpr("Root", fieldByName(rootSt, "bounded_nodes")); got != "[]" {
		t.Errorf("defaultExpr(Root.bounded_nodes) = %q, want \"[]\"", got)
	}
	if got := defaultExpr("Root", fieldByName(rootSt, "fixed_nodes")); got != "[None, None, None]" {
		t.Errorf("defaultExpr(Root.fixed_nodes) = %q, want \"[None, None, None]\"", got)
	}
	if got := defaultExpr("RangedHolder", fieldByName(rangedSt, "window")); got != "[None, None]" {
		t.Errorf("defaultExpr(RangedHolder.window) = %q, want \"[None, None]\"", got)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate(pointerArraysSchema): %v", err)
	}
	modSrc := string(files["ptrarr/PtrArr.py"])
	if !strings.Contains(modSrc, "guards=") {
		t.Errorf("generated PtrArr.py emits no field guard metadata")
	}
	if _, ok := files["ptrarr/_schema/kind_pointer_array.py"]; !ok {
		t.Errorf("generated runtime lacks ptrarr/_schema/kind_pointer_array.py")
	}

	// 2. Build Go-engine canonical wire corpus + hostile corpus.
	m := tabletext.NewModel(u)
	type goodCase struct {
		name  string
		table string
		wire  []byte
	}
	oracleSpecs := []struct {
		name  string
		table string
		json  string
	}{
		{"empty_root", "Root", `{}`},
		{"single_list", "Root", `{"list_nodes": [{"label": 10, "tag": 3}, null, {"label": -20, "tag": 9}]}`},
		{"diamond_shared", "Root", `{
			"head": {"&node": 1, "label": 42, "leaf": {"&node": 10, "id": 7, "weight": 99}, "pair": [{"&node": 10}, null]},
			"list_nodes": [{"&node": 1}, null, {"&node": 2, "label": 55, "children": [{"&node": 1}], "pair": [null, {"&node": 10}]}],
			"fixed_nodes": [null, {"&node": 1}, {"&node": 2}],
			"bounded_nodes": [{"&node": 2}, {"&node": 1}],
			"shelf": {"flag": true, "muted": false, "items": [{"&node": 10}], "fixed3": [{"&node": 10}, null, {"&node": 10}], "extra": [{"&node": 10}], "deep": {"&node": 1}}
		}`},
		{"fixed_all_null_elides", "Root", `{"fixed_nodes": [null, null, null]}`},
		{"fixed_middle_only", "Root", `{"fixed_nodes": [null, {"label": 77}, null]}`},
		{"bounded_full", "Root", `{"bounded_nodes": [{"label": 1}, {"label": 2}, {"label": 3}, {"label": 4}]}`},
		{"recursive_dag", "Root", `{
			"list_nodes": [{
				"&node": 1,
				"label": 100,
				"active": true,
				"guard_next": {"&node": 3, "label": 300, "active": false, "alt_leaf": {"&node": 9, "id": 9, "weight": 900}},
				"guard_kids": [{"&node": 2, "label": 200, "children": [{"&node": 3}]}, {"&node": 3}],
				"children": [{"&node": 2}, null, {"&node": 3}]
			}]
		}`},
		{"guard_true_nested_not_mode", "Root", `{
			"enabled": true,
			"mode": false,
			"score": -123456,
			"guarded_ptr": {"&node": 1, "label": 11},
			"guarded_list": [{"&node": 2, "id": 22, "weight": 220}, null, {"&node": 2}],
			"guarded_fixed": [null, {"&node": 2}],
			"nested_guard": 777
		}`},
		{"guard_true_mode_true_suppresses_nested", "Root", `{
			"enabled": true,
			"mode": true,
			"guarded_list": [{"id": 5, "weight": 50}]
		}`},
		{"guard_false_else_branch", "Root", `{
			"enabled": false,
			"fallback_ptr": {"id": 33, "weight": 330},
			"fallback_count": 88
		}`},
		{"scalar_guard_nested_active", "Root", `{"scalar_sub": {"active": true, "origin": 4, "bonus": 19}}`},
		{"scalar_guard_nested_else", "Root", `{"scalar_sub": {"active": false, "fallback": -12}}`},
		{"ranged_default", "RangedHolder", `{"window": [null, null]}`},
		{"ranged_two", "RangedHolder", `{"window": [{"&node": 1, "id": 8, "weight": 80}, {"&node": 1}], "note": 3}`},
		{"ranged_four", "RangedHolder", `{"window": [{"id": 1}, null, {"id": 2}, null]}`},
		{"value_only_guarded", "ValueOnly", `{"count": 5, "guarded": {"active": true, "bonus": 42}}`},
		// Two equal nodes built separately are two records; one node held
		// twice is one record named twice (docs/SPEC-TABLES.md §3.1).
		{"two_equal_nodes", "Root", `{"list_nodes": [{"label": 5}, {"label": 5}]}`},
		{"one_node_twice", "Root", `{"list_nodes": [{"&node": 1, "label": 5}, {"&node": 1}]}`},
	}

	var goodCorpus []goodCase
	for _, sp := range oracleSpecs {
		goodCorpus = append(goodCorpus, goodCase{
			name:  sp.name,
			table: sp.table,
			wire:  encodeOracleJSON(t, m, sp.table, sp.json),
		})
	}
	if len(goodCorpus[0].wire) != 10 {
		t.Fatalf("empty_root oracle wire length = %d, want 10", len(goodCorpus[0].wire))
	}
	if !bytes.Equal(goodCorpus[0].wire, goodCorpus[3].wire) {
		t.Fatalf("fixed_all_null_elides did not elide to empty_root in oracle")
	}

	hostileCorpus := buildHostileCorpus(t, u, m)

	// Evolution vector: writer has Secret + extra_list, reader only has Leaf + known.
	const evoWriterSrc = `package evo

fixed table Leaf
{
    id int32
}

fixed table Secret
{
    code int32
}

table Root
{
    known       *Leaf
    secret_list []*Secret
    leaf_list   []*Leaf
}
`
	const evoReaderSrc = `package evo

fixed table Leaf
{
    id int32
}

table Root
{
    known     *Leaf
    leaf_list []*Leaf
}
`
	uEvoW := loadNamedUnit(t, "Evo.schema", evoWriterSrc)
	mEvoW := tabletext.NewModel(uEvoW)
	evoWire := encodeOracleJSON(t, mEvoW, "Root", `{
		"known": {"&node": 1, "id": 77},
		"secret_list": [{"&node": 2, "code": 999}, {"&node": 2}],
		"leaf_list": [null, {"&node": 1}]
	}`)
	// Also construct a wire where leaf_list[0] points directly to an unknown node record type (undeclared type ID)!
	uEvoR := loadNamedUnit(t, "Evo.schema", evoReaderSrc)
	evoRootSt := uEvoR.Tables["Root"]
	evoLeafSt := uEvoR.Tables["Leaf"]
	evoLeafListID := ir.TableFieldWireId(fieldByName(evoRootSt, "leaf_list"))
	evoLeafTypeID := ir.TableWireId(evoLeafSt.WireName())
	const undeclaredNodeTypeID = uint64(0xCAFEBABE12345678)
	evoUnknownTargetWire := func() []byte {
		// leaf_list = [node 2 (undeclared), node 3 (Leaf id=55)]
		var leafBody []byte
		leafBody = append(leafBody, 4, 4, 55, 0, 0, 0, 0)
		var nt []byte
		nt = append(nt, 2)           // 2 records
		nt = append(nt, 3, 1, 0)     // rec 2: typeRef=3 (undeclared), empty body [0]
		nt = append(nt, 5, 7)        // rec 3: typeRef=5 (Leaf), len=7
		nt = append(nt, leafBody...) // field ref=4 (Leaf.id), kind=4, 55, term=0
		var root []byte
		root = append(root, 1, 14, 4, 17, 2, 2, 3) // leaf_list: [2, 3]
		root = append(root, 2, 12, byte(len(nt)))
		root = append(root, nt...)
		root = append(root, 0)
		return buildForm1File(root, []uint64{
			evoLeafListID,
			^uint64(0),
			undeclaredNodeTypeID,
			ir.TableFieldWireId(fieldByName(evoLeafSt, "id")),
			evoLeafTypeID,
		})
	}()
	{
		mEvoR := tabletext.NewModel(uEvoR)
		var rep tabletext.Report
		ok, decErr := tablewire.Decode(mEvoR, mEvoR.New(mEvoR.Lookup("Root")), evoUnknownTargetWire, &rep)
		if !ok || decErr != nil || rep.Unknown == 0 {
			t.Fatalf("Go tablewire.Decode on evoUnknownTargetWire: ok=%v err=%v rep=%+v", ok, decErr, rep)
		}
	}

	// ValueOnly with empty NODE_WIRE_ID section: must succeed with report.unknown == 0!
	valueOnlyNodeWire := buildForm1File([]byte{1, 12, 1, 0, 0}, []uint64{^uint64(0)})

	// 3. Stage generated trees (main schema, evo reader, Shared.schema, G1.schema) and run Python probe under BOTH pins!
	repoRoot, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sharedBytes, err := os.ReadFile(filepath.Join(repoRoot, "tables/lists/Shared.schema"))
	if err != nil {
		t.Fatal(err)
	}
	g1Bytes := []byte(`package tblg1

fixed table Node
{
    value int32
}

table Guarded
{
    at_rest bool
    always  *Node
    if at_rest
    {
        resting *Node
    }
}
`)
	listSharedWire, err := os.ReadFile(filepath.Join(repoRoot, "testdata/wire/tables/list_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	listBeforePtrWire, err := os.ReadFile(filepath.Join(repoRoot, "testdata/wire/tables/list_before_pointer.bin"))
	if err != nil {
		t.Fatal(err)
	}
	guardFalseWire, err := os.ReadFile(filepath.Join(repoRoot, "testdata/wire/tables/guard_false_no_edge.bin"))
	if err != nil {
		t.Fatal(err)
	}
	guardTrueWire, err := os.ReadFile(filepath.Join(repoRoot, "testdata/wire/tables/guard_true_both_edges.bin"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeTree(t, dir, files)

	evoFiles, err := Generate(uEvoR)
	if err != nil {
		t.Fatalf("Generate(evoReader): %v", err)
	}
	writeTree(t, dir, evoFiles)

	sharedFiles, err := Generate(loadNamedUnit(t, "Shared.schema", string(sharedBytes)))
	if err != nil {
		t.Fatalf("Generate(Shared.schema): %v", err)
	}
	writeTree(t, dir, sharedFiles)

	g1Files, err := Generate(loadNamedUnit(t, "G1.schema", string(g1Bytes)))
	if err != nil {
		t.Fatalf("Generate(G1.schema): %v", err)
	}
	writeTree(t, dir, g1Files)

	var manifest strings.Builder
	for _, gc := range goodCorpus {
		fmt.Fprintf(&manifest, "GOOD %s %s %s\n", gc.name, gc.table, hex.EncodeToString(gc.wire))
	}
	for _, hc := range hostileCorpus {
		fmt.Fprintf(&manifest, "BAD %s %s %s %s\n", hc.name, hc.table, hc.reason, hex.EncodeToString(hc.wire))
	}
	fmt.Fprintf(&manifest, "GOLD list_shared %s\n", hex.EncodeToString(listSharedWire))
	fmt.Fprintf(&manifest, "GOLD list_before_pointer %s\n", hex.EncodeToString(listBeforePtrWire))
	fmt.Fprintf(&manifest, "GOLD guard_false_no_edge %s\n", hex.EncodeToString(guardFalseWire))
	fmt.Fprintf(&manifest, "GOLD guard_true_both_edges %s\n", hex.EncodeToString(guardTrueWire))
	fmt.Fprintf(&manifest, "EVO writer_secret %s\n", hex.EncodeToString(evoWire))
	fmt.Fprintf(&manifest, "EVO unknown_target %s\n", hex.EncodeToString(evoUnknownTargetWire))
	fmt.Fprintf(&manifest, "VALONLY empty_nodetable %s\n", hex.EncodeToString(valueOnlyNodeWire))
	// Two chains, the second twice the first, both far past the recursion
	// limit: the probe holds each to the engine's bytes and counts the steps
	// of every walk on both, so the cost is held linear without a clock.
	for _, n := range []int{chainShort, chainLong} {
		fmt.Fprintf(&manifest, "CHAIN %d %s\n", n, hex.EncodeToString(buildChainWire(t, u, m, n)))
	}

	probe := pyrun.ImportPrelude + pointerArraysProbePy
	inputs := map[string][]byte{
		"cases.txt": []byte(manifest.String()),
		"probe.py":  []byte(probe),
	}

	gotCurrent := string(pyrun.Run(t, dir, "probe.py", inputs))
	gotFloor := string(pyrun.RunFloor(t, dir, "probe.py", inputs))
	if gotCurrent != gotFloor {
		t.Fatalf("pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", gotCurrent, gotFloor)
	}
	if !strings.Contains(gotCurrent, "ALL_POINTER_ARRAY_CHECKS_OK\n") {
		t.Fatalf("probe did not finish cleanly:\n%s", gotCurrent)
	}

	for _, pkg := range []string{"ptrarr", "ptrarr/_schema", "evo", "listdemo", "tblg1"} {
		if out, err := pyrun.Analyze(t, dir, pkg); err != nil {
			t.Fatalf("mypy --strict on %s failed:\n%s", pkg, out)
		}
	}
}

const pointerArraysProbePy = `import warnings
warnings.simplefilter("error")

import ptrarr
from ptrarr import (
    InvalidValue,
    Leaf,
    LoadReport,
    Node,
    Other,
    RangedHolder,
    Refused,
    Root,
    ScalarGuard,
    Shelf,
    ValueOnly,
)
import evo
import listdemo
import tblg1

def make_report(u: int = 0, w: int = 0, r: int = 0, rl: int = 0) -> LoadReport:
    rep = LoadReport()
    rep.unknown = u
    rep.widened = w
    rep.retained = r
    rep.retain_lost = rl
    return rep

def make_leaf(lid: int = 0, weight: int = 0) -> Leaf:
    lf = Leaf()
    lf.id = lid
    lf.weight = weight
    return lf

def make_node(label: int = 0) -> Node:
    nd = Node()
    nd.label = label
    return nd

def make_gnode(val: int = 0) -> tblg1.Node:
    nd = tblg1.Node()
    nd.value = val
    return nd

TABLES = {
    "Root": Root,
    "RangedHolder": RangedHolder,
    "ValueOnly": ValueOnly,
}

good_wires: dict[str, bytes] = {}
gold_wires: dict[str, bytes] = {}
evo_wires: dict[str, bytes] = {}
valonly_wires: dict[str, bytes] = {}
chain_wires: dict[int, bytes] = {}

with open("cases.txt", "r", encoding="utf-8") as fh:
    lines = [ln.strip() for ln in fh if ln.strip()]

# 1. Defaults at birth
r0 = Root()
assert r0.list_nodes == [] and type(r0.list_nodes) is list
assert r0.bounded_nodes == [] and type(r0.bounded_nodes) is list
assert r0.fixed_nodes == [None, None, None] and len(r0.fixed_nodes) == 3
assert r0.guarded_list == []
assert r0.guarded_fixed == [None, None]
rh0 = RangedHolder()
assert rh0.window == [None, None] and len(rh0.window) == 2

# 2. Oracle GOOD & hostile BAD vectors
for ln in lines:
    parts = ln.split(" ")
    tag = parts[0]
    if tag == "GOOD":
        _, name, tbl_name, hx = parts
        wire = bytes.fromhex(hx)
        good_wires[name] = wire
        cls = TABLES[tbl_name]
        rep = make_report(99, 99, 99, 99)
        val = cls.load(wire, report=rep)
        assert rep.unknown == 0 and rep.widened == 0 and rep.retained == 0 and rep.retain_lost == 0, (name, rep)
        measured = val.measure()
        resaved = val.save()
        assert measured == len(resaved), f"{name}: measure={measured} != len(save)={len(resaved)}"
        assert resaved == wire, f"{name}: resaved {resaved.hex()} != oracle {wire.hex()}"
        val2 = cls.load(resaved)
        assert val2 == val, f"{name}: loaded objects not equal"
        assert val2.save() == wire, f"{name}: second roundtrip differed"
        if name == "diamond_shared":
            assert val.head is not None
            assert val.list_nodes[0] is val.head
            assert val.list_nodes[1] is None
            n2 = val.list_nodes[2]
            assert n2 is not None and n2 is not val.head
            assert n2.children[0] is val.head
            assert val.fixed_nodes[1] is val.head
            assert val.fixed_nodes[2] is n2
            assert val.bounded_nodes[0] is n2 and val.bounded_nodes[1] is val.head
            assert val.shelf.deep is val.head
            leaf = val.head.leaf
            assert leaf is not None
            assert val.head.pair[0] is leaf and val.head.pair[1] is None
            assert n2.pair[1] is leaf
            assert val.shelf.items[0] is leaf
            assert val.shelf.fixed3[0] is leaf and val.shelf.fixed3[2] is leaf
            assert val.shelf.extra[0] is leaf
    elif tag == "BAD":
        _, name, tbl_name, want_reason, hx = parts
        wire = bytes.fromhex(hx)
        cls = TABLES[tbl_name]
        sentinel = make_report(77, 66, 55, 44)
        try:
            cls.load(wire, report=sentinel)
        except Refused as exc:
            assert exc.reason == want_reason, f"{name}: got reason {exc.reason!r}, want {want_reason!r}"
            assert (sentinel.unknown, sentinel.widened, sentinel.retained, sentinel.retain_lost) == (77, 66, 55, 44), f"{name}: report mutated on refusal"
        else:
            raise AssertionError(f"hostile vector {name} was accepted!")
    elif tag == "GOLD":
        gold_wires[parts[1]] = bytes.fromhex(parts[2])
    elif tag == "EVO":
        evo_wires[parts[1]] = bytes.fromhex(parts[2])
    elif tag == "VALONLY":
        valonly_wires[parts[1]] = bytes.fromhex(parts[2])
    elif tag == "CHAIN":
        chain_wires[int(parts[1])] = bytes.fromhex(parts[2])

# 3. Committed repository golden vectors (Shared.schema & G1.schema)
for gname in ("list_shared", "list_before_pointer"):
    gw = gold_wires[gname]
    rep = LoadReport()
    album = listdemo.Album.load(gw, report=rep)
    assert rep.unknown == 0
    assert album.measure() == len(gw)
    assert album.save() == gw, f"{gname} wire mismatch"
    if gname == "list_shared":
        assert len(album.photos) == 3
        assert album.photos[0] is not None and album.photos[0] is album.photos[1]
        assert album.photos[2] is None
        assert album.photos[0].width == 640 and album.photos[0].height == 480
    if gname == "list_before_pointer":
        assert len(album.photos) == 2
        assert album.photos[1] is not None and album.photos[1] is album.cover
        assert album.photos[0] is not None and album.photos[0].width == 2 and album.photos[1].width == 1

for gname in ("guard_false_no_edge", "guard_true_both_edges"):
    gw = gold_wires[gname]
    rep = LoadReport()
    gval = tblg1.Guarded.load(gw, report=rep)
    assert rep.unknown == 0
    assert gval.measure() == len(gw)
    assert gval.save() == gw, f"{gname} wire mismatch"

# Construct Guarded in Python with BOTH pointers populated while at_rest=False:
# resting MUST NOT be numbered or written!
g_manual = tblg1.Guarded()
g_manual.at_rest = False
g_manual.always = make_gnode(1)
g_manual.resting = make_gnode(2)
assert g_manual.save() == gold_wires["guard_false_no_edge"]
g_manual.at_rest = True
assert g_manual.save() == gold_wires["guard_true_both_edges"]

# 4. Inactive guarded fields neither validate, number, nor block elision!
r_inactive = Root()
r_inactive.enabled = False
cyclic_node = make_node(999)
cyclic_node.children = [cyclic_node]
r_inactive.guarded_ptr = cyclic_node
r_inactive.guarded_list = [make_leaf(123)]
r_inactive.guarded_fixed = [make_leaf(456), make_leaf(789)]
r_inactive.nested_guard = 1 << 40  # out-of-range int32 under inactive guard!
r_inactive.scalar_sub.active = False
r_inactive.scalar_sub.bonus = 999999  # inactive branch in nested ScalarGuard; active fallback==0 -> elides!
assert r_inactive.save() == good_wires["empty_root"]
assert r_inactive.measure() == 10

# 5. Evolution: unknown node records & ValueOnly NODE_WIRE_ID
rep_evo = LoadReport()
evo_val = evo.Root.load(evo_wires["writer_secret"], report=rep_evo)
assert rep_evo.unknown >= 2  # unknown field secret_list + unknown Secret node record
assert evo_val.known is not None and evo_val.known.id == 77
assert len(evo_val.leaf_list) == 2 and evo_val.leaf_list[0] is None and evo_val.leaf_list[1] is evo_val.known

rep_unk = LoadReport()
evo_unk = evo.Root.load(evo_wires["unknown_target"], report=rep_unk)
assert rep_unk.unknown == 1
assert len(evo_unk.leaf_list) == 2
assert evo_unk.leaf_list[0] is None
assert evo_unk.leaf_list[1] is not None and evo_unk.leaf_list[1].id == 55

rep_vo = make_report(12)
vo = ptrarr.ValueOnly.load(valonly_wires["empty_nodetable"], report=rep_vo)
assert rep_vo.unknown == 0 and vo.count == 0

# Fixed pointer-array growth: wire has [2]*Node, reader declares [3]*Node -> 3rd slot is None!
# Build wire from bounded/2-element encoding targeting fixed_nodes:
fixed_id = [f.field_id for f in Root.descriptor.fields if f.name == "fixed_nodes"][0]
grow_wire = (
    bytes([1, 1, 14, 4, 17, 2, 0, 0, 0])
    + fixed_id.to_bytes(8, "little")
    + (1).to_bytes(8, "little")
)
grown = ptrarr.Root.load(grow_wire)
assert grown.fixed_nodes == [None, None, None] and len(grown.fixed_nodes) == 3

# 6. Write-side InvalidValue refusals on BOTH save and measure, each by the
# path from the root and the rule it broke, with no byte written.
def must_refuse_write(obj, label: str, path: str, rule: str) -> None:
    for fn_name in ("save", "measure"):
        try:
            getattr(obj, fn_name)()
        except InvalidValue as exc:
            assert (exc.path, exc.rule) == (path, rule), f"{label} ({fn_name}): refused at {exc.path!r} for {exc.rule!r}, want {path!r} for {rule!r}"
        else:
            raise AssertionError(f"{label} ({fn_name}) did not raise InvalidValue")

bad = Root()
bad.list_nodes = None  # type: ignore[assignment]
must_refuse_write(bad, "list_nodes=None", "list_nodes", "not a list")

bad = Root()
bad.list_nodes = (Node(),)  # type: ignore[assignment]
must_refuse_write(bad, "list_nodes=tuple", "list_nodes", "not a list")

bad = Root()
bad.fixed_nodes = [None, None]
must_refuse_write(bad, "fixed_nodes_short", "fixed_nodes", "length 2 is not 3")

bad = Root()
bad.fixed_nodes = [None, None, None, None]
must_refuse_write(bad, "fixed_nodes_long", "fixed_nodes", "length 4 is not 3")

bad = Root()
bad.bounded_nodes = [None, None, None, None, None]
must_refuse_write(bad, "bounded_nodes_over", "bounded_nodes", "length 5 is outside [0, 4]")

bad_rh = RangedHolder()
bad_rh.window = [None]
must_refuse_write(bad_rh, "ranged_under_min", "window", "length 1 is outside [2, 4]")

bad_rh = RangedHolder()
bad_rh.window = [None, None, None, None, None]
must_refuse_write(bad_rh, "ranged_over_bound", "window", "length 5 is outside [2, 4]")

bad = Root()
bad.list_nodes = [123]  # type: ignore[list-item]
must_refuse_write(bad, "element_int", "list_nodes[0]", "not a Node or None")

bad = Root()
bad.list_nodes = [None, Other()]
must_refuse_write(bad, "element_wrong_table", "list_nodes[1]", "Other is not a Node or None")

# An element deep in the graph is refused by its path from the root.
bad = Root()
deep_bad = make_node(1)
deep_bad.children = [make_node(2), Leaf()]  # type: ignore[list-item]
bad.head = deep_bad
must_refuse_write(bad, "element_deep", "head.children[1]", "Leaf is not a Node or None")

# Self-cycle in []*Node, named by the path of the edge that closes it.
bad = Root()
self_n = make_node(1)
self_n.children = [self_n]
bad.list_nodes = [make_node(7), self_n]
must_refuse_write(bad, "self_cycle", "list_nodes[1].children[0]", "a pointer cycle has no wire form")

# 2-hop cycle across *Node and [3]*Node and []*Node
bad = Root()
na = make_node(1)
nb = make_node(2)
na.children = [nb]
nb.active = True
nb.guard_next = na
bad.fixed_nodes = [None, na, None]
must_refuse_write(bad, "two_hop_cycle", "fixed_nodes[1].children[0].guard_next", "a pointer cycle has no wire form")

# Active guard exposes cycle
bad = Root()
bad.enabled = True
bad.guarded_ptr = self_n
must_refuse_write(bad, "active_guard_cycle", "guarded_ptr.children[0]", "a pointer cycle has no wire form")

# A []*T over the int32 count cap is refused on write. The cap is lowered in
# the runtime for the check, so the test needs no list of 2**31 elements; a
# list at the lowered cap still saves.
import ptrarr._schema.kind_pointer_array as _kpa
saved_cap = _kpa._MAX_COUNT
_kpa._MAX_COUNT = 3
try:
    at_cap = Root()
    at_cap.list_nodes = [None, None, None]
    assert at_cap.measure() == len(at_cap.save())
    bad = Root()
    bad.list_nodes = [None, None, None, None]
    must_refuse_write(bad, "list_over_count", "list_nodes", "length exceeds int32 storage cap")
    bad = Root()
    bad.enabled = True
    bad.guarded_list = [None, None, None, None]
    must_refuse_write(bad, "guarded_list_over_count", "guarded_list", "length exceeds int32 storage cap")
finally:
    _kpa._MAX_COUNT = saved_cap

# The caller's objects are untouched by a refused write.
assert len(self_n.children) == 1 and self_n.children[0] is self_n

# 7. Two equal nodes built separately stay two nodes; one node referenced
# twice saves once. Both are held to the engine's bytes.
sep_wire = good_wires["two_equal_nodes"]
one_wire = good_wires["one_node_twice"]
assert sep_wire != one_wire
sep_val = Root.load(sep_wire)
assert sep_val.list_nodes[0] is not sep_val.list_nodes[1]
assert sep_val.list_nodes[0] == sep_val.list_nodes[1]
one_val = Root.load(one_wire)
assert one_val.list_nodes[0] is one_val.list_nodes[1]
built = Root()
built.list_nodes = [make_node(5), make_node(5)]
assert built.save() == sep_wire, f"two equal nodes: {built.save().hex()} != {sep_wire.hex()}"
assert built.measure() == len(sep_wire)
shared_n = make_node(5)
built = Root()
built.list_nodes = [shared_n, shared_n]
assert built.save() == one_wire, f"one node twice: {built.save().hex()} != {one_wire.hex()}"
assert built.measure() == len(one_wire)

# 8. Equality and repr hold on cyclic graphs a caller builds.
cyc_a = make_node(1)
cyc_a.children = [cyc_a]
cyc_b = make_node(1)
cyc_b.children = [cyc_b]
cyc_c = make_node(2)
cyc_c.children = [cyc_c]
assert cyc_a == cyc_b and not (cyc_a != cyc_b)
assert cyc_a != cyc_c
cyc_text = repr(cyc_a)
assert cyc_text.startswith("Node(label=1, ") and "children=[Node(...)]" in cyc_text, cyc_text
assert repr(Leaf()) == "Leaf(id=0, weight=0)", repr(Leaf())

# 9. A chain far deeper than the recursion limit loads from the engine's wire,
# saves back to the same bytes, measures, compares equal and reprs. Every walk
# is counted by the profiler, never timed: its Python frame depth must not
# grow with the chain, and its calls must double, not more, when the chain
# doubles.
import sys

def profiled(fn):
    state = [0, 0, 0]  # calls, depth, deepest
    def hook(frame, event, arg):
        if event == "call":
            state[0] += 1
            state[1] += 1
            if state[1] > state[2]:
                state[2] = state[1]
        elif event == "return":
            state[1] -= 1
    sys.setprofile(hook)
    try:
        result = fn()
    finally:
        sys.setprofile(None)
    return result, state[0], state[2]

assert sorted(chain_wires) == [10000, 20000], sorted(chain_wires)
assert sys.getrecursionlimit() < 10000
steps: dict[int, dict[str, int]] = {}
for n in sorted(chain_wires):
    wire = chain_wires[n]
    rep = make_report(9, 9, 9, 9)
    val, load_calls, load_depth = profiled(lambda: Root.load(wire, report=rep))
    assert rep.unknown == 0, (n, rep.unknown)
    node = val.head
    walked = 0
    while node is not None:
        walked += 1
        assert node.label == walked
        node = node.children[0] if node.children else None
    assert walked == n, (n, walked)
    saved, save_calls, save_depth = profiled(val.save)
    assert saved == wire, f"chain {n}: the resave differs from the engine's wire"
    measured, measure_calls, measure_depth = profiled(val.measure)
    assert measured == len(wire), (n, measured, len(wire))
    again = Root.load(saved)
    equal, eq_calls, eq_depth = profiled(lambda: again == val)
    assert equal is True, f"chain {n}: loaded twice, not equal"
    text, repr_calls, repr_depth = profiled(lambda: repr(val))
    assert text.startswith("Root(") and text.count("Node(") == n, (n, text[:80])
    steps[n] = {
        "load": load_calls, "save": save_calls, "measure": measure_calls,
        "eq": eq_calls, "repr": repr_calls,
    }
    for walk, depth in (("load", load_depth), ("save", save_depth), ("measure", measure_depth), ("eq", eq_depth), ("repr", repr_depth)):
        assert depth <= 40, f"chain {n}: {walk} reached a Python frame depth of {depth}"
    # One differing label at the far end makes the two unequal.
    tail = again.head
    while tail.children:
        tail = tail.children[0]
    tail.label = -1
    assert again != val, f"chain {n}: a changed tail still compared equal"
for walk in ("load", "save", "measure", "eq", "repr"):
    short, long = steps[10000][walk], steps[20000][walk]
    assert 1.8 * short <= long <= 2.2 * short, f"{walk}: {short} calls at 10000 nodes, {long} at 20000: not linear"

print("ALL_POINTER_ARRAY_CHECKS_OK")
`

// pointerArraysControlPrelude is the head of every control probe: helpers
// that turn any outcome other than the expected one into an AssertionError
// whose text starts with the control's own name, so a sabotaged run is red BY
// NAME and a run red for any other reason (a NameError in the probe, a
// different refusal) fails the control.
const pointerArraysControlPrelude = `import ptrarr
import listdemo
import tblg1
from ptrarr import InvalidValue, Node, Other, RangedHolder, Refused, Root

def make_node(label: int = 0) -> Node:
    nd = Node()
    nd.label = label
    return nd

def make_gnode(val: int = 0) -> tblg1.Node:
    nd = tblg1.Node()
    nd.value = val
    return nd

def expect_refused(name, cls, wire, reason, detail):
    try:
        cls.load(wire)
    except Refused as exc:
        if exc.reason != reason or str(exc) != f"{reason}: {detail}":
            raise AssertionError(f"{name}: refused as {str(exc)!r}, not {reason}: {detail}")
        return
    except Exception as exc:
        raise AssertionError(f"{name}: raised {type(exc).__name__} {exc}, not Refused {reason}")
    raise AssertionError(f"{name}: accepted, not refused as {reason}")

def expect_invalid(name, value, path, rule):
    for fn_name in ("save", "measure"):
        try:
            getattr(value, fn_name)()
        except InvalidValue as exc:
            if (exc.path, exc.rule) != (path, rule):
                raise AssertionError(f"{name}: {fn_name} refused at {exc.path!r} for {exc.rule!r}, not at {path!r} for {rule!r}")
            continue
        except Exception as exc:
            raise AssertionError(f"{name}: {fn_name} raised {type(exc).__name__} {exc}, not InvalidValue")
        raise AssertionError(f"{name}: {fn_name} accepted, not refused at {path!r} for {rule!r}")

def expect_bytes(name, got, want, what):
    if got != want:
        raise AssertionError(f"{name}: {what}: wrote {got.hex()}, not {want.hex()}")
`

// pointerArraysControl is one check's negative control: its probe, the one
// edit that plants the check out of a copy of the runtime, and the name its
// red must carry.
type pointerArraysControl struct {
	name   string // the subtest, the make target's stem and the probe's label
	path   string // the runtime file in the generated tree
	target string // the text the sabotage replaces; it must occur
	repl   string
	probe  string // the probe's own lines, after the prelude
}

// TestPythonVtPointerArraysNegativeControl gives every check this card adds
// its own control. Each runs its probe under both pinned interpreters twice:
// on the tree as generated, where it must exit 0 and print its OK line, and on
// a copy whose runtime has exactly that check planted out, where it must exit
// nonzero with "AssertionError: <name>:" in its stderr.
func TestPythonVtPointerArraysNegativeControl(t *testing.T) {
	t.Parallel()

	u := loadNamedUnit(t, "PtrArr.schema", pointerArraysSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	m := tabletext.NewModel(u)
	hostile := map[string][]byte{}
	for _, hc := range buildHostileCorpus(t, u, m) {
		hostile[hc.name] = hc.wire
	}
	repoRoot, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sharedBytes, err := os.ReadFile(filepath.Join(repoRoot, "tables/lists/Shared.schema"))
	if err != nil {
		t.Fatal(err)
	}
	g1Bytes := []byte(`package tblg1

fixed table Node
{
    value int32
}

table Guarded
{
    at_rest bool
    always  *Node
    if at_rest
    {
        resting *Node
    }
}
`)
	listSharedWire, err := os.ReadFile(filepath.Join(repoRoot, "testdata/wire/tables/list_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	guardFalseWire, err := os.ReadFile(filepath.Join(repoRoot, "testdata/wire/tables/guard_false_no_edge.bin"))
	if err != nil {
		t.Fatal(err)
	}
	sharedFiles, err := Generate(loadNamedUnit(t, "Shared.schema", string(sharedBytes)))
	if err != nil {
		t.Fatalf("Generate(Shared.schema): %v", err)
	}
	g1Files, err := Generate(loadNamedUnit(t, "G1.schema", string(g1Bytes)))
	if err != nil {
		t.Fatalf("Generate(G1.schema): %v", err)
	}
	hexOf := func(name string) string {
		w, ok := hostile[name]
		if !ok {
			t.Fatalf("hostile case %s missing", name)
		}
		return hex.EncodeToString(w)
	}

	const (
		pa   = "ptrarr/_schema/kind_pointer_array.py"
		load = "ptrarr/_schema/load.py"
	)
	controls := []pointerArraysControl{
		{
			name: "count-over-bound", path: pa,
			target: `if fd.array_mode != "list" and count > fd.bound:`, repl: `if False:`,
			probe: `bounded_id = [f.field_id for f in Root.descriptor.fields if f.name == "bounded_nodes"][0]
over_wire = bytes([1, 1, 14, 7, 17, 5, 0, 0, 0, 0, 0, 0]) + bounded_id.to_bytes(8, "little") + (1).to_bytes(8, "little")
expect_refused("count-over-bound", Root, over_wire, "count_over_bound", "bounded_nodes: count 5 exceeds bound 4")
`,
		},
		{
			name: "read-cycle", path: load,
			target: `if mark[target] == 1:`, repl: "if mark[target] == 1:\n            continue\n        if False:",
			probe: fmt.Sprintf(`expect_refused("read-cycle", Root, bytes.fromhex(%q), "wire_damaged", "a pointer to a node whose descent is still open: a cycle")
expect_refused("read-cycle", Root, bytes.fromhex(%q), "wire_damaged", "a pointer to a node whose descent is still open: a cycle")
`, hexOf("self_cycle_in_node_record"), hexOf("two_node_cycle_in_pointer_arrays")),
		},
		{
			name: "wrong-type-target", path: load,
			target: `if descriptor.by_id(type_id) is not None:`, repl: `if False:`,
			probe: fmt.Sprintf(`expect_refused("wrong-type-target", Root, bytes.fromhex(%q), "wire_damaged", "a pointer whose target is not the type its slot requires")
`, hexOf("declared_wrong_target_type_in_pointer_array")),
		},
		{
			name: "index-past-node-table", path: load,
			target: `if index - 1 >= n:`, repl: "if index - 1 >= n:\n                continue\n            if False:",
			probe: fmt.Sprintf(`expect_refused("index-past-node-table", Root, bytes.fromhex(%q), "wire_damaged", "a pointer index past the node table")
`, hexOf("pointer_index_past_node_table")),
		},
		{
			name: "element-kind", path: pa,
			target: `if ek != _ELEM_KIND_POINTER:`, repl: `if False:`,
			probe: fmt.Sprintf(`expect_refused("element-kind", Root, bytes.fromhex(%q), "kind_mismatch", "list_nodes: array element kind 4 is not 17")
`, hexOf("element_kind_mismatch_int32_for_pointer")),
		},
		{
			name: "shared-identity", path: "listdemo/_schema/save.py",
			target: `if key in indices:`, repl: `if False:`,
			probe: fmt.Sprintf(`ls_wire = bytes.fromhex(%q)
album = listdemo.Album.load(ls_wire)
if album.photos[0] is not album.photos[1]:
    raise AssertionError("shared-identity: the load did not share the node")
expect_bytes("shared-identity", album.save(), ls_wire, "a shared node")
`, hex.EncodeToString(listSharedWire)),
		},
		{
			name: "field-guard", path: "tblg1/_schema/save.py",
			target: `if not field.guard_holds(value):`, repl: `if False:`,
			probe: fmt.Sprintf(`g = tblg1.Guarded()
g.at_rest = False
g.always = make_gnode(1)
g.resting = make_gnode(2)
expect_bytes("field-guard", g.save(), bytes.fromhex(%q), "an inactive guarded pointer")
`, hex.EncodeToString(guardFalseWire)),
		},
		{
			name: "write-not-list", path: pa,
			target: `if type(value) is not list:`, repl: `if False:`,
			probe: `bad = Root()
bad.list_nodes = (make_node(1),)
expect_invalid("write-not-list", bad, "list_nodes", "not a list")
`,
		},
		{
			name: "write-fixed-length", path: pa,
			target: `if n != fd.bound:`, repl: `if False:`,
			probe: `bad = Root()
bad.fixed_nodes = [None, None]
expect_invalid("write-fixed-length", bad, "fixed_nodes", "length 2 is not 3")
`,
		},
		{
			name: "write-counted-range", path: pa,
			target: `if n < fd.min_count or n > fd.bound:`, repl: `if False:`,
			probe: `bad = RangedHolder()
bad.window = [None]
expect_invalid("write-counted-range", bad, "window", "length 1 is outside [2, 4]")
bad = Root()
bad.bounded_nodes = [None, None, None, None, None]
expect_invalid("write-counted-range", bad, "bounded_nodes", "length 5 is outside [0, 4]")
`,
		},
		{
			name: "write-over-count", path: pa,
			target: `elif n > _MAX_COUNT:`, repl: `elif False:`,
			probe: `import ptrarr._schema.kind_pointer_array as kpa
kpa._MAX_COUNT = 3
bad = Root()
bad.list_nodes = [None, None, None, None]
expect_invalid("write-over-count", bad, "list_nodes", "length exceeds int32 storage cap")
`,
		},
		{
			name: "write-non-node", path: pa,
			target: `raise InvalidValue(where, f"not a {fd.target} or None")`, repl: `pass`,
			probe: `bad = Root()
bad.list_nodes = [make_node(1), 123]
expect_invalid("write-non-node", bad, "list_nodes[1]", "not a Node or None")
`,
		},
		{
			name: "write-wrong-type-node", path: pa,
			target: `if isinstance(other, TableDesc):`, repl: `if False:`,
			probe: `bad = Root()
bad.list_nodes = [Other()]
expect_invalid("write-wrong-type-node", bad, "list_nodes[0]", "Other is not a Node or None")
`,
		},
		{
			name: "write-cycle", path: "ptrarr/_schema/save.py",
			target: `if key in onstack:`, repl: `if False:`,
			probe: `self_n = make_node(1)
self_n.children = [self_n]
bad = Root()
bad.list_nodes = [make_node(2), self_n]
expect_invalid("write-cycle", bad, "list_nodes[1].children[0]", "a pointer cycle has no wire form")
`,
		},
	}

	pins := []struct {
		name   string
		python string
	}{
		{"current", pyrun.Interpreter(t)},
		{"floor", pyrun.Floor(t)},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			probe := pyrun.ImportPrelude + pointerArraysControlPrelude + c.probe + fmt.Sprintf("print(%q)\n", "CONTROL_OK "+c.name)
			inputs := map[string][]byte{"control.py": []byte(probe)}
			want := "AssertionError: " + c.name + ":"
			for _, pin := range pins {
				clean := t.TempDir()
				writeTree(t, clean, files)
				writeTree(t, clean, sharedFiles)
				writeTree(t, clean, g1Files)
				stdout, stderr, runErr := pyrun.Try(t, pin.python, clean, "control.py", inputs)
				if runErr != nil || !strings.Contains(stdout, "CONTROL_OK "+c.name+"\n") {
					t.Fatalf("%s: the probe is red with no fault planted (%v):\nstdout:\n%s\nstderr:\n%s", pin.name, runErr, stdout, stderr)
				}

				dir := t.TempDir()
				writeTree(t, dir, files)
				writeTree(t, dir, sharedFiles)
				writeTree(t, dir, g1Files)
				fullPath := filepath.Join(dir, filepath.FromSlash(c.path))
				raw, err := os.ReadFile(fullPath)
				if err != nil {
					t.Fatalf("read %s: %v", c.path, err)
				}
				if !strings.Contains(string(raw), c.target) {
					t.Fatalf("sabotage target %q not found in %s", c.target, c.path)
				}
				mutated := strings.ReplaceAll(string(raw), c.target, c.repl)
				if err := os.WriteFile(fullPath, []byte(mutated), 0o644); err != nil {
					t.Fatal(err)
				}
				stdout, stderr, runErr = pyrun.Try(t, pin.python, dir, "control.py", inputs)
				if runErr == nil {
					t.Fatalf("%s: sabotage %s survived; the probe exited 0:\n%s", pin.name, c.name, stdout)
				}
				if !strings.Contains(stderr, want) {
					t.Fatalf("%s: sabotage %s went red, but not by name (want %q in stderr):\n%s", pin.name, c.name, want, stderr)
				}
			}
		})
	}
}
