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
	shapes := []string{"map", "list", "nested", "optional", "array", "union", "union-array", "nested-union", "map-value", "map-equal", "map-ascending", "node", "unreachable-node"}
	for _, shape := range shapes {
		for _, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
			root, wire := cCountWire(t, u, shape, count, false)
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
			fn := ir.RustSnake(root)
			fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        int old_damage, retaining;
        for (old_damage = 0; old_damage <= 1; old_damage++) {
            for (retaining = 0; retaining <= 1; retaining++) {
                TableReport report;
                TableRetain store;
                const void * loaded;
                memset(&report, 0, sizeof(report));
                report.unknown = 7; report.kind_mismatch = 11; report.widened = 13; report.clamped = 17;
                report.duplicate = 19; report.retained = 23; report.retain_lost = 29; report.malformed = old_damage;
                memset(&store, 0, sizeof(store));
                store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes); store.used = 12;
                store.ids = retain_ids; store.id_capacity = 64; store.id_used = 1; store.count = 1;
                loaded = retaining
                    ? (const void *)%s_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                    : (const void *)%s_load_tolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
                CHECK(loaded == NULL, "%s/%d partial value");
                CHECK(report.refused && report.reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP, "%s/%d named count refusal");
                CHECK(report.malformed == old_damage, "%s/%d damage history");
                CHECK(report.unknown == %d && report.kind_mismatch == 11 && report.widened == %d &&
                    report.clamped == 17 && report.duplicate == %d &&
                    report.retained == (retaining ? %d : 23) &&
                    report.retain_lost == (retaining ? %d : 29), "%s/%d counters");
                CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "%s/%d retained partial state");
            }
        }
        {
            TableReport strict_report;
            %sBuilder builder;
            TableReport builder_report;
            CHECK(%s_load(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &strict_report) == NULL, "%s/%d strict load");
            CHECK(strict_report.refuse_reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP && !strict_report.malformed, "%s/%d strict reason");
            CHECK(%s_builder_init(&builder), "%s/%d builder init");
            memset(&builder_report, 0, sizeof(builder_report));
            builder_report.unknown = 7; builder_report.widened = 13; builder_report.duplicate = 19;
            CHECK(!%s_load_builder_tolerant(&builder, wire, (int64_t)sizeof(wire), &builder_report), "%s/%d builder refusal");
            CHECK(!builder_report.malformed && !builder_report.refused &&
                builder_report.unknown == %d && builder_report.widened == %d && builder_report.duplicate == %d, "%s/%d builder counters");
            %s_builder_shutdown(&builder);
        }
    }
`, cBytes(wire), fn, fn, shape, count, shape, count, shape, count, r.Unknown, r.Widened, r.Duplicate, retainingReport.Retained, retainingReport.RetainLost, shape, count, shape, count,
				root, fn, shape, count, shape, count, fn, shape, count, fn, shape, count, r.Unknown, r.Widened, r.Duplicate, shape, count, fn)
		}
		root, wire := cCountWire(t, u, shape, 0, false)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), wire, &rep); !ok || err != nil || rep.Malformed {
			t.Fatalf("%s: valid twin rejected by oracle: %v %v %+v", shape, ok, err, rep)
		}
		fn := ir.RustSnake(root)
		fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        int retaining;
        for (retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store;
            const void * loaded;
            memset(&report, 0, sizeof(report));
            report.refused = 1; report.reason = SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP; report.malformed = 1;
            memset(&store, 0, sizeof(store));
            store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
            store.ids = retain_ids; store.id_capacity = 64;
            loaded = retaining
                ? (const void *)%s_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)%s_load_tolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded != NULL, "%s prior refusal is not current control state");
            CHECK(report.refused && report.reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP && report.malformed, "%s history preserved after good read");
        }
    }
`, cBytes(wire), fn, fn, shape, shape)
	}
	root, descending := cCountWire(t, u, "map-descending", 1<<31, false)
	var descendingReport tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup(root)), descending, &descendingReport); ok || err != nil || !descendingReport.Malformed {
		t.Fatalf("descending-map control is not damage: %v %v %+v", ok, err, descendingReport)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        int retaining;
        for (retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store;
            const void * loaded;
            memset(&report, 0, sizeof(report));
            memset(&store, 0, sizeof(store));
            store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
            store.ids = retain_ids; store.id_capacity = 64;
            loaded = retaining
                ? (const void *)map_holder_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)map_holder_load_tolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && report.malformed && !report.refused && report.reason != SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP, "descending key precedes nested value refusal");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "descending map retained state");
        }
    }
