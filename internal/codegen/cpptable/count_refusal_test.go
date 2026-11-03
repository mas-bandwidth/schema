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
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
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
		for _, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
			root, wire := cppCountWire(t, u, shape, count, false)
			r := tabletext.Report{Unknown: 7, Widened: 13, Duplicate: 19}
			ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &r)
			var refusal *tablewire.CountRefusal
			if ok || !errors.As(err, &refusal) || r.Malformed || r.Unknown < 7 {
				t.Fatalf("%s/%d: oracle did not preserve storage refusal: %v %v %+v", shape, count, ok, err, r)
			}
			retainingReport := tabletext.Report{Unknown: 7, Widened: 13, Duplicate: 19, Retained: 23, RetainLost: 29}
			retained := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
			ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup(root)), wire, &retained, &retainingReport)
			if ok || !errors.As(err, &refusal) || retainingReport.Malformed {
				t.Fatalf("%s/%d: retaining oracle did not preserve storage refusal: %v %v %+v", shape, count, ok, err, retainingReport)
			}
			fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int old_damage = 0; old_damage <= 1; old_damage++) {
            for (int retaining = 0; retaining <= 1; retaining++) {
                TableReport report;
                report.unknown = 7; report.kind_mismatch = 11; report.widened = 13; report.clamped = 17;
                report.duplicate = 19; report.retained = 23; report.retain_lost = 29; report.malformed = (old_damage != 0);
                report.refused = true; report.reason = non_finite_float; report.non_finite_floats = 3;
                TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
                store.used = 12; store.id_used = 1; store.count = 1;
                const void * loaded = retaining
                    ? (const void *)%sLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                    : (const void *)%sLoadTolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
                CHECK(loaded == NULL, "%s/%d partial value");
                CHECK(report.refused && report.reason == count_over_extent_cap, "%s/%d named count refusal");
                CHECK(report.malformed == (old_damage != 0), "%s/%d damage history");
                CHECK(report.unknown == %d && report.kind_mismatch == 11 && report.widened == %d &&
                    report.clamped == 17 && report.duplicate == %d && report.non_finite_floats == 3 &&
                    report.retained == (retaining ? %d : 23) &&
                    report.retain_lost == (retaining ? %d : 29), "%s/%d counters");
                CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "%s/%d retained partial state");
            }
        }
        {
            TableReport strict_report;
            CHECK(%sLoad(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &strict_report) == NULL, "%s/%d strict load");
            CHECK(strict_report.refuse_reason == count_over_extent_cap && !strict_report.malformed, "%s/%d strict reason");
            %sBuilder builder;
            TableReport builder_report;
            builder_report.unknown = 7; builder_report.widened = 13; builder_report.duplicate = 19;
            CHECK(!%sLoadBuilderTolerant(builder, wire, (int64_t)sizeof(wire), &builder_report), "%s/%d builder refusal");
            CHECK(!builder_report.malformed && builder_report.refused && builder_report.reason == count_over_extent_cap &&
                builder_report.refuse_reason == count_over_extent_cap &&
                builder_report.unknown == %d && builder_report.widened == %d && builder_report.duplicate == %d, "%s/%d builder counters");
            TableReport plain_report;
            CHECK(!%sLoadBuilder(builder, wire, (int64_t)sizeof(wire), &plain_report) && !plain_report.malformed &&
                plain_report.refuse_reason == count_over_extent_cap, "%s/%d plain builder reason");
        }
    }
`, cppBytes(wire), root, root, shape, count, shape, count, shape, count, r.Unknown, r.Widened, r.Duplicate, retainingReport.Retained, retainingReport.RetainLost, shape, count, shape, count,
				root, shape, count, shape, count, root, root, shape, count, r.Unknown, r.Widened, r.Duplicate, shape, count, root, shape, count)
		}
		root, wire := cppCountWire(t, u, shape, 0, false)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &rep); !ok || err != nil || rep.Malformed {
			t.Fatalf("%s: valid twin rejected by oracle: %v %v %+v", shape, ok, err, rep)
		}
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            report.refused = true; report.reason = count_over_extent_cap; report.malformed = true;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            const void * loaded = retaining
                ? (const void *)%sLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)%sLoadTolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded != NULL, "%s prior refusal is not current control state");
            CHECK(report.refused && report.reason == count_over_extent_cap && report.malformed, "%s history preserved after good read");
        }
    }
`, cppBytes(wire), root, root, shape, shape)
	}
	root, descending := cppCountWire(t, u, "map-descending", 1<<31, false)
	var descendingReport tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), descending, &descendingReport); ok || err != nil || !descendingReport.Malformed {
		t.Fatalf("descending-map control is not damage: %v %v %+v", ok, err, descendingReport)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            const void * loaded = retaining
                ? (const void *)MapHolderLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)MapHolderLoadTolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && report.malformed && !report.refused && report.reason != count_over_extent_cap, "descending key precedes nested value refusal");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "descending map retained state");
        }
    }
