package ctable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cCountRefusalSchema = `package countcheck
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

func cCountWire(t *testing.T, u *ir.Unit, shape string, count uint64, short bool) (string, []byte) {
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
	length := func(body []byte) []byte { return append(binary.AppendUvarint(nil, uint64(len(body))), body...) }
	// Payloads are closures so IDs are interned in the same order the wire is
	// read: parent field, nested headers, then deferred node records.
	type part func() []byte
	field := func(id uint64, kind byte, payload part) []byte {
		out := append(ref(id), kind)
		return append(out, payload()...)
	}
	named := func(name string, kind byte, payload part) []byte { return field(ir.TableWireId(name), kind, payload) }
	leaf := func() []byte {
		return append(named("entries", byte(ir.TableKindArray), func() []byte {
			data := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
			if count != 0 && !short {
				data = append(data, 1, 0)
			}
			return length(data)
		}), 0)
	}
	arm := func(name string, kind byte, payload part) []byte {
		out := append(ref(ir.TableWireId(name)), kind)
		return append(out, length(payload())...)
	}
	root := "Holder"
	var build part
	switch shape {
	case "map":
		root = "Leaf"
		build = func() []byte { v := leaf(); return v[:len(v)-1] }
	case "list":
		root = "Leaf"
		build = func() []byte {
			return named("values", byte(ir.TableKindArray), func() []byte { return length(binary.AppendUvarint([]byte{byte(ir.TableKindI32)}, count)) })
		}
	case "widened", "framing-tail":
		build = func() []byte {
			v := named("inner", byte(ir.TableKindTable), func() []byte {
				list := named("values", byte(ir.TableKindArray), func() []byte {
					payload := binary.AppendUvarint([]byte{byte(ir.TableKindI16)}, count)
					if count != 0 {
						payload = append(payload, 0, 0)
					}
					return length(payload)
				})
				return length(append(list, 0))
			})
			if shape == "framing-tail" {
				v = append(v, named("damaged_tail", byte(ir.TableKindTable), func() []byte { return []byte{127} })...)
			}
			return v
		}
	case "nested", "optional":
		name := "inner"
		if shape == "optional" {
			name = "maybe"
		}
		build = func() []byte { return named(name, byte(ir.TableKindTable), func() []byte { return length(leaf()) }) }
	case "array":
		build = func() []byte {
			return named("leaves", byte(ir.TableKindArray), func() []byte { return length(append([]byte{byte(ir.TableKindTable), 1}, length(leaf())...)) })
		}
	case "union":
		build = func() []byte {
			return named("choice", byte(ir.TableKindUnion), func() []byte { return arm("leaf", byte(ir.TableKindTable), leaf) })
		}
	case "union-array":
		build = func() []byte {
			return named("choices", byte(ir.TableKindArray), func() []byte {
				return length(append([]byte{byte(ir.TableKindUnion), 1}, arm("leaf", byte(ir.TableKindTable), leaf)...))
			})
		}
	case "nested-union":
		root = "Wrapper"
		build = func() []byte {
			return named("value", byte(ir.TableKindUnion), func() []byte {
				return arm("choice", byte(ir.TableKindUnion), func() []byte { return arm("leaf", byte(ir.TableKindTable), leaf) })
			})
		}
	case "map-value", "map-descending", "map-equal", "map-ascending":
		root = "MapHolder"
		entryType := u.Tables[root].Fields[0].MapEntry
		entry := func(key byte, value part, keyLast bool) []byte {
			keyPart := func() []byte {
				return field(ir.MapKeyWireId, byte(ir.TableKindI32), func() []byte { return []byte{key, 0, 0, 0} })
			}
			valuePart := func() []byte {
				return field(ir.TableFieldWireId(entryType.Fields[1]), byte(ir.TableKindTable), func() []byte { return length(value()) })
			}
			var out []byte
			if keyLast {
				out = append(valuePart(), keyPart()...)
			} else {
				out = append(keyPart(), valuePart()...)
			}
			return append(out, 0)
		}
		build = func() []byte {
			return named("entries", byte(ir.TableKindArray), func() []byte {
				var data []byte
				if shape == "map-value" {
					data = append([]byte{byte(ir.TableKindTable), 1}, length(entry(1, leaf, false))...)
				} else {
					key := byte(3)
					if shape == "map-equal" {
						key = 5
					}
					if shape == "map-ascending" {
						key = 7
					}
					data = append([]byte{byte(ir.TableKindTable), 2}, length(entry(5, func() []byte { return []byte{0} }, false))...)
					data = append(data, length(entry(key, leaf, true))...)
				}
				return length(data)
			})
		}
	case "node", "unreachable-node":
		build = func() []byte {
			body := named("root_values", byte(ir.TableKindArray), func() []byte { return length([]byte{byte(ir.TableKindI16), 1, 5, 0}) })
			if shape == "node" {
				body = append(body, named("head", byte(ir.TableKindPointer), func() []byte { return []byte{2} })...)
			}
			// The node-table field header precedes its type and body references.
			body = append(body, field(math.MaxUint64, byte(ir.TableKindString), func() []byte {
				nodes := append([]byte{1}, ref(ir.TableWireId(u.Tables["Leaf"].WireName()))...)
				nodes = append(nodes, length(leaf())...)
				return length(nodes)
			})...)
			return body
		}
	default:
		t.Fatalf("unknown shape %s", shape)
	}
	body := field(ir.TableWireId("unrecognized"), byte(ir.TableKindI32), func() []byte { return []byte{17, 0, 0, 0} })
	body = append(body, build()...)
	body = append(body, 0)
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return root, binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func runCCountRefusalProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(void) {
` + body + `
    return 0;
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return string(output), fmt.Errorf("compile: %w", err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	return string(output), err
}

func TestCountRefusalDiscardsTheWholeLoadC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	u := unitFrom(t, cCountRefusalSchema)
	model := tabletext.NewModel(u)
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    static uint8_t region[65536] __attribute__((aligned(64)));
    static uint8_t retain_bytes[4096];
    static TableRetainId retain_ids[64];
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	shapes := []string{"map", "list", "widened", "nested", "optional", "array", "union", "union-array", "nested-union", "map-value", "map-equal", "map-ascending", "node", "unreachable-node"}
	for _, shape := range shapes {
		for _, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
			root, wire := cCountWire(t, u, shape, count, false)
			var r tabletext.Report
			ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &r)
			var refusal *tablewire.CountRefusal
			if ok || !errors.As(err, &refusal) || !r.Refused || tablewire.RefusalReason(err) != tablewire.ReasonCountOverExtentCap {
				t.Fatalf("%s/%d: oracle count refusal: ok=%v err=%v report=%+v", shape, count, ok, err, r)
			}
			var rr tabletext.Report
			retained := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
			ok, err = tablewire.DecodeRetain(model, model.New(model.Lookup(root)), wire, &retained, &rr)
			if ok || !errors.As(err, &refusal) || !rr.Refused || retained.Used() != 0 {
				t.Fatalf("%s/%d: retaining oracle count refusal: ok=%v err=%v report=%+v retained=%d", shape, count, ok, err, rr, retained.Used())
			}
			fn := ir.RustSnake(root)
			// An unreachable record or repeated map key is a second fault; its
			// refusal may precede the oversized count. The reachable node and
			// ascending map cases separately pin the count reason.
			reasonCheck := "report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP"
			if shape == "unreachable-node" || shape == "map-equal" {
				reasonCheck = "report.refused"
			}
			fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        int retaining;
        for (retaining = 0; retaining <= 1; retaining++) {
            TableReport report; TableRetain store; const void *loaded;
            memset(&report, 0, sizeof(report));
            memset(&store, 0, sizeof(store));
            store.bytes=retain_bytes; store.capacity=(int64_t)sizeof(retain_bytes); store.used=12;
            store.ids=retain_ids; store.id_capacity=64; store.id_used=1; store.count=1;
            memset(region, 0xa5, sizeof(region));
            loaded = retaining ? (const void *)%s_load_retain(region, sizeof(region), wire, sizeof(wire), &store, &report)
                               : (const void *)%s_load(region, sizeof(region), wire, sizeof(wire), &report);
            CHECK(loaded == NULL, "%s/%d partial region");
            CHECK(%s, "%s/%d count verdict");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "%s/%d discarded retain");
        }
        {
            %sBuilder builder; TableReport report; %s before; %s *value;
            CHECK(%s_builder_init(&builder), "%s/%d builder init");
            value = %s_builder_root(&builder); CHECK(value != NULL, "%s/%d builder root");
            before = *value;
            memset(&report, 0, sizeof(report));
            CHECK(!%s_load_builder(&builder, wire, sizeof(wire), &report), "%s/%d builder accepted");
            CHECK(%s, "%s/%d builder verdict");
            value = %s_builder_root(&builder);
            CHECK(value == NULL || memcmp(value, &before, sizeof(before)) == 0, "%s/%d builder discarded partial value");
            %s_builder_shutdown(&builder);
        }
    }
`, cBytes(wire), fn, fn, shape, count, reasonCheck, shape, count, shape, count, root, root, root, fn, shape, count, fn, shape, count, fn, shape, count, reasonCheck, shape, count, fn, shape, count, fn)
		}
		// Equal map keys and an unreachable node are deliberately invalid in
		// their own right. Their valid twins use the corresponding ascending map
		// and reachable-node shapes, preserving the count placement.
		positiveShape := shape
		if shape == "map-equal" {
			positiveShape = "map-ascending"
		}
		if shape == "unreachable-node" {
			positiveShape = "node"
		}
		root, wire := cCountWire(t, u, positiveShape, 0, false)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &rep); !ok || err != nil || rep.Refused {
			t.Fatalf("%s: valid %s twin rejected: ok=%v err=%v report=%+v", shape, positiveShape, ok, err, rep)
		}
		fn := ir.RustSnake(root)
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        TableReport report; TableRetain store;
        memset(&report, 0, sizeof(report));
        memset(&store, 0, sizeof(store)); store.bytes=retain_bytes; store.capacity=sizeof(retain_bytes); store.ids=retain_ids; store.id_capacity=64;
        CHECK(%s_load_retain(region, sizeof(region), wire, sizeof(wire), &store, &report) != NULL, "%s valid twin");
        CHECK(!report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OK, "%s report reset on success");
    }
