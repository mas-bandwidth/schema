package cpptable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cppCountRefusalSchema = `package countcheck
table Leaf { entries map[int32]int32
    values []int32 }
union Choice { leaf Leaf }
union Wrapped { choice Choice }
table Holder { inner Leaf
    maybe ?Leaf
    leaves []Leaf
    choice Choice
    choices []Choice
    head *Leaf
    root_values []int32 }
table MapHolder { entries map[int32]Leaf }
table Wrapper { value Wrapped }
`

func cppCountWire(t *testing.T, u *ir.Unit, shape string, count uint64, short bool) (string, []byte) {
	t.Helper()
	var ids []uint64
	ref := func(id uint64) []byte {
		for i, v := range ids {
			if v == id {
				return binary.AppendUvarint(nil, uint64(i+1))
			}
		}
		ids = append(ids, id)
		return binary.AppendUvarint(nil, uint64(len(ids)))
	}
	length := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	field := func(id uint64, kind byte, payload []byte) []byte {
		return append(append(ref(id), kind), payload...)
	}
	named := func(name string, kind byte, payload []byte) []byte {
		return field(ir.TableWireId(name), kind, payload)
	}
	collection := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
	if count != 0 && !short {
		collection = append(collection, 1, 0)
	}
	leaf := append(named("entries", byte(ir.TableKindArray), length(collection)), 0)
	arm := func(union string, kind byte, body []byte) []byte {
		name := "leaf"
		if union == "Wrapped" {
			name = "choice"
		}
		return append(append(ref(ir.TableWireId(name)), kind), length(body)...)
	}
	root := "Holder"
	var body []byte
	switch shape {
	case "map":
		root, body = "Leaf", leaf[:len(leaf)-1]
	case "list":
		list := binary.AppendUvarint([]byte{byte(ir.TableKindI32)}, count)
		root, body = "Leaf", named("values", byte(ir.TableKindArray), length(list))
	case "widened", "framing-tail":
		list := binary.AppendUvarint([]byte{byte(ir.TableKindI16)}, count)
		list = append(list, 0, 0)
		leaf = append(named("values", byte(ir.TableKindArray), length(list)), 0)
		body = named("inner", byte(ir.TableKindTable), length(leaf))
		if shape == "framing-tail" {
			body = append(body, named("damaged_tail", byte(ir.TableKindTable), []byte{127})...)
		}
	case "nested", "optional":
		name := "inner"
		if shape == "optional" {
			name = "maybe"
		}
		body = named(name, byte(ir.TableKindTable), length(leaf))
	case "array":
		payload := append([]byte{byte(ir.TableKindTable), 1}, length(leaf)...)
		body = named("leaves", byte(ir.TableKindArray), length(payload))
	case "union":
		body = named("choice", byte(ir.TableKindUnion), arm("Choice", byte(ir.TableKindTable), leaf))
	case "union-array":
		payload := append([]byte{byte(ir.TableKindUnion), 1}, arm("Choice", byte(ir.TableKindTable), leaf)...)
		body = named("choices", byte(ir.TableKindArray), length(payload))
	case "nested-union":
		root = "Wrapper"
		body = named("value", byte(ir.TableKindUnion), arm("Wrapped", byte(ir.TableKindUnion), arm("Choice", byte(ir.TableKindTable), leaf)))
	case "map-value", "map-descending", "map-equal", "map-ascending":
		root = "MapHolder"
		entryType := u.Tables[root].Fields[0].MapEntry
		entry := func(key byte, value []byte, keyLast bool) []byte {
			k := field(ir.MapKeyWireId, byte(ir.TableKindI32), []byte{key, 0, 0, 0})
			v := field(ir.TableFieldWireId(entryType.Fields[1]), byte(ir.TableKindTable), length(value))
			if keyLast {
				return append(append(v, k...), 0)
			}
			return append(append(k, v...), 0)
		}
		payload := append([]byte{byte(ir.TableKindTable), 1}, length(entry(1, leaf, false))...)
		if shape != "map-value" {
			key := byte(3)
			if shape == "map-equal" {
				key = 5
			}
			if shape == "map-ascending" {
				key = 7
			}
			payload = append([]byte{byte(ir.TableKindTable), 2}, length(entry(5, []byte{0}, false))...)
			payload = append(payload, length(entry(key, leaf, true))...)
		}
		body = named("entries", byte(ir.TableKindArray), length(payload))
	case "node", "unreachable-node":
		rootWidened := append([]byte{byte(ir.TableKindI16), 1}, 5, 0)
		body = named("root_values", byte(ir.TableKindArray), length(rootWidened))
		if shape == "node" {
			body = append(body, named("head", byte(ir.TableKindPointer), []byte{2})...)
		}
		nodes := append([]byte{2}, ref(ir.TableWireId(u.Tables["Leaf"].WireName()))...)
		nodes = append(nodes, length(leaf)...)
		nodes = append(nodes, ref(ir.TableWireId("countcheck.UnknownNode"))...)
		nodes = append(nodes, length([]byte{0})...)
		body = append(body, field(math.MaxUint64, byte(ir.TableKindString), length(nodes))...)
	default:
		t.Fatalf("unknown shape %s", shape)
	}
	unknown := field(ir.TableWireId("unrecognized"), byte(ir.TableKindI32), []byte{17, 0, 0, 0})
	body = append(unknown, body...)
	body = append(body, 0)
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return root, binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func TestCountRefusalDiscardsTheWholeLoadCPP(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	u := loadUnitFromSource(t, cppCountRefusalSchema)
	model := tabletext.NewModel(u)

	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    alignas(64) static uint8_t region[65536];
    static uint8_t retain_bytes[4096];
    static TableRetain::Id retain_ids[64];
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	shapes := []string{"map", "list", "nested", "optional", "array", "union", "union-array", "nested-union", "map-value", "map-equal", "map-ascending", "node", "unreachable-node"}
	for _, shape := range shapes {
		var shapeRoot string
		for _, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
			root, wire := cppCountWire(t, u, shape, count, false)
			shapeRoot = root
			var r tabletext.Report
			ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &r)
			var refusal *tablewire.CountRefusal
			if ok || !errors.As(err, &refusal) || !r.Refused {
				t.Fatalf("%s/%d: oracle did not refuse oversized count: ok=%v err=%v report=%+v", shape, count, ok, err, r)
			}
			var retainingReport tabletext.Report
			retained := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
			ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup(root)), wire, &retained, &retainingReport)
			if ok || !errors.As(err, &refusal) || !retainingReport.Refused {
				t.Fatalf("%s/%d: retaining oracle did not refuse oversized count: ok=%v err=%v report=%+v", shape, count, ok, err, retainingReport)
			}
			fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            store.used = 12; store.id_used = 1; store.count = 1;
            const void * loaded = retaining
                ? (const void *)%sLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)%sLoad(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL, "%s/%d partial value");
            CHECK(report.refused || report.damaged, "%s/%d count refusal verdict");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "%s/%d retained partial state");
        }
        {
            %sBuilder builder;
            TableReport builder_report;
            CHECK(!%sLoadBuilder(builder, wire, (int64_t)sizeof(wire), &builder_report), "%s/%d builder refusal");
            CHECK(builder_report.refused || builder_report.damaged, "%s/%d builder refusal verdict");
        }
    }