`, cppBytes(descending))
	_, short := cppCountWire(t, u, "map", 1, true)
	var rep tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup("Leaf")), short, &rep); ok || err != nil || !rep.Malformed {
		t.Fatalf("body-short control is not damage: %v %v %+v", ok, err, rep)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store; store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            const void * loaded = retaining
                ? (const void *)LeafLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)LeafLoadTolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && report.malformed && !report.refused && report.reason != count_over_extent_cap, "body-short remains damage");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "body-short retained state");
        }
    }
    {
        const uint8_t forms[2] = { 2, 255 };
        const TableRefuseReason reasons[2] = { message_form_as_file, newer_form };
        for (int i = 0; i < 2; i++) {
            TableReport report;
            report.malformed = true; report.unknown = 7;
            CHECK(LeafLoadTolerant(region, (int64_t)sizeof(region), &forms[i], 1, &report) == NULL && report.refused && report.reason == reasons[i], "form refusal name");
            CHECK(report.malformed && report.unknown == 7, "form preserves history");
        }
    }
`, cppBytes(short))
	for _, shape := range []string{"widened", "framing-tail"} {
		root, wire := cppCountWire(t, u, shape, 1<<31, false)
		report := tabletext.Report{Unknown: 7, Widened: 13}
		ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &report)
		var refusal *tablewire.CountRefusal
		if ok || !errors.As(err, &refusal) || report.Malformed || report.Unknown != 8 || report.Widened != 14 {
			t.Fatalf("%s: unexpected oracle prefix: %v %v %+v", shape, ok, err, report)
		}
		retainingReport := tabletext.Report{Unknown: 7, Widened: 13, Retained: 23, RetainLost: 29}
		retained := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
		ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup(root)), wire, &retained, &retainingReport)
		if ok || !errors.As(err, &refusal) || retainingReport.Malformed || retainingReport.Unknown != 8 || retainingReport.Widened != 14 {
			t.Fatalf("%s: unexpected retaining oracle prefix: %v %v %+v", shape, ok, err, retainingReport)
		}
		lostReport := tabletext.Report{Unknown: 7, Widened: 13, Retained: 23, RetainLost: 29}
		lostRetain := tablewire.Retain{Capacity: 0, IdCapacity: 0}
		ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup(root)), wire, &lostRetain, &lostReport)
		if ok || !errors.As(err, &refusal) || lostReport.Malformed || lostReport.RetainLost != 30 {
			t.Fatalf("%s: unexpected lost retaining oracle prefix: %v %v %+v", shape, ok, err, lostReport)
		}
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        for (int mode = 0; mode < 3; mode++) {
            TableReport report;
            report.unknown = 7; report.widened = 13; report.retained = 23; report.retain_lost = 29;
            TableRetain store;
            if (mode == 1) {
                store = TableRetain(); store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.ids = retain_ids; store.id_capacity = 64;
            }
            const void * loaded = mode > 0
                ? (const void *)HolderLoadRetain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)HolderLoadTolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && report.refused && report.reason == count_over_extent_cap, "%s reason");
            CHECK(!report.malformed && report.unknown == %d && report.widened == %d, "%s prefix counters");
            CHECK(report.retained == (mode == 1 ? %d : 23) &&
                report.retain_lost == (mode == 2 ? %d : (mode == 1 ? %d : 29)), "%s retention counters");
            CHECK(store.used == 0 && store.id_used == 0 && store.count == 0, "%s partial retained state");
        }
        CHECK(HolderLoadMeasure(wire, (int64_t)sizeof(wire)) < 0, "%s measure < 0");
    }