`, cBytes(descending))
	_, short := cCountWire(t, u, "map", 1, true)
	var rep tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup("Leaf")), short, &rep); ok || err != nil || !rep.Malformed {
		t.Fatalf("body-short control is not damage: %v %v %+v", ok, err, rep)
	}
	fmt.Fprintf(&body, `
    {
        static const uint8_t wire[] = { %s };
        int retaining;
        for (retaining = 0; retaining <= 1; retaining++) {
            TableReport report;
            TableRetain store;
            const void * loaded;
            memset(&report, 0, sizeof(report));
            memset(&store, 0, sizeof(store));
            store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
            store.ids = retain_ids; store.id_capacity = 64;
            loaded = retaining
                ? (const void *)leaf_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)leaf_load_tolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && report.malformed && !report.refused && report.reason != SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP, "body-short remains damage");
            CHECK(!retaining || (store.used == 0 && store.id_used == 0 && store.count == 0), "body-short retained state");
        }
    }
    {
        uint8_t forms[2] = { 2, 255 };
        int reasons[2] = { SCHEMA_TABLE_MESSAGE_FORM_AS_FILE, SCHEMA_TABLE_NEWER_FORM };
        int i;
        for (i = 0; i < 2; i++) {
            TableReport report;
            memset(&report, 0, sizeof(report));
            report.malformed = 1; report.unknown = 7;
            CHECK(leaf_load_tolerant(region, (int64_t)sizeof(region), &forms[i], 1, &report) == NULL && report.refused && report.reason == reasons[i], "form refusal name");
            CHECK(report.malformed && report.unknown == 7, "form preserves history");
        }
    }
`, cBytes(short))
	for _, shape := range []string{"widened", "framing-tail"} {
		root, wire := cCountWire(t, u, shape, 1<<31, false)
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
        int mode;
        for (mode = 0; mode < 3; mode++) {
            TableReport report;
            TableRetain store;
            const void * loaded;
            memset(&report, 0, sizeof(report));
            report.unknown = 7; report.widened = 13; report.retained = 23; report.retain_lost = 29;
            memset(&store, 0, sizeof(store));
            if (mode == 1) {
                store.bytes = retain_bytes; store.capacity = (int64_t)sizeof(retain_bytes);
                store.ids = retain_ids; store.id_capacity = 64;
            }
            loaded = mode > 0
                ? (const void *)holder_load_retain(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &store, &report)
                : (const void *)holder_load_tolerant(region, (int64_t)sizeof(region), wire, (int64_t)sizeof(wire), &report);
            CHECK(loaded == NULL && report.refused && report.reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP, "%s reason");
            CHECK(!report.malformed && report.unknown == %d && report.widened == %d, "%s prefix counters");
            CHECK(report.retained == (mode == 1 ? %d : 23) &&
                report.retain_lost == (mode == 2 ? %d : (mode == 1 ? %d : 29)), "%s retention counters");
            CHECK(store.used == 0 && store.id_used == 0 && store.count == 0, "%s partial retained state");
        }
        CHECK(holder_load_measure(wire, (int64_t)sizeof(wire)) < 0, "%s measure < 0");
    }
`, cBytes(wire), shape, report.Unknown, report.Widened, shape, retainingReport.Retained, lostReport.RetainLost, retainingReport.RetainLost, shape, shape, shape)
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("count-refusal probe: %v\n%s", err, out)
	}
}