`, cppBytes(wire), root, root, shape, count, shape, count, shape, count, root, root, shape, count, shape, count)
		}
		validWire, err := tablewire.EncodeVariable(model, model.New(model.Lookup(shapeRoot)))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            const void * loaded = retaining
                ? (const void *)%sLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)%sLoad(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded != NULL && !report.refused && !report.damaged, "%s valid twin loads cleanly");
        }
    }
`, cppBytes(validWire), shapeRoot, shapeRoot, shape)
	}
	root, descending := cppCountWire(t, u, "map-descending", 1<<31, false)
	var descendingReport tabletext.Report
	if ok, _ := tablewire.Decode(model, model.New(model.Lookup(root)), descending, &descendingReport); ok || !descendingReport.Refused {
		t.Fatalf("descending-map control did not refuse: %v %+v", ok, descendingReport)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            const void * loaded = retaining
                ? (const void *)MapHolderLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)MapHolderLoad(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && (report.damaged || report.refused), "descending key refused");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "descending map retained state");
        }
    }
`, cppBytes(descending))
	_, short := cppCountWire(t, u, "map", 1, true)
	var srep tabletext.Report
	if ok, _ := tablewire.Decode(model, model.New(model.Lookup("Leaf")), short, &srep); ok || !srep.Refused {
		t.Fatalf("body-short control did not refuse: %v %+v", ok, srep)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            const void * loaded = retaining
                ? (const void *)LeafLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)LeafLoad(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && (report.damaged || report.refused), "body-short refused");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "body-short retained state");
        }
    }
    if (failures != 0) return 1;
`, cppBytes(short))
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("count-refusal probe: %v\n%s", err, out)
	}
}

func runCPPCountRefusalProbe(t *testing.T, u *ir.Unit, body string) ([]byte, error) {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skip("generated C++ execution requires c++")
	}
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var headers []string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			headers = append(headers, name)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	sort.Strings(headers)
	sort.Strings(sources)
	var includes strings.Builder
	for _, h := range headers {
		fmt.Fprintf(&includes, "#include \"%s\"\n", h)
	}
	src := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <cstdint>