`, cBytes(wire), fn, shape, shape)
	}
	// A descending key paired with an oversized nested count carries two
	// independent faults; detection order may choose either refusal.
	root, descending := cCountWire(t, u, "map-descending", 1<<31, false)
	var descReport tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), descending, &descReport); ok || !descReport.Refused || !tablewire.ReadRefusal(err) {
		t.Fatalf("descending map compound control: ok=%v err=%v report=%+v", ok, err, descReport)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s }; TableReport report;
        CHECK(map_holder_load(region, sizeof(region), wire, sizeof(wire), &report) == NULL && report.refused, "descending map compound refusal");
    }
`, cBytes(descending))
	// A count cap followed by a malformed tail is also a compound refusal,
	// not a single-fault reason precedence assertion.
	root, trailing := cCountWire(t, u, "framing-tail", 1<<31, false)
	var tailReport tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), trailing, &tailReport); ok || !tailReport.Refused || !tablewire.ReadRefusal(err) {
		t.Fatalf("framing tail compound control: ok=%v err=%v report=%+v", ok, err, tailReport)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s }; TableReport report;
        CHECK(holder_load(region, sizeof(region), wire, sizeof(wire), &report) == NULL && report.refused, "count with damaged tail refuses");
    }
`, cBytes(trailing))
	// The short collection is malformed, not a storage-cap refusal.
	root, short := cCountWire(t, u, "map", 1, true)
	var rep tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), short, &rep); ok || !rep.Refused || tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged {
		t.Fatalf("short body: ok=%v err=%v report=%+v", ok, err, rep)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s }; TableReport report;
        CHECK(leaf_load(region, sizeof(region), wire, sizeof(wire), &report) == NULL && report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_WIRE_DAMAGED, "short body damage");
    }
    {
        uint8_t forms[2] = { 2, 255 };
        int reasons[2] = { SCHEMA_TABLE_MESSAGE_FORM_AS_FILE, SCHEMA_TABLE_NEWER_FORM };
        for (int i = 0; i < 2; i++) {
            TableReport report;
            CHECK(leaf_load(region, sizeof(region), &forms[i], 1, &report) == NULL && report.refused && report.reason == reasons[i], "form refusal");
        }
    }
`, cBytes(short))
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("count-refusal probe: %v\n%s", err, out)
	}
}