`, cppBytes(wire), shape, report.Unknown, report.Widened, shape, retainingReport.Retained, lostReport.RetainLost, retainingReport.RetainLost, shape, shape, shape)
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("count-refusal probe: %v\n%s", err, out)
	}
}

// runCPPCountRefusalProbe compiles body into main over the unit's table
// headers and any further generated headers whose names end in one of
// include, then runs it.
func runCPPCountRefusalProbe(t *testing.T, u *ir.Unit, body string, include ...string) ([]byte, error) {
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
		if strings.HasSuffix(name, "Table.h") || slices.ContainsFunc(include, func(suffix string) bool { return strings.HasSuffix(name, suffix) }) {
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
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	return exec.CommandContext(ctx, exePath).CombinedOutput()
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
			if ok != tc.want || rep.Malformed != !tc.want {
				t.Fatalf("%s/%s: unexpected oracle result ok=%v rep=%+v", root, tc.name, ok, rep)
			}
			if rep.Unknown != 0 || rep.KindMismatch != 0 || rep.Widened != 0 || rep.Clamped != 0 || rep.Duplicate != 0 || rep.Retained != 0 || rep.RetainLost != 0 {
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
        bool ok_ft = FixedLeafFromJsonTolerant(vft, (const char *)text, (int64_t)sizeof(text), &rft);
        printf("BOM_CASE fixed/%s: strict=%%s tolerant=%%s\n", ok_fs ? "accept" : "refuse", ok_ft ? "accept" : "refuse");
        CHECK(ok_fs && !rfs.malformed && !rfs.refused && rfs.refuse_reason == TableRefuseReason::ok &&
            rfs.unknown == 0 && rfs.kind_mismatch == 0 && rfs.widened == 0 && rfs.clamped == 0 &&
            rfs.duplicate == 0 && rfs.retained == 0 && rfs.retain_lost == 0 && vfs.a == 0, "fixed/%s strict accept");
        CHECK(ok_ft && !rft.malformed && !rft.refused &&
            rft.unknown == 0 && rft.kind_mismatch == 0 && rft.widened == 0 && rft.clamped == 0 &&
            rft.duplicate == 0 && rft.retained == 0 && rft.retain_lost == 0 && vft.a == 0, "fixed/%s tolerant accept");
        int64_t nfs = FixedLeafToJson(vfs, out_fs, (int64_t)sizeof(out_fs));
        int64_t nft = FixedLeafToJson(vft, out_ft, (int64_t)sizeof(out_ft));
        CHECK(nfs == (int64_t)sizeof(fixed_canon) && memcmp(out_fs, fixed_canon, sizeof(fixed_canon)) == 0 &&
            nft == (int64_t)sizeof(fixed_canon) && memcmp(out_ft, fixed_canon, sizeof(fixed_canon)) == 0, "fixed/%s canonical json");

        GraphLeafBuilder bgs, bgt; TableReport rgs, rgt; char out_gs[128], out_gt[128];
        bgs.GetRoot()->a = 42;
        bool ok_gs = GraphLeafFromJson(bgs, (const char *)text, (int64_t)sizeof(text), &rgs);
        bgt.GetRoot()->a = 42;
        bool ok_gt = GraphLeafFromJsonTolerant(bgt, (const char *)text, (int64_t)sizeof(text), &rgt);
        printf("BOM_CASE graph/%s: strict=%%s tolerant=%%s\n", ok_gs ? "accept" : "refuse", ok_gt ? "accept" : "refuse");
        CHECK(ok_gs && !rgs.malformed && !rgs.refused && rgs.refuse_reason == TableRefuseReason::ok &&
            rgs.unknown == 0 && rgs.kind_mismatch == 0 && rgs.widened == 0 && rgs.clamped == 0 &&
            rgs.duplicate == 0 && rgs.retained == 0 && rgs.retain_lost == 0 && bgs.GetRoot()->a == 0, "graph/%s strict accept");
        CHECK(ok_gt && !rgt.malformed && !rgt.refused &&
            rgt.unknown == 0 && rgt.kind_mismatch == 0 && rgt.widened == 0 && rgt.clamped == 0 &&
            rgt.duplicate == 0 && rgt.retained == 0 && rgt.retain_lost == 0 && bgt.GetRoot()->a == 0, "graph/%s tolerant accept");
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
        bool ok_ft = FixedLeafFromJsonTolerant(vft, (const char *)text, (int64_t)sizeof(text), &rft);
        printf("BOM_CASE fixed/%s: strict=%%s tolerant=%%s\n", ok_fs ? "accept" : "refuse", ok_ft ? "accept" : "refuse");
        CHECK(!ok_fs && rfs.malformed && !rfs.refused && rfs.refuse_reason == TableRefuseReason::malformed &&
            rfs.unknown == 0 && rfs.kind_mismatch == 0 && rfs.widened == 0 && rfs.clamped == 0 &&
            rfs.duplicate == 0 && rfs.retained == 0 && rfs.retain_lost == 0 && vfs.a == 0, "fixed/%s strict refuse");
        CHECK(!ok_ft && rft.malformed && !rft.refused &&
            rft.unknown == 0 && rft.kind_mismatch == 0 && rft.widened == 0 && rft.clamped == 0 &&
            rft.duplicate == 0 && rft.retained == 0 && rft.retain_lost == 0 && vft.a == 0, "fixed/%s tolerant refuse");

        GraphLeafBuilder bgs, bgt; TableReport rgs, rgt;
        bgs.GetRoot()->a = 42;
        bool ok_gs = GraphLeafFromJson(bgs, (const char *)text, (int64_t)sizeof(text), &rgs);
        bgt.GetRoot()->a = 42;
        bool ok_gt = GraphLeafFromJsonTolerant(bgt, (const char *)text, (int64_t)sizeof(text), &rgt);
        printf("BOM_CASE graph/%s: strict=%%s tolerant=%%s\n", ok_gs ? "accept" : "refuse", ok_gt ? "accept" : "refuse");
        CHECK(!ok_gs && rgs.malformed && !rgs.refused && rgs.refuse_reason == TableRefuseReason::malformed &&
            rgs.unknown == 0 && rgs.kind_mismatch == 0 && rgs.widened == 0 && rgs.clamped == 0 &&
            rgs.duplicate == 0 && rgs.retained == 0 && rgs.retain_lost == 0 && bgs.GetRoot()->a == 0, "graph/%s strict refuse");
        CHECK(!ok_gt && rgt.malformed && !rgt.refused &&
            rgt.unknown == 0 && rgt.kind_mismatch == 0 && rgt.widened == 0 && rgt.clamped == 0 &&
            rgt.duplicate == 0 && rgt.retained == 0 && rgt.retain_lost == 0 && bgt.GetRoot()->a == 0, "graph/%s tolerant refuse");
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