%s
using namespace %s;

int main(void) {
%s
    return 0;
}
`, includes.String(), u.Package, body)
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-I", dir, mainPath}
	args = append(args, sources...)
	args = append(args, "-o", exePath)
	if out, err := testproc.Command(t, cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := testproc.CommandContext(t, ctx, exePath)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	return cmd.CombinedOutput()
}

const cppBomBoundarySchema = `package countcheck
fixed table FixedLeaf { a int32 = 0 }
table GraphLeaf { a int32 = 0
    next *GraphLeaf }
`

func TestJsonLeadingBOMBoundaryCPP(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	u := loadUnitFromSource(t, cppBomBoundarySchema)
	model := tabletext.NewModel(u)
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "plain", text: "{}\n", want: true},
		{name: "single-bom", text: "\xEF\xBB\xBF{}\n", want: true},
		{name: "double-bom", text: "\xEF\xBB\xBF\xEF\xBB\xBF{}\n", want: false},
		{name: "space-then-bom", text: " \xEF\xBB\xBF{}\n", want: false},
		{name: "truncated-bom", text: "\xEF\xBB{}\n", want: false},
	}
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, tc := range cases {
		canonByRoot := map[string][]byte{}
		for _, root := range []string{"FixedLeaf", "GraphLeaf"} {
			var rep tabletext.Report
			inst := model.New(model.Lookup(root))
			ok := model.Read(inst, []byte(tc.text), &rep)
			if ok != tc.want || rep.Refused != !tc.want {
				t.Fatalf("%s/%s: unexpected oracle result ok=%v rep=%+v", root, tc.name, ok, rep)
			}
			if rep.Unknown != 0 || rep.Widened != 0 || rep.Retained != 0 || rep.RetainLost != 0 {
				t.Fatalf("%s/%s: unexpected oracle counters %+v", root, tc.name, rep)
			}
			if tc.want {
				canon, err := model.Write(inst)
				if err != nil || len(canon) == 0 {
					t.Fatalf("%s/%s: unexpected oracle canonical output err=%v %q", root, tc.name, err, canon)
				}
				canonByRoot[root] = append(canon, 0x0a)
			}
		}
		if tc.want {
			fc := canonByRoot["FixedLeaf"]
			gc := canonByRoot["GraphLeaf"]
			fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        static const uint8_t fixed_canon[] = { %s };
        static const uint8_t graph_canon[] = { %s };
        FixedLeaf vfs, vft; TableReport rfs, rft; char out_fs[128], out_ft[128];
        FixedLeafReset(vfs); vfs.a = 42;
        bool ok_fs = FixedLeafFromJson(vfs, (const char *)text, (int64_t)sizeof(text), &rfs);
        FixedLeafReset(vft); vft.a = 42;
        bool ok_ft = FixedLeafFromJson(vft, (const char *)text, (int64_t)sizeof(text), &rft);
        printf("BOM_CASE fixed/%s: strict=%%s tolerant=%%s\n", ok_fs ? "accept" : "refuse", ok_ft ? "accept" : "refuse");
        CHECK(ok_fs && !rfs.damaged && !rfs.refused && rfs.refuse_reason == TableRefuseReason::ok &&
            rfs.unknown == 0 && rfs.refused == 0 && rfs.widened == 0 && rfs.refused == 0 &&
            rfs.refused == 0 && rfs.retained == 0 && rfs.retain_lost == 0 && vfs.a == 0, "fixed/%s strict accept");
        CHECK(ok_ft && !rft.damaged && !rft.refused &&
            rft.unknown == 0 && rft.refused == 0 && rft.widened == 0 && rft.refused == 0 &&
            rft.refused == 0 && rft.retained == 0 && rft.retain_lost == 0 && vft.a == 0, "fixed/%s tolerant accept");
        int64_t nfs = FixedLeafToJson(vfs, out_fs, (int64_t)sizeof(out_fs));
        int64_t nft = FixedLeafToJson(vft, out_ft, (int64_t)sizeof(out_ft));
        CHECK(nfs == (int64_t)sizeof(fixed_canon) && memcmp(out_fs, fixed_canon, sizeof(fixed_canon)) == 0 &&
            nft == (int64_t)sizeof(fixed_canon) && memcmp(out_ft, fixed_canon, sizeof(fixed_canon)) == 0, "fixed/%s canonical json");

        GraphLeafBuilder bgs, bgt; TableReport rgs, rgt; char out_gs[128], out_gt[128];
        bgs.GetRoot()->a = 42;
        bool ok_gs = GraphLeafFromJson(bgs, (const char *)text, (int64_t)sizeof(text), &rgs);
        bgt.GetRoot()->a = 42;
        bool ok_gt = GraphLeafFromJson(bgt, (const char *)text, (int64_t)sizeof(text), &rgt);
        printf("BOM_CASE graph/%s: strict=%%s tolerant=%%s\n", ok_gs ? "accept" : "refuse", ok_gt ? "accept" : "refuse");
        CHECK(ok_gs && !rgs.damaged && !rgs.refused && rgs.refuse_reason == TableRefuseReason::ok &&
            rgs.unknown == 0 && rgs.refused == 0 && rgs.widened == 0 && rgs.refused == 0 &&
            rgs.refused == 0 && rgs.retained == 0 && rgs.retain_lost == 0 && bgs.GetRoot()->a == 0, "graph/%s strict accept");
        CHECK(ok_gt && !rgt.damaged && !rgt.refused &&
            rgt.unknown == 0 && rgt.refused == 0 && rgt.widened == 0 && rgt.refused == 0 &&
            rgt.refused == 0 && rgt.retained == 0 && rgt.retain_lost == 0 && bgt.GetRoot()->a == 0, "graph/%s tolerant accept");
        int64_t ngs = GraphLeafToJson(bgs.GetRoot(), out_gs, (int64_t)sizeof(out_gs));
        int64_t ngt = GraphLeafToJson(bgt.GetRoot(), out_gt, (int64_t)sizeof(out_gt));
        CHECK(ngs == (int64_t)sizeof(graph_canon) && memcmp(out_gs, graph_canon, sizeof(graph_canon)) == 0 &&
            ngt == (int64_t)sizeof(graph_canon) && memcmp(out_gt, graph_canon, sizeof(graph_canon)) == 0, "graph/%s canonical json");
    }
`, cppBytes([]byte(tc.text)), cppBytes(fc), cppBytes(gc), tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name)
		} else {
			fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        FixedLeaf vfs, vft; TableReport rfs, rft;
        FixedLeafReset(vfs); vfs.a = 42;
        bool ok_fs = FixedLeafFromJson(vfs, (const char *)text, (int64_t)sizeof(text), &rfs);
        FixedLeafReset(vft); vft.a = 42;
        bool ok_ft = FixedLeafFromJson(vft, (const char *)text, (int64_t)sizeof(text), &rft);
        printf("BOM_CASE fixed/%s: strict=%%s tolerant=%%s\n", ok_fs ? "accept" : "refuse", ok_ft ? "accept" : "refuse");
        CHECK(!ok_fs && rfs.damaged && !rfs.refused && rfs.refuse_reason == TableRefuseReason::malformed &&
            rfs.unknown == 0 && rfs.refused == 0 && rfs.widened == 0 && rfs.refused == 0 &&
            rfs.refused == 0 && rfs.retained == 0 && rfs.retain_lost == 0 && vfs.a == 0, "fixed/%s strict refuse");
        CHECK(!ok_ft && rft.damaged && !rft.refused &&
            rft.unknown == 0 && rft.refused == 0 && rft.widened == 0 && rft.refused == 0 &&
            rft.refused == 0 && rft.retained == 0 && rft.retain_lost == 0 && vft.a == 0, "fixed/%s tolerant refuse");

        GraphLeafBuilder bgs, bgt; TableReport rgs, rgt;
        bgs.GetRoot()->a = 42;
        bool ok_gs = GraphLeafFromJson(bgs, (const char *)text, (int64_t)sizeof(text), &rgs);
        bgt.GetRoot()->a = 42;
        bool ok_gt = GraphLeafFromJson(bgt, (const char *)text, (int64_t)sizeof(text), &rgt);
        printf("BOM_CASE graph/%s: strict=%%s tolerant=%%s\n", ok_gs ? "accept" : "refuse", ok_gt ? "accept" : "refuse");
        CHECK(!ok_gs && rgs.damaged && !rgs.refused && rgs.refuse_reason == TableRefuseReason::malformed &&
            rgs.unknown == 0 && rgs.refused == 0 && rgs.widened == 0 && rgs.refused == 0 &&
            rgs.refused == 0 && rgs.retained == 0 && rgs.retain_lost == 0 && bgs.GetRoot()->a == 0, "graph/%s strict refuse");
        CHECK(!ok_gt && rgt.damaged && !rgt.refused &&
            rgt.unknown == 0 && rgt.refused == 0 && rgt.widened == 0 && rgt.refused == 0 &&
            rgt.refused == 0 && rgt.retained == 0 && rgt.retain_lost == 0 && bgt.GetRoot()->a == 0, "graph/%s tolerant refuse");
    }
`, cppBytes([]byte(tc.text)), tc.name, tc.name, tc.name, tc.name, tc.name, tc.name)
		}
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("bom-boundary probe: %v\n%s", err, out)
	}
}