const cBomBoundarySchema = `package countcheck
fixed table FixedLeaf { a int32 = 0 }
table GraphLeaf { a int32 = 0
    next *GraphLeaf }
`

func TestJsonLeadingBOMBoundaryC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	u := unitFrom(t, cBomBoundarySchema)
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
        FixedLeaf vfs, vft; TableReport rfs, rft; char out_fs[128], out_ft[128]; int ok_fs, ok_ft; int64_t nfs, nft;
        GraphLeafBuilder bgs, bgt; TableReport rgs, rgt; char out_gs[128], out_gt[128]; int ok_gs, ok_gt; int64_t ngs, ngt;
        fixed_leaf_reset(&vfs); vfs.a = 42; memset(&rfs, 0, sizeof(rfs));
        ok_fs = fixed_leaf_from_json(&vfs, (const char *)text, (int64_t)sizeof(text), &rfs);
        fixed_leaf_reset(&vft); vft.a = 42; memset(&rft, 0, sizeof(rft));
        ok_ft = fixed_leaf_from_json(&vft, (const char *)text, (int64_t)sizeof(text), &rft);
        printf("BOM_CASE fixed/%s: strict=%%s tolerant=%%s\n", ok_fs ? "accept" : "refuse", ok_ft ? "accept" : "refuse");
        CHECK(ok_fs && !rfs.refused && rfs.refuse_reason == SCHEMA_TABLE_REFUSE_OK &&
            rfs.unknown == 0 && rfs.widened == 0 && rfs.retained == 0 && rfs.retain_lost == 0 && vfs.a == 0, "fixed/%s strict accept");
        CHECK(ok_ft && !rft.refused &&
            rft.unknown == 0 && rft.widened == 0 && rft.retained == 0 && rft.retain_lost == 0 && vft.a == 0, "fixed/%s tolerant accept");
        nfs = fixed_leaf_to_json(&vfs, out_fs, (int64_t)sizeof(out_fs));
        nft = fixed_leaf_to_json(&vft, out_ft, (int64_t)sizeof(out_ft));
        CHECK(nfs == (int64_t)sizeof(fixed_canon) && memcmp(out_fs, fixed_canon, sizeof(fixed_canon)) == 0 &&
            nft == (int64_t)sizeof(fixed_canon) && memcmp(out_ft, fixed_canon, sizeof(fixed_canon)) == 0, "fixed/%s canonical json");

        CHECK(graph_leaf_builder_init(&bgs) && graph_leaf_builder_init(&bgt), "graph/%s builder init");
        graph_leaf_builder_root(&bgs)->a = 42; memset(&rgs, 0, sizeof(rgs));
        ok_gs = graph_leaf_from_json(&bgs, (const char *)text, (int64_t)sizeof(text), &rgs);
        graph_leaf_builder_root(&bgt)->a = 42; memset(&rgt, 0, sizeof(rgt));
        ok_gt = graph_leaf_from_json(&bgt, (const char *)text, (int64_t)sizeof(text), &rgt);
        printf("BOM_CASE graph/%s: strict=%%s tolerant=%%s\n", ok_gs ? "accept" : "refuse", ok_gt ? "accept" : "refuse");
        CHECK(ok_gs && !rgs.refused && rgs.refuse_reason == SCHEMA_TABLE_REFUSE_OK &&
            rgs.unknown == 0 && rgs.widened == 0 && rgs.retained == 0 && rgs.retain_lost == 0 && graph_leaf_builder_root(&bgs)->a == 0, "graph/%s strict accept");
        CHECK(ok_gt && !rgt.refused &&
            rgt.unknown == 0 && rgt.widened == 0 && rgt.retained == 0 && rgt.retain_lost == 0 && graph_leaf_builder_root(&bgt)->a == 0, "graph/%s tolerant accept");
        ngs = graph_leaf_to_json(graph_leaf_builder_root(&bgs), out_gs, (int64_t)sizeof(out_gs));
        ngt = graph_leaf_to_json(graph_leaf_builder_root(&bgt), out_gt, (int64_t)sizeof(out_gt));
        CHECK(ngs == (int64_t)sizeof(graph_canon) && memcmp(out_gs, graph_canon, sizeof(graph_canon)) == 0 &&
            ngt == (int64_t)sizeof(graph_canon) && memcmp(out_gt, graph_canon, sizeof(graph_canon)) == 0, "graph/%s canonical json");
        graph_leaf_builder_shutdown(&bgs);
        graph_leaf_builder_shutdown(&bgt);
    }
`, cBytes([]byte(tc.text)), cBytes(fc), cBytes(gc), tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name)
		} else {
			fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        FixedLeaf vf; TableReport rfs, rft; int ok_fs, ok_ft;
        GraphLeafBuilder bgs, bgt; TableReport rgs, rgt; int ok_gs, ok_gt;
        fixed_leaf_reset(&vf); vf.a = 42; memset(&rfs, 0, sizeof(rfs));
        ok_fs = fixed_leaf_from_json(&vf, (const char *)text, (int64_t)sizeof(text), &rfs);
        CHECK(vf.a == 0, "fixed/%s strict discard");
        fixed_leaf_reset(&vf); vf.a = 42; memset(&rft, 0, sizeof(rft));
        ok_ft = fixed_leaf_from_json(&vf, (const char *)text, (int64_t)sizeof(text), &rft);
        CHECK(vf.a == 0, "fixed/%s tolerant discard");
        printf("BOM_CASE fixed/%s: strict=%%s tolerant=%%s\n", ok_fs ? "accept" : "refuse", ok_ft ? "accept" : "refuse");
        CHECK(!ok_fs && rfs.refused && rfs.refuse_reason == SCHEMA_TABLE_REFUSE_WIRE_DAMAGED &&
            rfs.unknown == 0 && rfs.widened == 0 && rfs.retained == 0 && rfs.retain_lost == 0, "fixed/%s strict refuse");
        CHECK(!ok_ft && rft.refused &&
            rft.unknown == 0 && rft.widened == 0 && rft.retained == 0 && rft.retain_lost == 0, "fixed/%s tolerant refuse");

        CHECK(graph_leaf_builder_init(&bgs) && graph_leaf_builder_init(&bgt), "graph/%s builder init");
        graph_leaf_builder_root(&bgs)->a = 42; memset(&rgs, 0, sizeof(rgs));
        ok_gs = graph_leaf_from_json(&bgs, (const char *)text, (int64_t)sizeof(text), &rgs);
        graph_leaf_builder_root(&bgt)->a = 42; memset(&rgt, 0, sizeof(rgt));
        ok_gt = graph_leaf_from_json(&bgt, (const char *)text, (int64_t)sizeof(text), &rgt);
        printf("BOM_CASE graph/%s: strict=%%s tolerant=%%s\n", ok_gs ? "accept" : "refuse", ok_gt ? "accept" : "refuse");
        CHECK(!ok_gs && rgs.refused && rgs.refuse_reason == SCHEMA_TABLE_REFUSE_WIRE_DAMAGED &&
            rgs.unknown == 0 && rgs.widened == 0 && rgs.retained == 0 && rgs.retain_lost == 0 && graph_leaf_builder_root(&bgs)->a == 0, "graph/%s strict refuse");
        CHECK(!ok_gt && rgt.refused &&
            rgt.unknown == 0 && rgt.widened == 0 && rgt.retained == 0 && rgt.retain_lost == 0 && graph_leaf_builder_root(&bgt)->a == 0, "graph/%s tolerant refuse");
        graph_leaf_builder_shutdown(&bgs);
        graph_leaf_builder_shutdown(&bgt);
    }
`, cBytes([]byte(tc.text)), tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name, tc.name)
		}
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCCountRefusalProbe(t, u, body.String())
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("bom-boundary probe: %v\n%s", err, out)
	}
}
